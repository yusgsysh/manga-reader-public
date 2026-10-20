package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestFSCache(t *testing.T) *FSCache {
	t.Helper()
	return newTestFSCacheLimit(t, 0)
}

func newTestFSCacheLimit(t *testing.T, maxBytes int64) *FSCache {
	t.Helper()
	c, err := NewFSCache(t.TempDir(), maxBytes)
	if err != nil {
		t.Fatalf("NewFSCache: %v", err)
	}
	return c
}

func TestFSCachePutGetRoundTrip(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	if err := c.Put(ctx, "images/abc", []byte("hello"), "text/plain", "max-age=1"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	data, contentType, err := c.Get(ctx, "images/abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("Get data = %q, want %q", data, "hello")
	}
	if contentType != "text/plain" {
		t.Fatalf("Get contentType = %q, want text/plain", contentType)
	}
}

func TestFSCacheMissingSatisfiesIsNotFound(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	_, _, err := c.Get(ctx, "images/missing")
	if !IsNotFound(err) {
		t.Fatalf("Get missing err = %v, want IsNotFound", err)
	}

	rc, _, _, err := c.GetStream(ctx, "images/missing")
	if rc != nil {
		_ = rc.Close()
	}
	if !IsNotFound(err) {
		t.Fatalf("GetStream missing err = %v, want IsNotFound", err)
	}

	if _, err := c.Head(ctx, "images/missing"); !IsNotFound(err) {
		t.Fatalf("Head missing err = %v, want IsNotFound", err)
	}
}

func TestFSCacheGetStream(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()
	payload := []byte("streamed-bytes")

	if err := c.Put(ctx, "sprite/xyz", payload, "image/webp", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, contentType, size, err := c.GetStream(ctx, "sprite/xyz")
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	defer rc.Close()

	if contentType != "image/webp" {
		t.Fatalf("contentType = %q, want image/webp", contentType)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("streamed = %q, want %q", got, payload)
	}
}

func TestFSCacheHeadCarriesMetadata(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	err := c.PutWithMeta(ctx, "crop/1", []byte("crop"), "image/webp", map[string]string{
		"source": "sprite",
		"x":      "10",
	}, "max-age=2")
	if err != nil {
		t.Fatalf("PutWithMeta: %v", err)
	}

	info, err := c.Head(ctx, "crop/1")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if info.Key != "crop/1" {
		t.Fatalf("Key = %q, want crop/1", info.Key)
	}
	if info.Size != 4 {
		t.Fatalf("Size = %d, want 4", info.Size)
	}
	if info.ContentType != "image/webp" {
		t.Fatalf("ContentType = %q, want image/webp", info.ContentType)
	}
	if info.UserMetadata["source"] != "sprite" || info.UserMetadata["x"] != "10" {
		t.Fatalf("UserMetadata = %v, want source/x", info.UserMetadata)
	}
}

func TestFSCacheOverwriteKeepsLatest(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	if err := c.Put(ctx, "images/dup", []byte("first"), "text/plain", ""); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := c.Put(ctx, "images/dup", []byte("second"), "text/plain", ""); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	data, _, err := c.Get(ctx, "images/dup")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("Get = %q, want second", data)
	}
}

func TestFSCacheDetectsContentTypeWithoutSidecar(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()
	// A real PNG signature so http.DetectContentType recognizes it.
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}

	if err := c.Put(ctx, "images/png", png, "", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Simulate an entry written before sidecars existed.
	dataPath, metaPath := c.objectPath("images/png")
	if err := os.Remove(metaPath); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("data file missing: %v", err)
	}

	_, contentType, err := c.Get(ctx, "images/png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("detected contentType = %q, want image/png", contentType)
	}
}

func TestFSCacheKeyCannotEscapeRoot(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	if err := c.Put(ctx, "../../etc/passwd", []byte("nope"), "text/plain", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	dataPath, _ := c.objectPath("../../etc/passwd")
	if !strings.HasPrefix(dataPath, c.Root()+string(filepath.Separator)) {
		t.Fatalf("object path %q escaped root %q", dataPath, c.Root())
	}

	// Nothing was written outside the cache root.
	if _, err := os.Stat(filepath.Join(c.Root(), "..", "..", "etc", "passwd")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected file outside root: %v", err)
	}
}

func TestFSCacheClear(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	if err := c.Put(ctx, "images/a", []byte("a"), "text/plain", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, _, err := c.Get(ctx, "images/a"); !IsNotFound(err) {
		t.Fatalf("Get after Clear err = %v, want IsNotFound", err)
	}
}

func TestFSCacheSize(t *testing.T) {
	c := newTestFSCache(t)
	ctx := context.Background()

	if size, err := c.Size(); err != nil || size != 0 {
		t.Fatalf("empty Size = %d, %v; want 0, nil", size, err)
	}

	if err := c.Put(ctx, "images/a", []byte("1234"), "text/plain", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := c.Put(ctx, "images/b", []byte("567890"), "text/plain", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	// The two data files contribute 10 bytes; sidecars add a few more, so
	// assert a range instead of an exact value.
	if size < 10 {
		t.Fatalf("Size = %d, want >= 10", size)
	}
}

func TestNewFSCacheRequiresRoot(t *testing.T) {
	if _, err := NewFSCache("", 0); err == nil {
		t.Fatal("NewFSCache(\"\") = nil error, want error")
	}
}

// writeObjectAt writes a 1000-byte object and backdates its files so prune
// ordering is deterministic.
func writeObjectAt(t *testing.T, c *FSCache, key string, modTime time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := c.Put(ctx, key, bytes.Repeat([]byte("x"), 1000), "text/plain", ""); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
	dataPath, metaPath := c.objectPath(key)
	for _, p := range []string{dataPath, metaPath} {
		if err := os.Chtimes(p, modTime, modTime); err != nil {
			t.Fatalf("Chtimes %s: %v", p, err)
		}
	}
}

func TestFSCachePruneEvictsOldest(t *testing.T) {
	// Three 1000-byte objects plus sidecars; a 1500-byte limit leaves room
	// for one object plus part of the second, so the two oldest go.
	//
	// The limit goes on after the writes: Put prunes in the background once
	// the cache passes it, and that prune would evict the objects this test
	// counts before Prune here ever runs.
	c := newTestFSCacheLimit(t, 0)
	now := time.Now()
	writeObjectAt(t, c, "images/old", now.Add(-2*time.Hour))
	writeObjectAt(t, c, "images/mid", now.Add(-1*time.Hour))
	writeObjectAt(t, c, "images/new", now)

	// The limit goes on only now, so no background prune runs during the
	// writes.
	c.maxBytes.Store(1500)

	removed, err := c.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed <= 0 {
		t.Fatalf("Prune removed %d bytes, want > 0", removed)
	}

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size > 1500 {
		t.Fatalf("Size = %d, want <= 1500", size)
	}

	ctx := context.Background()
	if _, _, err := c.Get(ctx, "images/old"); !IsNotFound(err) {
		t.Fatalf("oldest Get err = %v, want IsNotFound", err)
	}
	if _, _, err := c.Get(ctx, "images/new"); err != nil {
		t.Fatalf("newest was evicted: %v", err)
	}
	// The evicted object's sidecar must go with its data file.
	dataPath, metaPath := c.objectPath("images/old")
	if _, err := os.Stat(dataPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old data file still present: %v", err)
	}
	if _, err := os.Stat(metaPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old sidecar still present: %v", err)
	}
}

func TestFSCachePutPrunesWhenOverLimit(t *testing.T) {
	c := newTestFSCacheLimit(t, 3000)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("images/%02d", i)
		if err := c.Put(ctx, key, bytes.Repeat([]byte{byte(i)}, 1000), "text/plain", ""); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
		// Distinct mtimes so the eviction order is well defined.
		time.Sleep(2 * time.Millisecond)
	}

	// Pruning happens in the background; wait for it to settle.
	deadline := time.Now().Add(5 * time.Second)
	var size int64
	for {
		var err error
		if size, err = c.Size(); err != nil {
			t.Fatalf("Size: %v", err)
		}
		if size <= 3000 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if size > 3000 {
		t.Fatalf("Size = %d, want <= 3000 after background prune", size)
	}

	// The most recently written object survives.
	if _, _, err := c.Get(ctx, "images/19"); err != nil {
		t.Fatalf("newest object evicted: %v", err)
	}
}

func TestFSCacheSetMaxBytesDisablesLimit(t *testing.T) {
	c := newTestFSCacheLimit(t, 100)
	if c.MaxBytes() != 100 {
		t.Fatalf("MaxBytes = %d, want 100", c.MaxBytes())
	}
	c.SetMaxBytes(0)
	if c.MaxBytes() != 0 {
		t.Fatalf("MaxBytes after disable = %d, want 0", c.MaxBytes())
	}
	// A negative limit is clamped to unlimited.
	c.SetMaxBytes(-5)
	if c.MaxBytes() != 0 {
		t.Fatalf("MaxBytes after negative = %d, want 0", c.MaxBytes())
	}
}
