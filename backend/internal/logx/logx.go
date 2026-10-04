// Package logx provides a slog handler whose level can be changed while the
// process is running, so LOG_LEVEL is a live setting rather than a boot-only
// environment variable.
package logx

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
)

// level holds the currently enabled minimum severity. Handlers derived from
// the same Dynamic share it, so a change reaches every child logger.
type level struct {
	v atomic.Int64
}

// Dynamic wraps a slog.Handler and gates records on a mutable level.
type Dynamic struct {
	base slog.Handler
	lvl  *level
}

var _ slog.Handler = (*Dynamic)(nil)

// New returns a logger writing through base, initially at level. base must be
// configured to accept at least the lowest level you intend to enable
// (slog.LevelDebug); Dynamic does the gating.
func New(base slog.Handler, lvl slog.Level) *Dynamic {
	d := &Dynamic{base: base, lvl: &level{}}
	d.lvl.v.Store(int64(lvl))
	return d
}

// Logger returns a slog.Logger backed by this handler.
func (d *Dynamic) Logger() *slog.Logger {
	return slog.New(d)
}

// SetLevel switches the minimum severity for this handler and every handler
// derived from it.
func (d *Dynamic) SetLevel(lvl slog.Level) {
	d.lvl.v.Store(int64(lvl))
}

// Level reports the current minimum severity.
func (d *Dynamic) Level() slog.Level {
	return slog.Level(d.lvl.v.Load())
}

func (d *Dynamic) Enabled(_ context.Context, l slog.Level) bool {
	return l >= slog.Level(d.lvl.v.Load())
}

func (d *Dynamic) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.Level(d.lvl.v.Load()) {
		return nil
	}
	return d.base.Handle(ctx, r)
}

func (d *Dynamic) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Dynamic{base: d.base.WithAttrs(attrs), lvl: d.lvl}
}

func (d *Dynamic) WithGroup(name string) slog.Handler {
	return &Dynamic{base: d.base.WithGroup(name), lvl: d.lvl}
}

// ParseLevel maps the LOG_LEVEL vocabulary to a slog.Level. Unknown values fall
// back to warn, matching the historical behaviour of the web entrypoint.
func ParseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}
