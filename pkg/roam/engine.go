// Package roam decides when to move to a different access point and carries
// out the move safely.
//
// Two things distinguish it from a simple polling loop. It reacts to the
// wireless service's own disconnect notification rather than waiting for the
// next tick, so a dropped link is handled in well under a second. And every
// intentional move is reversible: if the new access point cannot carry
// traffic, the previous one is restored instead of leaving the user stranded
// somewhere worse than where they started.
package roam

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/state"
	"github.com/mory-dev/nomadwifi/pkg/vpn"
	"github.com/mory-dev/nomadwifi/pkg/wifi"
)

// Config controls the roaming engine.
type Config struct {
	PollInterval time.Duration
	// DegradedSamples is how many consecutive bad measurements are required
	// before roaming. One bad sample is noise; three in a row is a problem.
	DegradedSamples int
	Cooldown        time.Duration
	MinScoreDelta   float64
	AutoRoam        bool
	ManageVPN       bool
	WarmSetSize     int
	Thresholds      Thresholds
}

// DefaultConfig returns settings tuned for travel networks.
func DefaultConfig() Config {
	return Config{
		PollInterval:    20 * time.Second,
		DegradedSamples: 3,
		Cooldown:        60 * time.Second,
		MinScoreDelta:   15.0,
		AutoRoam:        true,
		ManageVPN:       true,
		WarmSetSize:     wifi.DefaultWarmSetSize,
		Thresholds:      DefaultThresholds(),
	}
}

// Logf receives human-readable progress from the engine.
type Logf func(format string, args ...interface{})

// Engine runs the roaming loop.
type Engine struct {
	cfg  Config
	logf Logf
	vpn  *vpn.Manager

	mu             sync.Mutex
	degradedStreak int
	lastRoam       time.Time
	lastWarm       time.Time
}

// SetAutoRoam turns autonomous roaming on or off while the engine is running,
// so a UI toggle takes effect immediately.
func (e *Engine) SetAutoRoam(enabled bool) {
	e.mu.Lock()
	e.cfg.AutoRoam = enabled
	e.mu.Unlock()
}

// AutoRoam reports whether autonomous roaming is currently enabled.
func (e *Engine) AutoRoam() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.AutoRoam
}

// New builds an engine.
func New(cfg Config, logf Logf) *Engine {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Engine{
		cfg:  cfg,
		logf: logf,
		vpn:  vpn.NewManager(cfg.ManageVPN, logf),
	}
}

// Run drives the engine until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.cfg.PollInterval)
	defer ticker.Stop()

	events := wifi.Events()

	e.logf("[roam] watching (interval %v, auto-roam %v, VPN coordination %v)",
		e.cfg.PollInterval, e.cfg.AutoRoam, e.cfg.ManageVPN)
	if removed := wifi.CleanupProvisionedProfiles(); len(removed) > 0 {
		e.logf("[roam] removed %d stale profile(s) from earlier failed guesses: %s",
			len(removed), strings.Join(removed, ", "))
	}

	e.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			e.logf("[roam] stopped")
			return nil

		case ev := <-events:
			e.handleEvent(ctx, ev)

		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) handleEvent(ctx context.Context, ev wifi.Event) {
	switch ev.Kind {
	case wifi.EventDisconnected:
		// The link is gone right now. Recovering here, rather than on the next
		// tick, is the difference between a blip and a visible outage.
		e.logf("[roam] link dropped, starting recovery")
		wifi.InvalidateNetworkCaches()
		e.recover(ctx, "wireless link dropped")

	case wifi.EventConnected:
		wifi.InvalidateNetworkCaches()
		e.degradedStreak = 0
		e.vpn.Repair()

	case wifi.EventConnectFailed:
		e.logf("[roam] association attempt failed")
	}
}

func (e *Engine) tick(ctx context.Context) {
	health := Assess(e.cfg.Thresholds)

	if !health.Connected {
		e.recover(ctx, "adapter reports no connection")
		return
	}

	e.maybeWarm(ctx)

	if health.CaptivePortal {
		e.reportPortal(health)
		return
	}

	if !health.Degraded {
		if e.degradedStreak > 0 {
			e.logf("[roam] connection recovered: %s", health.Summary())
		}
		e.degradedStreak = 0
		return
	}

	e.degradedStreak++
	e.logf("[roam] degraded (%d/%d): %s [%s]",
		e.degradedStreak, e.cfg.DegradedSamples, health.Summary(), strings.Join(health.Reasons, "; "))

	if e.degradedStreak < e.cfg.DegradedSamples {
		return
	}
	if time.Since(e.lastRoam) < e.cfg.Cooldown {
		return
	}
	if !e.AutoRoam() {
		e.logf("[roam] auto-roam is off, so no switch was made")
		return
	}

	e.considerRoam(ctx, health)
}

func (e *Engine) reportPortal(health Health) {
	e.logf("[roam] captive portal detected at %s", health.PortalURL)
	if blocked, advice := e.vpn.PortalBlocked(); blocked {
		e.logf("[roam] %s", advice)
	}
}

// Candidate is one access point the engine may move to.
type Candidate struct {
	AP     wifi.AccessPoint
	Reason string
}

// Ladder builds the ordered list of access points worth trying, best first.
//
// It is maintained from the rolling scan cache so that when a link drops the
// decision is already made and recovery is just a connect call.
func (e *Engine) Ladder(currentSSID string) []Candidate {
	aps, err := wifi.ScanNetworks()
	if err != nil {
		return nil
	}

	var out []Candidate
	for _, ap := range aps {
		if strings.EqualFold(ap.SSID, currentSSID) {
			continue
		}
		if ap.IsHidden() || !ap.AuthStatus.Connectable() {
			continue
		}
		if benched, reason := state.IsPenalized(ap.SSID); benched {
			e.logf("[roam] skipping %s (%s)", ap.SSID, reason)
			continue
		}
		out = append(out, Candidate{AP: ap, Reason: candidateReason(ap)})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AP.QualityScore > out[j].AP.QualityScore
	})
	return out
}

func candidateReason(ap wifi.AccessPoint) string {
	if len(ap.Reasons) > 0 {
		return strings.Join(ap.Reasons, ", ")
	}
	return fmt.Sprintf("%s, %d%% signal", ap.Band, ap.SignalPercent)
}
