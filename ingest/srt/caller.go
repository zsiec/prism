package srt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	srtgo "github.com/zsiec/srtgo"

	"github.com/zsiec/prism/ingest"
)

// PullRequest describes a remote SRT source to pull from.
type PullRequest struct {
	Address   string `json:"address"`
	StreamKey string `json:"streamKey"`
	StreamID  string `json:"streamId,omitempty"`
}

type activePull struct {
	req    PullRequest
	cancel context.CancelFunc
}

// Caller manages SRT pull connections, dialing remote SRT sources
// and streaming their data into the ingest registry.
type Caller struct {
	log      *slog.Logger
	registry *ingest.Registry

	mu    sync.Mutex
	pulls map[string]*activePull
}

// NewCaller creates a Caller that uses the given registry to register
// pulled streams. If log is nil, slog.Default() is used.
func NewCaller(registry *ingest.Registry, log *slog.Logger) *Caller {
	if log == nil {
		log = slog.Default()
	}
	return &Caller{
		log:      log.With("component", "srt-caller"),
		registry: registry,
		pulls:    make(map[string]*activePull),
	}
}

// Pull dials the remote SRT listener synchronously (with a timeout),
// returning an error if the connection fails. On success, streaming
// continues in a background goroutine.
func (c *Caller) Pull(ctx context.Context, req PullRequest) error {
	if req.Address == "" {
		return fmt.Errorf("address is required")
	}
	if req.StreamKey == "" {
		return fmt.Errorf("streamKey is required")
	}

	c.mu.Lock()
	if _, exists := c.pulls[req.StreamKey]; exists {
		c.mu.Unlock()
		return fmt.Errorf("pull already active for stream key %q", req.StreamKey)
	}
	c.mu.Unlock()

	c.log.Info("dialing", "address", req.Address, "stream_key", req.StreamKey)

	cfg := srtgo.DefaultConfig()
	cfg.Latency = srtLatencyNs

	streamID := req.StreamID
	if streamID == "" {
		streamID = "live/" + req.StreamKey
	}
	cfg.StreamID = streamID

	type dialResult struct {
		conn *srtgo.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := srtgo.Dial(req.Address, cfg)
		ch <- dialResult{conn, err}
	}()

	dialTimeout := 10 * time.Second
	timer := time.NewTimer(dialTimeout)
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("SRT dial failed: %w", res.err)
		}
		return c.startStreaming(ctx, req, res.conn)
	case <-timer.C:
		// Drain the dial result in the background and close any leaked connection.
		go func() {
			if res := <-ch; res.conn != nil {
				res.conn.Close()
			}
		}()
		return fmt.Errorf("SRT dial timed out after %s", dialTimeout)
	case <-ctx.Done():
		// Drain the dial result in the background and close any leaked connection.
		go func() {
			if res := <-ch; res.conn != nil {
				res.conn.Close()
			}
		}()
		return ctx.Err()
	}
}

func (c *Caller) startStreaming(ctx context.Context, req PullRequest, conn *srtgo.Conn) error {
	pullCtx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	if _, exists := c.pulls[req.StreamKey]; exists {
		c.mu.Unlock()
		cancel()
		conn.Close()
		return fmt.Errorf("pull already active for stream key %q", req.StreamKey)
	}
	c.pulls[req.StreamKey] = &activePull{req: req, cancel: cancel}
	c.mu.Unlock()

	c.log.Info("connected", "address", req.Address, "stream_key", req.StreamKey)

	stream, writer := c.registry.Register(req.StreamKey, ingest.FormatMPEGTS)
	stream.SetRemoteAddr(req.Address)

	go func() {
		defer func() {
			conn.Close()
			stats := stream.IngestStats()
			c.registry.Unregister(req.StreamKey)
			c.mu.Lock()
			delete(c.pulls, req.StreamKey)
			c.mu.Unlock()
			c.log.Info("pull ended", "stream_key", req.StreamKey,
				"bytes", stats.BytesReceived, "reads", stats.ReadCount,
				"uptime_ms", stats.UptimeMs)
		}()

		buf := make([]byte, srtReadBufferSize)
		for {
			if pullCtx.Err() != nil {
				break
			}
			n, err := conn.Read(buf)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					c.log.Debug("read error", "stream_key", req.StreamKey, "error", err)
				}
				break
			}
			stream.RecordRead(n)
			if _, err := writer.Write(buf[:n]); err != nil {
				c.log.Debug("pipe write error", "stream_key", req.StreamKey, "error", err)
				break
			}
		}
	}()

	return nil
}

func (c *Caller) Stop(streamKey string) error {
	c.mu.Lock()
	ap, ok := c.pulls[streamKey]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active pull for stream key %q", streamKey)
	}

	ap.cancel()
	return nil
}

func (c *Caller) ActivePulls() []PullRequest {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]PullRequest, 0, len(c.pulls))
	for _, ap := range c.pulls {
		out = append(out, ap.req)
	}
	return out
}
