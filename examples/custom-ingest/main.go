// Custom ingest: feed an MPEG-TS file directly without SRT.
// This demonstrates that the ingest layer is optional — any io.Reader
// producing MPEG-TS data can drive the pipeline.
//
// Usage:
//
//	go run ./examples/custom-ingest input.ts
//	open https://localhost:4443/?stream=file
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/zsiec/prism/certs"
	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/pipeline"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: custom-ingest <file.ts>")
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	cert, err := certs.Generate(14 * 24 * time.Hour)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	distSrv, err := distribution.NewServer(distribution.ServerConfig{
		Addr:   ":4443",
		WebDir: "web/dist",
		Cert:   cert,
	})
	if err != nil {
		log.Fatal(err)
	}

	relay := distSrv.RegisterStream("file")
	p := pipeline.New("file", f, relay)
	p.SetProtocol("File")
	distSrv.SetPipeline("file", p)

	go func() {
		if err := p.Run(ctx); err != nil {
			slog.Error("pipeline finished", "error", err)
		}
		slog.Info("file playback complete")
	}()

	slog.Info("serving file stream", "webtransport", ":4443", "cert_hash", cert.FingerprintBase64())

	if err := distSrv.Start(ctx); err != nil {
		slog.Error("distribution server error", "error", err)
	}
}
