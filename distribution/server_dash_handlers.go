package distribution

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleDASHPullList(w http.ResponseWriter, _ *http.Request) {
	if s.config.DASHList == nil {
		writeJSON(w, http.StatusOK, []DASHPullInfo{})
		return
	}
	writeJSON(w, http.StatusOK, s.config.DASHList())
}

func (s *Server) handleDASHPullCreate(w http.ResponseWriter, r *http.Request) {
	if s.config.DASHPull == nil {
		writeError(w, http.StatusNotImplemented, "DASH pull not configured")
		return
	}
	var req struct {
		URL        string `json:"url"`
		StreamKey  string `json:"streamKey"`
		VideoRepID string `json:"videoRepresentationId,omitempty"`
		AudioRepID string `json:"audioRepresentationId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.URL == "" || req.StreamKey == "" {
		writeError(w, http.StatusBadRequest, "url and streamKey are required")
		return
	}
	if err := s.config.DASHPull(req.URL, req.StreamKey, req.VideoRepID, req.AudioRepID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "pulling", "streamKey": req.StreamKey})
}

func (s *Server) handleDASHPullStop(w http.ResponseWriter, r *http.Request) {
	if s.config.DASHStop == nil {
		writeError(w, http.StatusNotImplemented, "DASH pull not configured")
		return
	}
	streamKey := r.URL.Query().Get("streamKey")
	if streamKey == "" {
		writeError(w, http.StatusBadRequest, "streamKey query parameter required")
		return
	}
	if err := s.config.DASHStop(streamKey); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "streamKey": streamKey})
}

func (s *Server) handleDASHPullOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusNoContent)
}
