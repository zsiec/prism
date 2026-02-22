const MAX_CHANNELS = 8;
// IMPORTANT: SharedStates must be kept in sync with audio-worklet.ts SharedStates
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
    RMS_BASE: 8 + MAX_CHANNELS,
};
const NUM_SHARED_STATES = SharedStates.RMS_BASE + MAX_CHANNELS;
export { SharedStates, NUM_SHARED_STATES, MAX_CHANNELS };
function readPTSFromStates(states) {
    const hi = Atomics.load(states, SharedStates.PTS_HI);
    const lo = Atomics.load(states, SharedStates.PTS_LO);
    return hi * 1_000_000 + lo;
}
/**
 * Lock-free ring buffer for audio samples backed by SharedArrayBuffers.
 * The main thread writes decoded PCM samples; the AudioWorklet thread
 * reads them for playback. Communication (pointers, PTS, levels) uses
 * Atomics on a shared Int32Array to avoid locks or message passing on
 * the audio-critical path.
 */
export class AudioRingBuffer {
    audioBuffers = null;
    floatViews = [];
    commBuffer;
    sharedStates;
    size = -1;
    contextFrequency = -1;
    numChannels = 0;
    scratchBuf = null;
    constructor() {
        this.commBuffer = new SharedArrayBuffer(NUM_SHARED_STATES * Int32Array.BYTES_PER_ELEMENT);
        this.sharedStates = new Int32Array(this.commBuffer);
        for (let i = 0; i < NUM_SHARED_STATES; i++) {
            Atomics.store(this.sharedStates, i, 0);
        }
        Atomics.store(this.sharedStates, SharedStates.BUFF_START, -1);
        Atomics.store(this.sharedStates, SharedStates.BUFF_END, -1);
    }
    init(numChannels, numSamples, contextFrequency) {
        if (this.audioBuffers !== null) {
            throw new Error("Already initialized");
        }
        this.numChannels = numChannels;
        this.audioBuffers = [];
        this.floatViews = [];
        for (let c = 0; c < numChannels; c++) {
            const sab = new SharedArrayBuffer(numSamples * Float32Array.BYTES_PER_ELEMENT);
            this.audioBuffers.push(sab);
            this.floatViews.push(new Float32Array(sab));
        }
        this.contextFrequency = contextFrequency;
        this.size = numSamples;
        Atomics.store(this.sharedStates, SharedStates.BUFF_START, 0);
        Atomics.store(this.sharedStates, SharedStates.BUFF_END, 0);
        Atomics.store(this.sharedStates, SharedStates.NUM_CHANNELS, numChannels);
        Atomics.store(this.sharedStates, SharedStates.SAMPLE_RATE, contextFrequency);
    }
    write(audioData) {
        if (!this.audioBuffers || this.size <= 0)
            return 0;
        const numFrames = audioData.numberOfFrames;
        const numCh = Math.min(audioData.numberOfChannels, this.numChannels);
        const start = Atomics.load(this.sharedStates, SharedStates.BUFF_START);
        const end = Atomics.load(this.sharedStates, SharedStates.BUFF_END);
        const freeSlots = this.size - 1 - this.getUsedSlots(start, end);
        if (freeSlots <= 0)
            return 0;
        const toWrite = Math.min(numFrames, freeSlots);
        if (!this.scratchBuf || this.scratchBuf.length < numFrames) {
            this.scratchBuf = new Float32Array(numFrames);
        }
        for (let c = 0; c < numCh; c++) {
            audioData.copyTo(this.scratchBuf, { planeIndex: c, format: "f32-planar" });
            const view = this.floatViews[c];
            const firstPart = Math.min(toWrite, this.size - end);
            view.set(this.scratchBuf.subarray(0, firstPart), end);
            if (toWrite > firstPart) {
                view.set(this.scratchBuf.subarray(firstPart, toWrite), 0);
            }
        }
        const newEnd = (end + toWrite) % this.size;
        Atomics.store(this.sharedStates, SharedStates.BUFF_END, newEnd);
        return toWrite;
    }
    getStats() {
        const start = Atomics.load(this.sharedStates, SharedStates.BUFF_START);
        const end = Atomics.load(this.sharedStates, SharedStates.BUFF_END);
        const sizeSamples = (start >= 0 && end >= 0) ? this.getUsedSlots(start, end) : 0;
        const sizeMs = this.contextFrequency > 0 ? Math.floor((sizeSamples * 1000) / this.contextFrequency) : 0;
        const totalSilenceInsertedMs = Atomics.load(this.sharedStates, SharedStates.INSERTED_SILENCE_MS);
        const isPlaying = Atomics.load(this.sharedStates, SharedStates.IS_PLAYING) === 1;
        return { queueLengthMs: sizeMs, totalSilenceInsertedMs, isPlaying };
    }
    readPTS() {
        return readPTSFromStates(this.sharedStates);
    }
    readLevels() {
        const peak = [];
        const rms = [];
        for (let c = 0; c < this.numChannels; c++) {
            const rawPeak = Atomics.load(this.sharedStates, SharedStates.PEAK_BASE + c);
            const rawRms = Atomics.load(this.sharedStates, SharedStates.RMS_BASE + c);
            peak.push(rawPeak / 1_000_000);
            rms.push(rawRms / 1_000_000);
        }
        return { peak, rms };
    }
    play() {
        Atomics.store(this.sharedStates, SharedStates.IS_PLAYING, 1);
    }
    getSharedBuffers() {
        if (!this.audioBuffers)
            throw new Error("Not initialized");
        return { audioBuffers: this.audioBuffers, commBuffer: this.commBuffer };
    }
    clear() {
        if (this.size > 0) {
            Atomics.store(this.sharedStates, SharedStates.BUFF_START, 0);
            Atomics.store(this.sharedStates, SharedStates.BUFF_END, 0);
        }
    }
    destroy() {
        this.audioBuffers = null;
        this.floatViews = [];
        this.size = -1;
        this.contextFrequency = -1;
        this.numChannels = 0;
        this.scratchBuf = null;
        Atomics.store(this.sharedStates, SharedStates.BUFF_START, -1);
        Atomics.store(this.sharedStates, SharedStates.BUFF_END, -1);
        Atomics.store(this.sharedStates, SharedStates.INSERTED_SILENCE_MS, 0);
        Atomics.store(this.sharedStates, SharedStates.IS_PLAYING, 0);
    }
    getUsedSlots(start, end) {
        if (start < 0 || end < 0)
            return 0;
        if (start === end)
            return 0;
        if (end > start)
            return end - start;
        return (this.size - start) + end;
    }
}
