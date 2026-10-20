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
	// token is a per-run secret required by the loopback API.
	token string
	// startErr is what went wrong before the backend could start, such as
	// config.json not parsing. The setup screen shows it above the cookie
	// field so the user knows why they are being asked again.
	startErr string
}

var _ app.Backend = (*embeddedBackend)(nil)

func (b *embeddedBackend) Configured() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.server != nil
}

// AuthToken returns the per-run token; it is stable across restarts so the
// UI client keeps working after the user changes cookies.
func (b *embeddedBackend) AuthToken() string { return b.token }

// StartError is the failure that kept the backend from starting, or "".
func (b *embeddedBackend) StartError() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startErr
}

// clearStartErrorLocked drops the startup failure once the user has acted on
// it: a successful start rewrote the settings file.
func (b *embeddedBackend) clearStartErrorLocked() { b.startErr = "" }

// setStartError records a startup failure for the setup/settings screens.
// The caller (main) has already logged it.
func (b *embeddedBackend) setStartError(msg string) {
	b.mu.Lock()
	b.startErr = msg
	b.mu.Unlock()
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
		Cookie:        cfg,
		DBPath:        settings.DatabasePathOrDefault(),
		CacheDir:      settings.CacheDirOrDefault(),
		CacheMaxBytes: settings.CacheLimit(),
		AuthToken:     b.token,
		SyncToken:     settings.SyncToken,
		DevTools:      settings.DevTools,
	})
	if err != nil {
		return "", err
	}

	b.settings = settings
	b.server = srv
	b.clearStartErrorLocked()
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

func (b *embeddedBackend) CacheInfo() (app.CacheStats, error) {
	b.mu.Lock()
	srv := b.server
	b.mu.Unlock()
	if srv == nil {
		return app.CacheStats{}, nil
	}
	root, size, err := srv.CacheInfo()
	if err != nil {
		return app.CacheStats{}, err
	}
	return app.CacheStats{Root: root, Bytes: size, MaxBytes: srv.CacheMaxBytes()}, nil
}

// SetCacheMaxBytes persists the new limit and applies it to the running cache.
func (b *embeddedBackend) SetCacheMaxBytes(maxBytes int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if maxBytes < 0 {
		maxBytes = 0
	}
	settings := b.settings
	settings.CacheMaxBytes = &maxBytes
	if err := desktop.SaveSettings(settings); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	b.settings = settings
	if b.server != nil {
		b.server.SetCacheMaxBytes(maxBytes)
	}
	return nil
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

// SetAppearance persists the appearance ("system", "light" or "dark"). The
// window itself is themed by main through mygo.Theme.
func (b *embeddedBackend) SetAppearance(source string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch source {
	case "light", "dark":
	default:
		source = "system"
	}
	settings := b.settings
	settings.Appearance = source
	if err := desktop.SaveSettings(settings); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	b.settings = settings
	return nil
}

// Appearance returns the saved appearance; "system" when unset.
func (b *embeddedBackend) Appearance() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.settings.Appearance == "" {
		return "system"
	}
	return b.settings.Appearance
}

// SetLanguage persists the interface language tag.
func (b *embeddedBackend) SetLanguage(tag string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	settings := b.settings
	settings.Language = tag
	if err := desktop.SaveSettings(settings); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	b.settings = settings
	return nil
}

// Language returns the saved interface language tag.
func (b *embeddedBackend) Language() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settings.Language
}

// SettingsPath is the config.json the settings above are written to.
func (b *embeddedBackend) SettingsPath() string { return desktop.SettingsPath() }

// SyncToken is the peer-facing token this device accepts, or "".
func (b *embeddedBackend) SyncToken() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settings.SyncToken
}

// SetSyncToken persists the peer-facing token and applies it to the running
// server without a restart. Empty stops accepting peers.
func (b *embeddedBackend) SetSyncToken(token string) error {
	b.mu.Lock()
	settings := b.settings
	settings.SyncToken = token
	if err := desktop.SaveSettings(settings); err != nil {
		b.mu.Unlock()
		return fmt.Errorf("保存设置失败: %w", err)
	}
	b.settings = settings
	srv := b.server
	b.mu.Unlock()
	if srv != nil {
		srv.SetSyncToken(token)
	}
	return nil
}
