package dash

import (
	"testing"
	"time"

	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/media"
)

// mockBroadcaster captures all calls made by the dashPipeline for test assertions.
type mockBroadcaster struct {
	videoFrames []*media.VideoFrame
	audioFrames []*media.AudioFrame
	videoInfo   *distribution.VideoInfo
	audioInfo   *distribution.AudioInfo
	trackCount  int
}

func (m *mockBroadcaster) BroadcastVideo(frame *media.VideoFrame) {
	m.videoFrames = append(m.videoFrames, frame)
}

func (m *mockBroadcaster) BroadcastAudio(frame *media.AudioFrame) {
	m.audioFrames = append(m.audioFrames, frame)
}

func (m *mockBroadcaster) SetVideoInfo(info distribution.VideoInfo) {
	m.videoInfo = &info
}

func (m *mockBroadcaster) SetAudioTrackCount(count int) {
	m.trackCount = count
}

func (m *mockBroadcaster) AudioTrackCount() int {
	if m.trackCount == 0 {
		return 1
	}
	return m.trackCount
}

func (m *mockBroadcaster) SetAudioInfo(info distribution.AudioInfo) {
	m.audioInfo = &info
}

func (m *mockBroadcaster) ViewerCount() int {
	return 0
}

func (m *mockBroadcaster) ViewerStatsAll() []distribution.ViewerStats {
	return nil
}

func TestDASHPipelineProcessVideoSamples(t *testing.T) {
	t.Parallel()

	mock := &mockBroadcaster{}
	seqHdr := []byte{0x0A, 0x0B, 0x0C}
	p := newDASHPipeline("test-stream", mock, seqHdr)

	samples := []mediaSample{
		{PTS: 0, DTS: 0, Data: []byte{0x01, 0x02}, IsKeyframe: true},
		{PTS: 33333, DTS: 33333, Data: []byte{0x03, 0x04}, IsKeyframe: false},
		{PTS: 66666, DTS: 66666, Data: []byte{0x05, 0x06}, IsKeyframe: false},
	}

	p.processVideoSamples(samples)

	if len(mock.videoFrames) != 3 {
		t.Fatalf("expected 3 video frames, got %d", len(mock.videoFrames))
	}

	// All frames should have codec "av1".
	for i, f := range mock.videoFrames {
		if f.Codec != "av1" {
			t.Errorf("frame %d: codec = %q, want %q", i, f.Codec, "av1")
		}
	}

	// First frame: keyframe with SPS set.
	kf := mock.videoFrames[0]
	if !kf.IsKeyframe {
		t.Error("frame 0: expected IsKeyframe=true")
	}
	if kf.SPS == nil {
		t.Error("frame 0: expected SPS to be set for keyframe")
	}
	if len(kf.SPS) != 3 || kf.SPS[0] != 0x0A {
		t.Errorf("frame 0: SPS = %v, want %v", kf.SPS, seqHdr)
	}

	// Delta frames: no SPS.
	for i := 1; i < 3; i++ {
		df := mock.videoFrames[i]
		if df.IsKeyframe {
			t.Errorf("frame %d: expected IsKeyframe=false", i)
		}
		if df.SPS != nil {
			t.Errorf("frame %d: expected SPS=nil for delta frame, got %v", i, df.SPS)
		}
	}

	// GroupID should be 1 (incremented once for the keyframe).
	if kf.GroupID != 1 {
		t.Errorf("frame 0: GroupID = %d, want 1", kf.GroupID)
	}
	for i := 1; i < 3; i++ {
		if mock.videoFrames[i].GroupID != 1 {
			t.Errorf("frame %d: GroupID = %d, want 1", i, mock.videoFrames[i].GroupID)
		}
	}

	// WireData should match sample data.
	for i, s := range samples {
		f := mock.videoFrames[i]
		if len(f.WireData) != len(s.Data) {
			t.Errorf("frame %d: WireData length = %d, want %d", i, len(f.WireData), len(s.Data))
		}
		for j := range s.Data {
			if f.WireData[j] != s.Data[j] {
				t.Errorf("frame %d: WireData[%d] = %d, want %d", i, j, f.WireData[j], s.Data[j])
			}
		}
	}

	// Forwarded counter.
	if got := p.videoForwarded.Load(); got != 3 {
		t.Errorf("videoForwarded = %d, want 3", got)
	}
}

func TestDASHPipelineProcessAudioSamples(t *testing.T) {
	t.Parallel()

	mock := &mockBroadcaster{}
	p := newDASHPipeline("test-stream", mock, nil)

	samples := []mediaSample{
		{PTS: 0, Data: []byte{0xAA, 0xBB}},
		{PTS: 21333, Data: []byte{0xCC, 0xDD, 0xEE}},
	}

	p.processAudioSamples(samples)

	if len(mock.audioFrames) != 2 {
		t.Fatalf("expected 2 audio frames, got %d", len(mock.audioFrames))
	}

	for i, s := range samples {
		f := mock.audioFrames[i]
		if f.PTS != s.PTS {
			t.Errorf("frame %d: PTS = %d, want %d", i, f.PTS, s.PTS)
		}
		if len(f.Data) != len(s.Data) {
			t.Errorf("frame %d: Data length = %d, want %d", i, len(f.Data), len(s.Data))
			continue
		}
		for j := range s.Data {
			if f.Data[j] != s.Data[j] {
				t.Errorf("frame %d: Data[%d] = %d, want %d", i, j, f.Data[j], s.Data[j])
			}
		}
	}

	if got := p.audioForwarded.Load(); got != 2 {
		t.Errorf("audioForwarded = %d, want 2", got)
	}
}

func TestDASHPipelineGroupIDIncrement(t *testing.T) {
	t.Parallel()

	mock := &mockBroadcaster{}
	seqHdr := []byte{0x01}
	p := newDASHPipeline("test-stream", mock, seqHdr)

	// First GOP: keyframe + delta.
	p.processVideoSamples([]mediaSample{
		{PTS: 0, DTS: 0, IsKeyframe: true, Data: []byte{0x10}},
		{PTS: 33333, DTS: 33333, IsKeyframe: false, Data: []byte{0x11}},
	})

	// Second GOP: another keyframe + delta.
	p.processVideoSamples([]mediaSample{
		{PTS: 66666, DTS: 66666, IsKeyframe: true, Data: []byte{0x20}},
		{PTS: 99999, DTS: 99999, IsKeyframe: false, Data: []byte{0x21}},
	})

	// Third GOP: yet another keyframe.
	p.processVideoSamples([]mediaSample{
		{PTS: 133332, DTS: 133332, IsKeyframe: true, Data: []byte{0x30}},
	})

	if len(mock.videoFrames) != 5 {
		t.Fatalf("expected 5 video frames, got %d", len(mock.videoFrames))
	}

	// Group IDs: first keyframe=1, second keyframe=2, third keyframe=3.
	expectedGroupIDs := []uint32{1, 1, 2, 2, 3}
	for i, f := range mock.videoFrames {
		if f.GroupID != expectedGroupIDs[i] {
			t.Errorf("frame %d: GroupID = %d, want %d", i, f.GroupID, expectedGroupIDs[i])
		}
	}

	// All keyframes should have SPS.
	keyframeIndices := []int{0, 2, 4}
	for _, idx := range keyframeIndices {
		if mock.videoFrames[idx].SPS == nil {
			t.Errorf("frame %d (keyframe): expected SPS to be set", idx)
		}
	}

	// All delta frames should not have SPS.
	deltaIndices := []int{1, 3}
	for _, idx := range deltaIndices {
		if mock.videoFrames[idx].SPS != nil {
			t.Errorf("frame %d (delta): expected SPS=nil", idx)
		}
	}
}

func TestDASHPipelineStreamSnapshot(t *testing.T) {
	t.Parallel()

	mock := &mockBroadcaster{}
	p := newDASHPipeline("test-stream", mock, nil)

	// Push startTime back so UptimeMs is reliably > 0.
	p.startTime = p.startTime.Add(-10 * time.Millisecond)

	// Process some samples so counters are non-zero.
	p.processVideoSamples([]mediaSample{
		{PTS: 0, DTS: 0, IsKeyframe: true, Data: []byte{0x01}},
	})
	p.processAudioSamples([]mediaSample{
		{PTS: 0, Data: []byte{0x02}},
	})

	snap := p.StreamSnapshot()

	if snap.Protocol != "DASH" {
		t.Errorf("Protocol = %q, want %q", snap.Protocol, "DASH")
	}

	if snap.UptimeMs <= 0 {
		t.Errorf("UptimeMs = %d, want > 0", snap.UptimeMs)
	}

	if snap.Timestamp <= 0 {
		t.Errorf("Timestamp = %d, want > 0", snap.Timestamp)
	}

	if snap.Video.Codec != "AV1" {
		t.Errorf("Video.Codec = %q, want %q", snap.Video.Codec, "AV1")
	}

	if snap.Video.TotalFrames != 1 {
		t.Errorf("Video.TotalFrames = %d, want 1", snap.Video.TotalFrames)
	}

	if snap.ViewerCount != 0 {
		t.Errorf("ViewerCount = %d, want 0", snap.ViewerCount)
	}
}
