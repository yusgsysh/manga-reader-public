package desktop

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// keyring stores the ExHentai cookie in the OS credential store when one is
// available, falling back to the 0600 config file. The cookie is the only
// secret the app holds; the keyring keeps it out of a plain-text file for
// users whose desktop provides one.
//
// Each OS uses its stock CLI, so no CGO is needed:
//   - macOS:  security(1) generic passwords
//   - Linux:  secret-tool (libsecret, the GNOME/KDE keyring)
//   - Windows: cmdkey is write-only for generic credentials; PowerShell's
//     CredentialManager is not stock. Windows therefore always uses the file
//     (NTFS ACLs on the user profile protect it).
const (
	keyringService = "manga-reader"
	keyringAccount = "exhentai-cookie"
)

// keyringTimeout bounds the external tool call; a hung keyring daemon must
// not hang startup.
const keyringTimeout = 5 * time.Second

// ErrKeyringUnavailable reports that no usable credential store answered.
// Callers fall back to the config file.
var ErrKeyringUnavailable = errors.New("系统钥匙串不可用")

// probeKeyring remembers whether the store answered once, so the settings
// can show which storage the cookie uses without running a tool per frame.
var (
	keyringMu     sync.Mutex
	keyringUsable bool
	keyringProbed bool
)

// KeyringAvailable reports whether the OS credential store answered to a
// probe. One process, one probe; a round-trip of a sentinel proves it.
const keyringProbeValue = "manga-reader-keyring-probe"

func KeyringAvailable() bool {
	keyringMu.Lock()
	defer keyringMu.Unlock()
	if !keyringProbed {
		keyringProbed = true
		if err := keyringSave(keyringProbeValue); err == nil {
			got, err := keyringLoad()
			keyringUsable = err == nil && got == keyringProbeValue
			_ = keyringDelete()
		}
	}
	return keyringUsable
}

// keyringSave stores the cookie in the OS credential store. It reports
// ErrKeyringUnavailable when the platform has no working store.
func keyringSave(cookie string) error {
	switch runtime.GOOS {
	case "darwin":
		return keyringRun("security", "add-generic-password", "-U",
			"-s", keyringService, "-a", keyringAccount, "-w", cookie)
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ErrKeyringUnavailable
		}
		return keyringRun("secret-tool", "store", "--label="+keyringService,
			"service", keyringService, "account", keyringAccount, cookie)
	default:
		return ErrKeyringUnavailable
	}
}

// keyringLoad reads the cookie back, or ErrKeyringUnavailable.
func keyringLoad() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := keyringOutput("security", "find-generic-password",
			"-s", keyringService, "-a", keyringAccount, "-w")
		if err != nil {
			return "", err
		}
		return strings.TrimRight(out, "\n"), nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return "", ErrKeyringUnavailable
		}
		out, err := keyringOutput("secret-tool", "lookup",
			"service", keyringService, "account", keyringAccount)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(out, "\n"), nil
	default:
		return "", ErrKeyringUnavailable
	}
}

// keyringDelete removes the stored cookie; a missing entry is fine.
func keyringDelete() error {
	switch runtime.GOOS {
	case "darwin":
		// item-not-found (45) means there is nothing to delete.
		if err := keyringRun("security", "delete-generic-password",
			"-s", keyringService, "-a", keyringAccount); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 45 {
				return nil
			}
			return err
		}
		return nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ErrKeyringUnavailable
		}
		return keyringRun("secret-tool", "clear", "service", keyringService, "account", keyringAccount)
	default:
		return ErrKeyringUnavailable
	}
}

// keyringRun runs a store command that reads nothing back.
func keyringRun(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := runWithTimeout(cmd); err != nil {
		return wrapKeyringErr(err)
	}
	return nil
}

// keyringOutput runs a store command and returns its stdout.
func keyringOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := runWithTimeout(cmd); err != nil {
		return "", wrapKeyringErr(err)
	}
	return out.String(), nil
}

// runWithTimeout runs cmd, killing it when it exceeds keyringTimeout.
func runWithTimeout(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(keyringTimeout):
		_ = cmd.Process.Kill()
		<-done
		return ErrKeyringUnavailable
	}
}

// wrapKeyringErr turns tool failures into ErrKeyringUnavailable with the
// cause logged alongside, so callers can branch on one error.
func wrapKeyringErr(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return ErrKeyringUnavailable
	}
	return err
}
