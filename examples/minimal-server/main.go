// Minimal Prism server: SRT ingest → demux → pipeline → relay → WebTransport.
// This demonstrates wiring the core packages together — the same pattern
// used by cmd/prism but stripped to the essentials.
//
// Usage:
//
//	go run ./examples/minimal-server
//	ffmpeg -re -i input.ts -c copy -f mpegts srt://localhost:6000?streamid=demo
//	open https://localhost:4443
package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/zsiec/prism/certs"
	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/ingest"
	srtingest "github.com/zsiec/prism/ingest/srt"
	"github.com/zsiec/prism/pipeline"
	"github.com/zsiec/prism/stream"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cert, err := certs.Generate(14 * 24 * time.Hour)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	mgr := stream.NewManager(nil)

	var distSrv *distribution.Server

	registry := ingest.NewRegistry(func(key string, input io.Reader, _ ingest.InputFormat) {
		if _, created := mgr.Create(key); !created {
			return
		}
		defer func() {
			distSrv.UnregisterStream(key)
			mgr.Remove(key)
		}()

		relay := distSrv.RegisterStream(key)
		p := pipeline.New(key, input, relay)
		p.SetProtocol("SRT")
		distSrv.SetPipeline(key, p)

		if err := p.Run(ctx); err != nil {
			slog.Error("pipeline error", "stream", key, "error", err)
		}
	})

	distSrv, err = distribution.NewServer(distribution.ServerConfig{
		Addr:   ":4443",
		WebDir: "web/dist",
		Cert:   cert,
	})
	if err != nil {
		log.Fatal(err)
	}

	srtSrv := srtingest.NewServer(":6000", registry, nil)

	go func() {
		if err := srtSrv.Start(ctx); err != nil {
			slog.Error("SRT server error", "error", err)
			cancel()
		}
	}()

	slog.Info("prism minimal server", "srt", ":6000", "webtransport", ":4443", "cert_hash", cert.FingerprintBase64())

	if err := distSrv.Start(ctx); err != nil {
		slog.Error("distribution server error", "error", err)
	}
}
