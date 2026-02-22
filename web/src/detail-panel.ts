import type { MetricsStore } from "./metrics-store";
import type { BadgeKey } from "./hud";
import type { ServerSCTE35Event } from "./transport";

export function renderSparkline(
	canvas: HTMLCanvasElement,
	data: number[],
	color: string,
	fillAlpha = 0.15,
): void {
	const ctx = canvas.getContext("2d");
	if (!ctx || data.length < 2) return;

	const dpr = window.devicePixelRatio || 1;
	const w = canvas.clientWidth;
	const h = canvas.clientHeight;
	if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
		canvas.width = w * dpr;
		canvas.height = h * dpr;
		ctx.scale(dpr, dpr);
	}

	ctx.clearRect(0, 0, w, h);

	let min = Infinity;
	let max = -Infinity;
	for (const v of data) {
		if (v < min) min = v;
		if (v > max) max = v;
	}
	const range = max - min || 1;
	const pad = 2;

	const xStep = (w - pad * 2) / (data.length - 1);

	ctx.beginPath();
	for (let i = 0; i < data.length; i++) {
		const x = pad + i * xStep;
		const y = h - pad - ((data[i] - min) / range) * (h - pad * 2);
		if (i === 0) ctx.moveTo(x, y);
		else ctx.lineTo(x, y);
	}
	ctx.strokeStyle = color;
	ctx.lineWidth = 1.5;
	ctx.lineJoin = "round";
	ctx.stroke();

	ctx.lineTo(pad + (data.length - 1) * xStep, h - pad);
	ctx.lineTo(pad, h - pad);
	ctx.closePath();

	const r = parseInt(color.slice(1, 3), 16);
	const g = parseInt(color.slice(3, 5), 16);
	const b = parseInt(color.slice(5, 7), 16);
	ctx.fillStyle = `rgba(${r},${g},${b},${fillAlpha})`;
	ctx.fill();
}

function createSparklineCanvas(): HTMLCanvasElement {
	const c = document.createElement("canvas");
	c.style.width = "100%";
	c.style.height = "40px";
	c.style.display = "block";
	c.style.borderRadius = "4px";
	c.style.background = "rgba(0,0,0,0.3)";
	c.style.marginTop = "4px";
	return c;
}

interface StatRow {
	el: HTMLElement;
	valueEl: HTMLElement;
}

export class DetailPanel {
	private overlay: HTMLElement;
	private titleEl: HTMLElement;
	private body: HTMLElement;
	private closeBtn: HTMLElement;
	private animId: number | null = null;
	private rows: Map<string, StatRow> = new Map();
	private sparklines: Map<string, HTMLCanvasElement> = new Map();
	private onClose: (() => void) | null = null;
	private renderFn: (() => void) | null = null;

	constructor(container: HTMLElement, _key: BadgeKey, _store: MetricsStore, zIndex: number) {

		this.overlay = document.createElement("div");
		this.overlay.style.position = "absolute";
		this.overlay.style.top = "0";
		this.overlay.style.left = "0";
		this.overlay.style.bottom = "0";
		this.overlay.style.width = "40%";
		this.overlay.style.minWidth = "280px";
		this.overlay.style.maxWidth = "400px";
		this.overlay.style.background = "rgba(15,23,42,0.92)";
		this.overlay.style.backdropFilter = "blur(8px)";
		this.overlay.style.borderRight = "1px solid rgba(255,255,255,0.08)";
		this.overlay.style.zIndex = String(zIndex);
		this.overlay.style.display = "flex";
		this.overlay.style.flexDirection = "column";
		this.overlay.style.fontFamily = "'SF Mono', 'Menlo', 'Monaco', monospace";
		this.overlay.style.color = "#e2e8f0";
		this.overlay.style.fontSize = "12px";
		this.overlay.style.overflow = "hidden";
		this.overlay.style.transform = "translateX(-100%)";
		this.overlay.style.transition = "transform 0.25s cubic-bezier(0.16,1,0.3,1)";
		this.overlay.style.pointerEvents = "auto";

		const header = document.createElement("div");
		header.style.display = "flex";
		header.style.alignItems = "center";
		header.style.justifyContent = "space-between";
		header.style.padding = "12px 14px 8px";
		header.style.borderBottom = "1px solid rgba(255,255,255,0.06)";

		this.titleEl = document.createElement("div");
		this.titleEl.style.fontWeight = "700";
		this.titleEl.style.fontSize = "13px";
		this.titleEl.style.textTransform = "uppercase";
		this.titleEl.style.letterSpacing = "0.06em";
		header.appendChild(this.titleEl);

		this.closeBtn = document.createElement("div");
		this.closeBtn.textContent = "✕";
		this.closeBtn.style.cursor = "pointer";
		this.closeBtn.style.opacity = "0.5";
		this.closeBtn.style.fontSize = "14px";
		this.closeBtn.style.padding = "2px 6px";
		this.closeBtn.addEventListener("mouseenter", () => {
			this.closeBtn.style.opacity = "1";
		});
		this.closeBtn.addEventListener("mouseleave", () => {
			this.closeBtn.style.opacity = "0.5";
		});
		this.closeBtn.addEventListener("click", (e) => {
			e.stopPropagation();
			if (this.onClose) this.onClose();
		});
		header.appendChild(this.closeBtn);

		this.overlay.appendChild(header);

		this.body = document.createElement("div");
		this.body.style.flex = "1";
		this.body.style.overflow = "auto";
		this.body.style.padding = "10px 14px";
		this.overlay.appendChild(this.body);

		container.appendChild(this.overlay);

		requestAnimationFrame(() => {
			this.overlay.style.transform = "translateX(0)";
		});
	}

	setTitle(title: string): void {
		this.titleEl.textContent = title;
	}

	setOnClose(cb: () => void): void {
		this.onClose = cb;
	}

	setRenderFn(fn: () => void): void {
		this.renderFn = fn;
	}

	addSection(label: string): HTMLElement {
		const section = document.createElement("div");
		section.style.marginBottom = "12px";

		const title = document.createElement("div");
		title.textContent = label;
		title.style.fontWeight = "600";
		title.style.fontSize = "10px";
		title.style.textTransform = "uppercase";
		title.style.letterSpacing = "0.08em";
		title.style.opacity = "0.5";
		title.style.marginBottom = "6px";
		section.appendChild(title);

		this.body.appendChild(section);
		return section;
	}

	addStatRow(section: HTMLElement, id: string, label: string, initialValue = "—"): void {
		const row = document.createElement("div");
		row.style.display = "flex";
		row.style.justifyContent = "space-between";
		row.style.padding = "2px 0";

		const labelEl = document.createElement("span");
		labelEl.textContent = label;
		labelEl.style.opacity = "0.7";
		row.appendChild(labelEl);

		const valueEl = document.createElement("span");
		valueEl.textContent = initialValue;
		valueEl.style.fontWeight = "500";
		row.appendChild(valueEl);

		section.appendChild(row);
		this.rows.set(id, { el: row, valueEl });
	}

	addSparkline(section: HTMLElement, id: string): HTMLCanvasElement {
		const canvas = createSparklineCanvas();
		section.appendChild(canvas);
		this.sparklines.set(id, canvas);
		return canvas;
	}

	updateStat(id: string, value: string): void {
		const row = this.rows.get(id);
		if (row) row.valueEl.textContent = value;
	}

	getSparkline(id: string): HTMLCanvasElement | undefined {
		return this.sparklines.get(id);
	}

	start(): void {
		if (this.animId !== null) return;
		const tick = () => {
			this.animId = requestAnimationFrame(tick);
			if (this.renderFn) this.renderFn();
		};
		tick();
	}

	stop(): void {
		if (this.animId !== null) {
			cancelAnimationFrame(this.animId);
			this.animId = null;
		}
	}

	destroy(): void {
		this.stop();
		this.overlay.style.transform = "translateX(-100%)";
		setTimeout(() => {
			this.overlay.remove();
		}, 300);
	}
}

export function buildVideoPanel(container: HTMLElement, store: MetricsStore, zIndex: number): DetailPanel {
	const panel = new DetailPanel(container, "video", store, zIndex);
	panel.setTitle("Video");

	const codec = panel.addSection("Codec");
	panel.addStatRow(codec, "v-codec", "Codec");
	panel.addStatRow(codec, "v-res", "Resolution");

	const perf = panel.addSection("Performance");
	panel.addStatRow(perf, "v-fps-server", "Server FPS");
	panel.addStatRow(perf, "v-fps-decode", "Decode FPS");
	panel.addStatRow(perf, "v-fps-render", "Render FPS");
	panel.addSparkline(perf, "v-fps-spark");

	const bitrate = panel.addSection("Bitrate");
	panel.addStatRow(bitrate, "v-bitrate", "Bitrate");
	panel.addSparkline(bitrate, "v-bitrate-spark");

	const gop = panel.addSection("GOP Structure");
	panel.addStatRow(gop, "v-gop-len", "Current GOP");
	panel.addStatRow(gop, "v-keyframes", "Keyframes");
	panel.addStatRow(gop, "v-delta", "Delta Frames");
	panel.addStatRow(gop, "v-total", "Total Frames");

	const health = panel.addSection("Health");
	panel.addStatRow(health, "v-pts-err", "PTS Errors");
	panel.addStatRow(health, "v-queue", "Decode Queue");
	panel.addStatRow(health, "v-dropped", "Client Drops");

	panel.setRenderFn(() => {
		const v = store.getVideoMetrics();
		panel.updateStat("v-codec", v.codec);
		panel.updateStat("v-res", v.width > 0 ? `${v.width}×${v.height}` : "—");
		panel.updateStat("v-fps-server", v.serverFrameRate > 0 ? v.serverFrameRate.toFixed(1) : "—");
		panel.updateStat("v-fps-decode", String(v.decodeFps));
		panel.updateStat("v-fps-render", String(v.renderFps));
		panel.updateStat("v-bitrate", v.serverBitrateKbps > 0 ? `${v.serverBitrateKbps.toFixed(0)} Kbps` : "—");
		panel.updateStat("v-gop-len", `${v.currentGOPLen} frames`);
		panel.updateStat("v-keyframes", String(v.keyFrames));
		panel.updateStat("v-delta", String(v.deltaFrames));
		panel.updateStat("v-total", String(v.totalFrames));
		panel.updateStat("v-pts-err", String(v.ptsErrors));
		panel.updateStat("v-queue", `${v.decodeQueueDepth} (${v.decodeQueueMs.toFixed(0)}ms)`);
		panel.updateStat("v-dropped", String(v.clientDropped));

		const fpsSpark = panel.getSparkline("v-fps-spark");
		if (fpsSpark && v.fpsHistory.length > 1) {
			renderSparkline(fpsSpark, v.fpsHistory, "#22c55e");
		}
		const brSpark = panel.getSparkline("v-bitrate-spark");
		if (brSpark && v.bitrateHistory.length > 1) {
			renderSparkline(brSpark, v.bitrateHistory, "#3b82f6");
		}
	});

	return panel;
}

export function buildAudioPanel(container: HTMLElement, store: MetricsStore, zIndex: number): DetailPanel {
	const panel = new DetailPanel(container, "audio", store, zIndex);
	panel.setTitle("Audio");

	const overview = panel.addSection("Overview");
	panel.addStatRow(overview, "a-tracks", "Tracks");
	panel.addStatRow(overview, "a-buf", "Buffer");
	panel.addStatRow(overview, "a-silence", "Silence Inserted");

	const perTrack = panel.addSection("Per-Track");
	panel.addStatRow(perTrack, "a-detail", "");

	panel.setRenderFn(() => {
		const a = store.getAudioMetrics();
		panel.updateStat("a-tracks", String(a.tracks.length));
		panel.updateStat("a-buf", `${a.bufferMs.toFixed(0)}ms`);
		panel.updateStat("a-silence", `${a.silenceMs.toFixed(0)}ms`);

		if (a.tracks.length > 0) {
			const lines = a.tracks.slice(0, 8).map((t) => {
				return `#${t.trackIndex}: ${t.codec} ${t.sampleRate / 1000}kHz ${t.channels}ch ${t.bitrateKbps.toFixed(0)}kbps`;
			});
			if (a.tracks.length > 8) lines.push(`... +${a.tracks.length - 8} more`);
			panel.updateStat("a-detail", lines.join("\n"));
		}
	});

	return panel;
}

export function buildSyncPanel(container: HTMLElement, store: MetricsStore, zIndex: number): DetailPanel {
	const panel = new DetailPanel(container, "sync", store, zIndex);
	panel.setTitle("A/V Sync");

	const current = panel.addSection("Current");
	panel.addStatRow(current, "s-offset", "A/V Offset");
	panel.addStatRow(current, "s-drift", "Drift Rate");
	panel.addStatRow(current, "s-corrections", "Corrections");
	panel.addSparkline(current, "s-offset-spark");

	panel.setRenderFn(() => {
		const s = store.getSyncMetrics();
		const sign = s.offsetMs >= 0 ? "+" : "";
		panel.updateStat("s-offset", `${sign}${s.offsetMs.toFixed(1)}ms`);
		panel.updateStat("s-drift", `${s.driftRateMsPerSec.toFixed(2)} ms/s`);
		panel.updateStat("s-corrections", String(s.corrections));

		const spark = panel.getSparkline("s-offset-spark");
		if (spark && s.offsetHistory.length > 1) {
			renderSparkline(spark, s.offsetHistory, "#a855f7");
		}
	});

	return panel;
}

export function buildTransportPanel(container: HTMLElement, store: MetricsStore, zIndex: number): DetailPanel {
	const panel = new DetailPanel(container, "buffer", store, zIndex);
	panel.setTitle("Transport");

	const conn = panel.addSection("Connection");
	panel.addStatRow(conn, "t-proto", "Protocol");
	panel.addStatRow(conn, "t-uptime", "Uptime");
	panel.addStatRow(conn, "t-viewers", "Viewers");

	const bw = panel.addSection("Bandwidth");
	panel.addStatRow(bw, "t-rx-kbps", "Receive Bitrate");

	panel.setRenderFn(() => {
		const t = store.getTransportMetrics();
		panel.updateStat("t-proto", t.protocol);
		const sec = Math.floor(t.uptimeMs / 1000);
		const min = Math.floor(sec / 60);
		const hrs = Math.floor(min / 60);
		panel.updateStat("t-uptime", hrs > 0
			? `${hrs}h ${min % 60}m ${sec % 60}s`
			: min > 0
				? `${min}m ${sec % 60}s`
				: `${sec}s`);
		panel.updateStat("t-viewers", String(t.viewerCount));
		panel.updateStat("t-rx-kbps", `${t.receiveBitrateKbps.toFixed(0)} Kbps`);
	});

	return panel;
}

export function buildCaptionsPanel(container: HTMLElement, store: MetricsStore, zIndex: number): DetailPanel {
	const panel = new DetailPanel(container, "viewers", store, zIndex);
	panel.setTitle("Captions");

	const info = panel.addSection("Info");
	panel.addStatRow(info, "c-channels", "Active Channels");
	panel.addStatRow(info, "c-frames", "Total Frames");

	panel.setRenderFn(() => {
		const c = store.getCaptionMetrics();
		panel.updateStat("c-channels", c.activeChannels.length > 0 ? c.activeChannels.join(", ") : "None");
		panel.updateStat("c-frames", String(c.totalFrames));
	});

	return panel;
}

export function buildSCTE35Panel(
	container: HTMLElement,
	store: MetricsStore,
	zIndex: number,
	event: ServerSCTE35Event,
): DetailPanel {
	const panel = new DetailPanel(container, "video", store, zIndex);
	panel.setTitle("SCTE-35 Event");

	const cmd = panel.addSection("Command");
	panel.addStatRow(cmd, "sc-type", "Command Type");
	panel.addStatRow(cmd, "sc-type-id", "Command Type ID");

	if (event.eventId) {
		const eid = panel.addSection("Event");
		panel.addStatRow(eid, "sc-event-id", "Event ID");
	}

	if (event.segmentationType) {
		const seg = panel.addSection("Segmentation");
		panel.addStatRow(seg, "sc-seg-type", "Type");
		panel.addStatRow(seg, "sc-seg-id", "Type ID");
	}

	const details = panel.addSection("Details");
	panel.addStatRow(details, "sc-desc", "Description");
	if (event.duration && event.duration > 0) {
		panel.addStatRow(details, "sc-duration", "Duration");
	}
	if (event.commandType === "splice_insert") {
		panel.addStatRow(details, "sc-oon", "Out of Network");
		panel.addStatRow(details, "sc-imm", "Immediate");
	}
	panel.addStatRow(details, "sc-pts", "PTS");
	panel.addStatRow(details, "sc-time", "Received");

	const summary = panel.addSection("Stream Totals");
	panel.addStatRow(summary, "sc-total", "Total SCTE-35 Events");
	panel.addStatRow(summary, "sc-recent", "Recent Events");

	panel.setRenderFn(() => {
		panel.updateStat("sc-type", event.commandType);
		panel.updateStat("sc-type-id", `0x${event.commandTypeId.toString(16).padStart(2, "0")}`);

		if (event.eventId) {
			panel.updateStat("sc-event-id", `0x${event.eventId.toString(16)} (${event.eventId})`);
		}

		if (event.segmentationType) {
			panel.updateStat("sc-seg-type", event.segmentationType);
			panel.updateStat("sc-seg-id", `0x${(event.segmentationTypeId ?? 0).toString(16).padStart(2, "0")}`);
		}

		panel.updateStat("sc-desc", event.description);
		if (event.duration && event.duration > 0) {
			const mins = Math.floor(event.duration / 60);
			const secs = (event.duration % 60).toFixed(1);
			panel.updateStat("sc-duration", mins > 0 ? `${mins}m ${secs}s` : `${secs}s`);
		}
		if (event.commandType === "splice_insert") {
			panel.updateStat("sc-oon", event.outOfNetwork ? "Yes" : "No");
			panel.updateStat("sc-imm", event.immediate ? "Yes" : "No");
		}

		panel.updateStat("sc-pts", event.pts > 0 ? String(event.pts) : "—");

		const d = new Date(event.receivedAt);
		panel.updateStat("sc-time", d.toLocaleTimeString());

		panel.updateStat("sc-total", String(store.getSCTE35Total()));
		const recent = store.getSCTE35Events();
		panel.updateStat("sc-recent", String(recent.length));
	});

	return panel;
}
