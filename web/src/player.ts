import { TrackInfo, ServerStats, ServerSCTE35Event } from "./transport";
import { CaptionData } from "./protocol";
import { MoQTransport } from "./moq-transport";
import { PrismVideoDecoder } from "./video-decoder";
import { PrismAudioDecoder } from "./audio-decoder";
import { PrismRenderer, RendererStats } from "./renderer";
import { PrismStats } from "./stats";
import { CaptionRenderer } from "./captions";
import { VideoRenderBuffer } from "./video-render-buffer";
import { AudioTrackSelector } from "./audio-track-selector";
import { VUMeter } from "./vu-meter";
import { PlayerUI } from "./player-ui";
import { MetricsStore } from "./metrics-store";
import { HUD, BadgeKey } from "./hud";
import {
	DetailPanel,
	buildVideoPanel,
	buildAudioPanel,
	buildSyncPanel,
	buildTransportPanel,
	buildCaptionsPanel,
	buildSCTE35Panel,
} from "./detail-panel";
import { PerfOverlay, SingleStreamSnapshot } from "./perf-overlay";
import { FullscreenButton } from "./fullscreen-btn";
import { checkCapabilities, showCapabilityError } from "./capabilities";

/** Configuration options for a PrismPlayer instance. */
interface PlayerOptions {
	condensed?: boolean;
	muteAudio?: boolean;
	onStreamConnected?: (streamKey: string) => void;
	onStreamDisconnected?: (streamKey: string) => void;
}

/** Per-tile performance statistics used by the multiview manager for aggregate diagnostics. */
export interface TilePerfStats {
	streamKey: string | null;
	videoQueueSize: number;
	videoQueueMs: number;
	videoDiscarded: number;
	audioTracks: { track: number; queueMs: number; silenceMs: number; metering: boolean; muted: boolean }[];
	audioContextState: string;
}

/**
 * Single-stream player component that orchestrates video decoding, audio
 * decoding, rendering, captions, and transport for one live stream. Can
 * operate standalone (via `connect`) or as a tile inside a MultiviewManager
 * (via `connectMux` with externally injected frames).
 */
export class PrismPlayer {
	private container: HTMLElement;
	private options: PlayerOptions;

	private canvas: HTMLCanvasElement;
	private vuCanvas: HTMLCanvasElement;
	private captionsEl: HTMLDivElement;
	private statsEl: HTMLDivElement;

	private playerUI: PlayerUI;
	private stats: PrismStats;
	private metricsStore: MetricsStore;
	private captionRenderer: CaptionRenderer;
	private hud: HUD;

	private activeDetailPanel: DetailPanel | null = null;
	private activePanelKey: BadgeKey | null = null;

	private videoRenderBuffer: VideoRenderBuffer;
	private audioDecoders = new Map<number, PrismAudioDecoder>();
	private activeAudioTrack = 0;
	private primaryAudioDecoder: PrismAudioDecoder | null = null;
	private loudnessMode = false;
	private sharedAudioContext: AudioContext | null = null;
	private ownsAudioContext = true;

	private videoDecoder: PrismVideoDecoder;
	private renderer: PrismRenderer;
	private vuMeter: VUMeter;
	private audioTrackSelector: AudioTrackSelector | null = null;
	private fullscreenBtn: FullscreenButton | null = null;
	private perfOverlay: PerfOverlay;
	private perfStartTime = 0;

	private decodeFpsCounter = 0;
	private lastDecodeFpsTime = performance.now();

	private moqTransport: MoQTransport | null = null;
	private pendingVideoCodec: string | null = null;
	private pendingVideoWidth = 0;
	private pendingVideoHeight = 0;

	private connectedStreamKey: string | null = null;
	private destroyed = false;
	private globalMute: boolean;
	private reconnectDelay = 2000;

	constructor(container: HTMLElement, options: PlayerOptions = {}) {
		this.container = container;
		this.options = options;
		this.globalMute = options.muteAudio ?? false;

		const missing = checkCapabilities();
		if (missing.length > 0) {
			showCapabilityError(container, missing);
		}

		this.canvas = document.createElement("canvas");
		this.canvas.width = 960;
		this.canvas.height = 540;
		this.canvas.id = "";

		this.vuCanvas = document.createElement("canvas");
		this.vuCanvas.width = 960;
		this.vuCanvas.height = 540;

		this.statsEl = document.createElement("div");
		this.statsEl.style.background = "rgba(0,0,0,0.7)";
		this.statsEl.style.padding = "8px 12px";
		this.statsEl.style.borderRadius = "4px";
		this.statsEl.style.fontFamily = "'SF Mono', 'Monaco', monospace";
		this.statsEl.style.fontSize = "12px";
		this.statsEl.style.lineHeight = "1.6";
		this.statsEl.style.display = "none";

		this.captionsEl = document.createElement("div");
		this.captionsEl.style.color = "#fff";
		this.captionsEl.style.fontSize = options.condensed ? "0.7rem" : "1.5rem";
		this.captionsEl.style.lineHeight = "1.4";
		this.captionsEl.style.display = "none";
		this.captionsEl.style.fontFamily = "'SF Mono', 'Menlo', monospace";
		this.captionsEl.style.overflow = "hidden";
		this.captionsEl.style.pointerEvents = "none";

		container.appendChild(this.canvas);
		container.appendChild(this.vuCanvas);
		container.appendChild(this.statsEl);
		container.appendChild(this.captionsEl);

		// Resume suspended AudioContext on first user gesture (browser autoplay policy).
		const resumeAudio = () => {
			if (this.sharedAudioContext && this.sharedAudioContext.state === "suspended") {
				this.sharedAudioContext.resume();
			}
		};
		document.addEventListener("click", resumeAudio, { once: true });
		document.addEventListener("keydown", resumeAudio, { once: true });

		this.playerUI = new PlayerUI({
			container: this.container,
			videoCanvas: this.canvas,
			vuCanvas: this.vuCanvas,
			captionsEl: this.captionsEl,
			statsEl: this.statsEl,
		});

		this.stats = new PrismStats(this.statsEl);
		this.metricsStore = new MetricsStore();
		this.captionRenderer = new CaptionRenderer(this.captionsEl, this.playerUI, false);

		this.hud = new HUD(this.playerUI.getHUDContainer(), this.metricsStore);

		if (options.condensed) {
			this.applyCondensedStyles();
		}

		this.setupPanels();

		this.videoRenderBuffer = new VideoRenderBuffer();
		this.videoDecoder = new PrismVideoDecoder(this.videoRenderBuffer, () => this.stats.onVideoFrame());

		this.renderer = new PrismRenderer(
			this.canvas,
			this.videoRenderBuffer,
			{ getPlaybackPTS: () => this.primaryAudioDecoder?.getPlaybackPTS() ?? -1 },
			(rendererStats: RendererStats) => {
				this.stats.onRendererStats(rendererStats);
				this.metricsStore.updateRendererStats(rendererStats);
				if (this.primaryAudioDecoder) {
					const audioStats = this.primaryAudioDecoder.getStats();
					this.stats.updateAudioStats(audioStats.queueLengthMs, audioStats.totalSilenceInsertedMs);
					this.metricsStore.updateAudioStats(audioStats.queueLengthMs, audioStats.totalSilenceInsertedMs);
				}
				this.decodeFpsCounter++;
				const now = performance.now();
				if (now - this.lastDecodeFpsTime >= 1000) {
					this.metricsStore.updateRenderFps(this.decodeFpsCounter);
					this.decodeFpsCounter = 0;
					this.lastDecodeFpsTime = now;
				}
			},
		);

		this.vuMeter = new VUMeter(this.vuCanvas, this.audioDecoders);
		this.vuMeter.setBottomPadding(this.playerUI.getControlBarHeight() + 8);
		this.vuMeter.setOnTrackSelect((trackIndex) => {
			this.switchAudioTrack(trackIndex);
			this.vuMeter.setActiveTrack(trackIndex);
			if (this.audioTrackSelector) {
				this.audioTrackSelector.setActiveTrack(trackIndex);
			}
		});
		this.vuMeter.setOnLeftWidthChange((px) => {
			this.playerUI.setHUDLeftOffset(px);
		});
		this.playerUI.setOnCaptionsChanged((active) => {
			this.vuMeter.setCaptionsActive(active);
		});

		this.perfOverlay = new PerfOverlay(container, () => this.collectDiagnostics());

		if (!options.condensed && document.fullscreenEnabled) {
			this.fullscreenBtn = new FullscreenButton(this.playerUI);
		}
	}

	/** Connect to a single stream via a dedicated WebTransport session. Reconnects automatically on close. */
	async connect(streamKey: string): Promise<void> {
		if (this.destroyed) return;
		return this.connectMoQ(streamKey);
	}

	private async connectMoQ(streamKey: string): Promise<void> {
		this.disconnectInternal();
		this.perfStartTime = performance.now();

		// Pre-create AudioContext in parallel with the WebTransport handshake.
		// 48000 Hz is the standard broadcast sample rate; if the stream differs
		// (rare), setupAudioDecoders will close this and create a new one.
		if (!this.globalMute) {
			this.sharedAudioContext = new AudioContext({ sampleRate: 48000, latencyHint: "interactive" });
			this.ownsAudioContext = true;
			await this.sharedAudioContext.suspend();
		}

		try {
			this.moqTransport = new MoQTransport(streamKey, {
				onTrackInfo: async (tracks: TrackInfo[]) => {
					this.stats.start();
					this.hud.start();

					// Pre-spawn the video decoder Worker so module loading happens
					// in parallel with audio setup and media subscriptions.
					this.videoDecoder.preload();

					const videoTrack = tracks.find(t => t.type === "video");
					if (videoTrack) {
						if (videoTrack.initData) {
							// Decoder config available in catalog — configure immediately
							// so the decoder is ready before the first keyframe arrives.
							const desc = Uint8Array.from(atob(videoTrack.initData), c => c.charCodeAt(0));
							this.videoDecoder.configure(
								videoTrack.codec,
								videoTrack.width,
								videoTrack.height,
								desc.buffer as ArrayBuffer,
							);
						} else {
							// Defer configuration until the first keyframe with description.
							this.pendingVideoCodec = videoTrack.codec;
							this.pendingVideoWidth = videoTrack.width;
							this.pendingVideoHeight = videoTrack.height;
						}
					}

					if (!this.globalMute) {
						const audioTracks = tracks.filter(t => t.type === "audio");
						this.activeAudioTrack = 0;
						await this.setupAudioDecoders(audioTracks);

						if (this.audioTrackSelector) {
							this.audioTrackSelector.destroy();
							this.audioTrackSelector = null;
						}
						if (audioTracks.length > 0 && !this.options.condensed) {
							this.audioTrackSelector = new AudioTrackSelector(
								audioTracks,
								this.activeAudioTrack,
								(trackIndex) => this.switchAudioTrack(trackIndex),
								this.playerUI,
								() => this.enterLoudnessMode(),
								() => this.exitLoudnessMode(),
							);
						}
					} else if (this.moqTransport) {
						this.moqTransport.subscribeAudio([]);
					}

					this.renderer.start();
					this.connectedStreamKey = streamKey;
					this.reconnectDelay = 2000;
					this.perfOverlay.setStreamKey(streamKey);
					this.options.onStreamConnected?.(streamKey);
				},
				onVideoFrame: (data: Uint8Array, isKeyframe: boolean, timestamp: number, _groupID: number, description: Uint8Array | null) => {
					void _groupID;
					// Configure the video decoder on the first keyframe with a description
					if (description && this.pendingVideoCodec) {
						const descCopy = new Uint8Array(description).buffer as ArrayBuffer;
						this.videoDecoder.configure(
							this.pendingVideoCodec,
							this.pendingVideoWidth,
							this.pendingVideoHeight,
							descCopy,
						);
						this.pendingVideoCodec = null;
					}
					this.videoDecoder.decode(data, isKeyframe, timestamp, false);
				},
				onAudioFrame: (data: Uint8Array, timestamp: number, _groupID: number, trackIndex: number) => {
					const decoder = this.audioDecoders.get(trackIndex);
					if (decoder) {
						decoder.decode(data, timestamp, false);
					}
				},
				onCaptionFrame: (caption: CaptionData, _timestamp: number) => {
					this.captionRenderer.show(caption);
				},
				onServerStats: (serverStats: ServerStats) => {
					this.metricsStore.updateServerStats(serverStats);
				},
				onClose: () => {
					this.stats.stop();
					this.hud.stop();
					this.vuMeter.hide();
					this.playerUI.setForceVisible(false);
					this.closePanel();
					this.options.onStreamDisconnected?.(streamKey);

					if (!this.destroyed) {
						const delay = this.reconnectDelay + Math.random() * 1000;
						this.reconnectDelay = Math.min(this.reconnectDelay * 2, 16000);
						setTimeout(() => {
							if (!this.destroyed && this.connectedStreamKey === streamKey) {
								this.connect(streamKey);
							}
						}, delay);
					}
				},
				onError: (_err: string) => {
					this.options.onStreamDisconnected?.(streamKey);
				},
			});

			await this.moqTransport.connect();
		} catch {
			this.options.onStreamDisconnected?.(streamKey);
		}
	}

	private muxAudioTracks: TrackInfo[] = [];

	/**
	 * Connect this player as a tile in a multiplexed session. Unlike `connect`,
	 * the player does not own a transport — frames are injected externally via
	 * `injectVideoFrame` and `injectAudioFrame`. An optional shared AudioContext
	 * allows all tiles to share a single audio graph.
	 */
	async connectMux(streamKey: string, tracks: TrackInfo[], sharedAudioCtx?: AudioContext, deferVideoConfig?: boolean): Promise<void> {
		if (this.destroyed) return;

		this.disconnectInternal();
		this.stats.start();
		this.hud.start();

		this.videoDecoder.preload();

		const videoTrack = tracks.find(t => t.type === "video" || t.id === 0);
		if (videoTrack) {
			if (deferVideoConfig && videoTrack.initData) {
				// Decoder config available in catalog — configure immediately.
				const desc = Uint8Array.from(atob(videoTrack.initData), c => c.charCodeAt(0));
				this.videoDecoder.configure(
					videoTrack.codec,
					videoTrack.width,
					videoTrack.height,
					desc.buffer as ArrayBuffer,
				);
			} else if (deferVideoConfig) {
				// Defer configuration until the first keyframe with description.
				this.pendingVideoCodec = videoTrack.codec;
				this.pendingVideoWidth = videoTrack.width;
				this.pendingVideoHeight = videoTrack.height;
			} else {
				this.videoDecoder.configure(videoTrack.codec, videoTrack.width, videoTrack.height);
			}
		}

		this.muxAudioTracks = tracks.filter(t => t.type === "audio");

		if (this.muxAudioTracks.length > 0 && sharedAudioCtx) {
			this.sharedAudioContext = sharedAudioCtx;
			this.ownsAudioContext = false;
			this.activeAudioTrack = 0;
			await this.setupAudioDecoders(this.muxAudioTracks);

			for (const [, decoder] of this.audioDecoders) {
				decoder.setMuted(true);
				decoder.enableMetering();
			}
			this.vuMeter.setDecoders(this.audioDecoders);
			this.vuMeter.setActiveTrack(this.activeAudioTrack);
			const labelMap = new Map<number, string>();
			for (const t of this.muxAudioTracks) {
				labelMap.set(t.trackIndex, t.label);
			}
			this.vuMeter.setLabels(labelMap);
			if (this.options.condensed) {
				this.vuMeter.setThrottleMs(50);
				this.vuMeter.setCondensed(true);
			}
			this.vuMeter.show();
		}

		this.renderer.freeRunOnly = true;
		this.renderer.start();
		this.connectedStreamKey = streamKey;
		this.options.onStreamConnected?.(streamKey);
	}

	/** Feed a compressed video frame into the decoder. Used in mux mode where the transport is external. */
	injectVideoFrame(data: Uint8Array, isKeyframe: boolean, timestamp: number, description?: Uint8Array): void {
		if (description && this.pendingVideoCodec) {
			const descCopy = new Uint8Array(description).buffer as ArrayBuffer;
			this.videoDecoder.configure(
				this.pendingVideoCodec,
				this.pendingVideoWidth,
				this.pendingVideoHeight,
				descCopy,
			);
			this.pendingVideoCodec = null;
		}
		// If still waiting for AVC/HEVC description, skip — the decoder
		// can't handle AVC1 data without the configuration record.
		if (this.pendingVideoCodec) return;
		this.videoDecoder.decode(data, isKeyframe, timestamp, false);
	}

	/** Feed a compressed audio frame into the appropriate track decoder. Used in mux mode. */
	injectAudioFrame(data: Uint8Array, timestamp: number, trackIndex: number): void {
		const decoder = this.audioDecoders.get(trackIndex);
		if (decoder) {
			decoder.decode(data, timestamp, false);
		}
	}

	/** Feed a caption update into the renderer. Used in mux mode. */
	injectCaptionData(caption: CaptionData): void {
		this.captionRenderer.show(caption);
	}

	/** Forward server-side stats into the metrics store. Used in mux mode. */
	injectServerStats(stats: ServerStats): void {
		this.metricsStore.updateServerStats(stats);
	}

	/** Return the current SMPTE timecode string from server stats, if available. */
	getTimecode(): string {
		return this.metricsStore.getTimecode();
	}

	/** Disconnect from the current stream and tear down decoders. Safe to call when already disconnected. */
	disconnect(): void {
		this.connectedStreamKey = null;
		this.disconnectInternal();
	}

	/** Permanently tear down the player, releasing all resources. Not reusable after this call. */
	destroy(): void {
		this.destroyed = true;
		this.disconnect();
		this.renderer.destroy();
		this.vuMeter.destroy();
		this.hud.destroy();
		this.perfOverlay.destroy();
		if (this.fullscreenBtn) this.fullscreenBtn.destroy();
		this.playerUI.destroy();
		this.container.innerHTML = "";
	}

	/** Switch this player into or out of condensed layout mode (smaller text, simplified controls). */
	setCondensed(condensed: boolean): void {
		this.options.condensed = condensed;
		if (condensed) {
			this.applyCondensedStyles();
		}
	}

	/** Return the root DOM element containing this player. */
	getContainer(): HTMLElement {
		return this.container;
	}

	/** True if this player is currently connected to a stream. */
	isConnected(): boolean {
		return this.connectedStreamKey !== null;
	}

	/** Draw one video frame. Used by the multiview manager when driving the render loop externally. */
	renderOnce(): void {
		this.renderer.renderOnce();
	}

	/** Draw one VU meter frame. Used by the multiview manager when driving the render loop externally. */
	renderVUOnce(): void {
		this.vuMeter.renderOnce();
	}

	/** Return instantaneous audio levels for all metered tracks. Used by the shared VU renderer in composited mode. */
	getAudioLevels(): { trackIndex: number; peak: number[]; peakHold: number[] }[] {
		const result: { trackIndex: number; peak: number[]; peakHold: number[] }[] = [];
		for (const [idx, decoder] of this.audioDecoders) {
			if (!decoder.isMetering()) continue;
			const levels = decoder.getLevels();
			result.push({ trackIndex: idx, peak: levels.peak, peakHold: levels.peakHold });
		}
		return result;
	}

	/** When true, the player's subsystems skip their own rAF loops and rely on the caller to invoke render methods. */
	setExternallyDriven(v: boolean): void {
		this.renderer.externallyDriven = v;
		this.vuMeter.externallyDriven = v;
		this.hud.externallyDriven = v;
		this.stats.externallyDriven = v;
		this.playerUI.externallyDriven = v;
	}

	/** Cap the canvas resolution to this pixel dimension. 0 means no cap. Used to limit GPU work in multiview. */
	setMaxResolution(v: number): void {
		this.renderer.maxResolution = v;
	}

	/** Return the underlying video frame buffer. Used by the WebGPU compositor to read frames directly. */
	getVideoBuffer(): VideoRenderBuffer {
		return this.videoRenderBuffer;
	}

	/** Collect per-tile performance statistics for the multiview perf overlay. */
	getPerfStats(): TilePerfStats {
		const vBuf = this.videoRenderBuffer.getStats();
		const audioStats: { track: number; queueMs: number; silenceMs: number; metering: boolean; muted: boolean }[] = [];
		for (const [idx, dec] of this.audioDecoders) {
			const s = dec.getStats();
			audioStats.push({
				track: idx,
				queueMs: s.queueLengthMs,
				silenceMs: s.totalSilenceInsertedMs,
				metering: dec.isMetering(),
				muted: dec.isMuted(),
			});
		}
		return {
			streamKey: this.connectedStreamKey,
			videoQueueSize: vBuf.queueSize,
			videoQueueMs: vBuf.queueLengthMs,
			videoDiscarded: vBuf.totalDiscarded,
			audioTracks: audioStats,
			audioContextState: this.sharedAudioContext?.state ?? "none",
		};
	}

	/** Return the stream key this player is currently connected to, or null if disconnected. */
	getStreamKey(): string | null {
		return this.connectedStreamKey;
	}

	/**
	 * Mute or unmute all audio for this player. When muted, no audio tracks
	 * are subscribed on the transport (saving bandwidth). Used by the multiview
	 * manager to solo a single tile's audio.
	 */
	setGlobalMute(muted: boolean): void {
		if (this.globalMute === muted) return;
		this.globalMute = muted;
		if (muted) {
			for (const [, decoder] of this.audioDecoders) {
				decoder.setMuted(true);
			}
			if (this.moqTransport) {
				this.moqTransport.subscribeAudio([]);
			}
		} else {
			for (const [idx, decoder] of this.audioDecoders) {
				decoder.setMuted(idx !== this.activeAudioTrack);
			}
			if (this.moqTransport) {
				this.moqTransport.subscribeAudio([this.activeAudioTrack]);
			}
		}
	}

	/** True if this player's audio is globally muted (no tracks subscribed). */
	isGlobalMuted(): boolean {
		return this.globalMute;
	}

	/** Suspend or resume all audio decoders. Used when expanding a tile to free resources for the full-screen player. */
	setDecodersSuspended(suspended: boolean): void {
		for (const [, decoder] of this.audioDecoders) {
			decoder.setSuspended(suspended);
		}
	}

	/** Gather low-level audio state for every decoder, useful for diagnosing muting/context issues. */
	collectAudioDebug(): { globalMute: boolean; decoderCount: number; contextState: string; decoders: { trackIndex: number; muted: boolean; suspended: boolean; gain: number; playing: boolean; contextState: string }[] } {
		const decoders: { trackIndex: number; muted: boolean; suspended: boolean; gain: number; playing: boolean; contextState: string }[] = [];
		for (const [trackIdx, decoder] of this.audioDecoders) {
			const info = decoder.getAudioDebug();
			decoders.push({ trackIndex: trackIdx, ...info });
		}
		return {
			globalMute: this.globalMute,
			decoderCount: this.audioDecoders.size,
			contextState: this.sharedAudioContext?.state ?? "null",
			decoders,
		};
	}

	/** Return the sorted list of available audio track indices. */
	getAudioTrackIndices(): number[] {
		return [...this.audioDecoders.keys()].sort((a, b) => a - b);
	}

	/** Return the index of the currently selected (unmuted) audio track. */
	getActiveAudioTrack(): number {
		return this.activeAudioTrack;
	}

	/** Switch to the next or previous audio track, wrapping around. Returns the new track index. */
	cycleAudioTrack(direction: 1 | -1 = 1): number {
		const indices = this.getAudioTrackIndices();
		if (indices.length <= 1) return this.activeAudioTrack;
		const cur = indices.indexOf(this.activeAudioTrack);
		const next = (cur + direction + indices.length) % indices.length;
		this.switchAudioTrack(indices[next]);
		return indices[next];
	}

	/** Cycle through available caption channels (off -> CC1 -> CC2 -> ... -> off). Returns the new channel. */
	cycleCaptionChannel(): number {
		return this.captionRenderer.cycleChannel();
	}

	/** Return the currently active caption channel number (0 = off). */
	getActiveCaptionChannel(): number {
		return this.captionRenderer.getActiveChannel();
	}

	/**
	 * Collect a full diagnostic snapshot of the player's subsystems (renderer,
	 * video decoder, audio decoder, transport). Returns null if the audio
	 * pipeline is not yet active. Used by the perf overlay and clipboard export.
	 */
	async collectDiagnostics(): Promise<SingleStreamSnapshot | null> {
		const audioDiag = this.primaryAudioDecoder?.getDiagnostics();
		if (!audioDiag) return null;

		const rendererDiag = this.renderer.getDiagnostics();
		const videoDiag = await this.videoDecoder.getDiagnostics();
		const transportDiag = this.moqTransport
			? this.moqTransport.getDiagnostics()
			: { streamsOpened: 0, bytesReceived: 0, videoFramesReceived: 0, audioFramesReceived: 0, avgVideoArrivalMs: 0, maxVideoArrivalMs: 0 };

		return {
			timestamp: new Date().toISOString(),
			uptimeMs: this.perfStartTime > 0 ? performance.now() - this.perfStartTime : 0,
			renderer: rendererDiag,
			videoDecoder: videoDiag,
			audio: audioDiag,
			transport: transportDiag,
		};
	}

	private disconnectInternal(): void {
		if (this.moqTransport) {
			this.moqTransport.close();
			this.moqTransport = null;
		}
		this.pendingVideoCodec = null;
		this.pendingVideoWidth = 0;
		this.pendingVideoHeight = 0;
		this.videoDecoder.reset();
		for (const [, decoder] of this.audioDecoders) {
			decoder.reset();
		}
		this.audioDecoders.clear();
		this.muxAudioTracks = [];
		if (this.sharedAudioContext && this.ownsAudioContext) {
			this.sharedAudioContext.close();
		}
		this.sharedAudioContext = null;
		this.ownsAudioContext = true;
		this.primaryAudioDecoder = null;
		this.loudnessMode = false;
		this.vuMeter.hide();
		this.playerUI.setForceVisible(false);
		this.renderer.destroy();
		this.metricsStore.reset();
		this.closePanel();
		this.hud.stop();
	}

	private applyCondensedStyles(): void {
		this.captionsEl.style.fontSize = "0.7rem";
		this.captionsEl.style.lineHeight = "1.2";
		this.captionsEl.style.padding = "2px 4px";
	}

	private setupPanels(): void {
		const panelBuilders: Record<BadgeKey, (container: HTMLElement, store: MetricsStore, z: number) => DetailPanel> = {
			video: buildVideoPanel,
			audio: buildAudioPanel,
			sync: buildSyncPanel,
			buffer: buildTransportPanel,
			viewers: buildCaptionsPanel,
		};

		const togglePanel = (key: BadgeKey): void => {
			if (this.activePanelKey === key) {
				this.closePanel();
				return;
			}
			this.closePanel();
			const builder = panelBuilders[key];
			if (!builder) return;
			const panel = builder(this.playerUI.getContainer(), this.metricsStore, this.playerUI.getPanelZIndex());
			panel.setOnClose(() => this.closePanel());
			panel.start();
			this.activeDetailPanel = panel;
			this.activePanelKey = key;
			this.hud.setHighlight(key);
			this.playerUI.notifyPanelOpen(key);
		};

		this.hud.setOnBadgeClick(togglePanel);

		this.hud.setOnSCTE35Click((event: ServerSCTE35Event) => {
			this.closePanel();
			const panel = buildSCTE35Panel(
				this.playerUI.getContainer(),
				this.metricsStore,
				this.playerUI.getPanelZIndex(),
				event,
			);
			panel.setOnClose(() => this.closePanel());
			panel.start();
			this.activeDetailPanel = panel;
			this.activePanelKey = null;
		});
	}

	private closePanel(): void {
		if (this.activeDetailPanel) {
			this.activeDetailPanel.destroy();
			this.activeDetailPanel = null;
		}
		this.activePanelKey = null;
		this.hud.setHighlight(null);
		this.playerUI.notifyPanelClose();
	}

	private async setupAudioDecoders(audioTracks: TrackInfo[]): Promise<void> {
		for (const [, decoder] of this.audioDecoders) {
			decoder.reset();
		}
		this.audioDecoders.clear();

		if (audioTracks.length === 0) {
			if (this.sharedAudioContext && this.ownsAudioContext) {
				this.sharedAudioContext.close();
				this.sharedAudioContext = null;
			}
			return;
		}

		const sampleRate = audioTracks[0].sampleRate;

		// Reuse the pre-created AudioContext if sample rate matches, otherwise recreate.
		if (this.sharedAudioContext && this.ownsAudioContext) {
			if (sampleRate > 0 && this.sharedAudioContext.sampleRate !== sampleRate) {
				this.sharedAudioContext.close();
				this.sharedAudioContext = null;
			}
		}

		if (!this.sharedAudioContext) {
			this.sharedAudioContext = new AudioContext({ sampleRate, latencyHint: "interactive" });
			this.ownsAudioContext = true;
			await this.sharedAudioContext.suspend();
		}

		await Promise.all(audioTracks.map(async (track) => {
			const decoder = new PrismAudioDecoder();
			const isMuted = track.trackIndex !== this.activeAudioTrack;
			decoder.setMuted(isMuted);
			await decoder.configure(track.codec, track.sampleRate, track.channels, this.sharedAudioContext!);
			this.audioDecoders.set(track.trackIndex, decoder);
		}));

		this.primaryAudioDecoder = this.audioDecoders.get(this.activeAudioTrack) ?? this.audioDecoders.values().next().value ?? null;
		this.vuMeter.setDecoders(this.audioDecoders);
		this.vuMeter.setActiveTrack(this.activeAudioTrack);
		const labelMap = new Map<number, string>();
		for (const t of audioTracks) {
			labelMap.set(t.trackIndex, t.label);
		}
		this.vuMeter.setLabels(labelMap);
	}

	private switchAudioTrack(trackIndex: number): void {
		this.activeAudioTrack = trackIndex;
		for (const [idx, decoder] of this.audioDecoders) {
			decoder.setMuted(idx !== trackIndex);
		}
		this.primaryAudioDecoder = this.audioDecoders.get(trackIndex) ?? null;
		this.vuMeter.setActiveTrack(trackIndex);
		if (!this.loudnessMode) {
			if (this.moqTransport) {
				this.moqTransport.subscribeAudio([trackIndex]);
			}
		}
	}

	private enterLoudnessMode(): void {
		this.loudnessMode = true;
		for (const [idx, decoder] of this.audioDecoders) {
			decoder.setMuted(idx !== this.activeAudioTrack);
			decoder.enableMetering();
		}
		if (this.moqTransport) {
			this.moqTransport.subscribeAllAudio();
		}
		this.vuMeter.show();
		this.playerUI.setForceVisible(true);
		if (this.audioTrackSelector) {
			this.audioTrackSelector.setLoudnessMode(true);
		}
	}

	private exitLoudnessMode(): void {
		this.loudnessMode = false;
		this.vuMeter.hide();
		this.playerUI.setForceVisible(false);
		for (const [, decoder] of this.audioDecoders) {
			decoder.disableMetering();
		}
		if (this.moqTransport) {
			this.moqTransport.subscribeAudio([this.activeAudioTrack]);
		}
		if (this.audioTrackSelector) {
			this.audioTrackSelector.setLoudnessMode(false);
		}
	}
}
