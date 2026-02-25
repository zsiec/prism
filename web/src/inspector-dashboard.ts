/**
 * Full dashboard overlay for the Stream Inspector.
 * Layout: Identity Banner → Pipeline Flow → 2×2 grid:
 *   VIDEO PIPELINE | SYNC & BUFFERS
 *   STRUCTURE & ERRORS | SIGNALS & METADATA
 */

import type { MetricsStore } from "./metrics-store";
import { TimeSeriesChart, GaugeBar, GOPChart, SCTE35Timeline, createChartLegend } from "./inspector-charts";

// ── Pipeline Flow (HTML/CSS, not canvas) ─────────────────────────

function createPipelineFlow(container: HTMLElement, store: MetricsStore): {
	el: HTMLElement;
	update: () => void;
	destroy: () => void;
} {
	const el = document.createElement("div");
	el.className = "pipeline-flow";

	const stages = [
		{ id: "ingest", label: "Ingest", getValue: () => formatBitrate(store.getVideoMetrics().serverBitrateKbps) },
		{ id: "demux", label: "Demux", getValue: () => {
			const fps = store.getVideoMetrics().serverFrameRate;
			return fps > 0 ? `${fps.toFixed(2)}fps` : "\u2014";
		}},
		{ id: "relay", label: "Relay", getValue: () => {
			const n = store.getTransportMetrics().viewerCount;
			return `${n} viewer${n !== 1 ? "s" : ""}`;
		}},
		{ id: "player", label: "Player", getValue: () => {
			const fps = store.getVideoMetrics().renderFps;
			return fps > 0 ? `${fps}fps` : "\u2014";
		}},
	];

	const valueEls: HTMLElement[] = [];
	const nodeEls: HTMLElement[] = [];

	stages.forEach((stage, i) => {
		if (i > 0) {
			const arrow = document.createElement("div");
			arrow.className = "pipeline-arrow";
			arrow.textContent = "\u2192";
			el.appendChild(arrow);
		}

		const node = document.createElement("div");
		node.className = "pipeline-node";
		node.dataset.health = "good";

		const label = document.createElement("div");
		label.className = "pipeline-label";
		label.textContent = stage.label;
		node.appendChild(label);

		const value = document.createElement("div");
		value.className = "pipeline-value";
		value.textContent = stage.getValue();
		node.appendChild(value);
		valueEls.push(value);
		nodeEls.push(node);

		el.appendChild(node);
	});

	container.appendChild(el);

	return {
		el,
		update: () => {
			stages.forEach((stage, i) => {
				valueEls[i].textContent = stage.getValue();
			});

			// Health-colored borders
			const v = store.getVideoMetrics();
			const err = store.getErrorCounters();

			// Ingest: good if bitrate > 0
			nodeEls[0].dataset.health = v.serverBitrateKbps > 0 ? "good" : "critical";

			// Demux: good if no PTS errors
			nodeEls[1].dataset.health = err.ptsErrors > 0 ? "warn" : "good";

			// Relay: good if no server video drops
			nodeEls[2].dataset.health = err.serverVideoDropped > 0 ? "warn" : "good";

			// Player: render/server FPS ratio
			if (v.serverFrameRate > 0 && v.renderFps > 0) {
				const ratio = v.renderFps / v.serverFrameRate;
				nodeEls[3].dataset.health = ratio < 0.5 ? "critical" : ratio < 0.8 ? "warn" : "good";
			} else {
				nodeEls[3].dataset.health = "good";
			}
		},
		destroy: () => el.remove(),
	};
}

function formatBitrate(kbps: number): string {
	if (kbps >= 1000) return `${(kbps / 1000).toFixed(1)} Mbps`;
	if (kbps > 0) return `${Math.round(kbps)} kbps`;
	return "\u2014";
}

function formatUptime(ms: number): string {
	const s = Math.floor(ms / 1000);
	const m = Math.floor(s / 60);
	const h = Math.floor(m / 60);
	if (h > 0) return `${h}h ${m % 60}m`;
	if (m > 0) return `${m}m ${s % 60}s`;
	return `${s}s`;
}

// ── Dashboard ────────────────────────────────────────────────────

export class InspectorDashboard {
	private el: HTMLElement;
	private store: MetricsStore;
	private rafId = 0;
	private lastRenderTime = 0;
	private visible = false;
	private onClose: (() => void) | null = null;

	// Banner
	private bannerEl: HTMLElement | null = null;

	// Pipeline
	private pipelineFlow: { el: HTMLElement; update: () => void; destroy: () => void } | null = null;

	// Video Pipeline card
	private fpsChart: TimeSeriesChart | null = null;
	private bitrateChart: TimeSeriesChart | null = null;
	private decodeQueueChart: TimeSeriesChart | null = null;

	// Sync & Buffers card
	private syncChart: TimeSeriesChart | null = null;
	private audioBufferChart: TimeSeriesChart | null = null;
	private frameDropsChart: TimeSeriesChart | null = null;

	// Structure & Errors card
	private gopChart: GOPChart | null = null;
	private errorCountersEl: HTMLElement | null = null;
	private audioTracksEl: HTMLElement | null = null;

	// Signals & Metadata card
	private scteTimeline: SCTE35Timeline | null = null;
	private captionInfoEl: HTMLElement | null = null;
	private scteCountEl: HTMLElement | null = null;
	private silenceGauge: GaugeBar | null = null;
	private timecodeEl: HTMLElement | null = null;

	constructor(mount: HTMLElement, store: MetricsStore) {
		this.store = store;
		this.el = document.createElement("div");
		this.el.className = "inspector-dashboard";
		this.el.style.display = "none";

		this.buildHeader();
		this.buildIdentityBanner();
		this.buildPipeline();

		const grid = document.createElement("div");
		grid.className = "idash-grid";
		this.el.appendChild(grid);

		this.buildVideoPipelineCard(grid);
		this.buildSyncBuffersCard(grid);
		this.buildStructureCard(grid);
		this.buildSignalsMetadataCard(grid);

		mount.appendChild(this.el);
	}

	private buildHeader(): void {
		const header = document.createElement("div");
		header.className = "idash-header";

		const title = document.createElement("span");
		title.className = "idash-title";
		title.textContent = "STREAM INSPECTOR";
		header.appendChild(title);

		const hint = document.createElement("span");
		hint.className = "idash-hint";
		hint.textContent = "D to toggle";
		header.appendChild(hint);

		const closeBtn = document.createElement("button");
		closeBtn.className = "idash-close";
		closeBtn.innerHTML = "&times;";
		closeBtn.addEventListener("click", () => this.onClose?.());
		header.appendChild(closeBtn);

		this.el.appendChild(header);
	}

	private buildIdentityBanner(): void {
		this.bannerEl = document.createElement("div");
		this.bannerEl.className = "idash-banner";
		this.bannerEl.textContent = "\u2014";
		this.el.appendChild(this.bannerEl);
	}

	private buildPipeline(): void {
		this.pipelineFlow = createPipelineFlow(this.el, this.store);
	}

	private createCard(parent: HTMLElement, title: string): HTMLElement {
		const card = document.createElement("div");
		card.className = "inspector-card";

		const hdr = document.createElement("div");
		hdr.className = "icard-title";
		hdr.textContent = title;
		card.appendChild(hdr);

		const body = document.createElement("div");
		body.className = "icard-body";
		card.appendChild(body);
		parent.appendChild(card);
		return body;
	}

	private buildVideoPipelineCard(grid: HTMLElement): void {
		const body = this.createCard(grid, "VIDEO PIPELINE");

		// FPS chart — dual series: server + render
		const fpsLabel = document.createElement("div");
		fpsLabel.className = "icard-label";
		fpsLabel.textContent = "Frame Rate";
		body.appendChild(fpsLabel);

		createChartLegend(body, [
			{ label: "Server FPS", color: "#34d399" },
			{ label: "Render FPS", color: "#6366f1" },
		]);

		this.fpsChart = new TimeSeriesChart(body, [
			{ label: "Server FPS", color: "#34d399", getData: () => this.store.getVideoMetrics().fpsHistory, unit: "fps" },
			{ label: "Render FPS", color: "#6366f1", getData: () => this.store.getVideoMetrics().renderFpsHistory, unit: "fps" },
		], { height: 100, showGrid: true, showValue: true });

		// Bitrate chart
		const brLabel = document.createElement("div");
		brLabel.className = "icard-label";
		brLabel.textContent = "Bitrate";
		body.appendChild(brLabel);

		this.bitrateChart = new TimeSeriesChart(body, [
			{ label: "Bitrate", color: "#6366f1", getData: () => this.store.getVideoMetrics().bitrateHistory, unit: " kbps" },
		], { height: 80, showGrid: true, showValue: true });

		// Decode Queue — time series (replaces gauge)
		const dqLabel = document.createElement("div");
		dqLabel.className = "icard-label";
		dqLabel.textContent = "Decode Queue";
		body.appendChild(dqLabel);

		this.decodeQueueChart = new TimeSeriesChart(body, [
			{ label: "Queue", color: "#8b5cf6", getData: () => this.store.getVideoMetrics().decodeQueueHistory, unit: "ms" },
		], { height: 60, showGrid: true, showValue: true, thresholds: { warn: 200, critical: 400 } });
	}

	private buildSyncBuffersCard(grid: HTMLElement): void {
		const body = this.createCard(grid, "SYNC & BUFFERS");

		// Sync chart
		const syncLabel = document.createElement("div");
		syncLabel.className = "icard-label";
		syncLabel.textContent = "A/V Sync Offset";
		body.appendChild(syncLabel);

		this.syncChart = new TimeSeriesChart(body, [
			{ label: "Sync", color: "#c084fc", getData: () => this.store.getSyncMetrics().offsetHistory, unit: "ms" },
		], {
			height: 100, showGrid: true, showValue: true,
			symmetric: true,
			minRange: 50,
			thresholds: { warn: 50, critical: 200 },
		});

		// Audio Buffer — time series (replaces gauge)
		const bufLabel = document.createElement("div");
		bufLabel.className = "icard-label";
		bufLabel.textContent = "Audio Buffer";
		body.appendChild(bufLabel);

		this.audioBufferChart = new TimeSeriesChart(body, [
			{ label: "Buffer", color: "#34d399", getData: () => this.store.getAudioMetrics().bufferHistory, unit: "ms" },
		], { height: 60, showGrid: true, showValue: true });

		// Frame Drops/sec — bar chart
		const dropsLabel = document.createElement("div");
		dropsLabel.className = "icard-label";
		dropsLabel.textContent = "Frame Drops/sec";
		body.appendChild(dropsLabel);

		this.frameDropsChart = new TimeSeriesChart(body, [
			{ label: "Drops", color: "#f87171", getData: () => this.store.getVideoMetrics().frameDropsHistory, unit: "" },
		], { height: 50, showGrid: true, showValue: true, barMode: true });
	}

	private buildStructureCard(grid: HTMLElement): void {
		const body = this.createCard(grid, "STRUCTURE & ERRORS");

		// GOP chart
		const gopLabel = document.createElement("div");
		gopLabel.className = "icard-label";
		gopLabel.textContent = "GOP Structure";
		body.appendChild(gopLabel);

		this.gopChart = new GOPChart(body, { height: 50 });

		// Error Counters table
		const errLabel = document.createElement("div");
		errLabel.className = "icard-label";
		errLabel.style.marginTop = "8px";
		errLabel.textContent = "Error Counters";
		body.appendChild(errLabel);

		this.errorCountersEl = document.createElement("div");
		this.errorCountersEl.className = "icard-error-counters";
		body.appendChild(this.errorCountersEl);

		// Audio tracks
		const trackLabel = document.createElement("div");
		trackLabel.className = "icard-label";
		trackLabel.style.marginTop = "8px";
		trackLabel.textContent = "Audio Tracks";
		body.appendChild(trackLabel);

		this.audioTracksEl = document.createElement("div");
		this.audioTracksEl.className = "icard-tracks";
		body.appendChild(this.audioTracksEl);
	}

	private buildSignalsMetadataCard(grid: HTMLElement): void {
		const body = this.createCard(grid, "SIGNALS & METADATA");

		// SCTE-35 timeline
		const scteLabel = document.createElement("div");
		scteLabel.className = "icard-label";
		scteLabel.textContent = "SCTE-35 Timeline";
		body.appendChild(scteLabel);

		this.scteTimeline = new SCTE35Timeline(body, { height: 60 });

		// Summary stats
		const infoGrid = document.createElement("div");
		infoGrid.className = "icard-info-grid";
		infoGrid.style.marginTop = "12px";
		body.appendChild(infoGrid);

		this.captionInfoEl = this.addInfoRow(infoGrid, "Caption Channels");
		this.scteCountEl = this.addInfoRow(infoGrid, "SCTE-35 Events");

		// Silence gauge
		const silLabel = document.createElement("div");
		silLabel.className = "icard-label";
		silLabel.style.marginTop = "8px";
		silLabel.textContent = "Silence Inserted";
		body.appendChild(silLabel);

		this.silenceGauge = new GaugeBar(body, {
			label: "Silence", unit: "ms",
			min: 0, max: 1000,
			warnAt: 200, criticalAt: 500,
			color: "#34d399",
		});

		// Timecode
		this.timecodeEl = this.addInfoRow(body, "Timecode");
	}

	private addInfoRow(parent: HTMLElement, label: string): HTMLElement {
		const row = document.createElement("div");
		row.className = "icard-info-row";

		const lbl = document.createElement("span");
		lbl.className = "icard-info-label";
		lbl.textContent = label;
		row.appendChild(lbl);

		const val = document.createElement("span");
		val.className = "icard-info-value";
		val.textContent = "\u2014";
		row.appendChild(val);

		parent.appendChild(row);
		return val;
	}

	setOnClose(cb: () => void): void {
		this.onClose = cb;
	}

	show(): void {
		this.visible = true;
		this.el.style.display = "";

		// Trigger entry animation
		requestAnimationFrame(() => {
			this.el.classList.add("idash-visible");
		});

		this.startRaf();
	}

	hide(): void {
		this.visible = false;
		this.el.classList.remove("idash-visible");
		this.el.style.display = "none";
		this.stopRaf();
	}

	isVisible(): boolean {
		return this.visible;
	}

	private startRaf(): void {
		this.stopRaf();
		const loop = (ts: number) => {
			if (!this.visible) return;
			// Throttle to ~30fps
			if (ts - this.lastRenderTime >= 33) {
				this.lastRenderTime = ts;
				this.renderAll();
			}
			this.rafId = requestAnimationFrame(loop);
		};
		this.rafId = requestAnimationFrame(loop);
	}

	private stopRaf(): void {
		if (this.rafId) {
			cancelAnimationFrame(this.rafId);
			this.rafId = 0;
		}
	}

	private renderAll(): void {
		const a = this.store.getAudioMetrics();
		const s = this.store.getSyncMetrics();

		// Banner
		this.updateBanner();

		// Pipeline
		this.pipelineFlow?.update();

		// Video Pipeline card
		this.fpsChart?.render();
		this.bitrateChart?.render();
		this.decodeQueueChart?.render();

		// Sync & Buffers card
		this.syncChart?.render();
		this.audioBufferChart?.render();
		this.frameDropsChart?.render();

		// Structure & Errors card
		this.gopChart?.update(this.store.getFrameEvents());
		this.gopChart?.render();
		this.updateErrorCounters();
		this.updateAudioTracks(a.tracks);

		// Signals & Metadata card
		this.scteTimeline?.update(this.store.getAccumulatedSCTE35());
		this.scteTimeline?.render();
		this.updateCaptionsAndSignals();
		this.silenceGauge?.update(a.silenceMs);
		this.silenceGauge?.render();
		this.updateTimecode();

		void s; // consumed via chart getData callbacks
	}

	private updateBanner(): void {
		if (!this.bannerEl) return;
		const info = this.store.getStreamInfo();
		const parts: string[] = [];

		if (info.videoCodec) parts.push(info.videoCodec.toUpperCase());
		if (info.resolution) parts.push(info.resolution);
		if (info.frameRate) parts.push(info.frameRate);
		if (info.bitrate) parts.push(info.bitrate);

		if (info.audioCodec && info.audioConfig) {
			let audioSummary = `${info.audioCodec.toUpperCase()} ${info.audioConfig}`;
			if (info.audioTrackCount > 1) audioSummary += ` \u00d7${info.audioTrackCount}`;
			parts.push(audioSummary);
		}

		if (info.protocol) parts.push(info.protocol.toUpperCase());
		if (info.uptimeMs > 0) parts.push(formatUptime(info.uptimeMs));

		this.bannerEl.textContent = parts.length > 0 ? parts.join(" | ") : "\u2014";
	}

	private updateErrorCounters(): void {
		if (!this.errorCountersEl) return;
		const err = this.store.getErrorCounters();
		const rows = [
			{ label: "PTS Errors", value: err.ptsErrors },
			{ label: "Client Drops", value: err.clientDropped },
			{ label: "Relay Video Drops", value: err.serverVideoDropped },
			{ label: "Relay Audio Drops", value: err.serverAudioDropped },
		];

		const html = rows.map(r => {
			const cls = r.value > 0 ? "icard-error-value icard-error-nonzero" : "icard-error-value";
			return `<div class="icard-error-row"><span class="icard-error-label">${r.label}</span><span class="${cls}">${r.value}</span></div>`;
		}).join("");

		if (this.errorCountersEl.innerHTML !== html) {
			this.errorCountersEl.innerHTML = html;
		}
	}

	private updateAudioTracks(tracks: { codec: string; sampleRate: number; channels: number; bitrateKbps: number }[]): void {
		if (!this.audioTracksEl) return;
		if (tracks.length === 0) {
			this.audioTracksEl.textContent = "No audio tracks";
			return;
		}
		const html = tracks.map((tk, i) =>
			`<div class="icard-track"><span class="icard-track-idx">${i + 1}</span>${tk.codec.toUpperCase()} ${(tk.sampleRate / 1000).toFixed(0)}kHz ${tk.channels}ch ${Math.round(tk.bitrateKbps)}k</div>`
		).join("");
		if (this.audioTracksEl.innerHTML !== html) {
			this.audioTracksEl.innerHTML = html;
		}
	}

	private updateCaptionsAndSignals(): void {
		const c = this.store.getCaptionMetrics();
		if (this.captionInfoEl) {
			const channels = c.activeChannels;
			this.captionInfoEl.textContent = channels.length > 0
				? channels.map(ch => `CC${ch}`).join(", ")
				: "None";
		}
		if (this.scteCountEl) {
			this.scteCountEl.textContent = `${this.store.getSCTE35Total()}`;
		}
	}

	private updateTimecode(): void {
		if (!this.timecodeEl) return;
		const tc = this.store.getTimecode();
		this.timecodeEl.textContent = tc || "\u2014";
	}

	destroy(): void {
		this.stopRaf();
		this.fpsChart?.destroy();
		this.bitrateChart?.destroy();
		this.decodeQueueChart?.destroy();
		this.syncChart?.destroy();
		this.audioBufferChart?.destroy();
		this.frameDropsChart?.destroy();
		this.gopChart?.destroy();
		this.silenceGauge?.destroy();
		this.scteTimeline?.destroy();
		this.pipelineFlow?.destroy();
		this.el.remove();
	}
}
