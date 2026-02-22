/** Describes a single media track (video or audio) as reported by the server during session setup. */
export interface TrackInfo {
	id: number;
	type: string;
	codec: string;
	width: number;
	height: number;
	sampleRate: number;
	channels: number;
	trackIndex: number;
	label: string;
	initData?: string; // base64-encoded decoder config record (avcC / hvcC)
}

/** Server-side video track statistics received periodically on the control channel. */
interface ServerVideoStats {
	codec: string;
	width: number;
	height: number;
	totalFrames: number;
	keyFrames: number;
	deltaFrames: number;
	currentGOPLen: number;
	bitrateKbps: number;
	frameRate: number;
	ptsErrors: number;
	totalBytes: number;
	timecode?: string;
}

/** Per-audio-track server-side statistics. */
export interface ServerAudioTrackStats {
	trackIndex: number;
	codec: string;
	sampleRate: number;
	channels: number;
	frames: number;
	bitrateKbps: number;
	ptsErrors: number;
	totalBytes: number;
}

/** Server-side caption track statistics. */
interface ServerCaptionStats {
	activeChannels: number[];
	totalFrames: number;
}

/** Per-viewer delivery statistics reported by the server. */
export interface ServerViewerStats {
	id: string;
	videoSent: number;
	audioSent: number;
	captionSent: number;
	videoDropped: number;
	audioDropped: number;
	captionDropped: number;
	bytesSent: number;
}

/** A single SCTE-35 ad insertion event reported by the server. */
export interface ServerSCTE35Event {
	pts: number;
	commandType: string;
	commandTypeId: number;
	eventId?: number;
	segmentationType?: string;
	segmentationTypeId?: number;
	duration?: number;
	outOfNetwork?: boolean;
	immediate?: boolean;
	description: string;
	receivedAt: number;
}

/** Aggregate SCTE-35 statistics for a stream, including the most recent events. */
interface ServerSCTE35Stats {
	totalEvents: number;
	recent?: ServerSCTE35Event[];
}

/** Aggregate server-side statistics for a stream, sent periodically over the control channel. */
export interface ServerStats {
	ts: number;
	uptimeMs: number;
	protocol: string;
	ingestBytes: number;
	ingestKbps: number;
	video: ServerVideoStats;
	audio: ServerAudioTrackStats[];
	captions: ServerCaptionStats;
	scte35: ServerSCTE35Stats;
	viewerCount: number;
	viewers?: ServerViewerStats[];
}
