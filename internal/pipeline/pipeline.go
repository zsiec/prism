// Package pipeline orchestrates the demux-to-distribution data flow for a
// single stream, forwarding video, audio, and caption frames from the
// Demuxer to the Relay while collecting telemetry.
package pipeline

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/internal/demux"
	"github.com/zsiec/prism/internal/distribution"
	"github.com/zsiec/prism/internal/media"
	"github.com/zsiec/prism/internal/moq"
)

// Broadcaster is the subset of distribution.Relay that the pipeline uses
// to fan out parsed frames to viewers. Accepting an interface here decouples
// the pipeline from the concrete Relay type, making it testable with stubs.
type Broadcaster interface {
	BroadcastVideo(frame *media.VideoFrame)
	BroadcastAudio(frame *media.AudioFrame)
	BroadcastCaptions(frame *ccx.CaptionFrame)
	SetVideoInfo(info distribution.VideoInfo)
	SetAudioTrackCount(count int)
	AudioTrackCount() int
	SetAudioInfo(info distribution.AudioInfo)
	ViewerCount() int
	ViewerStatsAll() []distribution.ViewerStats
}

// Pipeline bridges a single stream's Demuxer and Relay. It reads parsed frames
// from the demuxer's output channels and broadcasts them to all viewers via the
// relay, while accumulating statistics for the control-stream stats overlay.
type Pipeline struct {
	log        *slog.Logger
	demuxer    *demux.Demuxer
	relay      Broadcaster
	streamKey  string
	demuxStats *distribution.DemuxStats
	startTime  time.Time
	protocol   string

	videoForwarded  atomic.Int64
	audioForwarded  atomic.Int64
	videoInfoSent   bool
	audioInfoSent   bool
	captionFwd      atomic.Int64
	lastVideoFwdPTS atomic.Int64
	lastAudioFwdPTS atomic.Int64
	videoChanDepth  atomic.Int32
	audioChanDepth  atomic.Int32
}

// New creates a Pipeline that reads demuxed frames from input and broadcasts
// them to all viewers via the relay.
func New(streamKey string, input io.Reader, relay Broadcaster) *Pipeline {
	p := &Pipeline{
		log:       slog.With("stream", streamKey),
		relay:     relay,
		streamKey: streamKey,
	}

	p.demuxer = demux.NewDemuxer(input, slog.With("component", "demuxer", "stream", streamKey))
	p.demuxStats = distribution.NewDemuxStats()
	p.demuxer.SetStats(p.demuxStats)
	p.startTime = time.Now()

	return p
}

// SetProtocol records the ingest protocol name (e.g. "SRT") for inclusion
// in the stats overlay sent to viewers.
func (p *Pipeline) SetProtocol(proto string) {
	p.protocol = proto
}

// StreamSnapshot returns a point-in-time snapshot of stream health metrics,
// suitable for JSON serialization and delivery to viewers via the control stream.
func (p *Pipeline) StreamSnapshot() distribution.StreamSnapshot {
	video, audio, captions, scte35 := p.demuxStats.Snapshot()

	return distribution.StreamSnapshot{
		Timestamp:   time.Now().UnixMilli(),
		UptimeMs:    time.Since(p.startTime).Milliseconds(),
		Protocol:    p.protocol,
		Video:       video,
		Audio:       audio,
		Captions:    captions,
		SCTE35:      scte35,
		ViewerCount: p.relay.ViewerCount(),
		Viewers:     p.relay.ViewerStatsAll(),
	}
}

// PipelineDebug returns low-level forwarding counters and channel depths
// for the /api/streams/{key}/debug endpoint.
func (p *Pipeline) PipelineDebug() distribution.PipelineDebugStats {
	return distribution.PipelineDebugStats{
		VideoForwarded:  p.videoForwarded.Load(),
		AudioForwarded:  p.audioForwarded.Load(),
		CaptionFwd:      p.captionFwd.Load(),
		LastVideoFwdPTS: p.lastVideoFwdPTS.Load(),
		LastAudioFwdPTS: p.lastAudioFwdPTS.Load(),
		VideoChanDepth:  int(p.videoChanDepth.Load()),
		AudioChanDepth:  int(p.audioChanDepth.Load()),
	}
}

// DemuxStats returns the underlying DemuxStats collector for PTS debug queries.
func (p *Pipeline) DemuxStats() *distribution.DemuxStats {
	return p.demuxStats
}

// Run starts the demuxer and frame-forwarding loop. It blocks until the
// context is cancelled, the demuxer finishes, or a channel closes.
func (p *Pipeline) Run(ctx context.Context) error {
	demuxErr := make(chan error, 1)
	go func() {
		err := p.demuxer.Run(ctx)
		p.log.Info("demuxer goroutine exited", "error", err)
		demuxErr <- err
	}()

	select {
	case <-p.demuxer.PMTReady():
		audioTracks := p.demuxer.AudioTrackChannels()
		p.relay.SetAudioTrackCount(len(audioTracks))
		p.log.Info("audio tracks", "count", len(audioTracks))
	case err := <-demuxErr:
		p.log.Info("demuxer finished before PMT", "error", err)
		return nil
	case <-ctx.Done():
		return nil
	}

	lastTrackCount := p.relay.AudioTrackCount()

	videoCh := p.demuxer.Video()
	audioCh := p.demuxer.Audio()
	captionCh := p.demuxer.Captions()

	for {
		p.videoChanDepth.Store(int32(len(videoCh)))
		p.audioChanDepth.Store(int32(len(audioCh)))

		// Priority drain: always forward video frames first to prevent
		// audio (which produces ~3x more frames) from starving video
		// delivery under Go's random select scheduling.
		select {
		case frame, ok := <-videoCh:
			if !ok {
				p.log.Info("video channel closed")
				return nil
			}
			p.forwardVideo(frame)
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return nil

		case frame, ok := <-videoCh:
			if !ok {
				p.log.Info("video channel closed")
				return nil
			}
			p.forwardVideo(frame)

		case frame, ok := <-audioCh:
			if !ok {
				p.log.Info("audio channel closed")
				return nil
			}
			newCount := len(p.demuxer.AudioTrackChannels())
			if newCount > lastTrackCount {
				p.relay.SetAudioTrackCount(newCount)
				p.log.Info("audio tracks updated", "count", newCount)
				lastTrackCount = newCount
			}
			if !p.audioInfoSent && frame.SampleRate > 0 {
				p.relay.SetAudioInfo(distribution.AudioInfo{
					Codec:      "mp4a.40.02",
					SampleRate: frame.SampleRate,
					Channels:   frame.Channels,
				})
				p.audioInfoSent = true
			}
			p.relay.BroadcastAudio(frame)
			p.audioForwarded.Add(1)
			p.lastAudioFwdPTS.Store(frame.PTS)

		case frame, ok := <-captionCh:
			if !ok {
				p.log.Info("caption channel closed")
				return nil
			}
			p.relay.BroadcastCaptions(frame)
			p.captionFwd.Add(1)

		case err := <-demuxErr:
			p.log.Info("demuxer finished", "error", err)
			return nil
		}
	}
}

// forwardVideo extracts video codec info on the first keyframe, then
// broadcasts the frame to all viewers via the relay.
func (p *Pipeline) forwardVideo(frame *media.VideoFrame) {
	if !p.videoInfoSent && frame.IsKeyframe && frame.SPS != nil {
		if vi, ok := p.buildVideoInfo(frame); ok {
			p.relay.SetVideoInfo(vi)
			p.videoInfoSent = true
		}
	}
	p.relay.BroadcastVideo(frame)
	p.videoForwarded.Add(1)
	p.lastVideoFwdPTS.Store(frame.PTS)
}

// buildVideoInfo parses the SPS from a keyframe and builds the VideoInfo
// including decoder configuration record for the catalog.
func (p *Pipeline) buildVideoInfo(frame *media.VideoFrame) (distribution.VideoInfo, bool) {
	var vi distribution.VideoInfo
	if frame.Codec == "h265" {
		info, err := demux.ParseHEVCSPS(frame.SPS)
		if err != nil {
			return vi, false
		}
		vi = distribution.VideoInfo{
			Codec:  info.CodecString(),
			Width:  info.Width,
			Height: info.Height,
		}
		if frame.VPS != nil {
			vi.DecoderConfig = moq.BuildHEVCDecoderConfig(frame.VPS, frame.SPS, frame.PPS)
		}
	} else {
		info, err := demux.ParseSPS(frame.SPS)
		if err != nil {
			return vi, false
		}
		vi = distribution.VideoInfo{
			Codec:  info.CodecString(),
			Width:  info.Width,
			Height: info.Height,
		}
		vi.DecoderConfig = moq.BuildAVCDecoderConfig(frame.SPS, frame.PPS)
	}
	return vi, vi.Width > 0
}
