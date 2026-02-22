import type { PrismAudioDecoder } from "./audio-decoder";
import { DB_MIN, DB_RANGE, linearToDb, dbToFraction } from "./audio-utils";

const QUADRANT_HEIGHT_FRAC = 0.38;
const QUADRANT_WIDTH_FRAC = 0.42;
const PADDING = 8;
const DEFAULT_BOTTOM_PADDING = 52;
const MIN_BAR_WIDTH = 4;
const MAX_BAR_WIDTH = 20;
const MIN_PAIR_GAP = 3;
const MIN_BAR_GAP = 1;
const LABEL_HEIGHT = 14;
const BG_ALPHA = 0.25;

const GREEN_END = (-12 - DB_MIN) / DB_RANGE;
const YELLOW_END = (0 - DB_MIN) / DB_RANGE;

interface Quadrant {
	x: number;
	y: number;
	w: number;
	h: number;
	alignRight: boolean;
}

function shortLabel(label: string | undefined, trackIndex: number): string {
	if (!label) return String(trackIndex + 1);
	const t = label.trim().toLowerCase();
	if (/^[a-z]{2}$/.test(t)) return t;
	if (/^[a-z]{3}$/.test(t)) return t.slice(0, 2);
	return String(trackIndex + 1);
}

interface HitRegion {
	x: number;
	y: number;
	w: number;
	h: number;
	trackIndex: number;
}

/**
 * Renders broadcast-style VU meters for multiple audio tracks onto a
 * canvas overlay. Meters are arranged in quadrants to avoid obscuring
 * the video. Supports stereo bar visualization with peak-hold indicators,
 * a green/yellow/red gradient, and click-to-select track switching.
 * Can operate in condensed mode for multiview tiles.
 */
export class VUMeter {
	private canvas: HTMLCanvasElement;
	private ctx: CanvasRenderingContext2D;
	private decoders: Map<number, PrismAudioDecoder>;
	private labels: Map<number, string> = new Map();
	private trackIndices: number[] = [];
	private animationId: number | null = null;
	private visible = false;
	private hitRegions: HitRegion[] = [];
	private activeTrack = -1;
	private bottomPadding = DEFAULT_BOTTOM_PADDING;
	private captionsActive = false;
	private onTrackSelect: ((trackIndex: number) => void) | null = null;
	private onLeftWidthChange: ((cssPixels: number) => void) | null = null;
	private clickHandler: ((e: MouseEvent) => void) | null = null;
	private lastLeftWidth = 0;
	private throttleMs = 0;
	private lastRenderTime = 0;
	private condensed = false;
	private _externallyDriven = false;
	private _lastSyncTime = 0;
	private _cachedGradient: CanvasGradient | null = null;
	private _gradientKey = "";

	constructor(canvas: HTMLCanvasElement, decoders: Map<number, PrismAudioDecoder>) {
		this.canvas = canvas;
		this.ctx = canvas.getContext("2d")!;
		this.decoders = decoders;

		this.clickHandler = (e: MouseEvent) => this.handleClick(e);
		this.canvas.addEventListener("click", this.clickHandler);
	}

	setBottomPadding(px: number): void {
		this.bottomPadding = px;
	}

	setCaptionsActive(active: boolean): void {
		this.captionsActive = active;
	}

	setOnTrackSelect(cb: (trackIndex: number) => void): void {
		this.onTrackSelect = cb;
	}

	setActiveTrack(trackIndex: number): void {
		this.activeTrack = trackIndex;
	}

	getLeftWidth(): number {
		return this.lastLeftWidth;
	}

	setOnLeftWidthChange(cb: (cssPixels: number) => void): void {
		this.onLeftWidthChange = cb;
	}

	setThrottleMs(ms: number): void {
		this.throttleMs = ms;
	}

	setCondensed(condensed: boolean): void {
		this.condensed = condensed;
	}

	set externallyDriven(v: boolean) {
		this._externallyDriven = v;
	}

	renderOnce(): void {
		if (!this.visible) return;
		const now = performance.now();
		if (this.throttleMs > 0 && now - this.lastRenderTime < this.throttleMs) return;
		this.lastRenderTime = now;
		this.render();
	}

	private handleClick(e: MouseEvent): void {
		if (!this.visible || !this.onTrackSelect) return;

		const rect = this.canvas.getBoundingClientRect();
		const dpr = window.devicePixelRatio || 1;
		const cx = (e.clientX - rect.left) * dpr;
		const cy = (e.clientY - rect.top) * dpr;

		for (const region of this.hitRegions) {
			if (cx >= region.x && cx <= region.x + region.w &&
				cy >= region.y && cy <= region.y + region.h) {
				this.onTrackSelect(region.trackIndex);
				return;
			}
		}
	}

	setDecoders(decoders: Map<number, PrismAudioDecoder>): void {
		this.decoders = decoders;
		this.rebuildTrackIndices();
	}

	private rebuildTrackIndices(): void {
		if (this.condensed) {
			this.trackIndices = [...this.decoders.entries()]
				.filter(([, d]) => d.isMetering())
				.map(([idx]) => idx)
				.sort((a, b) => a - b);
		} else {
			this.trackIndices = [...this.decoders.keys()].sort((a, b) => a - b);
		}
	}

	setLabels(labels: Map<number, string>): void {
		this.labels = labels;
	}

	show(): void {
		this.visible = true;
		this.canvas.style.display = "block";
		this.canvas.style.pointerEvents = "auto";
		this.canvas.style.cursor = "pointer";
		this.rebuildTrackIndices();
		if (!this._externallyDriven && !this.animationId) {
			this.renderLoop();
		}
	}

	hide(): void {
		this.visible = false;
		this.canvas.style.display = "none";
		this.canvas.style.pointerEvents = "none";
		this.canvas.style.cursor = "";
		if (this.animationId !== null) {
			cancelAnimationFrame(this.animationId);
			this.animationId = null;
		}
		if (this.lastLeftWidth !== 0) {
			this.lastLeftWidth = 0;
			if (this.onLeftWidthChange) this.onLeftWidthChange(0);
		}
	}

	destroy(): void {
		this.hide();
		if (this.clickHandler) {
			this.canvas.removeEventListener("click", this.clickHandler);
			this.clickHandler = null;
		}
	}

	private renderLoop = (): void => {
		if (!this.visible) return;
		this.animationId = requestAnimationFrame(this.renderLoop);
		if (this.throttleMs > 0) {
			const now = performance.now();
			if (now - this.lastRenderTime < this.throttleMs) return;
			this.lastRenderTime = now;
		}
		this.render();
	};

	private syncCanvasSize(now: number): void {
		if (now - this._lastSyncTime < 500) return;
		this._lastSyncTime = now;
		const rect = this.canvas.getBoundingClientRect();
		const dpr = window.devicePixelRatio || 1;
		const maxDim = this.condensed ? 400 : 0;
		let w = Math.round(rect.width * dpr);
		let h = Math.round(rect.height * dpr);
		if (maxDim > 0 && w > maxDim) {
			const scale = maxDim / w;
			w = maxDim;
			h = Math.round(h * scale);
		}
		if (this.canvas.width !== w || this.canvas.height !== h) {
			this.canvas.width = w;
			this.canvas.height = h;
			this._cachedGradient = null;
		}
	}

	private render(): void {
		this.syncCanvasSize(performance.now());
		const ctx = this.ctx;
		const W = this.canvas.width;
		const H = this.canvas.height;
		if (W < 10 || H < 10) return;
		const dpr = window.devicePixelRatio || 1;

		ctx.clearRect(0, 0, W, H);
		this.hitRegions = [];

		const trackCount = this.trackIndices.length;
		if (trackCount === 0) {
			this.lastLeftWidth = 0;
			return;
		}

		const quadrants = this.computeQuadrants(W, H, trackCount);
		const tracksPerQuadrant = Math.ceil(trackCount / quadrants.length);

		let maxLeftPx = 0;
		for (let qi = 0; qi < quadrants.length; qi++) {
			const q = quadrants[qi];
			const startIdx = qi * tracksPerQuadrant;
			const endIdx = Math.min(startIdx + tracksPerQuadrant, trackCount);
			const qTracks = this.trackIndices.slice(startIdx, endIdx);
			if (qTracks.length === 0) continue;

			const usedWidth = this.renderQuadrant(ctx, q, qTracks);
			if (!q.alignRight) {
				maxLeftPx = Math.max(maxLeftPx, q.x + usedWidth);
			}
		}

		const newLeft = Math.ceil(maxLeftPx / dpr);
		if (newLeft !== this.lastLeftWidth) {
			this.lastLeftWidth = newLeft;
			if (this.onLeftWidthChange) this.onLeftWidthChange(newLeft);
		}
	}

	private computeQuadrants(W: number, H: number, trackCount: number): Quadrant[] {
		if (this.condensed) {
			const qh = Math.floor(H * 0.45);
			const qw = Math.floor(W * 0.35);
			return [{ x: PADDING, y: PADDING, w: qw, h: qh, alignRight: false }];
		}

		const qh = Math.floor(H * QUADRANT_HEIGHT_FRAC);
		const qw = Math.floor(W * QUADRANT_WIDTH_FRAC);

		const tl: Quadrant = { x: PADDING, y: PADDING, w: qw, h: qh, alignRight: false };
		const tr: Quadrant = { x: W - PADDING, y: PADDING, w: qw, h: qh, alignRight: true };

		if (this.captionsActive) {
			if (trackCount <= 4) return [tl];
			return [tl, tr];
		}

		const bl: Quadrant = { x: PADDING, y: H - this.bottomPadding - qh, w: qw, h: qh, alignRight: false };
		const br: Quadrant = { x: W - PADDING, y: H - this.bottomPadding - qh, w: qw, h: qh, alignRight: true };

		if (trackCount <= 2) return [tl];
		if (trackCount <= 4) return [tl, tr];
		return [tl, tr, bl, br];
	}

	private renderQuadrant(ctx: CanvasRenderingContext2D, q: Quadrant, tracks: number[]): number {
		const numPairs = tracks.length;
		const available = q.w;

		const minGaps = (numPairs - 1) * MIN_PAIR_GAP;
		const perPair = Math.floor((available - minGaps) / numPairs);
		const barW = Math.max(MIN_BAR_WIDTH, Math.min(MAX_BAR_WIDTH, Math.floor((perPair - MIN_BAR_GAP) / 2)));
		const barGap = Math.max(MIN_BAR_GAP, Math.min(3, Math.floor(barW / 4)));
		const pairW = barW * 2 + barGap;
		const pairGap = Math.max(MIN_PAIR_GAP, Math.floor(barW * 0.8));
		const totalWidth = numPairs * pairW + (numPairs - 1) * pairGap;

		const meterHeight = this.condensed ? q.h : q.h - LABEL_HEIGHT - 2;

		if (this.condensed) {
			// Minimal draw path for multiview: bars only, no text, no hit regions
			const grad = this.getBarGradient(ctx, q.y, meterHeight);
			for (let i = 0; i < numPairs; i++) {
				const trackIdx = tracks[i];
				const decoder = this.decoders.get(trackIdx);
				const levels = decoder?.getLevels();

				const pairX = q.x + i * (pairW + pairGap);

				const peakL = levels?.peak[0] ?? 0;
				const peakR = levels?.peak[1] ?? peakL;

				// Background
				ctx.fillStyle = `rgba(30, 30, 30, ${BG_ALPHA})`;
				ctx.fillRect(pairX, q.y, barW, meterHeight);
				ctx.fillRect(pairX + barW + barGap, q.y, barW, meterHeight);

				// Level bars
				const hL = Math.round(dbToFraction(linearToDb(peakL)) * meterHeight);
				const hR = Math.round(dbToFraction(linearToDb(peakR)) * meterHeight);
				if (hL > 0) {
					ctx.fillStyle = grad;
					ctx.fillRect(pairX, q.y + meterHeight - hL, barW, hL);
				}
				if (hR > 0) {
					ctx.fillStyle = grad;
					ctx.fillRect(pairX + barW + barGap, q.y + meterHeight - hR, barW, hR);
				}
			}
			return totalWidth;
		}

		const fontSize = Math.max(9, Math.min(14, barW + 4));
		const hitPad = 2;

		for (let i = 0; i < numPairs; i++) {
			const trackIdx = tracks[i];
			const decoder = this.decoders.get(trackIdx);
			const levels = decoder?.getLevels();
			const isActive = trackIdx === this.activeTrack;

			let pairX: number;
			if (q.alignRight) {
				pairX = q.x - totalWidth + i * (pairW + pairGap);
			} else {
				pairX = q.x + i * (pairW + pairGap);
			}

			this.hitRegions.push({
				x: pairX - hitPad,
				y: q.y - hitPad,
				w: pairW + hitPad * 2,
				h: q.h + hitPad * 2,
				trackIndex: trackIdx,
			});

			if (isActive) {
				ctx.strokeStyle = "rgba(59, 130, 246, 0.8)";
				ctx.lineWidth = 2;
				ctx.strokeRect(pairX - 3, q.y - 3, pairW + 6, meterHeight + LABEL_HEIGHT + 4);
			}

			const peakL = levels?.peak[0] ?? 0;
			const peakR = levels?.peak[1] ?? peakL;
			const holdL = levels?.peakHold[0] ?? 0;
			const holdR = levels?.peakHold[1] ?? holdL;

			this.renderBar(ctx, pairX, q.y, barW, meterHeight, peakL, holdL);
			this.renderBar(ctx, pairX + barW + barGap, q.y, barW, meterHeight, peakR, holdR);

			const label = shortLabel(this.labels.get(trackIdx), trackIdx);
			const labelY = q.y + meterHeight + LABEL_HEIGHT - 2;
			const labelX = pairX + pairW / 2;
			ctx.font = isActive ? `bold ${fontSize}px sans-serif` : `${fontSize}px sans-serif`;
			ctx.textAlign = "center";
			ctx.textBaseline = "middle";
			ctx.strokeStyle = "rgba(0, 0, 0, 0.9)";
			ctx.lineWidth = 2.5;
			ctx.strokeText(label, labelX, labelY);
			ctx.fillStyle = isActive ? "rgba(59, 130, 246, 1)" : "rgba(255, 255, 255, 0.95)";
			ctx.fillText(label, labelX, labelY);
		}
		return totalWidth;
	}

	private renderBar(
		ctx: CanvasRenderingContext2D,
		x: number, qY: number,
		width: number, height: number,
		level: number, peakHold: number,
	): void {
		ctx.globalAlpha = 1;

		ctx.fillStyle = `rgba(30, 30, 30, ${BG_ALPHA})`;
		ctx.fillRect(x, qY, width, height);

		const levelDb = linearToDb(level);
		const levelFrac = dbToFraction(levelDb);
		const barHeight = Math.round(levelFrac * height);

		if (barHeight > 0) {
			this.drawGradientBar(ctx, x, qY, width, height, barHeight);
		}

		const holdDb = linearToDb(peakHold);
		const holdFrac = dbToFraction(holdDb);
		const holdPx = Math.round(holdFrac * height);
		if (holdPx > 1) {
			ctx.fillStyle = "#ffffff";
			ctx.fillRect(x, qY + height - holdPx, width, 1);
		}
	}

	private getBarGradient(ctx: CanvasRenderingContext2D, qY: number, height: number): CanvasGradient {
		const key = `${qY}:${height}`;
		if (this._cachedGradient && this._gradientKey === key) {
			return this._cachedGradient;
		}
		const bottom = qY + height;
		const top = qY;
		const grad = ctx.createLinearGradient(0, bottom, 0, top);
		grad.addColorStop(0, "#22c55e");
		grad.addColorStop(GREEN_END, "#22c55e");
		grad.addColorStop(GREEN_END + 0.001, "#eab308");
		grad.addColorStop(YELLOW_END, "#eab308");
		grad.addColorStop(YELLOW_END + 0.001, "#ef4444");
		grad.addColorStop(1, "#ef4444");
		this._cachedGradient = grad;
		this._gradientKey = key;
		return grad;
	}

	private drawGradientBar(
		ctx: CanvasRenderingContext2D,
		x: number, qY: number,
		width: number, height: number,
		barHeight: number,
	): void {
		ctx.fillStyle = this.getBarGradient(ctx, qY, height);
		ctx.fillRect(x, qY + height - barHeight, width, barHeight);
	}
}
