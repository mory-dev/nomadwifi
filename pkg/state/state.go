// Package state persists what NomadWiFi has learned between runs: which Wi-Fi
// profiles it created, which passwords turned out to be wrong, and which
// networks recently failed to carry traffic.
//
// Without this, a guessed password that does not work is written into Windows
// once per scan and never cleaned up, and a dead access point is retried
// forever.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProvisionedProfile records a Windows profile that NomadWiFi created, so it
// can be verified, retried, or removed later.
type ProvisionedProfile struct {
	SSID        string    `json:"ssid"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	Verified    bool      `json:"verified"`
	Failures    int       `json:"failures"`
	LastFailure time.Time `json:"last_failure,omitempty"`
}

// Penalty keeps a network out of the roaming ladder for a while after it fails.
type Penalty struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

// State is the on-disk document.
type State struct {
	InterfaceName string                         `json:"interface_name,omitempty"`
	Profiles      map[string]*ProvisionedProfile `json:"profiles"`
	Penalties     map[string]*Penalty            `json:"penalties"`
	LastVenue     string                         `json:"last_venue,omitempty"`
	// LastUpdateCheck throttles the release check. Persisting it means the
	// check is once a day per machine, not once per launch.
	LastUpdateCheck time.Time `json:"last_update_check,omitempty"`
	// UpdateDismissed is the version the user said no to, so the prompt does
	// not reappear for a release they have already declined.
	UpdateDismissed string    `json:"update_dismissed,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var (
	mu      sync.Mutex
	current *State
)

// Dir returns the NomadWiFi data directory, creating it if needed.
//
// The fallback matters more than it looks: once NomadWiFi is installed rather
// than unzipped, the working directory is the install tree, which a normal user
// cannot write to. Falling back to "." would silently attempt to create
// .nomadwifi inside it and -- since every write error here is swallowed -- lose
// all learned state without saying anything.
func Dir() string {
	base, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(base) == "" {
		// LOCALAPPDATA is per-user and always writable when it is set at all.
		base = os.Getenv("LOCALAPPDATA")
	}
	if strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}

	dir := filepath.Join(base, ".nomadwifi")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// Path returns the location of the state document.
func Path() string {
	return filepath.Join(Dir(), "state.json")
}

func blank() *State {
	return &State{
		Profiles:  map[string]*ProvisionedProfile{},
		Penalties: map[string]*Penalty{},
	}
}

// load reads the state document once per process. A missing or corrupt file is
// not an error: NomadWiFi simply starts with no memory.
func load() *State {
	if current != nil {
		return current
	}

	current = blank()
	data, err := os.ReadFile(Path())
	if err != nil {
		return current
	}

	var loaded State
	if err := json.Unmarshal(data, &loaded); err != nil {
		return current
	}
	if loaded.Profiles == nil {
		loaded.Profiles = map[string]*ProvisionedProfile{}
	}
	if loaded.Penalties == nil {
		loaded.Penalties = map[string]*Penalty{}
	}
	current = &loaded
	return current
}

func save() {
	if current == nil {
		return
	}
	current.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return
	}

	// Write through a temporary file so an interrupted write cannot leave a
	// truncated document behind.
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, Path())
}

func key(ssid string) string {
	return strings.ToLower(strings.TrimSpace(ssid))
}

// InterfaceName returns the cached wireless adapter name, if one is known.
func InterfaceName() string {
	mu.Lock()
	defer mu.Unlock()
	return load().InterfaceName
}

// SetInterfaceName remembers the adapter name so later runs skip resolving it.
func SetInterfaceName(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if s.InterfaceName == name {
		return
	}
	s.InterfaceName = name
	save()
}

// RecordProvisioned notes that NomadWiFi created a profile for this network.
func RecordProvisioned(ssid, source string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	k := key(ssid)
	if existing, ok := s.Profiles[k]; ok {
		existing.Source = source
		save()
		return
	}
	s.Profiles[k] = &ProvisionedProfile{SSID: ssid, Source: source, CreatedAt: time.Now()}
	save()
}

// MarkVerified records that a provisioned profile successfully associated,
// which is the only evidence that an inferred password was actually right.
func MarkVerified(ssid string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	p, ok := s.Profiles[key(ssid)]
	if !ok {
		return
	}
	p.Verified = true
	p.Failures = 0
	save()
}

// RecordFailure counts a failed association and reports the running total.
func RecordFailure(ssid string) int {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	k := key(ssid)
	p, ok := s.Profiles[k]
	if !ok {
		p = &ProvisionedProfile{SSID: ssid, CreatedAt: time.Now()}
		s.Profiles[k] = p
	}
	p.Failures++
	p.LastFailure = time.Now()
	save()
	return p.Failures
}

// Provisioned returns the record for a network NomadWiFi provisioned, if any.
func Provisioned(ssid string) (*ProvisionedProfile, bool) {
	mu.Lock()
	defer mu.Unlock()
	p, ok := load().Profiles[key(ssid)]
	if !ok {
		return nil, false
	}
	copyOf := *p
	return &copyOf, true
}

// ForgetProfile drops the record for a profile that has been deleted.
func ForgetProfile(ssid string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	delete(s.Profiles, key(ssid))
	save()
}

// UnverifiedProfiles lists provisioned profiles that never associated
// successfully, ordered arbitrarily. These are the garbage-collection targets.
func UnverifiedProfiles(minFailures int) []ProvisionedProfile {
	mu.Lock()
	defer mu.Unlock()
	var out []ProvisionedProfile
	for _, p := range load().Profiles {
		if !p.Verified && p.Failures >= minFailures {
			out = append(out, *p)
		}
	}
	return out
}

// Penalize keeps a network out of the roaming ladder for the given duration.
func Penalize(ssid string, d time.Duration, reason string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	s.Penalties[key(ssid)] = &Penalty{Until: time.Now().Add(d), Reason: reason}
	save()
}

// IsPenalized reports whether a network is currently benched, and why.
func IsPenalized(ssid string) (bool, string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	k := key(ssid)
	p, ok := s.Penalties[k]
	if !ok {
		return false, ""
	}
	if time.Now().After(p.Until) {
		delete(s.Penalties, k)
		save()
		return false, ""
	}
	return true, p.Reason
}

// ClearPenalty lifts a benching early, for example after a successful connect.
func ClearPenalty(ssid string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if _, ok := s.Penalties[key(ssid)]; ok {
		delete(s.Penalties, key(ssid))
		save()
	}
}

// LastVenue returns the venue root NomadWiFi last warmed profiles for.
func LastVenue() string {
	mu.Lock()
	defer mu.Unlock()
	return load().LastVenue
}

// SetLastVenue records the venue root so a warm pass can be skipped when
// nothing has changed.
func SetLastVenue(venue string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if s.LastVenue == venue {
		return
	}
	s.LastVenue = venue
	save()
}

// LastUpdateCheck reports when the release feed was last consulted.
func LastUpdateCheck() time.Time {
	mu.Lock()
	defer mu.Unlock()
	return load().LastUpdateCheck
}

// MarkUpdateChecked records that the release feed was just consulted, whatever
// the outcome: a failed check should still back off rather than retry in a loop.
func MarkUpdateChecked() {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	s.LastUpdateCheck = time.Now()
	save()
}

// UpdateDismissed reports the version the user last declined, if any.
func UpdateDismissed() string {
	mu.Lock()
	defer mu.Unlock()
	return load().UpdateDismissed
}

// DismissUpdate silences the prompt for one specific version. A later release
// still prompts, because the stored version no longer matches.
func DismissUpdate(version string) {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	s.UpdateDismissed = version
	save()
}

// Reset clears all persisted state. Used by tests and by an explicit reset.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	current = blank()
	save()
}
