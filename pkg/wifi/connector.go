package wifi

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GetInterfaceStatus returns current Wi-Fi adapter connection state and diagnostics.
func GetInterfaceStatus() (*InterfaceStatus, error) {
	cmd := SilentCommand("netsh", "wlan", "show", "interfaces")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	status := &InterfaceStatus{}
	scanner := bufio.NewScanner(bytes.NewReader(out))

	reField := regexp.MustCompile(`^\s*([^:]+)\s*:\s*(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		m := reField.FindStringSubmatch(line)
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
			if strings.Contains(val, "5") {
				status.Band = Band5GHz
			} else if strings.Contains(val, "6") {
				status.Band = Band6GHz
			} else {
				status.Band = Band24GHz
			}
		case "Channel":
			if ch, err := strconv.Atoi(val); err == nil {
				status.Channel = ch
			}
		case "Radio type":
			status.RadioType = val
		case "Receive rate (Mbps)":
			if r, err := strconv.Atoi(val); err == nil {
				status.RxMbps = r
			}
		case "Transmit rate (Mbps)":
			if t, err := strconv.Atoi(val); err == nil {
				status.TxMbps = t
			}
		case "Signal":
			clean := strings.TrimSuffix(val, "%")
			if s, err := strconv.Atoi(strings.TrimSpace(clean)); err == nil {
				status.SignalPercent = s
			}
		}
	}

	if status.Connected {
		status.GatewayIP = GetDefaultGateway()
		if status.GatewayIP != "" {
			status.GatewayLatencyMs, status.PacketLossPercent = PingGateway(status.GatewayIP)
		}
		status.CaptivePortal, status.CaptivePortalURL = CheckCaptivePortal()
	}


	return status, nil
}

// ConnectSSID initiates connection to an SSID, auto-provisioning Wi-Fi profiles from hotel passwords if needed.
func ConnectSSID(ssid string) error {
	newlyProvisioned := false

	// If profile does not exist, attempt hotel password guessing and profile synthesis
	if !HasProfile(ssid) {
		pwd, source := GuessPasswordForSSID(ssid)
		if pwd != "" {
			if err := AddWifiProfile(ssid, pwd); err != nil {
				return fmt.Errorf("failed to auto-provision profile with password from %s: %w", source, err)
			}
			newlyProvisioned = true
		} else {
			return fmt.Errorf("no known or inferable password for network '%s'", ssid)
		}
	}

	cmd := SilentCommand("netsh", "wlan", "connect", fmt.Sprintf("name=%s", ssid))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if newlyProvisioned {
			_ = DeleteWifiProfile(ssid)
		}
		return fmt.Errorf("connection failed: %w (output: %s)", err, string(out))
	}

	// Poll interface for up to 4 seconds to verify connection
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		status, err := GetInterfaceStatus()
		if err == nil && status != nil && status.Connected && strings.EqualFold(status.SSID, ssid) {
			return nil
		}
	}

	// If handshake failed to connect within timeout and it was newly provisioned, clean it up!
	if newlyProvisioned {
		_ = DeleteWifiProfile(ssid)
	}

	return fmt.Errorf("authentication or connection timed out for '%s'", ssid)
}

// Disconnect disconnects the active Wi-Fi connection.
func Disconnect() error {
	cmd := SilentCommand("netsh", "wlan", "disconnect")
	return cmd.Run()
}
