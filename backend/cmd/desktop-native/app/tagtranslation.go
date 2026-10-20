package app

import (
	"context"
	"encoding/json"

	"fmt"
	"github.com/egoist/mygo"
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

	// customPath is the user's own translations file, merged over the
	// database when it exists. Empty when the app cannot name a location.
	customPath string
	// custom is the merged content of that file, held in memory so an
	// import merges with what the app knows rather than with whatever the
	// file happens to hold at that moment (it is the user's to edit).
	custom customTranslationsFile
	// customCount is how many of the entries came from the user's file
	// rather than the downloaded database.
	customCount int

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
			db.applyParsedLocked(parsed)
		}
		hook := db.hooked
		db.mu.Unlock()
		if hook != nil {
			hook()
		}
	}()
}

// applyParsedLocked installs parsed tables plus the custom overlay. The
// caller holds mu.
func (db *TagTranslationDB) applyParsedLocked(parsed *TagTranslationDB) {
	db.entries, db.ns, db.names = parsed.entries, parsed.ns, parsed.names
	db.version = parsed.version
	db.loaded = true
	db.customCount = 0
	if custom := db.customLocked(); len(custom) > 0 {
		db.overlayCustomLocked(custom)
	}
}

// overlayCustomLocked folds the user's entries into the tables, marking the
// ones that are not from the downloaded database as theirs. The caller holds
// mu.
func (db *TagTranslationDB) overlayCustomLocked(custom customTranslationsFile) {
	for key, name := range custom {
		ns, tag, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		e := translationEntry{
			Namespace:   ns,
			Tag:         tag,
			Translation: name,
		}
		e.key = strings.ToLower(name + "\x00" + tag)
		if old, exists := db.entries[key]; exists {
			e.Description = old.Description
		} else {
			db.names = append(db.names, e)
		}
		// Both a replacement and an addition come from the user's file.
		db.customCount++
		db.entries[key] = e
	}
}
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
			db.applyParsedLocked(parsed)
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
		line := fmt.Sprintf("已加载 %d 个标签 · 版本 %s", len(db.entries), v)
		if db.customCount > 0 {
			line += fmt.Sprintf(" · 自定义 %d 条", db.customCount)
		}
		return line
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

// customTranslationsPath is where the user's own translations live: beside
// the database, so both are found together and survive cache clears of the
// images themselves.
func customTranslationsPath(cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cacheDir), "my-tag-translations.json")
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

// SetCustomPath names the user's own translations file and re-applies it to
// the loaded tables.
func (db *TagTranslationDB) SetCustomPath(path string) {
	db.mu.Lock()
	db.customPath = path
	if db.loaded {
		db.reloadCustomLocked()
	}
	db.mu.Unlock()
}

// SetCustom installs the user's own translations as given (as the settings
// of a test do) and re-applies them.
func (db *TagTranslationDB) SetCustom(entries customTranslationsFile) {
	db.mu.Lock()
	db.custom = entries.normalised()
	if db.loaded {
		db.reloadCustomLocked()
	}
	db.mu.Unlock()
}

// CustomPath is the user's own translations file, or "".
func (db *TagTranslationDB) CustomPath() string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.customPath
}

// ensureTranslations starts the background load of the tag database once.
func (a *App) ensureTranslations() {
	a.translations.Load()
}

// customTranslationsSuffix is the file extension the import/export dialog
// filters on. The format is plain JSON, but a distinct extension keeps the
// library file and the user's own file apart in a file list.
const customTranslationsSuffix = ".tag-tr.json"

// customTranslationsFile is the user's own translations, written and read as
// one JSON object mapping "namespace:tag" to the Chinese name. Only what the
// file contains is shown, so a small file stays small.
type customTranslationsFile map[string]string

// readCustomTranslations parses the user's file. A wrapper object
// {"translations": {...}} is accepted too, so a file with metadata later on
// does not break older files.
func readCustomTranslations(data []byte) (customTranslationsFile, error) {
	var flat customTranslationsFile
	if err := json.Unmarshal(data, &flat); err == nil && flat != nil {
		return flat.normalised(), nil
	}
	var wrapped struct {
		Translations customTranslationsFile `json:"translations"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Translations != nil {
		return wrapped.Translations.normalised(), nil
	}
	return nil, fmt.Errorf("不是可识别的标签翻译文件")
}

// normalised trims the entries and drops ones without a namespace:tag key or
// a name.
func (f customTranslationsFile) normalised() customTranslationsFile {
	out := make(customTranslationsFile, len(f))
	for k, v := range f {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" || !strings.Contains(k, ":") {
			continue
		}
		out[k] = v
	}
	return out
}

// exportCustomTranslations writes the given entries as the user's file.
func exportCustomTranslations(path string, entries customTranslationsFile) error {
	if len(entries) == 0 {
		return fmt.Errorf("没有可导出的自定义翻译")
	}
	data, err := json.MarshalIndent(entries.normalised(), "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// exportCustomPath is the file the 导出 button writes: the translations the
// user has added on top of the EhTagTranslation database.
func (a *App) exportCustomPath() (string, customTranslationsFile, error) {
	path, err := pickSavePath(a.win, userDocumentsDir(), "我的标签翻译"+customTranslationsSuffix)
	if err != nil {
		return "", nil, err
	}
	if path == "" {
		return "", nil, nil // canceled
	}
	return path, a.translations.Custom(), nil
}

// userDocumentsDir is where the dialogs start, when it can be found.
func userDocumentsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Documents")
	}
	return ""
}

// Custom returns the translations the user's file adds over the downloaded
// database, read from disk at the moment of the call.
func (db *TagTranslationDB) Custom() customTranslationsFile {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.customLocked()
}

// customLocked is Custom for a caller already holding mu — Custom itself
// holds it read-only, so it must not call back in.
func (db *TagTranslationDB) customLocked() customTranslationsFile {
	if db.custom != nil {
		return db.custom
	}
	// First use in this run: what the file already holds counts too.
	if db.customPath == "" {
		return nil
	}
	data, err := os.ReadFile(db.customPath)
	if err != nil {
		return nil
	}
	flat, err := readCustomTranslations(data)
	if err != nil {
		return nil
	}
	db.custom = flat
	return flat
}

// importCustomTranslations merges the file at path into the user's file and
// reloads the tables. It returns how many entries the file brought.
func (db *TagTranslationDB) importCustomTranslations(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取文件失败：%w", err)
	}
	incoming, err := readCustomTranslations(data)
	if err != nil {
		return 0, err
	}
	db.mu.Lock()
	merged := db.mergedCustomLocked(incoming)
	if err := writeFileAtomic(db.customPath, merged); err != nil {
		db.mu.Unlock()
		return 0, fmt.Errorf("写入自定义翻译失败：%w", err)
	}
	if db.loaded && db.entries != nil {
		db.reloadCustomLocked()
	}
	db.mu.Unlock()
	return len(incoming), nil
}

// reloadCustomLocked rebuilds the tables from the cached database file and
// applies the custom file again, so an import is visible at once. The caller
// holds mu. A failure keeps what is on screen.
func (db *TagTranslationDB) reloadCustomLocked() {
	if db.path == "" {
		return
	}
	data, err := os.ReadFile(db.path)
	if err != nil {
		return
	}
	parsed, err := parseTagTranslationDB(data)
	if err != nil {
		return
	}
	db.applyParsedLocked(parsed)
}

// mergedCustomLocked combines what the app has of the custom file with
// incoming, where incoming wins, and remembers it. The caller holds mu.
func (db *TagTranslationDB) mergedCustomLocked(incoming customTranslationsFile) []byte {
	if db.custom == nil {
		db.custom = customTranslationsFile{}
	}
	for k, v := range incoming {
		db.custom[k] = v
	}
	out, _ := json.MarshalIndent(db.custom.normalised(), "", "  ")
	return append(out, '\n')
}

// pickOpenPath is the native open dialog, a variable so tests can answer it.
var pickOpenPath = func(parent *mygo.Window, title string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      parent,
		Title:       title,
		ButtonLabel: "导入",
		Filters:     []mygo.FileFilter{{Name: "标签翻译 JSON", Extensions: []string{"json", "tag-tr"}}},
	})
	if len(paths) == 0 {
		return "", err
	}
	return paths[0], err
}

// importCustomTranslations asks for the user's file, merges it into the
// custom file and reports the count on the tag page.
func (a *App) importCustomTranslations() {
	if a.transBusy {
		return
	}
	a.transBusy = true
	a.transMsg = ""
	path, err := pickOpenPath(a.win, "导入自定义标签翻译")
	if err != nil || path == "" {
		a.transBusy = false
		a.requestFrame()
		return
	}
	db := &a.translations
	go func() {
		n, err := db.importCustomTranslations(path)
		a.update(func() {
			a.transBusy = false
			if err != nil {
				a.transMsg = err.Error()
				return
			}
			a.transMsg = fmt.Sprintf("已导入 %d 条自定义翻译", n)
		})
	}()
}

// exportCustomTranslations asks where to write the user's own translations
// and writes them, reporting the outcome on the tag page.
func (a *App) exportCustomTranslations() {
	if a.transBusy {
		return
	}
	a.transBusy = true
	a.transMsg = ""
	path, entries, err := a.exportCustomPath()
	if err != nil || path == "" {
		if err != nil {
			a.transMsg = err.Error()
		}
		a.transBusy = false
		a.requestFrame()
		return
	}
	go func() {
		err := exportCustomTranslations(path, entries)
		a.update(func() {
			a.transBusy = false
			if err != nil {
				a.transMsg = err.Error()
				return
			}
			a.transMsg = fmt.Sprintf("已导出 %d 条自定义翻译到 %s", len(entries), path)
		})
	}()
}
