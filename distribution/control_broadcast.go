package distribution

import (
	"context"
	"sync"
)

// viewerControlBuffer is the per-subscriber channel buffer size for control
// messages. Matches viewerCaptionBuffer since control messages are similarly
// low-frequency.
const viewerControlBuffer = 10

// ControlBroadcaster fans out messages from a single source channel to
// multiple subscriber channels. Each subscriber gets its own buffered
// channel; slow subscribers have messages dropped (non-blocking send)
// rather than blocking other subscribers or the source.
type ControlBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]chan []byte
}

// NewControlBroadcaster creates a ControlBroadcaster ready for use.
func NewControlBroadcaster() *ControlBroadcaster {
	return &ControlBroadcaster{
		subscribers: make(map[string]chan []byte),
	}
}

// Subscribe creates a per-subscriber buffered channel and returns it.
// The caller must call Unsubscribe when done. If a channel already
// exists for the given id, it is closed and replaced.
func (b *ControlBroadcaster) Subscribe(id string) <-chan []byte {
	ch := make(chan []byte, viewerControlBuffer)
	b.mu.Lock()
	if old, ok := b.subscribers[id]; ok {
		close(old)
	}
	b.subscribers[id] = ch
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes the subscriber's channel. It is safe
// to call multiple times for the same id.
func (b *ControlBroadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	ch, ok := b.subscribers[id]
	if ok {
		delete(b.subscribers, id)
		close(ch)
	}
	b.mu.Unlock()
}

// Run reads from the source channel and fans out each message to all
// subscribers. It blocks until ctx is cancelled or the source channel
// is closed. Non-blocking sends: if a subscriber's channel is full,
// the message is dropped for that subscriber (matching the Viewer
// drop pattern).
func (b *ControlBroadcaster) Run(ctx context.Context, source <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			b.closeAll()
			return
		case data, ok := <-source:
			if !ok {
				b.closeAll()
				return
			}
			b.mu.RLock()
			for _, ch := range b.subscribers {
				select {
				case ch <- data:
				default:
					// subscriber is slow; drop message
				}
			}
			b.mu.RUnlock()
		}
	}
}

// closeAll closes and removes all subscriber channels.
func (b *ControlBroadcaster) closeAll() {
	b.mu.Lock()
	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
}
