// Package vpn detects active VPN tunnels and, where the client allows it,
// coordinates them around Wi-Fi roams.
//
// Re-associating with Wi-Fi breaks the physical path a tunnel is riding on.
// The tunnel adapter stays up and keeps the default route and its DNS servers,
// so traffic is handed to a session that no longer exists: the network looks
// connected but nothing loads until the tunnel is toggled by hand. A kill
// switch makes it worse by blocking the hotel's captive portal outright.
package vpn

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Tunnel is one VPN adapter present on the machine.
type Tunnel struct {
	Provider         string `json:"provider"`
	Adapter          string `json:"adapter"`
	Up               bool   `json:"up"`
	OwnsDefaultRoute bool   `json:"owns_default_route"`
	Controllable     bool   `json:"controllable"`
	ControlHint      string `json:"control_hint,omitempty"`
	LocalIP          string `json:"local_ip,omitempty"`
}

// Status summarises VPN state for the UI and the roam engine.
type Status struct {
	Tunnels []Tunnel `json:"tunnels"`
	// Active is the tunnel currently carrying the default route, if any.
	Active *Tunnel `json:"active,omitempty"`
}

// AnyActive reports whether a tunnel currently owns the default route.
func (s Status) AnyActive() bool { return s.Active != nil }

// providerPatterns maps adapter names to the product behind them. Matching on
// the adapter name is deliberate: it works without elevation and without
// assuming a particular client is installed.
var providerPatterns = []struct {
	match    string
	provider string
}{
	{"nordlynx", "NordVPN (NordLynx)"},
	{"nordvpn", "NordVPN"},
	{"tailscale", "Tailscale"},
	{"mullvad", "Mullvad"},
	{"proton", "Proton VPN"},
	{"expressvpn", "ExpressVPN"},
	{"surfshark", "Surfshark"},
	{"cloudflare warp", "Cloudflare WARP"},
	{"zerotier", "ZeroTier"},
	{"wireguard", "WireGuard"},
	{"openvpn", "OpenVPN"},
	{"tap-windows", "OpenVPN (TAP)"},
	{"wan miniport", "Windows VPN"},
	{"wintun", "WireGuard"},
}

func providerFor(adapterName string) (string, bool) {
	lower := strings.ToLower(adapterName)
	for _, p := range providerPatterns {
		if strings.Contains(lower, p.match) {
			return p.provider, true
		}
	}
	return "", false
}

var (
	statusMu    sync.Mutex
	statusCache Status
	statusAt    time.Time
)

const statusTTL = 5 * time.Second

// Detect returns the VPN tunnels present, with the one owning the default
// route marked active. Results are cached briefly so a UI can poll freely.
func Detect() Status {
	statusMu.Lock()
	if time.Since(statusAt) < statusTTL {
		s := statusCache
		statusMu.Unlock()
		return s
	}
	statusMu.Unlock()

	status := detectUncached()

	statusMu.Lock()
	statusCache = status
	statusAt = time.Now()
	statusMu.Unlock()
	return status
}

// Invalidate drops the cached status, for use right after a change.
func Invalidate() {
	statusMu.Lock()
	statusAt = time.Time{}
	statusMu.Unlock()
}

func detectUncached() Status {
	var status Status

	ifaces, err := net.Interfaces()
	if err != nil {
		return status
	}

	defaultOwners := defaultRouteInterfaceIndexes()

	for _, iface := range ifaces {
		provider, ok := providerFor(iface.Name)
		if !ok {
			continue
		}

		t := Tunnel{
			Provider: provider,
			Adapter:  iface.Name,
			Up:       iface.Flags&net.FlagUp != 0,
			LocalIP:  firstIPv4(iface),
		}
		// An adapter with no address is installed but not carrying a session.
		if t.LocalIP == "" {
			t.Up = false
		}
		t.OwnsDefaultRoute = defaultOwners[iface.Index]

		if ctrl := controllerFor(provider, iface.Name); ctrl != nil {
			t.Controllable = ctrl.Available()
			t.ControlHint = ctrl.Hint()
		}

		status.Tunnels = append(status.Tunnels, t)
	}

	for i := range status.Tunnels {
		if status.Tunnels[i].Up && status.Tunnels[i].OwnsDefaultRoute {
			status.Active = &status.Tunnels[i]
			break
		}
	}
	return status
}

func firstIPv4(iface net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() && !v4.IsLinkLocalUnicast() {
				return v4.String()
			}
		}
	}
	return ""
}

var reDefaultRoute = regexp.MustCompile(`^\s*\S+\s+\S+\s+\d+\s+0\.0\.0\.0/0\s+(\d+)\s`)

// defaultRouteInterfaceIndexes returns the interface indexes that currently
// carry a default route. More than one is normal when a tunnel is up.
func defaultRouteInterfaceIndexes() map[int]bool {
	out := map[int]bool{}

	cmd := exec.Command("netsh", "interface", "ipv4", "show", "route")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	data, err := cmd.Output()
	if err != nil {
		return out
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if m := reDefaultRoute.FindStringSubmatch(scanner.Text()); len(m) > 1 {
			if idx, err := strconv.Atoi(m[1]); err == nil {
				out[idx] = true
			}
		}
	}
	return out
}
