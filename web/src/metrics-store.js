const HISTORY_SIZE = 60;
class RingBuffer {
    capacity;
    buf;
    head = 0;
    count = 0;
    constructor(capacity) {
        this.capacity = capacity;
        this.buf = new Array(capacity).fill(0);
    }
    push(val) {
        this.buf[this.head] = val;
        this.head = (this.head + 1) % this.capacity;
        if (this.count < this.capacity)
            this.count++;
    }
    toArray() {
        if (this.count === 0)
            return [];
        const out = new Array(this.count);
        const start = (this.head - this.count + this.capacity) % this.capacity;
        for (let i = 0; i < this.count; i++) {
            out[i] = this.buf[(start + i) % this.capacity];
        }
        return out;
    }
    last() {
        if (this.count === 0)
            return 0;
        return this.buf[(this.head - 1 + this.capacity) % this.capacity];
    }
}
/**
 * Centralized store for all player metrics: server stats, renderer stats,
 * transport diagnostics, and decode performance. Aggregates data from
 * multiple subsystems into snapshots consumed by the HUD and detail panels.
 */
export class MetricsStore {
    serverStats = null;
    rendererStats = null;
    decodeFps = 0;
    renderFps = 0;
    audioBufferMs = 0;
    silenceMs = 0;
    lastSyncOffset = 0;
    syncCorrections = 0;
    fpsRing = new RingBuffer(HISTORY_SIZE);
    bitrateRing = new RingBuffer(HISTORY_SIZE);
    syncOffsetRing = new RingBuffer(HISTORY_SIZE);
    prevOffset = 0;
    prevOffsetTime = 0;
    driftRate = 0;
    receiveKbps = 0;
    _dirty = false;
    onUpdate = null;
    get dirty() { return this._dirty; }
    clearDirty() { this._dirty = false; }
    updateServerStats(stats) {
        this.serverStats = stats;
        this._dirty = true;
        this.fpsRing.push(stats.video.frameRate);
        this.bitrateRing.push(stats.video.bitrateKbps);
        if (this.onUpdate)
            this.onUpdate();
    }
    updateRendererStats(stats) {
        this.rendererStats = stats;
        if (stats.currentAudioPTS >= 0 && stats.currentVideoPTS >= 0) {
            const offsetMs = (stats.currentVideoPTS - stats.currentAudioPTS) / 1000;
            this.lastSyncOffset = offsetMs;
            this.syncOffsetRing.push(offsetMs);
            const now = performance.now();
            if (this.prevOffsetTime > 0) {
                const dt = (now - this.prevOffsetTime) / 1000;
                if (dt > 0) {
                    this.driftRate = (offsetMs - this.prevOffset) / dt;
                }
            }
            this.prevOffset = offsetMs;
            this.prevOffsetTime = now;
        }
    }
    updateRenderFps(fps) {
        this.renderFps = fps;
    }
    updateAudioStats(bufferMs, silenceMs) {
        this.audioBufferMs = bufferMs;
        this.silenceMs = silenceMs;
    }
    getVideoMetrics() {
        const sv = this.serverStats?.video;
        return {
            codec: sv?.codec ?? "—",
            width: sv?.width ?? 0,
            height: sv?.height ?? 0,
            totalFrames: sv?.totalFrames ?? 0,
            keyFrames: sv?.keyFrames ?? 0,
            deltaFrames: sv?.deltaFrames ?? 0,
            currentGOPLen: sv?.currentGOPLen ?? 0,
            serverBitrateKbps: sv?.bitrateKbps ?? 0,
            serverFrameRate: sv?.frameRate ?? 0,
            ptsErrors: sv?.ptsErrors ?? 0,
            decodeFps: this.decodeFps,
            renderFps: this.renderFps,
            decodeQueueDepth: this.rendererStats?.videoQueueSize ?? 0,
            decodeQueueMs: this.rendererStats?.videoQueueLengthMs ?? 0,
            clientDropped: this.rendererStats?.videoTotalDiscarded ?? 0,
            fpsHistory: this.fpsRing.toArray(),
            bitrateHistory: this.bitrateRing.toArray(),
            timecode: sv?.timecode ?? "",
        };
    }
    getAudioMetrics() {
        return {
            tracks: this.serverStats?.audio ?? [],
            bufferMs: this.audioBufferMs,
            silenceMs: this.silenceMs,
        };
    }
    getSyncMetrics() {
        return {
            offsetMs: this.lastSyncOffset,
            offsetHistory: this.syncOffsetRing.toArray(),
            driftRateMsPerSec: this.driftRate,
            corrections: this.syncCorrections,
        };
    }
    getTransportMetrics() {
        return {
            protocol: this.serverStats?.protocol ?? "—",
            uptimeMs: this.serverStats?.uptimeMs ?? 0,
            viewerCount: this.serverStats?.viewerCount ?? 0,
            serverBytesSent: 0,
            receiveBitrateKbps: this.receiveKbps,
        };
    }
    getCaptionMetrics() {
        return {
            activeChannels: this.serverStats?.captions?.activeChannels ?? [],
            totalFrames: this.serverStats?.captions?.totalFrames ?? 0,
        };
    }
    getStreamInfo() {
        const sv = this.serverStats?.video;
        const a = this.serverStats?.audio ?? [];
        return {
            videoCodec: sv?.codec ?? "",
            resolution: sv && sv.width > 0 ? `${sv.height}p` : "",
            frameRate: sv && sv.frameRate > 0 ? `${Math.round(sv.frameRate)}fps` : "",
            audioCodec: a.length > 0 ? a[0].codec : "",
            audioConfig: a.length > 0
                ? `${(a[0].sampleRate / 1000).toFixed(0)}kHz ${a[0].channels}ch`
                : "",
        };
    }
    getHUDState() {
        const v = this.getVideoMetrics();
        const a = this.getAudioMetrics();
        const s = this.getSyncMetrics();
        const videoStatus = this.assessVideoHealth(v);
        const audioStatus = this.assessAudioHealth(a);
        const syncStatus = this.assessSyncHealth(s);
        const fpsLabel = v.serverFrameRate > 0 ? `${Math.round(v.serverFrameRate)}fps` : "\u2014";
        let audioLabel;
        if (audioStatus === "critical")
            audioLabel = "underrun";
        else if (audioStatus === "warn")
            audioLabel = "low buf";
        else
            audioLabel = "ok";
        const syncSign = s.offsetMs >= 0 ? "+" : "\u2212";
        const syncLabel = `${syncSign}${Math.abs(s.offsetMs).toFixed(0)}ms`;
        return {
            video: { label: fpsLabel, status: videoStatus },
            audio: { label: audioLabel, status: audioStatus },
            sync: { label: syncLabel, status: syncStatus },
        };
    }
    assessVideoHealth(v) {
        if (v.serverFrameRate > 0 && v.decodeFps > 0) {
            const ratio = v.decodeFps / v.serverFrameRate;
            if (ratio < 0.5)
                return "critical";
            if (ratio < 0.8)
                return "warn";
        }
        if (v.ptsErrors > 0)
            return "warn";
        return "good";
    }
    assessAudioHealth(a) {
        if (a.bufferMs < 20)
            return "critical";
        if (a.bufferMs < 50)
            return "warn";
        if (a.silenceMs > 500)
            return "warn";
        return "good";
    }
    assessSyncHealth(s) {
        const abs = Math.abs(s.offsetMs);
        if (abs > 200)
            return "critical";
        if (abs > 50)
            return "warn";
        return "good";
    }
    getTimecode() {
        return this.serverStats?.video?.timecode ?? "";
    }
    getSCTE35Events() {
        return this.serverStats?.scte35?.recent ?? [];
    }
    getSCTE35Total() {
        return this.serverStats?.scte35?.totalEvents ?? 0;
    }
    reset() {
        this.serverStats = null;
        this.rendererStats = null;
        this.decodeFps = 0;
        this.renderFps = 0;
        this.audioBufferMs = 0;
        this.silenceMs = 0;
        this.lastSyncOffset = 0;
        this.syncCorrections = 0;
        this.fpsRing = new RingBuffer(HISTORY_SIZE);
        this.bitrateRing = new RingBuffer(HISTORY_SIZE);
        this.syncOffsetRing = new RingBuffer(HISTORY_SIZE);
        this.prevOffset = 0;
        this.prevOffsetTime = 0;
        this.driftRate = 0;
        this.receiveKbps = 0;
    }
}
