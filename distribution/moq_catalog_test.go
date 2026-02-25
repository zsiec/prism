package distribution

import (
	"encoding/json"
	"testing"
)

func TestBuildMoQCatalogBasic(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	data, err := buildMoQCatalog("teststream", relay)
	if err != nil {
		t.Fatal(err)
	}

	var cat moqCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatal(err)
	}

	if cat.Version != 1 {
		t.Fatalf("version = %d, want 1", cat.Version)
	}
	if cat.StreamingFormat != 1 {
		t.Fatalf("streamingFormat = %d, want 1", cat.StreamingFormat)
	}
	if cat.StreamingFormatVersion != "0.2" {
		t.Fatalf("streamingFormatVersion = %q, want 0.2", cat.StreamingFormatVersion)
	}
	if cat.CommonTrackFields.Namespace != "prism/teststream" {
		t.Fatalf("namespace = %q", cat.CommonTrackFields.Namespace)
	}
	if cat.CommonTrackFields.Packaging != "loc" {
		t.Fatalf("packaging = %q", cat.CommonTrackFields.Packaging)
	}

	// Default relay: 1 audio track → video + audio0 + captions + stats = 4 tracks
	if len(cat.Tracks) != 4 {
		t.Fatalf("track count = %d, want 4", len(cat.Tracks))
	}

	// Video
	if cat.Tracks[0].Name != "video" {
		t.Fatalf("tracks[0].name = %q", cat.Tracks[0].Name)
	}
	if cat.Tracks[0].SelectionParams.Width != 1920 {
		t.Fatalf("video width = %d", cat.Tracks[0].SelectionParams.Width)
	}

	// Audio
	if cat.Tracks[1].Name != "audio0" {
		t.Fatalf("tracks[1].name = %q", cat.Tracks[1].Name)
	}
	if cat.Tracks[1].SelectionParams.Codec != "mp4a.40.02" {
		t.Fatalf("audio codec = %q", cat.Tracks[1].SelectionParams.Codec)
	}
	if cat.Tracks[1].SelectionParams.SampleRate != 48000 {
		t.Fatalf("audio sampleRate = %d", cat.Tracks[1].SelectionParams.SampleRate)
	}
	if cat.Tracks[1].SelectionParams.ChannelConfig != "2" {
		t.Fatalf("audio channelConfig = %q", cat.Tracks[1].SelectionParams.ChannelConfig)
	}

	// Captions
	if cat.Tracks[2].Name != "captions" {
		t.Fatalf("tracks[2].name = %q", cat.Tracks[2].Name)
	}
	if cat.Tracks[2].SelectionParams.Codec != "caption/v2" {
		t.Fatalf("caption codec = %q", cat.Tracks[2].SelectionParams.Codec)
	}

	// Stats
	if cat.Tracks[3].Name != "stats" {
		t.Fatalf("tracks[3].name = %q", cat.Tracks[3].Name)
	}
	if cat.Tracks[3].SelectionParams.Codec != "application/json" {
		t.Fatalf("stats codec = %q", cat.Tracks[3].SelectionParams.Codec)
	}
}

func TestBuildMoQCatalogMultiAudio(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	relay.SetAudioTrackCount(3)

	data, err := buildMoQCatalog("multi", relay)
	if err != nil {
		t.Fatal(err)
	}

	var cat moqCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatal(err)
	}

	// video + audio0 + audio1 + audio2 + captions + stats = 6 tracks
	if len(cat.Tracks) != 6 {
		t.Fatalf("track count = %d, want 6", len(cat.Tracks))
	}

	for i := 0; i < 3; i++ {
		expected := "audio" + string(rune('0'+i))
		if cat.Tracks[i+1].Name != expected {
			t.Fatalf("tracks[%d].name = %q, want %q", i+1, cat.Tracks[i+1].Name, expected)
		}
	}
}

func TestBuildMoQCatalogCustomVideoInfo(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	relay.mu.Lock()
	relay.videoInfo = VideoInfo{Codec: "avc1.640028", Width: 3840, Height: 2160}
	relay.videoInfoSet = true
	relay.mu.Unlock()

	data, err := buildMoQCatalog("4k", relay)
	if err != nil {
		t.Fatal(err)
	}

	var cat moqCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatal(err)
	}

	vp := cat.Tracks[0].SelectionParams
	if vp.Codec != "avc1.640028" {
		t.Fatalf("video codec = %q", vp.Codec)
	}
	if vp.Width != 3840 || vp.Height != 2160 {
		t.Fatalf("video resolution = %dx%d", vp.Width, vp.Height)
	}
}

func TestBuildMoQCatalogJSONFieldNames(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	data, err := buildMoQCatalog("test", relay)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// Check that JSON keys match spec exactly
	requiredKeys := []string{"version", "streamingFormat", "streamingFormatVersion", "commonTrackFields", "tracks"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing required JSON key: %q", key)
		}
	}

	ctf := raw["commonTrackFields"].(map[string]any)
	if _, ok := ctf["namespace"]; !ok {
		t.Fatal("missing commonTrackFields.namespace")
	}
	if _, ok := ctf["packaging"]; !ok {
		t.Fatal("missing commonTrackFields.packaging")
	}
}

func TestBuildMoQCatalogCustomAudioInfo(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	relay.SetAudioInfo(AudioInfo{Codec: "mp4a.40.05", SampleRate: 44100, Channels: 1})

	data, err := buildMoQCatalog("custom-audio", relay)
	if err != nil {
		t.Fatal(err)
	}

	var cat moqCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatal(err)
	}

	ap := cat.Tracks[1].SelectionParams
	if ap.Codec != "mp4a.40.05" {
		t.Fatalf("audio codec = %q", ap.Codec)
	}
	if ap.SampleRate != 44100 {
		t.Fatalf("audio sampleRate = %d", ap.SampleRate)
	}
	if ap.ChannelConfig != "1" {
		t.Fatalf("audio channelConfig = %q", ap.ChannelConfig)
	}
}
