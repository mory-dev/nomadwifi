package wifi

import (
	"fmt"
	"strings"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/state"
)

// Warming pre-creates the Windows profile for networks NomadWiFi expects to
// roam to, so a failover is a single connect call rather than a connect plus a
// profile round-trip.
//
// It deliberately does not run during a scan. Writing a profile costs a netsh
// launch, and doing that for every locked access point on every scan is what
// made scanning take seconds. A warm pass runs on its own schedule and only
// covers the handful of networks that are actually roam candidates.

// DefaultWarmSetSize is how many candidates are kept hot at once.
const DefaultWarmSetSize = 4

// WarmResult describes one warming attempt.
type WarmResult struct {
	SSID    string `json:"ssid"`
	Warmed  bool   `json:"warmed"`
	Skipped string `json:"skipped,omitempty"`
	Source  string `json:"source,omitempty"`
	Error   string `json:"error,omitempty"`
}

// MarkWarmStatus flags the access points NomadWiFi prepared itself.
//
// This deliberately does not mean "a Windows profile exists", which is
// already what AuthStatusSaved says. It means NomadWiFi worked out the key
// and provisioned the profile, which is the part worth surfacing.
func MarkWarmStatus(aps []AccessPoint) {
	for i := range aps {
		_, provisioned := state.Provisioned(aps[i].SSID)
		aps[i].IsWarm = provisioned
	}
}

// WarmCandidates picks the networks worth pre-provisioning: the best-scoring
// connectable access points in this venue that Windows has no profile for.
func WarmCandidates(aps []AccessPoint, limit int) []AccessPoint {
	if limit <= 0 {
		limit = DefaultWarmSetSize
	}
	saved := savedProfileSet()

	var out []AccessPoint
	for _, ap := range aps {
		if len(out) >= limit {
			break
		}
		if ap.IsHidden() || !ap.AuthStatus.Connectable() {
			continue
		}
		if saved[strings.ToLower(strings.TrimSpace(ap.SSID))] {
			continue // Windows already has a profile
		}
		if benched, _ := state.IsPenalized(ap.SSID); benched {
			continue
		}
		out = append(out, ap)
	}
	return out
}

// WarmVenue provisions profiles for the current venue's roam candidates.
func WarmVenue(aps []AccessPoint, limit int) []WarmResult {
	candidates := WarmCandidates(aps, limit)
	results := make([]WarmResult, 0, len(candidates))

	for _, ap := range candidates {
		results = append(results, warmOne(ap))
	}

	if len(aps) > 0 {
		state.SetLastVenue(ExtractVenueRoot(aps[0].SSID))
	}
	return results
}

func warmOne(ap AccessPoint) WarmResult {
	res := WarmResult{SSID: ap.SSID}

	security := SecurityForNetwork(ap.Authentication, ap.Cipher)
	if !security.SupportsAutoProfile() {
		res.Skipped = "enterprise network needs manual setup"
		return res
	}

	password := ""
	source := "no key needed on an open network"
	if security.NeedsPassword() {
		pwd, from := GuessPasswordForSSID(ap.SSID)
		if pwd == "" {
			res.Skipped = "no known or inferable password"
			return res
		}
		password, source = pwd, "key from "+from
	}

	if err := AddWifiProfileFor(ap.SSID, password, security, ap.IsHidden()); err != nil {
		res.Error = err.Error()
		return res
	}

	state.RecordProvisioned(ap.SSID, source)
	res.Warmed = true
	res.Source = source
	return res
}

// maxWarmFailures is how many failed associations a guessed profile gets
// before it is removed. Venue password inference is a guess, and a wrong guess
// left in Windows is a profile that fails forever.
const maxWarmFailures = 2

// NoteAssociationSuccess confirms an inferred password was correct.
func NoteAssociationSuccess(ssid string) {
	state.MarkVerified(ssid)
	state.ClearPenalty(ssid)
}

// NoteAssociationFailure records a failed association and removes the profile
// once the guess has clearly not worked out.
func NoteAssociationFailure(ssid, reason string) {
	rec, provisioned := state.Provisioned(ssid)
	if !provisioned || rec.Verified {
		// A profile the user saved themselves is not ours to delete; bench it
		// briefly so the roam ladder moves on.
		state.Penalize(ssid, 5*time.Minute, reason)
		return
	}

	if state.RecordFailure(ssid) >= maxWarmFailures {
		_ = DeleteWifiProfile(ssid)
		state.ForgetProfile(ssid)
		state.Penalize(ssid, 30*time.Minute,
			fmt.Sprintf("inferred password rejected %d times", maxWarmFailures))
		return
	}
	state.Penalize(ssid, 2*time.Minute, reason)
}

// CleanupProvisionedProfiles removes profiles NomadWiFi created that never
// managed to associate, so a bad guess does not accumulate in Windows.
func CleanupProvisionedProfiles() []string {
	var removed []string
	for _, p := range state.UnverifiedProfiles(maxWarmFailures) {
		if err := DeleteWifiProfile(p.SSID); err == nil {
			removed = append(removed, p.SSID)
		}
		state.ForgetProfile(p.SSID)
	}
	return removed
}
