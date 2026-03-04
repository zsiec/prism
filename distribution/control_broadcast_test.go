package distribution

import (
	"context"
	"testing"
	"time"
)

func TestControlBroadcasterFanOut(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte, 10)
	go b.Run(context.Background(), source)

	ch1 := b.Subscribe("s1")
	ch2 := b.Subscribe("s2")

	source <- []byte(`{"state":"live"}`)

	// Both subscribers should receive the same message.
	for _, tc := range []struct {
		name string
		ch   <-chan []byte
	}{
		{"s1", ch1},
		{"s2", ch2},
	} {
		select {
		case data := <-tc.ch:
			if string(data) != `{"state":"live"}` {
				t.Fatalf("%s got %q", tc.name, data)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s timeout", tc.name)
		}
	}
}

func TestControlBroadcasterUnsubscribe(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte, 10)
	go b.Run(context.Background(), source)

	ch1 := b.Subscribe("s1")
	_ = b.Subscribe("s2")

	b.Unsubscribe("s2")

	source <- []byte(`{"state":"off"}`)

	// s1 should still receive.
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("s1 timeout after s2 unsubscribe")
	}
}

func TestControlBroadcasterUnsubscribeIdempotent(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte, 10)
	go b.Run(context.Background(), source)

	b.Subscribe("s1")
	b.Unsubscribe("s1")
	b.Unsubscribe("s1") // second call must not panic
}

func TestControlBroadcasterSourceClose(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte)
	go b.Run(context.Background(), source)

	ch := b.Subscribe("s1")
	close(source)

	// Subscriber channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}

func TestControlBroadcasterContextCancel(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte) // unbuffered, never closed

	ctx, cancel := context.WithCancel(context.Background())
	go b.Run(ctx, source)

	ch := b.Subscribe("s1")
	cancel()

	// Subscriber channel should be closed when context is cancelled.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for channel close after ctx cancel")
	}
}

func TestControlBroadcasterDropOnFull(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte, 100)
	go b.Run(context.Background(), source)

	ch := b.Subscribe("s1")

	// Send more messages than the buffer can hold.
	for i := 0; i < viewerControlBuffer+5; i++ {
		source <- []byte(`{"i":"data"}`)
	}

	// Allow broadcaster goroutine to process.
	time.Sleep(50 * time.Millisecond)

	// Should have exactly viewerControlBuffer messages (extras dropped).
	if len(ch) != viewerControlBuffer {
		t.Fatalf("channel length = %d, want %d", len(ch), viewerControlBuffer)
	}
}

func TestControlBroadcasterNoSubscribers(t *testing.T) {
	t.Parallel()
	b := NewControlBroadcaster()
	source := make(chan []byte, 10)
	go b.Run(context.Background(), source)

	// Send with no subscribers — should not block or panic.
	source <- []byte(`{"empty":"room"}`)

	// Allow processing, then close cleanly.
	close(source)
}
