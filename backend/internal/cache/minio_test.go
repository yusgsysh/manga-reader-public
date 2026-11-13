package cache

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeS3 is the smallest object-store subset the MinIO client needs for the
// cache operations (bucket check/create, PUT, GET, HEAD). Signatures are
// ignored: the goal is to exercise cache business logic, not AWS auth.
type fakeS3 struct {
	mu          sync.Mutex
	bucket      string
	bucketReady bool
	objects     map[string]fakeObject
}

type fakeObject struct {
	data        []byte
	contentType string
	meta        map[string]string
}

func newFakeS3(bucket string) *fakeS3 {
	return &fakeS3{bucket: bucket, objects: map[string]fakeObject{}}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "minio/health/live" {
		w.WriteHeader(http.StatusOK)
		return
	}
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != f.bucket {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket")
		return
	}

	switch {
	case key == "" && r.Method == http.MethodHead:
		// BucketExists: false until the cache creates it.
		if !f.bucketReady {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket")
			return
		}
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodPut:
		f.bucketReady = true
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodGet:
		// Bucket location lookup.
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
	case key != "" && r.Method == http.MethodPut:
		var (
			data []byte
			err  error
		)
		if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
			data, err = decodeAWSChunked(r.Body)
		} else {
			data, err = io.ReadAll(r.Body)
		}
		if err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError")
			return
		}
		meta := map[string]string{}
		for name, values := range r.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(name), "X-Amz-Meta-") {
				meta[strings.TrimPrefix(http.CanonicalHeaderKey(name), "X-Amz-Meta-")] = values[0]
			}
		}
		f.objects[key] = fakeObject{data: data, contentType: r.Header.Get("Content-Type"), meta: meta}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case key != "" && r.Method == http.MethodHead:
		obj, ok := f.objects[key]
		if !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(obj.data)))
		w.Header().Set("Content-Type", obj.contentType)
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		w.Header().Set("ETag", `"etag"`)
		for name, value := range obj.meta {
			w.Header().Set("X-Amz-Meta-"+name, value)
		}
		w.WriteHeader(http.StatusOK)
	case key != "" && r.Method == http.MethodGet:
		obj, ok := f.objects[key]
		if !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(obj.data)))
		w.Header().Set("Content-Type", obj.contentType)
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		w.Header().Set("ETag", `"etag"`)
		_, _ = w.Write(obj.data)
	case key != "" && r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest")
	}
}

// decodeAWSChunked unwraps the streaming SigV4 body MinIO uses for PutObject:
// <hex-size>;chunk-signature=... CRLF <bytes> CRLF ... 0;chunk-signature=... CRLF CRLF.
func decodeAWSChunked(r io.Reader) ([]byte, error) {
	var out bytes.Buffer
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeField, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(sizeField, 16, 64)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out.Bytes(), nil
		}
		if _, err := io.CopyN(&out, br, n); err != nil {
			return nil, err
		}
		if _, err := br.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header+`<Error><Code>`+code+`</Code><Message>`+code+`</Message></Error>`)
}

func newTestMinIOCache(t *testing.T) *MinIOCache {
	t.Helper()
	const bucket = "manga-test"
	fake := newFakeS3(bucket)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	c, err := NewMinIOCache(&MinIOConfig{
		Endpoint:  strings.TrimPrefix(srv.URL, "http://"),
		AccessKey: "test",
		SecretKey: "test-secret",
		Bucket:    bucket,
		UseSSL:    false,
	})
	if err != nil {
		t.Fatalf("NewMinIOCache: %v", err)
	}
	return c
}

func TestNewMinIOCacheRejectsIncompleteConfig(t *testing.T) {
	if _, err := NewMinIOCache(&MinIOConfig{Endpoint: "localhost:9000"}); err == nil {
		t.Fatal("incomplete config accepted")
	}
}

func TestMinIOCachePutGetHeadStream(t *testing.T) {
	c := newTestMinIOCache(t)
	ctx := t.Context()
	key := CacheKey("https://exhentai.org/s/a/1-1")
	payload := []byte("image-bytes")

	if _, _, err := c.Get(ctx, key); !IsNotFound(err) {
		t.Fatalf("get before put error = %v, want not-found", err)
	}

	if err := c.Put(ctx, key, payload, "image/webp", "private, max-age=60"); err != nil {
		t.Fatalf("put: %v", err)
	}
	data, contentType, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(data, payload) || contentType != "image/webp" {
		t.Fatalf("get = %q/%q", data, contentType)
	}

	rc, contentType, size, err := c.GetStream(ctx, key)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	streamed, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("stream read/close: %v/%v", readErr, closeErr)
	}
	if !bytes.Equal(streamed, payload) || contentType != "image/webp" || size != int64(len(payload)) {
		t.Fatalf("stream = %q/%q/%d", streamed, contentType, size)
	}

	info, err := c.Head(ctx, key)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if info.Size != int64(len(payload)) || info.ContentType != "image/webp" {
		t.Fatalf("head = %+v", info)
	}

	// Overwriting is a normal cache refresh, not an error.
	if err := c.PutWithMeta(ctx, key, []byte("new"), "image/png", map[string]string{"source": "test"}, ""); err != nil {
		t.Fatalf("put with meta: %v", err)
	}
	updated, contentType, err := c.Get(ctx, key)
	if err != nil || string(updated) != "new" || contentType != "image/png" {
		t.Fatalf("updated get = %q/%q err %v", updated, contentType, err)
	}
}

func TestMinIOCacheMissIsNotFound(t *testing.T) {
	c := newTestMinIOCache(t)
	_, _, err := c.Get(t.Context(), "images/missing")
	if !IsNotFound(err) {
		t.Fatalf("missing object error = %v, want IsNotFound", err)
	}
}
