package wifi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache() *apCache {
	return &apCache{entries: map[string]*apCacheEntry{}}
}

// The wireless service returns whatever its last scan saw, so consecutive
// queries legitimately disagree. The cache exists to paper over that.
func TestCacheKeepsNetworksMissingFromOneScan(t *testing.T) {
	c := newTestCache()

	c.merge([]AccessPoint{
		{SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 60},
		{SSID: "Floor2", BSSID: "aa:aa:aa:aa:aa:02", SignalPercent: 50},
	})
	// A partial scan that only sees one of them.
	c.merge([]AccessPoint{
		{SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 62},
	})

	if got := len(c.snapshot()); got != 2 {
		t.Errorf("snapshot has %d APs, want 2: a partial scan must not drop known networks", got)
	}
}

func TestCacheExpiresStaleEntries(t *testing.T) {
	c := newTestCache()
	c.entries["aa:aa:aa:aa:aa:03"] = &apCacheEntry{
		ap:       AccessPoint{SSID: "Gone", BSSID: "aa:aa:aa:aa:aa:03"},
		lastSeen: time.Now().Add(-2 * apCacheTTL),
	}
	c.entries["aa:aa:aa:aa:aa:04"] = &apCacheEntry{
		ap:       AccessPoint{SSID: "Here", BSSID: "aa:aa:aa:aa:aa:04"},
		lastSeen: time.Now(),
	}

	snap := c.snapshot()
	if len(snap) != 1 || snap[0].SSID != "Here" {
		t.Errorf("expected only the recently seen AP, got %+v", snap)
	}
}

func TestCacheSmoothsSignalAndKeepsSecurityDetails(t *testing.T) {
	c := newTestCache()
	c.merge([]AccessPoint{{
		SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 80, RSSI: -60,
		Authentication: "WPA2-Personal", Cipher: "CCMP", RadioType: "802.11ac",
	}})
	// A noisy sample, with the security fields absent as the native BSS list
	// sometimes reports them.
	c.merge([]AccessPoint{{
		SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 20, RSSI: -90,
	}})

	snap := c.snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 AP, got %d", len(snap))
	}
	got := snap[0]

	if got.SignalPercent != 50 {
		t.Errorf("smoothed signal = %d%%, want 50%% (midpoint of 80 and 20)", got.SignalPercent)
	}
	if got.RSSI != -75 {
		t.Errorf("smoothed RSSI = %d, want -75", got.RSSI)
	}
	if got.Authentication != "WPA2-Personal" || got.Cipher != "CCMP" {
		t.Errorf("security details were lost on a sample that omitted them: %+v", got)
	}
}

func TestDedupeBySSIDCollapsesRadiosAndDropsHidden(t *testing.T) {
	aps := []AccessPoint{
		{SSID: "Hotel", BSSID: "aa:01", Band: Band24GHz, QualityScore: 40},
		{SSID: "Hotel", BSSID: "aa:02", Band: Band5GHz, QualityScore: 90},
		{SSID: "Hotel", BSSID: "aa:03", Band: Band5GHz, QualityScore: 70},
		{SSID: HiddenSSID, BSSID: "aa:04", QualityScore: 99},
		{SSID: "Cafe", BSSID: "aa:05", QualityScore: 60},
	}

	out := DedupeBySSID(aps)
	if len(out) != 2 {
		t.Fatalf("expected 2 networks after dedupe, got %d", len(out))
	}

	if out[0].SSID != "Hotel" {
		t.Errorf("expected Hotel first, got %s", out[0].SSID)
	}
	if out[0].QualityScore != 90 {
		t.Errorf("dedupe kept score %.0f, want the best radio (90)", out[0].QualityScore)
	}
	if out[0].BSSIDCount != 3 {
		t.Errorf("BSSIDCount = %d, want 3", out[0].BSSIDCount)
	}
	for _, ap := range out {
		if ap.IsHidden() {
			t.Error("a hidden network cannot be joined by name and must not be offered")
		}
	}
}

// Each CLI command is its own process, so the venue picture has to survive
// process exit or `nomadwifi scan` reports whatever single sweep it caught.
func TestCacheSurvivesProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan-cache.json")
	orig := cachePath
	cachePath = func() string { return path }
	t.Cleanup(func() { cachePath = orig })

	first := &apCache{entries: map[string]*apCacheEntry{}, persist: true}
	first.merge([]AccessPoint{
		{SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 60, Authentication: "WPA2-Personal"},
		{SSID: "Floor2", BSSID: "aa:aa:aa:aa:aa:02", SignalPercent: 50},
	})

	// A second process starts with an empty map and reads the file back.
	second := &apCache{entries: map[string]*apCacheEntry{}, persist: true}
	snap := second.snapshot()
	if len(snap) != 2 {
		t.Fatalf("restored %d APs, want 2", len(snap))
	}
	for _, ap := range snap {
		if ap.SSID == "Lobby" && ap.Authentication != "WPA2-Personal" {
			t.Errorf("security details were lost across the restart: %+v", ap)
		}
	}
}

func TestCacheWithoutPersistenceTouchesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan-cache.json")
	orig := cachePath
	cachePath = func() string { return path }
	t.Cleanup(func() { cachePath = orig })

	c := newTestCache()
	c.merge([]AccessPoint{{SSID: "Lobby", BSSID: "aa:aa:aa:aa:aa:01", SignalPercent: 60}})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an in-memory cache must not write %s", path)
	}
}
