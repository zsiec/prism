package dash

import (
	"sync/atomic"
	"time"

	"github.com/zsiec/prism/distribution"
	"github.com/zsiec/prism/media"
)

// Compile-time interface check.
var _ distribution.StatsProvider = (*dashPipeline)(nil)

// broadcaster is the subset of distribution.Relay the DASH pipeline uses.
// It omits BroadcastCaptions because DASH sources do not carry CEA-608/708.
type broadcaster interface {
	BroadcastVideo(frame *media.VideoFrame)
	BroadcastAudio(frame *media.AudioFrame)
	SetVideoInfo(info distribution.VideoInfo)
	SetAudioTrackCount(count int)
	AudioTrackCount() int
	SetAudioInfo(info distribution.AudioInfo)
	ViewerCount() int
	ViewerStatsAll() []distribution.ViewerStats
}

// dashPipeline bridges fMP4 mediaSamples to the frame-based Relay for fan-out
// to MoQ viewers. It converts decoded DASH segments into media.VideoFrame and
// media.AudioFrame values and broadcasts them through the Relay.
type dashPipeline struct {
	streamKey string
	relay     broadcaster
	seqHdrOBU []byte // AV1 sequence header OBU, prepended to keyframes as SPS
	startTime time.Time
	groupID   uint32

	videoForwarded atomic.Int64
	audioForwarded atomic.Int64
}

// newDASHPipeline creates a pipeline that converts DASH mediaSamples into
// VideoFrame/AudioFrame values and broadcasts them via relay. The seqHdrOBU
// is the AV1 sequence header OBU extracted from the init segment, attached
// to keyframes so downstream decoders can initialize.
func newDASHPipeline(streamKey string, relay broadcaster, seqHdrOBU []byte) *dashPipeline {
	return &dashPipeline{
		streamKey: streamKey,
		relay:     relay,
		seqHdrOBU: seqHdrOBU,
		startTime: time.Now(),
	}
}

// processVideoSamples converts video mediaSamples into VideoFrames and
// broadcasts them through the relay. Keyframes start a new group and carry
// the AV1 sequence header OBU as SPS so that late-joining decoders can
// configure themselves.
//
// Frames are paced based on PTS deltas to avoid bursting an entire segment
// worth of frames at once (which causes jittery playback at the viewer).
func (p *dashPipeline) processVideoSamples(samples []mediaSample) {
	if len(samples) == 0 {
		return
	}

	basePTS := samples[0].PTS
	wallStart := time.Now()

	for i := range samples {
		s := &samples[i]

		// Pace: sleep until this frame's presentation time relative to the
		// first frame in the batch, so frames arrive at ~real-time cadence.
		if i > 0 {
			ptsDelta := time.Duration(s.PTS-basePTS) * time.Microsecond
			wallElapsed := time.Since(wallStart)
			if sleep := ptsDelta - wallElapsed; sleep > 0 {
				time.Sleep(sleep)
			}
		}

		frame := &media.VideoFrame{
			PTS:        s.PTS,
			DTS:        s.DTS,
			IsKeyframe: s.IsKeyframe,
			Codec:      "av1",
			WireData:   s.Data,
		}

		if s.IsKeyframe {
			p.groupID++
			frame.SPS = p.seqHdrOBU
		}
		frame.GroupID = p.groupID

		p.relay.BroadcastVideo(frame)
		p.videoForwarded.Add(1)
	}
}

// processAudioSamples converts audio mediaSamples into AudioFrames and
// broadcasts them through the relay. Paced by PTS like video.
func (p *dashPipeline) processAudioSamples(samples []mediaSample) {
	if len(samples) == 0 {
		return
	}

	basePTS := samples[0].PTS
	wallStart := time.Now()

	for i := range samples {
		s := &samples[i]

		if i > 0 {
			ptsDelta := time.Duration(s.PTS-basePTS) * time.Microsecond
			wallElapsed := time.Since(wallStart)
			if sleep := ptsDelta - wallElapsed; sleep > 0 {
				time.Sleep(sleep)
			}
		}

		frame := &media.AudioFrame{
			PTS:  s.PTS,
			Data: s.Data,
		}

		p.relay.BroadcastAudio(frame)
		p.audioForwarded.Add(1)
	}
}

// StreamSnapshot returns a point-in-time snapshot of stream health metrics,
// implementing the distribution.StatsProvider interface for the stats overlay
// and REST API.
func (p *dashPipeline) StreamSnapshot() distribution.StreamSnapshot {
	return distribution.StreamSnapshot{
		Timestamp: time.Now().UnixMilli(),
		UptimeMs:  time.Since(p.startTime).Milliseconds(),
		Protocol:  "DASH",
		Video: distribution.VideoStats{
			Codec:       "AV1",
			TotalFrames: p.videoForwarded.Load(),
		},
		ViewerCount: p.relay.ViewerCount(),
		Viewers:     p.relay.ViewerStatsAll(),
	}
}
