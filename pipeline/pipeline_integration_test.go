package pipeline

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/media"
)

// testViewer implements distribution.Viewer to collect frames from the relay.
type testViewer struct {
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

func (v *testViewer) ID() string { return v.id }

func (v *testViewer) SendVideo(frame *media.VideoFrame) {
	v.mu.Lock()
	v.videos = append(v.videos, frame)
	v.mu.Unlock()
	v.videoSent.Add(1)
}

func (v *testViewer) SendAudio(frame *media.AudioFrame) {
	v.mu.Lock()
	v.audios = append(v.audios, frame)
	v.mu.Unlock()
	v.audioSent.Add(1)
}

func (v *testViewer) SendCaptions(frame *ccx.CaptionFrame) {
	v.mu.Lock()
	v.captions = append(v.captions, frame)
	v.mu.Unlock()
	v.captionSent.Add(1)
}

func (v *testViewer) Stats() distribution.ViewerStats {
	return distribution.ViewerStats{
		ID:             v.id,
		VideoSent:      v.videoSent.Load(),
		AudioSent:      v.audioSent.Load(),
		CaptionSent:    v.captionSent.Load(),
		VideoDropped:   v.videoDropped.Load(),
		AudioDropped:   v.audioDropped.Load(),
		CaptionDropped: v.captionDropped.Load(),
	}
}

// TestIntegration_TSFileToViewer feeds a real MPEG-TS file through the full
// pipeline (Demuxer → Pipeline → Relay → Viewer) and verifies that video
// and audio frames arrive at the viewer.
func TestIntegration_TSFileToViewer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	const fixture = "../../test/harness/BigBuckBunny_256x144-24fps.ts"
	f, err := os.Open(fixture)
	if err != nil {
		t.Skipf("test fixture not available: %v", err)
	}
	defer f.Close()

	relay := distribution.NewRelay()
	viewer := &testViewer{id: "integration-viewer"}
	relay.AddViewer(viewer)

	p := New("integration-test", f, relay)
	p.SetProtocol("test")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = p.Run(ctx)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// Verify video frames were delivered.
	viewer.mu.Lock()
	videoCount := len(viewer.videos)
	audioCount := len(viewer.audios)
	viewer.mu.Unlock()

	if videoCount == 0 {
		t.Fatal("expected video frames, got 0")
	}
	if audioCount == 0 {
		t.Fatal("expected audio frames, got 0")
	}

	t.Logf("delivered %d video frames, %d audio frames", videoCount, audioCount)

	// At least one keyframe must have been delivered.
	viewer.mu.Lock()
	hasKeyframe := false
	for _, vf := range viewer.videos {
		if vf.IsKeyframe {
			hasKeyframe = true
			break
		}
	}
	viewer.mu.Unlock()

	if !hasKeyframe {
		t.Error("expected at least one keyframe in video frames")
	}

	// Verify pipeline counters match what the viewer saw.
	debug := p.PipelineDebug()
	if debug.VideoForwarded != int64(videoCount) {
		t.Errorf("pipeline VideoForwarded=%d, viewer got %d", debug.VideoForwarded, videoCount)
	}
	if debug.AudioForwarded != int64(audioCount) {
		t.Errorf("pipeline AudioForwarded=%d, viewer got %d", debug.AudioForwarded, audioCount)
	}

	// Snapshot should reflect viewer count.
	snap := p.StreamSnapshot()
	if snap.ViewerCount != 1 {
		t.Errorf("StreamSnapshot.ViewerCount: got %d, want 1", snap.ViewerCount)
	}
	if snap.Video.TotalFrames == 0 {
		t.Error("StreamSnapshot.Video.TotalFrames should be > 0")
	}
}

// TestIntegration_LateJoinGOPReplay feeds a TS file through the pipeline,
// then adds a late-joining viewer and verifies it receives a GOP replay.
func TestIntegration_LateJoinGOPReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	const fixture = "../../test/harness/BigBuckBunny_256x144-24fps.ts"
	f, err := os.Open(fixture)
	if err != nil {
		t.Skipf("test fixture not available: %v", err)
	}
	defer f.Close()

	relay := distribution.NewRelay()

	p := New("late-join-test", f, relay)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = p.Run(ctx)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// After pipeline finishes, add a late-joining viewer.
	lateViewer := &testViewer{id: "late-joiner"}
	relay.AddViewer(lateViewer)

	lateViewer.mu.Lock()
	lateCount := len(lateViewer.videos)
	lateViewer.mu.Unlock()

	if lateCount == 0 {
		t.Fatal("late-joining viewer got 0 frames from GOP replay")
	}

	// First frame from GOP replay should be a keyframe.
	lateViewer.mu.Lock()
	firstFrame := lateViewer.videos[0]
	lateViewer.mu.Unlock()

	if !firstFrame.IsKeyframe {
		t.Error("first frame of GOP replay should be a keyframe")
	}

	t.Logf("late-joining viewer got %d frames from GOP replay", lateCount)
}
