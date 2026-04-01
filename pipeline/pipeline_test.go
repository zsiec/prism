package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/media"
)

func TestNew(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)
	if p == nil {
		t.Fatal("expected non-nil Pipeline")
	}
}

func TestStreamSnapshotBeforeRun(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)

	// Should not panic before Run
	snap := p.StreamSnapshot()
	if snap.ViewerCount != 0 {
		t.Errorf("ViewerCount: got %d, want 0", snap.ViewerCount)
	}
}

func TestRunWithEOFReader(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)

	p.SetProtocol("test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run with empty reader should return without error (EOF)
	if err := p.Run(ctx); err != nil {
		t.Errorf("Run with EOF reader: %v", err)
	}
}

func TestPipelineDebug(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)

	debug := p.PipelineDebug()
	if debug.VideoForwarded != 0 {
		t.Errorf("VideoForwarded: got %d, want 0", debug.VideoForwarded)
	}
}

func TestDemuxStats(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)

	ds := p.DemuxStats()
	if ds == nil {
		t.Fatal("expected non-nil DemuxStats")
	}
}

func TestBuildVideoInfoAV1(t *testing.T) {
	t.Parallel()

	relay := distribution.NewRelay()
	p := New("test-stream", strings.NewReader(""), relay)

	// AV1 sequence header OBU for 1920x1080, profile 0, level 8, 8-bit
	// (same test data as demux/av1_test.go testSeqHdrOBU1080p)
	seqHdrOBU := []byte{
		0x0a, 0x0b,
		0x00, 0x00, 0x00, 0x42, 0xab, 0xbf, 0xc3, 0x71,
		0x2b, 0xe4, 0x01,
	}

	frame := &media.VideoFrame{
		PTS:        0,
		IsKeyframe: true,
		Codec:      "av1",
		SPS:        seqHdrOBU,
		WireData:   []byte{0x32, 0x10, 0xAA, 0xBB},
	}

	vi, ok := p.buildVideoInfo(frame)
	if !ok {
		t.Fatal("buildVideoInfo returned false")
	}

	if vi.Width != 1920 {
		t.Errorf("Width: got %d, want 1920", vi.Width)
	}
	if vi.Height != 1080 {
		t.Errorf("Height: got %d, want 1080", vi.Height)
	}

	wantPrefix := "av01."
	if len(vi.Codec) < len(wantPrefix) || vi.Codec[:len(wantPrefix)] != wantPrefix {
		t.Errorf("Codec: got %q, want prefix %q", vi.Codec, wantPrefix)
	}

	if vi.DecoderConfig == nil {
		t.Fatal("DecoderConfig is nil")
	}
	// AV1CodecConfigurationRecord starts with marker|version = 0x81
	if vi.DecoderConfig[0] != 0x81 {
		t.Errorf("DecoderConfig[0]: got 0x%02x, want 0x81", vi.DecoderConfig[0])
	}
}
