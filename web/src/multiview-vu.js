import { DB_MIN, DB_RANGE, linearToDb, dbToFraction } from "./audio-utils";
const GREEN_END = (-18 - DB_MIN) / DB_RANGE;
const YELLOW_END = (-6 - DB_MIN) / DB_RANGE;
const BAR_W = 3;
const BAR_GAP = 1;
const INSET = 6;
// Quadrant positions: each stereo pair gets one corner.
// Tracks 1-4 go TL, TR, BL, BR. Track 5+ overflow right of BL.
var Quadrant;
(function (Quadrant) {
    Quadrant[Quadrant["TL"] = 0] = "TL";
    Quadrant[Quadrant["TR"] = 1] = "TR";
    Quadrant[Quadrant["BL"] = 2] = "BL";
    Quadrant[Quadrant["BR"] = 3] = "BR";
})(Quadrant || (Quadrant = {}));
const QUADRANT_ORDER = [Quadrant.TL, Quadrant.TR, Quadrant.BL, Quadrant.BR];
/**
 * Renders compact VU meter bars for multiview tiles. Draws horizontal
 * dB-scaled level bars with green/yellow/red color zones on a shared canvas.
 */
export class MultiviewVURenderer {
    canvas;
    ctx;
    _cols = 3;
    _rows = 3;
    _gap = 2;
    _lastSyncTime = 0;
    _cachedGradients = new Map();
    constructor(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext("2d");
    }
    setGrid(cols, rows, gap) {
        this._cols = cols;
        this._rows = rows;
        this._gap = gap;
    }
    render(tiles) {
        this.syncCanvasSize();
        const W = this.canvas.width;
        const H = this.canvas.height;
        if (W < 10 || H < 10)
            return;
        this.ctx.clearRect(0, 0, W, H);
        const cellW = (W - this._gap * (this._cols + 1)) / this._cols;
        const cellH = (H - this._gap * (this._rows + 1)) / this._rows;
        for (let i = 0; i < tiles.length && i < this._cols * this._rows; i++) {
            const tile = tiles[i];
            if (!tile.streamKey)
                continue;
            const col = i % this._cols;
            const row = Math.floor(i / this._cols);
            const cellX = this._gap + col * (cellW + this._gap);
            const cellY = this._gap + row * (cellH + this._gap);
            const levels = tile.player.getAudioLevels();
            if (levels.length === 0)
                continue;
            this.renderTileVU(cellX, cellY, cellW, cellH, levels);
        }
    }
    renderTileVU(cellX, cellY, cellW, cellH, levels) {
        const dpr = Math.min(window.devicePixelRatio || 1, 1.5);
        const barW = Math.max(2, Math.round(BAR_W * dpr));
        const barGap = Math.max(1, Math.round(BAR_GAP * dpr));
        const inset = Math.round(INSET * dpr);
        const topInset = Math.round(24 * dpr);
        const bottomInset = Math.round(18 * dpr);
        const meterH = Math.max(16, Math.floor((cellH - topInset - bottomInset) * 0.35));
        const pairW = barW * 2 + barGap;
        for (let i = 0; i < levels.length; i++) {
            const lev = levels[i];
            let x, y;
            if (i < 4) {
                const q = QUADRANT_ORDER[i];
                switch (q) {
                    case Quadrant.TL:
                        x = cellX + inset;
                        y = cellY + topInset;
                        break;
                    case Quadrant.TR:
                        x = cellX + cellW - inset - pairW;
                        y = cellY + topInset;
                        break;
                    case Quadrant.BL:
                        x = cellX + inset;
                        y = cellY + cellH - bottomInset - meterH;
                        break;
                    case Quadrant.BR:
                        x = cellX + cellW - inset - pairW;
                        y = cellY + cellH - bottomInset - meterH;
                        break;
                }
            }
            else {
                // Overflow: stack horizontally starting after BL
                const overflowOffset = (i - 3) * (pairW + Math.round(4 * dpr));
                x = cellX + inset + overflowOffset;
                y = cellY + cellH - bottomInset - meterH;
            }
            this.renderPair(x, y, barW, barGap, meterH, lev);
        }
    }
    renderPair(x, y, barW, barGap, h, lev) {
        const ctx = this.ctx;
        const grad = this.getGradient(y, h);
        const peakL = lev.peak[0] ?? 0;
        const peakR = lev.peak[1] ?? peakL;
        const pad = 3;
        const wellW = barW * 2 + barGap + pad * 2;
        const wellH = h + pad * 2;
        const wellR = 3;
        ctx.fillStyle = "rgba(0, 0, 0, 0.4)";
        ctx.beginPath();
        ctx.roundRect(x - pad, y - pad, wellW, wellH, wellR);
        ctx.fill();
        ctx.fillStyle = "rgba(255, 255, 255, 0.04)";
        ctx.fillRect(x, y, barW, h);
        ctx.fillRect(x + barW + barGap, y, barW, h);
        const hL = Math.round(dbToFraction(linearToDb(peakL)) * h);
        const hR = Math.round(dbToFraction(linearToDb(peakR)) * h);
        if (hL > 0) {
            ctx.fillStyle = grad;
            ctx.fillRect(x, y + h - hL, barW, hL);
        }
        if (hR > 0) {
            ctx.fillStyle = grad;
            ctx.fillRect(x + barW + barGap, y + h - hR, barW, hR);
        }
        const holdL = lev.peakHold[0] ?? 0;
        const holdR = lev.peakHold[1] ?? holdL;
        const holdPxL = Math.round(dbToFraction(linearToDb(holdL)) * h);
        const holdPxR = Math.round(dbToFraction(linearToDb(holdR)) * h);
        if (holdPxL > 1) {
            ctx.fillStyle = "rgba(255, 255, 255, 0.6)";
            ctx.fillRect(x, y + h - holdPxL, barW, 1);
        }
        if (holdPxR > 1) {
            ctx.fillStyle = "rgba(255, 255, 255, 0.6)";
            ctx.fillRect(x + barW + barGap, y + h - holdPxR, barW, 1);
        }
    }
    getGradient(y, height) {
        const key = `${y}:${height}`;
        const cached = this._cachedGradients.get(key);
        if (cached)
            return cached;
        const bottom = y + height;
        const grad = this.ctx.createLinearGradient(0, bottom, 0, y);
        grad.addColorStop(0, "#22c55e");
        grad.addColorStop(GREEN_END, "#22c55e");
        grad.addColorStop(GREEN_END + 0.01, "#84cc16");
        grad.addColorStop(YELLOW_END, "#eab308");
        grad.addColorStop(YELLOW_END + 0.01, "#f97316");
        grad.addColorStop(1, "#ef4444");
        this._cachedGradients.set(key, grad);
        return grad;
    }
    syncCanvasSize() {
        const now = performance.now();
        if (now - this._lastSyncTime < 500)
            return;
        this._lastSyncTime = now;
        const dpr = Math.min(window.devicePixelRatio || 1, 1.5);
        const rect = this.canvas.getBoundingClientRect();
        const w = Math.round(rect.width * dpr);
        const h = Math.round(rect.height * dpr);
        if (this.canvas.width !== w || this.canvas.height !== h) {
            this.canvas.width = w;
            this.canvas.height = h;
            this._cachedGradients.clear();
        }
    }
    destroy() {
        this._cachedGradients.clear();
    }
}
