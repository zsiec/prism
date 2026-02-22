declare class AudioWorkletProcessor {
	readonly port: MessagePort;
	constructor();
}

declare function registerProcessor(name: string, ctor: new () => AudioWorkletProcessor): void;

// IMPORTANT: SharedStates must be kept in sync with audio-ring-buffer.ts SharedStates
const SharedStates = {
	BUFF_START: 0,
	BUFF_END: 1,
	INSERTED_SILENCE_MS: 2,
	IS_PLAYING: 3,
	PTS_HI: 4,
	PTS_LO: 5,
	NUM_CHANNELS: 6,
	SAMPLE_RATE: 7,
	PEAK_BASE: 8,
} as const;

const MAX_CHANNELS = 8;
const RMS_BASE = SharedStates.PEAK_BASE + MAX_CHANNELS;

// Adaptive rate control: when buffer is between LOW and HIGH, consume at 1x.
// Below LOW, slow down consumption; above HIGH, speed up.
// This prevents underruns by stretching audio imperceptibly (~1-2%).
const TARGET_BUFFER_MS = 1000;
const LOW_WATER_MS = 600;
const HIGH_WATER_MS = 1500;
const MAX_SPEED_RATIO = 1.02;
const MIN_SPEED_RATIO = 0.98;

function writePTS(states: Int32Array, pts: number): void {
	const intPart = Math.trunc(pts);
	Atomics.store(states, SharedStates.PTS_HI, Math.trunc(intPart / 1_000_000));
	Atomics.store(states, SharedStates.PTS_LO, intPart % 1_000_000);
}

class PrismAudioWorkletProcessor extends AudioWorkletProcessor {
	private sharedStates: Int32Array | null = null;
	private floatViews: Float32Array[] = [];
	private ringSize = 0;
	private numChannels = 0;

	private basePTS = 0;
	private sampleOffset = 0;
	private samplesConsumed = 0;
	private localSampleRate = 0;

	private fractionalAccum = 0;
	private resampleScratch: Float32Array | null = null;

	constructor() {
		super();
		this.port.onmessage = (ev: MessageEvent) => this.handleMessage(ev.data);
	}

	private handleMessage(msg: { type: string; sab?: SharedArrayBuffer[]; commBuffer?: SharedArrayBuffer; numChannels?: number; sampleRate?: number; pts?: number; sampleOffset?: number }): void {
		if (msg.type === "init") {
			this.sharedStates = new Int32Array(msg.commBuffer!);
			this.numChannels = msg.numChannels!;
			this.localSampleRate = msg.sampleRate!;
			this.floatViews = [];
			for (let c = 0; c < this.numChannels; c++) {
				this.floatViews.push(new Float32Array(msg.sab![c]));
			}
			this.ringSize = this.floatViews[0].length;
		} else if (msg.type === "set-pts") {
			this.basePTS = msg.pts!;
			this.sampleOffset = msg.sampleOffset ?? 0;
			this.samplesConsumed = 0;
		}
	}

	process(_inputs: Float32Array[][], outputs: Float32Array[][], _parameters: Record<string, Float32Array>): boolean {
		if (!this.sharedStates || this.floatViews.length === 0) return true;

		const isPlaying = Atomics.load(this.sharedStates, SharedStates.IS_PLAYING);
		if (!isPlaying) {
			this.outputSilence(outputs);
			return true;
		}

		const output = outputs[0];
		const framesToFill = output[0].length;
		const start = Atomics.load(this.sharedStates, SharedStates.BUFF_START);
		const end = Atomics.load(this.sharedStates, SharedStates.BUFF_END);

		let available: number;
		if (start === end) {
			available = 0;
		} else if (end > start) {
			available = end - start;
		} else {
			available = (this.ringSize - start) + end;
		}

		if (available === 0) {
			this.outputSilence(outputs);
			Atomics.add(this.sharedStates, SharedStates.INSERTED_SILENCE_MS,
				Math.round((framesToFill / this.localSampleRate) * 1000));
			// Do NOT advance samplesConsumed or PTS during underruns.
			// The renderer detects stalled audio (200ms) and falls back to
			// wall-clock free-run for video. When real audio resumes, PTS
			// will accurately reflect the media timeline position.
			return true;
		}

		const bufferMs = (available / this.localSampleRate) * 1000;
		const speedRatio = this.computeSpeedRatio(bufferMs);

		const channelsToFill = Math.min(output.length, this.numChannels);

		if (speedRatio >= 0.999 && speedRatio <= 1.001) {
			// 1x path: no resampling needed, just copy directly
			const toRead = Math.min(framesToFill, available);
			this.readFromRing(start, toRead, output, channelsToFill);

			if (toRead < framesToFill) {
				for (let c = 0; c < channelsToFill; c++) {
					output[c].fill(0, toRead);
				}
			}

			for (let c = channelsToFill; c < output.length; c++) {
				output[c].fill(0);
			}

			const newStart = (start + toRead) % this.ringSize;
			Atomics.store(this.sharedStates, SharedStates.BUFF_START, newStart);
			this.samplesConsumed += toRead;
		} else {
			// Adaptive path: consume more or fewer source samples than output frames.
			// speedRatio > 1 means consume MORE source samples (play faster, drain buffer).
			// speedRatio < 1 means consume FEWER source samples (play slower, preserve buffer).
			const exactSourceSamples = framesToFill * speedRatio + this.fractionalAccum;
			const sourceSamples = Math.floor(exactSourceSamples);
			this.fractionalAccum = exactSourceSamples - sourceSamples;

			const toRead = Math.min(sourceSamples, available);

			if (!this.resampleScratch || this.resampleScratch.length < toRead) {
				this.resampleScratch = new Float32Array(Math.max(toRead, 256));
			}

			for (let c = 0; c < channelsToFill; c++) {
				this.readChannelFromRing(start, toRead, c, this.resampleScratch);
				this.linearResample(this.resampleScratch, toRead, output[c], framesToFill);
			}

			for (let c = channelsToFill; c < output.length; c++) {
				output[c].fill(0);
			}

			const newStart = (start + toRead) % this.ringSize;
			Atomics.store(this.sharedStates, SharedStates.BUFF_START, newStart);
			this.samplesConsumed += toRead;
		}

		const currentPTS = this.basePTS +
			((this.sampleOffset + this.samplesConsumed) / this.localSampleRate) * 1_000_000;
		writePTS(this.sharedStates, currentPTS);

		this.computeLevels(output, channelsToFill, framesToFill);

		return true;
	}

	private computeSpeedRatio(bufferMs: number): number {
		if (bufferMs < LOW_WATER_MS) {
			// Buffer is getting dangerously low -- slow down consumption
			const t = bufferMs / LOW_WATER_MS;
			return MIN_SPEED_RATIO + t * (1.0 - MIN_SPEED_RATIO);
		} else if (bufferMs > HIGH_WATER_MS) {
			// Buffer is too full -- speed up consumption to drain
			const excess = bufferMs - HIGH_WATER_MS;
			const range = TARGET_BUFFER_MS;
			const t = Math.min(excess / range, 1.0);
			return 1.0 + t * (MAX_SPEED_RATIO - 1.0);
		}
		return 1.0;
	}

	private readFromRing(start: number, count: number, output: Float32Array[], channels: number): void {
		for (let c = 0; c < channels; c++) {
			const dst = output[c];
			const src = this.floatViews[c];
			const firstPart = Math.min(count, this.ringSize - start);
			dst.set(src.subarray(start, start + firstPart), 0);
			if (count > firstPart) {
				dst.set(src.subarray(0, count - firstPart), firstPart);
			}
		}
	}

	private readChannelFromRing(start: number, count: number, channel: number, dst: Float32Array): void {
		const src = this.floatViews[channel];
		const firstPart = Math.min(count, this.ringSize - start);
		dst.set(src.subarray(start, start + firstPart), 0);
		if (count > firstPart) {
			dst.set(src.subarray(0, count - firstPart), firstPart);
		}
	}

	private linearResample(src: Float32Array, srcLen: number, dst: Float32Array, dstLen: number): void {
		if (srcLen === 0) {
			dst.fill(0);
			return;
		}
		if (srcLen === 1) {
			dst.fill(src[0]);
			return;
		}
		const ratio = (srcLen - 1) / (dstLen - 1);
		for (let i = 0; i < dstLen; i++) {
			const srcPos = i * ratio;
			const idx = Math.floor(srcPos);
			const frac = srcPos - idx;
			if (idx + 1 < srcLen) {
				dst[i] = src[idx] * (1 - frac) + src[idx + 1] * frac;
			} else {
				dst[i] = src[idx];
			}
		}
	}

	private outputSilence(outputs: Float32Array[][]): void {
		const output = outputs[0];
		for (let c = 0; c < output.length; c++) {
			output[c].fill(0);
		}
	}

	private computeLevels(output: Float32Array[], channels: number, samples: number): void {
		if (!this.sharedStates) return;

		for (let c = 0; c < channels; c++) {
			const data = output[c];
			let maxAbs = 0;
			let sumSq = 0;
			for (let i = 0; i < samples; i++) {
				const s = data[i];
				const abs = s < 0 ? -s : s;
				if (abs > maxAbs) maxAbs = abs;
				sumSq += s * s;
			}
			const rmsVal = Math.sqrt(sumSq / samples);

			Atomics.store(this.sharedStates, SharedStates.PEAK_BASE + c,
				Math.round(maxAbs * 1_000_000));
			Atomics.store(this.sharedStates, RMS_BASE + c,
				Math.round(rmsVal * 1_000_000));
		}
	}
}

registerProcessor("prism-audio-worklet", PrismAudioWorkletProcessor);
