package wifi

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
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
		status.CaptivePortal = CheckCaptivePortal()
	}

	return status, nil
}

// ConnectSSID initiates connection to a saved or visible Wi-Fi profile.
func ConnectSSID(ssid string) error {
	cmd := SilentCommand("netsh", "wlan", "connect", fmt.Sprintf("name=%s", ssid))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("connection failed: %w (output: %s)", err, string(out))
	}
	// Give interface a second to handshake
	time.Sleep(2 * time.Second)
	return nil
}

// Disconnect disconnects the active Wi-Fi connection.
func Disconnect() error {
	cmd := SilentCommand("netsh", "wlan", "disconnect")
	return cmd.Run()
}

