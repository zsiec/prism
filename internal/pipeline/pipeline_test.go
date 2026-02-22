package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/zsiec/prism/internal/distribution"
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
