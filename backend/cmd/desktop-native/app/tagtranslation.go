package app

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TagTranslationDB is the EhTagTranslation database (name and intro per
// namespace:name), fetched once and cached on disk. It is several megabytes
// of JSON, so it is parsed in the background and only searched when ready.
type TagTranslationDB struct {
	mu      sync.RWMutex
	entries map[string]translationEntry
	ns      map[string]string // namespace -> display name
	names   []translationEntry
	version string
	loaded  bool
	loading bool
	err     string
	path    string

	// hooked is called after a load or failure, on the load's goroutine; the
	// app uses it to ask for a frame.
	hooked func()
}

type translationEntry struct {
	Namespace   string
	Tag         string
	Translation string
	Description string
	// key is the lowercased translation and tag, searched as one string.
	key string
}

// TagTranslationsRemote is where a fresh database comes from when the cache
// is missing or the user asks to update. It is the plain JSON release: the
// .js variant needs a JavaScript inflate runtime, which a native app cannot
// run.
const TagTranslationsRemote = "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.json"

// rawTagDB mirrors the release file, which is wrapped in a callback:
//
//	load_ehtagtranslation_db_text({...});
type rawTagDB struct {
	Version any `json:"version"`
	Head    struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Data []struct {
		Namespace string `json:"namespace"`
		Data      map[string]struct {
			Name  string `json:"name"`
			Intro string `json:"intro"`
		} `json:"data"`
	} `json:"data"`
}

var (
	tagDBJSONP  = regexp.MustCompile(`(?s)^\s*[A-Za-z_$][\w$.]*\s*\((.*)\)\s*;?\s*$`)
	tagHTMLTags = regexp.MustCompile(`<[^>]*>`)
)

// parseTagTranslationDB builds the lookup tables from the release file's
// bytes, with or without the JSONP wrapper.
func parseTagTranslationDB(data []byte) (*TagTranslationDB, error) {
	body := data
	if m := tagDBJSONP.FindSubmatch(data); m != nil && m[1][0] == '{' {
		body = m[1]
	}

	var raw rawTagDB
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析标签翻译库失败：%w", err)
	}

	db := &TagTranslationDB{
		entries: make(map[string]translationEntry, 8192),
		ns:      make(map[string]string, 32),
	}
	if v := fmt.Sprint(raw.Version); v != "" && v != "<nil>" {
		db.version = v
	}
	for _, group := range raw.Data {
		namespace := group.Namespace
		for tag, info := range group.Data {
			translation := cleanHTMLText(info.Name)
			if translation == "" {
				continue
			}
			if namespace == "rows" {
				db.ns[tag] = translation
				continue
			}
			e := translationEntry{
				Namespace:   namespace,
				Tag:         tag,
				Translation: translation,
				Description: cleanHTMLText(info.Intro),
			}
			e.key = strings.ToLower(translation + "\x00" + tag)
			db.entries[namespace+":"+tag] = e
			db.names = append(db.names, e)
		}
	}
	return db, nil
}

// cleanHTMLText drops tags from the database's HTML and collapses spaces.
func cleanHTMLText(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "<") {
		return s
	}
	s = tagHTMLTags.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// Lookup returns the translation of a tag, or "" when it has none.
func (db *TagTranslationDB) Lookup(namespace, tag string) string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.entries[namespace+":"+tag].Translation
}

// NamespaceName returns the display name of a namespace ("female" → "女性").
func (db *TagTranslationDB) NamespaceName(namespace string) string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.ns[namespace]
}

// Describe returns the translation and description of a tag.
func (db *TagTranslationDB) Describe(namespace, tag string) (string, string) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	e := db.entries[namespace+":"+tag]
	return e.Translation, e.Description
}

// TagSuggestion is one search result of the translation database.
type TagSuggestion struct {
	Namespace   string
	Tag         string
	Translation string
	Description string
}

// Search finds tags whose translation or name contains query, up to limit,
// in a stable order.
func (db *TagTranslationDB) Search(query string, limit int) []TagSuggestion {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || limit <= 0 {
		return nil
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	out := make([]TagSuggestion, 0, limit)
	for _, e := range db.names {
		if strings.Contains(e.key, q) {
			out = append(out, TagSuggestion{
				Namespace: e.Namespace, Tag: e.Tag,
				Translation: e.Translation, Description: e.Description,
			})
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// Count is the number of tags the database knows.
func (db *TagTranslationDB) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.entries)
}

// Ready reports whether the database is parsed and searchable.
func (db *TagTranslationDB) Ready() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.loaded
}

// Load reads the cached database, or downloads a fresh one when there is no
// cache. It runs in the background; a second call while loading is a no-op.
func (db *TagTranslationDB) Load() {
	db.mu.Lock()
	if db.loading || db.loaded {
		db.mu.Unlock()
		return
	}
	db.loading = true
	db.err = ""
	db.mu.Unlock()

	go func() {
		// A failure keeps the old tables, so a bad update does not lose them.
		data, err := db.readOrFetch()
		var parsed *TagTranslationDB
		if err == nil {
			parsed, err = parseTagTranslationDB(data)
		}
		db.mu.Lock()
		db.loading = false
		if err != nil {
			db.err = err.Error()
		} else {
			db.entries, db.ns, db.names = parsed.entries, parsed.ns, parsed.names
			db.version = parsed.version
			db.loaded = true
		}
		hook := db.hooked
		db.mu.Unlock()
		if hook != nil {
			hook()
		}
	}()
}

// readOrFetch returns the cached file, downloading and caching it when there
// is none.
func (db *TagTranslationDB) readOrFetch() ([]byte, error) {
	if db.path != "" {
		if data, err := os.ReadFile(db.path); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	if db.path == "" {
		return nil, fmt.Errorf("标签翻译库路径未配置")
	}
	data, err := httpGetBytes(TagTranslationsRemote, 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("下载标签翻译库失败：%w", err)
	}
	if err := writeFileAtomic(db.path, data); err != nil {
		// The database still parses; failing to cache it only costs the next
		// download.
		return data, nil
	}
	return data, nil
}

// Update downloads the newest database, replaces the cache and the tables.
func (db *TagTranslationDB) Update() {
	db.mu.Lock()
	if db.loading {
		db.mu.Unlock()
		return
	}
	db.loading = true
	db.err = ""
	path := db.path
	db.mu.Unlock()

	go func() {
		var (
			parsed *TagTranslationDB
			err    error
			data   []byte
		)
		data, err = httpGetBytes(TagTranslationsRemote, 2*time.Minute)
		if err == nil {
			parsed, err = parseTagTranslationDB(data)
		}
		db.mu.Lock()
		db.loading = false
		if err != nil {
			db.err = err.Error()
		} else {
			db.entries, db.ns, db.names = parsed.entries, parsed.ns, parsed.names
			db.version = parsed.version
			db.loaded = true
			if path != "" {
				_ = writeFileAtomic(path, data)
			}
		}
		hook := db.hooked
		db.mu.Unlock()
		if hook != nil {
			hook()
		}
	}()
}

// Status is the line the tag page shows about the database.
func (db *TagTranslationDB) Status() string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	switch {
	case db.loading:
		return "正在加载翻译库…"
	case db.err != "" && !db.loaded:
		return "翻译库不可用：" + db.err
	case db.loaded:
		v := db.version
		if v == "" {
			v = "未知版本"
		}
		return fmt.Sprintf("已加载 %d 个标签 · 版本 %s", len(db.entries), v)
	case db.err != "":
		return "翻译库不可用：" + db.err
	}
	return "尚未加载翻译库"
}

// writeFileAtomic replaces path with data through a temporary file, so a
// crash never leaves a half-written database.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tagTranslationCachePath is where the database is kept: next to the image
// cache, named for the EhTagTranslation release.
func tagTranslationCachePath(cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cacheDir), "tag-translations.json")
}

// httpGetBytes fetches a URL into memory; the tag database is a few MB.
func httpGetBytes(target string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// ensureTranslations starts the background load of the tag database once.
func (a *App) ensureTranslations() {
	a.translations.Load()
}
