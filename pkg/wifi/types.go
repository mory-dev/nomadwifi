package wifi

import "fmt"

// Band represents Wi-Fi frequency band.
type Band string

const (
	Band24GHz Band = "2.4 GHz"
	Band5GHz  Band = "5 GHz"
	Band6GHz  Band = "6 GHz"
	BandOther Band = "Unknown"
)

// AccessPoint describes a discovered wireless access point / BSSID.
type AccessPoint struct {
	SSID           string   `json:"ssid"`
	BSSID          string   `json:"bssid"`
	SignalPercent  int      `json:"signal_percent"`
	RSSI           int      `json:"rssi"`
	Band           Band     `json:"band"`
	Channel        int      `json:"channel"`
	RadioType      string   `json:"radio_type"` // 802.11ax, 802.11ac, 802.11n, etc.
	Authentication string   `json:"authentication"`
	Cipher         string   `json:"cipher"`
	QualityScore   float64  `json:"quality_score"`
	Reasons        []string `json:"reasons,omitempty"`
}

// InterfaceStatus holds active network adapter details and link metrics.
type InterfaceStatus struct {
	InterfaceName     string  `json:"interface_name"`
	Description       string  `json:"description"`
	Connected         bool    `json:"connected"`
	SSID              string  `json:"ssid"`
	BSSID             string  `json:"bssid"`
	Band              Band    `json:"band"`
	Channel           int     `json:"channel"`
	RadioType         string  `json:"radio_type"`
	RxMbps            int     `json:"rx_mbps"`
	TxMbps            int     `json:"tx_mbps"`
	SignalPercent     int     `json:"signal_percent"`
	GatewayIP         string  `json:"gateway_ip,omitempty"`
	GatewayLatencyMs  float64 `json:"gateway_latency_ms,omitempty"`
	PacketLossPercent float64 `json:"packet_loss_percent,omitempty"`
	CaptivePortal     bool    `json:"captive_portal"`
}

func (ap AccessPoint) String() string {
	return fmt.Sprintf("%-20s | %-6s | Ch %-3d | %-8s | %3d%% (Score: %.1f)",
		ap.SSID, ap.Band, ap.Channel, ap.RadioType, ap.SignalPercent, ap.QualityScore)
}
