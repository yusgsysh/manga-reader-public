package storage

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is the smallest object-store subset the MinIO client needs for the
// storage operations: bucket check/create, single PUT, multipart upload
// (initiate / part / complete / abort), GET, HEAD and DELETE. Signatures are
// ignored — the goal is to exercise storage business logic, not AWS auth.
type fakeS3 struct {
	mu          sync.Mutex
	bucket      string
	bucketReady bool
	objects     map[string]fakeObject
	uploads     map[string]*fakeUpload
	// aborted records upload IDs that were aborted, to assert cleanup.
	aborted []string
	seq     int
}

type fakeUpload struct {
	parts       map[int][]byte
	contentType string
}

type fakeObject struct {
	data        []byte
	contentType string
}

func newFakeS3(bucket string) *fakeS3 {
	return &fakeS3{
		bucket:  bucket,
		objects: map[string]fakeObject{},
		uploads: map[string]*fakeUpload{},
	}
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
	q := r.URL.Query()
	uploadID := q.Get("uploadId")

	switch {
	case key == "" && r.Method == http.MethodHead:
		if !f.bucketReady {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket")
			return
		}
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodPut:
		f.bucketReady = true
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodGet:
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
	case uploadID != "" && r.Method == http.MethodPost && q.Get("uploads") == "":
		// Complete multipart upload.
		up := f.uploads[uploadID]
		var out bytes.Buffer
		for i := 1; ; i++ {
			p, ok := up.parts[i]
			if !ok {
				break
			}
			out.Write(p)
		}
		delete(f.uploads, uploadID)
		f.objects[key] = fakeObject{data: out.Bytes(), contentType: up.contentType}
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>"etag-%s"</ETag></CompleteMultipartUploadResult>`, bucket, key, uploadID)
	case uploadID != "" && r.Method == http.MethodDelete:
		f.aborted = append(f.aborted, uploadID)
		delete(f.uploads, uploadID)
		w.WriteHeader(http.StatusNoContent)
	case key != "" && r.Method == http.MethodPost:
		// Initiate multipart upload.
		f.seq++
		id := fmt.Sprintf("upload-%d", f.seq)
		f.uploads[id] = &fakeUpload{parts: map[int][]byte{}, contentType: r.Header.Get("Content-Type")}
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, bucket, key, id)
	case key != "" && r.Method == http.MethodPut && uploadID != "":
		// Upload part.
		partNum, _ := strconv.Atoi(q.Get("partNumber"))
		data, err := readBody(r)
		if err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError")
			return
		}
		f.uploads[uploadID].parts[partNum] = data
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%s-%d"`, uploadID, partNum))
		w.WriteHeader(http.StatusOK)
	case key != "" && r.Method == http.MethodPut:
		data, err := readBody(r)
		if err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError")
			return
		}
		f.objects[key] = fakeObject{data: data, contentType: r.Header.Get("Content-Type")}
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

func readBody(r *http.Request) ([]byte, error) {
	if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
		return decodeAWSChunked(r.Body)
	}
	return io.ReadAll(r.Body)
}

// decodeAWSChunked unwraps the streaming SigV4 body MinIO uses for streaming
// uploads: <hex-size>;chunk-signature=... CRLF <bytes> CRLF ... 0;... CRLF CRLF.
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

func newTestStorage(t *testing.T, bucket string) (*Minio, *fakeS3) {
	t.Helper()
	fake := newFakeS3(bucket)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	s, err := New(t.Context(), Config{
		Endpoint:  strings.TrimPrefix(srv.URL, "http://"),
		AccessKey: "test",
		SecretKey: "test-secret",
		Bucket:    bucket,
		UseSSL:    false,
		Prefix:    "archives/",
		PartSize:  MinimumPartSize,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, fake
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	if _, err := New(t.Context(), Config{Endpoint: "localhost:9000"}); err == nil {
		t.Fatal("incomplete config accepted")
	}
}

func TestMinioUploadUnknownSizeIsMultipart(t *testing.T) {
	s, _ := newTestStorage(t, "manga-archives")

	payload := bytes.Repeat([]byte("hello world "), 100000) // > 5 MiB, forces multiple parts
	info, err := s.Upload(t.Context(), "user/job.zip", bytes.NewReader(payload), -1, "application/zip")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", info.Size, len(payload))
	}
	if info.ETag == "" {
		t.Fatal("etag empty")
	}

	rc, ct, size, err := s.GetStream(t.Context(), "user/job.zip")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(got), len(payload))
	}
	if ct != "application/zip" || size != int64(len(payload)) {
		t.Fatalf("stream meta = %q/%d", ct, size)
	}
}

func TestMinioUploadKnownSizeSinglePut(t *testing.T) {
	s, fake := newTestStorage(t, "manga-archives")

	payload := []byte("small zip")
	if _, err := s.Upload(t.Context(), "a/b.zip", bytes.NewReader(payload), int64(len(payload)), "application/zip"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := fake.objects["archives/a/b.zip"].data; !bytes.Equal(got, payload) {
		t.Fatalf("stored = %q, want %q", got, payload)
	}
}

func TestMinioHeadGetDeleteNotFound(t *testing.T) {
	s, _ := newTestStorage(t, "manga-archives")

	if _, err := s.Head(t.Context(), "missing.zip"); !IsNotFound(err) {
		t.Fatalf("head missing = %v, want not-found", err)
	}
	if _, err := s.Get(t.Context(), "missing.zip"); !IsNotFound(err) {
		t.Fatalf("get missing = %v, want not-found", err)
	}

	if _, err := s.Upload(t.Context(), "x.zip", strings.NewReader("data"), -1, ""); err != nil {
		t.Fatalf("upload: %v", err)
	}
	info, err := s.Head(t.Context(), "x.zip")
	if err != nil || info.Size != 4 {
		t.Fatalf("head = %+v, %v", info, err)
	}
	if err := s.Delete(t.Context(), "x.zip"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Head(t.Context(), "x.zip"); !IsNotFound(err) {
		t.Fatalf("head after delete = %v, want not-found", err)
	}
	// Deleting a missing object is not an error.
	if err := s.Delete(t.Context(), "x.zip"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestMinioPresignGet(t *testing.T) {
	s, _ := newTestStorage(t, "manga-archives")

	loc, err := s.PresignGet(t.Context(), "user/job.zip", 15*time.Minute, map[string]string{
		"response-content-disposition": `attachment; filename="a.zip"`,
	})
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if !strings.Contains(u.Path, "archives/user/job.zip") {
		t.Fatalf("signed path = %q, want prefixed key", u.Path)
	}
	q := u.Query()
	if !strings.Contains(q.Get("response-content-disposition"), `filename="a.zip"`) {
		t.Fatalf("response-content-disposition = %q", q.Get("response-content-disposition"))
	}
	if q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-Expires") == "" {
		t.Fatalf("signed query missing signature/expiry: %v", q)
	}
}
