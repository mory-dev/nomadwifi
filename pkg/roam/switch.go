package roam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/state"
	"github.com/mory-dev/nomadwifi/pkg/vpn"
	"github.com/mory-dev/nomadwifi/pkg/wifi"
)

// SwitchResult describes the outcome of a move between access points.
type SwitchResult struct {
	From       string          `json:"from,omitempty"`
	To         string          `json:"to,omitempty"`
	Switched   bool            `json:"switched"`
	RolledBack bool            `json:"rolled_back,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Error      string          `json:"error,omitempty"`
	VPN        vpn.HoldOutcome `json:"vpn,omitempty"`
	Health     *Health         `json:"health,omitempty"`
}

// considerRoam looks for a materially better access point and moves to it.
func (e *Engine) considerRoam(ctx context.Context, current Health) {
	ladder := e.Ladder(current.SSID)
	if len(ladder) == 0 {
		e.logf("[roam] no usable alternative access point in range")
		return
	}

	currentScore := e.currentScore(current.SSID)
	best := ladder[0]

	// Require a clear margin so the engine does not trade one mediocre AP for
	// another and oscillate between them.
	if best.AP.QualityScore < currentScore+e.cfg.MinScoreDelta {
		e.logf("[roam] best alternative %s (%.1f) is not enough better than %s (%.1f)",
			best.AP.SSID, best.AP.QualityScore, current.SSID, currentScore)
		return
	}

	e.logf("[roam] moving to %s (%.1f vs %.1f): %s",
		best.AP.SSID, best.AP.QualityScore, currentScore, best.Reason)

	res := e.SwitchTo(ctx, current.SSID, best.AP.SSID, best.Reason)
	if res.Switched {
		e.logf("[roam] now on %s", res.To)
	} else if res.RolledBack {
		e.logf("[roam] %s did not work out, restored %s", best.AP.SSID, res.From)
	} else {
		e.logf("[roam] could not switch to %s: %s", best.AP.SSID, res.Error)
	}
}

func (e *Engine) currentScore(ssid string) float64 {
	aps, err := wifi.ScanNetworks()
	if err != nil {
		return 0
	}
	for _, ap := range aps {
		if strings.EqualFold(ap.SSID, ssid) {
			return ap.QualityScore
		}
	}
	return 0
}

// SwitchTo moves to a target access point, verifies the result, and restores
// the previous network if the move made things worse.
func (e *Engine) SwitchTo(ctx context.Context, from, to, reason string) SwitchResult {
	res := SwitchResult{From: from, To: to, Reason: reason}
	e.lastRoam = time.Now()

	// Pause the tunnel first. Re-associating pulls the path out from under an
	// active VPN session, which is what leaves it connected but carrying
	// nothing until the user toggles it manually.
	res.VPN = e.vpn.Hold(fmt.Sprintf("roaming to %s", to))
	defer func() {
		e.vpn.Repair()
		e.vpn.Resume()
	}()

	if err := wifi.ConnectSSID(to); err != nil {
		res.Error = err.Error()
		if from != "" {
			e.restore(from, &res)
		}
		return res
	}

	health := e.verify(ctx, to)
	res.Health = &health

	if health.Connected && (health.InternetOK || health.CaptivePortal) {
		// A captive portal still counts as a successful association: the link
		// works, the user simply has to sign in.
		wifi.NoteAssociationSuccess(to)
		res.Switched = true
		return res
	}

	res.Error = fmt.Sprintf("%s associated but carried no traffic", to)
	wifi.NoteAssociationFailure(to, "no internet after association")
	if from != "" {
		e.restore(from, &res)
	}
	return res
}

// verify waits for the OS to settle, then measures whether the new access
// point actually works.
func (e *Engine) verify(ctx context.Context, ssid string) Health {
	select {
	case <-ctx.Done():
	case <-time.After(vpn.SettleDelay):
	}
	wifi.InvalidateNetworkCaches()
	return Assess(e.cfg.Thresholds)
}

// restore reconnects the previous network after a failed move.
func (e *Engine) restore(previous string, res *SwitchResult) {
	e.logf("[roam] restoring previous network %s", previous)
	if err := wifi.ConnectSSID(previous); err == nil {
		res.RolledBack = true
		res.To = previous
	}
}

// recover handles a link that is down: it walks the candidate ladder until
// something associates and carries traffic.
func (e *Engine) recover(ctx context.Context, reason string) {
	if time.Since(e.lastRoam) < 3*time.Second {
		return // a switch is already in flight
	}

	ladder := e.Ladder("")
	if len(ladder) == 0 {
		e.logf("[roam] %s, but no known network is in range", reason)
		return
	}

	e.logf("[roam] %s; trying %d candidate(s)", reason, len(ladder))
	e.vpn.Hold("recovering dropped connection")
	defer func() {
		e.vpn.Repair()
		e.vpn.Resume()
	}()

	for _, c := range ladder {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e.logf("[roam] attempting %s (%.1f): %s", c.AP.SSID, c.AP.QualityScore, c.Reason)
		if err := wifi.ConnectSSID(c.AP.SSID); err != nil {
			e.logf("[roam] %s failed: %v", c.AP.SSID, err)
			continue
		}

		health := e.verify(ctx, c.AP.SSID)
		if health.Connected && (health.InternetOK || health.CaptivePortal) {
			wifi.NoteAssociationSuccess(c.AP.SSID)
			e.lastRoam = time.Now()
			e.degradedStreak = 0
			e.logf("[roam] recovered on %s", c.AP.SSID)
			if health.CaptivePortal {
				e.reportPortal(health)
			}
			return
		}

		e.logf("[roam] %s associated but carried no traffic", c.AP.SSID)
		wifi.NoteAssociationFailure(c.AP.SSID, "no internet after association")
	}

	e.logf("[roam] exhausted all candidates without recovering")
}

// maybeWarm keeps profiles ready for the venue's best alternatives so that a
// failover is a single connect rather than a profile round-trip.
func (e *Engine) maybeWarm(ctx context.Context) {
	const warmInterval = 5 * time.Minute
	if time.Since(e.lastWarm) < warmInterval {
		return
	}
	e.lastWarm = time.Now()

	aps, err := wifi.ScanNetworks()
	if err != nil {
		return
	}
	for _, r := range wifi.WarmVenue(aps, e.cfg.WarmSetSize) {
		switch {
		case r.Warmed:
			e.logf("[warm] %s is ready for instant failover (%s)", r.SSID, r.Source)
		case r.Error != "":
			e.logf("[warm] %s could not be prepared: %s", r.SSID, r.Error)
		}
	}
	_ = state.LastVenue()
}
