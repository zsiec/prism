/**
 * Compact 44px metrics strip shown below the video when connected.
 * Displays health dot, FPS, bitrate, A/V sync, viewer count,
 * mini sparklines, and a dashboard toggle button.
 */

import type { MetricsStore, HealthStatus } from "./metrics-store";
import { MiniSparkline } from "./inspector-charts";

const STATUS_COLORS: Record<HealthStatus, string> = {
	good: "#34d399",
	warn: "#fbbf24",
	critical: "#f87171",
};

function formatBitrate(kbps: number): string {
	if (kbps >= 1000) return `${(kbps / 1000).toFixed(1)}M`;
	return `${Math.round(kbps)}K`;
}

export class InspectorStrip {
	private el: HTMLElement;
	private dot: HTMLElement;
	private fpsLabel: HTMLElement;
	private bitrateLabel: HTMLElement;
	private syncLabel: HTMLElement;
	private viewerLabel: HTMLElement;
	private chevronBtn: HTMLButtonElement;

	private fpsSparkline: MiniSparkline;
	private bitrateSparkline: MiniSparkline;

	private store: MetricsStore;
	private onToggleDashboard: (() => void) | null = null;
	private updateHandler: () => void;

	constructor(mount: HTMLElement, store: MetricsStore) {
		this.store = store;

		this.el = document.createElement("div");
		this.el.className = "inspector-strip";
		this.el.style.display = "none";

		// Health dot
		this.dot = document.createElement("span");
		this.dot.className = "istrip-dot";
		this.el.appendChild(this.dot);

		// FPS
		this.fpsLabel = document.createElement("span");
		this.fpsLabel.className = "istrip-value";
		this.el.appendChild(this.fpsLabel);

		this.el.appendChild(this.makeSep());

		// FPS sparkline
		const fpsSpark = document.createElement("span");
		fpsSpark.className = "istrip-spark";
		this.el.appendChild(fpsSpark);
		this.fpsSparkline = new MiniSparkline(fpsSpark, () => store.getVideoMetrics().fpsHistory, {
			width: 80, height: 28, color: "#34d399",
		});

		this.el.appendChild(this.makeSep());

		// Bitrate
		this.bitrateLabel = document.createElement("span");
		this.bitrateLabel.className = "istrip-value";
		this.el.appendChild(this.bitrateLabel);

		this.el.appendChild(this.makeSep());

		// Bitrate sparkline
		const brSpark = document.createElement("span");
		brSpark.className = "istrip-spark";
		this.el.appendChild(brSpark);
		this.bitrateSparkline = new MiniSparkline(brSpark, () => store.getVideoMetrics().bitrateHistory, {
			width: 80, height: 28, color: "#6366f1",
		});

		this.el.appendChild(this.makeSep());

		// Sync
		this.syncLabel = document.createElement("span");
		this.syncLabel.className = "istrip-value";
		this.el.appendChild(this.syncLabel);

		this.el.appendChild(this.makeSep());

		// Viewers
		this.viewerLabel = document.createElement("span");
		this.viewerLabel.className = "istrip-value istrip-viewers";
		this.el.appendChild(this.viewerLabel);

		// Spacer
		const spacer = document.createElement("span");
		spacer.style.flex = "1";
		this.el.appendChild(spacer);

		// Dashboard toggle
		this.chevronBtn = document.createElement("button");
		this.chevronBtn.className = "istrip-chevron";
		this.chevronBtn.innerHTML = "&#x25B2;"; // up triangle
		this.chevronBtn.title = "Toggle dashboard (D)";
		this.chevronBtn.addEventListener("click", () => this.onToggleDashboard?.());
		this.el.appendChild(this.chevronBtn);

		mount.appendChild(this.el);

		this.updateHandler = () => this.refresh();
		store.addUpdateListener(this.updateHandler);
	}

	private makeSep(): HTMLElement {
		const sep = document.createElement("span");
		sep.className = "istrip-sep";
		return sep;
	}

	setOnToggleDashboard(cb: () => void): void {
		this.onToggleDashboard = cb;
	}

	setDashboardOpen(open: boolean): void {
		this.chevronBtn.innerHTML = open ? "&#x25BC;" : "&#x25B2;";
		this.chevronBtn.classList.toggle("istrip-chevron-open", open);
	}

	show(): void {
		this.el.style.display = "";
		this.refresh();
	}

	hide(): void {
		this.el.style.display = "none";
	}

	private refresh(): void {
		const v = this.store.getVideoMetrics();
		const s = this.store.getSyncMetrics();
		const t = this.store.getTransportMetrics();
		const hud = this.store.getHUDState();

		// Dot
		this.dot.style.background = STATUS_COLORS[hud.video.status];
		this.dot.style.boxShadow = `0 0 6px ${STATUS_COLORS[hud.video.status]}`;

		// FPS
		this.fpsLabel.textContent = v.serverFrameRate > 0 ? `${v.serverFrameRate.toFixed(2)}fps` : "\u2014";

		// Bitrate
		this.bitrateLabel.textContent = v.serverBitrateKbps > 0 ? formatBitrate(v.serverBitrateKbps) : "\u2014";

		// Sync
		const syncAbs = Math.abs(s.offsetMs);
		const syncSign = s.offsetMs >= 0 ? "+" : "\u2212";
		this.syncLabel.textContent = `${syncSign}${syncAbs.toFixed(0)}ms sync`;
		const syncStatus = syncAbs > 200 ? "critical" : syncAbs > 50 ? "warn" : "good";
		this.syncLabel.style.color = STATUS_COLORS[syncStatus];

		// Viewers
		this.viewerLabel.textContent = `${t.viewerCount} viewer${t.viewerCount !== 1 ? "s" : ""}`;

		// Sparklines
		requestAnimationFrame(() => {
			this.fpsSparkline.render();
			this.bitrateSparkline.render();
		});
	}

	destroy(): void {
		this.store.removeUpdateListener(this.updateHandler);
		this.fpsSparkline.destroy();
		this.bitrateSparkline.destroy();
		this.el.remove();
	}
}
