package roam

import (
	"fmt"

	"github.com/dariomory/nomadwifi/pkg/wifi"
)

// Health is a measured view of the current connection.
//
// Signal strength alone does not describe a usable connection: a hotel AP can
// show four bars while its uplink is saturated. Latency, jitter, loss and
// actual internet reachability are all measured, and every probe is bound to
// the Wi-Fi adapter's own address so an active VPN tunnel cannot distort them.
type Health struct {
	Connected     bool      `json:"connected"`
	SSID          string    `json:"ssid,omitempty"`
	BSSID         string    `json:"bssid,omitempty"`
	Band          wifi.Band `json:"band,omitempty"`
	SignalPercent int       `json:"signal_percent"`
	RSSI          int       `json:"rssi,omitempty"`
	RxMbps        int       `json:"rx_mbps"`
	GatewayIP     string    `json:"gateway_ip,omitempty"`
	LatencyMs     float64   `json:"latency_ms"`
	JitterMs      float64   `json:"jitter_ms"`
	LossPct       float64   `json:"loss_pct"`
	InternetOK    bool      `json:"internet_ok"`
	CaptivePortal bool      `json:"captive_portal"`
	PortalURL     string    `json:"portal_url,omitempty"`
	Degraded      bool      `json:"degraded"`
	Reasons       []string  `json:"reasons,omitempty"`
}

// Thresholds define when a connection counts as degraded.
type Thresholds struct {
	MinRxMbps     int
	MinSignalPct  int
	MaxLatencyMs  float64
	MaxJitterMs   float64
	MaxLossPct    float64
	Prefer5GHz    bool
	CheckInternet bool
}

// DefaultThresholds are tuned for hotel and cafe networks, where the common
// failure is a distant 2.4 GHz radio rather than a completely dead link.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MinRxMbps:     50,
		MinSignalPct:  35,
		MaxLatencyMs:  150,
		MaxJitterMs:   60,
		MaxLossPct:    10,
		Prefer5GHz:    true,
		CheckInternet: true,
	}
}

// Assess measures the current connection against the thresholds.
func Assess(t Thresholds) Health {
	var h Health

	status, err := wifi.GetLinkStatus()
	if err != nil || status == nil || !status.Connected {
		h.Degraded = true
		h.Reasons = append(h.Reasons, "no active Wi-Fi connection")
		return h
	}

	h.Connected = true
	h.SSID = status.SSID
	h.BSSID = status.BSSID
	h.Band = status.Band
	h.SignalPercent = status.SignalPercent
	h.RSSI = status.RSSI
	h.RxMbps = status.RxMbps

	gateway, local := wifi.InterfaceAddresses(status.InterfaceName)
	h.GatewayIP = gateway

	if gateway != "" {
		probe := wifi.ProbeGatewayFrom(gateway, local, 4)
		h.LatencyMs = probe.AvgMs
		h.JitterMs = probe.JitterMs
		h.LossPct = probe.LossPct
	}

	if t.CheckInternet {
		h.CaptivePortal, h.PortalURL = wifi.CheckCaptivePortalFrom(local)
		h.InternetOK = !h.CaptivePortal && wifi.HasInternet(local)
	} else {
		h.InternetOK = true
	}

	h.evaluate(t)
	return h
}

func (h *Health) evaluate(t Thresholds) {
	degrade := func(format string, args ...interface{}) {
		h.Degraded = true
		h.Reasons = append(h.Reasons, fmt.Sprintf(format, args...))
	}

	// A captive portal is a state for the user to resolve, not something to
	// roam away from: switching APs inside the same venue lands on the same
	// portal and loses the session that was already half-established.
	if h.CaptivePortal {
		h.Reasons = append(h.Reasons, "captive portal login required")
		return
	}

	if t.CheckInternet && !h.InternetOK {
		degrade("no internet access through this access point")
	}
	if h.LossPct > t.MaxLossPct {
		degrade("packet loss %.0f%% above %.0f%%", h.LossPct, t.MaxLossPct)
	}
	if h.LatencyMs > t.MaxLatencyMs {
		degrade("gateway latency %.0fms above %.0fms", h.LatencyMs, t.MaxLatencyMs)
	}
	if h.JitterMs > t.MaxJitterMs {
		degrade("jitter %.0fms above %.0fms", h.JitterMs, t.MaxJitterMs)
	}
	if h.RxMbps > 0 && h.RxMbps < t.MinRxMbps {
		degrade("link speed %d Mbps below %d Mbps", h.RxMbps, t.MinRxMbps)
	}
	if h.SignalPercent > 0 && h.SignalPercent < t.MinSignalPct {
		degrade("signal %d%% below %d%%", h.SignalPercent, t.MinSignalPct)
	}
	if t.Prefer5GHz && h.Band == wifi.Band24GHz {
		degrade("connected on 2.4 GHz while faster bands may be available")
	}
}

// Summary renders the health as a single line for logs.
func (h Health) Summary() string {
	if !h.Connected {
		return "disconnected"
	}
	return fmt.Sprintf("%s (%s, %d%%, %d Mbps, %.0fms ±%.0fms, %.0f%% loss, internet=%v)",
		h.SSID, h.Band, h.SignalPercent, h.RxMbps, h.LatencyMs, h.JitterMs, h.LossPct, h.InternetOK)
}
