// Cap the decode queue to prevent unbounded backlog. 16 frames ≈ 500ms at 30fps,
// enough headroom for MoQ's bursty per-subgroup delivery while still capping
// runaway queuing at high bitrates. (Was 60, which allowed 2+ seconds of stale frames.)
const MAX_QUEUED_CHUNKS = 16;
let videoDecoder = null;
let waitForKeyframe = true;
let discardedDelta = 0;
let discardedBufferFull = 0;
let diagFrameCount = 0;
let diagDecodeCount = 0;
let diagKeyframeCount = 0;
let diagDecodeErrors = 0;
let diagTotalDiscardedDelta = 0;
let diagTotalDiscardedFull = 0;
let diagLastOutputTime = 0;
let diagOutputIntervalSum = 0;
let diagOutputIntervalMax = 0;
let diagOutputIntervalMin = Infinity;
let diagLastInputTime = 0;
let diagInputIntervalSum = 0;
let diagInputIntervalMax = 0;
let diagInputIntervalMin = Infinity;
let diagFirstInputTime = 0;
let diagLastPTS = -1;
let diagPtsJumps = 0;
let lastConfigCodec = "";
let lastConfigWidth = 0;
let lastConfigHeight = 0;
let lastDescription = null;
function createDecoder() {
    const decoder = new VideoDecoder({
        output: (frame) => processVideoFrame(frame),
        error: (err) => {
            diagDecodeErrors++;
            waitForKeyframe = true;
            self.postMessage({ type: "error", message: err.message });
            recoverDecoder();
        },
    });
    const config = {
        codec: lastConfigCodec,
        codedWidth: lastConfigWidth,
        codedHeight: lastConfigHeight,
        optimizeForLatency: true,
    };
    if (lastDescription) {
        config.description = lastDescription;
    }
    decoder.configure(config);
    return decoder;
}
let recovering = false;
function recoverDecoder() {
    if (!lastConfigCodec || recovering)
        return;
    recovering = true;
    try {
        // Prefer reset() over close()+new — it's faster because it reuses the
        // underlying hardware decoder context instead of tearing it down.
        if (videoDecoder && videoDecoder.state !== "closed") {
            videoDecoder.reset();
            const config = {
                codec: lastConfigCodec,
                codedWidth: lastConfigWidth,
                codedHeight: lastConfigHeight,
                hardwareAcceleration: "prefer-software",
                optimizeForLatency: true,
            };
            if (lastDescription)
                config.description = lastDescription;
            videoDecoder.configure(config);
        }
        else {
            videoDecoder = createDecoder();
        }
    }
    catch {
        // reset() failed — fall back to full recreation
        try {
            if (videoDecoder)
                videoDecoder.close();
        }
        catch { /* */ }
        videoDecoder = createDecoder();
    }
    recovering = false;
}
function processVideoFrame(frame) {
    diagFrameCount++;
    const now = performance.now();
    if (diagLastOutputTime > 0) {
        const interval = now - diagLastOutputTime;
        diagOutputIntervalSum += interval;
        if (interval > diagOutputIntervalMax)
            diagOutputIntervalMax = interval;
        if (interval < diagOutputIntervalMin)
            diagOutputIntervalMin = interval;
    }
    diagLastOutputTime = now;
    self.postMessage({ type: "frame", frame }, { transfer: [frame] });
}
self.onmessage = async (e) => {
    const msg = e.data;
    if (msg.type === "configure") {
        if (videoDecoder) {
            await videoDecoder.flush();
            videoDecoder.close();
        }
        lastConfigCodec = msg.codec;
        lastConfigWidth = msg.width;
        lastConfigHeight = msg.height;
        lastDescription = msg.description ?? null;
        videoDecoder = createDecoder();
        waitForKeyframe = true;
        discardedDelta = 0;
        discardedBufferFull = 0;
        diagFrameCount = 0;
        diagDecodeCount = 0;
        diagKeyframeCount = 0;
        diagDecodeErrors = 0;
        diagTotalDiscardedDelta = 0;
        diagTotalDiscardedFull = 0;
        diagLastOutputTime = 0;
        diagOutputIntervalSum = 0;
        diagOutputIntervalMax = 0;
        diagOutputIntervalMin = Infinity;
        diagLastInputTime = 0;
        diagInputIntervalSum = 0;
        diagInputIntervalMax = 0;
        diagInputIntervalMin = Infinity;
        diagFirstInputTime = 0;
        diagLastPTS = -1;
        diagPtsJumps = 0;
        self.postMessage({ type: "configured" });
    }
    else if (msg.type === "decode") {
        if (!videoDecoder || videoDecoder.state !== "configured")
            return;
        const inputNow = performance.now();
        if (diagFirstInputTime === 0)
            diagFirstInputTime = inputNow;
        diagDecodeCount++;
        if (diagLastInputTime > 0) {
            const interval = inputNow - diagLastInputTime;
            diagInputIntervalSum += interval;
            if (interval > diagInputIntervalMax)
                diagInputIntervalMax = interval;
            if (interval < diagInputIntervalMin)
                diagInputIntervalMin = interval;
        }
        diagLastInputTime = inputNow;
        if (videoDecoder.decodeQueueSize >= MAX_QUEUED_CHUNKS) {
            discardedBufferFull++;
            diagTotalDiscardedFull++;
            // Subsequent deltas would reference this dropped frame and cause
            // decode errors, so skip ahead to the next keyframe immediately
            // rather than triggering an expensive error → recovery cycle.
            waitForKeyframe = true;
            return;
        }
        if (discardedBufferFull > 0) {
            self.postMessage({ type: "warning", message: `Discarded ${discardedBufferFull} video chunks (buffer full)` });
            discardedBufferFull = 0;
        }
        if (msg.isDisco) {
            waitForKeyframe = true;
        }
        const isKeyframe = msg.isKeyframe;
        if (waitForKeyframe && !isKeyframe) {
            discardedDelta++;
            diagTotalDiscardedDelta++;
            return;
        }
        if (discardedDelta > 0) {
            self.postMessage({ type: "warning", message: `Discarded ${discardedDelta} delta frames before key` });
            discardedDelta = 0;
        }
        waitForKeyframe = false;
        if (isKeyframe)
            diagKeyframeCount++;
        const ts = msg.timestamp;
        if (diagLastPTS >= 0) {
            const gap = Math.abs(ts - diagLastPTS);
            // 500ms threshold: high enough to ignore B-frame reordering
            // (~66ms gaps at 30fps) but catches real discontinuities.
            // Tracked for diagnostics only — keyframes reset decoder state
            // naturally, so no flush or waitForKeyframe needed here.
            if (gap > 500_000)
                diagPtsJumps++;
        }
        diagLastPTS = ts;
        try {
            videoDecoder.decode(new EncodedVideoChunk({
                type: isKeyframe ? "key" : "delta",
                timestamp: ts,
                data: msg.data,
            }));
        }
        catch {
            // A sync throw often precedes an async error if we continue feeding
            // frames from the same damaged sequence. Skip to the next keyframe
            // to avoid cascading into a more expensive async recovery.
            waitForKeyframe = true;
            diagDecodeErrors++;
        }
    }
    else if (msg.type === "getDiagnostics") {
        const avgInputInterval = diagDecodeCount > 1
            ? diagInputIntervalSum / (diagDecodeCount - 1) : 0;
        const avgOutputInterval = diagFrameCount > 1
            ? diagOutputIntervalSum / (diagFrameCount - 1) : 0;
        self.postMessage({
            type: "diagnostics",
            data: {
                inputCount: diagDecodeCount,
                outputCount: diagFrameCount,
                keyframeCount: diagKeyframeCount,
                decodeErrors: diagDecodeErrors,
                discardedDelta: diagTotalDiscardedDelta,
                discardedBufferFull: diagTotalDiscardedFull,
                decodeQueueSize: videoDecoder?.decodeQueueSize ?? 0,
                avgInputIntervalMs: avgInputInterval,
                minInputIntervalMs: diagInputIntervalMin === Infinity ? 0 : diagInputIntervalMin,
                maxInputIntervalMs: diagInputIntervalMax,
                avgOutputIntervalMs: avgOutputInterval,
                minOutputIntervalMs: diagOutputIntervalMin === Infinity ? 0 : diagOutputIntervalMin,
                maxOutputIntervalMs: diagOutputIntervalMax,
                inputFps: diagFirstInputTime > 0
                    ? diagDecodeCount / ((performance.now() - diagFirstInputTime) / 1000) : 0,
                outputFps: diagLastOutputTime > 0 && diagFrameCount > 1
                    ? (diagFrameCount - 1) / (diagOutputIntervalSum / 1000) : 0,
                ptsJumps: diagPtsJumps,
            },
        });
    }
    else if (msg.type === "stop") {
        if (videoDecoder) {
            try {
                await videoDecoder.flush();
                videoDecoder.close();
            }
            catch {
                // ignore
            }
            videoDecoder = null;
        }
        waitForKeyframe = true;
        discardedDelta = 0;
        discardedBufferFull = 0;
    }
};
export {};
