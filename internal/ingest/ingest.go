// Package ingest manages active ingest connections, coupling SRT byte
// readers with metadata, lifecycle signaling, and pipeline dispatch.
package ingest

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// InputFormat identifies the container format of an ingested stream.
type InputFormat int

// Supported ingest container formats.
const (
	FormatMPEGTS InputFormat = iota
)

// IngestStats captures connection-level metrics for an ingest stream,
// exposed via the debug API for monitoring source health.
type IngestStats struct {
	BytesReceived int64  `json:"bytesReceived"`
	ReadCount     int64  `json:"readCount"`
	ConnectedAt   int64  `json:"connectedAt"`
	UptimeMs      int64  `json:"uptimeMs"`
	RemoteAddr    string `json:"remoteAddr"`
}

// Stream represents an active ingest connection, coupling the raw byte
// reader with metadata and lifecycle signaling. Bytes written to the
// internal pipe by the SRT receiver are read by the demuxer pipeline.
type Stream struct {
	Key       string
	StartedAt time.Time
	Format    InputFormat
	input     io.ReadCloser
	pw        io.WriteCloser
	done      chan struct{}

	bytesReceived atomic.Int64
	readCount     atomic.Int64
	remoteAddr    atomic.Value
}

// RecordRead increments the byte and read counters, called by the SRT
// receiver after each successful socket read.
func (s *Stream) RecordRead(n int) {
	s.bytesReceived.Add(int64(n))
	s.readCount.Add(1)
}

// SetRemoteAddr stores the remote address of the ingest connection for
// diagnostics.
func (s *Stream) SetRemoteAddr(addr string) {
	s.remoteAddr.Store(addr)
}

// IngestStats returns a snapshot of ingest connection metrics.
func (s *Stream) IngestStats() IngestStats {
	addr, _ := s.remoteAddr.Load().(string)
	return IngestStats{
		BytesReceived: s.bytesReceived.Load(),
		ReadCount:     s.readCount.Load(),
		ConnectedAt:   s.StartedAt.UnixMilli(),
		UptimeMs:      time.Since(s.StartedAt).Milliseconds(),
		RemoteAddr:    addr,
	}
}

// Registry tracks active ingest streams by key and dispatches new streams
// to the onStream callback for pipeline setup. It is the rendezvous point
// between the SRT ingest layer and the demux/distribution pipeline.
type Registry struct {
	mu      sync.RWMutex
	streams map[string]*Stream

	onStream func(key string, input io.Reader, format InputFormat)
}

// NewRegistry creates a Registry. The onStream callback is invoked
// asynchronously whenever a new stream is registered.
func NewRegistry(onStream func(key string, input io.Reader, format InputFormat)) *Registry {
	return &Registry{
		streams:  make(map[string]*Stream),
		onStream: onStream,
	}
}

// Register creates a new ingest stream with the given key and format,
// returning the Stream and a Writer that the SRT receiver should write into.
// If OnStream is set, the callback is invoked asynchronously.
func (r *Registry) Register(key string, format InputFormat) (*Stream, io.Writer) {
	pr, pw := io.Pipe()

	stream := &Stream{
		Key:       key,
		StartedAt: time.Now(),
		Format:    format,
		input:     pr,
		pw:        pw,
		done:      make(chan struct{}),
	}

	r.mu.Lock()
	r.streams[key] = stream
	r.mu.Unlock()

	if r.onStream != nil {
		go r.onStream(key, pr, format)
	}

	return stream, pw
}

// Unregister removes a stream by key, closing its pipe and signaling Done.
func (r *Registry) Unregister(key string) {
	r.mu.Lock()
	stream, ok := r.streams[key]
	if ok {
		delete(r.streams, key)
	}
	r.mu.Unlock()

	if ok {
		stream.pw.Close()
		close(stream.done)
	}
}

// Get returns the Stream for the given key, or false if not found.
func (r *Registry) Get(key string) (*Stream, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.streams[key]
	return s, ok
}
