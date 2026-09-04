package wifi

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
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

// SecurityKind is the profile security template needed to associate with a network.
type SecurityKind string

const (
	SecurityOpen       SecurityKind = "open"
	SecurityWPAPSK     SecurityKind = "WPAPSK"
	SecurityWPA2PSK    SecurityKind = "WPA2PSK"
	SecurityWPA3SAE    SecurityKind = "WPA3SAE"
	SecurityEnterprise SecurityKind = "enterprise"
)

// SecurityForNetwork maps the netsh authentication/encryption pair onto the
// profile template Windows needs. Getting this wrong is why a hardcoded
// WPA2PSK template silently fails on WPA3 and open networks.
func SecurityForNetwork(authentication, cipher string) SecurityKind {
	auth := strings.ToLower(strings.TrimSpace(authentication))
	enc := strings.ToLower(strings.TrimSpace(cipher))

	switch {
	case auth == "" && (enc == "" || enc == "none"):
		return SecurityOpen
	case strings.Contains(auth, "open"), enc == "none":
		return SecurityOpen
	case strings.Contains(auth, "enterprise"), strings.Contains(auth, "802.1x"):
		return SecurityEnterprise
	case strings.Contains(auth, "wpa3"), strings.Contains(auth, "sae"):
		return SecurityWPA3SAE
	case strings.Contains(auth, "wpa2"):
		return SecurityWPA2PSK
	case strings.Contains(auth, "wpa"):
		return SecurityWPAPSK
	}
	return SecurityWPA2PSK
}

// SupportsAutoProfile reports whether NomadWiFi can synthesize a working
// profile for this security kind. Enterprise networks need credentials and
// certificate policy we cannot infer.
func (s SecurityKind) SupportsAutoProfile() bool {
	return s != SecurityEnterprise
}

// NeedsPassword reports whether a key is required to build the profile.
func (s SecurityKind) NeedsPassword() bool {
	return s == SecurityWPAPSK || s == SecurityWPA2PSK || s == SecurityWPA3SAE
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return ""
	}
	return buf.String()
}

// BuildProfileXML renders a Windows WLAN profile for the given network.
//
// connectionMode is always "manual": NomadWiFi decides when to associate, and
// an "auto" profile would let Windows reconnect on its own schedule, fighting
// the roaming logic.
func BuildProfileXML(ssid, password string, security SecurityKind, hidden bool) (string, error) {
	ssid = strings.TrimSpace(ssid)
	if ssid == "" || ssid == HiddenSSID {
		return "", fmt.Errorf("cannot build a profile for an unnamed network")
	}
	if !security.SupportsAutoProfile() {
		return "", fmt.Errorf("enterprise networks require manual credentials")
	}
	if security.NeedsPassword() && strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("network %q requires a password", ssid)
	}

	var msm string
	if security == SecurityOpen {
		msm = `        <security>
            <authEncryption>
                <authentication>open</authentication>
                <encryption>none</encryption>
                <useOneX>false</useOneX>
            </authEncryption>
        </security>`
	} else {
		encryption := "AES"
		if security == SecurityWPAPSK {
			encryption = "TKIP"
		}
		keyType := "passPhrase"
		msm = fmt.Sprintf(`        <security>
            <authEncryption>
                <authentication>%s</authentication>
                <encryption>%s</encryption>
                <useOneX>false</useOneX>
            </authEncryption>
            <sharedKey>
                <keyType>%s</keyType>
                <protected>false</protected>
                <keyMaterial>%s</keyMaterial>
            </sharedKey>
        </security>`, security, encryption, keyType, xmlEscape(password))
	}

	nonBroadcast := "false"
	if hidden {
		nonBroadcast = "true"
	}

	esc := xmlEscape(ssid)
	return fmt.Sprintf(`<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
    <name>%s</name>
    <SSIDConfig>
        <SSID>
            <name>%s</name>
        </SSID>
        <nonBroadcast>%s</nonBroadcast>
    </SSIDConfig>
    <connectionType>ESS</connectionType>
    <connectionMode>manual</connectionMode>
    <MSM>
%s
    </MSM>
</WLANProfile>`, esc, esc, nonBroadcast, msm), nil
}

// AddWifiProfile registers a profile for the given network in Windows.
//
// Profiles are added with user=current so warming works without elevation;
// user=all requires an administrator token that a normal desktop session
// does not have.
func AddWifiProfile(ssid, password string) error {
	return AddWifiProfileFor(ssid, password, SecurityWPA2PSK, false)
}

// AddWifiProfileFor registers a profile using the security template that
// actually matches the target network.
func AddWifiProfileFor(ssid, password string, security SecurityKind, hidden bool) error {
	xmlContent, err := BuildProfileXML(ssid, password, security, hidden)
	if err != nil {
		return err
	}

	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("nomadwifi_%x.xml", ssid))
	if err := os.WriteFile(tempFile, []byte(xmlContent), 0600); err != nil {
		return fmt.Errorf("failed to create temporary profile file: %w", err)
	}
	defer os.Remove(tempFile)

	cmd := SilentCommand("netsh", "wlan", "add", "profile",
		fmt.Sprintf("filename=%s", tempFile), "user=current")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh add profile failed: %w (out: %s)", err, strings.TrimSpace(string(out)))
	}

	profiles.invalidate()
	return nil
}

// DeleteWifiProfile removes a profile from Windows.
func DeleteWifiProfile(ssid string) error {
	cmd := SilentCommand("netsh", "wlan", "delete", "profile", fmt.Sprintf("name=%s", ssid))
	err := cmd.Run()
	profiles.invalidate()
	return err
}

// profileCache memoizes saved profile names and their cleartext keys.
//
// Without this, classifying one scan re-runs "netsh wlan show profile
// key=clear" once per candidate access point, which is what made a scan take
// seconds rather than milliseconds.
type profileCache struct {
	mu        sync.Mutex
	names     []string
	namesAt   time.Time
	passwords map[string]string
}

var profiles = &profileCache{passwords: map[string]string{}}

const profileCacheTTL = 30 * time.Second

func (c *profileCache) invalidate() {
	c.mu.Lock()
	c.names = nil
	c.namesAt = time.Time{}
	c.mu.Unlock()
}

func (c *profileCache) list() ([]string, error) {
	c.mu.Lock()
	if c.names != nil && time.Since(c.namesAt) < profileCacheTTL {
		out := make([]string, len(c.names))
		copy(out, c.names)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	cmd := SilentCommand("netsh", "wlan", "show", "profiles")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if m := reProfile.FindStringSubmatch(scanner.Text()); len(m) > 1 {
			if p := strings.TrimSpace(m[1]); p != "" && p != HiddenSSID {
				names = append(names, p)
			}
		}
	}

	c.mu.Lock()
	c.names = names
	c.namesAt = time.Now()
	c.mu.Unlock()

	result := make([]string, len(names))
	copy(result, names)
	return result, nil
}

func (c *profileCache) password(profileName string) (string, error) {
	key := strings.ToLower(profileName)

	c.mu.Lock()
	if pwd, ok := c.passwords[key]; ok {
		c.mu.Unlock()
		if pwd == "" {
			return "", fmt.Errorf("no password stored for profile %s", profileName)
		}
		return pwd, nil
	}
	c.mu.Unlock()

	cmd := SilentCommand("netsh", "wlan", "show", "profile",
		fmt.Sprintf("name=%s", profileName), "key=clear")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	pwd := ""
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if m := reKeyContent.FindStringSubmatch(scanner.Text()); len(m) > 1 {
			pwd = strings.TrimSpace(m[1])
			break
		}
	}

	c.mu.Lock()
	c.passwords[key] = pwd
	c.mu.Unlock()

	if pwd == "" {
		return "", fmt.Errorf("no password found for profile %s", profileName)
	}
	return pwd, nil
}

// GetSavedProfiles returns all Wi-Fi profile names saved in Windows.
func GetSavedProfiles() ([]string, error) {
	return profiles.list()
}

// HasProfile checks if a Windows profile exists for the given SSID.
func HasProfile(ssid string) bool {
	names, err := profiles.list()
	if err != nil {
		return false
	}
	for _, p := range names {
		if strings.EqualFold(p, ssid) {
			return true
		}
	}
	return false
}

// GetProfilePassword retrieves the cleartext password for a saved Wi-Fi profile.
func GetProfilePassword(profileName string) (string, error) {
	return profiles.password(profileName)
}

// ExtractVenueRoot normalizes an SSID down to its core venue identifier.
func ExtractVenueRoot(ssid string) string {
	clean := strings.TrimSpace(ssid)
	if clean == "" || clean == HiddenSSID {
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

// reVenueDecoration matches the trailing fragment that distinguishes sibling
// SSIDs inside one venue: a band tag, a floor marker, or an area name.
var reVenueDecoration = regexp.MustCompile(`(?i)^[-_ ]*(5g|5ghz|6g|6ghz|2\.4g|2\.4ghz|2g|guest|lobby|vip|open|free|floor\s*\d*|\d+f|f\d+|\d+)$`)

// IsSameHotelVenue reports whether two SSIDs belong to the same venue cluster.
//
// Venues broadcast sibling SSIDs that differ only by a band or floor tag
// ("SMFLoor21" / "SMFLoor21_5G"), and they almost always share one password.
func IsSameHotelVenue(ssidA, ssidB string) bool {
	a := strings.TrimSpace(ssidA)
	b := strings.TrimSpace(ssidB)
	if a == "" || b == "" || a == HiddenSSID || b == HiddenSSID {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}

	// One SSID is the other plus a band/floor decoration.
	shorter, longer := a, b
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if len(shorter) >= 4 && strings.HasPrefix(strings.ToLower(longer), strings.ToLower(shorter)) {
		if reVenueDecoration.MatchString(longer[len(shorter):]) {
			return true
		}
	}

	rootA := ExtractVenueRoot(a)
	rootB := ExtractVenueRoot(b)
	if rootA == "" || rootB == "" {
		return false
	}

	// Both reduce to the same venue name.
	if rootA == rootB && len(rootA) >= 3 {
		return true
	}

	// One venue name is a prefix of the other ("baannt" / "baanntvilla").
	if len(rootA) >= 4 && len(rootB) >= 4 {
		if strings.HasPrefix(rootA, rootB) || strings.HasPrefix(rootB, rootA) {
			return true
		}
	}

	return false
}

// GuessPasswordForSSID searches related venue profiles for a shared password.
//
// Hotels and co-working spaces routinely put the same key on every floor SSID,
// so a saved sibling profile is usually the right key for a new one.
func GuessPasswordForSSID(targetSSID string) (string, string) {
	cleanTarget := strings.TrimSpace(targetSSID)
	if cleanTarget == "" || cleanTarget == HiddenSSID {
		return "", ""
	}

	// 1. Prefer the network we are on right now, if it is the same venue.
	if current := CurrentSSID(); current != "" && IsSameHotelVenue(cleanTarget, current) {
		if pwd, err := profiles.password(current); err == nil && pwd != "" {
			return pwd, fmt.Sprintf("active hotel network '%s'", current)
		}
	}

	// 2. Otherwise search saved profiles for a venue sibling.
	names, err := profiles.list()
	if err != nil {
		return "", ""
	}
	for _, p := range names {
		if strings.EqualFold(p, cleanTarget) {
			continue
		}
		if IsSameHotelVenue(cleanTarget, p) {
			if pwd, err := profiles.password(p); err == nil && pwd != "" {
				return pwd, fmt.Sprintf("saved hotel profile '%s'", p)
			}
		}
	}

	return "", ""
}
