package distribution

import (
	"sync/atomic"

	"github.com/zsiec/prism/media"
)

// trySendVideo implements the damaged-group-aware video send logic shared
// by both single-stream viewerSession and multiplexed muxStreamView.
// It drops delta frames belonging to a GOP where an earlier frame was
// dropped, preventing the client from receiving un-decodable data.
func trySendVideo(
	frame *media.VideoFrame,
	videoCh chan *media.VideoFrame,
	damagedGroup *atomic.Uint32,
	videoSent *atomic.Int64,
	videoDropped *atomic.Int64,
) {
	if frame.IsKeyframe {
		damagedGroup.Store(0)
	} else if damagedGroup.Load() == frame.GroupID {
		videoDropped.Add(1)
		return
	}

	select {
	case videoCh <- frame:
		videoSent.Add(1)
	default:
		videoDropped.Add(1)
		if !frame.IsKeyframe {
			damagedGroup.Store(frame.GroupID)
		}
	}
}
