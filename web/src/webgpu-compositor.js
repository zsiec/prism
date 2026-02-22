// Tile rect uniform: vec4f(ndcLeft, ndcTop, ndcWidth, ndcHeight)
// NDC: x=-1..+1 left-to-right, y=-1..+1 bottom-to-top
// UV: (0,0) = top-left of texture, (1,1) = bottom-right
const SHADER_SOURCE = /* wgsl */ `
struct TileRect {
	rect: vec4f,
};

struct VertexOutput {
	@builtin(position) position: vec4f,
	@location(0) uv: vec2f,
};

@group(0) @binding(0) var samp: sampler;
@group(0) @binding(1) var tex: texture_external;
@group(0) @binding(2) var<uniform> tile: TileRect;

@vertex fn vs(@builtin(vertex_index) vi: u32) -> VertexOutput {
	// Unit quad: 2 triangles covering [0,1]^2
	var pos = array<vec2f, 6>(
		vec2f(0.0, 0.0),
		vec2f(1.0, 0.0),
		vec2f(0.0, 1.0),
		vec2f(0.0, 1.0),
		vec2f(1.0, 0.0),
		vec2f(1.0, 1.0),
	);

	var out: VertexOutput;
	let xy = pos[vi];
	let ndcX = tile.rect.x + xy.x * tile.rect.z;
	let ndcY = tile.rect.y - xy.y * tile.rect.w;
	out.position = vec4f(ndcX, ndcY, 0.0, 1.0);
	out.uv = xy;
	return out;
}

@fragment fn fs(in: VertexOutput) -> @location(0) vec4f {
	return textureSampleBaseClampToEdge(tex, samp, in.uv);
}
`;
// Adaptive clock recovery: buffer-level proportional control.
// Adjusts playback rate to keep each tile's queue near TARGET_QUEUE,
// absorbing transport jitter without visible speed artifacts.
const ADAPTIVE_TARGET_QUEUE = 3; // target buffer depth (frames)
const ADAPTIVE_GAIN = 0.002; // rate adjustment per frame of error
const ADAPTIVE_MAX_CORRECTION = 0.02; // hard clamp: +/- 2% speed
const ADAPTIVE_DEAD_ZONE = 1; // no correction within +/- 1 frame of target
const PTS_DISCONTINUITY_US = 1_000_000; // 1 second backward jump = reset clock
export class WebGPUCompositor {
    canvas;
    device = null;
    context = null;
    pipeline = null;
    sampler = null;
    bindGroupLayout = null;
    tiles = [];
    _cols = 3;
    _rows = 3;
    _gap = 4;
    _ready = false;
    _frameTimeMs = 0;
    _pickTimeMs = 0;
    _importTimeMs = 0;
    _drawTimeMs = 0;
    _presentTimeMs = 0;
    _tilesDrawn = 0;
    _renderFpsCounter = 0;
    _rafFpsCounter = 0;
    _fps = 0;
    _rafFps = 0;
    _fpsTime = 0;
    _framesRendered = 0;
    _skipped = 0;
    _lastSyncTime = 0;
    constructor(canvas) {
        this.canvas = canvas;
    }
    get ready() {
        return this._ready;
    }
    async init() {
        if (!navigator.gpu)
            return false;
        let adapter;
        try {
            adapter = await navigator.gpu.requestAdapter();
        }
        catch {
            return false;
        }
        if (!adapter)
            return false;
        try {
            this.device = await adapter.requestDevice();
        }
        catch {
            return false;
        }
        this.context = this.canvas.getContext("webgpu");
        if (!this.context) {
            this.device.destroy();
            this.device = null;
            return false;
        }
        const format = navigator.gpu.getPreferredCanvasFormat();
        this.context.configure({
            device: this.device,
            format,
            alphaMode: "opaque",
        });
        this.sampler = this.device.createSampler({
            magFilter: "linear",
            minFilter: "linear",
        });
        this.bindGroupLayout = this.device.createBindGroupLayout({
            entries: [
                { binding: 0, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
                { binding: 1, visibility: GPUShaderStage.FRAGMENT, externalTexture: {} },
                { binding: 2, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
            ],
        });
        const pipelineLayout = this.device.createPipelineLayout({
            bindGroupLayouts: [this.bindGroupLayout],
        });
        const shaderModule = this.device.createShaderModule({ code: SHADER_SOURCE });
        this.pipeline = this.device.createRenderPipeline({
            layout: pipelineLayout,
            vertex: { module: shaderModule, entryPoint: "vs" },
            fragment: {
                module: shaderModule,
                entryPoint: "fs",
                targets: [{ format }],
            },
            primitive: { topology: "triangle-list" },
        });
        this._ready = true;
        return true;
    }
    setGrid(cols, rows, gap) {
        this._cols = cols;
        this._rows = rows;
        this._gap = gap;
    }
    setTileBuffers(buffers) {
        if (!this.device)
            return;
        for (const slot of this.tiles) {
            if (slot.lastFrame) {
                slot.lastFrame.close();
                slot.lastFrame = null;
            }
            slot.uniformBuffer.destroy();
        }
        this.tiles = buffers.map((buf) => ({
            buffer: buf,
            uniformBuffer: this.device.createBuffer({
                size: 16,
                usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
            }),
            lastFrame: null,
            freeRunStart: -1,
            freeRunBasePTS: -1,
            lastAspect: 0,
            clockRate: 1.0,
            initialFillDone: false,
            framesDrawn: 0,
            framesDiscarded: 0,
            lastPTS: -1,
            lastFrameTime: 0,
            frameIntervalSum: 0,
            frameIntervalMin: Infinity,
            frameIntervalMax: 0,
        }));
        this.updateTransforms();
    }
    updateTransforms() {
        if (!this.device)
            return;
        for (let i = 0; i < this.tiles.length; i++) {
            this.updateTileTransform(i);
        }
    }
    updateTileTransform(i) {
        if (!this.device)
            return;
        const W = this.canvas.width;
        const H = this.canvas.height;
        if (W === 0 || H === 0)
            return;
        const dpr = Math.min(window.devicePixelRatio || 1, 1.5);
        const gapPx = this._gap * dpr;
        const cellW = (W - gapPx * (this._cols + 1)) / this._cols;
        const cellH = (H - gapPx * (this._rows + 1)) / this._rows;
        const col = i % this._cols;
        const row = Math.floor(i / this._cols);
        const cellX = gapPx + col * (cellW + gapPx);
        const cellY = gapPx + row * (cellH + gapPx);
        let tileW = cellW;
        let tileH = cellH;
        let offsetX = 0;
        let offsetY = 0;
        const videoAspect = this.tiles[i].lastAspect;
        if (videoAspect > 0) {
            const cellAspect = cellW / cellH;
            if (videoAspect > cellAspect) {
                tileH = cellW / videoAspect;
                offsetY = (cellH - tileH) / 2;
            }
            else {
                tileW = cellH * videoAspect;
                offsetX = (cellW - tileW) / 2;
            }
        }
        const px = cellX + offsetX;
        const py = cellY + offsetY;
        const ndcLeft = (px / W) * 2 - 1;
        const ndcTop = 1 - (py / H) * 2;
        const ndcW = (tileW / W) * 2;
        const ndcH = (tileH / H) * 2;
        const rect = new Float32Array([ndcLeft, ndcTop, ndcW, ndcH]);
        this.device.queue.writeBuffer(this.tiles[i].uniformBuffer, 0, rect);
    }
    syncSize(now) {
        const t = now ?? performance.now();
        if (t - this._lastSyncTime < 500)
            return;
        this._lastSyncTime = t;
        const dpr = Math.min(window.devicePixelRatio || 1, 1.5);
        const rect = this.canvas.getBoundingClientRect();
        const w = Math.round(rect.width * dpr);
        const h = Math.round(rect.height * dpr);
        if (this.canvas.width !== w || this.canvas.height !== h) {
            this.canvas.width = w;
            this.canvas.height = h;
            this.updateTransforms();
        }
    }
    getPerfStats() {
        let totalQueue = 0;
        let totalDiscarded = 0;
        for (const slot of this.tiles) {
            const s = slot.buffer.getStats();
            totalQueue += s.queueSize;
            totalDiscarded += s.totalDiscarded;
        }
        return {
            frameTimeMs: this._frameTimeMs,
            pickTimeMs: this._pickTimeMs,
            importTimeMs: this._importTimeMs,
            drawTimeMs: this._drawTimeMs,
            presentTimeMs: this._presentTimeMs,
            framesRendered: this._framesRendered,
            fps: this._fps,
            rafFps: this._rafFps,
            tilesDrawn: this._tilesDrawn,
            skipped: this._skipped,
            tilesTotalQueue: totalQueue,
            tilesTotalDiscarded: totalDiscarded,
            canvasWidth: this.canvas.width,
            canvasHeight: this.canvas.height,
        };
    }
    renderFrame() {
        if (!this._ready || !this.device || !this.context || !this.pipeline || !this.sampler || !this.bindGroupLayout)
            return;
        const t0 = performance.now();
        this.syncSize(t0);
        // Phase 1: Pick frames -- skip full render if nothing changed
        const tPickStart = performance.now();
        let anyNew = false;
        for (let i = 0; i < this.tiles.length; i++) {
            const slot = this.tiles[i];
            const hadFrame = slot.lastFrame;
            this.pickFrame(slot, i, tPickStart);
            if (slot.lastFrame !== hadFrame)
                anyNew = true;
        }
        const tPick = performance.now();
        this._pickTimeMs = tPick - tPickStart;
        if (!anyNew && this._framesRendered > 0) {
            this._skipped++;
        }
        // Phase 2: Import textures and create bind groups
        const bindGroups = new Array(this.tiles.length).fill(null);
        for (let i = 0; i < this.tiles.length; i++) {
            const slot = this.tiles[i];
            if (!slot.lastFrame)
                continue;
            let externalTexture;
            try {
                externalTexture = this.device.importExternalTexture({ source: slot.lastFrame });
            }
            catch {
                continue;
            }
            bindGroups[i] = this.device.createBindGroup({
                layout: this.bindGroupLayout,
                entries: [
                    { binding: 0, resource: this.sampler },
                    { binding: 1, resource: externalTexture },
                    { binding: 2, resource: { buffer: slot.uniformBuffer } },
                ],
            });
        }
        const tImport = performance.now();
        this._importTimeMs = tImport - tPick;
        // Phase 3: Acquire swap chain texture (can block on vsync back-pressure)
        let texture;
        try {
            texture = this.context.getCurrentTexture();
        }
        catch {
            return;
        }
        const tPresent = performance.now();
        this._presentTimeMs = tPresent - tImport;
        // Phase 4: Encode and submit
        const encoder = this.device.createCommandEncoder();
        const pass = encoder.beginRenderPass({
            colorAttachments: [{
                    view: texture.createView(),
                    clearValue: { r: 0.067, g: 0.067, b: 0.067, a: 1 },
                    loadOp: "clear",
                    storeOp: "store",
                }],
        });
        pass.setPipeline(this.pipeline);
        for (let i = 0; i < this.tiles.length; i++) {
            if (bindGroups[i]) {
                pass.setBindGroup(0, bindGroups[i]);
                pass.draw(6);
            }
        }
        pass.end();
        this.device.queue.submit([encoder.finish()]);
        const tDraw = performance.now();
        this._drawTimeMs = tDraw - tPresent;
        let drawn = 0;
        for (const bg of bindGroups) {
            if (bg)
                drawn++;
        }
        this._tilesDrawn = drawn;
        this._frameTimeMs = tDraw - t0;
        this._framesRendered++;
        this._renderFpsCounter++;
        this._rafFpsCounter++;
        if (t0 - this._fpsTime >= 1000) {
            this._fps = this._renderFpsCounter;
            this._rafFps = this._rafFpsCounter;
            this._renderFpsCounter = 0;
            this._rafFpsCounter = 0;
            this._fpsTime = t0;
        }
    }
    pickFrame(slot, slotIndex, now) {
        const first = slot.buffer.peekFirstFrame();
        if (!first) {
            // Empty queue: freeze the clock so it doesn't run ahead.
            // Re-anchor on the next arriving frame.
            slot.freeRunStart = -1;
            slot.freeRunBasePTS = -1;
            return slot.lastFrame;
        }
        // Detect PTS discontinuity (e.g. SRT pusher file loop)
        if (slot.lastPTS >= 0 && first.timestamp < slot.lastPTS - PTS_DISCONTINUITY_US) {
            slot.freeRunStart = -1;
            slot.freeRunBasePTS = -1;
            slot.initialFillDone = false;
        }
        const queueSize = slot.buffer.getStats().queueSize;
        // Initial fill: take frames sequentially until buffer reaches
        // the target depth, then switch to adaptive timestamp mode.
        if (!slot.initialFillDone) {
            if (queueSize < ADAPTIVE_TARGET_QUEUE) {
                return slot.lastFrame;
            }
            slot.initialFillDone = true;
            // If the buffer is overfull from a GOP burst, skip ahead to
            // the tail so we start close to live instead of slowly draining.
            if (queueSize > ADAPTIVE_TARGET_QUEUE * 3) {
                const skip = slot.buffer.getFrameByTimestamp(Infinity);
                if (skip.frame) {
                    if (slot.lastFrame)
                        slot.lastFrame.close();
                    slot.lastFrame = skip.frame;
                    slot.lastPTS = skip.frame.timestamp;
                    slot.framesDiscarded += skip.discarded;
                }
            }
            const anchor = slot.buffer.peekFirstFrame();
            slot.freeRunStart = now;
            slot.freeRunBasePTS = anchor ? anchor.timestamp : first.timestamp;
        }
        // Re-anchor clock after starvation reset
        if (slot.freeRunStart < 0) {
            slot.freeRunStart = now;
            slot.freeRunBasePTS = first.timestamp;
        }
        // Adaptive rate: proportional correction based on queue depth error
        const error = queueSize - ADAPTIVE_TARGET_QUEUE;
        if (Math.abs(error) > ADAPTIVE_DEAD_ZONE) {
            const correction = Math.max(-ADAPTIVE_MAX_CORRECTION, Math.min(ADAPTIVE_MAX_CORRECTION, error * ADAPTIVE_GAIN));
            slot.clockRate = 1.0 + correction;
        }
        else {
            slot.clockRate = 1.0;
        }
        const elapsed = now - slot.freeRunStart;
        const targetPTS = slot.freeRunBasePTS + elapsed * 1000 * slot.clockRate;
        const result = slot.buffer.getFrameByTimestamp(targetPTS);
        if (result.frame) {
            if (slot.lastFrame) {
                slot.lastFrame.close();
            }
            slot.lastFrame = result.frame;
            slot.lastPTS = result.frame.timestamp;
            slot.framesDrawn++;
            slot.framesDiscarded += result.discarded;
            if (slot.lastFrameTime > 0) {
                const interval = now - slot.lastFrameTime;
                slot.frameIntervalSum += interval;
                if (interval < slot.frameIntervalMin)
                    slot.frameIntervalMin = interval;
                if (interval > slot.frameIntervalMax)
                    slot.frameIntervalMax = interval;
            }
            slot.lastFrameTime = now;
            const aspect = result.frame.displayWidth / result.frame.displayHeight;
            if (Math.abs(aspect - slot.lastAspect) > 0.01) {
                slot.lastAspect = aspect;
                this.updateTileTransform(slotIndex);
            }
        }
        return slot.lastFrame;
    }
    getTileDiagnostics(index) {
        if (index < 0 || index >= this.tiles.length)
            return null;
        const slot = this.tiles[index];
        const vStats = slot.buffer.getStats();
        const avgInterval = slot.framesDrawn > 1
            ? slot.frameIntervalSum / (slot.framesDrawn - 1)
            : 0;
        return {
            rafCount: this._framesRendered + this._skipped,
            framesDrawn: slot.framesDrawn,
            framesSkipped: 0,
            avgRafIntervalMs: this._framesRendered > 0
                ? 1000 / this._fps
                : 0,
            minRafIntervalMs: 0,
            maxRafIntervalMs: 0,
            avgDrawMs: this._framesRendered > 0 && this._tilesDrawn > 0
                ? this._drawTimeMs / this._tilesDrawn
                : 0,
            maxDrawMs: this._tilesDrawn > 0
                ? this._drawTimeMs / this._tilesDrawn
                : 0,
            avgFrameIntervalMs: avgInterval,
            minFrameIntervalMs: slot.frameIntervalMin === Infinity ? 0 : slot.frameIntervalMin,
            maxFrameIntervalMs: slot.frameIntervalMax,
            avSyncMs: 0,
            avSyncMin: 0,
            avSyncMax: 0,
            avSyncAvg: 0,
            clockMode: `adaptive(${slot.clockRate.toFixed(4)})`,
            emptyBufferHits: 0,
            currentVideoPTS: slot.lastPTS,
            currentAudioPTS: -1,
            videoQueueSize: vStats.queueSize,
            videoQueueMs: vStats.queueLengthMs,
            videoTotalDiscarded: vStats.totalDiscarded,
        };
    }
    get tileCount() {
        return this.tiles.length;
    }
    destroy() {
        for (const slot of this.tiles) {
            if (slot.lastFrame) {
                slot.lastFrame.close();
                slot.lastFrame = null;
            }
            slot.uniformBuffer.destroy();
        }
        this.tiles = [];
        this.pipeline = null;
        this.sampler = null;
        this.bindGroupLayout = null;
        if (this.device) {
            this.device.destroy();
            this.device = null;
        }
        this.context = null;
        this._ready = false;
    }
}
