package cache

import (
	"testing"
	"time"
)

func TestGetSetRoundTrip(t *testing.T) {
	t.Parallel()
	c := New[string](time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache should miss")
	}
	c.Set("a", "value")
	got, ok := c.Get("a")
	if !ok || got != "value" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestEntryExpires(t *testing.T) {
	t.Parallel()
	c := New[int](time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }
	c.Set("k", 1)

	c.now = func() time.Time { return now.Add(59 * time.Second) }
	if _, ok := c.Get("k"); !ok {
		t.Fatal("should still be live just under the TTL")
	}

	c.now = func() time.Time { return now.Add(61 * time.Second) }
	if _, ok := c.Get("k"); ok {
		t.Fatal("should have expired past the TTL")
	}
	// The expired entry is deleted, not just hidden.
	c.mu.Lock()
	_, present := c.m["k"]
	c.mu.Unlock()
	if present {
		t.Fatal("expired entry should be removed from the map")
	}
}

func TestDeletePrefix(t *testing.T) {
	t.Parallel()
	c := New[int](time.Minute)
	c.Set("projects/p|gcs", 1)
	c.Set("projects/p|gke", 2)
	c.Set("projects/other|gcs", 3)

	c.DeletePrefix("projects/p")

	if _, ok := c.Get("projects/p|gcs"); ok {
		t.Fatal("projects/p|gcs should be gone")
	}
	if _, ok := c.Get("projects/p|gke"); ok {
		t.Fatal("projects/p|gke should be gone")
	}
	if _, ok := c.Get("projects/other|gcs"); !ok {
		t.Fatal("projects/other|gcs should survive an unrelated prefix delete")
	}
}
