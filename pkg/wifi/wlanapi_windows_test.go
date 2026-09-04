package wifi

import (
	"testing"
	"unsafe"
)

// The wlanapi structs are read straight out of memory the OS filled in. If a
// size drifts, every field after the drift silently decodes as garbage, so the
// layout is pinned here rather than trusted.
func TestWlanStructLayout(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"DOT11_SSID", unsafe.Sizeof(dot11SSID{}), 36},
		{"WLAN_RATE_SET", unsafe.Sizeof(wlanRateSet{}), 256},
		{"WLAN_BSS_ENTRY", unsafe.Sizeof(wlanBSSEntry{}), 360},
		{"WLAN_AVAILABLE_NETWORK", unsafe.Sizeof(wlanAvailableNetwork{}), 628},
		{"WLAN_NOTIFICATION_DATA", unsafe.Sizeof(wlanNotificationData{}), 40},
		{"WLAN_INTERFACE_INFO", unsafe.Sizeof(wlanInterfaceInfo{}), 532},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("sizeof(%s) = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	// Field offsets that alignment padding is easy to get wrong.
	var e wlanBSSEntry
	base := uintptr(unsafe.Pointer(&e))
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"BSSID", uintptr(unsafe.Pointer(&e.BSSID)) - base, 40},
		{"RSSI", uintptr(unsafe.Pointer(&e.RSSI)) - base, 56},
		{"Timestamp", uintptr(unsafe.Pointer(&e.Timestamp)) - base, 72},
		{"ChCenterFrequency", uintptr(unsafe.Pointer(&e.ChCenterFrequency)) - base, 92},
		{"IEOffset", uintptr(unsafe.Pointer(&e.IEOffset)) - base, 352},
	}
	for _, tc := range offsets {
		if tc.got != tc.want {
			t.Errorf("offsetof(WLAN_BSS_ENTRY.%s) = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestParseBSSLoad(t *testing.T) {
	// SSID element (id 0), then BSS Load (id 11): 2-byte station count,
	// 1-byte utilization scaled 0-255, 2-byte available capacity.
	ies := []byte{
		0, 4, 'T', 'e', 's', 't',
		11, 5, 0x0A, 0x00, 128, 0x00, 0x00,
	}
	stations, util, ok := parseBSSLoad(ies)
	if !ok {
		t.Fatal("expected a BSS Load element to be found")
	}
	if stations != 10 {
		t.Errorf("stations = %d, want 10", stations)
	}
	if util != 50 {
		t.Errorf("utilization = %d%%, want 50%% (128/255)", util)
	}

	if _, _, ok := parseBSSLoad([]byte{0, 4, 'T', 'e', 's', 't'}); ok {
		t.Error("expected no BSS Load element when none is present")
	}
	if _, _, ok := parseBSSLoad([]byte{11, 200, 0x01}); ok {
		t.Error("a truncated element must not be parsed")
	}
}

func TestChannelFromFrequencyKHz(t *testing.T) {
	tests := []struct {
		khz  uint32
		want int
	}{
		{2412000, 1},
		{2437000, 6},
		{2484000, 14},
		{5180000, 36},
		{5785000, 157},
		{5955000, 1},  // 6 GHz channel 1
		{6175000, 45}, // 6 GHz
	}
	for _, tc := range tests {
		if got := channelFromFrequencyKHz(tc.khz); got != tc.want {
			t.Errorf("channelFromFrequencyKHz(%d) = %d, want %d", tc.khz, got, tc.want)
		}
	}
}

func TestAuthAndCipherNamesMapToSecurityKinds(t *testing.T) {
	// The native path must produce labels the shared security mapper understands.
	tests := []struct {
		auth   uint32
		cipher uint32
		want   SecurityKind
	}{
		{authAlgoOpen, cipherNone, SecurityOpen},
		{authAlgoRSNAPSK, cipherCCMP, SecurityWPA2PSK},
		{authAlgoWPA3SAE, cipherCCMP, SecurityWPA3SAE},
		{authAlgoWPAPSK, cipherTKIP, SecurityWPAPSK},
		{authAlgoRSNA, cipherCCMP, SecurityEnterprise},
	}
	for _, tc := range tests {
		a := authAlgorithmName(tc.auth)
		c := cipherAlgorithmName(tc.cipher)
		if got := SecurityForNetwork(a, c); got != tc.want {
			t.Errorf("SecurityForNetwork(%q,%q) = %s, want %s", a, c, got, tc.want)
		}
	}
}
