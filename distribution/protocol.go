// Package distribution implements the WebTransport-based viewer delivery
// layer, including the fan-out relay, MoQ session management, and the
// HTTP/QUIC server that ties them together. The low-level MoQ wire protocol
// codec lives in [github.com/zsiec/prism/moq].
package distribution

import (
	"io"

	"github.com/zsiec/prism/media"
)

// Track ID constants used to identify media types in the MoQ catalog
// and session logic. Audio tracks beyond the first use sequential IDs
// starting at TrackIDAudioBase.
const (
	TrackIDVideo     byte = 0
	TrackIDCaptions  byte = 2
	TrackIDAudioBase byte = 10
)

// Per-viewer caption channel buffer size. Smaller than the demuxer-side
// media.CaptionBufferSize because captions are low-frequency and viewers
// that fall behind should drop rather than accumulate stale text.
const viewerCaptionBuffer = 10

// Publisher priority values for MoQ track subscriptions. Lower values
// indicate higher priority. Video and audio share the highest priority
// so neither starves under congestion. Captions and stats are deprioritized.
const (
	priorityVideo    = 128
	priorityAudio    = 128
	priorityCaptions = 200
	priorityStats    = 220
)

// AudioTrackID converts a zero-based audio track index to its wire track ID.
func AudioTrackID(trackIndex int) byte {
	return TrackIDAudioBase + byte(trackIndex)
}

// StreamFrameWriter abstracts the wire format used to write media data to
// WebTransport unidirectional streams. The MoQ writer implements this
// interface using MoQ Transport subgroup/object framing with LOC extensions.
type StreamFrameWriter interface {
	// WriteStreamHeader writes the stream-level header (subgroup header)
	// at the start of a new unidirectional stream.
	WriteStreamHeader(w io.Writer, trackID byte, groupID uint32, timestampMS uint32) error

	// WriteVideoFrame writes a single video frame (header + payload) to w,
	// returning the total bytes written.
	WriteVideoFrame(w io.Writer, frame *media.VideoFrame) (int64, error)

	// WriteAudioFrame writes a single audio frame (header + payload) to w,
	// returning the total bytes written.
	WriteAudioFrame(w io.Writer, data []byte, timestampMS uint32) (int64, error)

	// WriteCaptionFrame writes a single caption frame (header + payload) to w,
	// returning the total bytes written.
	WriteCaptionFrame(w io.Writer, data []byte, timestampMS uint32) (int64, error)

	// StreamHeaderSize returns the byte size of the stream header written
	// by WriteStreamHeader, used for accurate byte accounting.
	StreamHeaderSize() int64
}
