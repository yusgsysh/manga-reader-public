package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Diagnostics is the text the settings page can export: what the app is, where
// it keeps its data, and how big those files are. It never includes the
// cookie, the sync token or image bytes, so it can be attached to a report.
func (a *App) Diagnostics() string {
	var b strings.Builder
	a.mu.Lock()
	defer a.mu.Unlock()

	fmt.Fprintf(&b, "Manga Reader 诊断信息\n")
	fmt.Fprintf(&b, "生成时间：%s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "版本：%s\n", ourEmpty(a.build))
	fmt.Fprintf(&b, "平台：%s/%s，Go %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(&b, "后端地址：%s\n", ourEmpty(a.client.BaseURL()))
	fmt.Fprintf(&b, "后端模式：%s\n", map[bool]string{true: "外部（MANGA_READER_API）", false: "内嵌"}[a.backend == nil])
	fmt.Fprintf(&b, "配置文件：%s\n", ourEmpty(a.storage.ConfigPath))
	fmt.Fprintf(&b, "本地数据库：%s\n", ourEmpty(a.storage.DBPath))
	fmt.Fprintf(&b, "图片缓存目录：%s\n", ourEmpty(a.storage.CacheDir))
	fmt.Fprintf(&b, "缓存占用：%s / %s\n", humanBytes(a.cacheBytes), cacheLimitText(a.cacheMax))
	fmt.Fprintf(&b, "图片缓存上限：%s\n", cacheLimitText(a.cacheMax))

	// File sizes, not contents: a database path with a surprising size is
	// often the clue in a report.
	for _, p := range []string{a.storage.DBPath, a.storage.ConfigPath} {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s 大小：%s\n", filepath.Base(p), humanBytes(info.Size()))
		}
	}
	fmt.Fprintf(&b, "翻译库：%s\n", a.translations.Status())
	fmt.Fprintf(&b, "标签规则：%d 条\n", len(a.tags.rules))
	fmt.Fprintf(&b, "离线下载任务：%d 个\n", len(a.downloads.items))
	fmt.Fprintf(&b, "当前页面：%s\n", a.router.Path())
	return b.String()
}

func ourEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// exportDiagnostics asks for a path and writes Diagnostics() there.
func (a *App) exportDiagnostics() {
	if a.diagBusy {
		return
	}
	a.diagBusy = true
	a.diagMsg = ""

	a.diagSeq++
	seq := a.diagSeq
	go func() {
		dir := ""
		if a.storage.CacheDir != "" {
			dir = filepath.Dir(a.storage.CacheDir)
		}
		name := fmt.Sprintf("manga-reader-diagnostics-%s.txt", time.Now().Format("20060102-150405"))
		path, err := pickSavePath(a.win, dir, name)
		if err != nil {
			a.update(func() {
				if a.diagSeq != seq {
					return
				}
				a.diagBusy = false
				a.diagMsg = err.Error()
			})
			return
		}
		if path == "" {
			a.update(func() {
				if a.diagSeq == seq {
					a.diagBusy = false
				}
			})
			return
		}
		text := a.Diagnostics()
		err = writeFileAtomic(path, []byte(text))
		a.update(func() {
			if a.diagSeq != seq {
				return
			}
			a.diagBusy = false
			if err != nil {
				a.diagMsg = "写入失败：" + err.Error()
				return
			}
			a.diagMsg = "已保存 " + filepath.Base(path)
		})
	}()
}
