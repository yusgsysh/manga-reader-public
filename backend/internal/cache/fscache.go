package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
)

// metaSuffix is the sidecar name appended to an object's data path.
const metaSuffix = ".meta.json"

// FSCache is a filesystem-backed ImageCache for single-binary desktop builds.
// It replaces MinIO for local installs: objects live in a hashed directory
// tree under Root, each with a sidecar JSON file that carries the content
// type, cache-control header and user metadata MinIO stored as object
// metadata.
//
// Cache keys are already opaque digests (see CacheKey, ThumbnailCacheKey),
// but the cache never trusts a key as a path: it hashes the key again to
// derive the file name, so a hostile or malformed key cannot escape Root.
//
// When a size limit is set, writes trigger an asynchronous prune that evicts
// the least recently modified objects until the cache fits again, so a long
// browsing session cannot fill the disk.
type FSCache struct {
	root string

	maxBytes atomic.Int64 // 0 means unlimited
	approx   atomic.Int64 // bytes written since the last exact walk
	seeded   atomic.Bool  // whether approx reflects a full walk

	pruneMu sync.Mutex // serializes prunes
	pruning atomic.Bool
}

// fsCacheMeta is the sidecar persisted next to every object.
type fsCacheMeta struct {
	ContentType  string            `json:"content_type,omitempty"`
	CacheControl string            `json:"cache_control,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NewFSCache opens (creating if needed) a filesystem cache rooted at root. A
// maxBytes value greater than zero bounds the cache; eviction happens on Put.
func NewFSCache(root string, maxBytes int64) (*FSCache, error) {
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
	c := &FSCache{root: abs}
	if maxBytes > 0 {
		c.maxBytes.Store(maxBytes)
	}
	return c, nil
}

// Root returns the absolute directory objects are stored under.
func (c *FSCache) Root() string { return c.root }

// MaxBytes returns the configured size limit; 0 means unlimited.
func (c *FSCache) MaxBytes() int64 { return c.maxBytes.Load() }

// SetMaxBytes changes the size limit and, when the cache is already over it,
// prunes in the background. A non-positive value disables the limit.
func (c *FSCache) SetMaxBytes(maxBytes int64) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	c.maxBytes.Store(maxBytes)
	if maxBytes > 0 {
		go c.maybePrune()
	}
}

// objectPath derives the data and metadata paths for key. The first two
// digest bytes fan objects out into 256 directories so a large cache does not
// put every file in one directory (which is slow to list and prune).
func (c *FSCache) objectPath(key string) (dataPath, metaPath string) {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])
	dataPath = filepath.Join(c.root, name[:2], name[2:4], name)
	return dataPath, dataPath + metaSuffix
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

	// Track growth approximately and prune in the background when over the
	// limit. The estimate is reseeded by every exact Prune walk.
	c.approx.Add(int64(len(data)) + int64(len(sidecar)))
	if c.maxBytes.Load() > 0 {
		go c.maybePrune()
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
	c.approx.Store(0)
	c.seeded.Store(true)
	return nil
}

// Size returns the total number of bytes stored, used by the desktop settings
// page. Unreadable files are skipped rather than failing the whole walk.
func (c *FSCache) Size() (int64, error) {
	_, total, err := c.collect()
	return total, err
}

// maybePrune runs Prune at most once at a time, skipping the walk when the
// seeded estimate already fits.
func (c *FSCache) maybePrune() {
	if c.maxBytes.Load() <= 0 {
		return
	}
	if c.seeded.Load() && c.approx.Load() <= c.maxBytes.Load() {
		return
	}
	if !c.pruning.CompareAndSwap(false, true) {
		return
	}
	defer c.pruning.Store(false)
	_, _ = c.Prune()
}

// Prune evicts the least recently modified objects until the cache fits its
// limit, returning the number of bytes removed. A non-positive limit makes it
// a size-only no-op. It recomputes the exact size from disk, so it also
// reseeds the approximate counter.
func (c *FSCache) Prune() (int64, error) {
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()

	objects, total, err := c.collect()
	if err != nil {
		return 0, err
	}
	c.approx.Store(total)
	c.seeded.Store(true)

	max := c.maxBytes.Load()
	if max <= 0 || total <= max {
		return 0, nil
	}

	// Evict down to 90% of the limit so the next few puts do not immediately
	// retrigger a walk.
	target := max - max/10
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].modTime.Before(objects[j].modTime)
	})

	var removed int64
	for _, obj := range objects {
		if total-removed <= target {
			break
		}
		_ = os.Remove(obj.path)
		_ = os.Remove(obj.path + metaSuffix)
		removed += obj.size
	}
	c.approx.Add(-removed)
	return removed, nil
}

// cacheObject is one cached entry: its data file plus its sidecar.
type cacheObject struct {
	path    string
	size    int64
	modTime time.Time
}

// collect walks the cache tree into objects and their total size. Sidecars are
// grouped with their data file so eviction never leaves one orphaned.
func (c *FSCache) collect() ([]*cacheObject, int64, error) {
	byPath := make(map[string]*cacheObject)
	var total int64

	err := filepath.WalkDir(c.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		// Atomic writes stage through a ".tmp-*" file; never evict one out
		// from under a writer.
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		base := strings.TrimSuffix(path, metaSuffix)
		obj := byPath[base]
		if obj == nil {
			obj = &cacheObject{path: base}
			byPath[base] = obj
		}
		obj.size += info.Size()
		if info.ModTime().After(obj.modTime) {
			obj.modTime = info.ModTime()
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return nil, total, fmt.Errorf("walk fs cache: %w", err)
	}

	objects := make([]*cacheObject, 0, len(byPath))
	for _, obj := range byPath {
		objects = append(objects, obj)
	}
	return objects, total, nil
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
