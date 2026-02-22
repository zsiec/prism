package distribution

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/zsiec/prism/internal/certs"
	"github.com/zsiec/prism/internal/moq"
	"github.com/zsiec/prism/internal/webtransport"
)

// StatsProvider is implemented by Pipeline to supply stream statistics
// for the viewer stats overlay and the REST API.
type StatsProvider interface {
	StreamSnapshot() StreamSnapshot
}

// DebugProvider extends StatsProvider with lower-level pipeline and demuxer
// diagnostics, exposed via the /api/streams/{key}/debug endpoint.
type DebugProvider interface {
	StatsProvider
	PipelineDebug() PipelineDebugStats
	DemuxStats() *DemuxStats
}

// PipelineDebugStats captures frame forwarding counters and channel depths
// for the demux-to-relay pipeline, useful for diagnosing backpressure.
type PipelineDebugStats struct {
	VideoForwarded  int64 `json:"videoForwarded"`
	AudioForwarded  int64 `json:"audioForwarded"`
	CaptionFwd      int64 `json:"captionForwarded"`
	LastVideoFwdPTS int64 `json:"lastVideoFwdPTS"`
	LastAudioFwdPTS int64 `json:"lastAudioFwdPTS"`
	VideoChanDepth  int   `json:"videoChanDepth"`
	AudioChanDepth  int   `json:"audioChanDepth"`
}

// PipelineDebugSnapshot is the JSON response for /api/streams/{key}/debug,
// aggregating ingest, demuxer, pipeline, and viewer diagnostics.
type PipelineDebugSnapshot struct {
	Ingest   *IngestDebugStats  `json:"ingest,omitempty"`
	Demuxer  PTSDebugStats      `json:"demuxer"`
	Pipeline PipelineDebugStats `json:"pipeline"`
	Viewers  []ViewerStats      `json:"viewers"`
}

// IngestDebugStats captures SRT ingest connection metrics for the debug API.
type IngestDebugStats struct {
	BytesReceived int64  `json:"bytesReceived"`
	ReadCount     int64  `json:"readCount"`
	ConnectedAt   int64  `json:"connectedAt"`
	UptimeMs      int64  `json:"uptimeMs"`
	RemoteAddr    string `json:"remoteAddr"`
}

// StreamInfo is the JSON-serializable summary of a live stream, returned
// by the /api/streams list endpoint and used by the multi-stream viewer.
type StreamInfo struct {
	Key             string `json:"key"`
	Viewers         int    `json:"viewers"`
	Description     string `json:"description,omitempty"`
	VideoCodec      string `json:"videoCodec,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	AudioTracks     int    `json:"audioTracks,omitempty"`
	AudioChannels   int    `json:"audioChannels,omitempty"`
	HasCaptions     bool   `json:"hasCaptions,omitempty"`
	CaptionChannels []int  `json:"captionChannels,omitempty"`
	HasSCTE35       bool   `json:"hasScte35,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	UptimeMs        int64  `json:"uptimeMs,omitempty"`
}

// StreamLister is a callback that returns the current list of active streams.
type StreamLister func() []StreamInfo

// IngestLookup resolves a stream key to its ingest debug stats, or nil
// if the stream is not currently being ingested.
type IngestLookup func(key string) *IngestDebugStats

// SRTPullFunc initiates an SRT caller-mode pull from a remote address.
type SRTPullFunc func(address, streamKey, streamID string) error

// SRTStopFunc stops an active SRT pull by stream key.
type SRTStopFunc func(streamKey string) error

// SRTListFunc returns all active SRT pulls.
type SRTListFunc func() []SRTPullInfo

// SRTPullInfo describes an active SRT caller-mode pull, returned by the
// /api/srt-pull GET endpoint.
type SRTPullInfo struct {
	Address   string `json:"address"`
	StreamKey string `json:"streamKey"`
	StreamID  string `json:"streamId,omitempty"`
}

// WebTransport session close error codes sent to clients via CloseWithError.
const (
	wtErrStreamNotFound webtransport.SessionErrorCode = 1
	wtErrControlStream  webtransport.SessionErrorCode = 2
	wtErrInternal       webtransport.SessionErrorCode = 3
	wtErrBadRequest     webtransport.SessionErrorCode = 4
	wtErrSetupFailed    webtransport.SessionErrorCode = 5
)

// videoInfoTimeout is how long a new viewer waits for the first keyframe
// (and its SPS/PPS) before proceeding with default codec parameters.
const videoInfoTimeout = 30 * time.Second

// statsInterval is how often per-viewer stats snapshots are sent.
const statsInterval = 1 * time.Second

// ServerConfig holds the configuration for the distribution Server,
// including listen addresses, TLS certificate, and callback hooks.
type ServerConfig struct {
	Addr         string
	WebDir       string
	Cert         *certs.CertInfo
	StreamLister StreamLister
	IngestLookup IngestLookup
	SRTPull      SRTPullFunc
	SRTStop      SRTStopFunc
	SRTList      SRTListFunc
}

// streamResources bundles the relay and stats provider for a single live
// stream, ensuring both are registered and torn down as a unit.
type streamResources struct {
	relay    *Relay
	pipeline StatsProvider
}

// Server is the WebTransport/HTTP3 distribution server. It manages relays,
// pipelines, viewer sessions, and serves both the WebTransport watch
// endpoints and the REST API.
type Server struct {
	config ServerConfig
	wtSrv  *webtransport.Server

	mu      sync.RWMutex
	streams map[string]*streamResources
}

// NewServer creates a distribution Server with the given configuration.
// It returns an error if required fields are missing.
func NewServer(config ServerConfig) (*Server, error) {
	if config.Cert == nil {
		return nil, errors.New("distribution: Cert is required")
	}
	if config.Addr == "" {
		return nil, errors.New("distribution: Addr is required")
	}
	return &Server{
		config:  config,
		streams: make(map[string]*streamResources),
	}, nil
}

// RegisterStream creates a Relay for the given stream key and returns it.
// If the stream already has a relay, the existing one is returned.
func (s *Server) RegisterStream(streamKey string) *Relay {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sr, ok := s.streams[streamKey]; ok {
		return sr.relay
	}
	r := NewRelay()
	s.streams[streamKey] = &streamResources{relay: r}
	return r
}

// UnregisterStream removes the relay and pipeline for a stream key.
func (s *Server) UnregisterStream(streamKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.streams, streamKey)
}

// SetPipeline associates a StatsProvider with a stream key. The stream
// must already be registered via RegisterStream.
func (s *Server) SetPipeline(streamKey string, p StatsProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sr, ok := s.streams[streamKey]; ok {
		sr.pipeline = p
	}
}

// GetPipeline returns the StatsProvider for a stream key, or nil if not found.
func (s *Server) GetPipeline(streamKey string) StatsProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sr, ok := s.streams[streamKey]; ok {
		return sr.pipeline
	}
	return nil
}

// GetRelay returns the Relay for a stream key, or nil if not found.
func (s *Server) GetRelay(streamKey string) *Relay {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sr, ok := s.streams[streamKey]; ok {
		return sr.relay
	}
	return nil
}

// registerAPIRoutes registers the REST API endpoints on the given mux.
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/streams", s.handleListStreams)
	mux.HandleFunc("GET /api/streams/{key}/debug", s.handleStreamDebug)
	mux.HandleFunc("GET /api/cert-hash", s.handleCertHash)
	mux.HandleFunc("GET /api/srt-pull", s.handleSRTPullList)
	mux.HandleFunc("POST /api/srt-pull", s.handleSRTPullCreate)
	mux.HandleFunc("DELETE /api/srt-pull", s.handleSRTPullStop)
	mux.HandleFunc("OPTIONS /api/srt-pull", s.handleSRTPullOptions)
}

// APIHandler returns an http.Handler for the HTTPS REST API, including
// stream listing, debug endpoints, cert hash, and SRT pull management.
func (s *Server) APIHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	if s.config.WebDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(s.config.WebDir)))
	}

	return corsMiddleware(crossOriginIsolationMiddleware(mux))
}

func crossOriginIsolationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// Start launches the HTTP/3 WebTransport server and blocks until the context
// is cancelled or a fatal error occurs.
func (s *Server) Start(ctx context.Context) error {
	wtMux := http.NewServeMux()
	wtMux.HandleFunc("/moq", s.handleMoQ)
	s.registerAPIRoutes(wtMux)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.config.Cert.TLSCert},
	}

	s.wtSrv = &webtransport.Server{
		H3: http3.Server{
			Addr:      s.config.Addr,
			Handler:   corsMiddleware(wtMux),
			TLSConfig: tlsConfig,
			QUICConfig: &quic.Config{
				MaxIdleTimeout: 30 * time.Second,
				Allow0RTT:      true,
			},
		},
		// SECURITY: CheckOrigin accepts all origins. This is intentional for
		// development and local-network use. Production deployments behind a
		// reverse proxy should enforce origin checks at the proxy layer.
		CheckOrigin: func(_ *http.Request) bool {
			return true
		},
	}

	slog.Info("WebTransport server listening", "addr", s.config.Addr)

	stop := context.AfterFunc(ctx, func() { s.wtSrv.Close() })
	defer stop()

	err := s.wtSrv.ListenAndServe()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

type statsMessage struct {
	Type        string         `json:"type"`
	Stats       StreamSnapshot `json:"stats"`
	ViewerStats *ViewerStats   `json:"viewerStats,omitempty"`
}

type certHashResponse struct {
	Hash string `json:"hash"`
	Addr string `json:"addr"`
}

func (s *Server) handleMoQ(w http.ResponseWriter, r *http.Request) {
	session, controlStream, err := s.upgradeMoQ(w, r)
	if err != nil {
		return // upgradeMoQ already logged and closed the session
	}

	streamKey, relay, moqSession, err := s.setupMoQ(r, session, controlStream)
	if err != nil {
		return // setupMoQ already logged and closed the session
	}

	waitCtx, waitCancel := context.WithTimeout(r.Context(), videoInfoTimeout)
	defer waitCancel()
	relay.WaitVideoInfo(waitCtx)

	relay.AddViewer(moqSession)
	defer relay.RemoveViewer(moqSession.ID())

	_ = streamKey // used during setup; kept for clarity
	if err := moqSession.Run(session.Context()); err != nil {
		slog.Debug("moq session ended", "session", moqSession.ID(), "error", err)
	}
}

// upgradeMoQ upgrades the HTTP request to a WebTransport session and accepts
// the bidirectional control stream. On failure it logs, closes the session,
// and returns a non-nil error.
func (s *Server) upgradeMoQ(w http.ResponseWriter, r *http.Request) (*webtransport.Session, webtransport.Stream, error) {
	session, err := s.wtSrv.Upgrade(w, r)
	if err != nil {
		slog.Error("webtransport upgrade failed (moq)", "error", err)
		return nil, nil, err
	}

	slog.Info("moq viewer connected", "remote", r.RemoteAddr)

	controlStream, err := session.AcceptStream(r.Context())
	if err != nil {
		slog.Error("failed to accept moq control stream", "error", err)
		session.CloseWithError(wtErrControlStream, "control stream error")
		return nil, nil, err
	}

	return session, controlStream, nil
}

// setupMoQ performs the MoQ handshake, resolves the stream key (from URL query
// or PATH parameter), and returns the relay and session. On failure it logs,
// closes the session, and returns a non-nil error.
func (s *Server) setupMoQ(r *http.Request, session *webtransport.Session, controlStream webtransport.Stream) (string, *Relay, *MoQSession, error) {
	streamKey := r.URL.Query().Get("stream")

	relay := s.GetRelay(streamKey)
	if relay == nil && streamKey != "" {
		slog.Warn("moq stream not found", "stream", streamKey)
		session.CloseWithError(wtErrStreamNotFound, "stream not found")
		return "", nil, nil, moq.ErrUnknownTrack
	}

	moqSession := NewMoQSession(MoQSessionConfig{
		ID:            fmt.Sprintf("moq-%s-%s", streamKey, r.RemoteAddr),
		Session:       session,
		Control:       controlStream,
		StreamKey:     streamKey,
		Relay:         relay,
		StatsProvider: s.GetPipeline,
	})

	pathKey, err := moqSession.handleSetup()
	if err != nil {
		slog.Warn("moq setup failed", "error", err)
		session.CloseWithError(wtErrSetupFailed, "setup failed")
		return "", nil, nil, err
	}

	// PATH parameter may override URL query stream key
	if pathKey != "" && streamKey == "" {
		streamKey = pathKey
		moqSession.streamKey = streamKey
		relay = s.GetRelay(streamKey)
		if relay == nil {
			slog.Warn("moq stream not found (from PATH)", "stream", streamKey)
			session.CloseWithError(wtErrStreamNotFound, "stream not found")
			return "", nil, nil, moq.ErrUnknownTrack
		}
		moqSession.relay = relay
	}

	if streamKey == "" {
		slog.Warn("moq no stream key provided")
		session.CloseWithError(wtErrBadRequest, "missing stream key")
		return "", nil, nil, fmt.Errorf("moq: missing stream key")
	}

	return streamKey, relay, moqSession, nil
}

func (s *Server) handleListStreams(w http.ResponseWriter, _ *http.Request) {
	var resp []StreamInfo

	if s.config.StreamLister != nil {
		resp = s.config.StreamLister()
	}

	if resp == nil {
		resp = make([]StreamInfo, 0)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleStreamDebug(w http.ResponseWriter, r *http.Request) {
	streamKey := r.PathValue("key")

	s.mu.RLock()
	sr := s.streams[streamKey]
	s.mu.RUnlock()

	if sr == nil || sr.pipeline == nil {
		writeError(w, http.StatusNotFound, "stream not found")
		return
	}

	var snap PipelineDebugSnapshot

	if dp, ok := sr.pipeline.(DebugProvider); ok {
		snap.Pipeline = dp.PipelineDebug()
		snap.Demuxer = dp.DemuxStats().PTSDebug()
	}

	snap.Viewers = sr.relay.ViewerStatsAll()

	if s.config.IngestLookup != nil {
		snap.Ingest = s.config.IngestLookup(streamKey)
	}

	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleCertHash(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, certHashResponse{
		Hash: s.config.Cert.FingerprintBase64(),
		Addr: s.config.Addr,
	})
}

func (s *Server) handleSRTPullOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

// SECURITY: The SRT pull endpoint accepts arbitrary addresses, which could be
// used for SSRF if exposed to untrusted clients. In production, this endpoint
// should be restricted to authenticated operators or internal networks.
func (s *Server) handleSRTPullList(w http.ResponseWriter, _ *http.Request) {
	if s.config.SRTList == nil {
		writeJSON(w, http.StatusOK, []SRTPullInfo{})
		return
	}
	writeJSON(w, http.StatusOK, s.config.SRTList())
}

func (s *Server) handleSRTPullCreate(w http.ResponseWriter, r *http.Request) {
	if s.config.SRTPull == nil {
		writeError(w, http.StatusNotImplemented, "SRT pull not configured")
		return
	}
	var req struct {
		Address   string `json:"address"`
		StreamKey string `json:"streamKey"`
		StreamID  string `json:"streamId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Address == "" || req.StreamKey == "" {
		writeError(w, http.StatusBadRequest, "address and streamKey are required")
		return
	}
	if err := s.config.SRTPull(req.Address, req.StreamKey, req.StreamID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "pulling", "streamKey": req.StreamKey})
}

func (s *Server) handleSRTPullStop(w http.ResponseWriter, r *http.Request) {
	if s.config.SRTStop == nil {
		writeError(w, http.StatusNotImplemented, "SRT pull not configured")
		return
	}
	streamKey := r.URL.Query().Get("streamKey")
	if streamKey == "" {
		writeError(w, http.StatusBadRequest, "streamKey query parameter required")
		return
	}
	if err := s.config.SRTStop(streamKey); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "streamKey": streamKey})
}
