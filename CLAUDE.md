---
description: 
alwaysApply: true
---

# Prism — Project Guide

## Build & Test

```bash
# Run all checks (MUST pass before committing)
make check

# Build server
make build

# Run server
make run

# Run web dev server
make dev

# Run tests
make test

# Generate 9 broadcast-realistic test streams (requires ffmpeg)
make gen-streams

# Quick demo: build + push bundled test stream (no ffmpeg needed)
make demo

# Full demo: build + generate + push all 9 streams (requires ffmpeg)
make demo-full
```

## Architecture

Single Go module with web frontend:

- `cmd/prism/` — Entry point, wires everything together
- `ingest/` — Stream ingest (SRT)
- `demux/` — MPEG-TS demuxer, H.264/AAC parsers
- `media/` — Frame types and pipeline orchestration
- `distribution/` — WebTransport server, fan-out relay, MoQ session management
- `moq/` — MoQ Transport wire protocol codec (control messages, format conversion)
- `stream/` — Stream lifecycle management
- `certs/` — Self-signed cert generation for WebTransport
- `pipeline/` — Demux-to-distribution pipeline orchestration
- `web/` — Vanilla TypeScript viewer (Vite, WebTransport, WebCodecs)
- `test/tools/gen-streams/` — Test stream generator (video, audio, captions, SCTE-35, timecode)
- `test/tools/inject-captions/` — CEA-608 caption injector
- `test/tools/inject-timecode/` — SMPTE 12M timecode injector
- `test/tools/inject-scte35/` — SCTE-35 cue injector
- `test/tools/srt-push/` — SRT test stream pusher (`--all` for multi-stream)

## Key Conventions

- Go: `gofmt -s` enforced
- Go: `go vet` must pass
- Go: tests must pass with `-race`
- TypeScript: strict mode
- No AI attribution in commits
