package wifi

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var reInterfaceField = regexp.MustCompile(`^\s*([^:]+?)\s*:\s*(.*)$`)

// GetLinkStatus returns adapter and association state only. It performs no
// network probes, so it is cheap enough to call on a tight loop; use
// GetInterfaceStatus when gateway latency and captive-portal state are needed.
func GetLinkStatus() (*InterfaceStatus, error) {
	if NativeAvailable() {
		if s, err := nativeLinkStatus(); err == nil {
			return s, nil
		}
	}

	cmd := SilentCommand("netsh", "wlan", "show", "interfaces")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseInterfaceStatus(out), nil
}

func parseInterfaceStatus(out []byte) *InterfaceStatus {
	status := &InterfaceStatus{}
	scanner := bufio.NewScanner(bytes.NewReader(out))

	for scanner.Scan() {
		m := reInterfaceField.FindStringSubmatch(scanner.Text())
		if len(m) <= 2 {
			continue
		}
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])

		switch key {
		case "Name":
			status.InterfaceName = val
		case "Description":
			status.Description = val
		case "State":
			status.Connected = strings.EqualFold(val, "connected")
		case "SSID":
			status.SSID = val
		case "AP BSSID", "BSSID":
			status.BSSID = val
		case "Band":
			status.Band = bandFromLabel(val)
		case "Channel":
			if ch, err := strconv.Atoi(val); err == nil {
				status.Channel = ch
			}
		case "Radio type":
			status.RadioType = val
		case "Receive rate (Mbps)":
			status.RxMbps = parseRateMbps(val)
		case "Transmit rate (Mbps)":
			status.TxMbps = parseRateMbps(val)
		case "Signal":
			if s, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(val, "%"))); err == nil {
				status.SignalPercent = s
			}
		case "Rssi":
			if r, err := strconv.Atoi(val); err == nil {
				status.RSSI = r
			}
		}
	}

	if status.RSSI == 0 && status.SignalPercent > 0 {
		status.RSSI = rssiFromSignalPercent(status.SignalPercent)
	}
	if status.Band == "" || status.Band == BandOther {
		status.Band = bandFromChannel(status.Channel)
	}
	return status
}

// parseRateMbps handles rates reported as "390" and as "390.5".
func parseRateMbps(val string) int {
	f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil {
		return 0
	}
	return int(f)
}

var (
	ssidCacheMu    sync.Mutex
	ssidCacheVal   string
	ssidCacheStamp time.Time
)

const ssidCacheTTL = 3 * time.Second

// CurrentSSID returns the SSID we are associated with, cached briefly so that
// classifying a full scan does not re-query the adapter once per access point.
func CurrentSSID() string {
	ssidCacheMu.Lock()
	if time.Since(ssidCacheStamp) < ssidCacheTTL {
		v := ssidCacheVal
		ssidCacheMu.Unlock()
		return v
	}
	ssidCacheMu.Unlock()

	ssid := ""
	if s, err := GetLinkStatus(); err == nil && s != nil && s.Connected {
		ssid = s.SSID
	}

	ssidCacheMu.Lock()
	ssidCacheVal = ssid
	ssidCacheStamp = time.Now()
	ssidCacheMu.Unlock()
	return ssid
}

// GetInterfaceStatus returns connection state plus measured link health.
func GetInterfaceStatus() (*InterfaceStatus, error) {
	status, err := GetLinkStatus()
	if err != nil {
		return nil, err
	}

	if status.Connected {
		gw, local := interfaceIPv4Config(status.InterfaceName)
		status.GatewayIP = gw
		if gw != "" {
			r := ProbeGatewayFrom(gw, local, 3)
			status.GatewayLatencyMs = r.AvgMs
			status.PacketLossPercent = r.LossPct
		}
		status.CaptivePortal, status.CaptivePortalURL = CheckCaptivePortalFrom(local)
	}

	return status, nil
}

// findAP locates a scanned access point by SSID, best-scoring BSSID first.
func findAP(ssid string) *AccessPoint {
	aps, err := ScanNetworks()
	if err != nil {
		return nil
	}
	for i := range aps {
		if strings.EqualFold(aps[i].SSID, ssid) {
			return &aps[i]
		}
	}
	return nil
}

// ConnectSSID associates with an SSID, provisioning a profile first if needed.
//
// Open networks need a profile too, and it must be an open-security profile:
// synthesizing a WPA2 template for them (or refusing them for having no
// password) is why open hotel networks could not be joined at all.
func ConnectSSID(ssid string) error {
	if HasProfile(ssid) {
		return associate(ssid, false)
	}

	ap := findAP(ssid)
	if ap == nil {
		return fmt.Errorf("network '%s' is not in range", ssid)
	}

	security := SecurityForNetwork(ap.Authentication, ap.Cipher)
	if !security.SupportsAutoProfile() {
		return fmt.Errorf("'%s' is an enterprise network and needs manual setup in Windows", ssid)
	}

	password := ""
	if security.NeedsPassword() {
		pwd, source := GuessPasswordForSSID(ssid)
		if pwd == "" {
			return fmt.Errorf("no known or inferable password for network '%s'", ssid)
		}
		password = pwd
		_ = source
	}

	if err := AddWifiProfileFor(ssid, password, security, ap.IsHidden()); err != nil {
		return fmt.Errorf("failed to provision profile for '%s': %w", ssid, err)
	}

	return associate(ssid, true)
}

// ConnectSSIDWithPassword registers a user-supplied key and connects. A profile
// created here is removed again if the handshake fails, so a wrong password
// never leaves a broken profile behind.
func ConnectSSIDWithPassword(ssid, password string) error {
	security := SecurityWPA2PSK
	hidden := false
	if ap := findAP(ssid); ap != nil {
		security = SecurityForNetwork(ap.Authentication, ap.Cipher)
		hidden = ap.IsHidden()
	}
	if !security.NeedsPassword() {
		security = SecurityWPA2PSK
	}

	if err := AddWifiProfileFor(ssid, password, security, hidden); err != nil {
		return fmt.Errorf("failed to configure network profile: %w", err)
	}
	return associate(ssid, true)
}

// associate issues the connect and waits for the adapter to report the SSID.
// rollback removes a profile this call created when the handshake never completes.
func associate(ssid string, rollback bool) error {
	cmd := SilentCommand("netsh", "wlan", "connect", fmt.Sprintf("name=%s", ssid))
	if out, err := cmd.CombinedOutput(); err != nil {
		if rollback {
			_ = DeleteWifiProfile(ssid)
		}
		NoteAssociationFailure(ssid, "connect command rejected")
		return fmt.Errorf("connection failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	if WaitForAssociation(ssid, 12*time.Second) {
		NoteAssociationSuccess(ssid)
		InvalidateNetworkCaches()
		return nil
	}

	if rollback {
		_ = DeleteWifiProfile(ssid)
	}
	NoteAssociationFailure(ssid, "association timed out")
	return fmt.Errorf("authentication or connection timed out for '%s'", ssid)
}

// WaitForAssociation blocks until the adapter reports the given SSID, or the
// deadline passes.
func WaitForAssociation(ssid string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		status, err := GetLinkStatus()
		if err == nil && status != nil && status.Connected && strings.EqualFold(status.SSID, ssid) {
			invalidateSSIDCache()
			return true
		}
	}
	return false
}

func invalidateSSIDCache() {
	ssidCacheMu.Lock()
	ssidCacheStamp = time.Time{}
	ssidCacheMu.Unlock()
}

// Disconnect drops the active Wi-Fi association.
func Disconnect() error {
	cmd := SilentCommand("netsh", "wlan", "disconnect")
	err := cmd.Run()
	invalidateSSIDCache()
	return err
}
