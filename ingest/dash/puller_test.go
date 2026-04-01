package dash

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zsiec/prism/distribution"
)

func TestNewPuller(t *testing.T) {
	t.Parallel()

	t.Run("with logger", func(t *testing.T) {
		log := slog.Default()
		p := NewPuller(log)
		if p == nil {
			t.Fatal("NewPuller returned nil")
		}
		if p.pulls == nil {
			t.Error("pulls map is nil")
		}
		if p.client == nil {
			t.Error("client is nil")
		}
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		p := NewPuller(nil)
		if p == nil {
			t.Fatal("NewPuller returned nil")
		}
		if p.log == nil {
			t.Error("log is nil")
		}
	})
}

func TestPullerValidation(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())

	t.Run("empty URL", func(t *testing.T) {
		err := p.Pull(context.Background(), PullRequest{
			StreamKey: "test",
		}, nil, nil)
		if err == nil {
			t.Error("expected error for empty URL")
		}
	})

	t.Run("empty StreamKey", func(t *testing.T) {
		err := p.Pull(context.Background(), PullRequest{
			URL: "http://example.com/manifest.mpd",
		}, nil, nil)
		if err == nil {
			t.Error("expected error for empty StreamKey")
		}
	})
}

func TestPullerDuplicateStreamKey(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())

	// Insert a fake active pull directly into the map.
	p.mu.Lock()
	p.pulls["test-stream"] = &activePull{
		req:    PullRequest{URL: "http://example.com/dash.mpd", StreamKey: "test-stream"},
		cancel: func() {},
	}
	p.mu.Unlock()

	err := p.Pull(context.Background(), PullRequest{
		URL:       "http://example.com/dash.mpd",
		StreamKey: "test-stream",
	}, nil, nil)
	if err == nil {
		t.Error("expected error for duplicate stream key")
	}
}

func TestPullerStopNonexistent(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())
	err := p.Stop("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent stream key")
	}
}

func TestPullerActivePulls(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())

	// Empty initially.
	pulls := p.ActivePulls()
	if len(pulls) != 0 {
		t.Errorf("expected 0 active pulls, got %d", len(pulls))
	}

	// Insert fake pulls.
	p.mu.Lock()
	p.pulls["stream-a"] = &activePull{
		req:    PullRequest{URL: "http://a.com/dash.mpd", StreamKey: "stream-a"},
		cancel: func() {},
	}
	p.pulls["stream-b"] = &activePull{
		req:    PullRequest{URL: "http://b.com/dash.mpd", StreamKey: "stream-b"},
		cancel: func() {},
	}
	p.mu.Unlock()

	pulls = p.ActivePulls()
	if len(pulls) != 2 {
		t.Errorf("expected 2 active pulls, got %d", len(pulls))
	}

	// Verify both keys are present.
	keys := map[string]bool{}
	for _, pr := range pulls {
		keys[pr.StreamKey] = true
	}
	if !keys["stream-a"] || !keys["stream-b"] {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestPullerStop(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())

	cancelled := false
	p.mu.Lock()
	p.pulls["test-stream"] = &activePull{
		req:    PullRequest{URL: "http://example.com/dash.mpd", StreamKey: "test-stream"},
		cancel: func() { cancelled = true },
	}
	p.mu.Unlock()

	err := p.Stop("test-stream")
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if !cancelled {
		t.Error("cancel function was not called")
	}
}

func TestDeriveBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mpdURL     string
		mpdBaseURL string
		want       string
	}{
		{
			name:       "MPD has BaseURL",
			mpdURL:     "http://cdn.example.com/live/manifest.mpd",
			mpdBaseURL: "http://cdn.example.com/live/",
			want:       "http://cdn.example.com/live/",
		},
		{
			name:       "derive from MPD URL",
			mpdURL:     "http://cdn.example.com/live/manifest.mpd",
			mpdBaseURL: "",
			want:       "http://cdn.example.com/live/",
		},
		{
			name:       "MPD URL with deeper path",
			mpdURL:     "http://cdn.example.com/a/b/c/dash.mpd",
			mpdBaseURL: "",
			want:       "http://cdn.example.com/a/b/c/",
		},
		{
			name:       "MPD URL at root",
			mpdURL:     "http://cdn.example.com/manifest.mpd",
			mpdBaseURL: "",
			want:       "http://cdn.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveBaseURL(tt.mpdURL, tt.mpdBaseURL)
			if got != tt.want {
				t.Errorf("deriveBaseURL(%q, %q) = %q, want %q", tt.mpdURL, tt.mpdBaseURL, got, tt.want)
			}
		})
	}
}

func TestPullerFetchURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("hello"))
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := NewPuller(slog.Default())

	t.Run("success", func(t *testing.T) {
		data, err := p.fetchURL(context.Background(), srv.URL+"/ok")
		if err != nil {
			t.Fatalf("fetchURL: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("body = %q, want %q", string(data), "hello")
		}
	})

	t.Run("server error", func(t *testing.T) {
		_, err := p.fetchURL(context.Background(), srv.URL+"/error")
		if err == nil {
			t.Error("expected error for 500 status")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := p.fetchURL(ctx, srv.URL+"/ok")
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

// mockDistributionServer implements DistributionServer for testing.
type mockDistributionServer struct {
	registered   map[string]*distribution.Relay
	unregistered []string
	pipelines    map[string]distribution.StatsProvider
}

func newMockDistributionServer() *mockDistributionServer {
	return &mockDistributionServer{
		registered: make(map[string]*distribution.Relay),
		pipelines:  make(map[string]distribution.StatsProvider),
	}
}

func (m *mockDistributionServer) RegisterStream(key string) *distribution.Relay {
	r := distribution.NewRelay()
	m.registered[key] = r
	return r
}

func (m *mockDistributionServer) UnregisterStream(key string) {
	m.unregistered = append(m.unregistered, key)
	delete(m.registered, key)
}

func (m *mockDistributionServer) SetPipeline(key string, p distribution.StatsProvider) {
	m.pipelines[key] = p
}

// mockStreamManager implements StreamManager for testing.
type mockStreamManager struct {
	created []string
	removed []string
}

func (m *mockStreamManager) Create(key string) (interface{}, bool) {
	m.created = append(m.created, key)
	return nil, true
}

func (m *mockStreamManager) Remove(key string) {
	m.removed = append(m.removed, key)
}

func TestPullerPullStaticMPDRejected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testMPDStatic))
	}))
	defer srv.Close()

	p := NewPuller(slog.Default())
	distSrv := newMockDistributionServer()

	err := p.Pull(context.Background(), PullRequest{
		URL:       srv.URL + "/manifest.mpd",
		StreamKey: "test-static",
	}, distSrv, nil)

	if err == nil {
		t.Fatal("expected error for static MPD")
	}

	// No stream should have been registered.
	if len(distSrv.registered) != 0 {
		t.Errorf("expected 0 registered streams, got %d", len(distSrv.registered))
	}
}

func TestPullerPullBadURL(t *testing.T) {
	t.Parallel()

	p := NewPuller(slog.Default())
	distSrv := newMockDistributionServer()

	err := p.Pull(context.Background(), PullRequest{
		URL:       "http://localhost:1/nonexistent.mpd",
		StreamKey: "test-bad",
	}, distSrv, nil)

	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestPullerPullWithHTTP(t *testing.T) {
	t.Parallel()

	// Serve a dynamic MPD. The pull loop will try to fetch init segments,
	// which will fail, causing the loop to exit. We verify the lifecycle:
	// stream registered, then cleaned up after the loop exits.
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/manifest.mpd":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(testMPDDynamic))
			requestCount++
		default:
			// Init/media segments: return 404 so the loop exits.
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := NewPuller(slog.Default())
	distSrv := newMockDistributionServer()
	mgr := &mockStreamManager{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := p.Pull(ctx, PullRequest{
		URL:       srv.URL + "/manifest.mpd",
		StreamKey: "test-live",
	}, distSrv, mgr)

	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}

	// The pull should be registered immediately.
	pulls := p.ActivePulls()
	if len(pulls) != 1 {
		t.Fatalf("expected 1 active pull, got %d", len(pulls))
	}
	if pulls[0].StreamKey != "test-live" {
		t.Errorf("stream key = %q, want %q", pulls[0].StreamKey, "test-live")
	}

	// The background goroutine will fail on init segment fetch (404) and
	// clean up. Wait briefly for that.
	time.Sleep(500 * time.Millisecond)

	// After loop exits, pull should be removed from active list.
	pulls = p.ActivePulls()
	if len(pulls) != 0 {
		t.Errorf("expected 0 active pulls after loop exit, got %d", len(pulls))
	}

	// Distribution server should have been unregistered.
	if len(distSrv.unregistered) == 0 {
		t.Error("expected stream to be unregistered after loop exit")
	}

	// Stream manager should have been cleaned up.
	if len(mgr.removed) == 0 {
		t.Error("expected stream manager Remove to be called")
	}
}

func TestPullerPullAndStop(t *testing.T) {
	t.Parallel()

	// Serve a dynamic MPD and valid-ish init segments that won't parse
	// as fMP4 — the loop will exit on parse error. Instead, we'll use
	// a server that delays responses so we can test Stop().
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/manifest.mpd":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(testMPDDynamic))
		default:
			// Block until the request context is cancelled (simulating
			// a slow CDN so we can test Stop cancellation).
			<-r.Context().Done()
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	p := NewPuller(slog.Default())
	distSrv := newMockDistributionServer()

	ctx := context.Background()

	err := p.Pull(ctx, PullRequest{
		URL:       srv.URL + "/manifest.mpd",
		StreamKey: "stop-test",
	}, distSrv, nil)
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}

	// Verify active.
	if len(p.ActivePulls()) != 1 {
		t.Fatalf("expected 1 active pull, got %d", len(p.ActivePulls()))
	}

	// Stop the pull.
	err = p.Stop("stop-test")
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	// Wait for cleanup.
	time.Sleep(500 * time.Millisecond)

	if len(p.ActivePulls()) != 0 {
		t.Errorf("expected 0 active pulls after Stop, got %d", len(p.ActivePulls()))
	}
}
