package distribution

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/zsiec/prism/demux"
)

// Compile-time interface check.
var _ demux.StatsRecorder = (*DemuxStats)(nil)

// VideoStats holds point-in-time video metrics for a stream, serialized
// as JSON in stats snapshots sent to viewers over the control stream.
type VideoStats struct {
	Codec         string  `json:"codec"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	TotalFrames   int64   `json:"totalFrames"`
	KeyFrames     int64   `json:"keyFrames"`
	DeltaFrames   int64   `json:"deltaFrames"`
	CurrentGOPLen int     `json:"currentGOPLen"`
	BitrateKbps   float64 `json:"bitrateKbps"`
	FrameRate     float64 `json:"frameRate"`
	PTSErrors     int64   `json:"ptsErrors"`
	TotalBytes    int64   `json:"totalBytes"`
	Timecode      string  `json:"timecode,omitempty"`
}

// AudioTrackStats holds per-track audio metrics for a stream.
type AudioTrackStats struct {
	TrackIndex  int     `json:"trackIndex"`
	Codec       string  `json:"codec"`
	SampleRate  int     `json:"sampleRate"`
	Channels    int     `json:"channels"`
	Frames      int64   `json:"frames"`
	BitrateKbps float64 `json:"bitrateKbps"`
	PTSErrors   int64   `json:"ptsErrors"`
	TotalBytes  int64   `json:"totalBytes"`
}

// CaptionStats tracks closed-caption activity across all channels.
type CaptionStats struct {
	ActiveChannels []int `json:"activeChannels"`
	TotalFrames    int64 `json:"totalFrames"`
}

// ViewerStats captures per-viewer delivery metrics including frame counts
// and drop rates, used for diagnostics and the stats overlay.
type ViewerStats struct {
	ID             string `json:"id"`
	VideoSent      int64  `json:"videoSent"`
	AudioSent      int64  `json:"audioSent"`
	CaptionSent    int64  `json:"captionSent"`
	VideoDropped   int64  `json:"videoDropped"`
	AudioDropped   int64  `json:"audioDropped"`
	CaptionDropped int64  `json:"captionDropped"`
	BytesSent      int64  `json:"bytesSent"`
	LastVideoTsMS  int64  `json:"lastVideoTsMs,omitempty"`
	LastAudioTsMS  int64  `json:"lastAudioTsMs,omitempty"`
}

// SCTE35Stats summarizes SCTE-35 splice event activity for a stream.
type SCTE35Stats struct {
	TotalEvents int64               `json:"totalEvents"`
	Recent      []demux.SCTE35Event `json:"recent,omitempty"`
}

// StreamSnapshot is the top-level stats payload sent periodically to viewers
// over the control stream. It aggregates video, audio, caption, SCTE-35,
// and viewer metrics into a single JSON-serializable structure.
type StreamSnapshot struct {
	Timestamp   int64             `json:"ts"`
	UptimeMs    int64             `json:"uptimeMs"`
	Protocol    string            `json:"protocol"`
	IngestBytes int64             `json:"ingestBytes"`
	IngestKbps  float64           `json:"ingestKbps"`
	Video       VideoStats        `json:"video"`
	Audio       []AudioTrackStats `json:"audio"`
	Captions    CaptionStats      `json:"captions"`
	SCTE35      SCTE35Stats       `json:"scte35"`
	ViewerCount int               `json:"viewerCount"`
	Viewers     []ViewerStats     `json:"viewers,omitempty"`
}

// PTSWrapEvent records a detected PTS wrap-around, which occurs when the
// 33-bit MPEG-TS PTS counter overflows (every ~26.5 hours).
type PTSWrapEvent struct {
	Timestamp int64  `json:"ts"`
	Track     string `json:"track"`
	OldPTS    int64  `json:"oldPTS"`
	NewPTS    int64  `json:"newPTS"`
	DeltaMs   int64  `json:"deltaMs"`
}

// PTSDebugStats provides low-level PTS debugging information, including
// first/last timestamps and wrap events, exposed via the debug API endpoint.
type PTSDebugStats struct {
	FirstVideoPTS int64          `json:"firstVideoPTS"`
	FirstAudioPTS int64          `json:"firstAudioPTS"`
	LastVideoPTS  int64          `json:"lastVideoPTS"`
	LastAudioPTS  int64          `json:"lastAudioPTS"`
	VideoPTSWraps int64          `json:"videoPTSWraps"`
	AudioPTSWraps int64          `json:"audioPTSWraps"`
	RecentWraps   []PTSWrapEvent `json:"recentWraps,omitempty"`
}

// DemuxStats accumulates stream telemetry from the demuxer in a
// concurrency-safe manner using atomic counters. It implements the
// demux.StatsRecorder interface and produces point-in-time Snapshots for
// the stats API.
//
// Fields are organized by the mutex/mechanism that guards them:
//   - Atomic counters: lock-free concurrent reads/writes
//   - ptsWrapMu: PTS wrap event log
//   - timecodeMu: SMPTE timecode string
//   - mu: audio track accumulators, caption channels
//   - scte35Mu: SCTE-35 event log
//   - bitrateWindowMu: video bitrate sliding window
//   - fpsWindowMu: video FPS sliding window
//   - videoCodecMu: video codec label
type DemuxStats struct {
	// Atomic counters — no mutex needed
	videoFrames    atomic.Int64
	videoKeyframes atomic.Int64
	videoDelta     atomic.Int64
	videoBytes     atomic.Int64
	currentGOPLen  atomic.Int32
	lastVideoPTS   atomic.Int64
	ptsErrors      atomic.Int64
	videoWidth     atomic.Int32
	videoHeight    atomic.Int32
	firstVideoPTS  atomic.Int64
	firstAudioPTS  atomic.Int64
	videoPTSWraps  atomic.Int64
	audioPTSWraps  atomic.Int64
	firstVideoSet  atomic.Bool
	firstAudioSet  atomic.Bool
	captionCount   atomic.Int64
	scte35Total    atomic.Int64

	// ptsWrapMu guards ptsWrapLog
	ptsWrapMu  sync.Mutex
	ptsWrapLog []PTSWrapEvent

	// timecodeMu guards timecode
	timecodeMu sync.RWMutex
	timecode   string

	// mu guards audioStats and captionChans
	mu           sync.RWMutex
	audioStats   map[int]*audioTrackAccum
	captionChans map[int]bool

	// scte35Mu guards scte35Events
	scte35Mu     sync.RWMutex
	scte35Events []demux.SCTE35Event

	// bitrateWindowMu guards bitrateWindow
	bitrateWindowMu sync.Mutex
	bitrateWindow   []bitrateEntry

	// fpsWindowMu guards fpsWindow
	fpsWindowMu sync.Mutex
	fpsWindow   []time.Time

	// videoCodecMu guards videoCodec
	videoCodecMu sync.RWMutex
	videoCodec   string
}

// audioTrackAccum is a per-track accumulator for audio frame statistics,
// using atomic counters for concurrent updates from the demuxer goroutine.
type audioTrackAccum struct {
	Frames     atomic.Int64
	Bytes      atomic.Int64
	PTSErrors  atomic.Int64
	LastPTS    atomic.Int64
	SampleRate int
	Channels   int
}

type bitrateEntry struct {
	ts    time.Time
	bytes int64
}

// NewDemuxStats creates a DemuxStats ready for use as a StatsRecorder.
func NewDemuxStats() *DemuxStats {
	return &DemuxStats{
		audioStats:   make(map[int]*audioTrackAccum),
		captionChans: make(map[int]bool),
	}
}

// RecordVideoFrame records a video frame's size, type, and PTS, updating
// frame counters, GOP length, bitrate/FPS sliding windows, and PTS continuity.
func (ds *DemuxStats) RecordVideoFrame(bytes int64, isKeyframe bool, pts int64) {
	ds.videoFrames.Add(1)
	ds.videoBytes.Add(bytes)

	if !ds.firstVideoSet.Load() {
		ds.firstVideoPTS.Store(pts)
		ds.firstVideoSet.Store(true)
	}

	if isKeyframe {
		ds.videoKeyframes.Add(1)
		ds.currentGOPLen.Store(1)
	} else {
		ds.videoDelta.Add(1)
		ds.currentGOPLen.Add(1)
	}

	lastPTS := ds.lastVideoPTS.Swap(pts)
	if lastPTS > 0 && pts > 0 {
		delta := pts - lastPTS
		if delta < -30_000_000 {
			ds.videoPTSWraps.Add(1)
			ds.recordPTSWrap("video", lastPTS, pts)
		}
		if delta < 0 || delta > 5_000_000 {
			ds.ptsErrors.Add(1)
		}
	}

	now := time.Now()

	ds.fpsWindowMu.Lock()
	ds.fpsWindow = append(ds.fpsWindow, now)
	fpsCutoff := now.Add(-2 * time.Second)
	j := 0
	for j < len(ds.fpsWindow) && ds.fpsWindow[j].Before(fpsCutoff) {
		j++
	}
	ds.fpsWindow = ds.fpsWindow[j:]
	ds.fpsWindowMu.Unlock()

	ds.bitrateWindowMu.Lock()
	ds.bitrateWindow = append(ds.bitrateWindow, bitrateEntry{ts: now, bytes: bytes})
	cutoff := now.Add(-2 * time.Second)
	i := 0
	for i < len(ds.bitrateWindow) && ds.bitrateWindow[i].ts.Before(cutoff) {
		i++
	}
	ds.bitrateWindow = ds.bitrateWindow[i:]
	ds.bitrateWindowMu.Unlock()
}

// RecordAudioFrame records an audio frame for the given track, creating the
// per-track accumulator on first use.
func (ds *DemuxStats) RecordAudioFrame(trackIdx int, bytes int64, pts int64, sampleRate, channels int) {
	if !ds.firstAudioSet.Load() {
		ds.firstAudioPTS.Store(pts)
		ds.firstAudioSet.Store(true)
	}

	ds.mu.Lock()
	acc, ok := ds.audioStats[trackIdx]
	if !ok {
		acc = &audioTrackAccum{SampleRate: sampleRate, Channels: channels}
		ds.audioStats[trackIdx] = acc
	}
	ds.mu.Unlock()

	acc.Frames.Add(1)
	acc.Bytes.Add(bytes)

	lastPTS := acc.LastPTS.Swap(pts)
	if lastPTS > 0 && pts > 0 {
		delta := pts - lastPTS
		if delta < -30_000_000 {
			ds.audioPTSWraps.Add(1)
			ds.recordPTSWrap("audio", lastPTS, pts)
		}
		if delta < 0 || delta > 5_000_000 {
			acc.PTSErrors.Add(1)
		}
	}
}

const maxPTSWrapLog = 10

func (ds *DemuxStats) recordPTSWrap(track string, oldPTS, newPTS int64) {
	ev := PTSWrapEvent{
		Timestamp: time.Now().UnixMilli(),
		Track:     track,
		OldPTS:    oldPTS,
		NewPTS:    newPTS,
		DeltaMs:   (newPTS - oldPTS) / 1000,
	}
	ds.ptsWrapMu.Lock()
	ds.ptsWrapLog = append(ds.ptsWrapLog, ev)
	if len(ds.ptsWrapLog) > maxPTSWrapLog {
		ds.ptsWrapLog = ds.ptsWrapLog[len(ds.ptsWrapLog)-maxPTSWrapLog:]
	}
	ds.ptsWrapMu.Unlock()
}

// PTSDebug returns a snapshot of PTS debugging information.
func (ds *DemuxStats) PTSDebug() PTSDebugStats {
	ds.ptsWrapMu.Lock()
	wraps := make([]PTSWrapEvent, len(ds.ptsWrapLog))
	copy(wraps, ds.ptsWrapLog)
	ds.ptsWrapMu.Unlock()

	lastAudioPTS := int64(0)
	ds.mu.RLock()
	for _, acc := range ds.audioStats {
		p := acc.LastPTS.Load()
		if p > lastAudioPTS {
			lastAudioPTS = p
		}
	}
	ds.mu.RUnlock()

	return PTSDebugStats{
		FirstVideoPTS: ds.firstVideoPTS.Load(),
		FirstAudioPTS: ds.firstAudioPTS.Load(),
		LastVideoPTS:  ds.lastVideoPTS.Load(),
		LastAudioPTS:  lastAudioPTS,
		VideoPTSWraps: ds.videoPTSWraps.Load(),
		AudioPTSWraps: ds.audioPTSWraps.Load(),
		RecentWraps:   wraps,
	}
}

// RecordVideoCodec stores the detected video codec label (e.g. "H.264", "H.265").
func (ds *DemuxStats) RecordVideoCodec(codec string) {
	ds.videoCodecMu.Lock()
	ds.videoCodec = codec
	ds.videoCodecMu.Unlock()
}

// RecordResolution stores the detected video resolution from an SPS.
func (ds *DemuxStats) RecordResolution(width, height int) {
	ds.videoWidth.Store(int32(width))
	ds.videoHeight.Store(int32(height))
}

// RecordTimecode stores the latest SMPTE 12M timecode string.
func (ds *DemuxStats) RecordTimecode(tc string) {
	ds.timecodeMu.Lock()
	ds.timecode = tc
	ds.timecodeMu.Unlock()
}

const maxRecentSCTE35 = 20
const scte35ExpirySec = 30

// RecordSCTE35 records a SCTE-35 event, maintaining a bounded recent-events window.
func (ds *DemuxStats) RecordSCTE35(event demux.SCTE35Event) {
	ds.scte35Total.Add(1)
	ds.scte35Mu.Lock()
	ds.scte35Events = append(ds.scte35Events, event)
	if len(ds.scte35Events) > maxRecentSCTE35 {
		ds.scte35Events = ds.scte35Events[len(ds.scte35Events)-maxRecentSCTE35:]
	}
	ds.scte35Mu.Unlock()
}

// RecordCaption records a caption frame on the given channel.
func (ds *DemuxStats) RecordCaption(channel int) {
	ds.captionCount.Add(1)
	ds.mu.Lock()
	ds.captionChans[channel] = true
	ds.mu.Unlock()
}

// VideoFPS computes the current frame rate from a 2-second sliding window.
func (ds *DemuxStats) VideoFPS() float64 {
	ds.fpsWindowMu.Lock()
	defer ds.fpsWindowMu.Unlock()

	if len(ds.fpsWindow) < 2 {
		return 0
	}

	first := ds.fpsWindow[0]
	last := ds.fpsWindow[len(ds.fpsWindow)-1]
	dur := last.Sub(first).Seconds()
	if dur <= 0 {
		return 0
	}

	return float64(len(ds.fpsWindow)-1) / dur
}

// VideoBitrateKbps computes the current video bitrate from a 2-second
// sliding window of frame sizes.
func (ds *DemuxStats) VideoBitrateKbps() float64 {
	ds.bitrateWindowMu.Lock()
	defer ds.bitrateWindowMu.Unlock()

	if len(ds.bitrateWindow) < 2 {
		return 0
	}

	first := ds.bitrateWindow[0].ts
	last := ds.bitrateWindow[len(ds.bitrateWindow)-1].ts
	dur := last.Sub(first).Seconds()
	if dur <= 0 {
		return 0
	}

	var total int64
	for _, e := range ds.bitrateWindow {
		total += e.bytes
	}
	return float64(total) * 8 / dur / 1000
}

// Snapshot produces a consistent point-in-time view of all stream statistics.
func (ds *DemuxStats) Snapshot() (VideoStats, []AudioTrackStats, CaptionStats, SCTE35Stats) {
	fps := ds.VideoFPS()

	ds.timecodeMu.RLock()
	tc := ds.timecode
	ds.timecodeMu.RUnlock()

	ds.videoCodecMu.RLock()
	codecLabel := ds.videoCodec
	ds.videoCodecMu.RUnlock()
	if codecLabel == "" {
		codecLabel = "H.264"
	}

	vs := VideoStats{
		Codec:         codecLabel,
		Width:         int(ds.videoWidth.Load()),
		Height:        int(ds.videoHeight.Load()),
		TotalFrames:   ds.videoFrames.Load(),
		KeyFrames:     ds.videoKeyframes.Load(),
		DeltaFrames:   ds.videoDelta.Load(),
		CurrentGOPLen: int(ds.currentGOPLen.Load()),
		BitrateKbps:   ds.VideoBitrateKbps(),
		FrameRate:     fps,
		PTSErrors:     ds.ptsErrors.Load(),
		TotalBytes:    ds.videoBytes.Load(),
		Timecode:      tc,
	}

	ds.mu.RLock()
	audioTracks := make([]AudioTrackStats, 0, len(ds.audioStats))
	for idx, acc := range ds.audioStats {
		totalBytes := acc.Bytes.Load()
		totalFrames := acc.Frames.Load()
		var bitrateKbps float64
		if totalFrames > 0 && acc.SampleRate > 0 {
			durationSec := float64(totalFrames) * 1024 / float64(acc.SampleRate)
			if durationSec > 0 {
				bitrateKbps = float64(totalBytes) * 8 / durationSec / 1000
			}
		}
		audioTracks = append(audioTracks, AudioTrackStats{
			TrackIndex:  idx,
			Codec:       "AAC-LC",
			SampleRate:  acc.SampleRate,
			Channels:    acc.Channels,
			Frames:      totalFrames,
			BitrateKbps: bitrateKbps,
			PTSErrors:   acc.PTSErrors.Load(),
			TotalBytes:  totalBytes,
		})
	}

	activeChans := make([]int, 0, len(ds.captionChans))
	for ch := range ds.captionChans {
		activeChans = append(activeChans, ch)
	}
	ds.mu.RUnlock()

	cs := CaptionStats{
		ActiveChannels: activeChans,
		TotalFrames:    ds.captionCount.Load(),
	}

	ds.scte35Mu.RLock()
	cutoff := time.Now().UnixMilli() - scte35ExpirySec*1000
	var recent []demux.SCTE35Event
	for _, e := range ds.scte35Events {
		if e.ReceivedAt >= cutoff {
			recent = append(recent, e)
		}
	}
	ds.scte35Mu.RUnlock()

	sc := SCTE35Stats{
		TotalEvents: ds.scte35Total.Load(),
		Recent:      recent,
	}

	return vs, audioTracks, cs, sc
}
