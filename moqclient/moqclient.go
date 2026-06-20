// Package moqclient is a headless Go subscriber for Prism's MoQ media, over raw
// QUIC (the transport RunRawQUICSession serves). It performs the draft-15
// CLIENT_SETUP / SUBSCRIBE handshake, receives the video track's objects on
// unidirectional QUIC streams, and reports glass-to-glass reception stats —
// frames and keyframes delivered, bytes, startup latency. It is the automatable
// viewer the browser-only player could not be, so a network-impairment harness
// can grade what survives QUIC loss recovery without a browser.
package moqclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/zsiec/prism/moq"
)

// ALPN is the QUIC application protocol the raw-QUIC MoQ server and this client
// negotiate.
const ALPN = "prism-moq-quic"

// moqStreamTypeSubgroup is the MoQ data stream type Prism's writer uses
// (subgroup with explicit subgroup id + per-object extensions).
const moqStreamTypeSubgroup = 0x0d

// locVideoFrameMarking is the LOC extension id (even -> varint value) carrying
// the RFC 9626 frame-marking flags; bit 0x20 (I) marks a keyframe.
const locVideoFrameMarking = 4
const vfmKeyframeBit = 0x20

// Stats is a glass-to-glass reception snapshot.
type Stats struct {
	VideoFrames     int64   `json:"videoFrames"`
	KeyFrames       int64   `json:"keyFrames"`
	Groups          int64   `json:"groups"` // distinct GOP streams received
	Bytes           int64   `json:"bytes"`
	FirstFrameMs    float64 `json:"firstFrameMs"`
	FirstKeyframeMs float64 `json:"firstKeyframeMs"`
	ElapsedSec      float64 `json:"elapsedSec"`
}

// Subscriber holds a live raw-QUIC MoQ subscription to one stream's video track.
type Subscriber struct {
	conn          quic.Connection
	control       quic.Stream
	controlReader *bufio.Reader
	videoAlias    uint64
	start         time.Time

	videoFrames, keyFrames, groups, bytes atomic.Int64
	firstFrameNs, firstKeyframeNs         atomic.Int64
}

// Dial connects to a raw-QUIC MoQ server at addr, performs the MoQ setup, and
// subscribes to the video track of streamKey. TLS verification is skipped (this
// is a test harness against a self-signed server).
func Dial(ctx context.Context, addr, streamKey string) (*Subscriber, error) {
	tlsConf := &tls.Config{InsecureSkipVerify: true, NextProtos: []string{ALPN}} //nolint:gosec // harness only
	conn, err := quic.DialAddr(ctx, addr, tlsConf, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	control, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("open control: %w", err)
	}
	cr := bufio.NewReader(control)

	// CLIENT_SETUP -> wait for SERVER_SETUP.
	cs := moq.ClientSetup{Versions: []uint64{moq.Version}, Path: streamKey, HasPath: true, MaxRequestID: 100}
	if err := moq.WriteControlMsg(control, moq.MsgClientSetup, moq.SerializeClientSetup(cs)); err != nil {
		return nil, fmt.Errorf("write CLIENT_SETUP: %w", err)
	}
	for {
		mt, payload, err := moq.ReadControlMsg(cr)
		if err != nil {
			return nil, fmt.Errorf("read SERVER_SETUP: %w", err)
		}
		if mt == moq.MsgServerSetup {
			ss, err := moq.ParseServerSetup(payload)
			if err != nil {
				return nil, fmt.Errorf("parse SERVER_SETUP: %w", err)
			}
			if ss.SelectedVersion != moq.Version {
				return nil, fmt.Errorf("server selected version 0x%x, want 0x%x", ss.SelectedVersion, moq.Version)
			}
			break
		}
		// ignore MAX_REQUEST_ID and anything else before SERVER_SETUP
	}

	s := &Subscriber{conn: conn, control: control, controlReader: cr}

	// SUBSCRIBE video -> wait for SUBSCRIBE_OK (track alias).
	sub := moq.Subscribe{
		RequestID: 0, Namespace: []string{"prism", streamKey}, TrackName: "video",
		Priority: 0, GroupOrder: moq.GroupOrderDescending, Forward: 1, FilterType: moq.FilterLatestObject,
	}
	if err := moq.WriteControlMsg(control, moq.MsgSubscribe, moq.SerializeSubscribe(sub)); err != nil {
		return nil, fmt.Errorf("write SUBSCRIBE: %w", err)
	}
	for {
		mt, payload, err := moq.ReadControlMsg(cr)
		if err != nil {
			return nil, fmt.Errorf("read SUBSCRIBE_OK: %w", err)
		}
		switch mt {
		case moq.MsgSubscribeOK:
			sok, err := moq.ParseSubscribeOK(payload)
			if err != nil {
				return nil, fmt.Errorf("parse SUBSCRIBE_OK: %w", err)
			}
			s.videoAlias = sok.TrackAlias
			return s, nil
		case moq.MsgSubscribeError:
			return nil, fmt.Errorf("server refused video subscribe")
		}
	}
}

// Run accepts the video track's data streams and counts reception until ctx is
// cancelled or the connection ends. Each GOP arrives on its own unidirectional
// stream (a new one per keyframe).
func (s *Subscriber) Run(ctx context.Context) error {
	s.start = time.Now()
	go s.drainControl()
	for {
		uni, err := s.conn.AcceptUniStream(ctx)
		if err != nil {
			return err
		}
		go s.readStream(uni)
	}
}

func (s *Subscriber) drainControl() {
	for {
		if _, _, err := moq.ReadControlMsg(s.controlReader); err != nil {
			return
		}
	}
}

// readStream parses one MoQ subgroup data stream: a subgroup header followed by
// LOC objects (object id, extensions, payload), tallying video frames/keyframes.
func (s *Subscriber) readStream(uni quic.ReceiveStream) {
	br := bufio.NewReader(uni)
	streamType, err := quicvarint.Read(br)
	if err != nil || streamType != moqStreamTypeSubgroup {
		return
	}
	trackAlias, err := quicvarint.Read(br)
	if err != nil {
		return
	}
	if _, err := quicvarint.Read(br); err != nil { // group id
		return
	}
	if _, err := quicvarint.Read(br); err != nil { // subgroup id
		return
	}
	if _, err := br.ReadByte(); err != nil { // publisher priority
		return
	}
	if trackAlias != s.videoAlias {
		return // only video was subscribed; ignore anything else
	}
	s.groups.Add(1)

	for {
		if _, err := quicvarint.Read(br); err != nil { // object id (EOF ends the stream)
			return
		}
		extLen, err := quicvarint.Read(br)
		if err != nil {
			return
		}
		exts := make([]byte, extLen)
		if extLen > 0 {
			if _, err := io.ReadFull(br, exts); err != nil {
				return
			}
		}
		payLen, err := quicvarint.Read(br)
		if err != nil {
			return
		}
		if payLen > 0 {
			if _, err := br.Discard(int(payLen)); err != nil {
				return
			}
		}
		now := time.Since(s.start).Nanoseconds()
		s.videoFrames.Add(1)
		s.bytes.Add(int64(payLen))
		s.firstFrameNs.CompareAndSwap(0, now)
		if keyframe(exts) {
			s.keyFrames.Add(1)
			s.firstKeyframeNs.CompareAndSwap(0, now)
		}
	}
}

// keyframe reports whether the LOC extensions carry the RFC 9626 keyframe (I)
// bit in the Video Frame Marking extension.
func keyframe(exts []byte) bool {
	r := bytes.NewReader(exts)
	for r.Len() > 0 {
		id, err := quicvarint.Read(r)
		if err != nil {
			return false
		}
		if id%2 == 1 { // odd -> length-prefixed bytes
			n, err := quicvarint.Read(r)
			if err != nil {
				return false
			}
			if _, err := r.Seek(int64(n), io.SeekCurrent); err != nil {
				return false
			}
			continue
		}
		v, err := quicvarint.Read(r) // even -> varint value
		if err != nil {
			return false
		}
		if id == locVideoFrameMarking && v&vfmKeyframeBit != 0 {
			return true
		}
	}
	return false
}

// Stats returns the reception snapshot so far.
func (s *Subscriber) Stats() Stats {
	ns := func(v int64) float64 { return float64(v) / 1e6 }
	return Stats{
		VideoFrames:     s.videoFrames.Load(),
		KeyFrames:       s.keyFrames.Load(),
		Groups:          s.groups.Load(),
		Bytes:           s.bytes.Load(),
		FirstFrameMs:    ns(s.firstFrameNs.Load()),
		FirstKeyframeMs: ns(s.firstKeyframeNs.Load()),
		ElapsedSec:      time.Since(s.start).Seconds(),
	}
}

// Close tears down the QUIC connection.
func (s *Subscriber) Close() {
	_ = s.conn.CloseWithError(0, "bye")
}
