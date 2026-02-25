package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/zsiec/prism/certs"
	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/ingest"
	srtingest "github.com/zsiec/prism/ingest/srt"
	"github.com/zsiec/prism/pipeline"
	"github.com/zsiec/prism/stream"
)

var version = "dev"

func main() {
	level := slog.LevelInfo
	if os.Getenv("DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	slog.Info("generating self-signed certificate")
	cert, err := certs.Generate(14 * 24 * time.Hour)
	if err != nil {
		slog.Error("failed to generate cert", "error", err)
		os.Exit(1)
	}
	slog.Info("certificate generated",
		"fingerprint", cert.FingerprintBase64(),
		"expires", cert.NotAfter.Format(time.RFC3339),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	a := &app{
		mgr: stream.NewManager(nil),
	}

	wtAddr := envOr("WT_ADDR", ":4443")
	webDir := envOr("WEB_DIR", "web/dist")
	srtAddr := envOr("SRT_ADDR", ":6000")
	apiAddr := envOr("API_ADDR", ":4444")

	slog.Info("prism starting",
		"version", version,
		"srt", srtAddr,
		"webtransport", wtAddr,
		"api", apiAddr,
		"cert_hash", cert.FingerprintBase64(),
	)

	g, ctx := errgroup.WithContext(ctx)

	// Create registry and SRT caller after errgroup so closures capture the
	// errgroup-derived context, ensuring streams shut down when any component fails.
	a.registry = ingest.NewRegistry(func(key string, input io.Reader, format ingest.InputFormat) {
		a.handleNewStream(ctx, key, input, format)
	})
	a.srtCaller = srtingest.NewCaller(a.registry, nil)

	var distErr error
	a.distSrv, distErr = distribution.NewServer(distribution.ServerConfig{
		Addr:   wtAddr,
		WebDir: webDir,
		Cert:   cert,
		SRTPull: func(address, streamKey, streamID string) error {
			return a.srtCaller.Pull(ctx, srtingest.PullRequest{
				Address:   address,
				StreamKey: streamKey,
				StreamID:  streamID,
			})
		},
		SRTStop: func(streamKey string) error {
			return a.srtCaller.Stop(streamKey)
		},
		SRTList:      a.listSRTPulls,
		StreamLister: a.listStreams,
		IngestLookup: a.lookupIngest,
	})
	if distErr != nil {
		slog.Error("failed to create distribution server", "error", distErr)
		os.Exit(1)
	}

	srtSrv := srtingest.NewServer(srtAddr, a.registry, nil)

	apiSrv := &http.Server{
		Addr:    apiAddr,
		Handler: a.distSrv.APIHandler(),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert.TLSCert},
		},
	}

	g.Go(func() error {
		return srtSrv.Start(ctx)
	})

	g.Go(func() error {
		slog.Info("HTTPS API server listening", "addr", apiAddr)
		if err := apiSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("API server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return apiSrv.Shutdown(shutdownCtx)
	})

	g.Go(func() error {
		return a.distSrv.Start(ctx)
	})

	if err := g.Wait(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

type app struct {
	mgr       *stream.Manager
	registry  *ingest.Registry
	srtCaller *srtingest.Caller
	distSrv   *distribution.Server
}

func (a *app) listSRTPulls() []distribution.SRTPullInfo {
	pulls := a.srtCaller.ActivePulls()
	out := make([]distribution.SRTPullInfo, len(pulls))
	for i, p := range pulls {
		out[i] = distribution.SRTPullInfo{
			Address:   p.Address,
			StreamKey: p.StreamKey,
			StreamID:  p.StreamID,
		}
	}
	return out
}

func (a *app) listStreams() []distribution.StreamInfo {
	streams := a.mgr.List()
	infos := make([]distribution.StreamInfo, len(streams))
	for i, s := range streams {
		relay := a.distSrv.GetRelay(s.Key)
		viewers := 0
		if relay != nil {
			viewers = relay.ViewerCount()
		}
		info := distribution.StreamInfo{
			Key:     s.Key,
			Viewers: viewers,
		}

		p := a.distSrv.GetPipeline(s.Key)
		if p != nil {
			snap := p.StreamSnapshot()
			info.VideoCodec = snap.Video.Codec
			info.Width = snap.Video.Width
			info.Height = snap.Video.Height
			info.AudioTracks = len(snap.Audio)
			for _, audio := range snap.Audio {
				info.AudioChannels += audio.Channels
			}
			info.HasCaptions = snap.Captions.TotalFrames > 0
			info.CaptionChannels = snap.Captions.ActiveChannels
			info.HasSCTE35 = snap.SCTE35.TotalEvents > 0
			info.Protocol = snap.Protocol
			info.UptimeMs = snap.UptimeMs
			info.Description = buildStreamDescription(info)
		}

		infos[i] = info
	}
	return infos
}

func (a *app) lookupIngest(key string) *distribution.IngestDebugStats {
	stream, ok := a.registry.Get(key)
	if !ok {
		return nil
	}
	s := stream.IngestStats()
	return &distribution.IngestDebugStats{
		BytesReceived: s.BytesReceived,
		ReadCount:     s.ReadCount,
		ConnectedAt:   s.ConnectedAt,
		UptimeMs:      s.UptimeMs,
		RemoteAddr:    s.RemoteAddr,
	}
}

func (a *app) handleNewStream(ctx context.Context, key string, input io.Reader, format ingest.InputFormat) {
	slog.Info("new stream from ingest", "key", key)

	if _, created := a.mgr.Create(key); !created {
		slog.Warn("rejecting duplicate stream connection", "key", key)
		return
	}
	defer a.teardownStream(key)

	relay := a.distSrv.RegisterStream(key)

	p := pipeline.New(key, input, relay)
	p.SetProtocol("SRT")
	a.distSrv.SetPipeline(key, p)

	if err := p.Run(ctx); err != nil {
		slog.Error("pipeline error", "stream", key, "error", err)
	}
	slog.Info("stream ended", "key", key)
}

// teardownStream removes all resources for a stream across the distribution
// server and stream manager in a single call.
func (a *app) teardownStream(key string) {
	a.distSrv.UnregisterStream(key)
	a.mgr.Remove(key)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildStreamDescription(info distribution.StreamInfo) string {
	var parts []string

	if info.Width > 0 && info.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", info.Width, info.Height))
	}

	if info.AudioTracks > 0 {
		if info.AudioTracks == 1 {
			parts = append(parts, "1 audio track")
		} else {
			parts = append(parts, fmt.Sprintf("%d audio tracks", info.AudioTracks))
		}
	}

	if info.HasCaptions {
		n := len(info.CaptionChannels)
		if n > 0 {
			parts = append(parts, fmt.Sprintf("CC (%d ch)", n))
		} else {
			parts = append(parts, "CC")
		}
	}

	if info.HasSCTE35 {
		parts = append(parts, "SCTE-35")
	}

	return strings.Join(parts, " · ")
}
