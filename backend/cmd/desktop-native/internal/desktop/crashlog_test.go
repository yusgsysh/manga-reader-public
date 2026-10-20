package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrashLogPathIsInConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Skip("no config dir in this environment")
	}
	if got := CrashLogPath(); got != filepath.Join(dir, "crash.log") {
		t.Fatalf("crash log path = %q, want %q", got, filepath.Join(dir, "crash.log"))
	}
}

func TestInstallCrashLogCreatesWritableFile(t *testing.T) {
	path := InstallCrashLog()
	if path == "" {
		t.Skip("no config dir in this environment")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("crash log file: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("crash log mode = %v, want 0600", st.Mode().Perm())
	}
	// The file is appended to by the runtime; a manual write proves it is
	// on disk and writable (the test's own marker, not a crash).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("crashlog-test-marker\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, err := os.ReadFile(path)
	if err != nil || !osContains(data, "crashlog-test-marker") {
		t.Fatalf("marker not found (err=%v)", err)
	}
}

func osContains(data []byte, sub string) bool {
	return len(data) >= len(sub) && filepath.Separator != 0 && bytesContains(data, sub)
}

func bytesContains(data []byte, sub string) bool {
	for i := 0; i+len(sub) <= len(data); i++ {
		if string(data[i:i+len(sub)]) == sub {
			return true
		}
	}
	return false
}
