package distribution

import (
	"context"
	"log/slog"
	"sync"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/media"
	"github.com/zsiec/prism/moq"
)

// Viewer is the interface that a viewer session (single or mux) must implement
// to receive frames from a Relay.
type Viewer interface {
	ID() string
	SendVideo(frame *media.VideoFrame)
	SendAudio(frame *media.AudioFrame)
	SendCaptions(frame *ccx.CaptionFrame)
	Stats() ViewerStats
}

// VideoInfo holds the video codec string, resolution, and decoder configuration
// record. Sent to viewers during connection setup so they can configure their
// WebCodecs decoders immediately without waiting for the first keyframe.
type VideoInfo struct {
	Codec         string
	Width         int
	Height        int
	DecoderConfig []byte // AVCDecoderConfigurationRecord or HEVCDecoderConfigurationRecord
}

// AudioInfo holds the audio codec parameters for a single track, derived
// from the first ADTS frame seen by the demuxer.
type AudioInfo struct {
	Codec      string
	SampleRate int
	Channels   int
}

// audioCacheSize is the number of recent audio frames cached per track
// for replay to late-joining subscribers (~1 second at ~23ms/frame for AAC).
const audioCacheSize = 50

// Relay is the fan-out hub for a single stream. It distributes video, audio,
// and caption frames from the pipeline to all connected MoQ viewers. It also
// caches the current GOP so that late-joining viewers can start playback
// immediately from the most recent keyframe, and recent audio frames so that
// new audio subscribers can pre-fill their buffers.
type Relay struct {
	log             *slog.Logger
	mu              sync.RWMutex
	sessions        map[string]Viewer
	audioTrackCount int
	videoInfo       VideoInfo
	videoInfoSet    bool
	videoInfoReady  chan struct{}
	audioInfo       AudioInfo
	audioInfoSet    bool

	gopMu    sync.RWMutex
	gopCache []*media.VideoFrame

	audioMu    sync.RWMutex
	audioCache map[int][]*media.AudioFrame
}

// NewRelay creates a Relay with no viewers.
func NewRelay() *Relay {
	return &Relay{
		log:            slog.With("component", "relay"),
		sessions:       make(map[string]Viewer),
		videoInfoReady: make(chan struct{}),
		audioCache:     make(map[int][]*media.AudioFrame),
	}
}

// SetVideoInfo stores the video codec parameters detected from the first
// keyframe. Called by the pipeline once SPS parsing succeeds.
func (r *Relay) SetVideoInfo(info VideoInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.videoInfoSet {
		r.videoInfo = info
		r.videoInfoSet = true
		close(r.videoInfoReady)
		r.log.Debug("video info set",
			"codec", info.Codec,
			"width", info.Width,
			"height", info.Height,
			"decoderConfigLen", len(info.DecoderConfig))
	}
}

// SetAudioTrackCount sets the number of audio tracks discovered by the demuxer,
// used to advertise available tracks during viewer connection setup.
func (r *Relay) SetAudioTrackCount(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audioTrackCount = count
	if count == 0 {
		r.audioTrackCount = 1
	}
}

// AudioTrackCount returns the number of audio tracks, defaulting to 1.
func (r *Relay) AudioTrackCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.audioTrackCount == 0 {
		return 1
	}
	return r.audioTrackCount
}

// SetAudioInfo stores the audio codec parameters detected from the first
// audio frame. Called by the pipeline once ADTS header parsing succeeds.
func (r *Relay) SetAudioInfo(info AudioInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.audioInfoSet {
		r.audioInfo = info
		r.audioInfoSet = true
		r.log.Debug("audio info set",
			"codec", info.Codec,
			"sampleRate", info.SampleRate,
			"channels", info.Channels)
	}
}

// AudioInfo returns the detected audio codec parameters, or sensible
// defaults if no audio frame has been seen yet.
func (r *Relay) AudioInfo() AudioInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.audioInfoSet {
		return r.audioInfo
	}
	return AudioInfo{Codec: "mp4a.40.02", SampleRate: 48000, Channels: 2}
}

// AddViewer replays the cached GOP to the viewer, then registers it for
// live frame delivery. Replay happens before registration so that
// BroadcastVideo cannot interleave live frames before the replay completes.
func (r *Relay) AddViewer(session Viewer) {
	r.replayGOP(session)

	r.mu.Lock()
	r.sessions[session.ID()] = session
	r.mu.Unlock()

	r.log.Info("viewer added", "session", session.ID(), "viewers", r.ViewerCount())
}

// RemoveViewer unregisters a viewer by ID.
func (r *Relay) RemoveViewer(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()

	r.log.Info("viewer removed", "session", id, "viewers", r.ViewerCount())
}

// VideoInfo returns the detected video codec and resolution, or sensible
// defaults if the first keyframe hasn't arrived yet.
func (r *Relay) VideoInfo() VideoInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.videoInfoSet {
		return r.videoInfo
	}
	return VideoInfo{Codec: "avc1.42E01E", Width: 1920, Height: 1080}
}

// WaitVideoInfo blocks until the real video codec info is available, or until
// ctx is cancelled. Returns true if info is ready.
func (r *Relay) WaitVideoInfo(ctx context.Context) bool {
	r.mu.RLock()
	if r.videoInfoSet {
		r.mu.RUnlock()
		return true
	}
	r.mu.RUnlock()

	select {
	case <-r.videoInfoReady:
		return true
	case <-ctx.Done():
		return false
	}
}

// BroadcastVideo sends a video frame to all connected viewers and updates
// the GOP cache. Codec detection is handled by the pipeline via SetVideoInfo.
func (r *Relay) BroadcastVideo(frame *media.VideoFrame) {
	// Pre-compute AVC1 (length-prefixed) wire data once so all viewers share the same bytes.
	if frame.WireData == nil {
		frame.WireData = moq.AnnexBToAVC1(frame.NALUs)
	}

	r.gopMu.Lock()
	if frame.IsKeyframe {
		r.gopCache = r.gopCache[:0]
	}
	r.gopCache = append(r.gopCache, frame)
	r.gopMu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, session := range r.sessions {
		session.SendVideo(frame)
	}
}

func (r *Relay) replayGOP(session Viewer) {
	r.gopMu.RLock()
	defer r.gopMu.RUnlock()

	for _, frame := range r.gopCache {
		session.SendVideo(frame)
	}
}

// ReplayFullGOPToChannel sends the entire cached GOP (keyframe + all delta
// frames) into a channel, bypassing the Viewer interface. The client-side
// renderer skips to the latest decoded frame, so replaying the full GOP
// provides immediate decodable content at the live edge. Returns the
// number of frames replayed.
func (r *Relay) ReplayFullGOPToChannel(ch chan<- *media.VideoFrame) int {
	r.gopMu.RLock()
	defer r.gopMu.RUnlock()

	replayed := 0
	for _, frame := range r.gopCache {
		select {
		case ch <- frame:
			replayed++
		default:
			return replayed
		}
	}
	return replayed
}

// BroadcastAudio sends an audio frame to all connected viewers and updates
// the per-track audio cache for late-joining subscriber replay.
func (r *Relay) BroadcastAudio(frame *media.AudioFrame) {
	r.audioMu.Lock()
	cache := r.audioCache[frame.TrackIndex]
	if len(cache) >= audioCacheSize {
		copy(cache, cache[1:])
		cache[len(cache)-1] = frame
	} else {
		cache = append(cache, frame)
	}
	r.audioCache[frame.TrackIndex] = cache
	r.audioMu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, session := range r.sessions {
		session.SendAudio(frame)
	}
}

// ReplayAudioToChannel sends the cached recent audio frames for the given
// track index into a channel, pre-filling the subscriber's buffer so
// playback can start without waiting for new frames from the live edge.
// Returns the number of frames replayed.
func (r *Relay) ReplayAudioToChannel(trackIndex int, ch chan<- *media.AudioFrame) int {
	r.audioMu.RLock()
	defer r.audioMu.RUnlock()

	cache := r.audioCache[trackIndex]
	replayed := 0
	for _, frame := range cache {
		select {
		case ch <- frame:
			replayed++
		default:
			return replayed
		}
	}
	return replayed
}

// BroadcastCaptions sends a caption frame to all connected viewers.
func (r *Relay) BroadcastCaptions(frame *ccx.CaptionFrame) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, session := range r.sessions {
		session.SendCaptions(frame)
	}
}

// ViewerCount returns the number of currently connected viewers.
func (r *Relay) ViewerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

// ViewerStatsAll returns delivery metrics for every connected viewer.
func (r *Relay) ViewerStatsAll() []ViewerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats := make([]ViewerStats, 0, len(r.sessions))
	for _, s := range r.sessions {
		stats = append(stats, s.Stats())
	}
	return stats
}
