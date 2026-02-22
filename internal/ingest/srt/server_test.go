package srt

import "testing"

func TestExtractStreamKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		streamID string
		want     string
	}{
		{name: "simple key", streamID: "camera1", want: "camera1"},
		{name: "leading slash", streamID: "/camera1", want: "camera1"},
		{name: "live prefix", streamID: "live/camera1", want: "camera1"},
		{name: "slash and live prefix", streamID: "/live/camera1", want: "camera1"},
		{name: "empty returns default", streamID: "", want: "default"},
		{name: "just slash returns default", streamID: "/", want: "default"},
		{name: "just live/ returns default", streamID: "live/", want: "default"},
		{name: "nested path preserved", streamID: "studio/camera1", want: "studio/camera1"},
		{name: "live in name preserved", streamID: "liveshow", want: "liveshow"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractStreamKey(tc.streamID)
			if got != tc.want {
				t.Errorf("extractStreamKey(%q) = %q, want %q", tc.streamID, got, tc.want)
			}
		})
	}
}
