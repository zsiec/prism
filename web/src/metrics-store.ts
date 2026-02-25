import type { ServerStats, ServerAudioTrackStats, ServerSCTE35Event } from "./transport";
import type { RendererStats } from "./renderer";

const HISTORY_SIZE = 60;
const SYNC_HISTORY_SIZE = 120;
const RENDERER_HISTORY_SIZE = 120;
const SYNC_PUSH_INTERVAL_MS = 1000;
const RENDERER_PUSH_INTERVAL_MS = 1000;
const FRAME_EVENT_CAP = 300;
const SCTE35_ACC_CAP = 200;

export interface FrameEvent {
	isKey: boolean;
	size: number;
	ts: number;
}

class RingBuffer {
	private buf: number[];
	private head = 0;
	private count = 0;

	constructor(private capacity: number) {
		this.buf = new Array(capacity).fill(0);
	}

	push(val: number): void {
		this.buf[this.head] = val;
		this.head = (this.head + 1) % this.capacity;
		if (this.count < this.capacity) this.count++;
	}

	toArray(): number[] {
		if (this.count === 0) return [];
		const out: number[] = new Array(this.count);
		const start = (this.head - this.count + this.capacity) % this.capacity;
		for (let i = 0; i < this.count; i++) {
			out[i] = this.buf[(start + i) % this.capacity];
		}
		return out;
	}

	last(): number {
		if (this.count === 0) return 0;
		return this.buf[(this.head - 1 + this.capacity) % this.capacity];
	}
}

export interface VideoMetrics {
	codec: string;
	width: number;
	height: number;
	totalFrames: number;
	keyFrames: number;
	deltaFrames: number;
	currentGOPLen: number;
	serverBitrateKbps: number;
	serverFrameRate: number;
	ptsErrors: number;
	decodeFps: number;
	renderFps: number;
	decodeQueueDepth: number;
	decodeQueueMs: number;
	clientDropped: number;
	fpsHistory: number[];
	bitrateHistory: number[];
	renderFpsHistory: number[];
	decodeQueueHistory: number[];
	frameDropsHistory: number[];
	timecode: string;
}

export interface AudioMetrics {
	tracks: ServerAudioTrackStats[];
	bufferMs: number;
	silenceMs: number;
	bufferHistory: number[];
}

export interface SyncMetrics {
	offsetMs: number;
	offsetHistory: number[];
	driftRateMsPerSec: number;
	corrections: number;
}

export interface TransportMetrics {
	protocol: string;
	uptimeMs: number;
	viewerCount: number;
	serverBytesSent: number;
	receiveBitrateKbps: number;
}

export interface CaptionMetrics {
	activeChannels: number[];
	totalFrames: number;
}

export type HealthStatus = "good" | "warn" | "critical";

export interface StreamInfo {
	videoCodec: string;
	resolution: string;
	frameRate: string;
	bitrate: string;
	audioCodec: string;
	audioConfig: string;
	audioTrackCount: number;
	protocol: string;
	uptimeMs: number;
}

export interface ErrorCounters {
	ptsErrors: number;
	clientDropped: number;
	serverVideoDropped: number;
	serverAudioDropped: number;
}

interface HUDState {
	video: { label: string; status: HealthStatus };
	audio: { label: string; status: HealthStatus };
	sync: { label: string; status: HealthStatus };
}

/**
 * Centralized store for all player metrics: server stats, renderer stats,
 * transport diagnostics, and decode performance. Aggregates data from
 * multiple subsystems into snapshots consumed by the HUD and detail panels.
 */
export class MetricsStore {
	private serverStats: ServerStats | null = null;
	private rendererStats: RendererStats | null = null;
	private decodeFps = 0;
	private renderFps = 0;
	private audioBufferMs = 0;
	private silenceMs = 0;
	private lastSyncOffset = 0;
	private syncCorrections = 0;

	private fpsRing = new RingBuffer(HISTORY_SIZE);
	private bitrateRing = new RingBuffer(HISTORY_SIZE);
	private renderFpsRing = new RingBuffer(HISTORY_SIZE);
	private syncOffsetRing = new RingBuffer(SYNC_HISTORY_SIZE);
	private decodeQueueRing = new RingBuffer(RENDERER_HISTORY_SIZE);
	private audioBufferRing = new RingBuffer(RENDERER_HISTORY_SIZE);
	private frameDropsRing = new RingBuffer(RENDERER_HISTORY_SIZE);
	private lastSyncPushTime = 0;
	private lastRendererPushTime = 0;
	private prevVideoTotalDiscarded = 0;

	private prevOffset = 0;
	private prevOffsetTime = 0;
	private driftRate = 0;

	private receiveKbps = 0;

	private _dirty = false;

	private updateListeners: (() => void)[] = [];

	private frameRing: FrameEvent[] = [];
	private scte35Acc: ServerSCTE35Event[] = [];
	private scte35SeenKeys = new Set<string>();

	get dirty(): boolean { return this._dirty; }
	clearDirty(): void { this._dirty = false; }

	addUpdateListener(cb: () => void): void {
		this.updateListeners.push(cb);
	}

	removeUpdateListener(cb: () => void): void {
		this.updateListeners = this.updateListeners.filter(l => l !== cb);
	}

	updateServerStats(stats: ServerStats): void {
		this.serverStats = stats;
		this._dirty = true;

		this.fpsRing.push(stats.video.frameRate);
		this.bitrateRing.push(stats.video.bitrateKbps);
		this.renderFpsRing.push(this.renderFps);

		// Accumulate SCTE-35 events with dedup
		const recent = stats.scte35?.recent;
		if (recent) {
			for (const evt of recent) {
				const key = `${evt.pts}:${evt.commandType}:${evt.receivedAt}`;
				if (!this.scte35SeenKeys.has(key)) {
					this.scte35SeenKeys.add(key);
					this.scte35Acc.push(evt);
					if (this.scte35Acc.length > SCTE35_ACC_CAP) {
						const removed = this.scte35Acc.shift();
						if (removed) {
							this.scte35SeenKeys.delete(`${removed.pts}:${removed.commandType}:${removed.receivedAt}`);
						}
					}
				}
			}
		}

		for (const cb of this.updateListeners) cb();
	}

	updateRendererStats(stats: RendererStats): void {
		this.rendererStats = stats;

		const now = performance.now();

		if (stats.currentAudioPTS >= 0 && stats.currentVideoPTS >= 0) {
			const offsetMs = (stats.currentVideoPTS - stats.currentAudioPTS) / 1000;
			this.lastSyncOffset = offsetMs;

			// Throttle ring buffer pushes to ~1Hz so the chart scrolls at a readable pace.
			// The ring holds 120 samples → 2 minutes of visible history.
			if (now - this.lastSyncPushTime >= SYNC_PUSH_INTERVAL_MS) {
				this.syncOffsetRing.push(offsetMs);
				this.lastSyncPushTime = now;
			}

			if (this.prevOffsetTime > 0) {
				const dt = (now - this.prevOffsetTime) / 1000;
				if (dt > 0) {
					this.driftRate = (offsetMs - this.prevOffset) / dt;
				}
			}
			this.prevOffset = offsetMs;
			this.prevOffsetTime = now;
		}

		// Push decode queue, audio buffer, and frame drops at 1Hz
		if (now - this.lastRendererPushTime >= RENDERER_PUSH_INTERVAL_MS) {
			this.decodeQueueRing.push(stats.videoQueueLengthMs ?? 0);
			this.audioBufferRing.push(this.audioBufferMs);

			const discarded = stats.videoTotalDiscarded ?? 0;
			const delta = Math.max(0, discarded - this.prevVideoTotalDiscarded);
			this.frameDropsRing.push(delta);
			this.prevVideoTotalDiscarded = discarded;

			this.lastRendererPushTime = now;
		}
	}

	updateRenderFps(fps: number): void {
		this.renderFps = fps;
	}

	updateAudioStats(bufferMs: number, silenceMs: number): void {
		this.audioBufferMs = bufferMs;
		this.silenceMs = silenceMs;
	}

	getVideoMetrics(): VideoMetrics {
		const sv = this.serverStats?.video;
		return {
			codec: sv?.codec ?? "—",
			width: sv?.width ?? 0,
			height: sv?.height ?? 0,
			totalFrames: sv?.totalFrames ?? 0,
			keyFrames: sv?.keyFrames ?? 0,
			deltaFrames: sv?.deltaFrames ?? 0,
			currentGOPLen: sv?.currentGOPLen ?? 0,
			serverBitrateKbps: sv?.bitrateKbps ?? 0,
			serverFrameRate: sv?.frameRate ?? 0,
			ptsErrors: sv?.ptsErrors ?? 0,
			decodeFps: this.decodeFps,
			renderFps: this.renderFps,
			decodeQueueDepth: this.rendererStats?.videoQueueSize ?? 0,
			decodeQueueMs: this.rendererStats?.videoQueueLengthMs ?? 0,
			clientDropped: this.rendererStats?.videoTotalDiscarded ?? 0,
			fpsHistory: this.fpsRing.toArray(),
			bitrateHistory: this.bitrateRing.toArray(),
			renderFpsHistory: this.renderFpsRing.toArray(),
			decodeQueueHistory: this.decodeQueueRing.toArray(),
			frameDropsHistory: this.frameDropsRing.toArray(),
			timecode: sv?.timecode ?? "",
		};
	}

	getAudioMetrics(): AudioMetrics {
		return {
			tracks: this.serverStats?.audio ?? [],
			bufferMs: this.audioBufferMs,
			silenceMs: this.silenceMs,
			bufferHistory: this.audioBufferRing.toArray(),
		};
	}

	getSyncMetrics(): SyncMetrics {
		return {
			offsetMs: this.lastSyncOffset,
			offsetHistory: this.syncOffsetRing.toArray(),
			driftRateMsPerSec: this.driftRate,
			corrections: this.syncCorrections,
		};
	}

	getTransportMetrics(): TransportMetrics {
		return {
			protocol: this.serverStats?.protocol ?? "—",
			uptimeMs: this.serverStats?.uptimeMs ?? 0,
			viewerCount: this.serverStats?.viewerCount ?? 0,
			serverBytesSent: 0,
			receiveBitrateKbps: this.receiveKbps,
		};
	}

	getCaptionMetrics(): CaptionMetrics {
		return {
			activeChannels: this.serverStats?.captions?.activeChannels ?? [],
			totalFrames: this.serverStats?.captions?.totalFrames ?? 0,
		};
	}

	getStreamInfo(): StreamInfo {
		const sv = this.serverStats?.video;
		const a = this.serverStats?.audio ?? [];
		const bitrateKbps = sv?.bitrateKbps ?? 0;
		return {
			videoCodec: sv?.codec ?? "",
			resolution: sv && sv.width > 0 ? `${sv.width}×${sv.height}` : "",
			frameRate: sv && sv.frameRate > 0 ? `${sv.frameRate.toFixed(2)}fps` : "",
			bitrate: bitrateKbps >= 1000 ? `${(bitrateKbps / 1000).toFixed(1)} Mbps` : bitrateKbps > 0 ? `${Math.round(bitrateKbps)} kbps` : "",
			audioCodec: a.length > 0 ? a[0].codec : "",
			audioConfig: a.length > 0
				? `${(a[0].sampleRate / 1000).toFixed(0)}kHz ${a[0].channels}ch`
				: "",
			audioTrackCount: a.length,
			protocol: this.serverStats?.protocol ?? "",
			uptimeMs: this.serverStats?.uptimeMs ?? 0,
		};
	}

	getErrorCounters(): ErrorCounters {
		const sv = this.serverStats?.video;
		const viewers = this.serverStats?.viewers ?? [];
		let serverVideoDropped = 0;
		let serverAudioDropped = 0;
		for (const v of viewers) {
			serverVideoDropped += v.videoDropped;
			serverAudioDropped += v.audioDropped;
		}
		return {
			ptsErrors: sv?.ptsErrors ?? 0,
			clientDropped: this.rendererStats?.videoTotalDiscarded ?? 0,
			serverVideoDropped,
			serverAudioDropped,
		};
	}

	getHUDState(): HUDState {
		const v = this.getVideoMetrics();
		const a = this.getAudioMetrics();
		const s = this.getSyncMetrics();

		const videoStatus = this.assessVideoHealth(v);
		const audioStatus = this.assessAudioHealth(a);
		const syncStatus = this.assessSyncHealth(s);

		const fpsLabel = v.serverFrameRate > 0 ? `${Math.round(v.serverFrameRate)}fps` : "\u2014";

		let audioLabel: string;
		if (audioStatus === "critical") audioLabel = "underrun";
		else if (audioStatus === "warn") audioLabel = "low buf";
		else audioLabel = "ok";

		const syncSign = s.offsetMs >= 0 ? "+" : "\u2212";
		const syncLabel = `${syncSign}${Math.abs(s.offsetMs).toFixed(0)}ms`;

		return {
			video: { label: fpsLabel, status: videoStatus },
			audio: { label: audioLabel, status: audioStatus },
			sync: { label: syncLabel, status: syncStatus },
		};
	}

	private assessVideoHealth(v: VideoMetrics): HealthStatus {
		if (v.serverFrameRate > 0 && v.decodeFps > 0) {
			const ratio = v.decodeFps / v.serverFrameRate;
			if (ratio < 0.5) return "critical";
			if (ratio < 0.8) return "warn";
		}
		if (v.ptsErrors > 0) return "warn";
		return "good";
	}

	private assessAudioHealth(a: AudioMetrics): HealthStatus {
		if (a.bufferMs < 20) return "critical";
		if (a.bufferMs < 50) return "warn";
		if (a.silenceMs > 500) return "warn";
		return "good";
	}

	private assessSyncHealth(s: SyncMetrics): HealthStatus {
		const abs = Math.abs(s.offsetMs);
		if (abs > 200) return "critical";
		if (abs > 50) return "warn";
		return "good";
	}

	getTimecode(): string {
		return this.serverStats?.video?.timecode ?? "";
	}

	recordFrameEvent(isKey: boolean, size: number): void {
		this.frameRing.push({ isKey, size, ts: performance.now() });
		if (this.frameRing.length > FRAME_EVENT_CAP) {
			this.frameRing.shift();
		}
	}

	getFrameEvents(): FrameEvent[] {
		return this.frameRing;
	}

	getAccumulatedSCTE35(): ServerSCTE35Event[] {
		return this.scte35Acc;
	}

	getSCTE35Events(): ServerSCTE35Event[] {
		return this.serverStats?.scte35?.recent ?? [];
	}

	getSCTE35Total(): number {
		return this.serverStats?.scte35?.totalEvents ?? 0;
	}

	reset(): void {
		this.serverStats = null;
		this.rendererStats = null;
		this.decodeFps = 0;
		this.renderFps = 0;
		this.audioBufferMs = 0;
		this.silenceMs = 0;
		this.lastSyncOffset = 0;
		this.syncCorrections = 0;
		this.fpsRing = new RingBuffer(HISTORY_SIZE);
		this.bitrateRing = new RingBuffer(HISTORY_SIZE);
		this.renderFpsRing = new RingBuffer(HISTORY_SIZE);
		this.syncOffsetRing = new RingBuffer(SYNC_HISTORY_SIZE);
		this.decodeQueueRing = new RingBuffer(RENDERER_HISTORY_SIZE);
		this.audioBufferRing = new RingBuffer(RENDERER_HISTORY_SIZE);
		this.frameDropsRing = new RingBuffer(RENDERER_HISTORY_SIZE);
		this.lastSyncPushTime = 0;
		this.lastRendererPushTime = 0;
		this.prevVideoTotalDiscarded = 0;
		this.prevOffset = 0;
		this.prevOffsetTime = 0;
		this.driftRate = 0;
		this.receiveKbps = 0;
		this.frameRing = [];
		this.scte35Acc = [];
		this.scte35SeenKeys.clear();
	}
}
