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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
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
	locExtVideoConfig     = 13   // LOC ext (odd): AVCDecoderConfigurationRecord on keyframes
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

	// Ancillary-signal survival (P4.5): closed captions ride a dedicated MoQ
	// track (frame-level, like video); SCTE-35 ad-markers and SMPTE timecode ride
	// the periodic "stats" track (the out-of-band metadata channel). Under
	// impairment these answer "did the captions/ad-markers survive the link?".
	CaptionFrames  int64  `json:"captionFrames"`  // caption frames delivered glass-to-glass
	StatsSnapshots int64  `json:"statsSnapshots"` // metadata-channel snapshots received
	SCTE35Events   int64  `json:"scte35Events"`   // splice events the server signalled (via stats)
	CaptionTotal   int64  `json:"captionTotal"`   // server's caption-frame count (via stats)
	Timecode       string `json:"timecode"`       // latest SMPTE timecode (via stats)
}

// Subscriber holds a live raw-QUIC MoQ subscription to one stream's video +
// audio tracks.
type Subscriber struct {
	conn          quic.Connection
	control       quic.Stream
	controlReader *bufio.Reader
	videoAlias    uint64
	audioAlias    uint64
	captionAlias  uint64
	statsAlias    uint64
	start         time.Time

	videoFrames, keyFrames, groups, videoBytes atomic.Int64
	audioFrames, audioBytes                    atomic.Int64
	firstFrameNs, firstKeyframeNs              atomic.Int64
	lastVideoUs, lastAudioUs                   atomic.Int64

	captionFrames, statsSnapshots atomic.Int64
	scte35Events, captionTotal    atomic.Int64
	timecode                      atomic.Pointer[string]

	// optional glass-to-glass video dump (the "what survived" filmstrip source):
	// received video access units are accumulated per GOP and reassembled by
	// DumpVideo into a decodable Annex-B elementary stream. Groups lost to
	// impairment are simply absent, so a decoder renders the real freeze /
	// macroblock / black of what actually survived the link.
	dumpVideo  bool
	dumpMu     sync.Mutex
	dumpGroups map[uint64][]byte // groupID -> Annex-B access units (decode order within the GOP)
	dumpConfig []byte            // AVCDecoderConfigurationRecord from LOC ext 13 (first keyframe seen)
}

// EnableVideoDump turns on per-GOP capture of received video access units. Call
// it before Run; after the run window, DumpVideo writes the decodable stream.
func (s *Subscriber) EnableVideoDump() {
	s.dumpVideo = true
	s.dumpGroups = make(map[uint64][]byte)
}

// DumpVideo writes the captured reception as a decodable Annex-B H.264 elementary
// stream: SPS/PPS (from the AVCDecoderConfigurationRecord) followed by the
// received access units in GOP order. Missing groups are absent by design — that
// gap IS the impairment, rendered honestly by any decoder.
func (s *Subscriber) DumpVideo(w io.Writer) error {
	s.dumpMu.Lock()
	defer s.dumpMu.Unlock()
	if hdr := avcConfigToAnnexB(s.dumpConfig); len(hdr) > 0 {
		if _, err := w.Write(hdr); err != nil {
			return err
		}
	}
	ids := make([]uint64, 0, len(s.dumpGroups))
	for id := range s.dumpGroups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, err := w.Write(s.dumpGroups[id]); err != nil {
			return err
		}
	}
	return nil
}

// avc1ToAnnexB converts a length-prefixed (AVC1/ISO-14496-15) access unit — the
// MoQ video object payload format — to Annex-B (start-code framed) for ffmpeg.
func avc1ToAnnexB(p []byte) []byte {
	var out []byte
	for len(p) >= 4 {
		n := int(binary.BigEndian.Uint32(p))
		p = p[4:]
		if n < 0 || n > len(p) {
			break
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, p[:n]...)
		p = p[n:]
	}
	return out
}

// avcConfigToAnnexB extracts SPS+PPS from an AVCDecoderConfigurationRecord and
// returns them as Annex-B NAL units (the decoder config the stream needs first).
func avcConfigToAnnexB(cfg []byte) []byte {
	if len(cfg) < 6 {
		return nil
	}
	var out []byte
	p := cfg[5:] // skip configurationVersion(1) + profile/compat/level(3) + lengthSizeMinusOne(1)
	emit := func(n int) bool {
		if len(p) < 2 {
			return false
		}
		ln := int(binary.BigEndian.Uint16(p))
		p = p[2:]
		if ln > len(p) {
			return false
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, p[:ln]...)
		p = p[ln:]
		return true
	}
	numSPS := int(p[0] & 0x1f)
	p = p[1:]
	for i := 0; i < numSPS; i++ {
		if !emit(i) {
			return out
		}
	}
	if len(p) < 1 {
		return out
	}
	numPPS := int(p[0])
	p = p[1:]
	for i := 0; i < numPPS; i++ {
		if !emit(i) {
			return out
		}
	}
	return out
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

	s := &Subscriber{conn: conn, control: control, controlReader: cr,
		videoAlias: noAlias, audioAlias: noAlias, captionAlias: noAlias, statsAlias: noAlias}

	// Subscribe video (0) + audio0 (1) + captions (2) + stats (3). Captions and
	// stats are the ancillary-survival channels (closed captions; SCTE-35 +
	// timecode), tolerated-absent like audio.
	tracks := []struct {
		rid  uint64
		name string
	}{{0, "video"}, {1, "audio0"}, {2, "captions"}, {3, "stats"}}
	for _, t := range tracks {
		sub := moq.Subscribe{
			RequestID: t.rid, Namespace: []string{"prism", streamKey}, TrackName: t.name,
			Priority: 0, GroupOrder: moq.GroupOrderDescending, Forward: 1, FilterType: moq.FilterLatestObject,
		}
		if err := moq.WriteControlMsg(control, moq.MsgSubscribe, moq.SerializeSubscribe(sub)); err != nil {
			return nil, fmt.Errorf("write SUBSCRIBE %s: %w", t.name, err)
		}
	}
	// Collect a response (OK or ERROR) for each subscribe.
	for got := 0; got < len(tracks); {
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
			case 2:
				s.captionAlias = sok.TrackAlias
			case 3:
				s.statsAlias = sok.TrackAlias
			}
			got++
		case moq.MsgSubscribeError:
			got++ // a track was refused (e.g. no captions); its alias stays the sentinel
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
	var groupID uint64
	for i := 0; i < 2; i++ { // group id, subgroup id
		v, err := quicvarint.Read(br)
		if err != nil {
			return
		}
		if i == 0 {
			groupID = v
		}
	}
	if _, err := br.ReadByte(); err != nil { // publisher priority
		return
	}

	isVideo := trackAlias == s.videoAlias
	isAudio := trackAlias == s.audioAlias
	isCaption := trackAlias == s.captionAlias
	isStats := trackAlias == s.statsAlias
	if !isVideo && !isAudio && !isCaption && !isStats {
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
		var payload []byte
		if payLen > 0 {
			if isStats || (isVideo && s.dumpVideo) { // stats JSON snapshot, or video bytes for the dump
				payload = make([]byte, payLen)
				if _, err := io.ReadFull(br, payload); err != nil {
					return
				}
			} else if _, err := br.Discard(int(payLen)); err != nil {
				return
			}
		}
		info := parseExts(exts)
		now := time.Since(s.start).Nanoseconds()
		switch {
		case isVideo:
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
			if s.dumpVideo { // accumulate the surviving access unit into its GOP
				s.dumpMu.Lock()
				if s.dumpConfig == nil && len(info.config) > 0 {
					s.dumpConfig = info.config
				}
				if len(payload) > 0 {
					s.dumpGroups[groupID] = append(s.dumpGroups[groupID], avc1ToAnnexB(payload)...)
				}
				s.dumpMu.Unlock()
			}
		case isAudio:
			s.audioFrames.Add(1)
			s.audioBytes.Add(int64(payLen))
			if info.hasCapture {
				s.lastAudioUs.Store(int64(info.captureUs))
			}
		case isCaption:
			s.captionFrames.Add(1) // a caption frame survived glass-to-glass
		case isStats:
			s.statsSnapshots.Add(1)
			s.parseStats(payload)
		}
	}
}

// statsWire is the subset of Prism's "stats" track JSON snapshot we read: the
// SCTE-35 / caption / timecode counters that carry the ancillary signals.
type statsWire struct {
	Stats struct {
		Video struct {
			Timecode string `json:"timecode"`
		} `json:"video"`
		Captions struct {
			TotalFrames int64 `json:"totalFrames"`
		} `json:"captions"`
		SCTE35 struct {
			TotalEvents int64 `json:"totalEvents"`
		} `json:"scte35"`
	} `json:"stats"`
}

func (s *Subscriber) parseStats(payload []byte) {
	var w statsWire
	if json.Unmarshal(payload, &w) != nil {
		return
	}
	s.scte35Events.Store(w.Stats.SCTE35.TotalEvents)
	s.captionTotal.Store(w.Stats.Captions.TotalFrames)
	if w.Stats.Video.Timecode != "" {
		tc := w.Stats.Video.Timecode
		s.timecode.Store(&tc)
	}
}

type objInfo struct {
	keyframe   bool
	captureUs  uint64
	hasCapture bool
	config     []byte // AVCDecoderConfigurationRecord (LOC ext 13), when present
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
			if id == locExtVideoConfig { // capture the decoder config for the dump
				buf := make([]byte, n)
				if _, err := io.ReadFull(r, buf); err != nil {
					return info
				}
				info.config = buf
			} else if _, err := r.Seek(int64(n), io.SeekCurrent); err != nil {
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
	st.CaptionFrames = s.captionFrames.Load()
	st.StatsSnapshots = s.statsSnapshots.Load()
	st.SCTE35Events = s.scte35Events.Load()
	st.CaptionTotal = s.captionTotal.Load()
	if tc := s.timecode.Load(); tc != nil {
		st.Timecode = *tc
	}
	return st
}

// Close tears down the QUIC connection.
func (s *Subscriber) Close() {
	_ = s.conn.CloseWithError(0, "bye")
}
