package wifi

import (
	"testing"
)

func TestParseNetshNetworks(t *testing.T) {
	sampleOutput := `
Interface name : Wi-Fi
There are 2 networks currently visible.

SSID 1 : SMFLoor21
    Network type            : Infrastructure
    Authentication          : WPA2-Personal
    Encryption              : CCMP
    BSSID 1                 : 24:cf:24:e5:73:99
         Signal             : 86%
         Radio type         : 802.11n
         Channel            : 11

SSID 2 : SMFLoor21_5G
    Network type            : Infrastructure
    Authentication          : WPA2-Personal
    Encryption              : CCMP
    BSSID 1                 : 24:cf:24:e5:73:9a
         Signal             : 80%
         Radio type         : 802.11ac
         Channel            : 157
`

	aps, err := parseNetshNetworks([]byte(sampleOutput))
	if err != nil {
		t.Fatalf("unexpected error parsing netsh output: %v", err)
	}

	if len(aps) != 2 {
		t.Fatalf("expected 2 APs, got %d", len(aps))
	}

	// 5GHz 802.11ac should be scored higher and ranked first!
	if aps[0].SSID != "SMFLoor21_5G" {
		t.Errorf("expected top AP to be SMFLoor21_5G, got %s", aps[0].SSID)
	}
	if aps[0].Band != Band5GHz {
		t.Errorf("expected Band5GHz, got %s", aps[0].Band)
	}
	if aps[1].Band != Band24GHz {
		t.Errorf("expected Band24GHz, got %s", aps[1].Band)
	}
	if aps[0].QualityScore <= aps[1].QualityScore {
		t.Errorf("5GHz score (%.1f) should be higher than 2.4GHz score (%.1f)",
			aps[0].QualityScore, aps[1].QualityScore)
	}
}
