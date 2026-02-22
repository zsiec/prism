import type { MetricsStore, HealthStatus, StreamInfo } from "./metrics-store";
import type { ServerSCTE35Event } from "./transport";

const STATUS_COLORS: Record<HealthStatus, string> = {
	good: "#22c55e",
	warn: "#eab308",
	critical: "#ef4444",
};

const BADGE_BG: Record<HealthStatus, string> = {
	good: "rgba(34,197,94,0.15)",
	warn: "rgba(234,179,8,0.15)",
	critical: "rgba(239,68,68,0.2)",
};

type BadgeKey = "video" | "audio" | "sync" | "buffer" | "viewers";

const HEALTH_LABELS: Record<"video" | "audio" | "sync", string> = {
	video: "VID",
	audio: "AUD",
	sync: "SYNC",
};

const SCTE35_FADE_MS = 15_000;
const HUD_INTERVAL_MS = 1000;

export class HUD {
	private container: HTMLElement;
	private badges: Map<BadgeKey, HTMLElement> = new Map();
	private store: MetricsStore;
	private onBadgeClick: ((key: BadgeKey) => void) | null = null;
	private onSCTE35Click: ((event: ServerSCTE35Event) => void) | null = null;
	private intervalId: ReturnType<typeof setInterval> | null = null;
	private tcEl: HTMLElement | null = null;
	private scte35Row: HTMLElement | null = null;
	private renderedSCTE35Ids: Set<string> = new Set();
	private _externallyDriven = false;

	private streamInfoEl: HTMLElement | null = null;
	private lastStreamInfoStr = "";
	private lastBadgeLabels = new Map<string, string>();
	private lastBadgeStatuses = new Map<string, HealthStatus>();

	constructor(container: HTMLElement, store: MetricsStore) {
		this.container = container;
		this.store = store;
		this.build();
	}

	private build(): void {
		this.container.style.display = "flex";
		this.container.style.flexDirection = "column";
		this.container.style.gap = "6px";

		const topRow = document.createElement("div");
		topRow.style.display = "flex";
		topRow.style.gap = "6px";
		topRow.style.alignItems = "center";
		topRow.style.flexWrap = "nowrap";
		topRow.style.background = "rgba(0, 0, 0, 0.55)";
		topRow.style.backdropFilter = "blur(4px)";
		topRow.style.padding = "4px 8px";
		topRow.style.borderRadius = "4px";
		topRow.style.border = "1px solid rgba(255, 255, 255, 0.08)";
		this.container.appendChild(topRow);

		this.streamInfoEl = document.createElement("div");
		this.streamInfoEl.style.display = "flex";
		this.streamInfoEl.style.alignItems = "center";
		this.streamInfoEl.style.gap = "4px";
		this.streamInfoEl.style.fontSize = "11px";
		this.streamInfoEl.style.fontFamily = "'SF Mono', 'Menlo', monospace";
		this.streamInfoEl.style.fontWeight = "500";
		this.streamInfoEl.style.color = "#cbd5e1";
		this.streamInfoEl.style.whiteSpace = "nowrap";
		this.streamInfoEl.style.fontVariantNumeric = "tabular-nums";
		this.streamInfoEl.style.letterSpacing = "0.02em";
		this.streamInfoEl.style.marginRight = "6px";
		topRow.appendChild(this.streamInfoEl);

		const healthKeys: ("video" | "audio" | "sync")[] = ["video", "audio", "sync"];
		for (const key of healthKeys) {
			const el = document.createElement("div");
			el.style.display = "flex";
			el.style.alignItems = "center";
			el.style.gap = "4px";
			el.style.padding = "2px 6px";
			el.style.borderRadius = "3px";
			el.style.fontSize = "10px";
			el.style.fontFamily = "'SF Mono', 'Menlo', monospace";
			el.style.fontWeight = "600";
			el.style.cursor = "pointer";
			el.style.userSelect = "none";
			el.style.transition = "background 0.15s ease";
			el.style.whiteSpace = "nowrap";
			el.style.letterSpacing = "0.03em";
			el.style.fontVariantNumeric = "tabular-nums";

			el.addEventListener("click", (e) => {
				e.stopPropagation();
				if (this.onBadgeClick) this.onBadgeClick(key);
			});

			this.badges.set(key, el);
			topRow.appendChild(el);
		}

		const spacer = document.createElement("div");
		spacer.style.flex = "1";
		topRow.appendChild(spacer);

		const tc = document.createElement("div");
		tc.style.display = "none";
		tc.style.padding = "2px 8px";
		tc.style.borderRadius = "3px";
		tc.style.fontSize = "13px";
		tc.style.fontFamily = "'SF Mono', 'Menlo', 'Monaco', monospace";
		tc.style.fontWeight = "700";
		tc.style.fontVariantNumeric = "tabular-nums";
		tc.style.letterSpacing = "0.05em";
		tc.style.color = "#f8fafc";
		tc.style.background = "rgba(255, 255, 255, 0.08)";
		tc.style.userSelect = "none";
		tc.style.whiteSpace = "nowrap";
		tc.style.minWidth = "108px";
		tc.style.textAlign = "center";
		this.tcEl = tc;
		topRow.appendChild(tc);

		this.scte35Row = document.createElement("div");
		this.scte35Row.style.display = "flex";
		this.scte35Row.style.flexDirection = "column";
		this.scte35Row.style.gap = "4px";
		this.container.appendChild(this.scte35Row);
	}

	setOnBadgeClick(cb: (key: BadgeKey) => void): void {
		this.onBadgeClick = cb;
	}

	setOnSCTE35Click(cb: (event: ServerSCTE35Event) => void): void {
		this.onSCTE35Click = cb;
	}

	set externallyDriven(v: boolean) {
		this._externallyDriven = v;
		if (v) {
			this.stop();
			this.container.style.display = "none";
		} else {
			this.container.style.display = "flex";
		}
	}

	start(): void {
		if (this._externallyDriven) return;
		if (this.intervalId !== null) return;
		this.render();
		this.intervalId = setInterval(() => this.render(), HUD_INTERVAL_MS);
	}

	stop(): void {
		if (this.intervalId !== null) {
			clearInterval(this.intervalId);
			this.intervalId = null;
		}
	}

	private render(): void {
		this.renderStreamInfo();

		const state = this.store.getHUDState();
		this.updateBadge("video", HEALTH_LABELS.video, state.video.label, state.video.status);
		this.updateBadge("audio", HEALTH_LABELS.audio, state.audio.label, state.audio.status);
		this.updateBadge("sync", HEALTH_LABELS.sync, state.sync.label, state.sync.status);

		if (this.tcEl) {
			const tc = this.store.getTimecode();
			if (tc) {
				this.tcEl.style.display = "";
				if (this.tcEl.textContent !== tc) this.tcEl.textContent = tc;
			} else {
				this.tcEl.style.display = "none";
			}
		}

		this.renderSCTE35();
		this.store.clearDirty();
	}

	private renderStreamInfo(): void {
		if (!this.streamInfoEl) return;
		const info = this.store.getStreamInfo();
		const str = this.formatStreamInfo(info);
		if (str === this.lastStreamInfoStr) return;
		this.lastStreamInfoStr = str;
		this.streamInfoEl.textContent = str || "\u2014";
	}

	private formatStreamInfo(info: StreamInfo): string {
		const parts: string[] = [];
		if (info.videoCodec) {
			const vParts = [info.videoCodec, info.resolution, info.frameRate].filter(Boolean);
			parts.push(vParts.join(" "));
		}
		if (info.audioCodec || info.audioConfig) {
			const aParts = [info.audioCodec.toUpperCase(), info.audioConfig].filter(Boolean);
			parts.push(aParts.join(" "));
		}
		return parts.join("  |  ");
	}

	private renderSCTE35(): void {
		if (!this.scte35Row) return;

		const events = this.store.getSCTE35Events();
		const now = Date.now();

		const visible = events.filter((e) => now - e.receivedAt < SCTE35_FADE_MS);

		const currentIds = new Set(visible.map((e) => scte35Key(e)));

		for (const el of Array.from(this.scte35Row.children)) {
			const id = (el as HTMLElement).dataset.scte35Id;
			if (id && !currentIds.has(id)) {
				(el as HTMLElement).style.opacity = "0";
				(el as HTMLElement).style.transform = "translateX(-20px)";
				setTimeout(() => el.remove(), 300);
				this.renderedSCTE35Ids.delete(id);
			}
		}

		for (const event of visible) {
			const id = scte35Key(event);
			const age = now - event.receivedAt;

			if (!this.renderedSCTE35Ids.has(id)) {
				this.renderedSCTE35Ids.add(id);
				const el = this.buildSCTE35Toast(event, id);
				this.scte35Row.appendChild(el);
				requestAnimationFrame(() => {
					el.style.opacity = "1";
					el.style.transform = "translateX(0)";
				});
			}

			const existing = this.scte35Row.querySelector(`[data-scte35-id="${id}"]`) as HTMLElement | null;
			if (existing) {
				const fadeStart = SCTE35_FADE_MS * 0.7;
				if (age > fadeStart) {
					const fadeProgress = (age - fadeStart) / (SCTE35_FADE_MS - fadeStart);
					existing.style.opacity = String(Math.max(0, 1 - fadeProgress));
				}
			}
		}

		for (const agoEl of this.scte35Row.querySelectorAll(".scte35-ago")) {
			const recv = Number((agoEl as HTMLElement).dataset.receivedAt);
			if (!recv) continue;
			const secs = Math.floor((now - recv) / 1000);
			(agoEl as HTMLElement).textContent = secs < 1 ? "now" : `${secs}s ago`;
		}
	}

	private buildSCTE35Toast(event: ServerSCTE35Event, id: string): HTMLElement {
		const el = document.createElement("div");
		el.dataset.scte35Id = id;
		el.style.display = "flex";
		el.style.alignItems = "center";
		el.style.gap = "6px";
		el.style.padding = "4px 10px";
		el.style.borderRadius = "3px";
		el.style.fontSize = "11px";
		el.style.fontFamily = "'SF Mono', 'Menlo', monospace";
		el.style.fontWeight = "600";
		el.style.cursor = "pointer";
		el.style.userSelect = "none";
		el.style.whiteSpace = "nowrap";
		el.style.opacity = "0";
		el.style.transform = "translateX(-20px)";
		el.style.transition = "opacity 0.3s ease, transform 0.3s ease";
		el.style.background = "rgba(168, 85, 247, 0.2)";
		el.style.border = "1px solid rgba(168, 85, 247, 0.4)";
		el.style.borderLeft = "2px solid #a855f7";
		el.style.color = "#e2e8f0";

		const label = document.createElement("span");
		label.style.fontWeight = "700";
		label.style.color = "#c084fc";
		label.style.fontSize = "10px";
		label.style.textTransform = "uppercase";
		label.style.letterSpacing = "0.06em";
		label.textContent = "SCTE-35";
		el.appendChild(label);

		const desc = document.createElement("span");
		desc.textContent = event.description;
		desc.style.opacity = "0.9";
		el.appendChild(desc);

		if (event.duration && event.duration > 0) {
			const dur = document.createElement("span");
			dur.style.opacity = "0.5";
			dur.style.fontSize = "10px";
			dur.textContent = `${event.duration.toFixed(1)}s`;
			el.appendChild(dur);
		}

		const timeWrap = document.createElement("span");
		timeWrap.style.opacity = "0.5";
		timeWrap.style.fontSize = "10px";
		timeWrap.style.display = "flex";
		timeWrap.style.gap = "4px";

		const clock = document.createElement("span");
		const d = new Date(event.receivedAt);
		clock.textContent = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
		timeWrap.appendChild(clock);

		const ago = document.createElement("span");
		ago.className = "scte35-ago";
		ago.dataset.receivedAt = String(event.receivedAt);
		ago.style.opacity = "0.7";
		timeWrap.appendChild(ago);

		el.appendChild(timeWrap);

		el.addEventListener("click", (e) => {
			e.stopPropagation();
			if (this.onSCTE35Click) this.onSCTE35Click(event);
		});

		el.addEventListener("mouseenter", () => {
			el.style.background = "rgba(168, 85, 247, 0.35)";
		});
		el.addEventListener("mouseleave", () => {
			el.style.background = "rgba(168, 85, 247, 0.2)";
		});

		return el;
	}

	private updateBadge(key: "video" | "audio" | "sync", tag: string, label: string, status: HealthStatus): void {
		const el = this.badges.get(key);
		if (!el) return;

		const lastLabel = this.lastBadgeLabels.get(key);
		const lastStatus = this.lastBadgeStatuses.get(key);
		if (lastLabel === label && lastStatus === status) return;
		this.lastBadgeLabels.set(key, label);
		this.lastBadgeStatuses.set(key, status);

		const dot = `<span style="color:${STATUS_COLORS[status]};font-size:7px">\u25CF</span>`;
		el.innerHTML = `${dot} <span style="opacity:0.6;letter-spacing:0.05em">${tag}</span> ${label}`;
		el.style.background = BADGE_BG[status];
		el.style.color = "#e2e8f0";
		el.style.border = `1px solid ${STATUS_COLORS[status]}33`;
	}

	setHighlight(key: BadgeKey | null): void {
		for (const [k, el] of this.badges) {
			if (k === key) {
				el.style.outline = "1px solid rgba(255,255,255,0.5)";
				el.style.outlineOffset = "1px";
			} else {
				el.style.outline = "";
				el.style.outlineOffset = "";
			}
		}
	}

	destroy(): void {
		this.stop();
		this.renderedSCTE35Ids.clear();
		this.container.innerHTML = "";
	}
}

function scte35Key(e: ServerSCTE35Event): string {
	return `${e.receivedAt}-${e.commandType}-${e.eventId ?? 0}`;
}

export type { BadgeKey };
