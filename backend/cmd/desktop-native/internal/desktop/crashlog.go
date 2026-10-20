package desktop

import (
	"os"
	"path/filepath"
	"runtime/debug"
)

// CrashLogPath is the file panics are written to: next to config.json in the
// user config directory, so a user can find it without knowing where logs
// usually live. "" when no config directory can be resolved.
func CrashLogPath() string {
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crash.log")
}

// InstallCrashLog directs runtime crashes (unrecovered panics) to a crash
// log file: a GUI app usually has no visible stderr, so a crash would
// otherwise vanish. The file is appended, so a second crash keeps both
// traces, and each is timestamped by the runtime's own header. The fd is
// duplicated by the runtime, so the handle never needs closing.
//
// It returns the log path, or "" when none could be set up (the caller then
// only logs a note).
func InstallCrashLog() string {
	path := CrashLogPath()
	if path == "" {
		return ""
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ""
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_ = f.Close()
		return ""
	}
	// f stays open for the process lifetime: the runtime keeps writing
	// panics through its duplicated descriptor.
	return path
}
