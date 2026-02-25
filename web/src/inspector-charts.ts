/**
 * Canvas-based chart primitives for the Stream Inspector.
 * All charts are DPR-aware and use CSS custom properties for colors.
 */

// ── Helpers ──────────────────────────────────────────────────────

function getCSSVar(name: string): string {
	return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function syncCanvasDPR(canvas: HTMLCanvasElement, width: number, height: number): CanvasRenderingContext2D {
	const dpr = window.devicePixelRatio || 1;
	canvas.width = Math.round(width * dpr);
	canvas.height = Math.round(height * dpr);
	canvas.style.width = `${width}px`;
	canvas.style.height = `${height}px`;
	const ctx = canvas.getContext("2d")!;
	ctx.scale(dpr, dpr);
	return ctx;
}

function autoScale(values: number[], opts?: { symmetric?: boolean; minRange?: number }): { min: number; max: number } {
	if (values.length === 0) {
		if (opts?.symmetric) {
			const half = (opts?.minRange ?? 50) / 2;
			return { min: -half, max: half };
		}
		return { min: 0, max: 1 };
	}
	let min = Infinity, max = -Infinity;
	for (const v of values) {
		if (v < min) min = v;
		if (v > max) max = v;
	}

	if (opts?.symmetric) {
		// Symmetric around zero: range = [-extent, +extent]
		let extent = Math.max(Math.abs(min), Math.abs(max));
		const minHalf = (opts?.minRange ?? 50) / 2;
		extent = Math.max(extent, minHalf);
		const pad = extent * 0.15;
		return { min: -(extent + pad), max: extent + pad };
	}

	if (min === max) {
		if (min === 0) return { min: 0, max: 1 };
		return { min: min * 0.9, max: max * 1.1 };
	}
	const pad = (max - min) * 0.1;
	return { min: Math.max(0, min - pad), max: max + pad };
}

function niceStep(range: number, targetTicks: number): number {
	const rough = range / targetTicks;
	const pow = Math.pow(10, Math.floor(Math.log10(rough)));
	const norm = rough / pow;
	let nice: number;
	if (norm <= 1.5) nice = 1;
	else if (norm <= 3) nice = 2;
	else if (norm <= 7) nice = 5;
	else nice = 10;
	return nice * pow;
}

// ── TimeSeriesChart ──────────────────────────────────────────────

export interface ChartSeries {
	label: string;
	color: string;
	getData: () => number[];
	unit: string;
}

interface TimeSeriesChartOpts {
	height?: number;
	compact?: boolean;
	showGrid?: boolean;
	showValue?: boolean;
	/** Center the Y-axis symmetrically around zero (ideal for sync offset). */
	symmetric?: boolean;
	/** Minimum Y-axis range in data units (prevents tiny values from becoming invisible). */
	minRange?: number;
	/** Optional horizontal threshold bands drawn as subtle colored regions. */
	thresholds?: { warn: number; critical: number; warnColor?: string; criticalColor?: string };
	/** Render filled bars instead of lines (ideal for discrete events like frame drops). */
	barMode?: boolean;
}

export class TimeSeriesChart {
	private canvas: HTMLCanvasElement;
	private ctx: CanvasRenderingContext2D;
	private series: ChartSeries[];
	private cssW = 0;
	private cssH: number;
	private compact: boolean;
	private showGrid: boolean;
	private showValue: boolean;
	private symmetric: boolean;
	private minRange: number;
	private thresholds?: { warn: number; critical: number; warnColor?: string; criticalColor?: string };
	private barMode: boolean;

	constructor(container: HTMLElement, series: ChartSeries[], opts?: TimeSeriesChartOpts) {
		this.series = series;
		this.cssH = opts?.height ?? 120;
		this.compact = opts?.compact ?? false;
		this.showGrid = opts?.showGrid ?? !this.compact;
		this.showValue = opts?.showValue ?? !this.compact;
		this.symmetric = opts?.symmetric ?? false;
		this.minRange = opts?.minRange ?? 0;
		this.thresholds = opts?.thresholds;
		this.barMode = opts?.barMode ?? false;

		this.canvas = document.createElement("canvas");
		this.canvas.style.width = "100%";
		this.canvas.style.height = `${this.cssH}px`;
		this.canvas.style.display = "block";
		this.canvas.style.borderRadius = "4px";
		container.appendChild(this.canvas);

		this.ctx = this.canvas.getContext("2d")!;
	}

	render(): void {
		const rect = this.canvas.getBoundingClientRect();
		const w = rect.width;
		if (w === 0) return;

		if (Math.abs(w - this.cssW) > 1) {
			this.cssW = w;
			this.ctx = syncCanvasDPR(this.canvas, w, this.cssH);
		}

		const ctx = this.ctx;
		const W = w;
		const H = this.cssH;
		const padL = this.compact ? 0 : 44;
		const padR = this.compact ? 0 : 8;
		const padT = this.compact ? 2 : 8;
		const padB = this.compact ? 2 : 4;
		const plotW = W - padL - padR;
		const plotH = H - padT - padB;

		ctx.clearRect(0, 0, W, H);

		// Collect all data to find shared scale
		const allValues: number[] = [];
		const datasets: number[][] = [];
		for (const s of this.series) {
			const d = s.getData();
			datasets.push(d);
			for (const v of d) allValues.push(v);
		}
		const { min, max } = autoScale(allValues, {
			symmetric: this.symmetric,
			minRange: this.minRange,
		});
		const range = max - min || 1;
		const toY = (v: number) => padT + plotH - ((v - min) / range) * plotH;

		// Threshold bands (drawn behind everything)
		if (this.thresholds && max > min) {
			const t = this.thresholds;
			const warnColor = t.warnColor || "rgba(251,191,36,0.06)";
			const critColor = t.criticalColor || "rgba(248,113,113,0.06)";

			// Critical bands: above +critical and below -critical
			const critTopY = Math.max(padT, toY(t.critical));
			const critBotY = Math.min(padT + plotH, toY(-t.critical));
			ctx.fillStyle = critColor;
			ctx.fillRect(padL, padT, plotW, critTopY - padT);
			ctx.fillRect(padL, critBotY, plotW, padT + plotH - critBotY);

			// Warn bands: between ±warn and ±critical
			const warnTopY = Math.max(padT, toY(t.warn));
			const warnBotY = Math.min(padT + plotH, toY(-t.warn));
			ctx.fillStyle = warnColor;
			ctx.fillRect(padL, warnTopY, plotW, critTopY - warnTopY);
			ctx.fillRect(padL, critBotY, plotW, warnBotY - critBotY);
		}

		// Zero reference line (for symmetric charts)
		if (this.symmetric && max > min) {
			const zeroY = toY(0);
			if (zeroY >= padT && zeroY <= padT + plotH) {
				ctx.strokeStyle = "rgba(255,255,255,0.15)";
				ctx.lineWidth = 1;
				ctx.setLineDash([4, 3]);
				ctx.beginPath();
				ctx.moveTo(padL, zeroY);
				ctx.lineTo(padL + plotW, zeroY);
				ctx.stroke();
				ctx.setLineDash([]);
			}
		}

		// Grid
		if (this.showGrid && max > min) {
			const step = niceStep(max - min, 3);
			const gridColor = getCSSVar("--border-default") || "rgba(255,255,255,0.09)";
			const textColor = getCSSVar("--text-tertiary") || "#4e4e66";
			ctx.strokeStyle = gridColor;
			ctx.lineWidth = 1;
			ctx.font = `10px ${getCSSVar("--font-mono") || "monospace"}`;
			ctx.fillStyle = textColor;
			ctx.textAlign = "right";
			ctx.textBaseline = "middle";

			const start = Math.ceil(min / step) * step;
			for (let v = start; v <= max; v += step) {
				const y = toY(v);
				ctx.beginPath();
				ctx.moveTo(padL, y);
				ctx.lineTo(padL + plotW, y);
				ctx.stroke();
				const label = this.symmetric && v > 0 ? `+${this.formatLabel(v)}` : this.formatLabel(v);
				ctx.fillText(label, padL - 6, y);
			}
		}

		for (let si = 0; si < this.series.length; si++) {
			const data = datasets[si];
			const color = this.series[si].color;
			if (data.length < 2) continue;

			if (this.barMode) {
				// Bar mode: filled rectangles for each data point
				const barW = Math.max(1, plotW / data.length);
				const baseY = padT + plotH;
				for (let i = 0; i < data.length; i++) {
					if (data[i] <= 0) continue;
					const x = padL + i * barW;
					const y = toY(data[i]);
					const barH = baseY - y;
					ctx.fillStyle = this.hexToRGBA(color, 0.7);
					ctx.fillRect(x, y, Math.max(barW - 1, 1), barH);
				}
			} else {
				const step = plotW / (data.length - 1);

				// Fill: for symmetric charts, fill from line to zero; otherwise to bottom
				const fillBaseY = this.symmetric ? toY(0) : padT + plotH;
				const grad = ctx.createLinearGradient(0, padT, 0, padT + plotH);
				grad.addColorStop(0, this.hexToRGBA(color, 0.25));
				grad.addColorStop(1, this.hexToRGBA(color, 0.02));

				const fillPath = new Path2D();
				for (let i = 0; i < data.length; i++) {
					const x = padL + i * step;
					const y = toY(data[i]);
					if (i === 0) fillPath.moveTo(x, y);
					else fillPath.lineTo(x, y);
				}
				fillPath.lineTo(padL + (data.length - 1) * step, fillBaseY);
				fillPath.lineTo(padL, fillBaseY);
				fillPath.closePath();
				ctx.fillStyle = grad;
				ctx.fill(fillPath);

				// Line
				ctx.beginPath();
				for (let i = 0; i < data.length; i++) {
					const x = padL + i * step;
					const y = toY(data[i]);
					if (i === 0) ctx.moveTo(x, y);
					else ctx.lineTo(x, y);
				}
				ctx.strokeStyle = color;
				ctx.lineWidth = 2;
				ctx.lineJoin = "round";
				ctx.lineCap = "round";
				ctx.stroke();
			}

			// Current value callout
			if (this.showValue && data.length > 0) {
				const last = data[data.length - 1];
				const lastY = toY(last);
				ctx.fillStyle = color;
				ctx.font = `bold 11px ${getCSSVar("--font-mono") || "monospace"}`;
				ctx.textAlign = "right";
				ctx.textBaseline = "bottom";
				const label = this.formatValue(last, this.series[si].unit);
				ctx.fillText(label, padL + plotW, Math.max(lastY - 4, padT + 12));
			}
		}
	}

	private formatLabel(v: number): string {
		if (Math.abs(v) >= 1000) return (v / 1000).toFixed(1) + "k";
		if (Math.abs(v) >= 100) return v.toFixed(0);
		if (Math.abs(v) >= 10) return v.toFixed(1);
		return v.toFixed(1);
	}

	/** Format the value callout with its unit, avoiding "5.2k kbps" redundancy. */
	private formatValue(v: number, unit: string): string {
		if (unit.toLowerCase().includes("kbps") || unit.toLowerCase().includes("bps")) {
			// Show as Mbps / kbps directly
			if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)} Mbps`;
			return `${v.toFixed(0)} kbps`;
		}
		const formatted = this.formatLabel(v);
		const sign = this.symmetric && v > 0 ? "+" : "";
		return `${sign}${formatted}${unit}`;
	}

	private hexToRGBA(hex: string, alpha: number): string {
		// Handle CSS var values or hex
		if (hex.startsWith("#")) {
			const r = parseInt(hex.slice(1, 3), 16);
			const g = parseInt(hex.slice(3, 5), 16);
			const b = parseInt(hex.slice(5, 7), 16);
			return `rgba(${r},${g},${b},${alpha})`;
		}
		return `rgba(100,102,241,${alpha})`;
	}

	destroy(): void {
		this.canvas.remove();
	}
}

// ── GaugeBar ─────────────────────────────────────────────────────
// DOM-based for reliable text layout; canvas gauges had text/bar overlap.

interface GaugeBarConfig {
	label: string;
	unit: string;
	min: number;
	max: number;
	warnAt: number;
	criticalAt: number;
	color: string;
	/** When true, values BELOW thresholds trigger warn/critical (e.g. audio buffer). */
	invertThresholds?: boolean;
}

export class GaugeBar {
	private el: HTMLElement;
	private fillEl: HTMLElement;
	private valueEl: HTMLElement;
	private warnTick: HTMLElement;
	private critTick: HTMLElement;
	private config: GaugeBarConfig;
	private value = 0;

	constructor(container: HTMLElement, config: GaugeBarConfig) {
		this.config = config;

		this.el = document.createElement("div");
		this.el.className = "gauge-bar";

		// Track with fill
		const track = document.createElement("div");
		track.className = "gauge-track";

		this.fillEl = document.createElement("div");
		this.fillEl.className = "gauge-fill";
		track.appendChild(this.fillEl);

		// Threshold ticks
		const range = config.max - config.min;
		this.warnTick = document.createElement("div");
		this.warnTick.className = "gauge-tick";
		if (range > 0) this.warnTick.style.left = `${((config.warnAt - config.min) / range) * 100}%`;
		track.appendChild(this.warnTick);

		this.critTick = document.createElement("div");
		this.critTick.className = "gauge-tick";
		if (range > 0) this.critTick.style.left = `${((config.criticalAt - config.min) / range) * 100}%`;
		track.appendChild(this.critTick);

		this.el.appendChild(track);

		// Value readout
		this.valueEl = document.createElement("span");
		this.valueEl.className = "gauge-value";
		this.el.appendChild(this.valueEl);

		container.appendChild(this.el);
	}

	update(value: number): void {
		this.value = value;
	}

	render(): void {
		const range = this.config.max - this.config.min;
		const clamped = Math.max(this.config.min, Math.min(this.config.max, this.value));
		const pct = range > 0 ? ((clamped - this.config.min) / range) * 100 : 0;
		this.fillEl.style.width = `${pct}%`;

		// Determine color based on threshold direction
		let color: string;
		if (this.config.invertThresholds) {
			// Low = bad (e.g. audio buffer: <=15 = critical, <=30 = warn)
			if (this.value <= this.config.criticalAt) color = "var(--status-critical)";
			else if (this.value <= this.config.warnAt) color = "var(--status-warn)";
			else color = this.config.color;
		} else {
			// High = bad (e.g. silence: >=500 = critical, >=200 = warn)
			if (this.value >= this.config.criticalAt) color = "var(--status-critical)";
			else if (this.value >= this.config.warnAt) color = "var(--status-warn)";
			else color = this.config.color;
		}
		this.fillEl.style.background = color;

		this.valueEl.textContent = `${this.value.toFixed(0)}${this.config.unit}`;
		this.valueEl.style.color = color;
	}

	destroy(): void {
		this.el.remove();
	}
}

// ── GOPChart ─────────────────────────────────────────────────────

import type { FrameEvent } from "./metrics-store";

interface GOPChartOpts {
	height?: number;
	maxFrames?: number;
}

export class GOPChart {
	private canvas: HTMLCanvasElement;
	private ctx: CanvasRenderingContext2D;
	private frames: FrameEvent[] = [];
	private cssH: number;
	private maxFrames: number;
	private cssW = 0;

	constructor(container: HTMLElement, opts?: GOPChartOpts) {
		this.cssH = opts?.height ?? 60;
		this.maxFrames = opts?.maxFrames ?? 120;
		this.canvas = document.createElement("canvas");
		this.canvas.style.width = "100%";
		this.canvas.style.height = `${this.cssH}px`;
		this.canvas.style.display = "block";
		this.canvas.style.borderRadius = "4px";
		container.appendChild(this.canvas);
		this.ctx = this.canvas.getContext("2d")!;
	}

	update(frames: FrameEvent[]): void {
		this.frames = frames.slice(-this.maxFrames);
	}

	render(): void {
		const rect = this.canvas.getBoundingClientRect();
		const w = rect.width;
		if (w === 0) return;

		if (Math.abs(w - this.cssW) > 1) {
			this.cssW = w;
			this.ctx = syncCanvasDPR(this.canvas, w, this.cssH);
		}

		const ctx = this.ctx;
		const W = w;
		const H = this.cssH;
		const pad = 2;

		ctx.clearRect(0, 0, W, H);

		const frames = this.frames;
		if (frames.length === 0) return;

		// Find max size for scaling P-frame heights
		let maxSize = 0;
		for (const f of frames) {
			if (f.size > maxSize) maxSize = f.size;
		}
		if (maxSize === 0) maxSize = 1;

		const barW = Math.max(1, (W - pad * 2) / this.maxFrames);
		const iColor = getCSSVar("--prism-2") || "#8b5cf6";
		const pColor = getCSSVar("--prism-1") || "#6366f1";

		// First pass: draw GOP boundary lines behind bars
		ctx.strokeStyle = "rgba(255,255,255,0.08)";
		ctx.lineWidth = 1;
		for (let i = 0; i < frames.length; i++) {
			if (frames[i].isKey && i > 0) {
				const x = Math.round(pad + i * barW) + 0.5;
				ctx.beginPath();
				ctx.moveTo(x, 0);
				ctx.lineTo(x, H);
				ctx.stroke();
			}
		}

		// Second pass: draw frame bars
		for (let i = 0; i < frames.length; i++) {
			const f = frames[i];
			const x = pad + i * barW;
			const relH = f.size / maxSize;

			if (f.isKey) {
				// I-frame: full height, brighter color
				ctx.fillStyle = iColor;
				ctx.globalAlpha = 0.9;
				ctx.fillRect(x, pad, Math.max(barW - 0.5, 1.5), H - pad * 2);
				ctx.globalAlpha = 1;
			} else {
				// P-frame: height proportional to relative size
				const barH = Math.max(2, relH * (H - pad * 2));
				ctx.fillStyle = pColor;
				ctx.globalAlpha = 0.35;
				ctx.fillRect(x, H - pad - barH, Math.max(barW - 0.5, 1.5), barH);
				ctx.globalAlpha = 1;
			}
		}
	}

	destroy(): void {
		this.canvas.remove();
	}
}

// ── MiniSparkline ────────────────────────────────────────────────

interface MiniSparklineOpts {
	width?: number;
	height?: number;
	color?: string;
}

export class MiniSparkline {
	private canvas: HTMLCanvasElement;
	private ctx: CanvasRenderingContext2D;
	private color: string;
	private cssW: number;
	private cssH: number;
	private getData: () => number[];
	private needsResize = true;

	constructor(container: HTMLElement, getData: () => number[], opts?: MiniSparklineOpts) {
		this.getData = getData;
		this.cssW = opts?.width ?? 80;
		this.cssH = opts?.height ?? 28;
		this.color = opts?.color ?? "#6366f1";

		this.canvas = document.createElement("canvas");
		this.canvas.style.width = `${this.cssW}px`;
		this.canvas.style.height = `${this.cssH}px`;
		this.canvas.style.display = "block";
		this.canvas.style.flexShrink = "0";
		container.appendChild(this.canvas);
		this.ctx = this.canvas.getContext("2d")!;
	}

	render(): void {
		if (this.needsResize) {
			this.ctx = syncCanvasDPR(this.canvas, this.cssW, this.cssH);
			this.needsResize = false;
		}

		const ctx = this.ctx;
		const W = this.cssW;
		const H = this.cssH;
		const data = this.getData();

		ctx.clearRect(0, 0, W, H);
		if (data.length < 2) return;

		const { min, max } = autoScale(data);
		const range = max - min || 1;
		const step = W / (data.length - 1);

		// Fill
		const grad = ctx.createLinearGradient(0, 0, 0, H);
		grad.addColorStop(0, this.hexToRGBA(this.color, 0.3));
		grad.addColorStop(1, this.hexToRGBA(this.color, 0.0));

		ctx.beginPath();
		for (let i = 0; i < data.length; i++) {
			const x = i * step;
			const y = H - ((data[i] - min) / range) * (H - 4) - 2;
			if (i === 0) ctx.moveTo(x, y);
			else ctx.lineTo(x, y);
		}
		ctx.lineTo((data.length - 1) * step, H);
		ctx.lineTo(0, H);
		ctx.closePath();
		ctx.fillStyle = grad;
		ctx.fill();

		// Line
		ctx.beginPath();
		for (let i = 0; i < data.length; i++) {
			const x = i * step;
			const y = H - ((data[i] - min) / range) * (H - 4) - 2;
			if (i === 0) ctx.moveTo(x, y);
			else ctx.lineTo(x, y);
		}
		ctx.strokeStyle = this.color;
		ctx.lineWidth = 1.5;
		ctx.lineJoin = "round";
		ctx.stroke();
	}

	private hexToRGBA(hex: string, alpha: number): string {
		if (hex.startsWith("#")) {
			const r = parseInt(hex.slice(1, 3), 16);
			const g = parseInt(hex.slice(3, 5), 16);
			const b = parseInt(hex.slice(5, 7), 16);
			return `rgba(${r},${g},${b},${alpha})`;
		}
		return `rgba(100,102,241,${alpha})`;
	}

	destroy(): void {
		this.canvas.remove();
	}
}

// ── Chart Legend ──────────────────────────────────────────────────

export function createChartLegend(container: HTMLElement, items: { label: string; color: string }[]): HTMLElement {
	const legend = document.createElement("div");
	legend.className = "chart-legend";
	for (const item of items) {
		const el = document.createElement("div");
		el.className = "chart-legend-item";

		const swatch = document.createElement("span");
		swatch.className = "chart-legend-swatch";
		swatch.style.background = item.color;
		el.appendChild(swatch);

		const label = document.createElement("span");
		label.textContent = item.label;
		el.appendChild(label);

		legend.appendChild(el);
	}
	container.appendChild(legend);
	return legend;
}

// ── SCTE35Timeline ───────────────────────────────────────────────

import type { ServerSCTE35Event } from "./transport";

interface SCTE35TimelineOpts {
	height?: number;
	windowMs?: number;
}

export class SCTE35Timeline {
	private canvas: HTMLCanvasElement;
	private ctx: CanvasRenderingContext2D;
	private events: ServerSCTE35Event[] = [];
	private cssH: number;
	private windowMs: number;
	private cssW = 0;

	constructor(container: HTMLElement, opts?: SCTE35TimelineOpts) {
		this.cssH = opts?.height ?? 60;
		this.windowMs = opts?.windowMs ?? 5 * 60 * 1000;
		this.canvas = document.createElement("canvas");
		this.canvas.style.width = "100%";
		this.canvas.style.height = `${this.cssH}px`;
		this.canvas.style.display = "block";
		this.canvas.style.borderRadius = "4px";
		container.appendChild(this.canvas);
		this.ctx = this.canvas.getContext("2d")!;
	}

	update(events: ServerSCTE35Event[]): void {
		this.events = events;
	}

	render(): void {
		const rect = this.canvas.getBoundingClientRect();
		const w = rect.width;
		if (w === 0) return;

		if (Math.abs(w - this.cssW) > 1) {
			this.cssW = w;
			this.ctx = syncCanvasDPR(this.canvas, w, this.cssH);
		}

		const ctx = this.ctx;
		const W = w;
		const H = this.cssH;
		const timeAxisH = 16;
		const plotH = H - timeAxisH;

		ctx.clearRect(0, 0, W, H);

		const now = Date.now();
		const start = now - this.windowMs;

		// Time axis
		ctx.strokeStyle = getCSSVar("--border-subtle") || "rgba(255,255,255,0.06)";
		ctx.lineWidth = 1;
		ctx.beginPath();
		ctx.moveTo(0, plotH);
		ctx.lineTo(W, plotH);
		ctx.stroke();

		ctx.font = `9px ${getCSSVar("--font-mono") || "monospace"}`;
		ctx.fillStyle = getCSSVar("--text-tertiary") || "#4e4e66";
		ctx.textAlign = "center";
		ctx.textBaseline = "top";

		// Tick marks every 30s, labels every 60s
		const tickInterval = 30_000;
		const labelInterval = 60_000;
		const firstTick = Math.ceil(start / tickInterval) * tickInterval;
		for (let t = firstTick; t <= now; t += tickInterval) {
			const x = ((t - start) / this.windowMs) * W;
			ctx.strokeStyle = getCSSVar("--border-subtle") || "rgba(255,255,255,0.06)";
			ctx.beginPath();
			ctx.moveTo(x, plotH);
			ctx.lineTo(x, plotH + 4);
			ctx.stroke();

			if (t % labelInterval === 0) {
				const ago = Math.round((now - t) / 1000);
				ctx.fillText(ago > 0 ? `-${ago}s` : "now", x, plotH + 4);
			}
		}

		// Events
		const spliceColor = getCSSVar("--prism-3") || "#c084fc";
		const signalColor = getCSSVar("--prism-4") || "#f472b6";

		for (const evt of this.events) {
			const evtTime = evt.receivedAt;
			if (evtTime < start) continue;

			const x = ((evtTime - start) / this.windowMs) * W;
			const isSplice = evt.commandType === "splice_insert";
			const color = isSplice ? spliceColor : signalColor;
			const markerY = plotH / 2;

			// Duration bar
			if (evt.duration && evt.duration > 0) {
				const durPx = (evt.duration / this.windowMs) * W;
				ctx.fillStyle = color;
				ctx.globalAlpha = 0.15;
				ctx.fillRect(x, markerY - 4, durPx, 8);
				ctx.globalAlpha = 1;
			}

			// Marker
			ctx.fillStyle = color;
			if (isSplice) {
				// Diamond
				ctx.beginPath();
				ctx.moveTo(x, markerY - 6);
				ctx.lineTo(x + 5, markerY);
				ctx.lineTo(x, markerY + 6);
				ctx.lineTo(x - 5, markerY);
				ctx.closePath();
				ctx.fill();
			} else {
				// Circle
				ctx.beginPath();
				ctx.arc(x, markerY, 4, 0, Math.PI * 2);
				ctx.fill();
			}
		}
	}

	destroy(): void {
		this.canvas.remove();
	}
}
