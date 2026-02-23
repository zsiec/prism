package main

import (
	"bufio"
	"fmt"
	"io"
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
func encodeSegment(inputFilm, output string, startSec, durationSec float64, prefix string) error {
	fps := probeFrameRate(inputFilm)
	gop := int(fps * gopDurationSec)

	fmt.Printf("%s %.2f fps, GOP %d frames\n", prefix, fps, gop)

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", startSec),
		"-i", inputFilm,
		"-t", fmt.Sprintf("%.2f", durationSec),

		"-map", "0:v:0",
		"-map", "0:a:0",

		"-c:v", "libx264",
		"-preset", "veryfast",
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

	return runFFmpegWithProgress(args, durationSec, prefix)
}

// runFFmpegWithProgress runs ffmpeg, parsing stderr for time= progress and
// printing a percentage line per stream. On error the last 5 lines of stderr
// are included in the returned error for diagnostics.
func runFFmpegWithProgress(args []string, totalDuration float64, prefix string) error {
	cmd := exec.Command("ffmpeg", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	tailLines := scanProgress(stderr, totalDuration, prefix)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, strings.Join(tailLines, "\n"))
	}
	fmt.Printf("%s %s\n", prefix, progressBar(100))
	return nil
}

// scanProgress reads ffmpeg stderr, prints progress percentages, and returns
// the last 5 lines for error reporting. ffmpeg uses \r to overwrite its
// progress line in-place, so we split on both \r and \n.
func scanProgress(r io.Reader, totalDuration float64, prefix string) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	scanner.Split(splitCRLF)

	var tailLines []string
	const tailSize = 5
	lastPct := -1

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Keep a rolling tail for error diagnostics.
		tailLines = append(tailLines, line)
		if len(tailLines) > tailSize {
			tailLines = tailLines[1:]
		}

		if totalDuration <= 0 {
			continue
		}

		// ffmpeg stderr lines contain "time=HH:MM:SS.mm" or "time=N/A".
		idx := strings.Index(line, "time=")
		if idx < 0 {
			continue
		}
		timeStr := line[idx+5:]
		if sp := strings.IndexByte(timeStr, ' '); sp > 0 {
			timeStr = timeStr[:sp]
		}
		secs := parseFFmpegTime(timeStr)
		if secs <= 0 {
			continue
		}

		pct := int(secs / totalDuration * 100)
		if pct > 99 {
			continue // let the caller print the final 100% bar
		}
		if pct >= lastPct+10 {
			fmt.Printf("%s %s\n", prefix, progressBar(pct))
			lastPct = pct
		}
	}

	return tailLines
}

// splitCRLF is a bufio.SplitFunc that splits on \n, \r\n, or bare \r.
// This handles ffmpeg's \r-based progress overwriting.
func splitCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
		if b == '\r' {
			// \r\n counts as one line break
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// progressBar returns a 10-character arrow bar for the given percentage.
//
//	▸▸▸▸▸▸▸··· 70%
func progressBar(pct int) string {
	filled := pct / 10
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("▸", filled) + strings.Repeat("·", 10-filled) + fmt.Sprintf(" %3d%%", pct)
}

// parseFFmpegTime parses "HH:MM:SS.ss" into seconds.
func parseFFmpegTime(s string) float64 {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	h, e1 := strconv.ParseFloat(parts[0], 64)
	m, e2 := strconv.ParseFloat(parts[1], 64)
	sec, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0
	}
	return h*3600 + m*60 + sec
}
