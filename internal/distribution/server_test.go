package distribution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zsiec/prism/internal/certs"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cert, err := certs.Generate(24 * 60 * 60 * 1e9) // 1 day
	if err != nil {
		t.Fatalf("certs.Generate: %v", err)
	}
	srv, err := NewServer(ServerConfig{
		Addr:   ":0",
		Cert:   cert,
		WebDir: "",
		StreamLister: func() []StreamInfo {
			return []StreamInfo{
				{Key: "stream1", Viewers: 2},
				{Key: "stream2", Viewers: 0},
			}
		},
		SRTList: func() []SRTPullInfo { return nil },
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestHandleListStreams(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.APIHandler()

	req := httptest.NewRequest("GET", "/api/streams", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var streams []StreamInfo
	if err := json.NewDecoder(rec.Body).Decode(&streams); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("got %d streams, want 2", len(streams))
	}
}

func TestHandleListStreamsEmpty(t *testing.T) {
	t.Parallel()

	cert, err := certs.Generate(24 * 60 * 60 * 1e9)
	if err != nil {
		t.Fatalf("certs.Generate: %v", err)
	}
	srv, err := NewServer(ServerConfig{
		Addr: ":0",
		Cert: cert,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.APIHandler()

	req := httptest.NewRequest("GET", "/api/streams", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Should return empty array, not null.
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("body = %q, want %q", body, "[]")
	}
}

func TestHandleCertHash(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.APIHandler()

	req := httptest.NewRequest("GET", "/api/cert-hash", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp certHashResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Hash == "" {
		t.Fatal("hash is empty")
	}
}

func TestHandleStreamDebugNotFound(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.APIHandler()

	req := httptest.NewRequest("GET", "/api/streams/nonexistent/debug", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleSRTPullCreateMissingFields(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	srv.config.SRTPull = func(_, _, _ string) error { return nil }
	handler := srv.APIHandler()

	req := httptest.NewRequest("POST", "/api/srt-pull", strings.NewReader(`{"address":""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSRTPullNotConfigured(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	// SRTPull is nil.
	handler := srv.APIHandler()

	req := httptest.NewRequest("POST", "/api/srt-pull", strings.NewReader(`{"address":"srt://host:6000","streamKey":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSRTPullStopMissingKey(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	srv.config.SRTStop = func(_ string) error { return nil }
	handler := srv.APIHandler()

	req := httptest.NewRequest("DELETE", "/api/srt-pull", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCORSHeaders(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.APIHandler()

	req := httptest.NewRequest("GET", "/api/streams", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cors := rec.Header().Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Fatalf("CORS header = %q, want %q", cors, "*")
	}

	coop := rec.Header().Get("Cross-Origin-Opener-Policy")
	if coop != "same-origin" {
		t.Fatalf("COOP header = %q, want %q", coop, "same-origin")
	}
}

func TestErrorResponsesAreJSON(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.APIHandler()

	req := httptest.NewRequest("POST", "/api/srt-pull", strings.NewReader(`{"address":"srt://host:6000","streamKey":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", ct, "application/json")
	}

	var errResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp["error"] == "" {
		t.Fatal("expected non-empty error field")
	}
}

func TestNewServerValidation(t *testing.T) {
	t.Parallel()

	cert, err := certs.Generate(24 * 60 * 60 * 1e9)
	if err != nil {
		t.Fatalf("certs.Generate: %v", err)
	}

	t.Run("missing cert", func(t *testing.T) {
		t.Parallel()
		_, err := NewServer(ServerConfig{Addr: ":4443"})
		if err == nil {
			t.Fatal("expected error for missing cert")
		}
	})

	t.Run("missing addr", func(t *testing.T) {
		t.Parallel()
		_, err := NewServer(ServerConfig{Cert: cert})
		if err == nil {
			t.Fatal("expected error for missing addr")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(ServerConfig{Addr: ":4443", Cert: cert})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if srv == nil {
			t.Fatal("server is nil")
		}
	})
}
