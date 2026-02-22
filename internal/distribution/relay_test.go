package distribution

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/internal/media"
)

// mockViewer implements the Viewer interface for testing.
type mockViewer struct {
	id       string
	mu       sync.Mutex
	videos   []*media.VideoFrame
	audios   []*media.AudioFrame
	captions []*ccx.CaptionFrame

	videoSent      atomic.Int64
	audioSent      atomic.Int64
	captionSent    atomic.Int64
	videoDropped   atomic.Int64
	audioDropped   atomic.Int64
	captionDropped atomic.Int64
}

func newMockViewer(id string) *mockViewer {
	return &mockViewer{id: id}
}

func (m *mockViewer) ID() string { return m.id }

func (m *mockViewer) SendVideo(frame *media.VideoFrame) {
	m.mu.Lock()
	m.videos = append(m.videos, frame)
	m.mu.Unlock()
	m.videoSent.Add(1)
}

func (m *mockViewer) SendAudio(frame *media.AudioFrame) {
	m.mu.Lock()
	m.audios = append(m.audios, frame)
	m.mu.Unlock()
	m.audioSent.Add(1)
}

func (m *mockViewer) SendCaptions(frame *ccx.CaptionFrame) {
	m.mu.Lock()
	m.captions = append(m.captions, frame)
	m.mu.Unlock()
	m.captionSent.Add(1)
}

func (m *mockViewer) Stats() ViewerStats {
	return ViewerStats{
		ID:             m.id,
		VideoSent:      m.videoSent.Load(),
		AudioSent:      m.audioSent.Load(),
		CaptionSent:    m.captionSent.Load(),
		VideoDropped:   m.videoDropped.Load(),
		AudioDropped:   m.audioDropped.Load(),
		CaptionDropped: m.captionDropped.Load(),
	}
}

func (m *mockViewer) videoCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.videos)
}

func TestRelayAddRemoveViewer(t *testing.T) {
	t.Parallel()

	r := NewRelay()
	v := newMockViewer("v1")

	r.AddViewer(v)
	if r.ViewerCount() != 1 {
		t.Errorf("ViewerCount: got %d, want 1", r.ViewerCount())
	}

	r.RemoveViewer("v1")
	if r.ViewerCount() != 0 {
		t.Errorf("ViewerCount: got %d, want 0", r.ViewerCount())
	}
}

func TestRelayBroadcastVideo(t *testing.T) {
	t.Parallel()

	r := NewRelay()
	v1 := newMockViewer("v1")
	v2 := newMockViewer("v2")

	r.AddViewer(v1)
	r.AddViewer(v2)

	frame := &media.VideoFrame{
		PTS:        1000,
		IsKeyframe: true,
		GroupID:    1,
		NALUs:      [][]byte{{0x65, 0x00}},
	}
	r.BroadcastVideo(frame)

	if v1.videoCount() != 1 {
		t.Errorf("v1 video count: got %d, want 1", v1.videoCount())
	}
	if v2.videoCount() != 1 {
		t.Errorf("v2 video count: got %d, want 1", v2.videoCount())
	}
}

func TestRelayBroadcastAudio(t *testing.T) {
	t.Parallel()

	r := NewRelay()
	v := newMockViewer("v1")
	r.AddViewer(v)

	frame := &media.AudioFrame{PTS: 1000, Data: []byte{0xFF}}
	r.BroadcastAudio(frame)

	if v.audioSent.Load() != 1 {
		t.Errorf("audio sent: got %d, want 1", v.audioSent.Load())
	}
}

func TestRelayBroadcastCaptions(t *testing.T) {
	t.Parallel()

	r := NewRelay()
	v := newMockViewer("v1")
	r.AddViewer(v)

	frame := &ccx.CaptionFrame{PTS: 1000}
	r.BroadcastCaptions(frame)

	if v.captionSent.Load() != 1 {
		t.Errorf("caption sent: got %d, want 1", v.captionSent.Load())
	}
}

func TestRelayGOPReplayOrdering(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	// Build a GOP: keyframe + 2 delta frames
	keyframe := &media.VideoFrame{
		PTS: 1000, IsKeyframe: true, GroupID: 1,
		NALUs: [][]byte{{0x65, 0x00}},
	}
	delta1 := &media.VideoFrame{
		PTS: 2000, IsKeyframe: false, GroupID: 1,
		NALUs: [][]byte{{0x41, 0x01}},
	}
	delta2 := &media.VideoFrame{
		PTS: 3000, IsKeyframe: false, GroupID: 1,
		NALUs: [][]byte{{0x41, 0x02}},
	}

	r.BroadcastVideo(keyframe)
	r.BroadcastVideo(delta1)
	r.BroadcastVideo(delta2)

	// Late-joining viewer should get all 3 frames from GOP replay
	v := newMockViewer("late")
	r.AddViewer(v)

	if v.videoCount() != 3 {
		t.Fatalf("GOP replay: got %d frames, want 3", v.videoCount())
	}

	v.mu.Lock()
	if v.videos[0].PTS != 1000 {
		t.Errorf("first frame PTS: got %d, want 1000", v.videos[0].PTS)
	}
	if v.videos[1].PTS != 2000 {
		t.Errorf("second frame PTS: got %d, want 2000", v.videos[1].PTS)
	}
	if v.videos[2].PTS != 3000 {
		t.Errorf("third frame PTS: got %d, want 3000", v.videos[2].PTS)
	}
	v.mu.Unlock()
}

func TestRelayGOPResetOnKeyframe(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	// First GOP
	r.BroadcastVideo(&media.VideoFrame{
		PTS: 1000, IsKeyframe: true, GroupID: 1,
		NALUs: [][]byte{{0x65}},
	})
	r.BroadcastVideo(&media.VideoFrame{
		PTS: 2000, IsKeyframe: false, GroupID: 1,
		NALUs: [][]byte{{0x41}},
	})

	// New keyframe resets GOP cache
	r.BroadcastVideo(&media.VideoFrame{
		PTS: 3000, IsKeyframe: true, GroupID: 2,
		NALUs: [][]byte{{0x65}},
	})

	v := newMockViewer("late")
	r.AddViewer(v)

	// Should only get 1 frame (the new keyframe)
	if v.videoCount() != 1 {
		t.Errorf("GOP replay after reset: got %d frames, want 1", v.videoCount())
	}
}

func TestRelayWaitVideoInfo(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	// Before any video info, should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if r.WaitVideoInfo(ctx) {
		t.Error("expected WaitVideoInfo to return false before video info set")
	}

	// SetVideoInfo triggers the ready signal
	r.SetVideoInfo(VideoInfo{
		Codec:  "avc1.64001f",
		Width:  1280,
		Height: 720,
	})

	// Now should return immediately
	ctx2, cancel2 := context.WithTimeout(context.Background(), 0)
	defer cancel2()
	if !r.WaitVideoInfo(ctx2) {
		t.Error("expected WaitVideoInfo to return true after video info set")
	}

	vi := r.VideoInfo()
	if vi.Width != 1280 || vi.Height != 720 {
		t.Errorf("VideoInfo: got %dx%d, want 1280x720", vi.Width, vi.Height)
	}
}

func TestRelayViewerCount(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	if r.ViewerCount() != 0 {
		t.Errorf("initial ViewerCount: got %d, want 0", r.ViewerCount())
	}

	r.AddViewer(newMockViewer("a"))
	r.AddViewer(newMockViewer("b"))
	r.AddViewer(newMockViewer("c"))

	if r.ViewerCount() != 3 {
		t.Errorf("ViewerCount: got %d, want 3", r.ViewerCount())
	}

	r.RemoveViewer("b")
	if r.ViewerCount() != 2 {
		t.Errorf("ViewerCount after remove: got %d, want 2", r.ViewerCount())
	}
}

func TestRelayViewerStatsAll(t *testing.T) {
	t.Parallel()

	r := NewRelay()
	v := newMockViewer("v1")
	r.AddViewer(v)

	r.BroadcastVideo(&media.VideoFrame{
		PTS: 1000, IsKeyframe: true, GroupID: 1,
		NALUs: [][]byte{{0x65}},
	})

	stats := r.ViewerStatsAll()
	if len(stats) != 1 {
		t.Fatalf("ViewerStatsAll: got %d entries, want 1", len(stats))
	}
	if stats[0].VideoSent != 1 {
		t.Errorf("VideoSent: got %d, want 1", stats[0].VideoSent)
	}
}

func TestRelayAudioTrackCount(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	// Default should be 1
	if r.AudioTrackCount() != 1 {
		t.Errorf("default AudioTrackCount: got %d, want 1", r.AudioTrackCount())
	}

	r.SetAudioTrackCount(3)
	if r.AudioTrackCount() != 3 {
		t.Errorf("AudioTrackCount: got %d, want 3", r.AudioTrackCount())
	}

	// Setting to 0 should default to 1
	r.SetAudioTrackCount(0)
	if r.AudioTrackCount() != 1 {
		t.Errorf("AudioTrackCount after 0: got %d, want 1", r.AudioTrackCount())
	}
}

func TestRelayAudioInfo(t *testing.T) {
	t.Parallel()

	r := NewRelay()

	// Default
	ai := r.AudioInfo()
	if ai.Codec != "mp4a.40.02" {
		t.Errorf("default codec: got %q, want mp4a.40.02", ai.Codec)
	}

	r.SetAudioInfo(AudioInfo{Codec: "mp4a.40.02", SampleRate: 44100, Channels: 1})
	ai = r.AudioInfo()
	if ai.SampleRate != 44100 || ai.Channels != 1 {
		t.Errorf("AudioInfo: got %d/%d, want 44100/1", ai.SampleRate, ai.Channels)
	}
}
