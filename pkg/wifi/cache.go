package wifi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dariomory/nomadwifi/pkg/state"
)

// The wireless service reports whatever its last scan happened to see, and
// consecutive queries can legitimately return 19 access points, then 1, then 4.
// Rolling every observation into a short-lived cache keyed by BSSID turns that
// into a stable picture of the venue, which is what roaming decisions need.

const (
	apCacheTTL    = 75 * time.Second
	signalEMAGain = 0.5
	// The CLI is one process per command, so an in-memory cache would be cold
	// on every invocation and each `nomadwifi scan` would show whatever single
	// scan the driver happened to return. Persisting it keeps the venue
	// picture stable across runs.
	cacheSaveInterval = 5 * time.Second
)

type apCacheEntry struct {
	ap       AccessPoint
	lastSeen time.Time
}

type apCache struct {
	mu      sync.Mutex
	entries map[string]*apCacheEntry
	// persist is set only on the process-wide cache; test caches stay in
	// memory so they neither read nor overwrite the user's real venue file.
	persist  bool
	loaded   bool
	lastSave time.Time
}

var scanCache = &apCache{entries: map[string]*apCacheEntry{}, persist: true}

// cachePath is a variable so tests can redirect persistence to a temp dir.
var cachePath = func() string { return filepath.Join(state.Dir(), "scan-cache.json") }

type persistedEntry struct {
	AP       AccessPoint `json:"ap"`
	LastSeen time.Time   `json:"last_seen"`
}

// load pulls the previous process's observations back in. Callers hold c.mu.
func (c *apCache) loadLocked() {
	if c.loaded || !c.persist {
		return
	}
	c.loaded = true

	raw, err := os.ReadFile(cachePath())
	if err != nil {
		return
	}
	var saved []persistedEntry
	if json.Unmarshal(raw, &saved) != nil {
		return
	}

	now := time.Now()
	for _, e := range saved {
		if now.Sub(e.LastSeen) > apCacheTTL {
			continue
		}
		key := strings.ToLower(e.AP.BSSID)
		if key == "" {
			continue
		}
		c.entries[key] = &apCacheEntry{ap: e.AP, lastSeen: e.LastSeen}
	}
}

// saveLocked writes the cache out atomically. Callers hold c.mu.
func (c *apCache) saveLocked() {
	now := time.Now()
	if !c.persist || now.Sub(c.lastSave) < cacheSaveInterval {
		return
	}
	c.lastSave = now

	saved := make([]persistedEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		saved = append(saved, persistedEntry{AP: entry.ap, LastSeen: entry.lastSeen})
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return
	}

	path := cachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

// cold reports whether the cache holds nothing worth trusting yet, which is
// the case where a scan has to wait for the driver instead of reading through.
func (c *apCache) cold() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	return len(c.entries) == 0
}

// merge folds a fresh observation into the cache, smoothing signal strength so
// a single noisy sample cannot trigger a roam.
func (c *apCache) merge(aps []AccessPoint) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()

	for _, ap := range aps {
		key := strings.ToLower(ap.BSSID)
		if key == "" {
			continue
		}
		if prev, ok := c.entries[key]; ok {
			ap.SignalPercent = int(ema(float64(prev.ap.SignalPercent), float64(ap.SignalPercent)))
			if ap.RSSI != 0 && prev.ap.RSSI != 0 {
				ap.RSSI = int(ema(float64(prev.ap.RSSI), float64(ap.RSSI)))
			}
			// Keep the last known security details if this sample omitted them.
			if ap.Authentication == "" {
				ap.Authentication = prev.ap.Authentication
			}
			if ap.Cipher == "" {
				ap.Cipher = prev.ap.Cipher
			}
			if ap.RadioType == "" {
				ap.RadioType = prev.ap.RadioType
			}
		}
		c.entries[key] = &apCacheEntry{ap: ap, lastSeen: now}
	}

	for key, entry := range c.entries {
		if now.Sub(entry.lastSeen) > apCacheTTL {
			delete(c.entries, key)
		}
	}

	c.saveLocked()
}

func ema(prev, next float64) float64 {
	return prev*(1-signalEMAGain) + next*signalEMAGain
}

// snapshot returns every access point seen recently enough to still be trusted.
func (c *apCache) snapshot() []AccessPoint {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()

	out := make([]AccessPoint, 0, len(c.entries))
	for _, entry := range c.entries {
		if now.Sub(entry.lastSeen) <= apCacheTTL {
			out = append(out, entry.ap)
		}
	}
	return out
}

func (c *apCache) reset() {
	c.mu.Lock()
	c.entries = map[string]*apCacheEntry{}
	c.mu.Unlock()
}

// DedupeBySSID collapses the per-BSSID list down to one row per network,
// keeping the best-scoring radio and recording how many were seen. A venue
// with four radios on one SSID should read as one joinable network, not four.
func DedupeBySSID(aps []AccessPoint) []AccessPoint {
	best := map[string]AccessPoint{}
	counts := map[string]int{}
	var order []string

	for _, ap := range aps {
		if ap.IsHidden() {
			// A hidden network cannot be joined by name, so it is not offered.
			continue
		}
		key := strings.ToLower(ap.SSID)
		counts[key]++
		if current, ok := best[key]; !ok {
			best[key] = ap
			order = append(order, key)
		} else if ap.QualityScore > current.QualityScore {
			best[key] = ap
		}
	}

	out := make([]AccessPoint, 0, len(best))
	for _, key := range order {
		ap := best[key]
		ap.BSSIDCount = counts[key]
		out = append(out, ap)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].QualityScore > out[j].QualityScore
	})
	return out
}
