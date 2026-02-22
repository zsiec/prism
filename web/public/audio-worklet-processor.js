const BUFF_START = 0;
const BUFF_END = 1;
const INSERTED_SILENCE_MS = 2;
const IS_PLAYING = 3;

class PrismAudioProcessor extends AudioWorkletProcessor {
	constructor() {
		super();
		this.contextSampleFrequency = -1;
		this.sharedStates = null;
		this.sharedAudioBuffers = null;
		this.circularBufferSize = 0;
		this.totalSilenceInsertedSamples = 0;
		this.port.onmessage = this.handleMessage.bind(this);
	}

	handleMessage(e) {
		if (e.data.type === "init") {
			const config = e.data.config;
			this.contextSampleFrequency = config.contextSampleFrequency;
			this.circularBufferSize = config.circularBufferSize;
			this.sharedStates = new Int32Array(config.commBuffer);
			this.sharedAudioBuffers = config.audioBuffers;
		}
	}

	process(_inputs, outputs) {
		const output = outputs[0];
		const numSamples = output[0].length;
		if (numSamples <= 0 || !this.sharedStates) return true;

		const isPlaying = Atomics.load(this.sharedStates, IS_PLAYING);
		if (isPlaying === 0) return true;

		const start = Atomics.load(this.sharedStates, BUFF_START);
		const end = Atomics.load(this.sharedStates, BUFF_END);
		if (start < 0 || end < 0) return true;

		const available = this.getUsedSlots(start, end);
		const toRead = Math.min(numSamples, available);

		if (toRead === 0) {
			this.totalSilenceInsertedSamples += numSamples;
			const totalMs = (this.totalSilenceInsertedSamples * 1000) / this.contextSampleFrequency;
			Atomics.store(this.sharedStates, INSERTED_SILENCE_MS, Math.floor(totalMs));
			return true;
		}

		if (toRead < numSamples) {
			this.totalSilenceInsertedSamples += (numSamples - toRead);
			const totalMs = (this.totalSilenceInsertedSamples * 1000) / this.contextSampleFrequency;
			Atomics.store(this.sharedStates, INSERTED_SILENCE_MS, Math.floor(totalMs));
		}

		let newStart;
		if (start + toRead <= this.circularBufferSize) {
			for (let c = 0; c < output.length; c++) {
				const ringView = new Float32Array(this.sharedAudioBuffers[c], start * Float32Array.BYTES_PER_ELEMENT, toRead);
				output[c].set(ringView);
			}
			newStart = start + toRead;
		} else {
			const firstHalf = this.circularBufferSize - start;
			const secondHalf = toRead - firstHalf;
			for (let c = 0; c < output.length; c++) {
				const ring1 = new Float32Array(this.sharedAudioBuffers[c], start * Float32Array.BYTES_PER_ELEMENT, firstHalf);
				output[c].set(ring1);
				const ring2 = new Float32Array(this.sharedAudioBuffers[c], 0, secondHalf);
				output[c].set(ring2, firstHalf);
			}
			newStart = secondHalf;
		}
		Atomics.store(this.sharedStates, BUFF_START, newStart);

		return true;
	}

	getUsedSlots(start, end) {
		if (start === end) return 0;
		if (end > start) return end - start;
		return (this.circularBufferSize - start) + end;
	}
}

registerProcessor("prism-audio-processor", PrismAudioProcessor);
