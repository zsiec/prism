import { PrismPlayer } from "./player";
import { MoQMultiviewTransport } from "./moq-multiview-transport";
import { SCTE35HistoryPanel } from "./scte35-history";
import { WebGPUCompositor } from "./webgpu-compositor";
import { MultiviewVURenderer } from "./multiview-vu";
// Numpad layout: 7 8 9 / 4 5 6 / 1 2 3 maps to grid rows top->bottom
const NUMPAD_TO_GRID = {
    7: 0, 8: 1, 9: 2,
    4: 3, 5: 4, 6: 5,
    1: 6, 2: 7, 3: 8,
};
const GRID_TO_NUMPAD = {
    0: 7, 1: 8, 2: 9,
    3: 4, 4: 5, 5: 6,
    6: 1, 7: 2, 8: 3,
};
/**
 * Manages a 3x3 grid of PrismPlayer tiles for monitoring multiple live
 * streams simultaneously. Handles keyboard navigation, audio solo,
 * tile expansion, WebGPU compositing, and multiplexed MoQ transport.
 * Designed for broadcast monitoring use cases where an operator needs
 * to observe many feeds at once.
 */
export class MultiviewManager {
    container;
    grid;
    tiles = [];
    expandedIndex = null;
    expandOverlay = null;
    keyHandler = null;
    soloIndex = null;
    preExpandSoloIndex = null;
    focusedIndex = 0;
    muxTransport = null;
    connectedKeys = [];
    reconnectDelay = 2000;
    keyToMuxIndex = new Map();
    sharedAudioContext = null;
    scte35History;
    scte35SeenIds = new Set();
    helpOverlay = null;
    toastEl = null;
    toastTimer = null;
    compositor = null;
    compositorCanvas = null;
    compositorReady = false;
    sharedVUCanvas = null;
    sharedVURenderer = null;
    compositorInitPromise = null;
    renderLoopId = null;
    vuFrameCounter = 0;
    vuTileOffset = 0;
    perfOverlay = null;
    perfOverlayVisible = false;
    perfLoopFrameTime = 0;
    perfLoopIntervalMs = 0;
    perfLoopLastTime = 0;
    perfVuTimeMs = 0;
    perfLoopFps = 0;
    perfLoopFpsCounter = 0;
    perfLoopFpsTime = 0;
    perfHistory = [];
    perfRecording = false;
    perfRecordInterval = null;
    lastMuxViewerStats = {};
    constructor(container) {
        this.container = container;
        const wrapper = document.createElement("div");
        wrapper.style.display = "flex";
        wrapper.style.width = "100%";
        wrapper.style.height = "calc(100vh - 34px)";
        this.grid = document.createElement("div");
        this.grid.style.display = "grid";
        this.grid.style.gridTemplateColumns = "repeat(3, 1fr)";
        this.grid.style.gridTemplateRows = "repeat(3, 1fr)";
        this.grid.style.gap = "3px";
        this.grid.style.flex = "1";
        this.grid.style.minWidth = "0";
        this.grid.style.padding = "3px";
        this.grid.style.boxSizing = "border-box";
        this.grid.style.background = "#000";
        wrapper.appendChild(this.grid);
        this.scte35History = new SCTE35HistoryPanel();
        wrapper.appendChild(this.scte35History.getElement());
        this.container.appendChild(wrapper);
        this.compositorCanvas = document.createElement("canvas");
        this.compositorCanvas.style.position = "absolute";
        this.compositorCanvas.style.top = "0";
        this.compositorCanvas.style.left = "0";
        this.compositorCanvas.style.width = "100%";
        this.compositorCanvas.style.height = "100%";
        this.compositorCanvas.style.display = "none";
        this.compositorCanvas.style.pointerEvents = "none";
        this.compositorCanvas.style.zIndex = "0";
        this.compositorCanvas.style.borderRadius = "4px";
        this.grid.style.position = "relative";
        this.grid.insertBefore(this.compositorCanvas, this.grid.firstChild);
        this.sharedVUCanvas = document.createElement("canvas");
        this.sharedVUCanvas.style.position = "absolute";
        this.sharedVUCanvas.style.top = "0";
        this.sharedVUCanvas.style.left = "0";
        this.sharedVUCanvas.style.width = "100%";
        this.sharedVUCanvas.style.height = "100%";
        this.sharedVUCanvas.style.display = "none";
        this.sharedVUCanvas.style.pointerEvents = "none";
        this.sharedVUCanvas.style.zIndex = "2";
        this.grid.appendChild(this.sharedVUCanvas);
        this.sharedVURenderer = new MultiviewVURenderer(this.sharedVUCanvas);
        this.sharedVURenderer.setGrid(3, 3, 3);
        this.compositorInitPromise = this.initCompositor();
        for (let i = 0; i < 9; i++) {
            this.createTile(i);
        }
        this.updateFocusRing();
        this.keyHandler = (e) => this.handleKeyboard(e);
        document.addEventListener("keydown", this.keyHandler);
        const resumeAudio = () => {
            if (this.sharedAudioContext && this.sharedAudioContext.state === "suspended") {
                this.sharedAudioContext.resume();
            }
        };
        this.container.addEventListener("click", resumeAudio, { once: true });
        this.container.addEventListener("keydown", resumeAudio, { once: true });
    }
    // ── Keyboard ─────────────────────────────────────────────
    handleKeyboard(e) {
        if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement)
            return;
        if (e.key === "Escape") {
            if (this.helpOverlay) {
                this.hideHelp();
            }
            else if (this.expandedIndex !== null) {
                this.collapseExpanded();
            }
            else if (this.soloIndex !== null) {
                this.unsoloAudio();
            }
            e.preventDefault();
            return;
        }
        if (this.helpOverlay) {
            this.hideHelp();
            e.preventDefault();
            return;
        }
        if (e.key === "?" || (e.key === "/" && e.shiftKey)) {
            this.showHelp();
            e.preventDefault();
            return;
        }
        if (this.expandedIndex !== null) {
            let dx = 0, dy = 0;
            switch (e.key) {
                case "ArrowRight":
                    dx = 1;
                    break;
                case "ArrowLeft":
                    dx = -1;
                    break;
                case "ArrowDown":
                    dy = 1;
                    break;
                case "ArrowUp":
                    dy = -1;
                    break;
                default: return;
            }
            const col = this.expandedIndex % 3;
            const row = Math.floor(this.expandedIndex / 3);
            const nc = Math.max(0, Math.min(2, col + dx));
            const nr = Math.max(0, Math.min(2, row + dy));
            const newIdx = nr * 3 + nc;
            if (newIdx !== this.expandedIndex && this.tiles[newIdx]?.streamKey) {
                this.switchExpandedTile(newIdx);
            }
            e.preventDefault();
            return;
        }
        const digit = parseInt(e.key);
        if (digit >= 1 && digit <= 9) {
            const idx = NUMPAD_TO_GRID[digit] ?? (digit - 1);
            if (idx < this.tiles.length) {
                this.setFocus(idx);
            }
            e.preventDefault();
            return;
        }
        switch (e.key) {
            case "ArrowRight":
                this.moveFocus(1, 0);
                e.preventDefault();
                break;
            case "ArrowLeft":
                this.moveFocus(-1, 0);
                e.preventDefault();
                break;
            case "ArrowDown":
                this.moveFocus(0, 1);
                e.preventDefault();
                break;
            case "ArrowUp":
                this.moveFocus(0, -1);
                e.preventDefault();
                break;
            case "m":
            case "M":
                this.soloAudio(this.focusedIndex);
                e.preventDefault();
                break;
            case "a":
                this.cycleAudioOnFocused(1);
                e.preventDefault();
                break;
            case "A":
                this.cycleAudioOnFocused(-1);
                e.preventDefault();
                break;
            case "c":
            case "C":
                this.cycleCaptionsOnFocused();
                e.preventDefault();
                break;
            case "h":
            case "H":
                this.scte35History.toggle();
                e.preventDefault();
                break;
            case "p":
                this.togglePerfOverlay();
                e.preventDefault();
                break;
            case "P":
                this.dumpPerfSnapshot();
                e.preventDefault();
                break;
            case "r":
                if (!e.ctrlKey && !e.metaKey) {
                    this.togglePerfRecording();
                    e.preventDefault();
                }
                break;
            case "Enter":
            case " ":
                this.expandTile(this.focusedIndex);
                e.preventDefault();
                break;
        }
    }
    moveFocus(dx, dy) {
        const col = this.focusedIndex % 3;
        const row = Math.floor(this.focusedIndex / 3);
        const nc = Math.max(0, Math.min(2, col + dx));
        const nr = Math.max(0, Math.min(2, row + dy));
        this.setFocus(nr * 3 + nc);
    }
    setFocus(index) {
        if (index < 0 || index >= this.tiles.length)
            return;
        this.focusedIndex = index;
        this.updateFocusRing();
    }
    updateFocusRing() {
        for (const tile of this.tiles) {
            const focused = tile.index === this.focusedIndex;
            tile.container.style.borderColor = focused ? "rgba(59, 130, 246, 0.8)" : "transparent";
            tile.container.style.boxShadow = focused ? "0 0 0 1px rgba(59, 130, 246, 0.3)" : "none";
            tile.tileNumber.style.background = focused ? "rgba(59, 130, 246, 0.5)" : "rgba(255, 255, 255, 0.08)";
            tile.tileNumber.style.color = focused ? "#fff" : "#64748b";
            tile.streamNameLabel.style.color = focused ? "#e2e8f0" : "#94a3b8";
        }
    }
    cycleAudioOnFocused(direction) {
        const tile = this.tiles[this.focusedIndex];
        if (!tile?.streamKey)
            return;
        if (this.soloIndex !== tile.index) {
            this.soloAudio(tile.index);
        }
        const newTrack = tile.player.cycleAudioTrack(direction);
        const indices = tile.player.getAudioTrackIndices();
        const pos = indices.indexOf(newTrack) + 1;
        this.showToast(`Audio Track ${pos}/${indices.length}`, tile);
    }
    cycleCaptionsOnFocused() {
        const tile = this.tiles[this.focusedIndex];
        if (!tile?.streamKey)
            return;
        const ch = tile.player.cycleCaptionChannel();
        this.updateCCBadge(tile);
        this.showToast(ch === 0 ? "Captions Off" : `Captions CC${ch}`, tile);
    }
    updateCCBadge(tile) {
        const ch = tile.player.getActiveCaptionChannel();
        if (ch > 0) {
            tile.ccBadge.style.display = "flex";
            tile.ccBadge.textContent = ch <= 4 ? `CC${ch}` : `S${ch - 6}`;
        }
        else {
            tile.ccBadge.style.display = "none";
        }
    }
    // ── Toast ────────────────────────────────────────────────
    showToast(msg, tile) {
        if (this.toastTimer) {
            clearTimeout(this.toastTimer);
            this.toastTimer = null;
        }
        if (this.toastEl) {
            this.toastEl.remove();
            this.toastEl = null;
        }
        const el = document.createElement("div");
        el.style.position = "absolute";
        el.style.top = "50%";
        el.style.left = "50%";
        el.style.transform = "translate(-50%, -50%)";
        el.style.background = "rgba(0, 0, 0, 0.85)";
        el.style.color = "#e2e8f0";
        el.style.padding = "6px 14px";
        el.style.borderRadius = "3px";
        el.style.fontSize = "12px";
        el.style.fontFamily = "'SF Mono', monospace";
        el.style.fontWeight = "600";
        el.style.zIndex = "30";
        el.style.pointerEvents = "none";
        el.style.whiteSpace = "nowrap";
        el.style.transition = "opacity 0.2s ease";
        el.textContent = msg;
        tile.container.appendChild(el);
        this.toastEl = el;
        this.toastTimer = setTimeout(() => {
            el.style.opacity = "0";
            setTimeout(() => {
                el.remove();
                if (this.toastEl === el)
                    this.toastEl = null;
            }, 200);
        }, 1200);
    }
    // ── Help Overlay ─────────────────────────────────────────
    showHelp() {
        if (this.helpOverlay)
            return;
        const overlay = document.createElement("div");
        overlay.style.position = "fixed";
        overlay.style.inset = "0";
        overlay.style.background = "rgba(0, 0, 0, 0.85)";
        overlay.style.zIndex = "2000";
        overlay.style.display = "flex";
        overlay.style.alignItems = "center";
        overlay.style.justifyContent = "center";
        overlay.addEventListener("click", () => this.hideHelp());
        const card = document.createElement("div");
        card.style.background = "#1a1a1a";
        card.style.border = "1px solid #333";
        card.style.borderRadius = "6px";
        card.style.padding = "28px 36px";
        card.style.maxWidth = "460px";
        card.style.width = "100%";
        card.style.color = "#e2e8f0";
        card.addEventListener("click", (e) => e.stopPropagation());
        const title = document.createElement("h2");
        title.style.margin = "0 0 20px 0";
        title.style.fontSize = "16px";
        title.style.fontWeight = "700";
        title.style.fontFamily = "'SF Mono', monospace";
        title.style.color = "#60a5fa";
        title.textContent = "Keyboard Shortcuts";
        card.appendChild(title);
        const shortcuts = [
            ["1 - 9", "Select tile (numpad layout)"],
            ["Arrow Keys", "Navigate grid"],
            ["Click", "Select + expand tile"],
            ["M", "Solo / unsolo audio on tile"],
            ["A / Shift+A", "Cycle audio track fwd / back"],
            ["C", "Toggle / cycle captions (CC)"],
            ["Enter / Space", "Expand selected tile"],
            ["H", "Toggle SCTE-35 history"],
            ["p", "Toggle perf overlay"],
            ["Shift+P", "Perf snapshot to clipboard"],
            ["r", "Record perf history (toggle)"],
            ["?", "This help"],
            ["Esc", "Close expanded / unsolo / dismiss"],
        ];
        const table = document.createElement("div");
        table.style.display = "grid";
        table.style.gridTemplateColumns = "auto 1fr";
        table.style.gap = "8px 20px";
        table.style.fontFamily = "'SF Mono', monospace";
        table.style.fontSize = "13px";
        for (const [key, desc] of shortcuts) {
            const keyEl = document.createElement("span");
            keyEl.style.color = "#c084fc";
            keyEl.style.fontWeight = "600";
            keyEl.style.whiteSpace = "nowrap";
            keyEl.textContent = key;
            table.appendChild(keyEl);
            const descEl = document.createElement("span");
            descEl.style.color = "#94a3b8";
            descEl.textContent = desc;
            table.appendChild(descEl);
        }
        card.appendChild(table);
        const hint = document.createElement("div");
        hint.style.marginTop = "20px";
        hint.style.fontSize = "11px";
        hint.style.color = "#555";
        hint.style.textAlign = "center";
        hint.textContent = "Press any key or click to close";
        card.appendChild(hint);
        overlay.appendChild(card);
        document.body.appendChild(overlay);
        this.helpOverlay = overlay;
    }
    hideHelp() {
        if (this.helpOverlay) {
            this.helpOverlay.remove();
            this.helpOverlay = null;
        }
    }
    // ── WebGPU Compositor ────────────────────────────────────
    async initCompositor() {
        if (!this.compositorCanvas)
            return false;
        const comp = new WebGPUCompositor(this.compositorCanvas);
        const ok = await comp.init();
        if (ok) {
            this.compositor = comp;
            this.compositorReady = true;
            comp.setGrid(3, 3, 3);
        }
        return ok;
    }
    startRenderLoop() {
        if (this.renderLoopId !== null)
            return;
        this.vuFrameCounter = 0;
        this.perfLoopFpsTime = performance.now();
        this.perfLoopFpsCounter = 0;
        this.perfLoopLastTime = performance.now();
        const loop = () => {
            this.renderLoopId = requestAnimationFrame(loop);
            this.renderTick();
        };
        this.renderLoopId = requestAnimationFrame(loop);
    }
    renderTick() {
        const t0 = performance.now();
        if (this.perfLoopLastTime > 0) {
            this.perfLoopIntervalMs = t0 - this.perfLoopLastTime;
        }
        this.perfLoopLastTime = t0;
        if (this.compositorReady && this.compositor) {
            this.compositor.renderFrame();
        }
        else {
            for (const tile of this.tiles) {
                if (tile.streamKey) {
                    tile.player.renderOnce();
                }
            }
        }
        this.vuFrameCounter++;
        if (this.vuFrameCounter >= 3) {
            this.vuFrameCounter = 0;
            const vuStart = performance.now();
            if (this.sharedVURenderer && this.compositorReady) {
                this.sharedVURenderer.render(this.tiles);
            }
            else {
                const tilesPerBatch = 3;
                for (let i = 0; i < tilesPerBatch; i++) {
                    const idx = (this.vuTileOffset + i) % this.tiles.length;
                    const tile = this.tiles[idx];
                    if (tile.streamKey) {
                        tile.player.renderVUOnce();
                    }
                }
                this.vuTileOffset = (this.vuTileOffset + tilesPerBatch) % this.tiles.length;
            }
            this.perfVuTimeMs = performance.now() - vuStart;
        }
        const elapsed = performance.now() - t0;
        this.perfLoopFrameTime = elapsed;
        this.perfLoopFpsCounter++;
        if (t0 - this.perfLoopFpsTime >= 1000) {
            this.perfLoopFps = this.perfLoopFpsCounter;
            this.perfLoopFpsCounter = 0;
            this.perfLoopFpsTime = t0;
            this.updatePerfOverlay();
        }
    }
    stopRenderLoop() {
        if (this.renderLoopId !== null) {
            cancelAnimationFrame(this.renderLoopId);
            this.renderLoopId = null;
        }
    }
    // ── Performance Diagnostics ──────────────────────────────
    collectPerfSample() {
        const tileStats = [];
        for (const tile of this.tiles) {
            if (!tile.streamKey)
                continue;
            tileStats.push(tile.player.getPerfStats());
        }
        const compositorStats = (this.compositorReady && this.compositor)
            ? this.compositor.getPerfStats() : null;
        return {
            t: Date.now(),
            renderMode: this.compositorReady ? "WebGPU" : "Canvas2D",
            loopFps: this.perfLoopFps,
            loopFrameMs: +this.perfLoopFrameTime.toFixed(2),
            loopIntervalMs: +this.perfLoopIntervalMs.toFixed(2),
            vuMs: +this.perfVuTimeMs.toFixed(2),
            compositor: compositorStats ? {
                fps: compositorStats.fps,
                rafFps: compositorStats.rafFps,
                frameMs: +compositorStats.frameTimeMs.toFixed(2),
                pickMs: +compositorStats.pickTimeMs.toFixed(2),
                importMs: +compositorStats.importTimeMs.toFixed(2),
                presentMs: +compositorStats.presentTimeMs.toFixed(2),
                drawMs: +compositorStats.drawTimeMs.toFixed(2),
                tilesDrawn: compositorStats.tilesDrawn,
                skipped: compositorStats.skipped,
                totalQueue: compositorStats.tilesTotalQueue,
                totalDrops: compositorStats.tilesTotalDiscarded,
                canvasW: compositorStats.canvasWidth,
                canvasH: compositorStats.canvasHeight,
            } : null,
            audioCtx: this.sharedAudioContext ? {
                state: this.sharedAudioContext.state,
                sr: this.sharedAudioContext.sampleRate,
            } : null,
            tiles: tileStats.map(t => ({
                key: t.streamKey,
                vq: t.videoQueueSize,
                vqMs: +t.videoQueueMs.toFixed(0),
                vDrops: t.videoDiscarded,
                aTracks: t.audioTracks.length,
                aMetering: t.audioTracks.filter(a => a.metering).length,
                aMuted: t.audioTracks.filter(a => a.muted).length,
                aSilenceMs: +t.audioTracks.reduce((s, a) => s + a.silenceMs, 0).toFixed(0),
            })),
        };
    }
    togglePerfOverlay() {
        this.perfOverlayVisible = !this.perfOverlayVisible;
        if (this.perfOverlayVisible) {
            if (!this.perfOverlay) {
                this.perfOverlay = document.createElement("div");
                this.perfOverlay.style.position = "fixed";
                this.perfOverlay.style.bottom = "4px";
                this.perfOverlay.style.left = "4px";
                this.perfOverlay.style.background = "rgba(0, 0, 0, 0.92)";
                this.perfOverlay.style.color = "#94a3b8";
                this.perfOverlay.style.fontFamily = "'SF Mono', 'Menlo', monospace";
                this.perfOverlay.style.fontSize = "10px";
                this.perfOverlay.style.lineHeight = "1.6";
                this.perfOverlay.style.padding = "8px 12px";
                this.perfOverlay.style.borderRadius = "3px";
                this.perfOverlay.style.border = "1px solid #1a1a1a";
                this.perfOverlay.style.zIndex = "3000";
                this.perfOverlay.style.pointerEvents = "none";
                this.perfOverlay.style.minWidth = "300px";
                this.perfOverlay.style.whiteSpace = "pre";
                document.body.appendChild(this.perfOverlay);
            }
            this.perfOverlay.style.display = "block";
            this.updatePerfOverlay();
        }
        else {
            if (this.perfOverlay) {
                this.perfOverlay.style.display = "none";
            }
        }
    }
    updatePerfOverlay() {
        if (!this.perfOverlayVisible || !this.perfOverlay)
            return;
        const s = this.collectPerfSample();
        const lines = [];
        lines.push(`PERF  ${s.renderMode}  ${s.loopFps} fps  work=${s.loopFrameMs}ms  interval=${s.loopIntervalMs}ms  vu=${s.vuMs}ms`);
        if (s.compositor) {
            const rafFps = s.compositor.rafFps ?? s.compositor.fps;
            lines.push(`GPU   ${s.compositor.fps}/${rafFps} fps (render/rAF)  ${s.compositor.frameMs}ms  ${s.compositor.tilesDrawn}/${this.tiles.length} tiles  skip=${s.compositor.skipped ?? 0}`);
            const presentMs = s.compositor.presentMs ?? 0;
            lines.push(`  pick=${s.compositor.pickMs ?? 0}ms  import=${s.compositor.importMs ?? 0}ms  present=${presentMs}ms  draw=${s.compositor.drawMs ?? 0}ms`);
            const cw = s.compositor.canvasW ?? 0;
            const ch = s.compositor.canvasH ?? 0;
            lines.push(`VBUF  q=${s.compositor.totalQueue}  drop=${s.compositor.totalDrops}  canvas=${cw}x${ch}`);
        }
        lines.push("");
        for (const t of s.tiles) {
            const key = (t.key ?? "?").padEnd(12).slice(0, 12);
            const vq = `vq=${t.vq}/${t.vqMs}ms`;
            const vd = t.vDrops > 0 ? ` d=${t.vDrops}` : "";
            lines.push(`${key} ${vq}${vd} a=${t.aTracks}(${t.aMetering}m/${t.aMuted}x)`);
        }
        if (s.audioCtx) {
            lines.push(`\nAudioCtx: ${s.audioCtx.state}  sr=${s.audioCtx.sr}`);
        }
        const rec = this.perfRecording ? `  \u25CF REC (${this.perfHistory.length})` : "";
        lines.push(`\n[p] overlay  [P] snapshot+copy  [R] record${rec}`);
        this.perfOverlay.textContent = lines.join("\n");
    }
    async dumpPerfSnapshot() {
        const sample = this.collectPerfSample();
        const startTime = this.perfLoopFpsTime;
        const tileDiags = [];
        for (let i = 0; i < this.tiles.length; i++) {
            const tile = this.tiles[i];
            const tileStats = sample.tiles.find(t => t.key === tile.streamKey) ?? {
                key: tile.streamKey, vq: 0, vqMs: 0, vDrops: 0, aTracks: 0, aMetering: 0, aMuted: 0, aSilenceMs: 0,
            };
            let diag = null;
            if (tile.streamKey) {
                diag = await tile.player.collectDiagnostics();
                if (diag && this.compositorReady && this.compositor) {
                    const compDiag = this.compositor.getTileDiagnostics(i);
                    if (compDiag) {
                        diag.renderer = compDiag;
                    }
                }
            }
            tileDiags.push({ key: tile.streamKey, index: i, stats: tileStats, diagnostics: diag });
        }
        const activeTiles = tileDiags.filter(t => t.diagnostics !== null);
        let totalDecodeFps = 0;
        let totalUnderruns = 0;
        let totalSilenceMs = 0;
        let totalVideoDropped = 0;
        let totalVideoQueueFrames = 0;
        let totalRingBufferMs = 0;
        let totalAudioWorklets = 0;
        let worstTile = null;
        let worstFrameInterval = 0;
        for (const t of activeTiles) {
            const d = t.diagnostics;
            totalDecodeFps += d.videoDecoder.outputFps;
            totalUnderruns += d.audio.underruns;
            totalSilenceMs += d.audio.totalSilenceMs;
            totalVideoDropped += d.renderer.videoTotalDiscarded;
            totalVideoQueueFrames += d.renderer.videoQueueSize;
            totalRingBufferMs += d.audio.scheduleAheadMs;
            if (d.audio.isPlaying)
                totalAudioWorklets++;
            if (d.renderer.avgFrameIntervalMs > worstFrameInterval) {
                worstFrameInterval = d.renderer.avgFrameIntervalMs;
                worstTile = { key: t.key ?? "?", metric: "avgFrameIntervalMs", value: +d.renderer.avgFrameIntervalMs.toFixed(1) };
            }
        }
        const budgetMs = sample.loopIntervalMs > 0 ? sample.loopIntervalMs : 16.67;
        let totalAudioDropped = 0;
        const serverDebug = {};
        for (const t of activeTiles) {
            if (t.key && t.diagnostics?.serverDebug) {
                serverDebug[t.key] = t.diagnostics.serverDebug;
            }
        }
        const muxViewerStats = this.lastMuxViewerStats;
        if (Object.keys(muxViewerStats).length > 0) {
            serverDebug["viewerStats"] = muxViewerStats;
            for (const vs of Object.values(muxViewerStats)) {
                totalAudioDropped += vs.audioDropped;
            }
        }
        const snapshot = {
            _format: "prism-multiview-perf-v1",
            timestamp: new Date().toISOString(),
            uptimeMs: startTime > 0 ? performance.now() - startTime : 0,
            renderMode: sample.renderMode,
            compositor: sample.compositor,
            mainThread: {
                loopFps: sample.loopFps,
                loopFrameMs: sample.loopFrameMs,
                loopIntervalMs: sample.loopIntervalMs,
                vuMs: sample.vuMs,
                budgetUtilization: +(sample.loopFrameMs / budgetMs * 100).toFixed(1),
            },
            aggregate: {
                activeTiles: activeTiles.length,
                totalDecodeFps: +totalDecodeFps.toFixed(1),
                totalAudioWorklets,
                totalUnderruns,
                totalSilenceMs: +totalSilenceMs.toFixed(0),
                totalVideoDropped,
                totalAudioDropped,
                totalVideoQueueFrames,
                totalRingBufferMs: +totalRingBufferMs.toFixed(0),
                worstTile,
            },
            audioCtx: sample.audioCtx,
            tiles: tileDiags,
            server: serverDebug,
        };
        const json = JSON.stringify(snapshot, null, 2);
        navigator.clipboard.writeText(json).then(() => this.showToast("Multiview perf snapshot copied", this.tiles[this.focusedIndex]), () => {
            console.warn("Clipboard write failed");
            globalThis["__prismPerf"] = json;
            this.showToast("Snapshot in console (copy failed)", this.tiles[this.focusedIndex]);
        });
    }
    togglePerfRecording() {
        if (this.perfRecording) {
            this.perfRecording = false;
            if (this.perfRecordInterval) {
                clearInterval(this.perfRecordInterval);
                this.perfRecordInterval = null;
            }
            this.showToast(`Recording stopped (${this.perfHistory.length} samples)`, this.tiles[this.focusedIndex]);
        }
        else {
            this.perfHistory = [];
            this.perfRecording = true;
            this.perfRecordInterval = setInterval(() => {
                if (!this.perfRecording)
                    return;
                this.perfHistory.push(this.collectPerfSample());
                if (this.perfHistory.length > 300) {
                    this.perfHistory.shift();
                }
            }, 1000);
            this.showToast("Recording perf (1/sec, max 5min)", this.tiles[this.focusedIndex]);
        }
    }
    // ── Connection ───────────────────────────────────────────
    /**
     * Connect up to 9 streams over a single multiplexed WebTransport session.
     * If the same set of stream keys is already connected, only metadata
     * (labels, descriptions) is updated without reconnecting.
     */
    connectAll(streams) {
        const sorted = streams.slice(0, 9).sort((a, b) => a.key.localeCompare(b.key));
        const newKeys = sorted.map(s => s.key);
        const descMap = new Map();
        for (const s of sorted) {
            descMap.set(s.key, s.description ?? "");
        }
        const sameSet = newKeys.length === this.connectedKeys.length &&
            newKeys.every((k, i) => k === this.connectedKeys[i]);
        if (sameSet && this.muxTransport) {
            for (let i = 0; i < this.tiles.length && i < newKeys.length; i++) {
                const tile = this.tiles[i];
                tile.label.textContent = newKeys[i];
                tile.descriptionText = descMap.get(newKeys[i]) ?? "";
                tile.description.textContent = tile.descriptionText;
                tile.streamNameLabel.textContent = newKeys[i];
            }
            return;
        }
        for (let i = 0; i < Math.min(sorted.length, 9); i++) {
            const tile = this.tiles[i];
            tile.streamKey = sorted[i].key;
            tile.label.textContent = sorted[i].key;
            tile.descriptionText = sorted[i].description ?? "";
            tile.description.textContent = tile.descriptionText;
            tile.streamNameLabel.textContent = sorted[i].key;
            tile.tileStatusText.textContent = "Connecting\u2026";
            tile.tileStatus.style.display = "flex";
        }
        this.closeMuxTransport();
        this.connectedKeys = newKeys;
        const keyToTile = new Map();
        for (let i = 0; i < Math.min(newKeys.length, 9); i++) {
            keyToTile.set(newKeys[i], this.tiles[i]);
        }
        this.keyToMuxIndex.clear();
        let useGPU = false;
        const sharedCallbacks = {
            onSetup: async () => {
                this.reconnectDelay = 2000;
                if (!this.sharedAudioContext) {
                    this.sharedAudioContext = new AudioContext({ sampleRate: 48000, latencyHint: "interactive" });
                }
                if (this.compositorInitPromise) {
                    await this.compositorInitPromise;
                    this.compositorInitPromise = null;
                }
                useGPU = !!(this.compositorReady && this.compositor);
            },
            onStreamReady: async (ms) => {
                const tile = keyToTile.get(ms.key);
                if (!tile)
                    return;
                this.keyToMuxIndex.set(ms.key, ms.index);
                tile.player.setExternallyDriven(true);
                if (useGPU) {
                    tile.player.setCondensed(true);
                }
                await tile.player.connectMux(ms.key, ms.tracks, this.sharedAudioContext ?? undefined, true);
                const tileRef = tile;
                const cb = {
                    onVideoFrame: (data, isKeyframe, timestamp, _groupID, description) => {
                        tileRef.player.injectVideoFrame(data, isKeyframe, timestamp, description ?? undefined);
                    },
                    onAudioFrame: (data, timestamp, _groupID, trackIndex) => {
                        tileRef.player.injectAudioFrame(data, timestamp, trackIndex);
                    },
                    onCaptionFrame: (caption, _timestamp) => {
                        const hadCC = tileRef.player.getActiveCaptionChannel() > 0;
                        tileRef.player.injectCaptionData(caption);
                        if (!hadCC && tileRef.player.getActiveCaptionChannel() > 0) {
                            this.updateCCBadge(tileRef);
                        }
                    },
                };
                this.muxTransport.setStreamCallbacks(ms.index, cb);
                if (!useGPU) {
                    tile.player.setMaxResolution(640);
                }
                // Start render loop as soon as the first tile is ready.
                this.startRenderLoop();
            },
            onAllReady: () => {
                if (useGPU) {
                    const buffers = this.tiles
                        .filter(t => t.streamKey)
                        .map(t => t.player.getVideoBuffer());
                    this.compositor.setTileBuffers(buffers);
                    this.compositorCanvas.style.display = "block";
                    if (this.sharedVUCanvas) {
                        this.sharedVUCanvas.style.display = "block";
                    }
                    for (const tile of this.tiles) {
                        if (tile.streamKey) {
                            this.hidePlayerVideoCanvas(tile);
                            this.hidePlayerVUCanvas(tile);
                        }
                    }
                }
                this.muxTransport.enableAllAudio();
            },
            onMuxStats: (stats, viewerStats) => {
                for (const [key, stat] of Object.entries(stats)) {
                    const tile = keyToTile.get(key);
                    if (tile) {
                        tile.player.injectServerStats(stat);
                        this.processScte35ForTile(key, stat, tile);
                        if (tile.tileStatus.style.display !== "none") {
                            tile.tileStatus.style.display = "none";
                        }
                        const tc = stat.video?.timecode ?? "";
                        if (tc) {
                            tile.tcDisplay.textContent = tc;
                            tile.tcDisplay.style.display = "flex";
                        }
                        else {
                            tile.tcDisplay.style.display = "none";
                        }
                    }
                }
                if (viewerStats) {
                    this.lastMuxViewerStats = viewerStats;
                }
            },
            onClose: () => {
                if (!this.muxTransport)
                    return;
                this.muxTransport = null;
                if (this.connectedKeys.length > 0) {
                    const delay = this.reconnectDelay + Math.random() * 1000;
                    this.reconnectDelay = Math.min(this.reconnectDelay * 2, 16000);
                    setTimeout(() => {
                        if (this.connectedKeys.length > 0 && !this.muxTransport) {
                            this.reconnect();
                        }
                    }, delay);
                }
            },
            onError: (err) => {
                console.error("[MultiviewMux]", err);
            },
        };
        this.muxTransport = new MoQMultiviewTransport(newKeys, sharedCallbacks);
        this.muxTransport.connect();
    }
    /** Connect a single tile to an individual stream using its own transport. */
    connectTile(index, streamKey, description) {
        if (index < 0 || index >= 9)
            return;
        const tile = this.tiles[index];
        if (tile.streamKey === streamKey && tile.player.isConnected())
            return;
        tile.streamKey = streamKey;
        tile.label.textContent = streamKey;
        tile.descriptionText = description ?? "";
        tile.description.textContent = tile.descriptionText;
        tile.streamNameLabel.textContent = streamKey;
        tile.player.connect(streamKey);
    }
    /** Disconnect all tiles and close the mux transport and shared audio context. */
    disconnectAll() {
        this.stopRenderLoop();
        this.closeMuxTransport();
        this.closeAudioContext();
        this.connectedKeys = [];
        this.lastMuxViewerStats = {};
        for (const tile of this.tiles) {
            tile.player.setExternallyDriven(false);
            this.showPlayerVideoCanvas(tile);
            tile.player.disconnect();
            tile.streamKey = null;
            tile.label.textContent = "\u2014";
            tile.descriptionText = "";
            tile.description.textContent = "";
            tile.streamNameLabel.textContent = "";
            tile.tileStatusText.textContent = "";
            tile.tileStatus.style.display = "none";
        }
        if (this.compositorCanvas) {
            this.compositorCanvas.style.display = "none";
        }
        if (this.sharedVUCanvas) {
            this.sharedVUCanvas.style.display = "none";
        }
    }
    /** Tear down all tiles, transports, compositor, and event listeners. */
    destroy() {
        if (this.keyHandler) {
            document.removeEventListener("keydown", this.keyHandler);
            this.keyHandler = null;
        }
        this.stopRenderLoop();
        this.hideHelp();
        this.collapseExpanded();
        this.closeMuxTransport();
        this.closeAudioContext();
        if (this.compositor) {
            this.compositor.destroy();
            this.compositor = null;
        }
        if (this.sharedVURenderer) {
            this.sharedVURenderer.destroy();
            this.sharedVURenderer = null;
        }
        if (this.perfRecordInterval) {
            clearInterval(this.perfRecordInterval);
            this.perfRecordInterval = null;
        }
        if (this.perfOverlay) {
            this.perfOverlay.remove();
            this.perfOverlay = null;
        }
        this.connectedKeys = [];
        for (const tile of this.tiles) {
            tile.player.destroy();
        }
        this.tiles = [];
        this.container.innerHTML = "";
    }
    reconnect() {
        if (this.connectedKeys.length === 0)
            return;
        const streams = this.connectedKeys.map(key => {
            const tile = this.tiles.find(t => t.streamKey === key);
            return {
                key,
                viewers: 0,
                description: tile?.descriptionText,
            };
        });
        this.connectAll(streams);
    }
    hidePlayerVideoCanvas(tile) {
        const canvas = tile.playerContainer.querySelector("canvas:first-child");
        if (canvas) {
            canvas.style.display = "none";
        }
        tile.container.style.background = "transparent";
    }
    hidePlayerVUCanvas(tile) {
        const canvases = tile.playerContainer.querySelectorAll("canvas");
        if (canvases.length >= 2) {
            canvases[1].style.display = "none";
        }
    }
    showPlayerVideoCanvas(tile) {
        const canvas = tile.playerContainer.querySelector("canvas:first-child");
        if (canvas) {
            canvas.style.display = "block";
        }
        tile.container.style.background = "#111";
    }
    closeMuxTransport() {
        this.stopRenderLoop();
        if (this.muxTransport) {
            this.muxTransport.close();
            this.muxTransport = null;
        }
        if (this.compositorCanvas) {
            this.compositorCanvas.style.display = "none";
        }
        if (this.sharedVUCanvas) {
            this.sharedVUCanvas.style.display = "none";
        }
    }
    closeAudioContext() {
        if (this.sharedAudioContext) {
            this.sharedAudioContext.close();
            this.sharedAudioContext = null;
        }
    }
    // ── SCTE-35 ──────────────────────────────────────────────
    processScte35ForTile(streamKey, stats, tile) {
        const events = stats.scte35?.recent;
        if (!events || events.length === 0) {
            tile.scte35Badge.style.display = "none";
            return;
        }
        const newEvents = [];
        for (const event of events) {
            const id = `${event.receivedAt}-${event.commandType}-${event.eventId ?? 0}-${streamKey}`;
            if (!this.scte35SeenIds.has(id)) {
                this.scte35SeenIds.add(id);
                this.scte35History.addEvent(streamKey, event);
                newEvents.push(event);
            }
        }
        tile.scte35Badge.style.display = "flex";
        tile.scte35Badge.textContent = `SCTE`;
        if (newEvents.length > 0) {
            tile.scte35Badge.style.background = "rgba(168, 85, 247, 0.8)";
            setTimeout(() => {
                tile.scte35Badge.style.background = "rgba(168, 85, 247, 0.3)";
            }, 1500);
            for (const event of newEvents) {
                this.showScte35Toast(tile, event);
            }
        }
    }
    static scte35StyleInjected = false;
    static injectScte35Styles() {
        if (MultiviewManager.scte35StyleInjected)
            return;
        MultiviewManager.scte35StyleInjected = true;
        const style = document.createElement("style");
        // GPU-composited animation: slide in, hold, continuous decay, slide out.
        // 0-6%   = entrance (0.3s of 5s)
        // 6-35%  = full opacity hold
        // 35-90% = gradual decay
        // 90-100% = exit slide
        style.textContent = `
			@keyframes scte35-toast-lifecycle {
				0%   { opacity: 0; transform: translateY(-6px); }
				6%   { opacity: 1; transform: translateY(0); }
				35%  { opacity: 0.9; }
				55%  { opacity: 0.55; }
				75%  { opacity: 0.3; }
				90%  { opacity: 0.1; transform: translateY(0); }
				100% { opacity: 0; transform: translateY(-6px); }
			}
			@keyframes prism-pulse {
				0%, 100% { opacity: 0.4; }
				50% { opacity: 1; }
			}
		`;
        document.head.appendChild(style);
    }
    showScte35Toast(tile, event) {
        MultiviewManager.injectScte35Styles();
        const TOAST_DURATION_S = 5;
        const el = document.createElement("div");
        el.style.display = "flex";
        el.style.alignItems = "center";
        el.style.gap = "5px";
        el.style.padding = "3px 8px 3px 10px";
        el.style.borderRadius = "3px";
        el.style.fontSize = "9px";
        el.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        el.style.fontWeight = "600";
        el.style.whiteSpace = "nowrap";
        el.style.background = "rgba(168, 85, 247, 0.85)";
        el.style.color = "#fff";
        el.style.maxWidth = "100%";
        el.style.overflow = "hidden";
        el.style.boxSizing = "border-box";
        el.style.willChange = "opacity, transform";
        el.style.animation = `scte35-toast-lifecycle ${TOAST_DURATION_S}s ease-out forwards`;
        el.style.borderLeft = "2px solid #a855f7";
        const icon = document.createElement("span");
        icon.textContent = "\u26A1";
        icon.style.fontSize = "10px";
        icon.style.flexShrink = "0";
        el.appendChild(icon);
        const label = document.createElement("span");
        label.style.fontWeight = "700";
        label.style.fontSize = "9px";
        label.style.letterSpacing = "0.06em";
        label.style.flexShrink = "0";
        label.textContent = "SCTE-35";
        el.appendChild(label);
        const desc = document.createElement("span");
        desc.style.overflow = "hidden";
        desc.style.textOverflow = "ellipsis";
        desc.style.fontSize = "9px";
        desc.textContent = event.description || event.commandType;
        el.appendChild(desc);
        if (event.duration && event.duration > 0) {
            const dur = document.createElement("span");
            dur.style.opacity = "0.7";
            dur.style.fontSize = "9px";
            dur.style.flexShrink = "0";
            dur.textContent = `${event.duration.toFixed(1)}s`;
            el.appendChild(dur);
        }
        while (tile.scte35ToastArea.children.length >= 2) {
            tile.scte35ToastArea.removeChild(tile.scte35ToastArea.firstChild);
        }
        tile.scte35ToastArea.appendChild(el);
        el.addEventListener("animationend", () => el.remove(), { once: true });
    }
    // ── Audio ────────────────────────────────────────────────
    soloAudio(index) {
        if (this.soloIndex === index) {
            this.unsoloAudio();
            return;
        }
        this.soloIndex = index;
        for (const tile of this.tiles) {
            tile.player.setGlobalMute(tile.index !== index);
            this.updateAudioIndicator(tile);
        }
    }
    unsoloAudio() {
        this.soloIndex = null;
        for (const tile of this.tiles) {
            tile.player.setGlobalMute(true);
            this.updateAudioIndicator(tile);
        }
    }
    updateAudioIndicator(tile) {
        if (tile.index === this.soloIndex) {
            this.applySoloAudioStyle(tile.audioIndicator);
        }
        else {
            this.applyMutedAudioStyle(tile.audioIndicator);
        }
    }
    applyMutedAudioStyle(el) {
        el.style.background = "rgba(255, 255, 255, 0.05)";
        el.style.color = "#475569";
        el.textContent = "M";
    }
    applySoloAudioStyle(el) {
        el.style.background = "rgba(34, 197, 94, 0.25)";
        el.style.color = "#4ade80";
        el.textContent = "S";
    }
    // ── Tile Creation ────────────────────────────────────────
    createTile(index) {
        const tileContainer = document.createElement("div");
        tileContainer.style.position = "relative";
        tileContainer.style.overflow = "hidden";
        tileContainer.style.borderRadius = "4px";
        tileContainer.style.background = "#0a0a0a";
        tileContainer.style.cursor = "pointer";
        tileContainer.style.zIndex = "1";
        tileContainer.style.border = "1px solid transparent";
        tileContainer.style.transition = "border-color 0.15s ease, box-shadow 0.15s ease";
        const playerContainer = document.createElement("div");
        playerContainer.style.width = "100%";
        playerContainer.style.height = "100%";
        tileContainer.appendChild(playerContainer);
        // ── Top bar: tally number + badges ────────────────────
        const topBar = document.createElement("div");
        topBar.style.position = "absolute";
        topBar.style.top = "0";
        topBar.style.left = "0";
        topBar.style.right = "0";
        topBar.style.display = "flex";
        topBar.style.alignItems = "center";
        topBar.style.gap = "5px";
        topBar.style.padding = "3px 6px";
        topBar.style.background = "linear-gradient(to bottom, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.75) 70%, transparent 100%)";
        topBar.style.pointerEvents = "none";
        topBar.style.zIndex = "20";
        const tileNumber = document.createElement("div");
        const numpadKey = GRID_TO_NUMPAD[index] ?? (index + 1);
        tileNumber.textContent = String(numpadKey);
        tileNumber.style.width = "20px";
        tileNumber.style.height = "20px";
        tileNumber.style.display = "flex";
        tileNumber.style.alignItems = "center";
        tileNumber.style.justifyContent = "center";
        tileNumber.style.borderRadius = "3px";
        tileNumber.style.background = "rgba(255, 255, 255, 0.08)";
        tileNumber.style.color = "#64748b";
        tileNumber.style.fontSize = "11px";
        tileNumber.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        tileNumber.style.fontWeight = "700";
        tileNumber.style.flexShrink = "0";
        tileNumber.style.lineHeight = "1";
        const streamNameLabel = document.createElement("div");
        streamNameLabel.style.overflow = "hidden";
        streamNameLabel.style.textOverflow = "ellipsis";
        streamNameLabel.style.whiteSpace = "nowrap";
        streamNameLabel.style.color = "#94a3b8";
        streamNameLabel.style.fontSize = "9px";
        streamNameLabel.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        streamNameLabel.style.fontWeight = "600";
        streamNameLabel.style.flexShrink = "1";
        streamNameLabel.style.minWidth = "0";
        streamNameLabel.style.lineHeight = "1";
        const identityGroup = document.createElement("div");
        identityGroup.style.display = "flex";
        identityGroup.style.alignItems = "center";
        identityGroup.style.gap = "4px";
        identityGroup.style.marginRight = "2px";
        identityGroup.style.minWidth = "0";
        identityGroup.appendChild(tileNumber);
        identityGroup.appendChild(streamNameLabel);
        topBar.appendChild(identityGroup);
        // SCTE-35 badge
        const scte35Badge = document.createElement("div");
        scte35Badge.style.display = "none";
        scte35Badge.style.alignItems = "center";
        scte35Badge.style.padding = "1px 5px";
        scte35Badge.style.borderRadius = "3px";
        scte35Badge.style.background = "rgba(168, 85, 247, 0.25)";
        scte35Badge.style.color = "#c084fc";
        scte35Badge.style.fontSize = "8px";
        scte35Badge.style.fontFamily = "'SF Mono', monospace";
        scte35Badge.style.fontWeight = "700";
        scte35Badge.style.letterSpacing = "0.5px";
        scte35Badge.style.transition = "background 0.3s ease";
        topBar.appendChild(scte35Badge);
        // CC badge
        const ccBadge = document.createElement("div");
        ccBadge.style.display = "none";
        ccBadge.style.alignItems = "center";
        ccBadge.style.padding = "1px 5px";
        ccBadge.style.borderRadius = "3px";
        ccBadge.style.background = "rgba(250, 204, 21, 0.2)";
        ccBadge.style.color = "#facc15";
        ccBadge.style.fontSize = "8px";
        ccBadge.style.fontFamily = "'SF Mono', monospace";
        ccBadge.style.fontWeight = "700";
        ccBadge.style.letterSpacing = "0.5px";
        ccBadge.textContent = "CC";
        topBar.appendChild(ccBadge);
        // Spacer pushes right-aligned items (timecode, audio) to right
        const topSpacer = document.createElement("div");
        topSpacer.style.flex = "1";
        topBar.appendChild(topSpacer);
        // Timecode display (right-aligned, next to audio indicator)
        const tcDisplay = document.createElement("div");
        tcDisplay.style.display = "none";
        tcDisplay.style.alignItems = "center";
        tcDisplay.style.padding = "1px 5px";
        tcDisplay.style.borderRadius = "3px";
        tcDisplay.style.background = "rgba(255, 255, 255, 0.08)";
        tcDisplay.style.color = "#e2e8f0";
        tcDisplay.style.fontSize = "10px";
        tcDisplay.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        tcDisplay.style.fontWeight = "600";
        tcDisplay.style.letterSpacing = "0.3px";
        tcDisplay.style.fontVariantNumeric = "tabular-nums";
        topBar.appendChild(tcDisplay);
        // Audio indicator — always visible, shows muted/live state
        const audioIndicator = document.createElement("div");
        audioIndicator.style.display = "flex";
        audioIndicator.style.alignItems = "center";
        audioIndicator.style.justifyContent = "center";
        audioIndicator.style.width = "18px";
        audioIndicator.style.height = "18px";
        audioIndicator.style.borderRadius = "3px";
        audioIndicator.style.flexShrink = "0";
        audioIndicator.style.fontSize = "11px";
        audioIndicator.style.lineHeight = "1";
        this.applyMutedAudioStyle(audioIndicator);
        topBar.appendChild(audioIndicator);
        tileContainer.appendChild(topBar);
        // ── Tile status overlay (Connecting / No Signal) ─────
        const tileStatus = document.createElement("div");
        tileStatus.style.position = "absolute";
        tileStatus.style.top = "0";
        tileStatus.style.left = "0";
        tileStatus.style.right = "0";
        tileStatus.style.bottom = "0";
        tileStatus.style.display = "flex";
        tileStatus.style.alignItems = "center";
        tileStatus.style.justifyContent = "center";
        tileStatus.style.flexDirection = "column";
        tileStatus.style.gap = "6px";
        tileStatus.style.pointerEvents = "none";
        tileStatus.style.zIndex = "18";
        const tileStatusText = document.createElement("span");
        tileStatusText.style.color = "#64748b";
        tileStatusText.style.fontSize = "11px";
        tileStatusText.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        tileStatusText.style.fontWeight = "600";
        tileStatusText.style.letterSpacing = "0.5px";
        tileStatusText.style.textTransform = "uppercase";
        tileStatusText.style.animation = "prism-pulse 1.5s ease-in-out infinite";
        tileStatus.appendChild(tileStatusText);
        tileContainer.appendChild(tileStatus);
        // Hidden label/desc elements kept for data storage
        const label = document.createElement("span");
        label.style.display = "none";
        const desc = document.createElement("span");
        desc.style.display = "none";
        // ── Bottom gradient for caption readability ──────────
        const bottomGradient = document.createElement("div");
        bottomGradient.style.position = "absolute";
        bottomGradient.style.bottom = "0";
        bottomGradient.style.left = "0";
        bottomGradient.style.right = "0";
        bottomGradient.style.height = "18%";
        bottomGradient.style.background = "linear-gradient(to top, rgba(0,0,0,0.6) 0%, transparent 100%)";
        bottomGradient.style.pointerEvents = "none";
        bottomGradient.style.zIndex = "19";
        tileContainer.appendChild(bottomGradient);
        // ── SCTE-35 toast area (top of tile, below header) ───
        const scte35ToastArea = document.createElement("div");
        scte35ToastArea.style.position = "absolute";
        scte35ToastArea.style.top = "26px";
        scte35ToastArea.style.left = "20px";
        scte35ToastArea.style.right = "20px";
        scte35ToastArea.style.display = "flex";
        scte35ToastArea.style.flexDirection = "column";
        scte35ToastArea.style.gap = "2px";
        scte35ToastArea.style.padding = "2px 0";
        scte35ToastArea.style.pointerEvents = "none";
        scte35ToastArea.style.zIndex = "22";
        tileContainer.appendChild(scte35ToastArea);
        // ── Solo audio button (hover-reveal) ──────────────────
        const audioBtn = document.createElement("button");
        audioBtn.style.position = "absolute";
        audioBtn.style.top = "28px";
        audioBtn.style.right = "6px";
        audioBtn.style.background = "rgba(0,0,0,0.75)";
        audioBtn.style.border = "1px solid rgba(255,255,255,0.15)";
        audioBtn.style.color = "#94a3b8";
        audioBtn.style.padding = "3px 8px";
        audioBtn.style.borderRadius = "3px";
        audioBtn.style.cursor = "pointer";
        audioBtn.style.fontSize = "10px";
        audioBtn.style.fontFamily = "'SF Mono', monospace";
        audioBtn.style.fontWeight = "600";
        audioBtn.style.letterSpacing = "0.3px";
        audioBtn.style.zIndex = "25";
        audioBtn.style.opacity = "0";
        audioBtn.style.transition = "opacity 0.15s ease, background 0.1s ease";
        audioBtn.textContent = "SOLO";
        audioBtn.addEventListener("mouseenter", () => {
            audioBtn.style.color = "#fff";
            audioBtn.style.background = "rgba(37, 99, 235, 0.6)";
            audioBtn.style.borderColor = "rgba(59, 130, 246, 0.5)";
        });
        audioBtn.addEventListener("mouseleave", () => {
            audioBtn.style.color = "#94a3b8";
            audioBtn.style.background = "rgba(0,0,0,0.75)";
            audioBtn.style.borderColor = "rgba(255,255,255,0.15)";
        });
        audioBtn.addEventListener("click", (e) => {
            e.stopPropagation();
            this.soloAudio(index);
        });
        tileContainer.appendChild(audioBtn);
        tileContainer.addEventListener("mouseenter", () => {
            audioBtn.style.opacity = "1";
            if (index !== this.focusedIndex) {
                tileContainer.style.borderColor = "rgba(255, 255, 255, 0.15)";
            }
        });
        tileContainer.addEventListener("mouseleave", () => {
            audioBtn.style.opacity = "0";
            if (index !== this.focusedIndex) {
                tileContainer.style.borderColor = "transparent";
            }
        });
        const player = new PrismPlayer(playerContainer, { condensed: true, muteAudio: true });
        tileContainer.addEventListener("click", (e) => {
            e.stopPropagation();
            this.setFocus(index);
            if (this.expandedIndex === null) {
                this.expandTile(index);
            }
        });
        const tile = {
            container: tileContainer,
            playerContainer,
            player,
            label,
            description: desc,
            descriptionText: "",
            audioIndicator,
            audioBtn,
            tileNumber,
            streamNameLabel,
            tileStatus,
            tileStatusText,
            scte35Badge,
            scte35ToastArea,
            ccBadge,
            tcDisplay,
            streamKey: null,
            index,
        };
        this.tiles.push(tile);
        this.grid.appendChild(tileContainer);
    }
    // ── Expand / Collapse ────────────────────────────────────
    expandTile(index) {
        const tile = this.tiles[index];
        if (!tile || !tile.streamKey)
            return;
        this.expandedIndex = index;
        this.preExpandSoloIndex = this.soloIndex;
        // Silence all multiview audio before the expanded player starts.
        // Clear soloIndex so restore doesn't trigger the toggle-off path.
        this.soloIndex = null;
        for (const t of this.tiles) {
            t.player.setGlobalMute(true);
            t.player.setDecodersSuspended(true);
            this.updateAudioIndicator(t);
        }
        if (this.sharedAudioContext && this.sharedAudioContext.state === "running") {
            this.sharedAudioContext.suspend();
        }
        this.expandOverlay = document.createElement("div");
        this.expandOverlay.style.position = "fixed";
        this.expandOverlay.style.top = "0";
        this.expandOverlay.style.left = "0";
        this.expandOverlay.style.right = "0";
        this.expandOverlay.style.bottom = "0";
        this.expandOverlay.style.background = "#000";
        this.expandOverlay.style.zIndex = "1000";
        this.expandOverlay.style.display = "flex";
        this.expandOverlay.style.flexDirection = "column";
        this.expandOverlay.style.alignItems = "center";
        this.expandOverlay.style.justifyContent = "center";
        this.expandOverlay.style.padding = "40px 20px 20px";
        this.expandOverlay.style.opacity = "0";
        this.expandOverlay.style.transform = "scale(0.97)";
        this.expandOverlay.style.transition = "opacity 0.2s ease, transform 0.2s ease";
        const topBar = document.createElement("div");
        topBar.style.position = "absolute";
        topBar.style.top = "0";
        topBar.style.left = "0";
        topBar.style.right = "0";
        topBar.style.height = "36px";
        topBar.style.display = "flex";
        topBar.style.justifyContent = "space-between";
        topBar.style.alignItems = "center";
        topBar.style.padding = "0 12px";
        topBar.style.background = "#0a0a0a";
        topBar.style.borderBottom = "1px solid #1a1a1a";
        topBar.style.zIndex = "1001";
        const titleGroup = document.createElement("div");
        titleGroup.style.display = "flex";
        titleGroup.style.alignItems = "center";
        titleGroup.style.gap = "10px";
        const title = document.createElement("span");
        title.dataset.expandTitle = "";
        title.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        title.style.fontSize = "12px";
        title.style.fontWeight = "700";
        title.style.color = "#e2e8f0";
        title.style.textTransform = "uppercase";
        title.style.letterSpacing = "0.5px";
        title.textContent = tile.streamKey ?? "";
        titleGroup.appendChild(title);
        const meta = document.createElement("span");
        meta.dataset.expandDesc = "";
        meta.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        meta.style.fontSize = "10px";
        meta.style.fontWeight = "500";
        meta.style.color = "#64748b";
        meta.textContent = tile.descriptionText;
        meta.style.display = tile.descriptionText ? "" : "none";
        titleGroup.appendChild(meta);
        topBar.appendChild(titleGroup);
        const gridIndicator = this.buildGridIndicator(index);
        gridIndicator.style.position = "absolute";
        gridIndicator.style.left = "50%";
        gridIndicator.style.top = "50%";
        gridIndicator.style.transform = "translate(-50%, -50%)";
        topBar.appendChild(gridIndicator);
        const closeBtn = document.createElement("button");
        closeBtn.textContent = "\u2715 ESC";
        closeBtn.style.background = "rgba(255, 255, 255, 0.06)";
        closeBtn.style.border = "1px solid #333";
        closeBtn.style.color = "#94a3b8";
        closeBtn.style.padding = "4px 12px";
        closeBtn.style.borderRadius = "3px";
        closeBtn.style.cursor = "pointer";
        closeBtn.style.fontSize = "11px";
        closeBtn.style.fontFamily = "'SF Mono', monospace";
        closeBtn.style.fontWeight = "600";
        closeBtn.style.letterSpacing = "0.5px";
        closeBtn.style.transition = "background 0.15s ease, color 0.15s ease, border-color 0.15s ease";
        closeBtn.addEventListener("click", (e) => {
            e.stopPropagation();
            this.collapseExpanded();
        });
        closeBtn.addEventListener("mouseenter", () => {
            closeBtn.style.background = "rgba(239, 68, 68, 0.15)";
            closeBtn.style.color = "#ef4444";
            closeBtn.style.borderColor = "rgba(239, 68, 68, 0.3)";
        });
        closeBtn.addEventListener("mouseleave", () => {
            closeBtn.style.background = "rgba(255, 255, 255, 0.06)";
            closeBtn.style.color = "#94a3b8";
            closeBtn.style.borderColor = "#333";
        });
        topBar.appendChild(closeBtn);
        this.expandOverlay.appendChild(topBar);
        const expandedPlayerContainer = document.createElement("div");
        expandedPlayerContainer.dataset.expandPlayer = "";
        expandedPlayerContainer.style.maxWidth = "1280px";
        expandedPlayerContainer.style.width = "100%";
        expandedPlayerContainer.style.borderRadius = "3px";
        expandedPlayerContainer.style.overflow = "hidden";
        this.expandOverlay.appendChild(expandedPlayerContainer);
        const expandedPlayer = new PrismPlayer(expandedPlayerContainer, { condensed: false });
        expandedPlayer.connect(tile.streamKey);
        this.expandOverlay._player = expandedPlayer;
        document.body.appendChild(this.expandOverlay);
        requestAnimationFrame(() => {
            if (this.expandOverlay) {
                this.expandOverlay.style.opacity = "1";
                this.expandOverlay.style.transform = "scale(1)";
            }
        });
    }
    collapseExpanded() {
        if (this.expandOverlay) {
            const overlay = this.expandOverlay;
            const player = overlay._player;
            overlay.style.opacity = "0";
            overlay.style.transform = "scale(0.97)";
            overlay.addEventListener("transitionend", () => {
                if (player)
                    player.destroy();
                overlay.remove();
            }, { once: true });
            this.expandOverlay = null;
        }
        // Unsuspend all decoders so they can resume if needed
        for (const t of this.tiles) {
            t.player.setDecodersSuspended(false);
        }
        // Restore multiview audio state
        if (this.sharedAudioContext && this.sharedAudioContext.state === "suspended") {
            this.sharedAudioContext.resume();
        }
        if (this.preExpandSoloIndex !== null) {
            this.soloAudio(this.preExpandSoloIndex);
        }
        else {
            this.unsoloAudio();
        }
        this.preExpandSoloIndex = null;
        this.expandedIndex = null;
    }
    /** Switch the expanded view to a different tile without collapsing back to the grid. */
    switchExpandedTile(newIndex) {
        if (!this.expandOverlay)
            return;
        const tile = this.tiles[newIndex];
        if (!tile?.streamKey)
            return;
        // Destroy the old player.
        const oldPlayer = this.expandOverlay._player;
        if (oldPlayer)
            oldPlayer.destroy();
        // Update title text.
        const titleEl = this.expandOverlay.querySelector("[data-expand-title]");
        if (titleEl)
            titleEl.textContent = tile.streamKey;
        // Update description.
        const descEl = this.expandOverlay.querySelector("[data-expand-desc]");
        if (descEl) {
            descEl.textContent = tile.descriptionText;
            descEl.style.display = tile.descriptionText ? "" : "none";
        }
        // Update grid indicator.
        this.updateGridIndicator(this.expandOverlay, newIndex);
        // Remove old player container and create a fresh one.
        const oldContainer = this.expandOverlay.querySelector("[data-expand-player]");
        if (oldContainer)
            oldContainer.remove();
        const playerContainer = document.createElement("div");
        playerContainer.dataset.expandPlayer = "";
        playerContainer.style.maxWidth = "1280px";
        playerContainer.style.width = "100%";
        playerContainer.style.borderRadius = "3px";
        playerContainer.style.overflow = "hidden";
        this.expandOverlay.appendChild(playerContainer);
        const newPlayer = new PrismPlayer(playerContainer, { condensed: false });
        newPlayer.connect(tile.streamKey);
        this.expandOverlay._player = newPlayer;
        this.expandedIndex = newIndex;
        this.setFocus(newIndex);
    }
    /** Build a tiny 3x3 grid indicator showing which tile is active. */
    buildGridIndicator(activeIndex) {
        const container = document.createElement("div");
        container.dataset.gridIndicator = "";
        container.style.display = "grid";
        container.style.gridTemplateColumns = "repeat(3, 6px)";
        container.style.gridTemplateRows = "repeat(3, 6px)";
        container.style.gap = "2px";
        container.style.opacity = "0.7";
        for (let i = 0; i < 9; i++) {
            const dot = document.createElement("div");
            dot.style.width = "6px";
            dot.style.height = "6px";
            dot.style.borderRadius = "1px";
            const hasTile = !!this.tiles[i]?.streamKey;
            if (i === activeIndex) {
                dot.style.background = "#3b82f6";
            }
            else if (hasTile) {
                dot.style.background = "rgba(255, 255, 255, 0.15)";
            }
            else {
                dot.style.background = "rgba(255, 255, 255, 0.05)";
            }
            container.appendChild(dot);
        }
        return container;
    }
    /** Update the grid indicator dots inside an expanded overlay. */
    updateGridIndicator(overlay, activeIndex) {
        const container = overlay.querySelector("[data-grid-indicator]");
        if (!container)
            return;
        const dots = container.children;
        for (let i = 0; i < dots.length; i++) {
            const dot = dots[i];
            const hasTile = !!this.tiles[i]?.streamKey;
            if (i === activeIndex) {
                dot.style.background = "#3b82f6";
            }
            else if (hasTile) {
                dot.style.background = "rgba(255, 255, 255, 0.15)";
            }
            else {
                dot.style.background = "rgba(255, 255, 255, 0.05)";
            }
        }
    }
}
