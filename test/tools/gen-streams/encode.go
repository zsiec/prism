package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	targetBitrate  = "5000k"
	targetWidth    = 1920
	targetHeight   = 1080
	gopDurationSec = 2
)

// probeFrameRate uses ffprobe to detect the source video's native frame rate.
// Returns the fps as a float, falling back to 24 if detection fails.
func probeFrameRate(inputFilm string) float64 {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputFilm,
	)
	out, err := cmd.Output()
	if err != nil {
		return 24
	}

	raw := strings.TrimSpace(string(out))
	parts := strings.Split(raw, "/")
	if len(parts) == 2 {
		num, e1 := strconv.ParseFloat(parts[0], 64)
		den, e2 := strconv.ParseFloat(parts[1], 64)
		if e1 == nil && e2 == nil && den > 0 {
			return num / den
		}
	}
	if fps, err := strconv.ParseFloat(raw, 64); err == nil {
		return fps
	}
	return 24
}

// encodeSegment extracts a time range from a source film and re-encodes it
// to the target broadcast spec at the film's native frame rate. The film's
// first audio track is included as stereo AAC — additional tracks are mixed
// in separately by mixAudioTracks.
func encodeSegment(inputFilm, output string, startSec, durationSec float64) error {
	fps := probeFrameRate(inputFilm)
	gop := int(fps * gopDurationSec)

	fmt.Printf("    Source frame rate: %.2f fps, GOP: %d frames (%ds)\n", fps, gop, gopDurationSec)

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", startSec),
		"-i", inputFilm,
		"-t", fmt.Sprintf("%.2f", durationSec),

		"-map", "0:v:0",
		"-map", "0:a:0",

		"-c:v", "libx264",
		"-preset", "medium",
		"-b:v", targetBitrate,
		"-maxrate", targetBitrate,
		"-bufsize", "10000k",
		"-g", fmt.Sprintf("%d", gop),
		"-keyint_min", fmt.Sprintf("%d", gop),
		"-sc_threshold", "0",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
			targetWidth, targetHeight, targetWidth, targetHeight),
		"-x264-params", fmt.Sprintf("nal-hrd=cbr:vbv-bufsize=10000:vbv-maxrate=5000:pic-struct=1:fps=%d/1", int(fps)),
		"-pix_fmt", "yuv420p",

		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "48000",
		"-ac", "2",

		"-f", "mpegts",
		"-mpegts_flags", "resend_headers",
		output,
	}

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg encode: %w\n%s", err, string(out))
	}
	return nil
}
