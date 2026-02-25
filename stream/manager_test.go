package stream

import (
	"testing"
)

func TestManagerCreateAndGet(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	s, ok := m.Create("test-stream")
	if !ok {
		t.Fatal("Create returned not-ok for new stream")
	}
	if s == nil {
		t.Fatal("Create returned nil")
	}
	if s.Key != "test-stream" {
		t.Errorf("key: got %q, want %q", s.Key, "test-stream")
	}
	if s.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}

	streams := m.List()
	if len(streams) != 1 || streams[0].Key != "test-stream" {
		t.Error("List should return the created stream")
	}
}

func TestManagerCreateDuplicate(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	_, ok1 := m.Create("test")
	if !ok1 {
		t.Fatal("first Create should succeed")
	}
	s2, ok2 := m.Create("test")

	if ok2 {
		t.Error("duplicate Create should return false")
	}
	if s2 != nil {
		t.Error("duplicate Create should return nil stream")
	}
}

func TestManagerRemove(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	m.Create("test")
	if len(m.List()) != 1 {
		t.Errorf("count: got %d, want 1", len(m.List()))
	}

	m.Remove("test")
	if len(m.List()) != 0 {
		t.Errorf("count after remove: got %d, want 0", len(m.List()))
	}
}

func TestManagerList(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	m.Create("stream-a")
	m.Create("stream-b")
	m.Create("stream-c")

	streams := m.List()
	if len(streams) != 3 {
		t.Fatalf("expected 3 streams, got %d", len(streams))
	}

	keys := make(map[string]bool)
	for _, s := range streams {
		keys[s.Key] = true
	}

	for _, k := range []string{"stream-a", "stream-b", "stream-c"} {
		if !keys[k] {
			t.Errorf("missing stream %q", k)
		}
	}
}

func TestManagerRemoveNonexistent(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	// Should not panic
	m.Remove("nonexistent")
}
