package wifi

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// bandFromFrequencyKHz maps an AP center frequency (kHz) to its band.
// This is the authoritative mapping: 6 GHz channel numbers overlap 2.4/5 GHz
// numbering, so channel-based inference cannot distinguish them.
func bandFromFrequencyKHz(khz uint32) Band {
	mhz := khz / 1000
	switch {
	case mhz >= 2400 && mhz <= 2500:
		return Band24GHz
	case mhz >= 5150 && mhz <= 5895:
		return Band5GHz
	case mhz >= 5925 && mhz <= 7125:
		return Band6GHz
	}
	return BandOther
}

// bandFromLabel parses a netsh "Band" field value such as "5 GHz".
func bandFromLabel(val string) Band {
	v := strings.TrimSpace(val)
	switch {
	case strings.HasPrefix(v, "2.4"):
		return Band24GHz
	case strings.HasPrefix(v, "5"):
		return Band5GHz
	case strings.HasPrefix(v, "6"):
		return Band6GHz
	}
	return BandOther
}

// bandFromChannel is the legacy fallback for adapters whose netsh output
// predates the per-BSSID "Band" field. It cannot detect 6 GHz.
func bandFromChannel(ch int) Band {
	if ch <= 0 {
		return BandOther
	}
	if ch <= 14 {
		return Band24GHz
	}
	return Band5GHz
}

// isOpenNetwork reports whether an AP requires no key material at all.
func isOpenNetwork(ap AccessPoint) bool {
	return strings.EqualFold(strings.TrimSpace(ap.Authentication), "Open") ||
		strings.EqualFold(strings.TrimSpace(ap.Cipher), "None")
}

// signalPercentFromRSSI applies the Windows link-quality scale
// (-100 dBm = 0%, -50 dBm = 100%) to a measured RSSI.
//
// The driver reports uLinkQuality as ~99% for the BSS we are currently
// associated with regardless of its actual RSSI, which would inflate the
// incumbent AP by around 20 points and bias every roam decision towards
// staying put. Deriving the percentage from RSSI keeps all APs on one scale.
func signalPercentFromRSSI(rssi int) int {
	if rssi == 0 {
		return 0
	}
	pct := (rssi + 100) * 2
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// rssiFromSignalPercent reverses the Windows link-quality mapping
// (0% = -100 dBm, 100% = -50 dBm) for adapters that only report a percentage.
func rssiFromSignalPercent(pct int) int {
	if pct <= 0 {
		return 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct/2 - 100
}

// ScanNetworks returns the networks in range, one row per SSID, ranked by
// quality and connection readiness.
func ScanNetworks() ([]AccessPoint, error) {
	aps, err := ScanAllBSSIDs()
	if err != nil {
		return nil, err
	}
	return DedupeBySSID(aps), nil
}

// ScanAllBSSIDs returns every individual radio in range. Roaming works at BSSID
// granularity, so the roam engine uses this rather than the collapsed view.
func ScanAllBSSIDs() ([]AccessPoint, error) {
	fresh, err := scanRaw()
	if err != nil && len(fresh) == 0 {
		// Nothing new this round: fall back to what the cache still remembers
		// rather than reporting the venue as empty.
		if cached := scanCache.snapshot(); len(cached) > 0 {
			AnnotateAuthStatus(cached)
			MarkWarmStatus(cached)
			RankAccessPoints(cached)
			return cached, nil
		}
		return nil, err
	}

	scanCache.merge(fresh)
	aps := scanCache.snapshot()

	AnnotateAuthStatus(aps)
	MarkWarmStatus(aps)
	RankAccessPoints(aps)
	return aps, nil
}

// RankAccessPoints scores every AP in place and sorts the slice best-first.
func RankAccessPoints(aps []AccessPoint) {
	for i := range aps {
		aps[i].QualityScore, aps[i].Reasons = ScoreAccessPoint(aps[i])
	}
	sort.SliceStable(aps, func(i, j int) bool {
		return aps[i].QualityScore > aps[j].QualityScore
	})
}

// AnnotateAuthStatus classifies each AP by whether we can connect to it today.
func AnnotateAuthStatus(aps []AccessPoint) {
	savedMap := savedProfileSet()

	for i := range aps {
		ssid := strings.TrimSpace(aps[i].SSID)
		switch {
		case isOpenNetwork(aps[i]):
			aps[i].AuthStatus = AuthStatusOpen
		case savedMap[strings.ToLower(ssid)]:
			aps[i].AuthStatus = AuthStatusSaved
		default:
			if pwd, source := GuessPasswordForSSID(ssid); pwd != "" {
				aps[i].AuthStatus = AuthStatusInferred
				aps[i].InferredFrom = source
			} else {
				aps[i].AuthStatus = AuthStatusLocked
			}
		}
	}
}

func savedProfileSet() map[string]bool {
	profiles, _ := GetSavedProfiles()
	set := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		set[strings.ToLower(strings.TrimSpace(p))] = true
	}
	return set
}

// scanRaw returns unscored access points from the platform scanner, preferring
// the Native Wifi API and falling back to netsh where it is unavailable.
// coldScanBudget bounds how long a scan waits for the driver when there is no
// cached picture of the venue to fall back on.
const coldScanBudget = 2500 * time.Millisecond

func scanRaw() ([]AccessPoint, error) {
	if NativeAvailable() {
		// Ask for a fresh sweep; the driver may rate-limit us, in which case
		// we still read the most recent results below.
		_ = TriggerScan()
		aps, err := nativeScan()
		if err == nil && scanCache.cold() {
			// A sweep takes the radio a second or two, and with nothing cached
			// an immediate read returns whatever few BSSIDs were already in the
			// driver's list. Give it a moment to fill in the rest.
			aps = settleScan(aps)
		}
		if err == nil && len(aps) > 0 {
			return aps, nil
		}
	}

	cmd := SilentCommand("netsh", "wlan", "show", "networks", "mode=bssid")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseNetshNetworks(out)
}

// settleScan re-reads the driver's BSS list until it stops growing, so a
// cold start reports the whole venue instead of the first few radios to
// answer. It returns as soon as two consecutive reads agree.
func settleScan(first []AccessPoint) []AccessPoint {
	best := first
	deadline := time.Now().Add(coldScanBudget)

	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		next, err := nativeScan()
		if err != nil {
			break
		}
		if len(next) <= len(best) {
			return best
		}
		best = next
	}
	return best
}

func parseNetshNetworks(data []byte) ([]AccessPoint, error) {
	reSSID := regexp.MustCompile(`(?i)^SSID\s+\d+\s+:\s*(.*)$`)
	reAuth := regexp.MustCompile(`(?i)^Authentication\s+:\s*(.*)$`)
	reCipher := regexp.MustCompile(`(?i)^Encryption\s+:\s*(.*)$`)
	reBSSID := regexp.MustCompile(`(?i)^BSSID\s+\d+\s+:\s*([0-9a-f:]+)`)
	reSignal := regexp.MustCompile(`(?i)^Signal\s+:\s*(\d+)%`)
	reRadio := regexp.MustCompile(`(?i)^Radio\s+type\s+:\s*(.*)$`)
	reBand := regexp.MustCompile(`(?i)^Band\s+:\s*(.*)$`)
	reChannel := regexp.MustCompile(`(?i)^Channel\s+:\s*(\d+)\s*$`)
	reChanUtil := regexp.MustCompile(`(?i)^Channel\s+Utilization\s*:\s*\d+\s*\((\d+)\s*%\)`)

	var aps []AccessPoint

	var ssid, auth, cipher string
	var bssid, radio string
	var signal, channel, util int
	var band Band
	var haveUtil bool

	saveAP := func() {
		if bssid == "" {
			return
		}
		b := band
		if b == "" || b == BandOther {
			b = bandFromChannel(channel)
		}
		aps = append(aps, AccessPoint{
			SSID:               ssid,
			BSSID:              bssid,
			SignalPercent:      signal,
			RSSI:               rssiFromSignalPercent(signal),
			Band:               b,
			Channel:            channel,
			RadioType:          radio,
			Authentication:     auth,
			Cipher:             cipher,
			ChannelUtilization: util,
			HasChannelUtil:     haveUtil,
		})

		// Reset BSSID-scoped fields only; auth/cipher stay until the next SSID header.
		bssid, radio, band = "", "", ""
		signal, channel, util = 0, 0, 0
		haveUtil = false
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if m := reSSID.FindStringSubmatch(line); len(m) > 1 {
			saveAP()
			ssid = strings.TrimSpace(m[1])
			if ssid == "" {
				ssid = HiddenSSID
			}
			// Auth/cipher are per-SSID: clear them so a network that omits the
			// fields cannot inherit the previous network's values.
			auth, cipher = "", ""
			continue
		}
		if m := reAuth.FindStringSubmatch(line); len(m) > 1 {
			auth = strings.TrimSpace(m[1])
			continue
		}
		if m := reCipher.FindStringSubmatch(line); len(m) > 1 {
			cipher = strings.TrimSpace(m[1])
			continue
		}
		if m := reBSSID.FindStringSubmatch(line); len(m) > 1 {
			saveAP()
			bssid = strings.TrimSpace(m[1])
			continue
		}
		if m := reSignal.FindStringSubmatch(line); len(m) > 1 {
			signal, _ = strconv.Atoi(m[1])
			continue
		}
		if m := reRadio.FindStringSubmatch(line); len(m) > 1 {
			radio = strings.TrimSpace(m[1])
			continue
		}
		if m := reBand.FindStringSubmatch(line); len(m) > 1 {
			band = bandFromLabel(m[1])
			continue
		}
		if m := reChanUtil.FindStringSubmatch(line); len(m) > 1 {
			if u, err := strconv.Atoi(m[1]); err == nil {
				util, haveUtil = u, true
			}
			continue
		}
		if m := reChannel.FindStringSubmatch(line); len(m) > 1 {
			channel, _ = strconv.Atoi(m[1])
			continue
		}
	}
	saveAP()

	return aps, nil
}

// CalculateQualityScore weights band, Wi-Fi standard, signal, cipher,
// airtime congestion, and connection readiness.
func CalculateQualityScore(ap AccessPoint) float64 {
	score, _ := ScoreAccessPoint(ap)
	return score
}

// ScoreAccessPoint returns the quality score alongside the human-readable
// factors that produced it, so the UI and roam logs can explain a decision.
func ScoreAccessPoint(ap AccessPoint) (float64, []string) {
	score := 0.0
	var reasons []string

	add := func(pts float64, why string) {
		score += pts
		if why != "" {
			reasons = append(reasons, why)
		}
	}

	// 1. Wi-Fi standard
	radio := strings.ToLower(ap.RadioType)
	switch {
	case strings.Contains(radio, "802.11be"):
		add(45.0, "Wi-Fi 7 (802.11be)")
	case strings.Contains(radio, "802.11ax"):
		add(35.0, "Wi-Fi 6 (802.11ax)")
	case strings.Contains(radio, "802.11ac"):
		add(25.0, "Wi-Fi 5 (802.11ac)")
	case strings.Contains(radio, "802.11n"):
		add(10.0, "Wi-Fi 4 (802.11n)")
	case strings.Contains(radio, "802.11g") || strings.Contains(radio, "802.11a"):
		add(0.0, "Legacy 802.11a/g")
	case strings.Contains(radio, "802.11b"):
		add(-25.0, "Obsolete 802.11b")
	default:
		add(5.0, "")
	}

	// 2. Band
	switch ap.Band {
	case Band6GHz:
		add(30.0, "6 GHz band (clean spectrum)")
	case Band5GHz:
		add(25.0, "5 GHz band")
	case Band24GHz:
		add(-10.0, "2.4 GHz band (congested)")
	}

	// 3. Signal strength (0..40 points)
	add(float64(ap.SignalPercent)*0.40, "")

	// 4. Cipher
	cipher := strings.ToUpper(ap.Cipher)
	if strings.Contains(cipher, "CCMP") || strings.Contains(cipher, "AES") || strings.Contains(cipher, "GCMP") {
		add(5.0, "")
	} else if strings.Contains(cipher, "TKIP") || strings.Contains(cipher, "WEP") {
		add(-20.0, "Legacy cipher (TKIP/WEP)")
	}

	// 5. Airtime congestion, when the AP advertises a BSS Load element.
	if ap.HasChannelUtil {
		switch {
		case ap.ChannelUtilization >= 70:
			add(-20.0, "AP airtime heavily congested")
		case ap.ChannelUtilization >= 40:
			add(-8.0, "AP airtime moderately busy")
		case ap.ChannelUtilization <= 15:
			add(6.0, "AP airtime nearly idle")
		}
	}

	// 6. Connection readiness
	switch ap.AuthStatus {
	case AuthStatusSaved:
		add(15.0, "Saved network")
	case AuthStatusInferred:
		add(10.0, "Venue password inferred")
	case AuthStatusOpen:
		add(0.0, "Open network")
	case AuthStatusLocked:
		add(-50.0, "Password required")
	}

	return score, reasons
}
