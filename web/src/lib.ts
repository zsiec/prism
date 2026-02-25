// Headless library entry point for embedding Prism's player and transport
// in external applications. Build with: npx vite build --config vite.lib.config.ts

// Player
export { PrismPlayer } from "./player";
export type { TilePerfStats } from "./player";

// MoQ Transport
export { MoQTransport } from "./moq-transport";
export type { MoQTransportCallbacks } from "./moq-transport";

// MoQ Multiview Transport
export { MoQMultiviewTransport } from "./moq-multiview-transport";
export type { MoQMultiviewCallbacks } from "./moq-multiview-transport";

// Metrics
export { MetricsStore } from "./metrics-store";
export type {
  FrameEvent,
  VideoMetrics,
  AudioMetrics,
  SyncMetrics,
  TransportMetrics,
  CaptionMetrics,
  HealthStatus,
  StreamInfo,
  ErrorCounters,
} from "./metrics-store";

// Transport types
export type {
  TrackInfo,
  ServerAudioTrackStats,
  ServerViewerStats,
  ServerSCTE35Event,
  ServerStats,
} from "./transport";

// Protocol types
export { parseCaptionData } from "./protocol";
export type {
  CaptionSpan,
  CaptionRow,
  CaptionRegion,
  CaptionData,
  ProtocolDiagnostics,
} from "./protocol";

// Stream buffer
export { StreamBuffer } from "./stream-buffer";

// Multiview types
export type {
  MuxStreamEntry,
  MuxStreamCallbacks,
  MuxViewerStats,
} from "./multiview-types";
