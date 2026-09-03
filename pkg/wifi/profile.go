package wifi

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reKeyContent = regexp.MustCompile(`(?i)Key\s+Content\s*:\s*(.+)`)
	reProfile    = regexp.MustCompile(`(?i)All\s+User\s+Profile\s*:\s*(.+)`)
	reSuffixes   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[-_ ]*(5g|5ghz|2\.4g|2\.4ghz|2g|guest|lobby)$`),
		regexp.MustCompile(`(?i)[-_ ]*floor\s*\d+.*$`),
		regexp.MustCompile(`(?i)[-_ ]*\d+f$`),
		regexp.MustCompile(`(?i)[-_ ]*floor$`),
		regexp.MustCompile(`\d+$`),
	}
)

func cleanVenueName(ssid string) string {
	clean := strings.TrimSpace(ssid)
	if clean == "" || clean == "[Hidden SSID]" {
		return ""
	}
	for {
		changed := false
		for _, pattern := range reSuffixes {
			if pattern.MatchString(clean) {
				newClean := strings.TrimRight(pattern.ReplaceAllString(clean, ""), "-_ ")
				if len(newClean) >= 2 && newClean != clean {
					clean = newClean
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	if len(clean) < 2 {
		return strings.TrimSpace(ssid)
	}
	return strings.ToLower(clean)
}

// GetSavedProfiles returns all Wi-Fi profile names saved in Windows.
func GetSavedProfiles() ([]string, error) {
	cmd := SilentCommand("netsh", "wlan", "show", "profiles")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var profiles []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := reProfile.FindStringSubmatch(line)
		if len(m) > 1 {
			p := strings.TrimSpace(m[1])
			if p != "" {
				profiles = append(profiles, p)
			}
		}
	}
	return profiles, nil
}

// HasProfile checks if a Windows profile exists for the given SSID.
func HasProfile(ssid string) bool {
	profiles, err := GetSavedProfiles()
	if err != nil {
		return false
	}
	for _, p := range profiles {
		if strings.EqualFold(p, ssid) {
			return true
		}
	}
	return false
}

// GetProfilePassword retrieves the cleartext password for a saved Wi-Fi profile.
func GetProfilePassword(profileName string) (string, error) {
	cmd := SilentCommand("netsh", "wlan", "show", "profile", fmt.Sprintf("name=%s", profileName), "key=clear")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := reKeyContent.FindStringSubmatch(line)
		if len(m) > 1 {
			return strings.TrimSpace(m[1]), nil
		}
	}
	return "", fmt.Errorf("no password found for profile %s", profileName)
}

// GuessPasswordForSSID searches related hotel cluster profiles to find the most likely shared password.
func GuessPasswordForSSID(targetSSID string) (string, string) {
	targetVenue := cleanVenueName(targetSSID)

	// 1. Try currently connected network first
	status, err := GetInterfaceStatus()
	if err == nil && status != nil && status.Connected && status.SSID != "" {
		activeVenue := cleanVenueName(status.SSID)
		if targetVenue != "" && (targetVenue == activeVenue || strings.HasPrefix(targetVenue, activeVenue) || strings.HasPrefix(activeVenue, targetVenue)) {
			if pwd, err := GetProfilePassword(status.SSID); err == nil && pwd != "" {
				return pwd, fmt.Sprintf("active hotel connection '%s'", status.SSID)
			}
		}
	}

	// 2. Search all saved profiles for matching prefix cluster
	profiles, err := GetSavedProfiles()
	if err != nil {
		return "", ""
	}

	for _, p := range profiles {
		pVenue := cleanVenueName(p)
		if targetVenue != "" && (targetVenue == pVenue || strings.HasPrefix(targetVenue, pVenue) || strings.HasPrefix(pVenue, targetVenue)) {
			if pwd, err := GetProfilePassword(p); err == nil && pwd != "" {
				return pwd, fmt.Sprintf("related hotel profile '%s'", p)
			}
		}
	}

	// 3. Fallback: Check 2-letter venue codes (e.g. "SM" in "SM Resort" and "SMFloor21")
	for _, p := range profiles {
		if len(targetSSID) >= 2 && len(p) >= 2 {
			if strings.EqualFold(targetSSID[:2], p[:2]) {
				if pwd, err := GetProfilePassword(p); err == nil && pwd != "" {
					return pwd, fmt.Sprintf("matching venue code profile '%s'", p)
				}
			}
		}
	}

	// 4. Fallback to active connection password
	if status != nil && status.Connected && status.SSID != "" {
		if pwd, err := GetProfilePassword(status.SSID); err == nil && pwd != "" {
			return pwd, fmt.Sprintf("current network '%s'", status.SSID)
		}
	}

	return "", ""
}

// AddWifiProfile generates and registers a WPA2-PSK profile XML in Windows.
func AddWifiProfile(ssid, password string) error {
	xmlTemplate := `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
    <name>%s</name>
    <SSIDConfig>
        <SSID>
            <name>%s</name>
        </SSID>
    </SSIDConfig>
    <connectionType>ESS</connectionType>
    <connectionMode>auto</connectionMode>
    <MSM>
        <security>
            <authEncryption>
                <authentication>WPA2PSK</authentication>
                <encryption>AES</encryption>
                <useOneX>false</useOneX>
            </authEncryption>
            <sharedKey>
                <keyType>passPhrase</keyType>
                <protected>false</protected>
                <keyMaterial>%s</keyMaterial>
            </sharedKey>
        </security>
    </MSM>
</WLANProfile>`

	xmlContent := fmt.Sprintf(xmlTemplate, ssid, ssid, password)
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("nomadwifi_%x.xml", ssid))
	if err := os.WriteFile(tempFile, []byte(xmlContent), 0600); err != nil {
		return fmt.Errorf("failed to create temporary profile file: %w", err)
	}
	defer os.Remove(tempFile)

	cmd := SilentCommand("netsh", "wlan", "add", "profile", fmt.Sprintf("filename=%s", tempFile), "user=all")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh add profile failed: %w (out: %s)", err, string(out))
	}
	return nil
}
