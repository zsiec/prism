/**
 * Toggleable heads-up performance display for a single stream. Polls
 * diagnostics once per second and renders a detailed HTML table covering
 * video rendering, A/V sync, decoder stats, and transport metrics.
 * Supports exporting a full diagnostic snapshot (with server-side debug
 * data) to the clipboard via Ctrl+Shift+S.
 */
export class PerfOverlay {
    el;
    visible = false;
    intervalId = null;
    getDiag;
    snapshotHistory = [];
    streamKey = null;
    _keydownHandler;
    constructor(container, getDiag) {
        this.getDiag = getDiag;
        this.el = document.createElement("div");
        Object.assign(this.el.style, {
            position: "absolute",
            top: "8px",
            right: "8px",
            background: "rgba(0,0,0,0.85)",
            color: "#e0e0e0",
            padding: "10px 14px",
            borderRadius: "6px",
            fontFamily: "'SF Mono', 'Monaco', 'Consolas', monospace",
            fontSize: "11px",
            lineHeight: "1.5",
            zIndex: "9999",
            pointerEvents: "auto",
            maxHeight: "90%",
            overflowY: "auto",
            minWidth: "340px",
            display: "none",
            border: "1px solid rgba(255,255,255,0.1)",
        });
        container.appendChild(this.el);
        this._keydownHandler = (e) => {
            if (e.key === "p" || e.key === "P") {
                if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement)
                    return;
                this.toggle();
            }
            if ((e.key === "s" || e.key === "S") && e.ctrlKey && e.shiftKey) {
                e.preventDefault();
                this.exportSnapshot();
            }
        };
        document.addEventListener("keydown", this._keydownHandler);
    }
    toggle() {
        this.visible = !this.visible;
        this.el.style.display = this.visible ? "block" : "none";
        if (this.visible) {
            this.startPolling();
        }
        else {
            this.stopPolling();
        }
    }
    startPolling() {
        this.stopPolling();
        this.poll();
        this.intervalId = setInterval(() => this.poll(), 1000);
    }
    stopPolling() {
        if (this.intervalId !== null) {
            clearInterval(this.intervalId);
            this.intervalId = null;
        }
    }
    async poll() {
        const snap = await this.getDiag();
        if (!snap)
            return;
        this.snapshotHistory.push(snap);
        if (this.snapshotHistory.length > 60)
            this.snapshotHistory.shift();
        this.render(snap);
    }
    setStreamKey(key) {
        this.streamKey = key;
    }
    async fetchServerDebug() {
        if (!this.streamKey)
            return null;
        try {
            const resp = await fetch(`/api/streams/${encodeURIComponent(this.streamKey)}/debug`);
            if (!resp.ok)
                return null;
            return await resp.json();
        }
        catch {
            return null;
        }
    }
    async exportSnapshot() {
        const snap = await this.getDiag();
        if (!snap)
            return;
        const serverDebug = await this.fetchServerDebug();
        const output = {
            snapshot: { ...snap, serverDebug },
            recentHistory: this.snapshotHistory.slice(-10),
        };
        const json = JSON.stringify(output, null, 2);
        navigator.clipboard.writeText(json).then(() => {
            this.flashMessage("Snapshot copied to clipboard");
        }).catch(() => {
            console.log("=== PRISM PERF SNAPSHOT ===");
            console.log(json);
            this.flashMessage("Snapshot logged to console");
        });
    }
    flashMessage(msg) {
        const badge = document.createElement("div");
        Object.assign(badge.style, {
            position: "fixed",
            bottom: "20px",
            left: "50%",
            transform: "translateX(-50%)",
            background: "rgba(0,200,100,0.9)",
            color: "#fff",
            padding: "8px 20px",
            borderRadius: "6px",
            fontFamily: "'SF Mono', monospace",
            fontSize: "13px",
            zIndex: "99999",
            transition: "opacity 0.5s",
        });
        badge.textContent = msg;
        document.body.appendChild(badge);
        setTimeout(() => { badge.style.opacity = "0"; }, 1500);
        setTimeout(() => badge.remove(), 2000);
    }
    render(s) {
        const f = (v, d = 1) => v.toFixed(d);
        const sec = (ms) => (ms / 1000).toFixed(1) + "s";
        const warn = (val, threshold) => val > threshold ? ' style="color:#ff6b6b"' : "";
        const good = (val, threshold) => val < threshold ? ' style="color:#66ff66"' : "";
        const r = s.renderer;
        const v = s.videoDecoder;
        const a = s.audio;
        const t = s.transport;
        const html = `
<div style="color:#8be9fd;font-weight:bold;margin-bottom:6px;font-size:12px">
  PRISM Perf &mdash; ${sec(s.uptimeMs)}
  <span style="float:right;color:#888;font-weight:normal;font-size:10px">P=toggle Ctrl+Shift+S=export</span>
</div>

<div style="color:#ffb86c;font-weight:bold;margin:8px 0 2px">VIDEO RENDER</div>
<table style="width:100%;border-collapse:collapse;font-size:11px">
<tr><td>rAF calls</td><td align="right">${r.rafCount}</td></tr>
<tr><td>Frames drawn</td><td align="right">${r.framesDrawn}</td></tr>
<tr><td>Frames skipped (no new)</td><td align="right"${warn(r.framesSkipped, 10)}>${r.framesSkipped}</td></tr>
<tr><td>Empty buffer hits</td><td align="right"${warn(r.emptyBufferHits, 5)}>${r.emptyBufferHits}</td></tr>
<tr><td>rAF interval</td><td align="right">${f(r.avgRafIntervalMs)}ms avg / ${f(r.minRafIntervalMs)}–${f(r.maxRafIntervalMs)}ms</td></tr>
<tr><td>Frame interval</td><td align="right">${f(r.avgFrameIntervalMs)}ms avg / ${f(r.minFrameIntervalMs)}–${f(r.maxFrameIntervalMs)}ms</td></tr>
<tr><td>Draw time</td><td align="right">${f(r.avgDrawMs, 2)}ms avg / ${f(r.maxDrawMs, 2)}ms max</td></tr>
<tr><td>Clock mode</td><td align="right">${r.clockMode}</td></tr>
<tr><td>Video queue</td><td align="right">${r.videoQueueSize} frames (${f(r.videoQueueMs, 0)}ms)</td></tr>
<tr><td>Video discarded</td><td align="right"${warn(r.videoTotalDiscarded, 20)}>${r.videoTotalDiscarded}</td></tr>
</table>

<div style="color:#ff79c6;font-weight:bold;margin:8px 0 2px">A/V SYNC</div>
<table style="width:100%;border-collapse:collapse;font-size:11px">
<tr><td>Current</td><td align="right"${warn(Math.abs(r.avSyncMs), 40)}>${f(r.avSyncMs, 0)}ms</td></tr>
<tr><td>Range</td><td align="right">${f(r.avSyncMin, 0)}ms – ${f(r.avSyncMax, 0)}ms</td></tr>
<tr><td>Average</td><td align="right">${f(r.avSyncAvg, 1)}ms</td></tr>
<tr><td>Video PTS</td><td align="right">${r.currentVideoPTS >= 0 ? f(r.currentVideoPTS / 1000, 0) + "ms" : "n/a"}</td></tr>
<tr><td>Audio PTS</td><td align="right">${r.currentAudioPTS >= 0 ? f(r.currentAudioPTS / 1000, 0) + "ms" : "n/a"}</td></tr>
</table>

<div style="color:#50fa7b;font-weight:bold;margin:8px 0 2px">VIDEO DECODER (worker)</div>
<table style="width:100%;border-collapse:collapse;font-size:11px">
<tr><td>Input / Output</td><td align="right">${v.inputCount} / ${v.outputCount}</td></tr>
<tr><td>Keyframes</td><td align="right">${v.keyframeCount}</td></tr>
<tr><td>Decode queue</td><td align="right">${v.decodeQueueSize}</td></tr>
<tr><td>Input FPS</td><td align="right"${good(v.inputFps, 30)}>${f(v.inputFps)}</td></tr>
<tr><td>Output FPS</td><td align="right"${good(v.outputFps, 30)}>${f(v.outputFps)}</td></tr>
<tr><td>Input interval</td><td align="right">${f(v.avgInputIntervalMs)}ms avg / ${f(v.minInputIntervalMs)}–${f(v.maxInputIntervalMs)}ms</td></tr>
<tr><td>Output interval</td><td align="right">${f(v.avgOutputIntervalMs)}ms avg / ${f(v.minOutputIntervalMs)}–${f(v.maxOutputIntervalMs)}ms</td></tr>
<tr><td>Decode errors</td><td align="right"${warn(v.decodeErrors, 0)}>${v.decodeErrors}</td></tr>
<tr><td>PTS jumps</td><td align="right"${warn(v.ptsJumps, 5)}>${v.ptsJumps}</td></tr>
<tr><td>Discarded (delta)</td><td align="right">${v.discardedDelta}</td></tr>
<tr><td>Discarded (buf full)</td><td align="right"${warn(v.discardedBufferFull, 0)}>${v.discardedBufferFull}</td></tr>
<tr><td>Buffer dropped</td><td align="right"${warn(v.bufferDropped, 0)}>${v.bufferDropped}</td></tr>
</table>

<div style="color:#bd93f9;font-weight:bold;margin:8px 0 2px">AUDIO DECODER</div>
<table style="width:100%;border-collapse:collapse;font-size:11px">
<tr><td>State</td><td align="right">${a.contextState} (${a.isPlaying ? "playing" : "buffering"})</td></tr>
<tr><td>Callbacks</td><td align="right">${a.callbackCount} (${f(a.callbacksPerSec, 0)}/s)</td></tr>
<tr><td>Callback interval</td><td align="right">${f(a.avgCallbackIntervalMs)}ms avg / ${f(a.minCallbackIntervalMs)}–${f(a.maxCallbackIntervalMs)}ms</td></tr>
<tr><td>Schedule ahead</td><td align="right"${warn(-a.scheduleAheadMs, 0)}>${f(a.scheduleAheadMs, 0)}ms</td></tr>
<tr><td>Last drift</td><td align="right"${warn(Math.abs(a.lastDriftMs), 30)}>${f(a.lastDriftMs, 1)}ms</td></tr>
<tr><td>Max drift</td><td align="right"${warn(a.maxDriftMs, 50)}>${f(a.maxDriftMs, 1)}ms</td></tr>
<tr><td>Gap repairs</td><td align="right"${warn(a.gapRepairs, 2)}>${a.gapRepairs}</td></tr>
<tr><td>Underruns</td><td align="right"${warn(a.underruns, 0)}>${a.underruns}</td></tr>
<tr><td>Total silence</td><td align="right"${warn(a.totalSilenceMs, 100)}>${f(a.totalSilenceMs, 0)}ms</td></tr>
<tr><td>Decode errors</td><td align="right"${warn(a.decodeErrors, 0)}>${a.decodeErrors}</td></tr>
<tr><td>PTS jumps (output)</td><td align="right"${warn(a.ptsJumps, 5)}>${a.ptsJumps}</td></tr>
<tr><td>PTS jumps (input)</td><td align="right">${a.inputPtsJumps ?? 0}</td></tr>
<tr><td>PTS wraps (input)</td><td align="right" style="color:#8be9fd">${a.inputPtsWraps ?? 0}</td></tr>
<tr><td>Last input PTS</td><td align="right">${a.lastInputPTS != null ? (a.lastInputPTS / 1000).toFixed(0) + "ms" : "n/a"}</td></tr>
<tr><td>Last output PTS</td><td align="right">${a.lastOutputPTS != null ? (a.lastOutputPTS / 1000).toFixed(0) + "ms" : "n/a"}</td></tr>
<tr><td>Pending frames</td><td align="right">${a.pendingFrames}</td></tr>
<tr><td>Sample rate</td><td align="right">${a.contextSampleRate} Hz</td></tr>
<tr><td>Base latency</td><td align="right">${f(a.contextBaseLatency * 1000, 1)}ms</td></tr>
<tr><td>Output latency</td><td align="right">${f(a.contextOutputLatency * 1000, 1)}ms</td></tr>
</table>

<div style="color:#f1fa8c;font-weight:bold;margin:8px 0 2px">TRANSPORT</div>
<table style="width:100%;border-collapse:collapse;font-size:11px">
<tr><td>Streams opened</td><td align="right">${t.streamsOpened}</td></tr>
<tr><td>Bytes received</td><td align="right">${(t.bytesReceived / 1024).toFixed(0)} KB</td></tr>
<tr><td>Video frames rx</td><td align="right">${t.videoFramesReceived}</td></tr>
<tr><td>Audio frames rx</td><td align="right">${t.audioFramesReceived}</td></tr>
<tr><td>Video arrival interval</td><td align="right">${f(t.avgVideoArrivalMs)}ms avg / ${f(t.maxVideoArrivalMs)}ms max</td></tr>
</table>
`;
        this.el.innerHTML = html;
    }
    destroy() {
        this.stopPolling();
        document.removeEventListener("keydown", this._keydownHandler);
        this.el.remove();
    }
}
