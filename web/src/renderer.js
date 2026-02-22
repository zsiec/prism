/**
 * Drives the video render loop using requestAnimationFrame. Pulls decoded
 * VideoFrames from a VideoRenderBuffer and draws them to a canvas, paced
 * either by an audio clock (for A/V sync) or by a wall-clock free-run
 * mode when audio is unavailable. Collects timing diagnostics for the
 * perf overlay.
 */
export class PrismRenderer {
    canvas;
    ctx;
    animationId = null;
    videoBuffer;
    audioClock;
    currentVideoPTS = -1;
    currentAudioPTS = -1;
    lastDrawnFrame = null;
    onStats = null;
    freeRunStart = -1;
    freeRunBasePTS = -1;
    _freeRunOnly = false;
    _maxResolution = 0;
    _externallyDriven = false;
    lastStatsTime = 0;
    lastAudioAdvanceTime = 0;
    audioStallFreeRunStart = -1;
    audioStallFreeRunBasePTS = -1;
    // --- diagnostics ---
    _diagRafCount = 0;
    _diagFramesDrawn = 0;
    _diagFramesSkipped = 0;
    _diagLastRafTime = 0;
    _diagRafIntervalSum = 0;
    _diagRafIntervalMax = 0;
    _diagRafIntervalMin = Infinity;
    _diagDrawTimeSum = 0;
    _diagDrawTimeMax = 0;
    _diagLastFrameDrawTime = 0;
    _diagFrameIntervalSum = 0;
    _diagFrameIntervalMax = 0;
    _diagFrameIntervalMin = Infinity;
    _diagAvSyncSum = 0;
    _diagAvSyncCount = 0;
    _diagAvSyncMin = Infinity;
    _diagAvSyncMax = -Infinity;
    _diagLastAvSync = 0;
    _diagEmptyBufferHits = 0;
    constructor(canvas, videoBuffer, audioClock, onStats) {
        this.canvas = canvas;
        this.ctx = canvas.getContext("2d");
        this.videoBuffer = videoBuffer;
        this.audioClock = audioClock;
        this.onStats = onStats ?? null;
    }
    set freeRunOnly(v) {
        this._freeRunOnly = v;
    }
    set maxResolution(v) {
        this._maxResolution = v;
    }
    set externallyDriven(v) {
        this._externallyDriven = v;
    }
    getVideoBuffer() {
        return this.videoBuffer;
    }
    start() {
        if (this._externallyDriven)
            return;
        if (this.animationId !== null)
            return;
        this.renderLoop();
    }
    renderOnce() {
        const now = performance.now();
        this.renderTick(now);
    }
    renderLoop = () => {
        this.animationId = requestAnimationFrame(this.renderLoop);
        this.renderTick(performance.now());
    };
    renderTick(now) {
        this._diagRafCount++;
        if (this._diagLastRafTime > 0) {
            const interval = now - this._diagLastRafTime;
            this._diagRafIntervalSum += interval;
            if (interval > this._diagRafIntervalMax)
                this._diagRafIntervalMax = interval;
            if (interval < this._diagRafIntervalMin)
                this._diagRafIntervalMin = interval;
        }
        this._diagLastRafTime = now;
        let targetPTS;
        const audioPTS = this._freeRunOnly ? -1 : this.audioClock.getPlaybackPTS();
        const AUDIO_STALE_MS = 200;
        if (audioPTS >= 0) {
            if (this.currentAudioPTS >= 0 && this.currentVideoPTS >= 0 &&
                this.currentAudioPTS - audioPTS > 30_000_000) {
                this.videoBuffer.clear();
                this.currentVideoPTS = -1;
            }
            const audioAdvanced = this.currentAudioPTS < 0 || audioPTS !== this.currentAudioPTS;
            if (audioAdvanced) {
                this.lastAudioAdvanceTime = now;
                this.audioStallFreeRunStart = -1;
                this.audioStallFreeRunBasePTS = -1;
            }
            this.currentAudioPTS = audioPTS;
            this.freeRunStart = -1;
            this.freeRunBasePTS = -1;
            const audioStale = this.lastAudioAdvanceTime > 0 &&
                (now - this.lastAudioAdvanceTime) > AUDIO_STALE_MS;
            if (audioStale && this.videoBuffer.getStats().queueSize > 0) {
                // Audio clock has stalled — pace video using wall clock
                // anchored from when the stall was first detected.
                if (this.audioStallFreeRunStart < 0) {
                    this.audioStallFreeRunStart = now;
                    this.audioStallFreeRunBasePTS = this.currentVideoPTS >= 0
                        ? this.currentVideoPTS
                        : (this.videoBuffer.peekFirstFrame()?.timestamp ?? -1);
                }
                if (this.audioStallFreeRunBasePTS >= 0) {
                    targetPTS = this.audioStallFreeRunBasePTS +
                        (now - this.audioStallFreeRunStart) * 1000;
                }
                else {
                    targetPTS = -1;
                }
            }
            else {
                const avDelta = this.currentVideoPTS >= 0
                    ? Math.abs(audioPTS - this.currentVideoPTS)
                    : 0;
                if (avDelta > 30_000_000) {
                    targetPTS = -1;
                }
                else if (this.currentVideoPTS >= 0 && audioPTS - this.currentVideoPTS > 150_000) {
                    targetPTS = -1;
                }
                else {
                    targetPTS = audioPTS;
                }
            }
        }
        else {
            const firstFrame = this.videoBuffer.peekFirstFrame();
            if (!firstFrame) {
                this._diagEmptyBufferHits++;
                this.reportStats(now);
                return;
            }
            if (this.freeRunStart < 0) {
                this.freeRunStart = now;
                const stats = this.videoBuffer.getStats();
                if (stats.queueSize > 9) {
                    const skip = this.videoBuffer.getFrameByTimestamp(Infinity);
                    if (skip.frame) {
                        if (this.lastDrawnFrame)
                            this.lastDrawnFrame.close();
                        this.lastDrawnFrame = skip.frame;
                        this.currentVideoPTS = skip.frame.timestamp;
                        this.freeRunBasePTS = skip.frame.timestamp;
                        this.drawFrame(skip.frame);
                        this.reportStats(now);
                        return;
                    }
                }
                this.freeRunBasePTS = firstFrame.timestamp;
            }
            targetPTS = this.freeRunBasePTS + (now - this.freeRunStart) * 1000;
        }
        let frame = null;
        if (targetPTS < 0) {
            frame = this.videoBuffer.takeNextFrame();
        }
        else {
            const result = this.videoBuffer.getFrameByTimestamp(targetPTS);
            frame = result.frame;
        }
        if (frame) {
            if (this.lastDrawnFrame) {
                this.lastDrawnFrame.close();
            }
            this.lastDrawnFrame = frame;
            const t0 = performance.now();
            this.drawFrame(frame);
            const drawMs = performance.now() - t0;
            this._diagDrawTimeSum += drawMs;
            if (drawMs > this._diagDrawTimeMax)
                this._diagDrawTimeMax = drawMs;
            this._diagFramesDrawn++;
            if (this._diagLastFrameDrawTime > 0) {
                const fInterval = now - this._diagLastFrameDrawTime;
                this._diagFrameIntervalSum += fInterval;
                if (fInterval > this._diagFrameIntervalMax)
                    this._diagFrameIntervalMax = fInterval;
                if (fInterval < this._diagFrameIntervalMin)
                    this._diagFrameIntervalMin = fInterval;
            }
            this._diagLastFrameDrawTime = now;
            this.currentVideoPTS = frame.timestamp;
            if (this.currentAudioPTS >= 0 && this.currentVideoPTS >= 0) {
                const delta = Math.abs(this.currentVideoPTS - this.currentAudioPTS);
                if (delta < 30_000_000) {
                    const syncMs = (this.currentVideoPTS - this.currentAudioPTS) / 1000;
                    this._diagLastAvSync = syncMs;
                    this._diagAvSyncSum += syncMs;
                    this._diagAvSyncCount++;
                    if (syncMs < this._diagAvSyncMin)
                        this._diagAvSyncMin = syncMs;
                    if (syncMs > this._diagAvSyncMax)
                        this._diagAvSyncMax = syncMs;
                }
            }
        }
        else {
            this._diagFramesSkipped++;
        }
        this.reportStats(now);
    }
    cachedCanvasW = 0;
    cachedCanvasH = 0;
    drawFrame(frame) {
        let targetW = frame.displayWidth;
        let targetH = frame.displayHeight;
        if (this._maxResolution > 0) {
            const scale = Math.min(1, this._maxResolution / Math.max(targetW, targetH));
            targetW = Math.round(targetW * scale);
            targetH = Math.round(targetH * scale);
        }
        if (this.cachedCanvasW !== targetW || this.cachedCanvasH !== targetH) {
            this.canvas.width = targetW;
            this.canvas.height = targetH;
            this.cachedCanvasW = targetW;
            this.cachedCanvasH = targetH;
        }
        this.ctx.drawImage(frame, 0, 0, targetW, targetH);
    }
    reportStats(now) {
        if (!this.onStats)
            return;
        if (this._externallyDriven && now - this.lastStatsTime < 250)
            return;
        this.lastStatsTime = now;
        const vStats = this.videoBuffer.getStats();
        this.onStats({
            currentVideoPTS: this.currentVideoPTS,
            currentAudioPTS: this.currentAudioPTS,
            videoQueueSize: vStats.queueSize,
            videoQueueLengthMs: vStats.queueLengthMs,
            videoTotalDiscarded: vStats.totalDiscarded,
        });
    }
    getDiagnostics() {
        const vStats = this.videoBuffer.getStats();
        return {
            rafCount: this._diagRafCount,
            framesDrawn: this._diagFramesDrawn,
            framesSkipped: this._diagFramesSkipped,
            avgRafIntervalMs: this._diagRafCount > 1 ? this._diagRafIntervalSum / (this._diagRafCount - 1) : 0,
            minRafIntervalMs: this._diagRafIntervalMin === Infinity ? 0 : this._diagRafIntervalMin,
            maxRafIntervalMs: this._diagRafIntervalMax,
            avgDrawMs: this._diagFramesDrawn > 0 ? this._diagDrawTimeSum / this._diagFramesDrawn : 0,
            maxDrawMs: this._diagDrawTimeMax,
            avgFrameIntervalMs: this._diagFramesDrawn > 1 ? this._diagFrameIntervalSum / (this._diagFramesDrawn - 1) : 0,
            minFrameIntervalMs: this._diagFrameIntervalMin === Infinity ? 0 : this._diagFrameIntervalMin,
            maxFrameIntervalMs: this._diagFrameIntervalMax,
            avSyncMs: this._diagLastAvSync,
            avSyncMin: this._diagAvSyncMin === Infinity ? 0 : this._diagAvSyncMin,
            avSyncMax: this._diagAvSyncMax === -Infinity ? 0 : this._diagAvSyncMax,
            avSyncAvg: this._diagAvSyncCount > 0 ? this._diagAvSyncSum / this._diagAvSyncCount : 0,
            clockMode: (this.freeRunStart >= 0 || this._freeRunOnly) ? "freerun"
                : this.audioStallFreeRunStart >= 0 ? "audio-stall-freerun"
                    : "audio",
            emptyBufferHits: this._diagEmptyBufferHits,
            currentVideoPTS: this.currentVideoPTS,
            currentAudioPTS: this.currentAudioPTS,
            videoQueueSize: vStats.queueSize,
            videoQueueMs: vStats.queueLengthMs,
            videoTotalDiscarded: vStats.totalDiscarded,
        };
    }
    destroy() {
        if (this.animationId !== null) {
            cancelAnimationFrame(this.animationId);
            this.animationId = null;
        }
        if (this.lastDrawnFrame) {
            this.lastDrawnFrame.close();
            this.lastDrawnFrame = null;
        }
    }
}
