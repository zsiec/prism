package distribution

import (
	"sync"
	"testing"
)

func TestDemuxStatsRecordVideoFrame(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	ds.RecordVideoFrame(1000, true, 90000)
	ds.RecordVideoFrame(500, false, 93000)
	ds.RecordVideoFrame(500, false, 96000)

	vs, _, _, _ := ds.Snapshot()
	if vs.TotalFrames != 3 {
		t.Fatalf("TotalFrames = %d, want 3", vs.TotalFrames)
	}
	if vs.KeyFrames != 1 {
		t.Fatalf("KeyFrames = %d, want 1", vs.KeyFrames)
	}
	if vs.DeltaFrames != 2 {
		t.Fatalf("DeltaFrames = %d, want 2", vs.DeltaFrames)
	}
	if vs.CurrentGOPLen != 3 {
		t.Fatalf("CurrentGOPLen = %d, want 3", vs.CurrentGOPLen)
	}
}

func TestDemuxStatsRecordVideoFrameGOPReset(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	ds.RecordVideoFrame(1000, true, 90000)
	ds.RecordVideoFrame(500, false, 93000)
	ds.RecordVideoFrame(500, false, 96000)
	ds.RecordVideoFrame(1000, true, 99000)

	vs, _, _, _ := ds.Snapshot()
	if vs.CurrentGOPLen != 1 {
		t.Fatalf("CurrentGOPLen = %d after new keyframe, want 1", vs.CurrentGOPLen)
	}
}

func TestDemuxStatsRecordAudioFrame(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	ds.RecordAudioFrame(0, 200, 90000, 48000, 2)
	ds.RecordAudioFrame(0, 200, 92000, 48000, 2)
	ds.RecordAudioFrame(1, 100, 90000, 44100, 1)

	_, audio, _, _ := ds.Snapshot()
	if len(audio) != 2 {
		t.Fatalf("audio tracks = %d, want 2", len(audio))
	}

	// Find track 0.
	var track0 AudioTrackStats
	for _, a := range audio {
		if a.TrackIndex == 0 {
			track0 = a
		}
	}
	if track0.Frames != 2 {
		t.Fatalf("track0 frames = %d, want 2", track0.Frames)
	}
	if track0.SampleRate != 48000 {
		t.Fatalf("track0 sample rate = %d, want 48000", track0.SampleRate)
	}
}

func TestDemuxStatsRecordCaption(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	ds.RecordCaption(1)
	ds.RecordCaption(1)
	ds.RecordCaption(2)

	_, _, cs, _ := ds.Snapshot()
	if cs.TotalFrames != 3 {
		t.Fatalf("TotalFrames = %d, want 3", cs.TotalFrames)
	}
	if len(cs.ActiveChannels) != 2 {
		t.Fatalf("ActiveChannels = %d, want 2", len(cs.ActiveChannels))
	}
}

func TestDemuxStatsRecordResolution(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()
	ds.RecordResolution(1920, 1080)

	vs, _, _, _ := ds.Snapshot()
	if vs.Width != 1920 {
		t.Fatalf("Width = %d, want 1920", vs.Width)
	}
	if vs.Height != 1080 {
		t.Fatalf("Height = %d, want 1080", vs.Height)
	}
}

func TestDemuxStatsRecordTimecode(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()
	ds.RecordTimecode("01:02:03:04")

	vs, _, _, _ := ds.Snapshot()
	if vs.Timecode != "01:02:03:04" {
		t.Fatalf("Timecode = %q, want %q", vs.Timecode, "01:02:03:04")
	}
}

func TestDemuxStatsRecordVideoCodec(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()
	ds.RecordVideoCodec("H.265")

	vs, _, _, _ := ds.Snapshot()
	if vs.Codec != "H.265" {
		t.Fatalf("Codec = %q, want %q", vs.Codec, "H.265")
	}
}

func TestDemuxStatsDefaultVideoCodec(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	vs, _, _, _ := ds.Snapshot()
	if vs.Codec != "H.264" {
		t.Fatalf("default codec = %q, want %q", vs.Codec, "H.264")
	}
}

func TestDemuxStatsPTSWrap(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	// Normal PTS progression.
	ds.RecordVideoFrame(1000, true, 8_589_900_000)
	// PTS wraps around (large negative delta).
	ds.RecordVideoFrame(500, false, 100_000)

	debug := ds.PTSDebug()
	if debug.VideoPTSWraps != 1 {
		t.Fatalf("VideoPTSWraps = %d, want 1", debug.VideoPTSWraps)
	}
	if len(debug.RecentWraps) != 1 {
		t.Fatalf("RecentWraps = %d, want 1", len(debug.RecentWraps))
	}
}

func TestDemuxStatsConcurrentAccess(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			ds.RecordVideoFrame(int64(n*100), n%5 == 0, int64(n*3000))
		}(i)
		go func(n int) {
			defer wg.Done()
			ds.RecordAudioFrame(n%3, int64(n*50), int64(n*2000), 48000, 2)
		}(i)
		go func(n int) {
			defer wg.Done()
			ds.RecordCaption(n % 4)
		}(i)
	}

	wg.Wait()

	// Snapshot should not race.
	vs, audio, cs, _ := ds.Snapshot()
	if vs.TotalFrames != 100 {
		t.Fatalf("TotalFrames = %d, want 100", vs.TotalFrames)
	}
	if len(audio) != 3 {
		t.Fatalf("audio tracks = %d, want 3", len(audio))
	}
	if cs.TotalFrames != 100 {
		t.Fatalf("caption frames = %d, want 100", cs.TotalFrames)
	}
}

func TestDemuxStatsFirstPTS(t *testing.T) {
	t.Parallel()

	ds := NewDemuxStats()

	ds.RecordVideoFrame(1000, true, 90000)
	ds.RecordVideoFrame(500, false, 93000)
	ds.RecordAudioFrame(0, 200, 80000, 48000, 2)
	ds.RecordAudioFrame(0, 200, 82000, 48000, 2)

	debug := ds.PTSDebug()
	if debug.FirstVideoPTS != 90000 {
		t.Fatalf("FirstVideoPTS = %d, want 90000", debug.FirstVideoPTS)
	}
	if debug.FirstAudioPTS != 80000 {
		t.Fatalf("FirstAudioPTS = %d, want 80000", debug.FirstAudioPTS)
	}
	if debug.LastVideoPTS != 93000 {
		t.Fatalf("LastVideoPTS = %d, want 93000", debug.LastVideoPTS)
	}
}
