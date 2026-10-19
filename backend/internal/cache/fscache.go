package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"
)

// FSCache is a filesystem-backed ImageCache for single-binary desktop builds.
// It replaces MinIO for local installs: objects live in a hashed directory
// tree under Root, each with a sidecar JSON file that carries the content
// type, cache-control header and user metadata MinIO stored as object
// metadata.
//
// Cache keys are already opaque digests (see CacheKey, ThumbnailCacheKey),
// but the cache never trusts a key as a path: it hashes the key again to
// derive the file name, so a hostile or malformed key cannot escape Root.
type FSCache struct {
	root string
}

// fsCacheMeta is the sidecar persisted next to every object.
type fsCacheMeta struct {
	ContentType  string            `json:"content_type,omitempty"`
	CacheControl string            `json:"cache_control,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NewFSCache opens (creating if needed) a filesystem cache rooted at root.
func NewFSCache(root string) (*FSCache, error) {
	if root == "" {
		return nil, fmt.Errorf("fs cache root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve fs cache root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create fs cache root: %w", err)
	}
	return &FSCache{root: abs}, nil
}

// Root returns the absolute directory objects are stored under.
func (c *FSCache) Root() string { return c.root }

// objectPath derives the data and metadata paths for key. The first two
// digest bytes fan objects out into 256 directories so a large cache does not
// put every file in one directory (which is slow to list and prune).
func (c *FSCache) objectPath(key string) (dataPath, metaPath string) {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])
	dataPath = filepath.Join(c.root, name[:2], name[2:4], name)
	return dataPath, dataPath + ".meta.json"
}

func (c *FSCache) Get(_ context.Context, key string) ([]byte, string, error) {
	dataPath, metaPath := c.objectPath(key)
	st, err := os.Stat(dataPath)
	if err != nil {
		return nil, "", err
	}
	if st.Size() > maxObjectBytes {
		return nil, "", fmt.Errorf("cached object %q too large: %d bytes", key, st.Size())
	}
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, "", err
	}
	contentType := readMeta(metaPath).ContentType
	if contentType == "" {
		// Older entries (or a lost sidecar) still serve a sane type instead
		// of letting the HTTP layer guess.
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

// GetStream opens a cached object without buffering it. A missing key
// surfaces as an error that satisfies IsNotFound (it wraps fs.ErrNotExist).
func (c *FSCache) GetStream(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	dataPath, metaPath := c.objectPath(key)
	f, err := os.Open(dataPath)
	if err != nil {
		return nil, "", 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, "", 0, err
	}
	if st.Size() > maxObjectBytes {
		_ = f.Close()
		return nil, "", 0, fmt.Errorf("cached object %q too large: %d bytes", key, st.Size())
	}
	contentType := readMeta(metaPath).ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return f, contentType, st.Size(), nil
}

func (c *FSCache) Put(ctx context.Context, key string, data []byte, contentType string, cacheControl string) error {
	return c.PutWithMeta(ctx, key, data, contentType, nil, cacheControl)
}

func (c *FSCache) PutWithMeta(ctx context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dataPath, metaPath := c.objectPath(key)
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		return fmt.Errorf("create fs cache dir: %w", err)
	}

	// Sidecar first: a reader that finds the data file always finds the
	// metadata too (worst case it falls back to DetectContentType).
	sidecar, err := json.Marshal(fsCacheMeta{
		ContentType:  contentType,
		CacheControl: cacheControl,
		Metadata:     meta,
	})
	if err != nil {
		return fmt.Errorf("encode fs cache metadata: %w", err)
	}
	if err := writeFileAtomic(metaPath, sidecar, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(dataPath, data, 0o644); err != nil {
		return err
	}
	return nil
}

func (c *FSCache) Head(_ context.Context, key string) (minio.ObjectInfo, error) {
	dataPath, metaPath := c.objectPath(key)
	st, err := os.Stat(dataPath)
	if err != nil {
		return minio.ObjectInfo{}, err
	}
	meta := readMeta(metaPath)
	return minio.ObjectInfo{
		Key:          key,
		Size:         st.Size(),
		ContentType:  meta.ContentType,
		LastModified: st.ModTime(),
		UserMetadata: minio.StringMap(meta.Metadata),
	}, nil
}

// Clear removes every cached object. It is the "clear cache" action in the
// desktop settings; it is not safe against concurrent Put calls, which is
// acceptable for a user-triggered purge.
func (c *FSCache) Clear() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return fmt.Errorf("read fs cache root: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(c.root, entry.Name())); err != nil {
			return fmt.Errorf("clear fs cache entry %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func readMeta(path string) fsCacheMeta {
	var meta fsCacheMeta
	raw, err := os.ReadFile(path)
	if err != nil {
		return meta
	}
	// Best-effort: a corrupt sidecar degrades to "no metadata", never to a
	// cache error.
	_ = json.Unmarshal(raw, &meta)
	return meta
}

// writeFileAtomic writes data to path via a temp file plus rename so readers
// never observe a half-written object.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create fs cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write fs cache temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod fs cache temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync fs cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close fs cache temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename fs cache temp file: %w", err)
	}
	return nil
}
