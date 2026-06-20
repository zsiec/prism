// Package moqclient is a headless Go subscriber for Prism's MoQ media, over raw
// QUIC (the transport RunRawQUICSession serves). It performs the draft-15
// CLIENT_SETUP / SUBSCRIBE handshake, receives the video AND audio tracks'
// objects on unidirectional QUIC streams, and reports glass-to-glass reception
// stats — frames/keyframes delivered, audio delivered, A/V sync skew, bytes,
// startup latency. It is the automatable viewer the browser-only player could
// not be, so a network-impairment harness can grade what survives QUIC loss
// recovery (and whether the picture stays in sync) without a browser.
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

const (
	moqStreamTypeSubgroup = 0x0d // MoQ subgroup data stream (Prism's writer)
	locCaptureTimestamp   = 2    // LOC ext (even): varint microseconds (PTS)
	locVideoFrameMarking  = 4    // LOC ext (even): RFC 9626 flags; bit 0x20 = keyframe
	vfmKeyframeBit        = 0x20

	noAlias = ^uint64(0) // sentinel: track not subscribed / refused
)

// Stats is a glass-to-glass reception snapshot.
type Stats struct {
	VideoFrames     int64   `json:"videoFrames"`
	KeyFrames       int64   `json:"keyFrames"`
	Groups          int64   `json:"groups"` // distinct GOP streams received
	VideoBytes      int64   `json:"videoBytes"`
	AudioFrames     int64   `json:"audioFrames"`
	AudioBytes      int64   `json:"audioBytes"`
	Bytes           int64   `json:"bytes"` // video + audio
	FirstFrameMs    float64 `json:"firstFrameMs"`
	FirstKeyframeMs float64 `json:"firstKeyframeMs"`
	// AVSkewMs is (latest video PTS - latest audio PTS) in ms: how far video and
	// audio playback positions have drifted apart. ~0 when both tracks are
	// delivered in step; grows when one track stalls under impairment. Valid only
	// when both tracks delivered (AVValid).
	AVSkewMs   float64 `json:"avSkewMs"`
	AVValid    bool    `json:"avValid"`
	ElapsedSec float64 `json:"elapsedSec"`
}

// Subscriber holds a live raw-QUIC MoQ subscription to one stream's video +
// audio tracks.
type Subscriber struct {
	conn          quic.Connection
	control       quic.Stream
	controlReader *bufio.Reader
	videoAlias    uint64
	audioAlias    uint64
	start         time.Time

	videoFrames, keyFrames, groups, videoBytes atomic.Int64
	audioFrames, audioBytes                    atomic.Int64
	firstFrameNs, firstKeyframeNs              atomic.Int64
	lastVideoUs, lastAudioUs                   atomic.Int64
}

// Dial connects to a raw-QUIC MoQ server at addr, performs the MoQ setup, and
// subscribes to the video and audio0 tracks of streamKey. A missing audio track
// is tolerated (the audio subscribe may be refused). TLS verification is skipped
// (test harness against a self-signed server).
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
	}

	s := &Subscriber{conn: conn, control: control, controlReader: cr, videoAlias: noAlias, audioAlias: noAlias}

	// Subscribe video (request 0) + audio0 (request 1).
	for _, t := range []struct {
		rid  uint64
		name string
	}{{0, "video"}, {1, "audio0"}} {
		sub := moq.Subscribe{
			RequestID: t.rid, Namespace: []string{"prism", streamKey}, TrackName: t.name,
			Priority: 0, GroupOrder: moq.GroupOrderDescending, Forward: 1, FilterType: moq.FilterLatestObject,
		}
		if err := moq.WriteControlMsg(control, moq.MsgSubscribe, moq.SerializeSubscribe(sub)); err != nil {
			return nil, fmt.Errorf("write SUBSCRIBE %s: %w", t.name, err)
		}
	}
	// Collect a response (OK or ERROR) for each of the two subscribes.
	for got := 0; got < 2; {
		mt, payload, err := moq.ReadControlMsg(cr)
		if err != nil {
			return nil, fmt.Errorf("read SUBSCRIBE response: %w", err)
		}
		switch mt {
		case moq.MsgSubscribeOK:
			sok, err := moq.ParseSubscribeOK(payload)
			if err != nil {
				return nil, fmt.Errorf("parse SUBSCRIBE_OK: %w", err)
			}
			switch sok.RequestID {
			case 0:
				s.videoAlias = sok.TrackAlias
			case 1:
				s.audioAlias = sok.TrackAlias
			}
			got++
		case moq.MsgSubscribeError:
			got++ // a track was refused (e.g. no audio); its alias stays the sentinel
		}
	}
	if s.videoAlias == noAlias {
		return nil, fmt.Errorf("server refused video subscribe")
	}
	return s, nil
}

// Run accepts the subscribed tracks' data streams and counts reception until ctx
// is cancelled or the connection ends.
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

// readStream parses one MoQ subgroup data stream and tallies the track it
// belongs to (video or audio), tracking keyframes and capture timestamps.
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
	for i := 0; i < 2; i++ { // group id, subgroup id
		if _, err := quicvarint.Read(br); err != nil {
			return
		}
	}
	if _, err := br.ReadByte(); err != nil { // publisher priority
		return
	}

	isVideo := trackAlias == s.videoAlias
	isAudio := trackAlias == s.audioAlias
	if !isVideo && !isAudio {
		return
	}
	if isVideo {
		s.groups.Add(1)
	}

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
		info := parseExts(exts)
		now := time.Since(s.start).Nanoseconds()
		if isVideo {
			s.videoFrames.Add(1)
			s.videoBytes.Add(int64(payLen))
			s.firstFrameNs.CompareAndSwap(0, now)
			if info.hasCapture {
				s.lastVideoUs.Store(int64(info.captureUs))
			}
			if info.keyframe {
				s.keyFrames.Add(1)
				s.firstKeyframeNs.CompareAndSwap(0, now)
			}
		} else {
			s.audioFrames.Add(1)
			s.audioBytes.Add(int64(payLen))
			if info.hasCapture {
				s.lastAudioUs.Store(int64(info.captureUs))
			}
		}
	}
}

type objInfo struct {
	keyframe   bool
	captureUs  uint64
	hasCapture bool
}

// parseExts reads the LOC extension list (capture timestamp + frame marking).
func parseExts(exts []byte) objInfo {
	r := bytes.NewReader(exts)
	var info objInfo
	for r.Len() > 0 {
		id, err := quicvarint.Read(r)
		if err != nil {
			return info
		}
		if id%2 == 1 { // odd -> length-prefixed bytes (e.g. video config)
			n, err := quicvarint.Read(r)
			if err != nil {
				return info
			}
			if _, err := r.Seek(int64(n), io.SeekCurrent); err != nil {
				return info
			}
			continue
		}
		v, err := quicvarint.Read(r) // even -> varint value
		if err != nil {
			return info
		}
		switch id {
		case locCaptureTimestamp:
			info.captureUs, info.hasCapture = v, true
		case locVideoFrameMarking:
			if v&vfmKeyframeBit != 0 {
				info.keyframe = true
			}
		}
	}
	return info
}

// Stats returns the reception snapshot so far.
func (s *Subscriber) Stats() Stats {
	toMs := func(v int64) float64 { return float64(v) / 1e6 }
	vbytes, abytes := s.videoBytes.Load(), s.audioBytes.Load()
	st := Stats{
		VideoFrames:     s.videoFrames.Load(),
		KeyFrames:       s.keyFrames.Load(),
		Groups:          s.groups.Load(),
		VideoBytes:      vbytes,
		AudioFrames:     s.audioFrames.Load(),
		AudioBytes:      abytes,
		Bytes:           vbytes + abytes,
		FirstFrameMs:    toMs(s.firstFrameNs.Load()),
		FirstKeyframeMs: toMs(s.firstKeyframeNs.Load()),
		ElapsedSec:      time.Since(s.start).Seconds(),
	}
	if st.VideoFrames > 0 && st.AudioFrames > 0 {
		st.AVValid = true
		st.AVSkewMs = float64(s.lastVideoUs.Load()-s.lastAudioUs.Load()) / 1000
	}
	return st
}

// Close tears down the QUIC connection.
func (s *Subscriber) Close() {
	_ = s.conn.CloseWithError(0, "bye")
}
