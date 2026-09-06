package wifi

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestSecurityForNetwork(t *testing.T) {
	tests := []struct {
		auth, cipher string
		want         SecurityKind
	}{
		{"Open", "None", SecurityOpen},
		{"", "None", SecurityOpen},
		{"WPA2-Personal", "CCMP", SecurityWPA2PSK},
		{"WPA3-Personal", "CCMP", SecurityWPA3SAE},
		{"WPA3-SAE", "GCMP", SecurityWPA3SAE},
		{"WPA-Personal", "TKIP", SecurityWPAPSK},
		{"WPA2-Enterprise", "CCMP", SecurityEnterprise},
	}
	for _, tc := range tests {
		if got := SecurityForNetwork(tc.auth, tc.cipher); got != tc.want {
			t.Errorf("SecurityForNetwork(%q,%q) = %s, want %s", tc.auth, tc.cipher, got, tc.want)
		}
	}
}

// A profile must be well-formed XML even when the SSID or key contains
// characters that are special in XML. Hotel SSIDs frequently contain "&".
func TestBuildProfileXMLEscapesSpecialCharacters(t *testing.T) {
	out, err := BuildProfileXML(`Bed & Breakfast <Lobby>`, `p@ss"&"word`, SecurityWPA2PSK, false)
	if err != nil {
		t.Fatalf("BuildProfileXML: %v", err)
	}

	var probe struct {
		XMLName xml.Name `xml:"WLANProfile"`
		Name    string   `xml:"name"`
	}
	if err := xml.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("generated profile is not well-formed XML: %v\n%s", err, out)
	}
	if probe.Name != `Bed & Breakfast <Lobby>` {
		t.Errorf("SSID round-tripped as %q", probe.Name)
	}
	if strings.Contains(out, `p@ss"&"word`) {
		t.Error("raw unescaped key material leaked into the profile XML")
	}
}

// Windows must never reconnect on its own schedule: NomadWiFi owns the decision.
func TestBuildProfileXMLIsManualConnect(t *testing.T) {
	out, err := BuildProfileXML("Venue", "hunter2hunter2", SecurityWPA2PSK, false)
	if err != nil {
		t.Fatalf("BuildProfileXML: %v", err)
	}
	if !strings.Contains(out, "<connectionMode>manual</connectionMode>") {
		t.Error("profile should be manual-connect so Windows does not roam on its own")
	}
	if strings.Contains(out, "<connectionMode>auto</connectionMode>") {
		t.Error("profile must not be auto-connect")
	}
}

func TestBuildProfileXMLOpenNetworkNeedsNoKey(t *testing.T) {
	out, err := BuildProfileXML("Airport Free WiFi", "", SecurityOpen, false)
	if err != nil {
		t.Fatalf("open networks must be provisionable without a password: %v", err)
	}
	if strings.Contains(out, "sharedKey") {
		t.Error("an open profile must not carry a sharedKey block")
	}
	if !strings.Contains(out, "<authentication>open</authentication>") {
		t.Error("expected open authentication")
	}
}

func TestBuildProfileXMLWPA3UsesSAE(t *testing.T) {
	out, err := BuildProfileXML("Modern Cafe", "hunter2hunter2", SecurityWPA3SAE, false)
	if err != nil {
		t.Fatalf("BuildProfileXML: %v", err)
	}
	if !strings.Contains(out, "<authentication>WPA3SAE</authentication>") {
		t.Error("WPA3 networks need an SAE profile, not a WPA2PSK one")
	}
}

func TestBuildProfileXMLRejectsUnusableInput(t *testing.T) {
	if _, err := BuildProfileXML(HiddenSSID, "hunter2hunter2", SecurityWPA2PSK, false); err == nil {
		t.Error("expected an error for an unnamed network")
	}
	if _, err := BuildProfileXML("Venue", "", SecurityWPA2PSK, false); err == nil {
		t.Error("expected an error when a PSK network is given no password")
	}
	if _, err := BuildProfileXML("Corp", "x", SecurityEnterprise, false); err == nil {
		t.Error("expected enterprise networks to be rejected")
	}
}

func TestBuildProfileXMLHiddenNetwork(t *testing.T) {
	out, err := BuildProfileXML("Stealth", "hunter2hunter2", SecurityWPA2PSK, true)
	if err != nil {
		t.Fatalf("BuildProfileXML: %v", err)
	}
	if !strings.Contains(out, "<nonBroadcast>true</nonBroadcast>") {
		t.Error("hidden networks need nonBroadcast set so Windows probes for them")
	}
}

func TestIsSameHotelVenue(t *testing.T) {
	same := [][2]string{
		{"SMFLoor21", "SMFLoor21_5G"},
		{"BaanNT_2.4G", "BaanNT_5G"},
		{"Patong Blue Lobby", "Patong Blue"},
	}
	for _, p := range same {
		if !IsSameHotelVenue(p[0], p[1]) {
			t.Errorf("expected %q and %q to be the same venue", p[0], p[1])
		}
	}

	different := [][2]string{
		{"SMFLoor21", "UnrelatedCoffeeShop"},
		{"Starbucks", "BaanNT"},
		{HiddenSSID, "SMFLoor21"},
	}
	for _, p := range different {
		if IsSameHotelVenue(p[0], p[1]) {
			t.Errorf("expected %q and %q to be different venues", p[0], p[1])
		}
	}
}

func TestProfileCacheInvalidationClearsNamesAndPasswords(t *testing.T) {
	c := &profileCache{
		names:     []string{"Hotel"},
		namesAt:   time.Now(),
		passwords: map[string]string{"hotel": "old-key"},
	}

	c.invalidate()
	if c.names != nil {
		t.Error("profile names should be invalidated")
	}
	if len(c.passwords) != 0 {
		t.Errorf("password cache has %d entries after invalidation, want 0", len(c.passwords))
	}

	c.remember("Hotel", "new-key")
	if got := c.passwords["hotel"]; got != "new-key" {
		t.Errorf("remembered password = %q, want new-key", got)
	}
}
