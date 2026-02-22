package main

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"

	"github.com/zsiec/prism/test/tools/tsutil"
)

// mixAudioTracks takes a base TS (which already has the film's native audio
// as track 0) and produces an output with N total stereo AAC tracks. Track 0
// is always the original film audio, copied through. Additional tracks are
// created by pitch-shifting or time-offsetting the original audio to simulate
// SAP/secondary language tracks.
func mixAudioTracks(inputTS, output string, numTracks int, sourcesDir string, rng *rand.Rand) error {
	if numTracks <= 1 {
		return tsutil.CopyFile(inputTS, output)
	}

	extra := numTracks - 1

	var filterParts []string
	for i := 0; i < extra; i++ {
		speed := 0.9 + rng.Float64()*0.2 // 0.9x - 1.1x pitch variation
		filterParts = append(filterParts,
			fmt.Sprintf("[0:a:0]asetrate=48000*%.4f,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=0.7[extra%d]",
				speed, i))
	}

	var args []string
	args = append(args, "-y", "-i", inputTS)
	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))

	args = append(args, "-map", "0:v:0", "-c:v", "copy")
	args = append(args, "-map", "0:a:0") // original film audio as track 0
	for i := 0; i < extra; i++ {
		args = append(args, "-map", fmt.Sprintf("[extra%d]", i))
	}

	args = append(args, "-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2")
	args = append(args, "-f", "mpegts", "-mpegts_flags", "resend_headers", output)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg audio mix: %w\n%s", err, string(out))
	}
	return nil
}

// randomTrackCount returns a random stereo track count using a weighted
// distribution that favors smaller counts but occasionally produces large ones.
func randomTrackCount(rng *rand.Rand) int {
	type bucket struct {
		count  int
		weight int
	}
	buckets := []bucket{
		{1, 20},
		{2, 25},
		{3, 15},
		{4, 12},
		{6, 8},
		{8, 8},
		{12, 5},
		{16, 7},
	}

	total := 0
	for _, b := range buckets {
		total += b.weight
	}
	r := rng.Intn(total)
	for _, b := range buckets {
		r -= b.weight
		if r < 0 {
			return b.count
		}
	}
	return 2
}
