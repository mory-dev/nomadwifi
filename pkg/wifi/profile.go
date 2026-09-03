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
		regexp.MustCompile(`(?i)[-_ ]*(5g|5ghz|2\.4g|2\.4ghz|2g|guest|lobby|vip|open|free)$`),
		regexp.MustCompile(`(?i)[-_ ]*floor\s*\d+.*$`),
		regexp.MustCompile(`(?i)[-_ ]*\d+f.*$`),
		regexp.MustCompile(`(?i)[-_ ]*f\d+.*$`),
		regexp.MustCompile(`(?i)[-_ ]*floor$`),
		regexp.MustCompile(`\d+$`),
	}
)

// ExtractVenueRoot normalizes an SSID down to its core venue identifier.
func ExtractVenueRoot(ssid string) string {
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
	return strings.ToLower(strings.TrimSpace(clean))
}

// IsSameHotelVenue checks if two SSIDs belong to the same hotel / venue cluster.
func IsSameHotelVenue(ssidA, ssidB string) bool {
	if strings.EqualFold(ssidA, ssidB) {
		return true
	}
	rootA := ExtractVenueRoot(ssidA)
	rootB := ExtractVenueRoot(ssidB)

	if rootA == "" || rootB == "" {
		return false
	}

	// Exact root match (e.g. "smfloor" == "smfloor", "patong blue" == "patong blue")
	if rootA == rootB && len(rootA) >= 3 {
		return true
	}

	// Prefix match with significant length
	sA := strings.ToLower(strings.TrimSpace(ssidA))
	sB := strings.ToLower(strings.TrimSpace(ssidB))

	if strings.HasPrefix(sA, "sm") && strings.HasPrefix(sB, "sm") {
		return true
	}

	if len(rootA) >= 4 && len(rootB) >= 4 {
		if strings.HasPrefix(rootA, rootB) || strings.HasPrefix(rootB, rootA) {
			return true
		}
	}

	return false
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
			if p != "" && p != "[Hidden SSID]" {
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
	cleanTarget := strings.TrimSpace(targetSSID)
	if cleanTarget == "" || cleanTarget == "[Hidden SSID]" {
		return "", ""
	}

	// 1. Check active connection if in same venue
	status, err := GetInterfaceStatus()
	if err == nil && status != nil && status.Connected && status.SSID != "" {
		if IsSameHotelVenue(cleanTarget, status.SSID) {
			if pwd, err := GetProfilePassword(status.SSID); err == nil && pwd != "" {
				return pwd, fmt.Sprintf("active hotel network '%s'", status.SSID)
			}
		}
	}

	// 2. Search saved profiles for matching hotel cluster
	profiles, err := GetSavedProfiles()
	if err != nil {
		return "", ""
	}

	for _, p := range profiles {
		if IsSameHotelVenue(cleanTarget, p) {
			if pwd, err := GetProfilePassword(p); err == nil && pwd != "" {
				return pwd, fmt.Sprintf("saved hotel profile '%s'", p)
			}
		}
	}

	return "", ""
}

// AddWifiProfile generates and registers a WPA2-PSK profile XML in Windows.
func AddWifiProfile(ssid, password string) error {
	if strings.TrimSpace(ssid) == "" || strings.TrimSpace(ssid) == "[Hidden SSID]" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("invalid ssid or password")
	}

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

// DeleteWifiProfile removes a profile from Windows.
func DeleteWifiProfile(ssid string) error {
	cmd := SilentCommand("netsh", "wlan", "delete", "profile", fmt.Sprintf("name=%s", ssid))
	return cmd.Run()
}
