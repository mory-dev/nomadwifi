package vpn

import (
	"fmt"
	"sync"
	"syscall"
	"time"
)

// Manager coordinates VPN tunnels around Wi-Fi changes.
type Manager struct {
	mu     sync.Mutex
	logf   func(string, ...interface{})
	held   []heldTunnel
	enable bool
}

type heldTunnel struct {
	tunnel Tunnel
	ctrl   controller
}

// NewManager builds a manager. Pass enable=false to detect and report only,
// never touching the VPN client.
func NewManager(enable bool, logf func(string, ...interface{})) *Manager {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Manager{logf: logf, enable: enable}
}

// Status returns the current tunnel picture.
func (m *Manager) Status() Status { return Detect() }

// HoldOutcome describes what the manager was able to do before a roam.
type HoldOutcome struct {
	// Held lists tunnels that were actually paused and will be resumed.
	Held []string `json:"held,omitempty"`
	// NeedsManual lists active tunnels that could not be paused automatically.
	NeedsManual []string `json:"needs_manual,omitempty"`
	// Advice is a user-facing explanation when manual action is needed.
	Advice string `json:"advice,omitempty"`
}

// Hold pauses every active tunnel before a roam, so the tunnel is not left
// bound to a Wi-Fi session that is about to disappear.
//
// It reports what it could not do rather than failing: a roam is still better
// than staying on a dead link, and Repair recovers most cases anyway.
func (m *Manager) Hold(reason string) HoldOutcome {
	var outcome HoldOutcome
	if !m.enable {
		return outcome
	}

	status := Detect()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range status.Tunnels {
		if !t.Up {
			continue
		}
		ctrl := controllerFor(t.Provider, t.Adapter)
		if ctrl == nil || !ctrl.Available() {
			outcome.NeedsManual = append(outcome.NeedsManual, t.Provider)
			continue
		}
		m.logf("[vpn] pausing %s before roam (%s)", t.Provider, reason)
		if err := ctrl.Down(); err != nil {
			m.logf("[vpn] could not pause %s: %v", t.Provider, err)
			outcome.NeedsManual = append(outcome.NeedsManual, t.Provider)
			continue
		}
		m.held = append(m.held, heldTunnel{tunnel: t, ctrl: ctrl})
		outcome.Held = append(outcome.Held, t.Provider)
	}

	if len(outcome.NeedsManual) > 0 {
		outcome.Advice = fmt.Sprintf(
			"%s cannot be paused automatically on Windows. If the connection does not recover, toggle it off and on once.",
			joinList(outcome.NeedsManual))
	}

	Invalidate()
	return outcome
}

// Resume brings back every tunnel Hold paused.
func (m *Manager) Resume() {
	m.mu.Lock()
	held := m.held
	m.held = nil
	m.mu.Unlock()

	for _, h := range held {
		m.logf("[vpn] resuming %s", h.tunnel.Provider)
		if err := h.ctrl.Up(); err != nil {
			m.logf("[vpn] could not resume %s: %v", h.tunnel.Provider, err)
		}
	}
	Invalidate()
}

// Repair clears the state that survives a Wi-Fi change and silently breaks a
// tunnel: cached DNS answers that resolve through the old session.
//
// This runs whether or not a tunnel was paused, because it is the automated
// form of the manual "turn it off and back on" that otherwise fixes it.
func (m *Manager) Repair() {
	if err := FlushDNS(); err != nil {
		m.logf("[vpn] DNS cache flush failed: %v", err)
		return
	}
	m.logf("[vpn] flushed DNS resolver cache after network change")
}

// PortalBlocked reports whether a captive portal is unreachable because a
// tunnel is carrying all traffic. A kill switch will not let the login page
// load, which is the single most confusing failure in a hotel.
func (m *Manager) PortalBlocked() (bool, string) {
	status := Detect()
	if status.Active == nil {
		return false, ""
	}
	return true, fmt.Sprintf(
		"%s is carrying all traffic, so the Wi-Fi login page cannot load. Pause the VPN, sign in, then reconnect it.",
		status.Active.Provider)
}

var (
	moddnsapi                 = syscall.NewLazyDLL("dnsapi.dll")
	procDnsFlushResolverCache = moddnsapi.NewProc("DnsFlushResolverCache")
)

// FlushDNS clears the Windows resolver cache in-process, without shelling out
// to ipconfig.
func FlushDNS() error {
	r, _, err := procDnsFlushResolverCache.Call()
	if r == 0 {
		return fmt.Errorf("DnsFlushResolverCache failed: %w", err)
	}
	return nil
}

// SettleDelay is how long to let the OS rebind routes after a link change
// before judging whether connectivity came back.
const SettleDelay = 1200 * time.Millisecond

func joinList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	out := ""
	for i, s := range items[:len(items)-1] {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out + ", and " + items[len(items)-1]
}
