package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Once NomadWiFi is installed rather than unzipped, the working directory is
// the install tree, which a normal user cannot write to. Dir() must never
// resolve to a relative path, because every write through it is best-effort and
// the failure would be silent.
func TestDirIsAlwaysAbsolute(t *testing.T) {
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	t.Setenv("LOCALAPPDATA", t.TempDir())

	dir := Dir()
	if !filepath.IsAbs(dir) {
		t.Fatalf("Dir() = %q, want an absolute path", dir)
	}
	if strings.HasPrefix(dir, ".") {
		t.Errorf("Dir() = %q, must not be relative to the working directory", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("Dir() = %q was not created: %v", dir, err)
	}
}

func TestDirPrefersTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	if got, want := Dir(), filepath.Join(home, ".nomadwifi"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

// With no home and no LOCALAPPDATA there is still nowhere relative that is
// acceptable, so it lands in the temp directory rather than ".".
func TestDirFallsBackToTempNotWorkingDirectory(t *testing.T) {
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	t.Setenv("LOCALAPPDATA", "")

	dir := Dir()
	if !filepath.IsAbs(dir) {
		t.Fatalf("Dir() = %q, want an absolute path", dir)
	}
	if !strings.HasPrefix(dir, filepath.Clean(os.TempDir())) {
		t.Errorf("Dir() = %q, want it under %q", dir, os.TempDir())
	}
}

// The app starts with Windows, so a preference that silently reverts to "on"
// would roam against the user's wishes without them ever seeing it happen.
func TestAutoRoamDefaultsOnButRemembersOff(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	Reset()

	if !AutoRoam() {
		t.Error("with no preference stored, auto-roam should default to on")
	}

	SetAutoRoam(false)
	if AutoRoam() {
		t.Error("auto-roam should be off after being turned off")
	}

	// Simulate a restart: drop the in-process cache and read the file again.
	mu.Lock()
	current = nil
	mu.Unlock()

	if AutoRoam() {
		t.Error("auto-roam was turned off but came back on after a restart")
	}

	SetAutoRoam(true)
	mu.Lock()
	current = nil
	mu.Unlock()
	if !AutoRoam() {
		t.Error("auto-roam should be on again after being re-enabled")
	}
}
