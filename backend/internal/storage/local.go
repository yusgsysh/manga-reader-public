package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalConfig configures LocalStorage.
type LocalConfig struct {
	// Root is the directory the cache keys are mapped into. It is created when
	// missing.
	Root string
}

// LocalStorage stores objects as plain files under a root directory. Every
// key maps to <root>/<key>; object metadata lives in a sibling "<path>.meta"
// JSON sidecar so the object file itself stays byte-identical to what a
// downloader would produce.
type LocalStorage struct {
	root string
}

var _ Storage = (*LocalStorage)(nil)

// NewLocalStorage prepares a local cache rooted at cfg.Root.
func NewLocalStorage(cfg LocalConfig) (*LocalStorage, error) {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		return nil, fmt.Errorf("storage: local storage root must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve local root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create local root %q: %w", abs, err)
	}
	return &LocalStorage{root: abs}, nil
}

// Root returns the resolved cache root directory.
func (s *LocalStorage) Root() string { return s.root }

// Get reads the object and its stored content type.
func (s *LocalStorage) Get(_ context.Context, key string) ([]byte, string, error) {
	file, err := s.resolve(key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("storage: get %q: %w", key, ErrNotFound)
		}
		return nil, "", fmt.Errorf("storage: get %q: %w", key, err)
	}
	meta, err := s.readMeta(file)
	if err != nil {
		return nil, "", err
	}
	return data, meta.ContentType, nil
}

// Put writes data and its metadata atomically. The object file is the commit
// point: readers either see the previous object or the new one, never a
// partial write.
func (s *LocalStorage) Put(_ context.Context, key string, data []byte, contentType string, opts PutOptions) error {
	file, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	meta := localMeta{
		ContentType:  contentType,
		CacheControl: opts.CacheControl,
		Meta:         opts.Meta,
	}
	// Metadata first: a concurrent reader of a brand new key then gets a
	// clean ErrNotFound instead of bytes without a content type.
	if err := writeFileAtomic(metaPath(file), mustMarshalMeta(key, meta)); err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	if err := writeFileAtomic(file, data); err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}

// Delete removes the object and its metadata sidecar. It is idempotent.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	file, err := s.resolve(key)
	if err != nil {
		return err
	}
	for _, target := range []string{file, metaPath(file)} {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("storage: delete %q: %w", key, err)
		}
	}
	return nil
}

// Exists reports whether the object is present.
func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	file, err := s.resolve(key)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(file); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("storage: stat %q: %w", key, err)
	}
}

// Stat returns the object metadata without reading the payload.
func (s *LocalStorage) Stat(_ context.Context, key string) (ObjectInfo, error) {
	file, err := s.resolve(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectInfo{}, fmt.Errorf("storage: stat %q: %w", key, ErrNotFound)
		}
		return ObjectInfo{}, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	meta, err := s.readMeta(file)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{
		ContentType:  meta.ContentType,
		CacheControl: meta.CacheControl,
		Size:         info.Size(),
		Meta:         meta.Meta,
	}, nil
}

// resolve maps a cache key onto an absolute path inside the root. Anything
// that would escape the root (absolute keys, "..", backslashes, NUL bytes) is
// rejected.
func (s *LocalStorage) resolve(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("storage: empty cache key")
	}
	if strings.ContainsAny(key, "\x00\\") {
		return "", fmt.Errorf("storage: invalid cache key %q", key)
	}
	if path.IsAbs(key) || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("storage: invalid cache key %q", key)
	}
	clean := path.Clean(key)
	if clean != key || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("storage: invalid cache key %q", key)
	}
	full := filepath.Join(s.root, filepath.FromSlash(clean))
	if full != s.root && !strings.HasPrefix(full, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: cache key %q escapes the cache root", key)
	}
	return full, nil
}

func (s *LocalStorage) readMeta(file string) (localMeta, error) {
	raw, err := os.ReadFile(metaPath(file))
	if err != nil {
		if os.IsNotExist(err) {
			return localMeta{}, nil
		}
		return localMeta{}, fmt.Errorf("storage: read metadata: %w", err)
	}
	var meta localMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		// A corrupt sidecar must not poison reads of otherwise valid bytes.
		return localMeta{}, nil
	}
	return meta, nil
}

type localMeta struct {
	ContentType  string            `json:"contentType,omitempty"`
	CacheControl string            `json:"cacheControl,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
}

func metaPath(file string) string { return file + ".meta" }

func mustMarshalMeta(key string, meta localMeta) []byte {
	raw, err := json.Marshal(meta)
	if err != nil {
		// localMeta is always marshalable; keep a panic-free fallback anyway.
		panic(fmt.Sprintf("storage: marshal metadata for %q: %v", key, err))
	}
	return raw
}

// writeFileAtomic writes data to a temporary file in the same directory, fsyncs
// it and renames it over path, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
