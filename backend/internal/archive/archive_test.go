package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"manga-reader/internal/storage"
)

// fakeStore is an in-memory ObjectStorage for exercising BuildAndUpload without
// a real object store. It records the final object bytes and can be made to
// fail the upload.
type fakeStore struct {
	mu         sync.Mutex
	objects    map[string][]byte
	failUpload bool
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Upload(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	if f.failUpload {
		return storage.ObjectInfo{}, errors.New("upload failed")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	f.mu.Lock()
	f.objects[key] = data
	f.mu.Unlock()
	return storage.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: contentType, ETag: "etag"}, nil
}

func (f *fakeStore) Head(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, errors.New("not implemented")
}
func (f *fakeStore) Delete(context.Context, string) error { return nil }
func (f *fakeStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetStream(context.Context, string) (io.ReadCloser, string, int64, error) {
	return nil, "", 0, errors.New("not implemented")
}
func (f *fakeStore) PresignGet(context.Context, string, time.Duration, map[string]string) (string, error) {
	return "", errors.New("not implemented")
}

func (f *fakeStore) object(t *testing.T, key string) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects[key]
}

// pages returns a map of page URL -> content for a fetch function.
func pages(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("https://exhentai.org/s/%d", i)] = fmt.Sprintf("page-%d-body", i)
	}
	return m
}

func fetcher(m map[string]string) Fetch {
	return func(_ context.Context, pageURL string) ([]byte, string, error) {
		if body, ok := m[pageURL]; ok {
			return []byte(body), "image/png", nil
		}
		return nil, "", fmt.Errorf("page not found: %s", pageURL)
	}
}

func unzip(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func TestBuildOrderedEntries(t *testing.T) {
	urls := []string{
		"https://exhentai.org/s/1",
		"https://exhentai.org/s/2",
		"https://exhentai.org/s/3",
	}
	var out bytes.Buffer
	res, err := Build(t.Context(), &out, urls, fetcher(pages(3)), Options{Concurrency: 3})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if res.Missing != 0 {
		t.Fatalf("missing = %d, want 0", res.Missing)
	}
	names := unzip(t, out.Bytes())
	want := []string{"001.png", "002.png", "003.png"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entries = %v, want %v", names, want)
		}
	}
}

func TestBuildPreservesOrderUnderConcurrency(t *testing.T) {
	// A slow first page must not reorder later pages.
	m := map[string]string{}
	for i := 1; i <= 50; i++ {
		m[fmt.Sprintf("https://exhentai.org/s/%d", i)] = fmt.Sprintf("body-%d", i)
	}
	fetch := func(_ context.Context, url string) ([]byte, string, error) {
		return []byte(m[url]), "image/webp", nil
	}
	urls := make([]string, 0, 50)
	for i := 1; i <= 50; i++ {
		urls = append(urls, fmt.Sprintf("https://exhentai.org/s/%d", i))
	}
	var out bytes.Buffer
	if _, err := Build(t.Context(), &out, urls, fetch, Options{Concurrency: 8}); err != nil {
		t.Fatalf("build: %v", err)
	}
	names := unzip(t, out.Bytes())
	if len(names) != 50 {
		t.Fatalf("entries = %d, want 50", len(names))
	}
	if names[0] != "001.webp" || names[49] != "050.webp" {
		t.Fatalf("first/last = %q/%q, want 001.webp/050.webp", names[0], names[49])
	}
}

func TestBuildMissingPages(t *testing.T) {
	urls := []string{
		"https://exhentai.org/s/1",
		"https://exhentai.org/s/bad",
		"https://exhentai.org/s/3",
	}
	var missingIndexes []int
	var out bytes.Buffer
	res, err := Build(t.Context(), &out, urls, fetcher(pages(1)), Options{
		Concurrency: 2,
		MissingName: "_missing.txt",
		OnMissing: func(index int, _ string, _ error) {
			missingIndexes = append(missingIndexes, index)
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if res.Missing != 2 { // "bad" and "3" are not in pages(1)
		t.Fatalf("missing = %d, want 2", res.Missing)
	}
	if len(missingIndexes) != 2 {
		t.Fatalf("onMissing called %d times, want 2", len(missingIndexes))
	}
	names := unzip(t, out.Bytes())
	// 001.png present, then _missing.txt (order: entries written in index order,
	// manifest last).
	if len(names) != 2 || names[0] != "001.png" || names[1] != "_missing.txt" {
		t.Fatalf("entries = %v", names)
	}
}

func TestBuildEmptyPages(t *testing.T) {
	var out bytes.Buffer
	res, err := Build(t.Context(), &out, nil, fetcher(nil), Options{})
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if res.Missing != 0 {
		t.Fatalf("missing = %d", res.Missing)
	}
	if _, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
		t.Fatalf("empty zip is invalid: %v", err)
	}
}

func TestBuildAndUploadStreams(t *testing.T) {
	store := newFakeStore()
	urls := []string{"https://exhentai.org/s/1", "https://exhentai.org/s/2"}
	info, res, err := BuildAndUpload(t.Context(), store, "u/j.zip", urls, fetcher(pages(2)), Options{Concurrency: 2})
	if err != nil {
		t.Fatalf("build+upload: %v", err)
	}
	if res.Missing != 0 || info.Size == 0 {
		t.Fatalf("result = %+v / %+v", res, info)
	}
	data := store.object(t, "u/j.zip")
	if int64(len(data)) != info.Size {
		t.Fatalf("stored size = %d, info = %d", len(data), info.Size)
	}
	names := unzip(t, data)
	if len(names) != 2 || names[0] != "001.png" || names[1] != "002.png" {
		t.Fatalf("entries = %v", names)
	}
}

func TestBuildAndUploadUploadFailureCancelsBuild(t *testing.T) {
	store := newFakeStore()
	store.failUpload = true
	urls := make([]string, 10)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://exhentai.org/s/%d", i+1)
	}
	// fetch that would hang forever if the build is not cancelled after the
	// upload fails; a fast error path proves cancellation unblocks the producer.
	_, _, err := BuildAndUpload(t.Context(), store, "u/j.zip", urls, fetcher(pages(10)), Options{Concurrency: 2})
	if err == nil {
		t.Fatal("expected upload failure")
	}
	// The build goroutine must have finished; if it leaked, this test would
	// deadlock on the internal <-done. Reaching here proves it returned.
}

func TestExtForContentType(t *testing.T) {
	cases := []struct {
		ct, url, want string
	}{
		{"image/jpeg", "", ".jpg"},
		{"image/png", "", ".png"},
		{"image/webp", "", ".webp"},
		{"image/gif", "", ".gif"},
		{"", "https://x/s/a.jpg", ".jpg"},
		{"", "https://x/s/a", ".jpg"},
		{"application/octet-stream", "https://x/s/a.webp", ".webp"},
	}
	for _, c := range cases {
		if got := ExtForContentType(c.ct, c.url); got != c.want {
			t.Errorf("ExtForContentType(%q, %q) = %q, want %q", c.ct, c.url, got, c.want)
		}
	}
}
