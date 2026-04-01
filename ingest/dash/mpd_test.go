package dash

import (
	"testing"
	"time"
)

const testMPDDynamic = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"
     type="dynamic"
     availabilityStartTime="2026-03-18T00:00:00Z"
     minimumUpdatePeriod="PT2S"
     minBufferTime="PT2S">
  <Period id="1" start="PT0S">
    <AdaptationSet mimeType="video/mp4" contentType="video">
      <Representation id="1080p" bandwidth="4000000" width="1920" height="1080" codecs="av01.0.09M.08">
        <SegmentTemplate initialization="video/$RepresentationID$/init.mp4"
                         media="video/$RepresentationID$/seg-$Number$.m4s"
                         startNumber="1" timescale="90000" duration="180000"/>
      </Representation>
      <Representation id="720p" bandwidth="2000000" width="1280" height="720" codecs="av01.0.04M.08">
        <SegmentTemplate initialization="video/$RepresentationID$/init.mp4"
                         media="video/$RepresentationID$/seg-$Number$.m4s"
                         startNumber="1" timescale="90000" duration="180000"/>
      </Representation>
    </AdaptationSet>
    <AdaptationSet mimeType="audio/mp4" contentType="audio">
      <Representation id="aac-128k" bandwidth="128000" codecs="mp4a.40.2">
        <SegmentTemplate initialization="audio/$RepresentationID$/init.mp4"
                         media="audio/$RepresentationID$/seg-$Number$.m4s"
                         startNumber="1" timescale="48000" duration="96000"/>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

const testMPDStatic = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"
     type="static"
     mediaPresentationDuration="PT60S"
     minBufferTime="PT2S">
  <Period id="1">
    <AdaptationSet mimeType="video/mp4">
      <Representation id="v1" bandwidth="1000000" width="640" height="360" codecs="av01.0.01M.08">
        <SegmentTemplate initialization="init-$RepresentationID$.mp4"
                         media="seg-$RepresentationID$-$Number$.m4s"
                         startNumber="1" timescale="90000" duration="180000"/>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

func TestParseMPD(t *testing.T) {
	info, err := parseMPD([]byte(testMPDDynamic))
	if err != nil {
		t.Fatalf("parseMPD: %v", err)
	}

	if !info.IsDynamic {
		t.Error("expected IsDynamic=true")
	}

	expectedAST := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	if !info.AvailabilityStartTime.Equal(expectedAST) {
		t.Errorf("AvailabilityStartTime = %v, want %v", info.AvailabilityStartTime, expectedAST)
	}

	if info.MinUpdatePeriod != 2*time.Second {
		t.Errorf("MinUpdatePeriod = %v, want 2s", info.MinUpdatePeriod)
	}

	// Video adaptations
	if len(info.VideoAdaptations) != 1 {
		t.Fatalf("video adaptation count = %d, want 1", len(info.VideoAdaptations))
	}
	va := info.VideoAdaptations[0]
	if len(va.Representations) != 2 {
		t.Fatalf("video rep count = %d, want 2", len(va.Representations))
	}
	if va.Representations[0].ID != "1080p" {
		t.Errorf("video rep[0] ID = %q, want 1080p", va.Representations[0].ID)
	}
	if va.Representations[1].ID != "720p" {
		t.Errorf("video rep[1] ID = %q, want 720p", va.Representations[1].ID)
	}
	if va.Representations[0].Width != 1920 || va.Representations[0].Height != 1080 {
		t.Errorf("video rep[0] dims = %dx%d, want 1920x1080",
			va.Representations[0].Width, va.Representations[0].Height)
	}
	if va.Representations[0].Codecs != "av01.0.09M.08" {
		t.Errorf("video rep[0] codecs = %q, want av01.0.09M.08", va.Representations[0].Codecs)
	}

	// Audio adaptations
	if len(info.AudioAdaptations) != 1 {
		t.Fatalf("audio adaptation count = %d, want 1", len(info.AudioAdaptations))
	}
	aa := info.AudioAdaptations[0]
	if len(aa.Representations) != 1 {
		t.Fatalf("audio rep count = %d, want 1", len(aa.Representations))
	}
	if aa.Representations[0].ID != "aac-128k" {
		t.Errorf("audio rep[0] ID = %q, want aac-128k", aa.Representations[0].ID)
	}

	// Segment template on video rep
	st := va.Representations[0].SegmentTemplate
	if st.Timescale != 90000 {
		t.Errorf("timescale = %d, want 90000", st.Timescale)
	}
	if st.Duration != 180000 {
		t.Errorf("duration = %d, want 180000", st.Duration)
	}
	if st.StartNumber != 1 {
		t.Errorf("startNumber = %d, want 1", st.StartNumber)
	}
}

func TestSelectRepresentation(t *testing.T) {
	info, err := parseMPD([]byte(testMPDDynamic))
	if err != nil {
		t.Fatalf("parseMPD: %v", err)
	}

	t.Run("explicit ID", func(t *testing.T) {
		rep, tmpl, err := selectRepresentation(info.VideoAdaptations, "720p")
		if err != nil {
			t.Fatalf("selectRepresentation: %v", err)
		}
		if rep.ID != "720p" {
			t.Errorf("ID = %q, want 720p", rep.ID)
		}
		if rep.Bandwidth != 2000000 {
			t.Errorf("Bandwidth = %d, want 2000000", rep.Bandwidth)
		}
		if tmpl.Timescale != 90000 {
			t.Errorf("Timescale = %d, want 90000", tmpl.Timescale)
		}
	})

	t.Run("empty ID selects highest bandwidth", func(t *testing.T) {
		rep, _, err := selectRepresentation(info.VideoAdaptations, "")
		if err != nil {
			t.Fatalf("selectRepresentation: %v", err)
		}
		if rep.ID != "1080p" {
			t.Errorf("ID = %q, want 1080p", rep.ID)
		}
		if rep.Bandwidth != 4000000 {
			t.Errorf("Bandwidth = %d, want 4000000", rep.Bandwidth)
		}
	})

	t.Run("nonexistent ID", func(t *testing.T) {
		_, _, err := selectRepresentation(info.VideoAdaptations, "4k")
		if err == nil {
			t.Error("expected error for nonexistent ID")
		}
	})
}

func TestResolveInitURL(t *testing.T) {
	tmpl := segmentTemplate{
		InitializationPattern: "video/$RepresentationID$/init.mp4",
		MediaPattern:          "video/$RepresentationID$/seg-$Number$.m4s",
		StartNumber:           1,
		Timescale:             90000,
		Duration:              180000,
	}

	got := resolveInitURL(tmpl, "1080p", "")
	want := "video/1080p/init.mp4"
	if got != want {
		t.Errorf("resolveInitURL = %q, want %q", got, want)
	}

	got = resolveInitURL(tmpl, "720p", "https://cdn.example.com/")
	want = "https://cdn.example.com/video/720p/init.mp4"
	if got != want {
		t.Errorf("resolveInitURL with baseURL = %q, want %q", got, want)
	}
}

func TestResolveMediaURL(t *testing.T) {
	tmpl := segmentTemplate{
		InitializationPattern: "video/$RepresentationID$/init.mp4",
		MediaPattern:          "video/$RepresentationID$/seg-$Number$.m4s",
		StartNumber:           1,
		Timescale:             90000,
		Duration:              180000,
	}

	got := resolveMediaURL(tmpl, "1080p", 42, "")
	want := "video/1080p/seg-42.m4s"
	if got != want {
		t.Errorf("resolveMediaURL = %q, want %q", got, want)
	}

	got = resolveMediaURL(tmpl, "720p", 1, "https://cdn.example.com/")
	want = "https://cdn.example.com/video/720p/seg-1.m4s"
	if got != want {
		t.Errorf("resolveMediaURL with baseURL = %q, want %q", got, want)
	}
}

func TestComputeSegmentNumber(t *testing.T) {
	ast := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	tmpl := segmentTemplate{
		StartNumber: 1,
		Timescale:   90000,
		Duration:    180000, // 2 seconds per segment
	}

	tests := []struct {
		name   string
		offset time.Duration
		want   int
	}{
		{"before first segment", 1 * time.Second, 1},
		{"exactly one segment", 2 * time.Second, 1},
		{"two segments elapsed", 4 * time.Second, 1 + 1},
		{"ten segments elapsed", 20 * time.Second, 1 + 9},
		{"one hour", 1 * time.Hour, 1 + 1799},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := ast.Add(tt.offset)
			got := computeSegmentNumber(now, ast, tmpl)
			if got != tt.want {
				t.Errorf("computeSegmentNumber(offset=%v) = %d, want %d", tt.offset, got, tt.want)
			}
		})
	}
}

func TestParseMPDStatic(t *testing.T) {
	info, err := parseMPD([]byte(testMPDStatic))
	if err != nil {
		t.Fatalf("parseMPD: %v", err)
	}
	if info.IsDynamic {
		t.Error("expected IsDynamic=false for static MPD")
	}
	if len(info.VideoAdaptations) != 1 {
		t.Errorf("video adaptation count = %d, want 1", len(info.VideoAdaptations))
	}
}

func TestParseMPDInvalid(t *testing.T) {
	_, err := parseMPD([]byte("this is not xml at all {{{"))
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestResolveMediaURLZeroPadded(t *testing.T) {
	t.Parallel()
	// ffmpeg uses $Number%05d$ for zero-padded segment numbers
	tmpl := segmentTemplate{
		MediaPattern: "chunk-stream$RepresentationID$-$Number%05d$.m4s",
	}
	got := resolveMediaURL(tmpl, "0", 3, "http://localhost:8080/")
	want := "http://localhost:8080/chunk-stream0-00003.m4s"
	if got != want {
		t.Errorf("resolveMediaURL zero-padded = %q, want %q", got, want)
	}
}

// Test MPD with contentType on AdaptationSet and mimeType on Representation
// (ffmpeg pattern)
const testMPDContentType = `<?xml version="1.0" encoding="utf-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"
     type="dynamic"
     availabilityStartTime="2026-03-18T00:00:00Z"
     minimumUpdatePeriod="PT2S"
     minBufferTime="PT2S">
  <Period id="0" start="PT0.0S">
    <AdaptationSet id="0" contentType="video" segmentAlignment="true">
      <Representation id="0" mimeType="video/mp4" codecs="av01.0.05M.08" bandwidth="3000000" width="1280" height="720">
        <SegmentTemplate timescale="1000000" duration="2000000"
                         initialization="init-stream$RepresentationID$.m4s"
                         media="chunk-stream$RepresentationID$-$Number%05d$.m4s" startNumber="1"/>
      </Representation>
    </AdaptationSet>
    <AdaptationSet id="1" contentType="audio" segmentAlignment="true">
      <Representation id="1" mimeType="audio/mp4" codecs="mp4a.40.2" bandwidth="128000" audioSamplingRate="48000">
        <SegmentTemplate timescale="1000000" duration="2000000"
                         initialization="init-stream$RepresentationID$.m4s"
                         media="chunk-stream$RepresentationID$-$Number%05d$.m4s" startNumber="1"/>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

func TestParseMPDContentTypeAttr(t *testing.T) {
	t.Parallel()
	info, err := parseMPD([]byte(testMPDContentType))
	if err != nil {
		t.Fatalf("parseMPD: %v", err)
	}
	if !info.IsDynamic {
		t.Error("expected dynamic MPD")
	}
	if len(info.VideoAdaptations) != 1 {
		t.Fatalf("video adaptations = %d, want 1", len(info.VideoAdaptations))
	}
	if len(info.AudioAdaptations) != 1 {
		t.Fatalf("audio adaptations = %d, want 1", len(info.AudioAdaptations))
	}
	videoRep := info.VideoAdaptations[0].Representations[0]
	if videoRep.Codecs != "av01.0.05M.08" {
		t.Errorf("video codecs = %q, want av01.0.05M.08", videoRep.Codecs)
	}
}
