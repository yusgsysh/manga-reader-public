package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manga-reader/internal/config"
)

// TestStartDatabaseFailureExplains covers a database that cannot be opened:
// the error names the file, says what to try, and keeps the library's cause.
func TestStartDatabaseFailureExplains(t *testing.T) {
	dir := t.TempDir()
	// A directory where the database file should be: SQLite cannot open it.
	dbPath := filepath.Join(dir, "manga.db")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Start(context.Background(), Options{
		Cookie:   config.CookieConfig{IpbMemberID: "1", IpbPassHash: "h"},
		DBPath:   dbPath,
		CacheDir: filepath.Join(dir, "images"),
	})
	if err == nil {
		t.Fatal("Start succeeded with a directory as the database")
	}
	se := AsStartError(err)
	if se == nil {
		t.Fatalf("error is not a *StartError: %T %v", err, err)
	}
	if se.Kind != FailDatabase {
		t.Fatalf("kind = %v, want FailDatabase", se.Kind)
	}
	msg := err.Error()
	for _, want := range []string{"数据库无法打开", dbPath, "原始错误"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if errors.Unwrap(err) == nil {
		t.Error("the library error was not kept for logs")
	}
}

// TestStartCacheDirFailureExplains covers the cache path the same way.
func TestStartCacheDirFailureExplains(t *testing.T) {
	dir := t.TempDir()
	// A regular file where the cache directory should be.
	cachePath := filepath.Join(dir, "images")
	if err := os.WriteFile(cachePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Start(context.Background(), Options{
		Cookie:   config.CookieConfig{IpbMemberID: "1", IpbPassHash: "h"},
		DBPath:   filepath.Join(dir, "manga.db"),
		CacheDir: cachePath,
	})
	if err == nil {
		t.Fatal("Start succeeded with a file as the cache directory")
	}
	se := AsStartError(err)
	if se == nil || se.Kind != FailCacheDir {
		t.Fatalf("error = %#v, want a FailCacheDir StartError", se)
	}
	if !strings.Contains(err.Error(), "图片缓存目录无法打开") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestAsStartErrorPassesThroughPlainErrors(t *testing.T) {
	if got := AsStartError(errors.New("plain")); got != nil {
		t.Fatalf("AsStartError(plain) = %v", got)
	}
}
