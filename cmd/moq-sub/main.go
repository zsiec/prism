// moq-sub is a headless MoQ subscriber: it connects to a raw-QUIC moq-server,
// receives the video (+audio) track for a fixed duration, and prints glass-to-glass
// reception stats (frames/keyframes delivered, A/V skew, bytes, startup latency) as
// JSON. A network-impairment harness runs this through the relay to grade what
// survives.
//
//	go run ./cmd/moq-sub -addr 127.0.0.1:4455 -stream file -dur 8s
//
// With -interval >0 it streams a one-line JSON Stats snapshot every interval until
// -dur elapses (the Live Control console reads this as a live feed); the cumulative
// counters let the reader window them into rates.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/zsiec/prism/moqclient"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:4455", "raw-QUIC moq-server address")
	stream := flag.String("stream", "file", "stream key")
	dur := flag.Duration("dur", 8*time.Second, "how long to receive before reporting")
	interval := flag.Duration("interval", 0, "if >0, emit a one-line JSON Stats snapshot every interval (live feed)")
	dump := flag.String("dump", "", "if set, write the received video as a decodable Annex-B .h264 (the glass-to-glass 'what survived' stream)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *dur+15*time.Second)
	defer cancel()

	sub, err := moqclient.Dial(ctx, *addr, *stream)
	if err != nil {
		fmt.Fprintln(os.Stderr, "moq-sub: dial:", err)
		os.Exit(1)
	}
	if *dump != "" {
		sub.EnableVideoDump()
	}

	runCtx, runCancel := context.WithTimeout(ctx, *dur)
	defer runCancel()
	go func() { _ = sub.Run(runCtx) }()

	if *interval > 0 {
		// Live mode: stream compact one-line snapshots until the run window ends.
		enc := json.NewEncoder(os.Stdout)
		tick := time.NewTicker(*interval)
		defer tick.Stop()
		for {
			select {
			case <-runCtx.Done():
				_ = enc.Encode(sub.Stats())
				sub.Close()
				return
			case <-tick.C:
				_ = enc.Encode(sub.Stats())
			}
		}
	}

	<-runCtx.Done()
	st := sub.Stats()
	if *dump != "" {
		if f, err := os.Create(*dump); err == nil {
			if derr := sub.DumpVideo(f); derr != nil {
				fmt.Fprintln(os.Stderr, "moq-sub: dump:", derr)
			}
			_ = f.Close()
		} else {
			fmt.Fprintln(os.Stderr, "moq-sub: dump:", err)
		}
	}
	sub.Close()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(st)
}
