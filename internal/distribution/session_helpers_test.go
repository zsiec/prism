package distribution

import (
	"sync/atomic"
	"testing"

	"github.com/zsiec/prism/internal/media"
)

func TestTrySendVideoKeyframeResetsGroup(t *testing.T) {
	t.Parallel()

	ch := make(chan *media.VideoFrame, 10)
	var damaged atomic.Uint32
	var sent, dropped atomic.Int64

	damaged.Store(5)

	trySendVideo(&media.VideoFrame{IsKeyframe: true, GroupID: 6}, ch, &damaged, &sent, &dropped)

	if damaged.Load() != 0 {
		t.Fatalf("damagedGroup = %d after keyframe, want 0", damaged.Load())
	}
	if sent.Load() != 1 {
		t.Fatalf("sent = %d, want 1", sent.Load())
	}
	if len(ch) != 1 {
		t.Fatalf("channel length = %d, want 1", len(ch))
	}
}

func TestTrySendVideoDropsDamagedGroupDelta(t *testing.T) {
	t.Parallel()

	ch := make(chan *media.VideoFrame, 10)
	var damaged atomic.Uint32
	var sent, dropped atomic.Int64

	damaged.Store(5)

	trySendVideo(&media.VideoFrame{IsKeyframe: false, GroupID: 5}, ch, &damaged, &sent, &dropped)

	if dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want 1", dropped.Load())
	}
	if sent.Load() != 0 {
		t.Fatalf("sent = %d, want 0", sent.Load())
	}
	if len(ch) != 0 {
		t.Fatalf("channel length = %d, want 0", len(ch))
	}
}

func TestTrySendVideoSendsDeltaFromHealthyGroup(t *testing.T) {
	t.Parallel()

	ch := make(chan *media.VideoFrame, 10)
	var damaged atomic.Uint32
	var sent, dropped atomic.Int64

	damaged.Store(3)

	trySendVideo(&media.VideoFrame{IsKeyframe: false, GroupID: 5}, ch, &damaged, &sent, &dropped)

	if sent.Load() != 1 {
		t.Fatalf("sent = %d, want 1", sent.Load())
	}
	if dropped.Load() != 0 {
		t.Fatalf("dropped = %d, want 0", dropped.Load())
	}
}

func TestTrySendVideoFullChannelMarksDamaged(t *testing.T) {
	t.Parallel()

	ch := make(chan *media.VideoFrame, 1)
	var damaged atomic.Uint32
	var sent, dropped atomic.Int64

	// Fill the channel.
	ch <- &media.VideoFrame{}

	trySendVideo(&media.VideoFrame{IsKeyframe: false, GroupID: 7}, ch, &damaged, &sent, &dropped)

	if dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want 1", dropped.Load())
	}
	if damaged.Load() != 7 {
		t.Fatalf("damagedGroup = %d, want 7", damaged.Load())
	}
}

func TestTrySendVideoFullChannelKeyframeNoDamage(t *testing.T) {
	t.Parallel()

	ch := make(chan *media.VideoFrame, 1)
	var damaged atomic.Uint32
	var sent, dropped atomic.Int64

	// Fill the channel.
	ch <- &media.VideoFrame{}

	trySendVideo(&media.VideoFrame{IsKeyframe: true, GroupID: 7}, ch, &damaged, &sent, &dropped)

	if dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want 1", dropped.Load())
	}
	// Keyframe drop should NOT mark group as damaged — the next keyframe will reset anyway.
	if damaged.Load() != 0 {
		t.Fatalf("damagedGroup = %d, want 0", damaged.Load())
	}
}
