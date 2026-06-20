// moq-sub is a headless MoQ subscriber: it connects to a raw-QUIC moq-server,
// receives the video track for a fixed duration, and prints glass-to-glass
// reception stats (frames/keyframes delivered, bytes, startup latency) as JSON.
// A network-impairment harness runs this through the relay to grade what survives.
//
//	go run ./cmd/moq-sub -addr 127.0.0.1:4455 -stream file -dur 8s
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
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *dur+15*time.Second)
	defer cancel()

	sub, err := moqclient.Dial(ctx, *addr, *stream)
	if err != nil {
		fmt.Fprintln(os.Stderr, "moq-sub: dial:", err)
		os.Exit(1)
	}

	runCtx, runCancel := context.WithTimeout(ctx, *dur)
	defer runCancel()
	go func() { _ = sub.Run(runCtx) }()
	<-runCtx.Done()

	st := sub.Stats()
	sub.Close()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(st)
}
