import { TrackInfo } from "./transport";

/** A single stream within a multiplexed session, including its server-assigned index and track metadata. */
export interface MuxStreamEntry {
	index: number;
	key: string;
	tracks: TrackInfo[];
}

/** Per-stream callbacks for demuxed media frames. Registered via `setStreamCallbacks` after the mux session is ready. */
export interface MuxStreamCallbacks {
	onVideoFrame: (data: Uint8Array, isKeyframe: boolean, timestamp: number, groupID: number, description?: Uint8Array | null) => void;
	onAudioFrame: (data: Uint8Array, timestamp: number, groupID: number, trackIndex: number) => void;
	onCaptionFrame?: (caption: unknown, timestamp: number) => void;
}

/** Per-stream viewer-side statistics reported by the server for a mux session. */
export interface MuxViewerStats {
	id: string;
	videoSent: number;
	audioSent: number;
	captionSent: number;
	videoDropped: number;
	audioDropped: number;
	captionDropped: number;
	bytesSent: number;
}
