# Prism

A low-latency live video server built on SRT ingest and WebTransport delivery, implementing [Media over QUIC Transport](https://datatracker.ietf.org/doc/draft-ietf-moq-transport/) (MoQ) for browser playback via WebCodecs.

Prism accepts MPEG-TS streams over SRT, demuxes H.264/H.265 video and AAC audio, extracts CEA-608/708 captions and SCTE-35 cues, and delivers them to browser viewers over WebTransport with sub-second latency.

## Features

- **SRT ingest** — Push and pull modes via a pure Go SRT implementation
- **MoQ Transport** — IETF draft-15 with LOC media packaging
- **H.264 and H.265** — Full NAL unit parsing, SPS extraction, codec string generation
- **Multi-track AAC audio** — Dynamic subscription and switching
- **CEA-608/708 captions** — Extracted from H.264 SEI messages
- **SCTE-35** — Splice insert and time signal parsing
- **SMPTE 12M timecode** — Extracted from pic_timing SEI
- **GOP cache** — Late-joining viewers start from the most recent keyframe
- **Multiview** — 9-stream composited grid with per-tile audio solo
- **WebCodecs decoding** — Hardware-accelerated video/audio decode in the browser

## Quick Start

**Prerequisites:** Go 1.24+, Node.js 22+

```bash
make demo
```

This builds the server, builds the web viewer, and pushes a bundled test stream. Open `https://localhost:4444/?stream=demo` in your browser and accept the self-signed certificate.

### Full broadcast demo

For the complete 9-stream experience with captions, SCTE-35 cues, timecode, and multi-track audio:

**Additional prerequisites:** [ffmpeg](https://ffmpeg.org/) (includes ffprobe)

```bash
make demo-full
```

This downloads Blender open-movie sources (~2 GB on first run), encodes 9 broadcast-realistic streams, and pushes them all simultaneously. Open `https://localhost:4444/` to see the multiview grid.

### Push your own stream

```bash
ffmpeg -re -i input.ts -c copy -f mpegts srt://localhost:6000?streamid=mystream
```

Then open `https://localhost:4444/?stream=mystream`.

## Architecture

```
SRT socket ──> io.Pipe ──> MPEG-TS Demuxer ──> Pipeline ──> Relay ──> Viewers
                               │                              │
                          H.264/H.265                   GOP cache
                          AAC (multi-track)             Fan-out
                          CEA-608/708                   Pre-computed wire data
                          SCTE-35
                          SMPTE 12M timecode
```

Single Go binary, vanilla TypeScript frontend:

| Package | Purpose |
|---|---|
| `cmd/prism/` | Entry point, wires everything together |
| `internal/ingest/` | Stream ingest registry |
| `internal/ingest/srt/` | SRT server (push) and caller (pull) |
| `internal/demux/` | MPEG-TS demuxer, H.264/H.265/AAC parsers |
| `internal/media/` | Frame types (`VideoFrame`, `AudioFrame`) |
| `internal/distribution/` | WebTransport server, MoQ sessions, relay fan-out |
| `internal/moq/` | MoQ Transport wire protocol codec |
| `internal/pipeline/` | Demux-to-distribution orchestration |
| `internal/stream/` | Stream lifecycle management |
| `internal/mpegts/` | Low-level MPEG-TS packet/PES/PSI parsing |
| `internal/scte35/` | SCTE-35 splice info encoding/decoding |
| `internal/certs/` | Self-signed ECDSA certificate generation |
| `internal/webtransport/` | WebTransport server on quic-go/HTTP3 |
| `web/` | Vanilla TypeScript viewer (Vite, WebTransport, WebCodecs) |

## Configuration

Environment variables with defaults:

| Variable | Default | Description |
|---|---|---|
| `SRT_ADDR` | `:6000` | SRT ingest listen address |
| `WT_ADDR` | `:4443` | WebTransport listen address |
| `API_ADDR` | `:4444` | HTTPS REST API listen address |
| `WEB_DIR` | `web/dist` | Static file directory for the viewer |
| `DEBUG` | *(unset)* | Set to any value to enable debug logging |

The server listens on:
- `:6000` — SRT ingest
- `:4443` — WebTransport (MoQ)
- `:4444` — HTTPS REST API + web viewer

## REST API

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/streams` | List active streams |
| `GET` | `/api/streams/{key}/debug` | Stream debug diagnostics |
| `GET` | `/api/cert-hash` | WebTransport certificate hash |
| `POST` | `/api/srt-pull` | Start an SRT pull from a remote address |
| `GET` | `/api/srt-pull` | List active SRT pulls |
| `DELETE` | `/api/srt-pull?streamKey=...` | Stop an SRT pull |

## Development

```bash
# Run all checks (must pass before committing)
make check

# Run tests with race detector
make test

# Format code
make fmt

# Build and run the server
make run

# Run web dev server with hot reload (port 5173, proxies API to :4444)
make dev

# Quick demo with bundled test stream
make demo

# Full 9-stream broadcast demo (requires ffmpeg)
make demo-full
```

`make check` requires [staticcheck](https://staticcheck.dev/) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck):

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

## Security Considerations

Prism is designed for development and local-network use. If you expose it to untrusted networks, be aware of the following:

- **CORS** — `Access-Control-Allow-Origin: *` is set on all responses. Production deployments should restrict this at a reverse proxy layer.
- **WebTransport origins** — `CheckOrigin` accepts all origins. Production deployments should enforce origin checks at the proxy layer.
- **SRT pull endpoint** — `POST /api/srt-pull` accepts arbitrary addresses, which could be used for SSRF. Restrict this endpoint to authenticated operators or internal networks.
- **Self-signed certificates** — The server generates a self-signed certificate at startup. Production deployments should use proper TLS certificates.

See [SECURITY.md](SECURITY.md) for the vulnerability reporting policy.

## Dependencies

Four direct Go dependencies:

| Dependency | License | Purpose |
|---|---|---|
| [quic-go/quic-go](https://github.com/quic-go/quic-go) | MIT | QUIC + HTTP/3 for WebTransport |
| [zsiec/ccx](https://github.com/zsiec/ccx) | MIT | CEA-608/708 closed caption extraction |
| [zsiec/srtgo](https://github.com/zsiec/srtgo) | MIT | Pure Go SRT implementation |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) | BSD-3-Clause | `errgroup` for structured concurrency |

## License

[MIT](LICENSE)
