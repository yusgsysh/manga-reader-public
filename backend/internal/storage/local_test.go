package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"manga-reader/internal/config"
)

func newTestLocal(t *testing.T) *LocalStorage {
	t.Helper()
	s, err := NewLocalStorage(LocalConfig{Root: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	return s
}

func TestLocalStorage_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "images/abcdef"
	data := []byte("hello world")

	if ok, err := s.Exists(ctx, key); err != nil || ok {
		t.Fatalf("Exists before put = %v, %v; want false, nil", ok, err)
	}
	err := s.Put(ctx, key, data, "image/jpeg", PutOptions{
		CacheControl: "public, max-age=31536000, immutable",
		Meta:         map[string]string{"source": "unit-test"},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ct, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("Get data = %q, want %q", got, data)
	}
	if ct != "image/jpeg" {
		t.Fatalf("Get contentType = %q, want image/jpeg", ct)
	}
	if ok, err := s.Exists(ctx, key); err != nil || !ok {
		t.Fatalf("Exists after put = %v, %v; want true, nil", ok, err)
	}

	info, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.ContentType != "image/jpeg" || info.Size != int64(len(data)) {
		t.Fatalf("Stat = %+v", info)
	}
	if info.CacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("Stat CacheControl = %q", info.CacheControl)
	}
	if info.Meta["source"] != "unit-test" {
		t.Fatalf("Stat Meta = %v", info.Meta)
	}
}

func TestLocalStorage_FileLivesUnderRootAtTheKeyPath(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "page-sprite/abc123"
	if err := s.Put(ctx, key, []byte("x"), "image/webp", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := filepath.Join(s.Root(), filepath.FromSlash(key))
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("object file missing at %s: %v", want, err)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != "x" {
		t.Fatalf("object bytes = %q, want %q", raw, "x")
	}
}

func TestLocalStorage_GetMissingReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	_, _, err := s.Get(ctx, "images/missing")
	if !IsNotFound(err) {
		t.Fatalf("Get missing err = %v, want ErrNotFound", err)
	}
	if _, err := s.Stat(ctx, "images/missing"); !IsNotFound(err) {
		t.Fatalf("Stat missing err = %v, want ErrNotFound", err)
	}
}

func TestLocalStorage_DeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "images/deleteme"
	if err := s.Put(ctx, key, []byte("x"), "image/png", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete again: %v", err)
	}
	if ok, err := s.Exists(ctx, key); err != nil || ok {
		t.Fatalf("Exists after delete = %v, %v; want false, nil", ok, err)
	}
	_, _, err := s.Get(ctx, key)
	if !IsNotFound(err) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}

func TestLocalStorage_OverwriteKeepsKeysStable(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "images/abcdef"
	if err := s.Put(ctx, key, []byte("first"), "image/jpeg", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(ctx, key, []byte("second"), "image/webp", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ct, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second" || ct != "image/webp" {
		t.Fatalf("Get = %q/%q, want second/webp", got, ct)
	}
}

func TestLocalStorage_RejectsEscapingKeys(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	bad := []string{
		"",
		"../escape",
		"images/../../escape",
		"images/sub/../../../escape",
		"/etc/passwd",
		"images/a\\b",
		"images/./b",
		"images/a//b",
		".",
		"..",
		"images/\x00x",
	}
	for _, key := range bad {
		if _, _, err := s.Get(ctx, key); err == nil {
			t.Errorf("Get(%q) succeeded, want rejection", key)
		} else if IsNotFound(err) {
			t.Errorf("Get(%q) = ErrNotFound, want key rejection", key)
		}
		if err := s.Put(ctx, key, []byte("x"), "image/png", PutOptions{}); err == nil {
			t.Errorf("Put(%q) succeeded, want rejection", key)
		}
	}

	// Nothing must have escaped the cache root.
	root := s.Root()
	var found []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		found = append(found, rel)
		return nil
	})
	for _, rel := range found {
		if strings.Contains(rel, "escape") || strings.Contains(rel, "passwd") {
			t.Fatalf("cache key escaped the root: found %q", rel)
		}
	}
}

func TestLocalStorage_RejectsEmptyRoot(t *testing.T) {
	if _, err := NewLocalStorage(LocalConfig{Root: "  "}); err == nil {
		t.Fatal("NewLocalStorage with empty root succeeded, want error")
	}
}

func TestLocalStorage_ConcurrentPutsAndGets(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "images/concurrent"

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers*4)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := []byte(strings.Repeat(fmt.Sprintf("%d-", i), 64))
			for n := 0; n < 4; n++ {
				if err := s.Put(ctx, key, payload, "image/png", PutOptions{}); err != nil {
					errs <- err
				}
				if data, _, err := s.Get(ctx, key); err != nil {
					if !IsNotFound(err) {
						errs <- err
					}
				} else if len(data) == 0 {
					errs <- fmt.Errorf("read %d bytes", len(data))
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op: %v", err)
	}
}

func TestLocalStorage_CorruptMetadataStillServesBytes(t *testing.T) {
	ctx := context.Background()
	s := newTestLocal(t)
	key := "images/corrupt"
	if err := s.Put(ctx, key, []byte("payload"), "image/png", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	metaFile := filepath.Join(s.Root(), filepath.FromSlash(key)+".meta")
	if err := os.WriteFile(metaFile, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}
	data, ct, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "payload" || ct != "" {
		t.Fatalf("Get = %q/%q, want payload/empty", data, ct)
	}
}

func TestNew_FactorySelectsLocalDriver(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, config.StorageConfig{Driver: config.StorageDriverLocal, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New local: %v", err)
	}
	if _, ok := s.(*LocalStorage); !ok {
		t.Fatalf("New local returned %T, want *LocalStorage", s)
	}
}
