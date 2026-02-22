import { TrackInfo, ServerStats, ServerViewerStats } from "./transport";
import { StreamBuffer } from "./stream-buffer";
import { parseCaptionData, CaptionData, ProtocolDiagnostics } from "./protocol";
import { fetchServerInfo, wtBaseURL, connectWebTransport } from "./transport-utils";
import {
	MOQ_VERSION,
	MOQ_MSG_CLIENT_SETUP,
	MOQ_MSG_SERVER_SETUP,
	MOQ_MSG_SUBSCRIBE,
	MOQ_MSG_SUBSCRIBE_OK,
	MOQ_MSG_SUBSCRIBE_ERROR,
	MOQ_MSG_UNSUBSCRIBE,
	MOQ_MSG_MAX_REQUEST_ID,
	MOQ_MSG_GOAWAY,
	MOQ_STREAM_TYPE_SUBGROUP_SID_EXT,
	MOQ_FILTER_NEXT_GROUP_START,
	readVarint,
	writeControlMsg,
	readControlMsgFromBuffer,
	serializeClientSetup,
	serializeSubscribe,
	serializeUnsubscribe,
	parseSubscribeOK,
	parseSubscribeError,
	parseServerSetup,
	parseExtensions,
	readVarintFromBuffer,
} from "./moq-constants";

/** Callbacks delivered by MoQTransport to the player. */
export interface MoQTransportCallbacks {
	onTrackInfo: (tracks: TrackInfo[]) => void | Promise<void>;
	onVideoFrame: (
		data: Uint8Array,
		isKeyframe: boolean,
		timestamp: number,
		groupID: number,
		description: Uint8Array | null,
	) => void;
	onAudioFrame: (
		data: Uint8Array,
		timestamp: number,
		groupID: number,
		trackIndex: number,
	) => void;
	onCaptionFrame: (caption: CaptionData, timestamp: number) => void;
	onServerStats: (stats: ServerStats) => void;
	onViewerStats?: (stats: ServerViewerStats) => void;
	onClose: () => void;
	onError: (err: string) => void;
}

interface PendingSubscribe {
	trackName: string;
	resolve: (alias: number) => void;
	reject: (err: Error) => void;
}

interface ActiveSubscription {
	requestID: number;
	trackAlias: number;
}

interface MoQCatalog {
	version: number;
	streamingFormat: number;
	streamingFormatVersion: string;
	commonTrackFields: { namespace: string; packaging: string };
	tracks: {
		name: string;
		selectionParams: {
			codec: string;
			width?: number;
			height?: number;
			initData?: string;
			samplerate?: number;
			channelConfig?: string;
		};
	}[];
}

/**
 * MoQ Transport client implementing draft-15. Handles the WebTransport
 * connection, MoQ control handshake, catalog subscription, and incoming
 * media stream demuxing for a single stream.
 */
export class MoQTransport {
	private streamKey: string;
	private callbacks: MoQTransportCallbacks;
	private transport: WebTransport | null = null;
	private controlWriter: WritableStreamDefaultWriter<Uint8Array> | null = null;
	private controlBuffer: StreamBuffer | null = null;
	private closed = false;

	private nextRequestID = 0;
	private serverMaxRequestID = 0;
	private trackAliasMap = new Map<number, string>(); // alias → trackName
	private pendingSubscribes = new Map<number, PendingSubscribe>(); // requestID → pending
	private activeSubscriptions = new Map<string, ActiveSubscription>(); // trackName → sub info
	private namespace: string[] = [];
	private catalogTracks: MoQCatalog["tracks"] = [];

	// Diagnostics counters (matches ProtocolDiagnostics)
	private _diagStreamsOpened = 0;
	private _diagBytesReceived = 0;
	private _diagVideoFrames = 0;
	private _diagAudioFrames = 0;
	private _diagLastVideoArrival = 0;
	private _diagVideoArrivalSum = 0;
	private _diagVideoArrivalMax = 0;
	private _diagVideoArrivalCount = 0;

	constructor(streamKey: string, callbacks: MoQTransportCallbacks) {
		this.streamKey = streamKey;
		this.callbacks = callbacks;
	}

	async connect(): Promise<void> {
		const info = await fetchServerInfo();
		const url = `${wtBaseURL(info)}/moq?stream=${encodeURIComponent(this.streamKey)}`;

		try {
			this.transport = await connectWebTransport(url, info.certHash);
		} catch (err) {
			this.callbacks.onError(`MoQ WebTransport connection failed: ${err}`);
			return;
		}

		this.transport.closed
			.then(() => {
				if (!this.closed) this.callbacks.onClose();
			})
			.catch(() => {
				if (!this.closed) this.callbacks.onClose();
			});

		// Open bidirectional control stream
		const controlStream = await this.transport.createBidirectionalStream();
		this.controlWriter = controlStream.writable.getWriter();
		this.controlBuffer = new StreamBuffer(controlStream.readable.getReader());

		// CLIENT_SETUP handshake
		const setupPayload = serializeClientSetup([MOQ_VERSION], this.streamKey, 100);
		await writeControlMsg(this.controlWriter, MOQ_MSG_CLIENT_SETUP, setupPayload);

		// Read SERVER_SETUP
		const setupMsg = await readControlMsgFromBuffer(this.controlBuffer);
		if (!setupMsg || setupMsg.type !== MOQ_MSG_SERVER_SETUP) {
			this.callbacks.onError("Expected SERVER_SETUP");
			this.close();
			return;
		}
		const serverSetup = parseServerSetup(setupMsg.payload);
		if (serverSetup.selectedVersion !== MOQ_VERSION) {
			this.callbacks.onError(`Version mismatch: server selected ${serverSetup.selectedVersion.toString(16)}`);
			this.close();
			return;
		}
		this.serverMaxRequestID = serverSetup.maxRequestID;

		// Read MAX_REQUEST_ID (server sends it immediately after setup)
		const maxReqMsg = await readControlMsgFromBuffer(this.controlBuffer);
		if (maxReqMsg && maxReqMsg.type === MOQ_MSG_MAX_REQUEST_ID) {
			const result = readVarint(maxReqMsg.payload, 0);
			this.serverMaxRequestID = result.value;
		}

		// Start the control-message reader BEFORE subscribing so that
		// SUBSCRIBE_OK responses can be processed while we await them.
		this.readControlLoop();

		// Subscribe to catalog
		this.namespace = ["prism", this.streamKey];
		const catalogAlias = await this.subscribe(this.namespace, "catalog", 192);

		// Read catalog from incoming uni-stream
		const catalogJSON = await this.readCatalogFromStream(catalogAlias);
		if (!catalogJSON) {
			this.callbacks.onError("Failed to read catalog");
			this.close();
			return;
		}

		const catalog: MoQCatalog = JSON.parse(new TextDecoder().decode(catalogJSON));
		this.catalogTracks = catalog.tracks;
		const tracks = this.catalogToTrackInfo(catalog);
		await this.callbacks.onTrackInfo(tracks);

		// Start reading incoming data streams BEFORE subscribing so that
		// the first keyframe can be processed as soon as it arrives, even
		// while SUBSCRIBE_OK responses are still in flight. The trackAliasMap
		// is populated by readControlLoop when SUBSCRIBE_OK arrives, so
		// data streams are handled correctly regardless of arrival order.
		this.readIncomingStreams();

		// Subscribe to media tracks
		const mediaSubscriptions: Promise<number>[] = [];
		for (const track of catalog.tracks) {
			if (track.name === "catalog") continue;
			const priority = track.name === "video" ? 0 : track.name.startsWith("audio") ? 64 : 128;
			mediaSubscriptions.push(this.subscribe(this.namespace, track.name, priority));
		}
		await Promise.all(mediaSubscriptions);
	}

	close(): void {
		this.closed = true;
		this.rejectPendingSubscribes("transport closed");
		this.controlWriter = null;
		this.controlBuffer = null;
		if (this.transport) {
			try {
				this.transport.close();
			} catch { /* already closed */ }
			this.transport = null;
		}
	}

	getDiagnostics(): ProtocolDiagnostics {
		return {
			streamsOpened: this._diagStreamsOpened,
			bytesReceived: this._diagBytesReceived,
			videoFramesReceived: this._diagVideoFrames,
			audioFramesReceived: this._diagAudioFrames,
			avgVideoArrivalMs: this._diagVideoArrivalCount > 0
				? this._diagVideoArrivalSum / this._diagVideoArrivalCount : 0,
			maxVideoArrivalMs: this._diagVideoArrivalMax,
		};
	}

	/** Subscribe to a specific audio track set. Unsubscribes tracks not in the list. */
	async subscribeAudio(trackIndices: number[]): Promise<void> {
		const wantNames = new Set(trackIndices.map(i => `audio${i}`));

		// Unsubscribe audio tracks not in the wanted set
		const unsubPromises: Promise<void>[] = [];
		for (const [name, sub] of this.activeSubscriptions) {
			if (name.startsWith("audio") && !wantNames.has(name)) {
				unsubPromises.push(this.unsubscribeTrack(name, sub.requestID));
			}
		}
		await Promise.all(unsubPromises);

		// Subscribe to audio tracks not yet active
		const subPromises: Promise<number>[] = [];
		for (const name of wantNames) {
			if (!this.activeSubscriptions.has(name)) {
				subPromises.push(this.subscribe(this.namespace, name, 64));
			}
		}
		await Promise.all(subPromises);
	}

	/** Subscribe to all audio tracks from the catalog. */
	async subscribeAllAudio(): Promise<void> {
		const audioIndices: number[] = [];
		for (const t of this.catalogTracks) {
			if (t.name.startsWith("audio")) {
				const idx = parseInt(t.name.replace("audio", ""), 10);
				if (!isNaN(idx)) audioIndices.push(idx);
			}
		}
		await this.subscribeAudio(audioIndices);
	}

	private async subscribe(namespace: string[], trackName: string, priority: number): Promise<number> {
		const requestID = this.nextRequestID++;
		if (requestID > this.serverMaxRequestID) {
			throw new Error(`Request ID ${requestID} exceeds server max ${this.serverMaxRequestID}`);
		}
		const payload = serializeSubscribe(requestID, namespace, trackName, priority, MOQ_FILTER_NEXT_GROUP_START);
		await writeControlMsg(this.controlWriter!, MOQ_MSG_SUBSCRIBE, payload);

		return new Promise<number>((resolve, reject) => {
			this.pendingSubscribes.set(requestID, { trackName, resolve, reject });
		});
	}

	private async unsubscribeTrack(trackName: string, requestID: number): Promise<void> {
		const payload = serializeUnsubscribe(requestID);
		await writeControlMsg(this.controlWriter!, MOQ_MSG_UNSUBSCRIBE, payload);

		const sub = this.activeSubscriptions.get(trackName);
		if (sub) {
			this.trackAliasMap.delete(sub.trackAlias);
			this.activeSubscriptions.delete(trackName);
		}
	}

	private async readCatalogFromStream(catalogAlias: number): Promise<Uint8Array | null> {
		if (!this.transport) return null;

		const reader = this.transport.incomingUnidirectionalStreams.getReader();

		// We may receive streams for other tracks, but the first one should be the catalog.
		// Keep accepting until we find the catalog stream.
		while (true) {
			const { value: stream, done } = await reader.read();
			if (done || !stream) {
				reader.releaseLock();
				return null;
			}

			const streamReader = stream.getReader();
			const buffer = new StreamBuffer(streamReader);

			// Read subgroup header
			const streamType = await readVarintFromBuffer(buffer);
			if (streamType === null) continue;

			const trackAlias = await readVarintFromBuffer(buffer);
			if (trackAlias === null) continue;

			await readVarintFromBuffer(buffer); // group_id
			await readVarintFromBuffer(buffer); // subgroup_id
			await buffer.read(1); // publisher_priority

			if (streamType !== MOQ_STREAM_TYPE_SUBGROUP_SID_EXT || trackAlias !== catalogAlias) {
				// Not the catalog — start handling this stream asynchronously
				// and keep waiting for the catalog
				streamReader.releaseLock();
				continue;
			}

			// Read the single catalog object
			await readVarintFromBuffer(buffer); // object_id
			const extLen = await readVarintFromBuffer(buffer); // extensions_length
			if (extLen !== null && extLen > 0) {
				await buffer.read(extLen); // skip extensions
			}
			const payloadLen = await readVarintFromBuffer(buffer);
			if (payloadLen === null || payloadLen === 0) {
				streamReader.releaseLock();
				reader.releaseLock();
				return null;
			}

			const payload = await buffer.read(payloadLen);
			streamReader.releaseLock();
			reader.releaseLock();
			return payload;
		}
	}

	private async readControlLoop(): Promise<void> {
		if (!this.controlBuffer) return;

		try {
			while (!this.closed) {
				const msg = await readControlMsgFromBuffer(this.controlBuffer);
				if (!msg) break;

				switch (msg.type) {
					case MOQ_MSG_SUBSCRIBE_OK: {
						const sok = parseSubscribeOK(msg.payload);
						const pending = this.pendingSubscribes.get(sok.requestID);
						if (pending) {
							this.trackAliasMap.set(sok.trackAlias, pending.trackName);
							this.activeSubscriptions.set(pending.trackName, {
								requestID: sok.requestID,
								trackAlias: sok.trackAlias,
							});
							this.pendingSubscribes.delete(sok.requestID);
							pending.resolve(sok.trackAlias);
						}
						break;
					}
					case MOQ_MSG_SUBSCRIBE_ERROR: {
						const se = parseSubscribeError(msg.payload);
						const pending = this.pendingSubscribes.get(se.requestID);
						if (pending) {
							this.pendingSubscribes.delete(se.requestID);
							pending.reject(new Error(`Subscribe error ${se.errorCode}: ${se.reasonPhrase}`));
						}
						break;
					}
					case MOQ_MSG_MAX_REQUEST_ID: {
						const result = readVarint(msg.payload, 0);
						this.serverMaxRequestID = result.value;
						break;
					}
					case MOQ_MSG_GOAWAY: {
						this.close();
						this.callbacks.onClose();
						return;
					}
				}
			}
		} catch {
			// control stream ended or errored
		}
		this.rejectPendingSubscribes("control stream closed");
	}

	private rejectPendingSubscribes(reason: string): void {
		for (const [, pending] of this.pendingSubscribes) {
			pending.reject(new Error(reason));
		}
		this.pendingSubscribes.clear();
	}

	private waitForTrackAlias(alias: number, timeoutMs: number): Promise<string | undefined> {
		return new Promise<string | undefined>((resolve) => {
			const start = performance.now();
			const check = (): void => {
				const name = this.trackAliasMap.get(alias);
				if (name) {
					resolve(name);
				} else if (this.closed || performance.now() - start > timeoutMs) {
					resolve(undefined);
				} else {
					setTimeout(check, 5);
				}
			};
			check();
		});
	}

	private async readIncomingStreams(): Promise<void> {
		if (!this.transport) return;

		const reader = this.transport.incomingUnidirectionalStreams.getReader();
		try {
			while (!this.closed) {
				const { value: stream, done } = await reader.read();
				if (done || !stream) break;
				this.handleDataStream(stream);
			}
		} catch {
			// transport closed
		}
	}

	private async handleDataStream(stream: ReadableStream<Uint8Array>): Promise<void> {
		const reader = stream.getReader();
		const buffer = new StreamBuffer(reader);
		this._diagStreamsOpened++;

		try {
			// Read subgroup header
			const streamType = await readVarintFromBuffer(buffer);
			if (streamType !== MOQ_STREAM_TYPE_SUBGROUP_SID_EXT) return;

			const trackAlias = await readVarintFromBuffer(buffer);
			if (trackAlias === null) return;

			const groupID = await readVarintFromBuffer(buffer);
			if (groupID === null) return;

			await readVarintFromBuffer(buffer); // subgroup_id
			const priorityByte = await buffer.read(1); // publisher_priority
			if (!priorityByte) return;

			// The track alias may not be registered yet if this data stream
			// arrived before the SUBSCRIBE_OK on the control stream. Wait
			// briefly for the alias to appear.
			let trackName = this.trackAliasMap.get(trackAlias);
			if (!trackName) {
				trackName = await this.waitForTrackAlias(trackAlias, 500);
				if (!trackName) return;
			}

			// Read objects in a loop
			while (!this.closed) {
				const objectID = await readVarintFromBuffer(buffer);
				if (objectID === null) break; // stream ended

				const extLen = await readVarintFromBuffer(buffer);
				if (extLen === null) break;

				let extensions = { captureTimestamp: 0, isKeyframe: false, videoConfig: null as Uint8Array | null };
				if (extLen > 0) {
					const extData = await buffer.read(extLen);
					if (!extData) break;
					extensions = parseExtensions(extData);
				}

				const payloadLen = await readVarintFromBuffer(buffer);
				if (payloadLen === null) break;

				if (payloadLen === 0) continue;

				const payload = await buffer.read(payloadLen);
				if (!payload) break;

				this._diagBytesReceived += payload.byteLength;
				const timestamp = extensions.captureTimestamp;

				if (trackName === "video") {
					this._diagVideoFrames++;
					const now = performance.now();
					if (this._diagLastVideoArrival > 0) {
						const gap = now - this._diagLastVideoArrival;
						this._diagVideoArrivalSum += gap;
						this._diagVideoArrivalCount++;
						if (gap > this._diagVideoArrivalMax) {
							this._diagVideoArrivalMax = gap;
						}
					}
					this._diagLastVideoArrival = now;
					this.callbacks.onVideoFrame(
						payload,
						extensions.isKeyframe,
						timestamp,
						groupID,
						extensions.videoConfig,
					);
				} else if (trackName.startsWith("audio")) {
					this._diagAudioFrames++;
					const idx = parseInt(trackName.replace("audio", ""), 10) || 0;
					this.callbacks.onAudioFrame(payload, timestamp, groupID, idx);
				} else if (trackName === "captions") {
					const caption = parseCaptionData(payload);
					this.callbacks.onCaptionFrame(caption, timestamp);
				} else if (trackName === "stats") {
					try {
						const msg = JSON.parse(new TextDecoder().decode(payload));
						if (msg.stats) {
							this.callbacks.onServerStats(msg.stats as ServerStats);
						}
						if (msg.viewerStats && this.callbacks.onViewerStats) {
							this.callbacks.onViewerStats(msg.viewerStats as ServerViewerStats);
						}
					} catch {
						// malformed stats JSON
					}
				}
			}
		} catch {
			// stream ended or errored
		} finally {
			reader.releaseLock();
		}
	}

	private catalogToTrackInfo(catalog: MoQCatalog): TrackInfo[] {
		const tracks: TrackInfo[] = [];
		let audioIndex = 0;

		for (const t of catalog.tracks) {
			const sp = t.selectionParams;

			if (t.name === "video") {
				tracks.push({
					id: 0,
					type: "video",
					codec: sp.codec,
					width: sp.width ?? 0,
					height: sp.height ?? 0,
					sampleRate: 0,
					channels: 0,
					trackIndex: 0,
					label: "",
					initData: sp.initData,
				});
			} else if (t.name.startsWith("audio")) {
				const idx = parseInt(t.name.replace("audio", ""), 10) || audioIndex;
				tracks.push({
					id: 10 + idx,
					type: "audio",
					codec: sp.codec,
					width: 0,
					height: 0,
					sampleRate: sp.samplerate ?? 0,
					channels: sp.channelConfig ? parseInt(sp.channelConfig, 10) : 0,
					trackIndex: idx,
					label: `Audio ${idx + 1}`,
				});
				audioIndex++;
			} else if (t.name === "captions") {
				tracks.push({
					id: 2,
					type: "caption",
					codec: sp.codec,
					width: 0,
					height: 0,
					sampleRate: 0,
					channels: 0,
					trackIndex: 0,
					label: "",
				});
			}
		}

		return tracks;
	}
}
