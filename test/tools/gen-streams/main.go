package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zsiec/prism/test/tools/tsutil"
)

type StreamConfig struct {
	Number      int      `json:"number"`
	Key         string   `json:"key"`
	Description string   `json:"description"`
	SourceFilm  string   `json:"sourceFilm"`
	StartSec    float64  `json:"startSec"`
	DurationSec float64  `json:"durationSec"`
	AudioTracks int      `json:"audioTracks"`
	Channels    int      `json:"channels"`
	Captions    []string `json:"captions"`
	CaptionType string   `json:"captionType"`
	SCTE35      bool     `json:"scte35"`
	SCTE35Type  string   `json:"scte35Type,omitempty"`
	Timecode    bool     `json:"timecode"`
}

type Manifest struct {
	Generated string         `json:"generated"`
	Streams   []StreamConfig `json:"streams"`
}

var streams = []StreamConfig{
	{
		Number: 1, Key: "scifi_a",
		SourceFilm: "tears_of_steel.mov", StartSec: 0, DurationSec: 240,
		Captions: []string{"EN", "ES"}, CaptionType: "cea-708",
		SCTE35: true, SCTE35Type: "ad_breaks", Timecode: true,
	},
	{
		Number: 2, Key: "scifi_b",
		SourceFilm: "tears_of_steel.mov", StartSec: 240, DurationSec: 240,
		Captions: []string{"FR", "DE"}, CaptionType: "cea-708",
		SCTE35: true, SCTE35Type: "program", Timecode: true,
	},
	{
		Number: 3, Key: "scifi_c",
		SourceFilm: "tears_of_steel.mov", StartSec: 480, DurationSec: 254,
		Captions: []string{"JA", "EN"}, CaptionType: "cea-708",
		SCTE35: false, Timecode: true,
	},
	{
		Number: 4, Key: "fantasy_a",
		SourceFilm: "sintel.mkv", StartSec: 0, DurationSec: 300,
		Captions: []string{"PT"}, CaptionType: "cea-608",
		SCTE35: true, SCTE35Type: "mixed", Timecode: true,
	},
	{
		Number: 5, Key: "fantasy_b",
		SourceFilm: "sintel.mkv", StartSec: 300, DurationSec: 300,
		Captions: []string{"KO", "ZH"}, CaptionType: "cea-708",
		SCTE35: false, Timecode: true,
	},
	{
		Number: 6, Key: "fantasy_c",
		SourceFilm: "sintel.mkv", StartSec: 600, DurationSec: 288,
		Captions: []string{"AR", "EN"}, CaptionType: "cea-708",
		SCTE35: true, SCTE35Type: "chapters", Timecode: true,
	},
	{
		Number: 7, Key: "nature_a",
		SourceFilm: "bbb.mov", StartSec: 0, DurationSec: 300,
		Captions: []string{"IT"}, CaptionType: "cea-608",
		SCTE35: false, Timecode: true,
	},
	{
		Number: 8, Key: "nature_b",
		SourceFilm: "bbb.mov", StartSec: 300, DurationSec: 296,
		Captions: []string{"RU", "EN"}, CaptionType: "cea-708",
		SCTE35: true, SCTE35Type: "unscheduled", Timecode: true,
	},
	{
		Number: 9, Key: "abstract",
		SourceFilm: "elephants_dream.mov", StartSec: 0, DurationSec: 654,
		Captions: []string{"HI", "EN"}, CaptionType: "cea-708",
		SCTE35: true, SCTE35Type: "po_start_end", Timecode: true,
	},
}

func main() {
	checkDeps()

	rng := rand.New(rand.NewSource(42))

	rootDir := findProjectRoot()
	sourcesDir := filepath.Join(rootDir, "test", "sources")
	streamsDir := filepath.Join(rootDir, "test", "streams")
	toolsDir := filepath.Join(rootDir, "test", "tools")

	if err := os.MkdirAll(sourcesDir, 0755); err != nil {
		fatal("create sources dir: %v", err)
	}
	if err := os.MkdirAll(streamsDir, 0755); err != nil {
		fatal("create streams dir: %v", err)
	}

	fmt.Println("=== Prism Stream Generator ===")
	fmt.Printf("Generating %d test streams from broadcast films\n\n", len(streams))

	for i := range streams {
		streams[i].AudioTracks = randomTrackCount(rng)
		streams[i].Channels = streams[i].AudioTracks * 2
		sc := streams[i]
		features := fmt.Sprintf("%d stereo audio", sc.AudioTracks)
		if len(sc.Captions) > 0 {
			features += ", " + strings.Join(sc.Captions, "+") + " " + sc.CaptionType
		}
		if sc.SCTE35 {
			features += ", " + sc.SCTE35Type + " SCTE-35"
		}
		title := strings.ToUpper(sc.Key[:1]) + sc.Key[1:]
		dur := fmt.Sprintf("%.0fs", sc.DurationSec)
		streams[i].Description = fmt.Sprintf("%s — %s from %s [%s]", title, features, sc.SourceFilm, dur)
	}

	fmt.Println("Step 1: Downloading source films...")
	if err := downloadSources(sourcesDir); err != nil {
		fatal("source download failed: %v", err)
	}

	for i, sc := range streams {
		fmt.Printf("\n--- Stream %d: %s (%s %.0f-%.0fs, %d audio tracks) ---\n",
			sc.Number, sc.Key, sc.SourceFilm, sc.StartSec, sc.StartSec+sc.DurationSec, sc.AudioTracks)
		outFile := filepath.Join(streamsDir, fmt.Sprintf("stream_%d.ts", sc.Number))

		if tsutil.FileExists(outFile) {
			fmt.Printf("  Already exists, skipping\n")
			continue
		}

		baseFile := filepath.Join(streamsDir, fmt.Sprintf("_base_%d.ts", sc.Number))
		audioFile := filepath.Join(streamsDir, fmt.Sprintf("_audio_%d.ts", sc.Number))
		captionFile := filepath.Join(streamsDir, fmt.Sprintf("_caption_%d.ts", sc.Number))

		fmt.Printf("  Step 2: Encoding segment from %s...\n", sc.SourceFilm)
		filmSource := filepath.Join(sourcesDir, sc.SourceFilm)
		if err := encodeSegment(filmSource, baseFile, sc.StartSec, sc.DurationSec); err != nil {
			fatal("encode failed for stream %d: %v", sc.Number, err)
		}

		fmt.Printf("  Step 3: Adding %d stereo audio tracks...\n", sc.AudioTracks)
		if err := mixAudioTracks(baseFile, audioFile, sc.AudioTracks, sourcesDir, rng); err != nil {
			fatal("audio mixing failed for stream %d: %v", sc.Number, err)
		}
		os.Remove(baseFile)
		current := audioFile

		scte35File := filepath.Join(streamsDir, fmt.Sprintf("_scte35_%d.ts", sc.Number))
		if sc.SCTE35 {
			fmt.Printf("  Step 4: Injecting SCTE-35 (%s)...\n", sc.SCTE35Type)
			scte35Tool := filepath.Join(toolsDir, "inject-scte35", "main.go")
			interval := scte35Interval(sc.SCTE35Type)
			if err := runGoToolWithArgs(scte35Tool, current, scte35File, fmt.Sprintf("%.0f", interval)); err != nil {
				fmt.Printf("    Warning: SCTE-35 injection failed: %v (continuing without)\n", err)
				scte35File = current
			} else {
				os.Remove(current)
				current = scte35File
			}
		} else {
			fmt.Printf("  Step 4: No SCTE-35 for this stream\n")
		}

		fmt.Printf("  Step 5: Injecting captions (%s)...\n", sc.CaptionType)
		if err := injectCaptions(current, captionFile, sc); err != nil {
			fmt.Printf("    Warning: caption injection failed: %v (continuing without)\n", err)
			captionFile = current
		} else {
			os.Remove(current)
			current = captionFile
		}

		fmt.Printf("  Step 6: Injecting timecode...\n")
		tcTool := filepath.Join(toolsDir, "inject-timecode", "main.go")
		if err := runGoTool(tcTool, current, outFile); err != nil {
			fmt.Printf("    Warning: timecode injection failed: %v (continuing without)\n", err)
			os.Rename(current, outFile)
		} else {
			os.Remove(current)
		}

		cleanupTempFiles(streamsDir, sc.Number)
		streams[i] = sc

		info, _ := os.Stat(outFile)
		if info != nil {
			fmt.Printf("  Output: %s (%.1f MB)\n", outFile, float64(info.Size())/1024/1024)
		}
	}

	manifestFile := filepath.Join(streamsDir, "manifest.json")
	if err := writeManifest(manifestFile); err != nil {
		fatal("write manifest: %v", err)
	}

	fmt.Printf("\n=== Done! %d streams generated in %s ===\n", len(streams), streamsDir)
}

func checkDeps() {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		fatal("ffmpeg not found in PATH. Install with: brew install ffmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		fatal("ffprobe not found in PATH. Install with: brew install ffmpeg")
	}
}

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}
	for {
		if tsutil.FileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fatal("could not find project root (no go.mod found)")
		}
		dir = parent
	}
}

func scte35Interval(scteType string) float64 {
	switch scteType {
	case "ad_breaks":
		return 10
	case "program":
		return 12
	case "chapters":
		return 8
	case "mixed":
		return 7
	case "unscheduled":
		return 15
	case "po_start_end":
		return 9
	default:
		return 10
	}
}

func runGoTool(toolMain string, input, output string) error {
	cmd := exec.Command("go", "run", toolMain, input, output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runGoToolWithArgs(toolMain string, input, output, extra string) error {
	cmd := exec.Command("go", "run", toolMain, input, output, extra)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeManifest(path string) error {
	m := Manifest{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Streams:   streams,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	fmt.Printf("Manifest written to %s\n", path)
	return nil
}

func cleanupTempFiles(dir string, streamNum int) {
	for _, prefix := range []string{"_base_", "_audio_", "_caption_", "_scte35_", "_timecode_"} {
		p := filepath.Join(dir, fmt.Sprintf("%s%d.ts", prefix, streamNum))
		os.Remove(p)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
