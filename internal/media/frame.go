// Package media defines the core frame types that flow through the Prism
// processing pipeline, from demuxing through distribution.
package media

// Channel buffer sizes used by both the demuxer (producer) and viewer sessions
// (consumer) to decouple frame production from consumption. Sized to absorb
// jitter without excessive memory: ~2 seconds of video, ~2.5s of audio.
const (
	VideoBufferSize   = 60
	AudioBufferSize   = 120
	CaptionBufferSize = 30
)

// VideoFrame represents a single decoded video access unit (one picture) ready
// for relay to viewers. It carries the raw NAL units in Annex B format along
// with parameter sets needed by decoders to initialize or reconfigure.
type VideoFrame struct {
	PTS        int64
	DTS        int64
	IsKeyframe bool
	NALUs      [][]byte
	SPS        []byte
	PPS        []byte
	VPS        []byte
	Codec      string // "h264" or "h265"
	GroupID    uint32
	WireData   []byte // pre-serialized AVC1 (length-prefixed) NALUs for distribution
}

// AudioFrame represents a single AAC audio frame (ADTS-wrapped) belonging
// to a specific audio track. Multi-track streams produce separate AudioFrames
// with distinct TrackIndex values.
type AudioFrame struct {
	PTS        int64
	Data       []byte
	SampleRate int
	Channels   int
	TrackIndex int
}
