import { MoQTransport, MoQTransportCallbacks } from "./moq-transport";
import { TrackInfo, ServerStats, ServerViewerStats } from "./transport";
import { CaptionData } from "./protocol";
import { MuxStreamEntry, MuxStreamCallbacks, MuxViewerStats } from "./multiview-types";

/** Callbacks from the multiview transport to the multiview manager. */
export interface MoQMultiviewCallbacks {
	/** Called once before any streams are ready, for one-time setup. */
	onSetup: () => void | Promise<void>;
	/** Called per-stream as each transport becomes ready — no all-or-nothing barrier. */
	onStreamReady: (stream: MuxStreamEntry) => void | Promise<void>;
	/** Called once after all streams have been set up. */
	onAllReady: () => void;
	onMuxStats: (stats: Record<string, ServerStats>, viewerStats?: Record<string, MuxViewerStats>) => void;
	onClose: () => void;
	onError: (err: string) => void;
}

/**
 * MoQ multiview transport: manages N independent MoQTransport instances,
 * one per tile. Each tile starts receiving frames as soon as its own
 * transport completes the handshake + catalog exchange — no waiting for
 * slower peers.
 */
export class MoQMultiviewTransport {
	private streamKeys: string[];
	private callbacks: MoQMultiviewCallbacks;
	private transports: MoQTransport[] = [];
	private streamCallbacks = new Map<number, MuxStreamCallbacks>();
	private latestStats = new Map<string, ServerStats>();
	private latestViewerStats = new Map<string, MuxViewerStats>();
	private closed = false;
	private statsInterval: ReturnType<typeof setInterval> | null = null;

	constructor(streamKeys: string[], callbacks: MoQMultiviewCallbacks) {
		this.streamKeys = streamKeys;
		this.callbacks = callbacks;
	}

	async connect(): Promise<void> {
		// One-time setup (AudioContext, compositor, etc.) before any transports start.
		await this.callbacks.onSetup();

		// Track per-stream completion so we know when all are done.
		const perStreamDone: Promise<void>[] = [];

		for (let i = 0; i < this.streamKeys.length; i++) {
			const index = i;
			const key = this.streamKeys[i];

			let resolved = false;
			let onReady: () => void;
			const readyPromise = new Promise<void>(resolve => {
				onReady = () => { if (!resolved) { resolved = true; resolve(); } };
			});
			perStreamDone.push(readyPromise);

			const moqCallbacks = this.buildCallbacks(index, key, async (tracks) => {
				// This stream's catalog arrived — set up its tile immediately.
				const entry: MuxStreamEntry = { index, key, tracks };
				await this.callbacks.onStreamReady(entry);
				onReady!();
			}, onReady!);

			const transport = new MoQTransport(key, moqCallbacks);
			this.transports.push(transport);
		}

		// Fire off all connections in parallel — don't block on connect().
		// Each transport will call onStreamReady independently when its catalog arrives.
		for (const t of this.transports) {
			t.connect().catch(() => { /* errors handled by onError callback */ });
		}

		// Wait for all streams to finish setup, then notify.
		await Promise.all(perStreamDone);
		this.callbacks.onAllReady();

		// Start periodic stats aggregation (mirrors muxStatsTickerLoop)
		this.statsInterval = setInterval(() => {
			if (this.closed) return;
			if (this.latestStats.size > 0) {
				const stats: Record<string, ServerStats> = {};
				for (const [key, stat] of this.latestStats) {
					stats[key] = stat;
				}
				const viewerStats = this.latestViewerStats.size > 0
					? Object.fromEntries(this.latestViewerStats)
					: undefined;
				this.callbacks.onMuxStats(stats, viewerStats);
			}
		}, 1000);
	}

	setStreamCallbacks(index: number, cb: MuxStreamCallbacks): void {
		this.streamCallbacks.set(index, cb);
	}

	enableAllAudio(): void {
		for (const transport of this.transports) {
			transport.subscribeAllAudio();
		}
	}

	disableAllAudio(): void {
		for (const transport of this.transports) {
			transport.subscribeAudio([]);
		}
	}

	enableAudio(index: number): void {
		const transport = this.transports[index];
		if (transport) {
			transport.subscribeAllAudio();
		}
	}

	disableAudio(index: number): void {
		const transport = this.transports[index];
		if (transport) {
			transport.subscribeAudio([]);
		}
	}

	close(): void {
		this.closed = true;
		if (this.statsInterval) {
			clearInterval(this.statsInterval);
			this.statsInterval = null;
		}
		for (const transport of this.transports) {
			transport.close();
		}
		this.transports = [];
		this.streamCallbacks.clear();
		this.latestStats.clear();
		this.latestViewerStats.clear();
	}

	private buildCallbacks(
		index: number,
		key: string,
		onTracks: (tracks: TrackInfo[]) => void | Promise<void>,
		onReady: () => void,
	): MoQTransportCallbacks {
		return {
			onTrackInfo: async (tracks: TrackInfo[]) => {
				await onTracks(tracks);
			},
			onVideoFrame: (data: Uint8Array, isKeyframe: boolean, timestamp: number, groupID: number, description: Uint8Array | null) => {
				const cb = this.streamCallbacks.get(index);
				if (cb) {
					cb.onVideoFrame(data, isKeyframe, timestamp, groupID, description);
				}
			},
			onAudioFrame: (data: Uint8Array, timestamp: number, groupID: number, trackIndex: number) => {
				const cb = this.streamCallbacks.get(index);
				if (cb) {
					cb.onAudioFrame(data, timestamp, groupID, trackIndex);
				}
			},
			onCaptionFrame: (caption: CaptionData, timestamp: number) => {
				const cb = this.streamCallbacks.get(index);
				if (cb?.onCaptionFrame) {
					cb.onCaptionFrame(caption, timestamp);
				}
			},
			onServerStats: (stats: ServerStats) => {
				this.latestStats.set(key, stats);
			},
			onViewerStats: (vs: ServerViewerStats) => {
				this.latestViewerStats.set(key, {
					id: vs.id,
					videoSent: vs.videoSent,
					audioSent: vs.audioSent,
					captionSent: vs.captionSent,
					videoDropped: vs.videoDropped,
					audioDropped: vs.audioDropped,
					captionDropped: vs.captionDropped,
					bytesSent: vs.bytesSent,
				});
			},
			onClose: () => {
				onReady(); // prevent deadlock if transport closes before onTrackInfo
				if (!this.closed) {
					this.callbacks.onClose();
				}
			},
			onError: (err: string) => {
				onReady(); // prevent deadlock if transport errors before onTrackInfo
				this.callbacks.onError(`[${key}] ${err}`);
			},
		};
	}
}
