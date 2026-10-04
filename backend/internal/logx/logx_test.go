package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// The base handler deliberately accepts everything; gating is Dynamic's job,
// otherwise raising the level later would have nothing to reveal.
func TestDynamicGatesRecordsOnTheCurrentLevel(t *testing.T) {
	var buf bytes.Buffer
	d := New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), slog.LevelWarn)
	logger := d.Logger()

	logger.Debug("debug-should-be-hidden")
	logger.Info("info-should-be-hidden")
	logger.Warn("warn-should-show")

	out := buf.String()
	if strings.Contains(out, "should-be-hidden") {
		t.Errorf("output = %s, want only records at or above warn", out)
	}
	if !strings.Contains(out, "warn-should-show") {
		t.Errorf("output = %s, want the warn record", out)
	}

	d.SetLevel(slog.LevelInfo)
	logger.Info("info-should-show")
	if !strings.Contains(buf.String(), "info-should-show") {
		t.Errorf("output = %s, want info visible after SetLevel", buf.String())
	}

	d.SetLevel(slog.LevelError)
	logger.Warn("warn-should-hide-now")
	if strings.Contains(buf.String(), "warn-should-hide-now") {
		t.Errorf("output = %s, want warn hidden after raising the level", buf.String())
	}
}

func TestDynamicLevelIsSharedWithDerivedHandlers(t *testing.T) {
	var buf bytes.Buffer
	d := New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), slog.LevelError)
	derived := d.Logger().With("component", "test")

	derived.Info("hidden")
	if strings.Contains(buf.String(), "hidden") {
		t.Errorf("output = %s, want nothing at error level", buf.String())
	}

	d.SetLevel(slog.LevelInfo)
	derived.Info("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Errorf("output = %s, want the derived logger to follow the new level", buf.String())
	}
	if d.Level() != slog.LevelInfo {
		t.Errorf("Level() = %s, want info", d.Level())
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{" info ", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelWarn},
		{"nonsense", slog.LevelWarn},
	}
	for _, tc := range tests {
		if got := ParseLevel(tc.in); got != tc.want {
			t.Errorf("ParseLevel(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
