package demux

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/internal/media"
	"github.com/zsiec/prism/internal/mpegts"
	"github.com/zsiec/prism/internal/scte35"
)

const (
	streamTypeH264            = 0x1B
	streamTypeH265            = 0x24
	streamTypeAAC             = 0x0F
	scte35PIDWellKnown uint16 = 500
)

// AudioTrackInfo associates an MPEG-TS PID with its zero-based track index,
// used to distinguish multiple audio programs within a single transport stream.
type AudioTrackInfo struct {
	PID        uint16
	TrackIndex int
}

// StatsRecorder is the interface accepted by Demuxer for recording stream
// telemetry. The distribution layer's DemuxStats implements this interface.
type StatsRecorder interface {
	RecordVideoFrame(bytes int64, isKeyframe bool, pts int64)
	RecordAudioFrame(trackIdx int, bytes int64, pts int64, sampleRate, channels int)
	RecordCaption(channel int)
	RecordResolution(width, height int)
	RecordTimecode(tc string)
	RecordSCTE35(event SCTE35Event)
	RecordVideoCodec(codec string)
}

// SCTE35Event represents a parsed SCTE-35 splice information event extracted
// from the transport stream, including splice inserts, time signals, and
// segmentation descriptors used for ad insertion and content identification.
type SCTE35Event struct {
	PTS                int64   `json:"pts"`
	CommandType        string  `json:"commandType"`
	CommandTypeID      uint32  `json:"commandTypeId"`
	EventID            uint32  `json:"eventId,omitempty"`
	SegmentationType   string  `json:"segmentationType,omitempty"`
	SegmentationTypeID uint32  `json:"segmentationTypeId,omitempty"`
	Duration           float64 `json:"duration,omitempty"`
	OutOfNetwork       bool    `json:"outOfNetwork,omitempty"`
	Immediate          bool    `json:"immediate,omitempty"`
	Description        string  `json:"description"`
	ReceivedAt         int64   `json:"receivedAt"`
}

// Demuxer splits an MPEG-TS byte stream into video frames, audio frames,
// closed captions (CEA-608/708), and SCTE-35 events. It supports both H.264
// and H.265 video with multiple AAC audio tracks. Parsed output is delivered
// through channels obtained via the Video, Audio, and Captions methods.
type Demuxer struct {
	log         *slog.Logger
	reader      io.Reader
	videoCh     chan *media.VideoFrame
	audioCh     chan *media.AudioFrame
	captionCh   chan *ccx.CaptionFrame
	cea608Decs  map[int]*ccx.CEA608Decoder
	cea708Svcs  map[int]*ccx.CEA708Service
	dtvccBuf    []byte
	videoPID    uint16
	audioPIDs   map[uint16]int
	audioTracks []AudioTrackInfo
	pmtReady    chan struct{}
	pmtDone     bool
	isHEVC      bool
	sps         []byte
	pps         []byte
	vps         []byte
	spsInfo     SPSInfo
	hevcSPSInfo HEVCSPSInfo
	groupID     uint32
	videoCount  int64
	stats       StatsRecorder

	lastCCCtrl      [2][2]byte
	lastCCWasCtrl   [2]bool
	lastCCCtrlFrame [2]int64
}

// NewDemuxer creates a Demuxer that reads MPEG-TS packets from r. Call Run
// to begin demuxing and read from the Video, Audio, and Captions channels.
// If log is nil, slog.Default() is used.
func NewDemuxer(r io.Reader, log *slog.Logger) *Demuxer {
	if log == nil {
		log = slog.Default()
	}
	return &Demuxer{
		log:       log.With("component", "demux"),
		reader:    r,
		videoCh:   make(chan *media.VideoFrame, media.VideoBufferSize),
		audioCh:   make(chan *media.AudioFrame, media.AudioBufferSize),
		captionCh: make(chan *ccx.CaptionFrame, media.CaptionBufferSize),
		audioPIDs: make(map[uint16]int),
		pmtReady:  make(chan struct{}),
		cea708Svcs: map[int]*ccx.CEA708Service{
			1: ccx.NewCEA708Service(),
			2: ccx.NewCEA708Service(),
			3: ccx.NewCEA708Service(),
			4: ccx.NewCEA708Service(),
			5: ccx.NewCEA708Service(),
			6: ccx.NewCEA708Service(),
		},
		cea608Decs: map[int]*ccx.CEA608Decoder{
			1: ccx.NewCEA608Decoder(),
			2: ccx.NewCEA608Decoder(),
			3: ccx.NewCEA608Decoder(),
			4: ccx.NewCEA608Decoder(),
		},
	}
}

// Video returns the channel on which parsed video frames are delivered.
func (d *Demuxer) Video() <-chan *media.VideoFrame {
	return d.videoCh
}

// Audio returns the channel on which parsed audio frames are delivered.
func (d *Demuxer) Audio() <-chan *media.AudioFrame {
	return d.audioCh
}

// Captions returns the channel on which decoded CEA-608/708 caption frames
// are delivered.
func (d *Demuxer) Captions() <-chan *ccx.CaptionFrame {
	return d.captionCh
}

// AudioTrackChannels returns metadata for all discovered audio tracks.
func (d *Demuxer) AudioTrackChannels() []AudioTrackInfo {
	return d.audioTracks
}

// PMTReady returns a channel that is closed once the first PMT has been
// parsed and all PID-to-track mappings are established.
func (d *Demuxer) PMTReady() <-chan struct{} {
	return d.pmtReady
}

// SetStats attaches a StatsRecorder that receives telemetry callbacks for
// every video frame, audio frame, caption, and SCTE-35 event processed.
func (d *Demuxer) SetStats(s StatsRecorder) {
	d.stats = s
}

// Run starts the demuxing loop, reading MPEG-TS packets from the underlying
// reader until EOF or context cancellation. Parsed frames are sent to the
// Video, Audio, and Captions channels. Run closes all output channels on return.
func (d *Demuxer) Run(ctx context.Context) error {
	defer close(d.videoCh)
	defer close(d.audioCh)
	defer close(d.captionCh)

	scte35Parser := func(ps []*mpegts.Packet) (ds []*mpegts.DemuxerData, skip bool, err error) {
		if len(ps) == 0 {
			return nil, false, nil
		}
		if ps[0].Header.PID != scte35PIDWellKnown {
			return nil, false, nil
		}
		var payload []byte
		for _, p := range ps {
			payload = append(payload, p.Payload...)
		}
		if len(payload) > 0 && payload[0] == 0x00 {
			payload = payload[1:]
		}
		if len(payload) < 3 {
			return nil, true, nil
		}
		sectionLen := int(payload[1]&0x0F)<<8 | int(payload[2])
		totalLen := 3 + sectionLen
		if totalLen > len(payload) {
			totalLen = len(payload)
		}
		d.handleSCTE35(payload[:totalLen])
		return nil, true, nil
	}

	dmx := mpegts.NewDemuxer(ctx, d.reader,
		mpegts.DemuxerOptPacketSize(188),
		mpegts.DemuxerOptPacketsParser(scte35Parser),
	)

	for {
		data, err := dmx.NextData()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			d.log.Debug("skipping corrupt packet", "error", err)
			continue
		}

		if data.PMT != nil {
			audioIdx := len(d.audioTracks)
			for _, es := range data.PMT.ElementaryStreams {
				switch es.StreamType {
				case streamTypeH264:
					if d.videoPID == 0 {
						d.videoPID = es.ElementaryPID
						d.isHEVC = false
						d.log.Info("found video PID", "pid", es.ElementaryPID, "codec", "H.264")
					}
				case streamTypeH265:
					if d.videoPID == 0 {
						d.videoPID = es.ElementaryPID
						d.isHEVC = true
						d.log.Info("found video PID", "pid", es.ElementaryPID, "codec", "H.265")
					}
				case streamTypeAAC:
					if _, exists := d.audioPIDs[es.ElementaryPID]; !exists {
						d.audioPIDs[es.ElementaryPID] = audioIdx
						d.audioTracks = append(d.audioTracks, AudioTrackInfo{
							PID:        es.ElementaryPID,
							TrackIndex: audioIdx,
						})
						d.log.Info("found audio PID", "pid", es.ElementaryPID, "trackIndex", audioIdx)
						audioIdx++
					}
				}
			}
			if !d.pmtDone {
				d.pmtDone = true
				if d.stats != nil && d.videoPID != 0 {
					if d.isHEVC {
						d.stats.RecordVideoCodec("H.265")
					} else {
						d.stats.RecordVideoCodec("H.264")
					}
				}
				close(d.pmtReady)
			}
			continue
		}

		if data.PES == nil {
			continue
		}

		pid := data.FirstPacket.Header.PID

		if pid == d.videoPID {
			d.handleVideo(ctx, data.PES)
		} else if trackIdx, ok := d.audioPIDs[pid]; ok {
			d.handleAudio(ctx, data.PES, trackIdx)
		}
	}
}

func (d *Demuxer) handleVideo(ctx context.Context, pes *mpegts.PESData) {
	if len(pes.Data) == 0 {
		return
	}

	var pts, dts int64
	if pes.Header != nil && pes.Header.OptionalHeader != nil {
		if pes.Header.OptionalHeader.PTS != nil {
			pts = pes.Header.OptionalHeader.PTS.Base * 1000000 / 90000
		}
		if pes.Header.OptionalHeader.DTS != nil {
			dts = pes.Header.OptionalHeader.DTS.Base * 1000000 / 90000
		} else {
			dts = pts
		}
	}

	if d.isHEVC {
		d.handleVideoHEVC(ctx, pes.Data, pts, dts)
	} else {
		d.handleVideoH264(ctx, pes.Data, pts, dts)
	}
}

func (d *Demuxer) handleVideoH264(ctx context.Context, data []byte, pts, dts int64) {
	nalus := ParseAnnexB(data)
	if len(nalus) == 0 {
		return
	}

	isKeyframe := false
	var naluBytes [][]byte

	for _, nalu := range nalus {
		// Skip AUD and filler data NALUs — unnecessary for clients.
		if nalu.Type == NALTypeAUD || nalu.Type == NALTypeFillerData {
			continue
		}

		switch {
		case IsSPS(nalu.Type):
			d.sps = make([]byte, len(nalu.Data))
			copy(d.sps, nalu.Data)
			isKeyframe = true
			if info, err := ParseSPS(nalu.Data); err == nil {
				d.spsInfo = info
				if d.stats != nil {
					d.stats.RecordResolution(info.Width, info.Height)
				}
			}
		case IsPPS(nalu.Type):
			d.pps = make([]byte, len(nalu.Data))
			copy(d.pps, nalu.Data)
		case IsKeyframe(nalu.Type):
			isKeyframe = true
		case nalu.Type == NALTypeSEI:
			if d.stats != nil && d.spsInfo.PicStructPresent {
				if tc, ok := ParsePicTimingSEI(nalu.Data, d.spsInfo); ok {
					d.stats.RecordTimecode(tc.String())
				}
			}

			d.handleCaptionSEI(ctx, nalu.Data, pts)
		}

		annexB := make([]byte, 4+len(nalu.Data))
		annexB[0] = 0
		annexB[1] = 0
		annexB[2] = 0
		annexB[3] = 1
		copy(annexB[4:], nalu.Data)
		naluBytes = append(naluBytes, annexB)
	}

	d.buildAndEmitFrame(ctx, isKeyframe, naluBytes, "h264", pts, dts)
}

func (d *Demuxer) handleVideoHEVC(ctx context.Context, data []byte, pts, dts int64) {
	nalus := ParseAnnexBHEVC(data)
	if len(nalus) == 0 {
		return
	}

	isKeyframe := false
	var naluBytes [][]byte

	for _, nalu := range nalus {
		// Skip AUD and filler data NALUs — unnecessary for clients.
		if nalu.Type == HEVCNALAUD || nalu.Type == HEVCNALFillerData {
			continue
		}

		switch {
		case IsHEVCVPS(nalu.Type):
			d.vps = make([]byte, len(nalu.Data))
			copy(d.vps, nalu.Data)
		case IsHEVCSPS(nalu.Type):
			d.sps = make([]byte, len(nalu.Data))
			copy(d.sps, nalu.Data)
			if info, err := ParseHEVCSPS(nalu.Data); err == nil {
				d.hevcSPSInfo = info
				if d.stats != nil {
					d.stats.RecordResolution(info.Width, info.Height)
				}
			}
		case IsHEVCPPS(nalu.Type):
			d.pps = make([]byte, len(nalu.Data))
			copy(d.pps, nalu.Data)
		case IsHEVCKeyframe(nalu.Type):
			isKeyframe = true
		case nalu.Type == HEVCNALSEIPrefix:
			if len(nalu.Data) > 2 {
				d.handleCaptionSEI(ctx, nalu.Data, pts)
			}
		}

		annexB := make([]byte, 4+len(nalu.Data))
		annexB[0] = 0
		annexB[1] = 0
		annexB[2] = 0
		annexB[3] = 1
		copy(annexB[4:], nalu.Data)
		naluBytes = append(naluBytes, annexB)
	}

	d.buildAndEmitFrame(ctx, isKeyframe, naluBytes, "h265", pts, dts)
}

func (d *Demuxer) buildAndEmitFrame(ctx context.Context, isKeyframe bool, naluBytes [][]byte, codec string, pts, dts int64) {
	if isKeyframe {
		d.groupID++
	}

	frame := &media.VideoFrame{
		PTS:        pts,
		DTS:        dts,
		IsKeyframe: isKeyframe,
		NALUs:      naluBytes,
		Codec:      codec,
		GroupID:    d.groupID,
	}

	if d.sps != nil {
		frame.SPS = make([]byte, len(d.sps))
		copy(frame.SPS, d.sps)
	}
	if d.pps != nil {
		frame.PPS = make([]byte, len(d.pps))
		copy(frame.PPS, d.pps)
	}
	if d.vps != nil {
		frame.VPS = make([]byte, len(d.vps))
		copy(frame.VPS, d.vps)
	}

	d.emitVideoFrame(ctx, frame, naluBytes, pts)
}

func (d *Demuxer) handleCaptionSEI(ctx context.Context, seiData []byte, pts int64) {
	cd := ccx.ExtractCaptions(seiData)
	if cd == nil {
		return
	}

	for _, pair := range cd.CC608Pairs {
		cc1, cc2 := pair.Data[0], pair.Data[1]

		isCtrl := cc1 >= 0x10 && cc1 <= 0x1F
		f := pair.Field
		if isCtrl {
			cp := [2]byte{cc1, cc2}
			frameGap := d.videoCount - d.lastCCCtrlFrame[f]
			if d.lastCCWasCtrl[f] && d.lastCCCtrl[f] == cp && frameGap <= 2 {
				d.lastCCWasCtrl[f] = false
				continue
			}
			d.lastCCCtrl[f] = cp
			d.lastCCWasCtrl[f] = true
			d.lastCCCtrlFrame[f] = d.videoCount
		} else {
			d.lastCCWasCtrl[f] = false
		}

		dec := d.cea608Decs[pair.Channel]
		if dec == nil {
			continue
		}
		text := dec.Decode(cc1, cc2)
		if text != "" {
			frame := &ccx.CaptionFrame{PTS: pts, Text: text, Channel: pair.Channel}
			frame.Regions = dec.StyledRegions()
			if d.stats != nil {
				d.stats.RecordCaption(pair.Channel)
			}
			select {
			case d.captionCh <- frame:
			case <-ctx.Done():
				return
			}
		}
	}

	for _, t := range cd.DTVCC {
		if t.Start {
			d.drainDTVCC(ctx, pts)
			d.dtvccBuf = d.dtvccBuf[:0]
		}
		d.dtvccBuf = append(d.dtvccBuf, t.Data[0], t.Data[1])
	}
}

func (d *Demuxer) emitVideoFrame(ctx context.Context, frame *media.VideoFrame, naluBytes [][]byte, pts int64) {
	d.videoCount++

	if d.stats != nil {
		var totalBytes int64
		for _, n := range naluBytes {
			totalBytes += int64(len(n))
		}
		d.stats.RecordVideoFrame(totalBytes, frame.IsKeyframe, pts)
	}

	select {
	case d.videoCh <- frame:
	case <-ctx.Done():
	}
}

func (d *Demuxer) drainDTVCC(ctx context.Context, pts int64) {
	if len(d.dtvccBuf) < 1 {
		return
	}

	packetSize := ccx.DTVCCPacketSize(d.dtvccBuf[0])
	if len(d.dtvccBuf) < packetSize {
		return
	}

	for _, block := range ccx.ParseDTVCCPacket(d.dtvccBuf[:packetSize]) {
		svc := d.cea708Svcs[block.ServiceNum]
		if svc == nil {
			continue
		}
		if svc.ProcessBlock(block.Data) {
			text := svc.DisplayText()
			if text != "" {
				channel := block.ServiceNum + 6
				frame := &ccx.CaptionFrame{PTS: pts, Text: text, Channel: channel}
				frame.Regions = svc.StyledRegions()
				if d.stats != nil {
					d.stats.RecordCaption(channel)
				}
				select {
				case d.captionCh <- frame:
				case <-ctx.Done():
					return
				}
			}
		}
	}
	d.dtvccBuf = d.dtvccBuf[packetSize:]
}

func (d *Demuxer) handleSCTE35(section []byte) {
	if d.stats == nil || len(section) == 0 {
		return
	}

	sis, err := scte35.DecodeBytes(section)
	if err != nil {
		d.log.Warn("failed to parse SCTE-35", "error", err)
		return
	}

	event := SCTE35Event{
		ReceivedAt: time.Now().UnixMilli(),
	}

	if sis.SpliceCommand == nil {
		return
	}

	switch cmd := sis.SpliceCommand.(type) {
	case *scte35.SpliceInsert:
		event.CommandType = "splice_insert"
		event.CommandTypeID = scte35.SpliceInsertType
		event.EventID = cmd.SpliceEventID
		event.OutOfNetwork = cmd.OutOfNetworkIndicator
		event.Immediate = cmd.SpliceImmediateFlag
		if cmd.BreakDuration != nil {
			event.Duration = float64(cmd.BreakDuration.Duration) / 90000.0
		}
		if event.OutOfNetwork {
			event.Description = "Splice Out (Ad Insertion)"
		} else {
			event.Description = "Splice In (Return to Program)"
		}
	case *scte35.TimeSignal:
		event.CommandType = "time_signal"
		event.CommandTypeID = scte35.TimeSignalType
		if cmd.SpliceTime.PTSTime != nil {
			event.PTS = int64(*cmd.SpliceTime.PTSTime)
		}
		event.Description = "Time Signal"
	case *scte35.SpliceNull:
		event.CommandType = "splice_null"
		event.CommandTypeID = scte35.SpliceNullType
		event.Description = "Heartbeat"
	default:
		event.CommandType = "unknown"
		event.Description = "Unknown Command"
	}

	for _, desc := range sis.SpliceDescriptors {
		if sd, ok := desc.(*scte35.SegmentationDescriptor); ok {
			event.EventID = sd.SegmentationEventID
			event.SegmentationTypeID = sd.SegmentationTypeID
			event.SegmentationType = sd.Name()
			if sd.SegmentationDuration != nil {
				event.Duration = float64(*sd.SegmentationDuration) / 90000.0
			}
			event.Description = sd.Name()
			break
		}
	}

	d.log.Debug("SCTE-35", "command", event.CommandType, "desc", event.Description, "eventID", event.EventID)
	d.stats.RecordSCTE35(event)
}

func (d *Demuxer) handleAudio(ctx context.Context, pes *mpegts.PESData, trackIndex int) {
	if len(pes.Data) == 0 {
		return
	}

	var pts int64
	if pes.Header != nil && pes.Header.OptionalHeader != nil {
		if pes.Header.OptionalHeader.PTS != nil {
			pts = pes.Header.OptionalHeader.PTS.Base * 1000000 / 90000
		}
	}

	aacFrames, err := ParseADTS(pes.Data)
	if err != nil {
		d.log.Warn("failed to parse ADTS", "error", err)
		return
	}

	for i, aac := range aacFrames {
		framePTS := pts
		if aac.SampleRate > 0 {
			framePTS += int64(i) * 1024 * 1_000_000 / int64(aac.SampleRate)
		}

		frame := &media.AudioFrame{
			PTS:        framePTS,
			Data:       aac.Data,
			SampleRate: aac.SampleRate,
			Channels:   aac.Channels,
			TrackIndex: trackIndex,
		}

		if d.stats != nil {
			d.stats.RecordAudioFrame(trackIndex, int64(len(aac.Data)), framePTS, aac.SampleRate, aac.Channels)
		}

		select {
		case d.audioCh <- frame:
		case <-ctx.Done():
			return
		}
	}
}
