// Package stream tracks the lifecycle of active live streams, providing
// create/remove/list operations used by the ingest and distribution layers.
package stream

import (
	"log/slog"
	"sync"
	"time"
)

// Stream represents a live stream.
type Stream struct {
	Key       string
	StartedAt time.Time
	done      chan struct{}
}

// Manager manages the lifecycle of active streams.
type Manager struct {
	log     *slog.Logger
	mu      sync.RWMutex
	streams map[string]*Stream
}

// NewManager creates a new stream manager. If log is nil, slog.Default() is used.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		log:     log.With("component", "stream-manager"),
		streams: make(map[string]*Stream),
	}
}

// Create registers a new stream. Returns the stream and true if created,
// or nil and false if a stream with this key already exists.
func (m *Manager) Create(key string) (*Stream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.streams[key]; ok {
		m.log.Warn("stream already exists, rejecting duplicate", "key", key)
		return nil, false
	}

	s := &Stream{
		Key:       key,
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	m.streams[key] = s
	m.log.Info("stream created", "key", key)
	return s, true
}

// Remove removes a stream from the manager.
func (m *Manager) Remove(key string) {
	m.mu.Lock()
	s, ok := m.streams[key]
	if ok {
		delete(m.streams, key)
	}
	m.mu.Unlock()

	if ok {
		close(s.done)
		m.log.Info("stream removed", "key", key)
	}
}

// List returns all active streams.
func (m *Manager) List() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streams := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	return streams
}
