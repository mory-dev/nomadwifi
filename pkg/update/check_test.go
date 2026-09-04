package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		current, candidate string
		want               bool
	}{
		{"1.2.1", "1.3.0", true},
		{"1.2.1", "1.2.2", true},
		{"1.2.1", "2.0.0", true},
		{"1.2.1", "1.2.1", false},
		{"1.3.0", "1.2.9", false},
		{"1.2.1", "v1.3.0", true}, // a leading v is tolerated on either side
		{"v1.2.1", "1.3.0", true},
		{"1.2", "1.2.1", true},     // two-part versions are accepted
		{"1.10.0", "1.9.0", false}, // compared numerically, not as strings
		{"1.9.0", "1.10.0", true},
		{"1.2.1-rc1", "1.2.1", false}, // suffixes are ignored, so these are equal
		{"", "1.3.0", false},          // a dev build is never told to update
		{"1.2.1", "", false},
		{"1.2.1", "not-a-version", false},
	}

	for _, tc := range tests {
		if got := Newer(tc.current, tc.candidate); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
		}
	}
}

// The installer is executed after downloading, so a payload that does not match
// the published digest must never reach the caller.
func TestDownloadRejectsAChecksumMismatch(t *testing.T) {
	payload := []byte("this is not the installer you are looking for")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	r := &Release{
		Version:      "9.9.9",
		InstallerURL: srv.URL,
		SHA256:       hex.EncodeToString(sha256.New().Sum(nil)), // digest of empty input
	}

	path, err := Download(context.Background(), r)
	if err == nil {
		t.Fatalf("expected a checksum mismatch to fail, got %q", path)
	}
	if path != "" {
		t.Errorf("a rejected download must not return a path, got %q", path)
	}
}

func TestDownloadAcceptsAMatchingChecksum(t *testing.T) {
	payload := []byte("pretend installer bytes")
	sum := sha256.Sum256(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	r := &Release{Version: "9.9.9", InstallerURL: srv.URL, SHA256: hex.EncodeToString(sum[:])}

	path, err := Download(context.Background(), r)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(path) })

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded content does not match what was served")
	}
}

func TestDownloadRefusesWithoutAnExpectedChecksum(t *testing.T) {
	if _, err := Download(context.Background(), &Release{InstallerURL: "http://example.invalid"}); err == nil {
		t.Error("expected a download with no expected checksum to be refused")
	}
}

// A release that has no installer asset cannot be applied, so offering it would
// be a dead end for the user.
func TestCheckIgnoresAReleaseWithNoInstaller(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	body, _ := json.Marshal(ghRelease{
		TagName: "v9.9.9",
		Assets: []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}{{Name: "something-else.zip", URL: "http://example.invalid"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	rel, err := checkAt(context.Background(), "1.0.0", srv.URL)
	if err != nil {
		t.Fatalf("checkAt: %v", err)
	}
	if rel != nil {
		t.Errorf("expected no update to be offered, got %+v", rel)
	}
}

func TestFetchChecksumFindsTheRightAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "aaaa  nomadwifi-cli-windows.zip\nbbbb  NomadWiFi-Setup.exe\ncccc  NomadWiFi-windows.zip\n")
	}))
	defer srv.Close()

	got, err := fetchChecksum(context.Background(), srv.Client(), srv.URL, "NomadWiFi-Setup.exe")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if got != "bbbb" {
		t.Errorf("checksum = %q, want %q", got, "bbbb")
	}
}
