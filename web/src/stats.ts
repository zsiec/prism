import type { RendererStats } from "./renderer";

export class PrismStats {
	private el: HTMLElement;
	private frameCount = 0;
	private lastFrameCount = 0;
	private fps = 0;
	private intervalId: ReturnType<typeof setInterval> | null = null;
	private startTime = 0;
	private _externallyDriven = false;

	private rendererStats: RendererStats | null = null;
	private audioQueueMs = 0;
	private silenceMs = 0;

	constructor(el: HTMLElement) {
		this.el = el;
	}

	set externallyDriven(v: boolean) {
		this._externallyDriven = v;
		if (v) {
			this.stop();
			this.el.style.display = "none";
		} else {
			this.el.style.display = "";
		}
	}

	start(): void {
		this.startTime = performance.now();
		this.frameCount = 0;
		this.lastFrameCount = 0;

		if (this._externallyDriven) return;

		this.intervalId = setInterval(() => {
			this.fps = this.frameCount - this.lastFrameCount;
			this.lastFrameCount = this.frameCount;
			this.render();
		}, 1000);
	}

	stop(): void {
		if (this.intervalId !== null) {
			clearInterval(this.intervalId);
			this.intervalId = null;
		}
	}

	onVideoFrame(): void {
		this.frameCount++;
	}

	onRendererStats(stats: RendererStats): void {
		this.rendererStats = stats;
	}

	updateAudioStats(queueMs: number, silenceMs: number): void {
		this.audioQueueMs = queueMs;
		this.silenceMs = silenceMs;
	}

	private render(): void {
		const uptime = ((performance.now() - this.startTime) / 1000).toFixed(0);

		const lines = [
			`FPS: ${this.fps}`,
			`Frames: ${this.frameCount}`,
			`Uptime: ${uptime}s`,
		];

		if (this.rendererStats) {
			const rs = this.rendererStats;
			lines.push(`V-Buf: ${rs.videoQueueSize} (${rs.videoQueueLengthMs.toFixed(0)}ms)`);
			lines.push(`V-Drop: ${rs.videoTotalDiscarded}`);

			if (rs.currentAudioPTS >= 0 && rs.currentVideoPTS >= 0) {
				const syncOffset = (rs.currentVideoPTS - rs.currentAudioPTS) / 1000;
				lines.push(`A/V: ${syncOffset.toFixed(0)}ms`);
			}
		}

		if (this.audioQueueMs > 0 || this.silenceMs > 0) {
			lines.push(`A-Buf: ${this.audioQueueMs.toFixed(0)}ms`);
			if (this.silenceMs > 0) {
				lines.push(`Silence: ${this.silenceMs.toFixed(0)}ms`);
			}
		}

		this.el.innerHTML = lines.join("<br>");
	}
}
