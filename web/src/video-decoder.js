/**
 * Manages a Web Worker that runs the WebCodecs VideoDecoder. Compressed
 * frames are posted to the worker, and decoded VideoFrames are transferred
 * back and inserted into a VideoRenderBuffer for the renderer to consume.
 * The worker isolation prevents decode stalls from blocking the main thread.
 */
export class PrismVideoDecoder {
    worker = null;
    renderBuffer;
    onFrameReceived;
    configured = false;
    _lastDiag = null;
    _diagResolve = null;
    _bufferDropped = 0;
    constructor(renderBuffer, onFrameReceived) {
        this.renderBuffer = renderBuffer;
        this.onFrameReceived = onFrameReceived ?? null;
    }
    preload() {
        if (this.worker)
            return;
        this.worker = new Worker(new URL("./video-decoder-worker.ts", import.meta.url), { type: "module" });
        this.worker.onmessage = (e) => this.handleWorkerMessage(e);
    }
    configure(codec, width, height, description) {
        if (this.configured) {
            // Already configured — reconfigure the existing worker
            if (this.worker) {
                this.worker.postMessage({ type: "stop" });
                this.worker.terminate();
                this.worker = null;
            }
            this.renderBuffer.clear();
            this.configured = false;
        }
        if (!this.worker) {
            this.preload();
        }
        this.worker.postMessage({
            type: "configure",
            codec,
            width,
            height,
            description: description ?? null,
        }, description ? [description] : []);
        this.configured = true;
    }
    decode(data, isKeyframe, timestamp, isDisco) {
        if (!this.worker || !this.configured)
            return;
        this.worker.postMessage({
            type: "decode",
            data: data.buffer,
            isKeyframe,
            timestamp,
            isDisco,
        }, [data.buffer]);
    }
    reset() {
        if (this.worker) {
            this.worker.postMessage({ type: "stop" });
            this.worker.terminate();
            this.worker = null;
        }
        this.renderBuffer.clear();
        this.configured = false;
    }
    async getDiagnostics() {
        if (!this.worker) {
            return this.emptyDiag();
        }
        return new Promise((resolve) => {
            this._diagResolve = resolve;
            this.worker.postMessage({ type: "getDiagnostics" });
            setTimeout(() => {
                if (this._diagResolve) {
                    this._diagResolve(this._lastDiag ?? this.emptyDiag());
                    this._diagResolve = null;
                }
            }, 200);
        });
    }
    emptyDiag() {
        return {
            inputCount: 0, outputCount: 0, keyframeCount: 0, decodeErrors: 0,
            discardedDelta: 0, discardedBufferFull: 0, decodeQueueSize: 0,
            avgInputIntervalMs: 0, minInputIntervalMs: 0, maxInputIntervalMs: 0,
            avgOutputIntervalMs: 0, minOutputIntervalMs: 0, maxOutputIntervalMs: 0,
            inputFps: 0, outputFps: 0, ptsJumps: 0, bufferDropped: 0,
        };
    }
    handleWorkerMessage(e) {
        const msg = e.data;
        if (msg.type === "frame") {
            const frame = msg.frame;
            this.renderBuffer.addFrame(frame);
            if (this.onFrameReceived) {
                this.onFrameReceived();
            }
        }
        else if (msg.type === "diagnostics") {
            const d = { ...msg.data, bufferDropped: this._bufferDropped };
            this._lastDiag = d;
            if (this._diagResolve) {
                this._diagResolve(d);
                this._diagResolve = null;
            }
        }
        else if (msg.type === "error") {
            console.error("[VideoDecoder] worker error:", msg.message);
        }
        else if (msg.type === "warning") {
            console.warn("[VideoDecoder]", msg.message);
        }
    }
}
