package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"manga-reader/cmd/desktop-native/app"
	"manga-reader/cmd/desktop-native/internal/desktop"
	"manga-reader/internal/config"
)

// embeddedBackend owns the in-process API server's lifecycle so the UI can
// start (and restart) it from the first-run and settings screens.
type embeddedBackend struct {
	mu       sync.Mutex
	settings desktop.Settings
	server   *desktop.Server
}

var _ app.Backend = (*embeddedBackend)(nil)

func (b *embeddedBackend) Configured() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.server != nil
}

func (b *embeddedBackend) BaseURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.server == nil {
		return ""
	}
	return b.server.BaseURL()
}

func (b *embeddedBackend) Start(cookie string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cookie = strings.TrimSpace(cookie)
	cfg := config.ParseCookieString(cookie)
	if !cfg.IsValid() {
		return "", fmt.Errorf("Cookies 不完整：至少需要 ipb_member_id 和 ipb_pass_hash")
	}

	if b.server != nil {
		_ = b.server.Close()
		b.server = nil
	}

	settings := b.settings
	settings.Cookie = cookie
	if err := desktop.SaveSettings(settings); err != nil {
		return "", fmt.Errorf("保存设置失败: %w", err)
	}

	srv, err := desktop.Start(context.Background(), desktop.Options{
		Cookie:    cfg,
		DBPath:    settings.DatabasePathOrDefault(),
		CacheDir:  settings.CacheDirOrDefault(),
		SyncToken: settings.SyncToken,
		DevTools:  settings.DevTools,
	})
	if err != nil {
		return "", err
	}

	b.settings = settings
	b.server = srv
	return srv.BaseURL(), nil
}

func (b *embeddedBackend) Storage() app.BackendStorage {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.settings
	return app.BackendStorage{
		DBPath:     s.DatabasePathOrDefault(),
		CacheDir:   s.CacheDirOrDefault(),
		ConfigPath: desktop.SettingsPath(),
	}
}

func (b *embeddedBackend) ClearCache() error {
	b.mu.Lock()
	srv := b.server
	b.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.ClearCache()
}

func (b *embeddedBackend) CacheInfo() (string, int64, error) {
	b.mu.Lock()
	srv := b.server
	b.mu.Unlock()
	if srv == nil {
		return "", 0, nil
	}
	return srv.CacheInfo()
}

// Close stops the server, if any. Called when the window loop exits.
func (b *embeddedBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.server == nil {
		return nil
	}
	err := b.server.Close()
	b.server = nil
	return err
}
