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

| File | Purpose |
|---|---|
| `main.ts` | App entry point — routes to single-stream or multiview |
| `lib.ts` | Library entry point — barrel export for `build:lib` |
| `player.ts` | `PrismPlayer` — orchestrates decoding, rendering, and transport for one stream |
| `multiview.ts` | Multiview manager — 9-tile grid with per-tile audio solo |
| `moq-transport.ts` | MoQ Transport client — WebTransport + MoQ control/data parsing |
| `moq-multiview-transport.ts` | Multi-stream MoQ coordinator |
| `video-decoder.ts` | WebCodecs video decoder with worker offload |
| `video-decoder-worker.ts` | Web Worker for `VideoDecoder` |
| `audio-decoder.ts` | WebCodecs audio decoder with AudioWorklet output |
| `renderer.ts` | Canvas 2D / WebGPU video renderer |
| `captions.ts` | CEA-608/708 caption overlay renderer |
| `metrics-store.ts` | Per-frame metrics collection and health scoring |
| `hud.ts` | Heads-up display badges (codec, resolution, bitrate, etc.) |
| `inspector.ts` | Stream inspector panel with real-time charts |
| `protocol.ts` | Wire protocol types and caption parsing |

## Build Configuration

Two Vite configs serve different purposes:

- **`vite.config.ts`** — Main app build. Sets `Cross-Origin-Opener-Policy` and `Cross-Origin-Embedder-Policy` headers required by `SharedArrayBuffer` (used by WebCodecs workers). Proxies `/api` to the Prism server during development.
- **`vite.lib.config.ts`** — Library build. Produces a single ES module (`dist-lib/prism.js`) with worker chunks in `dist-lib/assets/`.
