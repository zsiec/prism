package dash

import (
	"bytes"
	"fmt"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/zsiec/prism/demux"
)

// initSegmentInfo holds codec and track metadata extracted from an fMP4 init
// segment (ftyp + moov).
type initSegmentInfo struct {
	VideoCodec     string // "av1", "h264", etc.
	Width          int
	Height         int
	SeqHeaderOBU   []byte // AV1: sequence header OBU from av1C ConfigOBUs
	AudioCodec     string // e.g. "mp4a.40.2"
	SampleRate     int
	Channels       int
	VideoTimescale uint32
	AudioTimescale uint32
	VideoTrackID   uint32
	AudioTrackID   uint32
	videoTrex      *mp4.TrexBox // stored for GetFullSamples
	audioTrex      *mp4.TrexBox
}

// mediaSample is a decoded audio or video sample with timestamps in
// microseconds.
type mediaSample struct {
	PTS        int64 // microseconds
	DTS        int64 // microseconds
	Data       []byte
	IsKeyframe bool
}

// parseInitSegment decodes an fMP4 init segment (ftyp+moov) and extracts
// video/audio track metadata including codec configuration.
func parseInitSegment(data []byte) (*initSegmentInfo, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty init segment data")
	}

	parsed, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode init segment: %w", err)
	}

	// The moov box can be at parsed.Moov (when DecodeFile sees ftyp+moov)
	// or at parsed.Init.Moov (when it's recognized as a fragmented init).
	moov := parsed.Moov
	if moov == nil && parsed.Init != nil {
		moov = parsed.Init.Moov
	}
	if moov == nil {
		return nil, fmt.Errorf("no moov box found in init segment")
	}

	if len(moov.Traks) == 0 {
		return nil, fmt.Errorf("no tracks found in init segment")
	}

	info := &initSegmentInfo{}

	for _, trak := range moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Hdlr == nil {
			continue
		}

		trackID := trak.Tkhd.TrackID
		handler := trak.Mdia.Hdlr.HandlerType

		switch handler {
		case "vide":
			info.VideoTrackID = trackID
			if trak.Mdia.Mdhd != nil {
				info.VideoTimescale = trak.Mdia.Mdhd.Timescale
			}
			if err := extractVideoInfo(trak, info); err != nil {
				return nil, fmt.Errorf("extract video info: %w", err)
			}
		case "soun":
			info.AudioTrackID = trackID
			if trak.Mdia.Mdhd != nil {
				info.AudioTimescale = trak.Mdia.Mdhd.Timescale
			}
			if err := extractAudioInfo(trak, info); err != nil {
				return nil, fmt.Errorf("extract audio info: %w", err)
			}
		}
	}

	// Resolve trex boxes from mvex.
	if moov.Mvex != nil {
		resolveTrex(moov.Mvex, info)
	}

	return info, nil
}

// extractVideoInfo populates video fields on info from the given video trak.
func extractVideoInfo(trak *mp4.TrakBox, info *initSegmentInfo) error {
	stsd := getStsd(trak)
	if stsd == nil {
		return fmt.Errorf("no stsd box in video track")
	}

	if stsd.Av01 != nil {
		info.VideoCodec = "av1"
		info.Width = int(stsd.Av01.Width)
		info.Height = int(stsd.Av01.Height)
		if stsd.Av01.Av1C != nil {
			configOBUs := stsd.Av01.Av1C.ConfigOBUs
			seqHdr := demux.FindSequenceHeaderOBU(configOBUs)
			if seqHdr != nil {
				info.SeqHeaderOBU = make([]byte, len(seqHdr))
				copy(info.SeqHeaderOBU, seqHdr)
			}
		}
	} else if stsd.AvcX != nil {
		info.VideoCodec = "h264"
		info.Width = int(stsd.AvcX.Width)
		info.Height = int(stsd.AvcX.Height)
	} else {
		return fmt.Errorf("unsupported video codec (no av01 or avcX in stsd)")
	}
	return nil
}

// extractAudioInfo populates audio fields on info from the given audio trak.
func extractAudioInfo(trak *mp4.TrakBox, info *initSegmentInfo) error {
	stsd := getStsd(trak)
	if stsd == nil {
		return fmt.Errorf("no stsd box in audio track")
	}

	if stsd.Mp4a != nil {
		info.AudioCodec = "mp4a.40.2"
		info.SampleRate = int(stsd.Mp4a.SampleRate)
		info.Channels = int(stsd.Mp4a.ChannelCount)
	} else {
		return fmt.Errorf("unsupported audio codec (no mp4a in stsd)")
	}
	return nil
}

// getStsd returns the stsd box from a track, or nil if the path is incomplete.
func getStsd(trak *mp4.TrakBox) *mp4.StsdBox {
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
		return nil
	}
	return trak.Mdia.Minf.Stbl.Stsd
}

// resolveTrex finds TrexBox entries matching video and audio track IDs.
func resolveTrex(mvex *mp4.MvexBox, info *initSegmentInfo) {
	// mp4ff stores a single trex in Trex and multiple in Trexs.
	trexList := mvex.Trexs
	if len(trexList) == 0 && mvex.Trex != nil {
		trexList = []*mp4.TrexBox{mvex.Trex}
	}
	for _, trex := range trexList {
		switch trex.TrackID {
		case info.VideoTrackID:
			info.videoTrex = trex
		case info.AudioTrackID:
			info.audioTrex = trex
		}
	}
}

// parseMediaSegment decodes an fMP4 media segment and extracts samples for the
// specified track type. The init parameter must have been obtained from
// parseInitSegment on the corresponding init segment.
func parseMediaSegment(data []byte, init *initSegmentInfo, isVideo bool) ([]mediaSample, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty media segment data")
	}
	if init == nil {
		return nil, fmt.Errorf("nil init segment info")
	}

	parsed, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode media segment: %w", err)
	}

	var trex *mp4.TrexBox
	var timescale uint32
	if isVideo {
		trex = init.videoTrex
		timescale = init.VideoTimescale
	} else {
		trex = init.audioTrex
		timescale = init.AudioTimescale
	}

	if timescale == 0 {
		return nil, fmt.Errorf("timescale is zero")
	}

	var samples []mediaSample

	for _, seg := range parsed.Segments {
		for _, frag := range seg.Fragments {
			fullSamples, err := frag.GetFullSamples(trex)
			if err != nil {
				return nil, fmt.Errorf("get full samples: %w", err)
			}

			for i := range fullSamples {
				fs := &fullSamples[i]
				ms := mediaSample{
					DTS:        scaleToMicroseconds(fs.DecodeTime, timescale),
					PTS:        scaleToMicroseconds(uint64(fs.PresentationTime()), timescale),
					Data:       fs.Data,
					IsKeyframe: fs.IsSync(),
				}
				samples = append(samples, ms)
			}
		}
	}

	return samples, nil
}

// scaleToMicroseconds converts a timestamp in the given timescale to
// microseconds.
func scaleToMicroseconds(ts uint64, timescale uint32) int64 {
	return int64(ts * 1_000_000 / uint64(timescale))
}
