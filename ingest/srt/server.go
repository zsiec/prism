package srt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	srtgo "github.com/zsiec/srtgo"

	"github.com/zsiec/prism/ingest"
)

// srtReadBufferSize is the read buffer for SRT socket reads.
// 1316 bytes = 7 MPEG-TS packets (188 * 7), the standard SRT payload size.
const srtReadBufferSize = 1316 * 10

// srtLatencyNs is the SRT latency setting in nanoseconds (120ms).
const srtLatencyNs = 120_000_000

// Server accepts incoming SRT publish connections and registers them
// with the ingest registry for demuxing.
type Server struct {
	log      *slog.Logger
	addr     string
	registry *ingest.Registry
}

// NewServer creates an SRT server that listens on addr and registers
// incoming streams with the given registry. If log is nil, slog.Default() is used.
func NewServer(addr string, registry *ingest.Registry, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:      log.With("component", "srt-server"),
		addr:     addr,
		registry: registry,
	}
}

// Start begins accepting SRT publish connections. It blocks until the
// context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	cfg := srtgo.DefaultConfig()
	cfg.Latency = srtLatencyNs

	l, err := srtgo.Listen(s.addr, cfg)
	if err != nil {
		return fmt.Errorf("SRT listen on %s: %w", s.addr, err)
	}
	s.log.Info("listening", "addr", s.addr)

	l.SetAcceptRejectFunc(func(req srtgo.ConnRequest) srtgo.RejectReason {
		if req.StreamID == "" {
			return srtgo.RejPeer
		}
		return 0
	})

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.log.Warn("accept error", "error", err)
			continue
		}

		streamKey := extractStreamKey(conn.StreamID())
		s.log.Info("publish", "stream_key", streamKey, "remote", conn.RemoteAddr())

		go s.handleConnection(ctx, conn, streamKey)
	}
}

func (s *Server) handleConnection(ctx context.Context, conn *srtgo.Conn, streamKey string) {
	defer conn.Close()

	stream, writer := s.registry.Register(streamKey, ingest.FormatMPEGTS)
	stream.SetRemoteAddr(conn.RemoteAddr().String())

	buf := make([]byte, srtReadBufferSize)
	for {
		if ctx.Err() != nil {
			break
		}
		n, err := conn.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("read error", "stream_key", streamKey, "error", err)
			}
			break
		}
		stream.RecordRead(n)
		if _, err := writer.Write(buf[:n]); err != nil {
			s.log.Debug("pipe write error", "stream_key", streamKey, "error", err)
			break
		}
	}

	stats := stream.IngestStats()
	s.registry.Unregister(streamKey)
	s.log.Info("connection closed", "stream_key", streamKey,
		"bytes", stats.BytesReceived, "reads", stats.ReadCount,
		"uptime_ms", stats.UptimeMs)
}

func extractStreamKey(streamID string) string {
	streamID = strings.TrimPrefix(streamID, "/")
	streamID = strings.TrimPrefix(streamID, "live/")
	if streamID == "" {
		return "default"
	}
	return streamID
}
