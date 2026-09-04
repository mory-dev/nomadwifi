package wifi

import "fmt"

// HiddenSSID is the placeholder used for access points that broadcast no SSID.
const HiddenSSID = "[Hidden SSID]"

// Band represents Wi-Fi frequency band.
type Band string

const (
	Band24GHz Band = "2.4 GHz"
	Band5GHz  Band = "5 GHz"
	Band6GHz  Band = "6 GHz"
	BandOther Band = "Unknown"
)

// IsFast reports whether the band is one of the uncongested high-throughput bands.
func (b Band) IsFast() bool {
	return b == Band5GHz || b == Band6GHz
}

// AuthStatus defines the connection readiness and security state of an access point.
type AuthStatus string

const (
	AuthStatusSaved    AuthStatus = "SAVED"    // Existing saved profile with verified password
	AuthStatusInferred AuthStatus = "INFERRED" // Hotel/venue cluster match with inferred shared password
	AuthStatusOpen     AuthStatus = "OPEN"     // Unencrypted open network
	AuthStatusLocked   AuthStatus = "LOCKED"   // Encrypted network with no known or inferred password
)

// Connectable reports whether NomadWiFi can associate without asking the user for a key.
func (a AuthStatus) Connectable() bool {
	return a == AuthStatusSaved || a == AuthStatusInferred || a == AuthStatusOpen
}

// AccessPoint describes a discovered wireless access point / BSSID.
type AccessPoint struct {
	SSID           string     `json:"ssid"`
	BSSID          string     `json:"bssid"`
	SignalPercent  int        `json:"signal_percent"`
	RSSI           int        `json:"rssi"`
	Band           Band       `json:"band"`
	Channel        int        `json:"channel"`
	FrequencyKHz   uint32     `json:"frequency_khz,omitempty"`
	RadioType      string     `json:"radio_type"` // 802.11ax, 802.11ac, 802.11n, etc.
	Authentication string     `json:"authentication"`
	Cipher         string     `json:"cipher"`
	QualityScore   float64    `json:"quality_score"`
	AuthStatus     AuthStatus `json:"auth_status"`
	InferredFrom   string     `json:"inferred_from,omitempty"`
	IsWarm         bool       `json:"is_warm"`
	Reasons        []string   `json:"reasons,omitempty"`

	// ChannelUtilization is the AP-advertised airtime busy fraction (0-100),
	// taken from the 802.11 BSS Load element. Only meaningful when HasChannelUtil.
	ChannelUtilization int  `json:"channel_utilization,omitempty"`
	HasChannelUtil     bool `json:"has_channel_util,omitempty"`
	StationCount       int  `json:"station_count,omitempty"`

	// BSSIDCount is how many radios broadcast this SSID in range. Set when a
	// per-BSSID list is collapsed to one row per network.
	BSSIDCount int `json:"bssid_count,omitempty"`
}

// IsHidden reports whether this AP broadcasts no SSID and so cannot be targeted by name.
func (ap AccessPoint) IsHidden() bool {
	return ap.SSID == HiddenSSID || ap.SSID == ""
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
	RSSI              int     `json:"rssi,omitempty"`
	GatewayIP         string  `json:"gateway_ip,omitempty"`
	GatewayLatencyMs  float64 `json:"gateway_latency_ms,omitempty"`
	PacketLossPercent float64 `json:"packet_loss_percent,omitempty"`
	CaptivePortal     bool    `json:"captive_portal"`
	CaptivePortalURL  string  `json:"captive_portal_url,omitempty"`
}

func (ap AccessPoint) String() string {
	return fmt.Sprintf("%-20s | %-6s | Ch %-3d | %-8s | %3d%% (Score: %.1f, Status: %s, Warm: %v)",
		ap.SSID, ap.Band, ap.Channel, ap.RadioType, ap.SignalPercent, ap.QualityScore, ap.AuthStatus, ap.IsWarm)
}
