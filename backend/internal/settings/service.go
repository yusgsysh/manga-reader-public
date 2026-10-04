package settings

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

// Hooks push a configuration that has already been validated into the running
// process. app.New supplies the ones it can reach; a nil hook simply means
// that setting is not hot-swappable in this build.
type Hooks struct {
	// Storage rebuilds the object store. It runs before the database write so
	// an unreachable endpoint is reported as a rejected save instead of being
	// persisted.
	Storage func(config.StorageConfig) error
	// Cookies replaces the ExHentai cookie jar.
	Cookies func(config.CookieConfig) error
	// LogLevel changes the active slog level.
	LogLevel func(level string)
	// DevTools toggles the /api/dev/* endpoints.
	DevTools func(enabled bool)
}

// Service reads, validates and applies persisted settings.
//
// Load must be called once during startup before any other method. Every other
// method is safe for concurrent use.
type Service struct {
	client *ent.Client
	logger *slog.Logger

	mu        sync.Mutex
	env       *config.Config
	saved     map[string]string
	active    *config.Config
	persisted bool
	hooks     Hooks
}

// New creates a Service backed by client.
func New(client *ent.Client, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{client: client, logger: logger}
}

// lookup layers overrides on top of the process environment: a key the settings
// page has saved always wins, and an override of "" is still an override.
func lookup(overrides map[string]string) config.EnvLookup {
	return func(key string) string {
		if v, ok := overrides[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}

// Load merges persisted overrides into base (the configuration derived from
// the process environment) and remembers both as the active settings.
//
// It always returns a usable configuration: when the persisted values are
// rejected, base is returned alongside a non-nil error, because a bad row must
// never keep the settings page from booting.
func (s *Service) Load(ctx context.Context, base *config.Config) (*config.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if base == nil {
		return nil, fmt.Errorf("settings: base configuration is nil")
	}

	saved, err := readOverrides(ctx, s.client)
	if err != nil {
		s.env = base
		s.active = base
		s.saved = map[string]string{}
		s.persisted = false
		return base, err
	}

	merged, loadErr := config.LoadWith(lookup(saved))
	if loadErr != nil {
		s.logger.Error("persisted settings rejected, running with the environment configuration",
			"error", loadErr)
		merged = base
	}

	s.env = base
	s.saved = saved
	s.active = merged
	s.persisted = len(saved) > 0
	return merged, loadErr
}

// Bind registers the hooks used to apply later updates. It is called once,
// after the process has been built from the loaded configuration.
func (s *Service) Bind(h Hooks) {
	s.mu.Lock()
	s.hooks = h
	s.mu.Unlock()
}

// Get returns the active settings with secrets masked.
func (s *Service) Get() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotOf(s.active, s.persisted)
}

// Update applies in on top of the active settings, validates the result,
// pushes it into the running process and persists only the keys that differ
// from the environment.
func (s *Service) Update(ctx context.Context, in Update) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active == nil || s.env == nil {
		return Snapshot{}, fmt.Errorf("settings not loaded")
	}

	envCfg := s.env
	next := *s.active
	applyOverlay(&next, in)

	// Only keys whose effective value moved away from the environment are
	// stored; anything that matches it again is dropped so later edits to .env
	// keep working for untouched settings.
	base := flatten(envCfg)
	want := flatten(&next)
	overrides := make(map[string]string, len(config.ManagedKeys))
	for _, key := range config.ManagedKeys {
		if want[key] != base[key] {
			overrides[key] = want[key]
		}
	}

	merged, err := config.LoadWith(lookup(overrides))
	if err != nil {
		return Snapshot{}, &InvalidError{err}
	}
	if err := validate(merged); err != nil {
		return Snapshot{}, &InvalidError{err}
	}

	prev := s.active
	if err := s.apply(prev, merged); err != nil {
		return Snapshot{}, &InvalidError{err}
	}
	if err := writeOverrides(ctx, s.client, overrides); err != nil {
		// Put the process back the way it was so it keeps matching the stored
		// configuration.
		if revertErr := s.apply(merged, prev); revertErr != nil {
			s.logger.Error("failed to roll back settings after a save error", "error", revertErr)
		}
		return Snapshot{}, err
	}

	s.saved = overrides
	s.persisted = len(overrides) > 0
	s.active = merged
	return snapshotOf(merged, s.persisted), nil
}

// Overrides reports the persisted key/value pairs. It exists for tests and
// diagnostics.
func (s *Service) Overrides() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.saved))
	for k, v := range s.saved {
		out[k] = v
	}
	return out
}

// apply pushes every changed section into the process. Only Storage can fail:
// it talks to the network. The other hooks are in-process swaps.
func (s *Service) apply(prev, next *config.Config) error {
	h := s.hooks
	if h.Storage != nil && prev.Storage != next.Storage {
		if err := h.Storage(next.Storage); err != nil {
			return fmt.Errorf("storage settings rejected: %w", err)
		}
	}
	if h.Cookies != nil && prev.Cookie != next.Cookie {
		if err := h.Cookies(next.Cookie); err != nil {
			s.logger.Error("cookie swap failed", "error", err)
		}
	}
	if h.LogLevel != nil && prev.LogLevel != next.LogLevel {
		h.LogLevel(next.LogLevel)
	}
	if h.DevTools != nil && prev.DevTools != next.DevTools {
		h.DevTools(next.DevTools)
	}
	return nil
}

// snapshotOf renders a configuration for the API with secrets masked.
func snapshotOf(c *config.Config, persisted bool) Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return Snapshot{
		Persisted: persisted,
		Cookie: CookieView{
			MemberID:   c.Cookie.IpbMemberID,
			PassHash:   mask(c.Cookie.IpbPassHash),
			Igneous:    mask(c.Cookie.Igneous),
			SK:         mask(c.Cookie.SK),
			Configured: c.Cookie.IsValid(),
		},
		Storage: StorageView{
			Driver:         c.Storage.Driver,
			ResolvedDriver: c.Storage.ResolvedDriver(),
			Dir:            c.Storage.Dir,
			S3: S3View{
				Endpoint:  c.Storage.S3.Endpoint,
				Region:    c.Storage.S3.Region,
				Bucket:    c.Storage.S3.Bucket,
				AccessKey: c.Storage.S3.AccessKey,
				SecretKey: mask(c.Storage.S3.SecretKey),
				UseSSL:    c.Storage.S3.UseSSL,
				PathStyle: c.Storage.S3.PathStyle,
			},
		},
		LogLevel: c.LogLevel,
		DevTools: c.DevTools,
	}
}

func mask(v string) string {
	if v == "" {
		return ""
	}
	return Mask
}
