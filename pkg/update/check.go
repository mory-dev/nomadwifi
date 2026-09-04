// Package update checks whether a newer NomadWiFi release exists.
//
// It deliberately does not apply updates on its own. NomadWiFi manages the
// user's network connection, so an unattended restart at the wrong moment is
// worse than a version that is a few days old. The caller is expected to ask.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/state"
)

const (
	// releasesURL is the public feed. Unauthenticated GitHub API calls are rate
	// limited per IP, which the once-a-day throttle keeps us well inside.
	releasesURL = "https://api.github.com/repos/mory-dev/nomadwifi/releases/latest"

	// installerAsset is the version-less asset the release workflow publishes
	// precisely so this URL never has to be rebuilt per version.
	installerAsset = "NomadWiFi-Setup.exe"
	checksumAsset  = "SHA256SUMS.txt"

	// CheckInterval is how often the feed is consulted at most.
	CheckInterval = 24 * time.Hour

	requestTimeout  = 15 * time.Second
	downloadTimeout = 10 * time.Minute
)

// Release describes a newer version that is available to install.
type Release struct {
	Version        string    `json:"version"`
	CurrentVersion string    `json:"current_version"`
	Notes          string    `json:"notes,omitempty"`
	InstallerURL   string    `json:"installer_url"`
	SHA256         string    `json:"sha256"`
	PublishedAt    time.Time `json:"published_at"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Due reports whether enough time has passed to consult the feed again.
func Due() bool {
	return time.Since(state.LastUpdateCheck()) >= CheckInterval
}

// Check asks GitHub for the newest release and reports it only when it is
// actually newer than current. A nil Release with a nil error means up to date.
//
// current is the version the running binary was built with. An empty or
// unparseable value means a development build, which is never told to update:
// a dev binary is usually ahead of the last release, not behind it.
func Check(ctx context.Context, current string) (*Release, error) {
	state.MarkUpdateChecked()
	return checkAt(ctx, current, releasesURL)
}

// checkAt is Check with the feed address injected, so tests can serve their own
// release document instead of reaching GitHub.
func checkAt(ctx context.Context, current, feedURL string) (*Release, error) {
	if _, ok := parseVersion(current); !ok {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NomadWiFi/"+current)

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release feed returned %s", resp.Status)
	}

	var gh ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gh); err != nil {
		return nil, err
	}
	if gh.Draft || gh.Prerelease {
		return nil, nil
	}

	latest := strings.TrimPrefix(strings.TrimSpace(gh.TagName), "v")
	if !Newer(current, latest) {
		return nil, nil
	}

	var installerURL, checksumURL string
	for _, a := range gh.Assets {
		switch a.Name {
		case installerAsset:
			installerURL = a.URL
		case checksumAsset:
			checksumURL = a.URL
		}
	}
	if installerURL == "" {
		// A release without the installer asset is not installable by this
		// path; reporting it would offer an update that cannot be applied.
		return nil, nil
	}

	sum, err := fetchChecksum(ctx, client, checksumURL, installerAsset)
	if err != nil {
		return nil, err
	}

	return &Release{
		Version:        latest,
		CurrentVersion: current,
		Notes:          strings.TrimSpace(gh.Body),
		InstallerURL:   installerURL,
		SHA256:         sum,
		PublishedAt:    gh.PublishedAt,
	}, nil
}

// fetchChecksum pulls the expected digest for one asset out of SHA256SUMS.txt.
// Without it the downloaded installer could not be validated before it runs,
// and running an unvalidated executable is the one thing this must not do.
func fetchChecksum(ctx context.Context, client *http.Client, url, asset string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("release has no %s to verify the download against", checksumAsset)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum file returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && strings.EqualFold(fields[1], asset) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", asset)
}

// Download fetches the installer to a temporary file and verifies its digest
// before returning the path. A mismatch deletes the file and errors, so a
// caller can never be handed something it should not execute.
func Download(ctx context.Context, r *Release) (string, error) {
	if r == nil || r.InstallerURL == "" {
		return "", fmt.Errorf("no installer to download")
	}
	if r.SHA256 == "" {
		return "", fmt.Errorf("refusing to download an installer with no expected checksum")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.InstallerURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("installer download returned %s", resp.Status)
	}

	dir, err := os.MkdirTemp("", "nomadwifi-update-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("NomadWiFi-Setup-%s.exe", r.Version))

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}

	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, digest), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.RemoveAll(dir)
		return "", copyErr
	}
	if closeErr != nil {
		os.RemoveAll(dir)
		return "", closeErr
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if !strings.EqualFold(got, r.SHA256) {
		os.RemoveAll(dir)
		return "", fmt.Errorf("installer checksum mismatch: expected %s, got %s", r.SHA256, got)
	}
	return path, nil
}

// Newer reports whether candidate is a later version than current.
func Newer(current, candidate string) bool {
	c, okC := parseVersion(current)
	n, okN := parseVersion(candidate)
	if !okC || !okN {
		return false
	}
	for i := 0; i < 3; i++ {
		if n[i] != c[i] {
			return n[i] > c[i]
		}
	}
	return false
}

// parseVersion reads a major.minor.patch triple, ignoring any pre-release or
// build suffix. Anything else -- including the empty version of an untagged
// dev build -- is rejected rather than guessed at.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int

	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return out, false
	}
	// Drop a pre-release or build suffix: 1.3.0-rc1 compares as 1.3.0.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}

	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
