import { AudioRingBuffer } from "./audio-ring-buffer";
import audioWorkletUrl from "./audio-worklet.ts?worker&url";
const MIN_BUFFER_MS = 300;
const RING_BUFFER_SECONDS = 4;
const PEAK_HOLD_SEC = 1.5;
const PEAK_HOLD_DECAY = 0.9;
const BAR_ATTACK = 0.6;
const BAR_RELEASE = 0.92;
/**
 * Decodes compressed audio using WebCodecs AudioDecoder and plays it
 * through the Web Audio API. Decoded samples are written to a
 * SharedArrayBuffer-backed AudioRingBuffer, consumed by an AudioWorklet
 * on the audio thread for glitch-free playback. Supports per-track
 * muting, metering (for VU meters), and provides the current playback
 * PTS used by the renderer for A/V sync.
 */
export class PrismAudioDecoder {
    context = null;
    ownsContext = false;
    decoder = null;
    playing = false;
    starting = false;
    sampleRate = 0;
    gainNode = null;
    muted = false;
    numChannels = 0;
    _suspended = false;
    metering = false;
    _peakHold = [];
    _peakHoldTime = [];
    _smoothedPeak = [];
    ringBuffer = null;
    workletNode = null;
    firstPTS = -1;
    totalScheduled = 0;
    totalSilenceMs = 0;
    samplesWritten = 0;
    // --- diagnostics ---
    _diagCallbackCount = 0;
    _diagDecodeErrors = 0;
    _diagLastCallbackTime = 0;
    _diagCallbackIntervalSum = 0;
    _diagCallbackIntervalMax = 0;
    _diagCallbackIntervalMin = Infinity;
    _diagGapRepairs = 0;
    _diagLastDrift = 0;
    _diagMaxDrift = 0;
    _diagUnderruns = 0;
    _diagFirstCallbackTime = 0;
    _diagLastPTS = -1;
    _diagPtsJumps = 0;
    _diagPtsJumpMaxUs = 0;
    // --- input PTS tracking (before WebCodecs) ---
    _diagLastInputPTS = -1;
    _diagInputPtsJumps = 0;
    _diagInputPtsWraps = 0;
    _ptsEpochReset = false;
    _configuredCodec = "";
    async configure(codec, sampleRate, channels, ctx) {
        this.reset();
        this.sampleRate = sampleRate;
        this.numChannels = channels;
        this._peakHold = new Array(channels).fill(0);
        this._peakHoldTime = new Array(channels).fill(0);
        this._smoothedPeak = new Array(channels).fill(0);
        if (ctx) {
            this.context = ctx;
            this.ownsContext = false;
        }
        else {
            this.context = new AudioContext({ sampleRate, latencyHint: "interactive" });
            this.ownsContext = true;
            if (this.context.state === "running") {
                await this.context.suspend();
            }
        }
        this.gainNode = this.context.createGain();
        this.gainNode.gain.value = this.muted ? 0 : 1;
        this.gainNode.connect(this.context.destination);
        const ringSize = Math.ceil(sampleRate * RING_BUFFER_SECONDS);
        this.ringBuffer = new AudioRingBuffer();
        this.ringBuffer.init(channels, ringSize, sampleRate);
        try {
            await this.context.audioWorklet.addModule(audioWorkletUrl);
        }
        catch (e) {
            console.error("[AudioDecoder] Failed to load AudioWorklet module", e);
        }
        this.workletNode = new AudioWorkletNode(this.context, "prism-audio-worklet", {
            numberOfInputs: 0,
            numberOfOutputs: 1,
            outputChannelCount: [channels],
        });
        this.workletNode.connect(this.gainNode);
        const shared = this.ringBuffer.getSharedBuffers();
        this.workletNode.port.postMessage({
            type: "init",
            sab: shared.audioBuffers,
            commBuffer: shared.commBuffer,
            numChannels: channels,
            sampleRate,
        });
        this._configuredCodec = codec;
        this.createDecoder(codec, channels, sampleRate);
    }
    enableMetering() {
        this.metering = true;
    }
    createDecoder(codec, channels, sampleRate) {
        this.decoder = new AudioDecoder({
            output: (frame) => {
                this.onDecodedAudio(frame);
            },
            error: (err) => {
                console.error("[AudioDecoder] error:", err.message);
                this._diagDecodeErrors++;
                this.recoverDecoder();
            },
        });
        this.decoder.configure({ codec, numberOfChannels: channels, sampleRate });
    }
    recoverDecoder() {
        if (!this._configuredCodec || !this.sampleRate || !this.numChannels)
            return;
        if (this.decoder) {
            try {
                this.decoder.close();
            }
            catch { /* ignore */ }
            this.decoder = null;
        }
        console.warn("[AudioDecoder] Recovering decoder");
        this.createDecoder(this._configuredCodec, this.numChannels, this.sampleRate);
    }
    disableMetering() {
        this.metering = false;
        this._peakHold = new Array(this.numChannels).fill(0);
        this._peakHoldTime = new Array(this.numChannels).fill(0);
    }
    decode(data, timestamp, _isDisco) {
        if (!this.decoder || this.decoder.state !== "configured")
            return;
        if (this._diagLastInputPTS >= 0) {
            const gap = Math.abs(timestamp - this._diagLastInputPTS);
            if (gap > 100_000)
                this._diagInputPtsJumps++;
            if (timestamp < this._diagLastInputPTS &&
                this._diagLastInputPTS - timestamp > 30_000_000) {
                this._diagInputPtsWraps++;
                this._ptsEpochReset = true;
            }
        }
        this._diagLastInputPTS = timestamp;
        try {
            this.decoder.decode(new EncodedAudioChunk({ type: "key", timestamp, data }));
        }
        catch {
            this._diagDecodeErrors++;
        }
    }
    setSuspended(suspended) {
        this._suspended = suspended;
    }
    setMuted(muted) {
        if (this.muted === muted)
            return;
        this.muted = muted;
        if (this.gainNode) {
            this.gainNode.gain.value = muted ? 0 : 1;
        }
    }
    isMuted() {
        return this.muted;
    }
    getAudioDebug() {
        return {
            muted: this.muted,
            suspended: this._suspended,
            gain: this.gainNode?.gain.value ?? -1,
            playing: this.playing,
            contextState: this.context?.state ?? "null",
        };
    }
    isMetering() {
        return this.metering;
    }
    getPlaybackPTS() {
        if (!this.ringBuffer || !this.playing)
            return -1;
        return this.ringBuffer.readPTS();
    }
    getLevels() {
        if (!this.metering || !this.ringBuffer) {
            return { peak: [], rms: [], peakHold: [], channels: this.numChannels };
        }
        const raw = this.ringBuffer.readLevels();
        const now = performance.now() / 1000;
        const peak = [];
        const rms = [];
        for (let c = 0; c < this.numChannels; c++) {
            const maxAbs = raw.peak[c] ?? 0;
            const rmsVal = raw.rms[c] ?? 0;
            if (c < this._smoothedPeak.length) {
                if (maxAbs > this._smoothedPeak[c]) {
                    this._smoothedPeak[c] += (maxAbs - this._smoothedPeak[c]) * BAR_ATTACK;
                }
                else {
                    this._smoothedPeak[c] *= BAR_RELEASE;
                    if (this._smoothedPeak[c] < 0.0001)
                        this._smoothedPeak[c] = 0;
                }
            }
            peak.push(this._smoothedPeak[c] ?? maxAbs);
            rms.push(rmsVal);
            if (c < this._peakHold.length) {
                if (maxAbs >= this._peakHold[c]) {
                    this._peakHold[c] = maxAbs;
                    this._peakHoldTime[c] = now;
                }
                else if (now - this._peakHoldTime[c] > PEAK_HOLD_SEC) {
                    this._peakHold[c] *= PEAK_HOLD_DECAY;
                    if (this._peakHold[c] < 0.001)
                        this._peakHold[c] = 0;
                }
            }
        }
        return { peak, rms, peakHold: this._peakHold, channels: this.numChannels };
    }
    getStats() {
        if (this.ringBuffer) {
            const stats = this.ringBuffer.getStats();
            return {
                queueLengthMs: stats.queueLengthMs,
                totalSilenceInsertedMs: stats.totalSilenceInsertedMs,
                isPlaying: this.playing,
            };
        }
        return {
            queueLengthMs: 0,
            totalSilenceInsertedMs: Math.floor(this.totalSilenceMs),
            isPlaying: this.playing,
        };
    }
    getDiagnostics() {
        const ringStats = this.ringBuffer?.getStats();
        const avgInterval = this._diagCallbackCount > 1
            ? this._diagCallbackIntervalSum / (this._diagCallbackCount - 1)
            : 0;
        return {
            callbackCount: this._diagCallbackCount,
            callbacksPerSec: this._diagFirstCallbackTime > 0
                ? this._diagCallbackCount / ((performance.now() - this._diagFirstCallbackTime) / 1000)
                : 0,
            avgCallbackIntervalMs: avgInterval,
            minCallbackIntervalMs: this._diagCallbackIntervalMin === Infinity ? 0 : this._diagCallbackIntervalMin,
            maxCallbackIntervalMs: this._diagCallbackIntervalMax,
            scheduleAheadMs: ringStats?.queueLengthMs ?? 0,
            lastDriftMs: this._diagLastDrift * 1000,
            maxDriftMs: this._diagMaxDrift * 1000,
            gapRepairs: this._diagGapRepairs,
            underruns: this._diagUnderruns,
            totalSilenceMs: ringStats?.totalSilenceInsertedMs ?? this.totalSilenceMs,
            totalScheduled: this.totalScheduled,
            decodeErrors: this._diagDecodeErrors,
            ptsJumps: this._diagPtsJumps,
            ptsJumpMaxMs: this._diagPtsJumpMaxUs / 1000,
            inputPtsJumps: this._diagInputPtsJumps,
            inputPtsWraps: this._diagInputPtsWraps,
            lastInputPTS: this._diagLastInputPTS,
            lastOutputPTS: this.ringBuffer?.readPTS() ?? this._diagLastPTS,
            contextState: this.context?.state ?? "closed",
            contextSampleRate: this.context?.sampleRate ?? 0,
            contextCurrentTime: this.context?.currentTime ?? 0,
            contextBaseLatency: this.context?.baseLatency ?? 0,
            contextOutputLatency: this.context?.outputLatency ?? 0,
            isPlaying: this.playing,
            pendingFrames: 0,
        };
    }
    resetDiagnostics() {
        this._diagCallbackCount = 0;
        this._diagDecodeErrors = 0;
        this._diagLastCallbackTime = 0;
        this._diagCallbackIntervalSum = 0;
        this._diagCallbackIntervalMax = 0;
        this._diagCallbackIntervalMin = Infinity;
        this._diagGapRepairs = 0;
        this._diagLastDrift = 0;
        this._diagMaxDrift = 0;
        this._diagUnderruns = 0;
        this._diagFirstCallbackTime = 0;
        this._diagLastPTS = -1;
        this._diagPtsJumps = 0;
        this._diagPtsJumpMaxUs = 0;
        this._diagLastInputPTS = -1;
        this._diagInputPtsJumps = 0;
        this._diagInputPtsWraps = 0;
        this._ptsEpochReset = false;
    }
    reset() {
        if (this.decoder) {
            try {
                this.decoder.close();
            }
            catch { /* ignore */ }
            this.decoder = null;
        }
        if (this.workletNode) {
            this.workletNode.disconnect();
            this.workletNode = null;
        }
        if (this.ringBuffer) {
            this.ringBuffer.destroy();
            this.ringBuffer = null;
        }
        if (this.gainNode) {
            this.gainNode.disconnect();
            this.gainNode = null;
        }
        if (this.context && this.ownsContext) {
            this.context.close();
        }
        this.context = null;
        this.ownsContext = false;
        this.playing = false;
        this.starting = false;
        this.firstPTS = -1;
        this._ptsEpochReset = false;
        this.totalScheduled = 0;
        this.totalSilenceMs = 0;
        this.samplesWritten = 0;
        this.numChannels = 0;
        this._peakHold = [];
        this._peakHoldTime = [];
        this._smoothedPeak = [];
        this._suspended = false;
        this.resetDiagnostics();
    }
    onDecodedAudio(audioData) {
        if (!this.context || !this.ringBuffer) {
            audioData.close();
            return;
        }
        const numFrames = audioData.numberOfFrames;
        const durationSec = numFrames / audioData.sampleRate;
        const pts = audioData.timestamp;
        const cbNow = performance.now();
        this._diagCallbackCount++;
        if (this._diagFirstCallbackTime === 0)
            this._diagFirstCallbackTime = cbNow;
        if (this._diagLastCallbackTime > 0) {
            const interval = cbNow - this._diagLastCallbackTime;
            this._diagCallbackIntervalSum += interval;
            if (interval > this._diagCallbackIntervalMax)
                this._diagCallbackIntervalMax = interval;
            if (interval < this._diagCallbackIntervalMin)
                this._diagCallbackIntervalMin = interval;
        }
        this._diagLastCallbackTime = cbNow;
        if (this._diagLastPTS >= 0) {
            const expectedDurationUs = durationSec * 1_000_000;
            const gap = Math.abs(pts - this._diagLastPTS - expectedDurationUs);
            if (gap > expectedDurationUs * 0.5) {
                this._diagPtsJumps++;
                if (gap > this._diagPtsJumpMaxUs)
                    this._diagPtsJumpMaxUs = gap;
            }
        }
        this._diagLastPTS = pts;
        if (this.firstPTS < 0) {
            this.firstPTS = pts;
        }
        if (this._ptsEpochReset) {
            this._ptsEpochReset = false;
            const inputPTS = this._diagLastInputPTS;
            if (inputPTS >= 0 && this.workletNode) {
                this.ringBuffer.clear();
                this.samplesWritten = 0;
                this.workletNode.port.postMessage({
                    type: "set-pts",
                    pts: inputPTS,
                    sampleOffset: 0,
                });
            }
        }
        const written = this.ringBuffer.write(audioData);
        audioData.close();
        if (written > 0) {
            this.samplesWritten += written;
            this.totalScheduled++;
        }
        if (!this.playing && !this.starting) {
            const bufferedMs = (this.samplesWritten / this.sampleRate) * 1000;
            if (bufferedMs >= MIN_BUFFER_MS) {
                this.startPlayback();
            }
        }
    }
    startPlayback() {
        if (!this.context || this.playing || this.starting || !this.workletNode || !this.ringBuffer)
            return;
        if (this._suspended) {
            return;
        }
        this.starting = true;
        if (this.workletNode) {
            this.workletNode.port.postMessage({
                type: "set-pts",
                pts: this.firstPTS,
                sampleOffset: 0,
            });
        }
        this.ringBuffer.play();
        this.context.resume().then(() => {
            this.playing = true;
            this.starting = false;
        });
    }
}
