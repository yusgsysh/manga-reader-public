package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestFSCache(t *testing.T) *FSCache {
	t.Helper()
	c, err := NewFSCache(t.TempDir())
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

func TestNewFSCacheRequiresRoot(t *testing.T) {
	if _, err := NewFSCache(""); err == nil {
		t.Fatal("NewFSCache(\"\") = nil error, want error")
	}
}
