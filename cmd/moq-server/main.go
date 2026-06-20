// moq-server serves a single MPEG-TS file as a Prism MoQ stream over RAW QUIC
// (no WebTransport/HTTP3) — the headless, automatable counterpart to the browser
// path, for network-impairment testing. Subscribers connect with moqclient (or
// cmd/moq-sub). The browser/WebTransport server is unaffected; this is an
// additional transport for the same MoQ media.
//
//	go run ./cmd/moq-server -addr 127.0.0.1:4455 -stream file test/harness/BigBuckBunny_256x144-24fps.ts
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/zsiec/prism/certs"
	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/moqclient"
	"github.com/zsiec/prism/pipeline"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:4455", "raw-QUIC MoQ listen address")
	stream := flag.String("stream", "file", "stream key")
	loop := flag.Bool("loop", true, "restart the file when it ends (continuous source)")
	rate := flag.Float64("rate", 8, "ingest pacing in Mbps (0 = unpaced/file-speed); shapes the glass-to-glass bitrate")
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatal("usage: moq-server [-addr host:port] [-stream key] <file.ts>")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	cert, err := certs.Generate(14 * 24 * time.Hour)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	relay := distribution.NewRelay()
	// Track the current pipeline so subscribers' "stats" track (SCTE-35 / caption /
	// timecode counts) and "captions" track resolve to the live demux. feed swaps
	// it each loop iteration; the provider returns whatever is current.
	var curPipe pipelineHolder
	go feed(ctx, flag.Arg(0), *stream, relay, *loop, *rate, &curPipe)
	statsProvider := func(string) distribution.StatsProvider { return curPipe.get() }

	tlsConf := &tls.Config{Certificates: []tls.Certificate{cert.TLSCert}, NextProtos: []string{moqclient.ALPN}}
	ln, err := quic.ListenAddr(*addr, tlsConf, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	fmt.Printf("moq-server: raw-QUIC MoQ on %s, stream=%s\n", *addr, *stream)

	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			return
		}
		go func() {
			if err := distribution.RunRawQUICSession(ctx, conn, *stream, relay, statsProvider); err != nil {
				slog.Debug("session ended", "error", err)
			}
		}()
	}
}

// feed runs the file through the demux pipeline into the relay, optionally
// looping so subscribers always have media to receive. rateMbps > 0 paces the
// byte feed so the demux produces frames at a realistic streaming bitrate
// (unpaced file ingest otherwise rushes the whole clip through in a burst).
func feed(ctx context.Context, path, stream string, relay *distribution.Relay, loop bool, rateMbps float64, holder *pipelineHolder) {
	for {
		f, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		var src io.Reader = f
		if rateMbps > 0 {
			src = &pacedReader{r: f, bytesPerSec: rateMbps * 1e6 / 8}
		}
		p := pipeline.New(stream, src, relay)
		p.SetProtocol("File")
		holder.set(p)
		_ = p.Run(ctx)
		f.Close()
		if !loop || ctx.Err() != nil {
			return
		}
	}
}

// pipelineHolder publishes the current pipeline so the MoQ "stats"/"captions"
// tracks resolve to the live demux across feed's loop iterations.
type pipelineHolder struct {
	mu sync.Mutex
	p  *pipeline.Pipeline
}

func (h *pipelineHolder) set(p *pipeline.Pipeline) { h.mu.Lock(); h.p = p; h.mu.Unlock() }
func (h *pipelineHolder) get() distribution.StatsProvider {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.p == nil {
		return nil
	}
	return h.p
}

// pacedReader throttles reads to bytesPerSec so a file source feeds the pipeline
// at a steady streaming rate rather than at disk speed.
type pacedReader struct {
	r           io.Reader
	bytesPerSec float64
	start       time.Time
	read        float64
}

func (p *pacedReader) Read(b []byte) (int, error) {
	if p.start.IsZero() {
		p.start = time.Now()
	}
	n, err := p.r.Read(b)
	p.read += float64(n)
	if p.bytesPerSec > 0 {
		target := time.Duration(p.read / p.bytesPerSec * float64(time.Second))
		if d := time.Until(p.start.Add(target)); d > 0 {
			time.Sleep(d)
		}
	}
	return n, err
}
