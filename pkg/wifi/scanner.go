package wifi

import (
	"bufio"
	"bytes"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ScanNetworks scans all available wireless access points and returns them scored by quality.
func ScanNetworks() ([]AccessPoint, error) {
	cmd := SilentCommand("netsh", "wlan", "show", "networks", "mode=bssid")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return parseNetshNetworks(out)
}


func parseNetshNetworks(data []byte) ([]AccessPoint, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var aps []AccessPoint

	var currentSSID string
	var currentAuth string
	var currentCipher string

	var currentBSSID string
	var currentSignal int
	var currentRadio string
	var currentChannel int

	saveAP := func() {
		if currentBSSID == "" {
			return
		}
		band := Band24GHz
		if currentChannel > 14 && currentChannel <= 177 {
			band = Band5GHz
		} else if currentChannel > 177 {
			band = Band6GHz
		}

		ap := AccessPoint{
			SSID:           currentSSID,
			BSSID:          currentBSSID,
			SignalPercent:  currentSignal,
			Band:           band,
			Channel:        currentChannel,
			RadioType:      currentRadio,
			Authentication: currentAuth,
			Cipher:         currentCipher,
		}
		ap.QualityScore = CalculateQualityScore(ap)
		aps = append(aps, ap)

		// Reset BSSID specific fields
		currentBSSID = ""
		currentSignal = 0
		currentRadio = ""
		currentChannel = 0
	}

	reSSID := regexp.MustCompile(`(?i)^SSID\s+\d+\s+:\s*(.*)$`)
	reAuth := regexp.MustCompile(`(?i)^\s*Authentication\s+:\s*(.*)$`)
	reCipher := regexp.MustCompile(`(?i)^\s*Encryption\s+:\s*(.*)$`)
	reBSSID := regexp.MustCompile(`(?i)^\s*BSSID\s+\d+\s+:\s*([0-9a-f:]+)`)
	reSignal := regexp.MustCompile(`(?i)^\s*Signal\s+:\s*(\d+)%`)
	reRadio := regexp.MustCompile(`(?i)^\s*Radio\s+type\s+:\s*(.*)$`)
	reChannel := regexp.MustCompile(`(?i)^\s*Channel\s+:\s*(\d+)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if m := reSSID.FindStringSubmatch(line); len(m) > 1 {
			saveAP()
			currentSSID = strings.TrimSpace(m[1])
			if currentSSID == "" {
				currentSSID = "[Hidden SSID]"
			}
			continue
		}
		if m := reAuth.FindStringSubmatch(line); len(m) > 1 {
			currentAuth = strings.TrimSpace(m[1])
			continue
		}
		if m := reCipher.FindStringSubmatch(line); len(m) > 1 {
			currentCipher = strings.TrimSpace(m[1])
			continue
		}
		if m := reBSSID.FindStringSubmatch(line); len(m) > 1 {
			saveAP()
			currentBSSID = strings.TrimSpace(m[1])
			continue
		}
		if m := reSignal.FindStringSubmatch(line); len(m) > 1 {
			if s, err := strconv.Atoi(m[1]); err == nil {
				currentSignal = s
			}
			continue
		}
		if m := reRadio.FindStringSubmatch(line); len(m) > 1 {
			currentRadio = strings.TrimSpace(m[1])
			continue
		}
		if m := reChannel.FindStringSubmatch(line); len(m) > 1 {
			if ch, err := strconv.Atoi(m[1]); err == nil {
				currentChannel = ch
			}
			continue
		}
	}
	saveAP()

	// Sort by Quality Score descending
	sort.Slice(aps, func(i, j int) bool {
		return aps[i].QualityScore > aps[j].QualityScore
	})

	return aps, nil
}

// CalculateQualityScore weights band, Wi-Fi standard, signal, and channel cleanliness.
func CalculateQualityScore(ap AccessPoint) float64 {
	score := 0.0

	// 1. Wi-Fi standard weight
	radio := strings.ToLower(ap.RadioType)
	switch {
	case strings.Contains(radio, "802.11be"): // Wi-Fi 7
		score += 45.0
	case strings.Contains(radio, "802.11ax"): // Wi-Fi 6 / 6E
		score += 35.0
	case strings.Contains(radio, "802.11ac"): // Wi-Fi 5
		score += 25.0
	case strings.Contains(radio, "802.11n"): // Wi-Fi 4
		score += 10.0
	case strings.Contains(radio, "802.11g") || strings.Contains(radio, "802.11a"):
		score += 0.0
	case strings.Contains(radio, "802.11b"):
		score -= 25.0
	default:
		score += 5.0
	}

	// 2. Band weight: 5GHz/6GHz is immune to 2.4GHz hotel microwave/neighbor congestion
	switch ap.Band {
	case Band6GHz:
		score += 30.0
	case Band5GHz:
		score += 25.0
	case Band24GHz:
		// Heavy penalty for 2.4 GHz in hotel environments
		score -= 10.0
	}

	// 3. Signal strength contribution (0..40 points)
	score += float64(ap.SignalPercent) * 0.40

	// 4. Modern cipher bonus (AES CCMP vs old TKIP/WEP)
	cipher := strings.ToUpper(ap.Cipher)
	if strings.Contains(cipher, "CCMP") || strings.Contains(cipher, "AES") || strings.Contains(cipher, "GCMP") {
		score += 5.0
	} else if strings.Contains(cipher, "TKIP") || strings.Contains(cipher, "WEP") {
		score -= 20.0
	}

	return score
}
