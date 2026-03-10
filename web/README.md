# Prism Web

Vanilla TypeScript frontend for Prism. Provides a single-stream player, a 9-up multiview grid, and an embeddable library for use in external applications.

## Quick Start

```bash
npm install
npm run dev          # Vite dev server on :5173, proxies /api to Prism on :4444
```

Make sure the Prism server is running (`make run` from the repo root).

- Single stream: `http://localhost:5173/?stream=demo`
- Multiview: `http://localhost:5173/`

## Scripts

| Script | Description |
|---|---|
| `npm run dev` | Vite dev server with hot reload and API proxy |
| `npm run build` | Production build to `dist/` |
| `npm run build:lib` | Library build to `dist-lib/prism.js` |
| `npm run demo:lib` | Build library + start dev server (for testing `examples/standalone.html`) |
| `npm run preview` | Preview the production build |

## Library

The library build (`npm run build:lib`) produces `dist-lib/prism.js` — an ES module exporting the player and transport classes for embedding in external applications.

```js
import { PrismPlayer } from "./dist-lib/prism.js";

const player = new PrismPlayer(document.getElementById("container"), {
  onStreamConnected(key) { console.log("connected:", key); },
  onStreamDisconnected(key) { console.log("disconnected:", key); },
});
player.connect("demo");
```

### Exports

| Export | Description |
|---|---|
| `PrismPlayer` | Single-stream player — creates canvas, audio, captions, and transport internally |
| `MoQTransport` | Low-level MoQ Transport client for a single stream |
| `MoQMultiviewTransport` | Manages N `MoQTransport` instances for multiview |
| `MetricsStore` | Frame-level metrics collection (video, audio, sync, transport, captions) |
| `StreamBuffer` | Buffered stream reader |
| `parseCaptionData` | CEA-608/708 caption parser |

See [`examples/standalone.html`](examples/standalone.html) for a complete working example.

## Source Structure

**Entry points:**

| File | Purpose |
|---|---|
| `main.ts` | App entry point — routes to single-stream or multiview |
| `lib.ts` | Library entry point — barrel export for `build:lib` |

**Player and transport:**

| File | Purpose |
|---|---|
| `player.ts` | `PrismPlayer` — orchestrates decoding, rendering, and transport for one stream |
| `player-ui.ts` | Player UI chrome — creates canvas, audio elements, captions, and control bar |
| `multiview.ts` | Multiview manager — 9-tile grid with per-tile audio solo |
| `moq-transport.ts` | MoQ Transport client — WebTransport + MoQ control/data parsing |
| `moq-multiview-transport.ts` | Multi-stream MoQ coordinator |
| `moq-constants.ts` | MoQ protocol constants (message types, versions, parameter keys) |
| `transport.ts` | Track info and server stats types (`ServerStats`, `ServerSCTE35Event`) |
| `transport-utils.ts` | Connection utilities (`fetchServerInfo`, `connectWebTransport`) |
| `stream-buffer.ts` | Buffered stream reader for exact-length reads from WebTransport |
| `protocol.ts` | Wire protocol types and CEA-608/708 caption parsing |
| `capabilities.ts` | Browser API capability checker (WebCodecs, WebTransport, SharedArrayBuffer) |
| `multiview-types.ts` | Multiview data structures (`MuxStreamEntry`, `MuxStreamCallbacks`, `MuxViewerStats`) |

**Video:**

| File | Purpose |
|---|---|
| `video-decoder.ts` | WebCodecs video decoder with worker offload |
| `video-decoder-worker.ts` | Web Worker for `VideoDecoder` |
| `video-render-buffer.ts` | Ring-buffer backed video frame queue with timestamp-based lookup |
| `renderer.ts` | Canvas 2D / WebGPU video renderer with A/V sync |
| `webgpu-compositor.ts` | WebGPU-based compositor for multiview tile rendering |

**Audio:**

| File | Purpose |
|---|---|
| `audio-decoder.ts` | WebCodecs audio decoder with AudioWorklet output |
| `audio-worklet.ts` | AudioWorklet processor for audio playback scheduling |
| `audio-ring-buffer.ts` | Shared ring buffer for audio samples with state management |
| `audio-utils.ts` | Audio utilities (dB conversion, level scaling) |
| `audio-track-selector.ts` | Dropdown UI for selecting between multiple audio tracks |
| `vu-meter.ts` | Single-stream VU meter visualization |
| `multiview-vu.ts` | Multi-tile VU meter visualization for multiview |

**Captions and signals:**

| File | Purpose |
|---|---|
| `captions.ts` | CEA-608/708 caption overlay renderer |
| `scte35-history.ts` | SCTE-35 ad insertion event tracking and history |

**Metrics and diagnostics:**

| File | Purpose |
|---|---|
| `metrics-store.ts` | Per-frame metrics collection and health scoring |
| `stats.ts` | Real-time FPS and stats display on canvas |
| `perf-overlay.ts` | Complete diagnostic snapshot aggregation |
| `detail-panel.ts` | Sparklines and detailed metric panels |
| `inspector.ts` | Stream inspector orchestrator — metrics strip + dashboard overlay |
| `inspector-strip.ts` | Compact 44px metrics strip (FPS, bitrate, A/V sync, viewers) |
| `inspector-dashboard.ts` | Full dashboard overlay with pipeline flow and 2×2 chart grid |
| `inspector-charts.ts` | Canvas chart primitives (time series, gauges, GOP structure, SCTE-35 timeline) |

**UI components:**

| File | Purpose |
|---|---|
| `hud.ts` | Heads-up display badges (codec, resolution, bitrate, health status) |
| `fullscreen-btn.ts` | Fullscreen toggle button for player control bar |

## Build Configuration

Two Vite configs serve different purposes:

- **`vite.config.ts`** — Main app build. Sets `Cross-Origin-Opener-Policy` and `Cross-Origin-Embedder-Policy` headers required by `SharedArrayBuffer` (used by WebCodecs workers). Proxies `/api` to the Prism server during development.
- **`vite.lib.config.ts`** — Library build. Produces a single ES module (`dist-lib/prism.js`) with worker chunks in `dist-lib/assets/`.
