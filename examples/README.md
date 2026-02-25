# Examples

Standalone programs demonstrating how to use Prism's packages as a library.

## Go

### Minimal Server

[`minimal-server/main.go`](minimal-server/main.go) — A complete SRT-to-WebTransport server in ~60 lines. Same architecture as `cmd/prism` but stripped to the essentials.

```bash
go run ./examples/minimal-server
ffmpeg -re -i input.ts -c copy -f mpegts srt://localhost:6000?streamid=demo
open https://localhost:4443
```

### Custom Ingest

[`custom-ingest/main.go`](custom-ingest/main.go) — Feed any MPEG-TS `io.Reader` directly into the pipeline, bypassing SRT entirely.

```bash
go run ./examples/custom-ingest input.ts
open https://localhost:4443/?stream=file
```

## Web

### Standalone Player

[`../web/examples/standalone.html`](../web/examples/standalone.html) — Embed `PrismPlayer` in a plain HTML page using the built library bundle.

```bash
cd web && npm run demo:lib   # builds dist-lib/prism.js + starts dev server
# (start the Prism server in another terminal: make run)
open http://localhost:5173/examples/standalone.html?stream=demo
```

## Key Packages

These are the main packages you'll use when embedding Prism:

| Package | Description |
|---|---|
| `certs` | Generate self-signed ECDSA certificates for WebTransport |
| `distribution` | WebTransport server, MoQ sessions, relay fan-out |
| `ingest` | Stream ingest registry (pairs stream keys with pipelines) |
| `ingest/srt` | SRT push server and pull caller |
| `pipeline` | Connects a demuxer to a distribution relay |
| `stream` | Stream lifecycle management |
