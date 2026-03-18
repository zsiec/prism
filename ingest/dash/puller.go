package dash

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zsiec/prism/distribution"
)

// PullRequest describes a DASH source to pull from.
type PullRequest struct {
	URL        string `json:"url"`
	StreamKey  string `json:"streamKey"`
	VideoRepID string `json:"videoRepresentationId,omitempty"`
	AudioRepID string `json:"audioRepresentationId,omitempty"`
}

// DistributionServer is the subset of distribution.Server needed by the puller.
type DistributionServer interface {
	RegisterStream(key string) *distribution.Relay
	UnregisterStream(key string)
	SetPipeline(key string, p distribution.StatsProvider)
}

// StreamManager is the subset of stream.Manager needed by the puller.
type StreamManager interface {
	Create(key string) (interface{}, bool)
	Remove(key string)
}

type activePull struct {
	req    PullRequest
	cancel context.CancelFunc
}

// Puller manages active DASH pull connections.
type Puller struct {
	log    *slog.Logger
	client *http.Client
	mu     sync.Mutex
	pulls  map[string]*activePull
}

// NewPuller creates a Puller with a default HTTP client. If log is nil,
// slog.Default() is used.
func NewPuller(log *slog.Logger) *Puller {
	if log == nil {
		log = slog.Default()
	}
	return &Puller{
		log: log.With("component", "dash-puller"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		pulls: make(map[string]*activePull),
	}
}

// Pull validates the request, fetches the MPD manifest synchronously, and
// starts a background goroutine that continuously fetches and processes
// DASH segments. It returns immediately after the MPD is validated.
func (p *Puller) Pull(ctx context.Context, req PullRequest, distSrv DistributionServer, mgr StreamManager) error {
	if req.URL == "" {
		return fmt.Errorf("url is required")
	}
	if req.StreamKey == "" {
		return fmt.Errorf("streamKey is required")
	}

	p.mu.Lock()
	if _, exists := p.pulls[req.StreamKey]; exists {
		p.mu.Unlock()
		return fmt.Errorf("pull already active for stream key %q", req.StreamKey)
	}
	p.mu.Unlock()

	// Fetch and validate MPD synchronously so the caller gets immediate
	// feedback on bad URLs or malformed manifests.
	p.log.Info("fetching MPD", "url", req.URL, "stream_key", req.StreamKey)
	mpdData, err := p.fetchURL(ctx, req.URL)
	if err != nil {
		return fmt.Errorf("fetch MPD: %w", err)
	}

	info, err := parseMPD(mpdData)
	if err != nil {
		return fmt.Errorf("parse MPD: %w", err)
	}

	if !info.IsDynamic {
		return fmt.Errorf("MPD is not dynamic (live); static MPDs are not supported")
	}

	// Select video representation.
	videoRep, videoTmpl, err := selectRepresentation(info.VideoAdaptations, req.VideoRepID)
	if err != nil {
		return fmt.Errorf("select video representation: %w", err)
	}

	// Select audio representation (optional — some streams are video-only).
	var audioRep representationInfo
	var audioTmpl segmentTemplate
	hasAudio := len(info.AudioAdaptations) > 0
	if hasAudio {
		audioRep, audioTmpl, err = selectRepresentation(info.AudioAdaptations, req.AudioRepID)
		if err != nil {
			return fmt.Errorf("select audio representation: %w", err)
		}
	}

	// Double-check for duplicate before committing.
	pullCtx, cancel := context.WithCancel(ctx)

	p.mu.Lock()
	if _, exists := p.pulls[req.StreamKey]; exists {
		p.mu.Unlock()
		cancel()
		return fmt.Errorf("pull already active for stream key %q", req.StreamKey)
	}
	p.pulls[req.StreamKey] = &activePull{req: req, cancel: cancel}
	p.mu.Unlock()

	p.log.Info("starting DASH pull",
		"stream_key", req.StreamKey,
		"video_rep", videoRep.ID,
		"video_dims", fmt.Sprintf("%dx%d", videoRep.Width, videoRep.Height),
		"has_audio", hasAudio,
	)

	go p.runPullLoop(pullCtx, req, info, videoRep, videoTmpl, audioRep, audioTmpl, hasAudio, distSrv, mgr)

	return nil
}

// Stop cancels an active pull by stream key.
func (p *Puller) Stop(streamKey string) error {
	p.mu.Lock()
	ap, ok := p.pulls[streamKey]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active pull for stream key %q", streamKey)
	}

	ap.cancel()
	return nil
}

// ActivePulls returns a snapshot of all active pull requests.
func (p *Puller) ActivePulls() []PullRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]PullRequest, 0, len(p.pulls))
	for _, ap := range p.pulls {
		out = append(out, ap.req)
	}
	return out
}

// fetchURL performs an HTTP GET with context support and returns the response body.
func (p *Puller) fetchURL(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP GET %s: status %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}

// deriveBaseURL computes the base URL for resolving relative segment URLs.
// If the MPD contains a BaseURL, that is used; otherwise the MPD URL's
// directory is used.
func deriveBaseURL(mpdURL string, mpdBaseURL string) string {
	if mpdBaseURL != "" {
		return mpdBaseURL
	}
	// Strip filename from MPD URL to get directory.
	u, err := url.Parse(mpdURL)
	if err != nil {
		return ""
	}
	idx := strings.LastIndex(u.Path, "/")
	if idx >= 0 {
		u.Path = u.Path[:idx+1]
	}
	return u.String()
}

// runPullLoop is the background goroutine that continuously fetches DASH
// segments and feeds them into the distribution pipeline.
func (p *Puller) runPullLoop(
	ctx context.Context,
	req PullRequest,
	info *mpdInfo,
	videoRep representationInfo,
	videoTmpl segmentTemplate,
	audioRep representationInfo,
	audioTmpl segmentTemplate,
	hasAudio bool,
	distSrv DistributionServer,
	mgr StreamManager,
) {
	defer func() {
		distSrv.UnregisterStream(req.StreamKey)
		if mgr != nil {
			mgr.Remove(req.StreamKey)
		}
		p.mu.Lock()
		delete(p.pulls, req.StreamKey)
		p.mu.Unlock()
		p.log.Info("DASH pull ended", "stream_key", req.StreamKey)
	}()

	baseURL := deriveBaseURL(req.URL, info.BaseURL)

	// Download and parse video init segment.
	videoInitURL := resolveInitURL(videoTmpl, videoRep.ID, baseURL)
	videoInitData, err := p.fetchURL(ctx, videoInitURL)
	if err != nil {
		p.log.Error("fetch video init segment", "url", videoInitURL, "error", err)
		return
	}

	initInfo, err := parseInitSegment(videoInitData)
	if err != nil {
		p.log.Error("parse video init segment", "error", err)
		return
	}

	// If audio uses a different init URL, download that too.
	if hasAudio {
		audioInitURL := resolveInitURL(audioTmpl, audioRep.ID, baseURL)
		if audioInitURL != videoInitURL {
			audioInitData, err := p.fetchURL(ctx, audioInitURL)
			if err != nil {
				p.log.Warn("fetch audio init segment", "url", audioInitURL, "error", err)
				hasAudio = false
			} else {
				audioInit, err := parseInitSegment(audioInitData)
				if err != nil {
					p.log.Warn("parse audio init segment", "error", err)
					hasAudio = false
				} else {
					// Merge audio info into initInfo.
					initInfo.AudioCodec = audioInit.AudioCodec
					initInfo.SampleRate = audioInit.SampleRate
					initInfo.Channels = audioInit.Channels
					initInfo.AudioTimescale = audioInit.AudioTimescale
					initInfo.AudioTrackID = audioInit.AudioTrackID
					initInfo.audioTrex = audioInit.audioTrex
				}
			}
		}
	}

	// Register stream with distribution server.
	relay := distSrv.RegisterStream(req.StreamKey)
	if mgr != nil {
		mgr.Create(req.StreamKey)
	}

	// Set video info on relay.
	relay.SetVideoInfo(distribution.VideoInfo{
		Codec:  initInfo.VideoCodec,
		Width:  initInfo.Width,
		Height: initInfo.Height,
	})

	// Set audio info if available.
	if hasAudio {
		relay.SetAudioTrackCount(1)
		relay.SetAudioInfo(distribution.AudioInfo{
			Codec:      initInfo.AudioCodec,
			SampleRate: initInfo.SampleRate,
			Channels:   initInfo.Channels,
		})
	}

	// Create pipeline and register as stats provider.
	pipeline := newDASHPipeline(req.StreamKey, relay, initInfo.SeqHeaderOBU)
	distSrv.SetPipeline(req.StreamKey, pipeline)

	// Segment fetch loop.
	lastSegNum := -1
	segmentDuration := time.Duration(0)
	if videoTmpl.Timescale > 0 && videoTmpl.Duration > 0 {
		segmentDuration = time.Duration(float64(videoTmpl.Duration) / float64(videoTmpl.Timescale) * float64(time.Second))
	}

	for {
		if ctx.Err() != nil {
			return
		}

		segNum := computeSegmentNumber(time.Now(), info.AvailabilityStartTime, videoTmpl)

		if segNum == lastSegNum {
			// Sleep until the next segment boundary.
			sleepDur := segmentDuration / 4
			if sleepDur < 100*time.Millisecond {
				sleepDur = 100 * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(sleepDur):
				continue
			}
		}
		lastSegNum = segNum

		// Fetch video segment.
		videoMediaURL := resolveMediaURL(videoTmpl, videoRep.ID, segNum, baseURL)
		videoData, err := p.fetchURL(ctx, videoMediaURL)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.log.Warn("fetch video segment", "seg", segNum, "url", videoMediaURL, "error", err)
			continue
		}

		videoSamples, err := parseMediaSegment(videoData, initInfo, true)
		if err != nil {
			p.log.Warn("parse video segment", "seg", segNum, "error", err)
			continue
		}
		pipeline.processVideoSamples(videoSamples)

		// Fetch audio segment.
		if hasAudio {
			audioSegNum := computeSegmentNumber(time.Now(), info.AvailabilityStartTime, audioTmpl)
			audioMediaURL := resolveMediaURL(audioTmpl, audioRep.ID, audioSegNum, baseURL)
			audioData, err := p.fetchURL(ctx, audioMediaURL)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				p.log.Warn("fetch audio segment", "seg", audioSegNum, "url", audioMediaURL, "error", err)
				continue
			}

			audioSamples, err := parseMediaSegment(audioData, initInfo, false)
			if err != nil {
				p.log.Warn("parse audio segment", "seg", audioSegNum, "error", err)
				continue
			}
			pipeline.processAudioSamples(audioSamples)
		}
	}
}
