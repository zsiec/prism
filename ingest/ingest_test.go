package ingest

import (
	"io"
	"sync"
	"testing"
	"time"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	stream, w := r.Register("test-stream", FormatMPEGTS)

	if stream.Key != "test-stream" {
		t.Fatalf("got key %q, want %q", stream.Key, "test-stream")
	}
	if stream.Format != FormatMPEGTS {
		t.Fatalf("got format %d, want %d", stream.Format, FormatMPEGTS)
	}
	if w == nil {
		t.Fatal("writer is nil")
	}

	got, ok := r.Get("test-stream")
	if !ok {
		t.Fatal("Get returned false for registered stream")
	}
	if got != stream {
		t.Fatal("Get returned different stream pointer")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("Get returned true for missing stream")
	}
}

func TestRegistryUnregister(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	r.Register("stream1", FormatMPEGTS)

	r.Unregister("stream1")

	_, ok := r.Get("stream1")
	if ok {
		t.Fatal("stream still found after Unregister")
	}
}

func TestRegistryUnregisterMissing(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	// Should not panic.
	r.Unregister("nonexistent")
}

func TestRegistryUnregisterClosesPipe(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	stream, _ := r.Register("stream1", FormatMPEGTS)
	r.Unregister("stream1")

	// Reading from the input side should return EOF after pipe is closed.
	buf := make([]byte, 1)
	_, err := stream.input.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF after Unregister, got %v", err)
	}
}

func TestRegistryOnStreamCallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calledKey string
	var calledFormat InputFormat

	done := make(chan struct{})
	r := NewRegistry(func(key string, _ io.Reader, format InputFormat) {
		mu.Lock()
		calledKey = key
		calledFormat = format
		mu.Unlock()
		close(done)
	})

	r.Register("cb-stream", FormatMPEGTS)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onStream callback not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if calledKey != "cb-stream" {
		t.Fatalf("callback got key %q, want %q", calledKey, "cb-stream")
	}
	if calledFormat != FormatMPEGTS {
		t.Fatalf("callback got format %d, want %d", calledFormat, FormatMPEGTS)
	}
}

func TestStreamRecordRead(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	stream, _ := r.Register("s1", FormatMPEGTS)

	stream.RecordRead(100)
	stream.RecordRead(200)

	stats := stream.IngestStats()
	if stats.BytesReceived != 300 {
		t.Fatalf("BytesReceived = %d, want 300", stats.BytesReceived)
	}
	if stats.ReadCount != 2 {
		t.Fatalf("ReadCount = %d, want 2", stats.ReadCount)
	}
}

func TestStreamSetRemoteAddr(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	stream, _ := r.Register("s1", FormatMPEGTS)

	stream.SetRemoteAddr("192.168.1.1:5000")

	stats := stream.IngestStats()
	if stats.RemoteAddr != "192.168.1.1:5000" {
		t.Fatalf("RemoteAddr = %q, want %q", stats.RemoteAddr, "192.168.1.1:5000")
	}
}

func TestStreamIngestStatsUptime(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	stream, _ := r.Register("s1", FormatMPEGTS)

	// Sleep briefly to ensure uptime is measurable.
	time.Sleep(10 * time.Millisecond)

	stats := stream.IngestStats()
	if stats.UptimeMs < 10 {
		t.Fatalf("UptimeMs = %d, expected at least 10", stats.UptimeMs)
	}
	if stats.ConnectedAt == 0 {
		t.Fatal("ConnectedAt is zero")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := NewRegistry(nil)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "stream-" + string(rune('A'+n%26))
			r.Register(key, FormatMPEGTS)
			r.Get(key)
			r.Unregister(key)
		}(i)
	}

	wg.Wait()
}
