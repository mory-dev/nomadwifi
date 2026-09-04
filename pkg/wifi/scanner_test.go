package wifi

import "testing"

// netsh emits one SSID block containing one or more BSSID sub-blocks, and on
// current Windows builds each BSSID carries its own explicit Band line.
const sampleNetshOutput = `
Interface name : Wi-Fi
There are 3 networks currently visible.

SSID 1 : SMFLoor21
    Network type            : Infrastructure
    Authentication          : WPA2-Personal
    Encryption              : CCMP
    BSSID 1                 : 24:cf:24:e5:73:99
         Signal             : 86%
         Radio type         : 802.11n
         Band               : 2.4 GHz
         Channel            : 11
         Bss Load:
             Connected Stations:         4
             Channel Utilization:        180 (70 %)
             Medium Available Capacity:  31250 (1000000 us/s)
    BSSID 2                 : 24:cf:24:e5:73:9a
         Signal             : 80%
         Radio type         : 802.11ac
         Band               : 5 GHz
         Channel            : 157

SSID 2 : CafeSix
    Network type            : Infrastructure
    Authentication          : WPA3-Personal
    Encryption              : CCMP
    BSSID 1                 : aa:bb:cc:dd:ee:01
         Signal             : 70%
         Radio type         : 802.11be
         Band               : 6 GHz
         Channel            : 37

SSID 3 :
    Network type            : Infrastructure
    Authentication          : Open
    Encryption              : None
    BSSID 1                 : aa:bb:cc:dd:ee:02
         Signal             : 50%
         Radio type         : 802.11ax
         Band               : 2.4 GHz
         Channel            : 6
`

func TestParseNetshNetworksExpandsEveryBSSID(t *testing.T) {
	aps, err := parseNetshNetworks([]byte(sampleNetshOutput))
	if err != nil {
		t.Fatalf("unexpected error parsing netsh output: %v", err)
	}

	if len(aps) != 4 {
		t.Fatalf("expected 4 BSSIDs across 3 SSIDs, got %d", len(aps))
	}

	// Both BSSIDs under SSID 1 inherit that network's SSID, auth, and cipher.
	for _, i := range []int{0, 1} {
		if aps[i].SSID != "SMFLoor21" {
			t.Errorf("ap[%d] SSID = %q, want SMFLoor21", i, aps[i].SSID)
		}
		if aps[i].Authentication != "WPA2-Personal" {
			t.Errorf("ap[%d] Authentication = %q, want WPA2-Personal", i, aps[i].Authentication)
		}
	}

	if aps[0].Band != Band24GHz || aps[1].Band != Band5GHz {
		t.Errorf("bands = %s / %s, want 2.4 GHz / 5 GHz", aps[0].Band, aps[1].Band)
	}
	if aps[0].Channel != 11 || aps[1].Channel != 157 {
		t.Errorf("channels = %d / %d, want 11 / 157", aps[0].Channel, aps[1].Channel)
	}
}

func TestParseNetshNetworksUsesExplicitBandFor6GHz(t *testing.T) {
	aps, err := parseNetshNetworks([]byte(sampleNetshOutput))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Channel 37 also exists in 5 GHz, so only the explicit Band line can
	// classify this AP correctly.
	six := aps[2]
	if six.SSID != "CafeSix" {
		t.Fatalf("expected CafeSix at index 2, got %q", six.SSID)
	}
	if six.Band != Band6GHz {
		t.Errorf("Band = %s, want 6 GHz (channel 37 is ambiguous)", six.Band)
	}
}

func TestParseNetshNetworksDoesNotLeakAuthBetweenSSIDs(t *testing.T) {
	aps, err := parseNetshNetworks([]byte(sampleNetshOutput))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	open := aps[3]
	if open.SSID != HiddenSSID {
		t.Errorf("SSID = %q, want %q for a blank SSID line", open.SSID, HiddenSSID)
	}
	if open.Authentication != "Open" || open.Cipher != "None" {
		t.Errorf("auth/cipher = %q/%q, want Open/None", open.Authentication, open.Cipher)
	}
	if !isOpenNetwork(open) {
		t.Error("expected the blank-SSID network to be classified as open")
	}
}

func TestParseNetshNetworksReadsChannelUtilization(t *testing.T) {
	aps, err := parseNetshNetworks([]byte(sampleNetshOutput))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !aps[0].HasChannelUtil {
		t.Fatal("expected BSS Load channel utilization to be parsed")
	}
	if aps[0].ChannelUtilization != 70 {
		t.Errorf("ChannelUtilization = %d, want 70", aps[0].ChannelUtilization)
	}
	if aps[1].HasChannelUtil {
		t.Error("BSSID without a Bss Load block should not report utilization")
	}
}

func TestBandFromFrequencyKHz(t *testing.T) {
	tests := []struct {
		khz  uint32
		want Band
	}{
		{2412000, Band24GHz},
		{2484000, Band24GHz},
		{5180000, Band5GHz},
		{5825000, Band5GHz},
		{5955000, Band6GHz}, // 6 GHz channel 1
		{6175000, Band6GHz}, // 6 GHz channel 37
		{7115000, Band6GHz},
		{900000, BandOther},
	}
	for _, tc := range tests {
		if got := bandFromFrequencyKHz(tc.khz); got != tc.want {
			t.Errorf("bandFromFrequencyKHz(%d) = %s, want %s", tc.khz, got, tc.want)
		}
	}
}

func TestScoreAccessPointRanksFastBandsHigher(t *testing.T) {
	slow := AccessPoint{
		SSID: "Venue", Band: Band24GHz, RadioType: "802.11n",
		SignalPercent: 86, Cipher: "CCMP", AuthStatus: AuthStatusSaved,
	}
	fast := AccessPoint{
		SSID: "Venue_5G", Band: Band5GHz, RadioType: "802.11ac",
		SignalPercent: 80, Cipher: "CCMP", AuthStatus: AuthStatusSaved,
	}

	slowScore, _ := ScoreAccessPoint(slow)
	fastScore, reasons := ScoreAccessPoint(fast)

	if fastScore <= slowScore {
		t.Errorf("5 GHz 802.11ac (%.1f) should outrank 2.4 GHz 802.11n (%.1f)", fastScore, slowScore)
	}
	if len(reasons) == 0 {
		t.Error("expected the score to be explained by at least one reason")
	}
}

func TestScoreAccessPointPenalizesCongestionAndLocks(t *testing.T) {
	base := AccessPoint{
		SSID: "Venue", Band: Band5GHz, RadioType: "802.11ac",
		SignalPercent: 80, Cipher: "CCMP", AuthStatus: AuthStatusSaved,
	}

	busy := base
	busy.HasChannelUtil, busy.ChannelUtilization = true, 85

	baseScore, _ := ScoreAccessPoint(base)
	busyScore, _ := ScoreAccessPoint(busy)
	if busyScore >= baseScore {
		t.Errorf("a congested AP (%.1f) should score below an uncongested one (%.1f)", busyScore, baseScore)
	}

	locked := base
	locked.AuthStatus = AuthStatusLocked
	lockedScore, _ := ScoreAccessPoint(locked)
	if lockedScore >= baseScore {
		t.Errorf("a locked AP (%.1f) should score below a saved one (%.1f)", lockedScore, baseScore)
	}
}

func TestRankAccessPointsSortsBestFirst(t *testing.T) {
	aps := []AccessPoint{
		{SSID: "Slow", Band: Band24GHz, RadioType: "802.11n", SignalPercent: 90, AuthStatus: AuthStatusSaved},
		{SSID: "Fast", Band: Band5GHz, RadioType: "802.11ax", SignalPercent: 75, AuthStatus: AuthStatusSaved},
	}
	RankAccessPoints(aps)

	if aps[0].SSID != "Fast" {
		t.Errorf("expected Fast first after ranking, got %s", aps[0].SSID)
	}
	if aps[0].QualityScore == 0 {
		t.Error("ranking should populate QualityScore")
	}
}

func TestRSSIFromSignalPercent(t *testing.T) {
	if got := rssiFromSignalPercent(100); got != -50 {
		t.Errorf("100%% -> %d dBm, want -50", got)
	}
	if got := rssiFromSignalPercent(0); got != 0 {
		t.Errorf("0%% -> %d dBm, want 0 (unknown)", got)
	}
	if got := rssiFromSignalPercent(50); got != -75 {
		t.Errorf("50%% -> %d dBm, want -75", got)
	}
}

// The driver reports the associated AP at ~99% link quality no matter its
// actual RSSI. Ranking has to put every AP on the same scale or the incumbent
// always wins and the roamer never moves.
func TestSignalPercentFromRSSIIsComparableAcrossAPs(t *testing.T) {
	tests := []struct {
		rssi int
		want int
	}{
		{-50, 100},
		{-57, 86},
		{-77, 46},
		{-79, 42},
		{-100, 0},
		{-120, 0},
		{-40, 100},
		{0, 0},
	}
	for _, tc := range tests {
		if got := signalPercentFromRSSI(tc.rssi); got != tc.want {
			t.Errorf("signalPercentFromRSSI(%d dBm) = %d%%, want %d%%", tc.rssi, got, tc.want)
		}
	}

	// A weak incumbent must not outrank a strong neighbour once both are
	// scored from RSSI.
	incumbent := AccessPoint{SSID: "Current", Band: Band5GHz, RadioType: "802.11ac",
		RSSI: -77, SignalPercent: signalPercentFromRSSI(-77), AuthStatus: AuthStatusSaved}
	neighbour := AccessPoint{SSID: "Nearer", Band: Band5GHz, RadioType: "802.11ac",
		RSSI: -57, SignalPercent: signalPercentFromRSSI(-57), AuthStatus: AuthStatusSaved}

	inc, _ := ScoreAccessPoint(incumbent)
	nbr, _ := ScoreAccessPoint(neighbour)
	if nbr <= inc {
		t.Errorf("stronger neighbour (%.1f) should outrank weak incumbent (%.1f)", nbr, inc)
	}
}
