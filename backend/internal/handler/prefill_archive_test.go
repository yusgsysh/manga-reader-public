package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	json "encoding/json/v2"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/database"
	"manga-reader/internal/storage"
)

// mockArchiveStorage is an in-memory ObjectStorage for exercising the archive
// pipeline without a real object store.
type mockArchiveStorage struct {
	mu      sync.Mutex
	objects map[string]mockArchiveObject
}

type mockArchiveObject struct {
	data        []byte
	contentType string
}

func newMockArchiveStorage() *mockArchiveStorage {
	return &mockArchiveStorage{objects: map[string]mockArchiveObject{}}
}

func (m *mockArchiveStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	m.mu.Lock()
	m.objects[key] = mockArchiveObject{data: data, contentType: contentType}
	m.mu.Unlock()
	return storage.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: contentType, ETag: "etag"}, nil
}

func (m *mockArchiveStorage) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return storage.ObjectInfo{}, fmt.Errorf("missing %s", key)
	}
	return storage.ObjectInfo{Key: key, Size: int64(len(o.data)), ContentType: o.contentType, ETag: "etag"}, nil
}

func (m *mockArchiveStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *mockArchiveStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, _, _, err := m.GetStream(ctx, key)
	return rc, err
}

func (m *mockArchiveStorage) GetStream(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, "", 0, fmt.Errorf("missing %s", key)
	}
	return io.NopCloser(bytes.NewReader(o.data)), o.contentType, int64(len(o.data)), nil
}

func (m *mockArchiveStorage) PresignGet(_ context.Context, key string, _ time.Duration, _ map[string]string) (string, error) {
	return "https://signed.example/" + key, nil
}

// newArchiveTestServer builds a server wired with an archive store and an
// optional image cache. presign toggles the redirect-to-signed-URL download path.
func newArchiveTestServer(t *testing.T, up *prefillUpstream, cache ImageCache, archive storage.ObjectStorage, presign bool) (*gin.Engine, *Server) {
	t.Helper()
	client := newTestDB(t)
	upSrv := httptest.NewServer(up.handler())
	t.Cleanup(upSrv.Close)
	server := &Server{
		Client:               newMockClient(upSrv.URL),
		DB:                   &database.DB{Client: client},
		Cache:                cache,
		Archive:              archive,
		ArchiveConcurrency:   2,
		ArchivePresign:       presign,
		ArchivePresignExpiry: 15 * time.Minute,
	}
	return setupMockRouter(server), server
}

type archiveJobResp struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ArchiveReady bool   `json:"archive_ready"`
}

func TestPrefillArchive_StoresAndServes(t *testing.T) {
	up := &prefillUpstream{}
	imgCache := newMockImageCache()
	arch := newMockArchiveStorage()
	r, _ := newArchiveTestServer(t, up, imgCache, arch, false)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "archive-test",
		"urls":  pageURLs("a1", "a2", "a3"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created archiveJobResp
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	job := waitArchiveStatus(t, r, created.ID, 15*time.Second, prefillStatusCompleted)
	if !job.ArchiveReady {
		t.Fatal("archive_ready = false, want true after completion")
	}

	// Download: must stream from the archive (no new upstream fetches).
	before := up.count()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prefill/%s/zip", job.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("zip: status = %d, body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 3 {
		t.Fatalf("entries = %d, want 3", len(zr.File))
	}
	for i, want := range []string{"001.webp", "002.webp", "003.webp"} {
		if zr.File[i].Name != want {
			t.Errorf("entries[%d] = %q, want %q", i, zr.File[i].Name, want)
		}
	}
	if after := up.count(); after != before {
		t.Errorf("archive download hit upstream: %d -> %d requests", before, after)
	}
}

func TestPrefillArchive_ArchiveOnlyMode(t *testing.T) {
	up := &prefillUpstream{}
	arch := newMockArchiveStorage()
	r, _ := newArchiveTestServer(t, up, nil, arch, false)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "archive-only",
		"urls":  pageURLs("o1", "o2"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created archiveJobResp
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	job := waitArchiveStatus(t, r, created.ID, 15*time.Second, prefillStatusCompleted)
	if !job.ArchiveReady {
		t.Fatal("archive_ready = false in archive-only mode")
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prefill/%s/zip", job.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("zip: status = %d", w.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("entries = %d, want 2", len(zr.File))
	}
}

func TestPrefillZip_NoStorageConfigured(t *testing.T) {
	client := newTestDB(t)
	r := setupMockRouter(&Server{Client: &http.Client{}, DB: &database.DB{Client: client}})
	if code, _ := doJSON(t, r, http.MethodGet, "/api/prefill/1/zip", nil); code != http.StatusServiceUnavailable {
		t.Errorf("zip no storage: status = %d, want 503", code)
	}
	if code, _ := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{"urls": pageURLs("x1")}); code != http.StatusServiceUnavailable {
		t.Errorf("start no storage: status = %d, want 503", code)
	}
}

func TestPrefillArchive_PresignRedirect(t *testing.T) {
	up := &prefillUpstream{}
	imgCache := newMockImageCache()
	arch := newMockArchiveStorage()
	r, _ := newArchiveTestServer(t, up, imgCache, arch, true)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "presign",
		"urls":  pageURLs("p1", "p2"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created archiveJobResp
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	waitArchiveStatus(t, r, created.ID, 15*time.Second, prefillStatusCompleted)

	// With presign enabled the download is a redirect to a signed URL, and the
	// backend must not proxy the bytes itself.
	before := up.count()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prefill/%s/zip", created.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://signed.example/") || !strings.HasSuffix(loc, ".zip") {
		t.Fatalf("Location = %q, want signed .zip URL", loc)
	}
	if after := up.count(); after != before {
		t.Errorf("presign redirect hit upstream: %d -> %d", before, after)
	}
}

func waitArchiveStatus(t *testing.T, r *gin.Engine, id string, timeout time.Duration, want ...string) archiveJobResp {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last archiveJobResp
	for time.Now().Before(deadline) {
		code, body := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%s", id), nil)
		if code == http.StatusOK {
			var job archiveJobResp
			if json.Unmarshal(body, &job) == nil {
				last = job
				if slices.Contains(want, job.Status) {
					return job
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %v within %s (last status=%q)", id, want, timeout, last.Status)
	return last
}
