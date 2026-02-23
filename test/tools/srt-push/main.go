package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	srt "github.com/zsiec/srtgo"

	"github.com/zsiec/prism/test/tools/tsutil"
)

type streamManifestEntry struct {
	Number int    `json:"number"`
	Key    string `json:"key"`
}

type manifest struct {
	Streams []streamManifestEntry `json:"streams"`
}

func main() {
	allFlag := flag.Bool("all", false, "Push all 9 generated streams simultaneously")
	fileFlag := flag.String("file", "", "Single TS file to push")
	keyFlag := flag.String("key", "", "Stream key (default: filename without extension)")
	addrFlag := flag.String("addr", "127.0.0.1:6000", "SRT server address")
	durationFlag := flag.Float64("duration", 0, "Known duration in seconds (skips ffprobe detection)")
	flag.Parse()

	if *allFlag {
		pushAll(*addrFlag, *durationFlag)
		return
	}

	filePath := *fileFlag
	if filePath == "" && flag.NArg() > 0 {
		filePath = flag.Arg(0)
	}
	if filePath == "" {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  srt-push --all                          Push all 9 test streams\n")
		fmt.Fprintf(os.Stderr, "  srt-push --file stream.ts --key mykey   Push a single stream\n")
		fmt.Fprintf(os.Stderr, "  srt-push <file.ts> [streamid] [host:port]  (legacy positional args)\n")
		os.Exit(1)
	}

	streamID := *keyFlag
	if streamID == "" && flag.NArg() > 1 {
		streamID = flag.Arg(1)
	}
	if streamID == "" {
		base := filepath.Base(filePath)
		streamID = "live/" + base[:len(base)-len(filepath.Ext(base))]
	}

	addr := *addrFlag
	if flag.NArg() > 2 {
		addr = flag.Arg(2)
	}

	pushSingle(filePath, streamID, addr, *durationFlag)
}

func pushAll(addr string, durationOverride float64) {
	streamsDir := findStreamsDir()
	manifestPath := filepath.Join(streamsDir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read manifest at %s: %v\n", manifestPath, err)
		fmt.Fprintf(os.Stderr, "Run 'make gen-streams' first.\n")
		os.Exit(1)
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid manifest: %v\n", err)
		os.Exit(1)
	}

	if len(m.Streams) == 0 {
		fmt.Fprintf(os.Stderr, "No streams in manifest\n")
		os.Exit(1)
	}

	fmt.Printf("Pushing %d streams to %s\n", len(m.Streams), addr)

	var wg sync.WaitGroup
	for _, s := range m.Streams {
		tsFile := filepath.Join(streamsDir, fmt.Sprintf("stream_%d.ts", s.Number))
		if _, err := os.Stat(tsFile); os.IsNotExist(err) {
			fmt.Printf("  Skipping stream %d (%s): file not found\n", s.Number, s.Key)
			continue
		}

		wg.Add(1)
		go func(file, key string, num int) {
			defer wg.Done()
			streamID := "live/" + key
			fmt.Printf("  Stream %d: %s -> %s\n", num, key, streamID)
			pushSingle(file, streamID, addr, durationOverride)
		}(tsFile, s.Key, s.Number)

		time.Sleep(200 * time.Millisecond)
	}

	wg.Wait()
}

func pushSingle(filePath, streamID, addr string, durationOverride float64) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file: %v\n", err)
		return
	}

	totalPackets := len(data) / tsutil.TSPacketSize
	if len(data)%tsutil.TSPacketSize != 0 {
		fmt.Fprintf(os.Stderr, "Warning: file size not a multiple of %d\n", tsutil.TSPacketSize)
	}

	var duration float64
	if durationOverride > 0 {
		duration = durationOverride
	} else {
		duration = findDuration(filePath)
		if duration <= 0 {
			duration = 60.0
		}
	}
	bytesPerSec := float64(len(data)) / duration
	chunkSize := tsutil.TSPacketSize * 7

	fmt.Printf("File: %s (%d packets, %.1fs, %.0f bytes/sec)\n", filePath, totalPackets, duration, bytesPerSec)

	for {
		fmt.Printf("[%s] Connecting to SRT %s...\n", streamID, addr)

		cfg := srt.DefaultConfig()
		cfg.StreamID = streamID

		conn, err := srt.Dial(addr, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] SRT connect failed: %v, retrying...\n", streamID, err)
			time.Sleep(time.Second)
			continue
		}

		fmt.Printf("[%s] Connected, streaming continuously\n", streamID)
		writeErr := streamLoop(conn, data, bytesPerSec, chunkSize, streamID)
		conn.Close()

		if writeErr != nil {
			fmt.Fprintf(os.Stderr, "[%s] Connection lost: %v, reconnecting...\n", streamID, writeErr)
			time.Sleep(time.Second)
		}
	}
}

func streamLoop(conn *srt.Conn, data []byte, bytesPerSec float64, chunkSize int, streamID string) error {
	globalStart := time.Now()
	var totalBytesSent int64
	lastLog := time.Now()
	const logInterval = 10 * time.Second

	for loop := 1; ; loop++ {
		if loop > 1 {
			fmt.Printf("[%s] Loop %d complete, restarting from offset 0 (total sent: %.1f MB, elapsed: %s)\n",
				streamID, loop-1,
				float64(totalBytesSent)/(1024*1024),
				time.Since(globalStart).Truncate(time.Second))
		}

		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}

			if _, err := conn.Write(data[i:end]); err != nil {
				return err
			}
			totalBytesSent += int64(end - i)

			// Pace against the global clock so timing is continuous across
			// loop boundaries -- no burst/gap at the seam.
			expectedTime := float64(totalBytesSent) / bytesPerSec
			elapsed := time.Since(globalStart).Seconds()
			if expectedTime > elapsed {
				time.Sleep(time.Duration((expectedTime - elapsed) * float64(time.Second)))
			}

			if time.Since(lastLog) >= logInterval {
				actualRate := float64(totalBytesSent) / time.Since(globalStart).Seconds()
				loopOffset := float64(i) / float64(len(data)) * 100
				fmt.Printf("[%s] loop=%d offset=%.1f%% rate=%.0f B/s (target=%.0f) total=%.1f MB\n",
					streamID, loop, loopOffset, actualRate, bytesPerSec,
					float64(totalBytesSent)/(1024*1024))
				lastLog = time.Now()
			}
		}
	}
}

func findStreamsDir() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}
	for {
		candidate := filepath.Join(dir, "test", "streams")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		if tsutil.FileExists(filepath.Join(dir, "go.mod")) {
			return filepath.Join(dir, "test", "streams")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join("test", "streams")
		}
		dir = parent
	}
}

func findDuration(filePath string) float64 {
	// Use ffprobe for accurate duration; the PCR-based scan can report
	// double the actual duration when mpegts_flags=resend_headers is used.
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	).Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if d, err := strconv.ParseFloat(s, 64); err == nil && d > 0 {
			return d
		}
	}
	return 0
}
