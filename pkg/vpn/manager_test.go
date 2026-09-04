package vpn

import "testing"

func TestProviderForRecognisesTunnelAdapters(t *testing.T) {
	tests := []struct {
		adapter string
		want    string
	}{
		{"NordLynx", "NordVPN (NordLynx)"},
		{"Tailscale", "Tailscale"},
		{"OpenVPN Data Channel Offload for NordVPN", "NordVPN"},
		{"Mullvad", "Mullvad"},
		{"ProtonVPN TUN", "Proton VPN"},
		{"WireGuard Tunnel", "WireGuard"},
	}
	for _, tc := range tests {
		got, ok := providerFor(tc.adapter)
		if !ok {
			t.Errorf("providerFor(%q) did not recognise the adapter", tc.adapter)
			continue
		}
		if got != tc.want {
			t.Errorf("providerFor(%q) = %q, want %q", tc.adapter, got, tc.want)
		}
	}

	for _, adapter := range []string{"Wi-Fi", "Ethernet", "Loopback Pseudo-Interface 1"} {
		if _, ok := providerFor(adapter); ok {
			t.Errorf("providerFor(%q) should not match a physical adapter", adapter)
		}
	}
}

// NordVPN ships no command line on Windows, so it must fall back to the
// adapter controller and say so rather than silently doing nothing.
func TestControllerSelection(t *testing.T) {
	if _, ok := controllerFor("Tailscale", "Tailscale").(*tailscaleController); !ok {
		t.Error("Tailscale should use the CLI controller")
	}
	if _, ok := controllerFor("WireGuard", "wg0").(*wireguardController); !ok {
		t.Error("WireGuard should use wireguard.exe")
	}
	ctrl, ok := controllerFor("NordVPN (NordLynx)", "NordLynx").(*adapterController)
	if !ok {
		t.Fatal("NordVPN should fall back to the adapter controller")
	}
	if ctrl.Hint() == "" {
		t.Error("the fallback controller must explain what the user has to do")
	}
}

func TestJoinList(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"NordVPN"}, "NordVPN"},
		{[]string{"NordVPN", "Tailscale"}, "NordVPN and Tailscale"},
		{[]string{"A", "B", "C"}, "A, B, and C"},
	}
	for _, tc := range tests {
		if got := joinList(tc.in); got != tc.want {
			t.Errorf("joinList(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStatusAnyActive(t *testing.T) {
	var s Status
	if s.AnyActive() {
		t.Error("an empty status has no active tunnel")
	}
	s.Tunnels = []Tunnel{{Provider: "NordVPN", Up: true, OwnsDefaultRoute: true}}
	s.Active = &s.Tunnels[0]
	if !s.AnyActive() {
		t.Error("a tunnel owning the default route is active")
	}
}
