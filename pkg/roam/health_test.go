package roam

import (
	"strings"
	"testing"

	"github.com/dariomory/nomadwifi/pkg/wifi"
)

func healthyBaseline() Health {
	return Health{
		Connected: true, SSID: "Venue_5G", Band: wifi.Band5GHz,
		SignalPercent: 80, RxMbps: 300, LatencyMs: 12, JitterMs: 4,
		LossPct: 0, InternetOK: true,
	}
}

func TestHealthyConnectionIsNotDegraded(t *testing.T) {
	h := healthyBaseline()
	h.evaluate(DefaultThresholds())
	if h.Degraded {
		t.Errorf("a fast, low-latency 5 GHz link should not be degraded: %v", h.Reasons)
	}
}

func TestDegradationTriggers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Health)
		expect string
	}{
		{"packet loss", func(h *Health) { h.LossPct = 25 }, "packet loss"},
		{"latency", func(h *Health) { h.LatencyMs = 400 }, "latency"},
		{"jitter", func(h *Health) { h.JitterMs = 120 }, "jitter"},
		{"link speed", func(h *Health) { h.RxMbps = 12 }, "link speed"},
		{"weak signal", func(h *Health) { h.SignalPercent = 20 }, "signal"},
		{"wrong band", func(h *Health) { h.Band = wifi.Band24GHz }, "2.4 GHz"},
		{"no internet", func(h *Health) { h.InternetOK = false }, "no internet"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := healthyBaseline()
			tc.mutate(&h)
			h.evaluate(DefaultThresholds())

			if !h.Degraded {
				t.Fatalf("expected %s to mark the connection degraded", tc.name)
			}
			if !strings.Contains(strings.Join(h.Reasons, " "), tc.expect) {
				t.Errorf("reasons %v do not mention %q", h.Reasons, tc.expect)
			}
		})
	}
}

// Roaming away from a captive portal lands on the same portal inside one
// venue and throws away a part-finished login, so it must not count as
// degradation.
func TestCaptivePortalDoesNotTriggerRoaming(t *testing.T) {
	h := healthyBaseline()
	h.CaptivePortal = true
	h.InternetOK = false
	h.evaluate(DefaultThresholds())

	if h.Degraded {
		t.Errorf("a captive portal should not be treated as a degraded link: %v", h.Reasons)
	}
	if len(h.Reasons) == 0 || !strings.Contains(h.Reasons[0], "portal") {
		t.Errorf("expected the portal to be reported to the user, got %v", h.Reasons)
	}
}

func TestPrefer5GHzCanBeDisabled(t *testing.T) {
	h := healthyBaseline()
	h.Band = wifi.Band24GHz

	th := DefaultThresholds()
	th.Prefer5GHz = false
	h.evaluate(th)

	if h.Degraded {
		t.Errorf("with Prefer5GHz off, a healthy 2.4 GHz link is fine: %v", h.Reasons)
	}
}

func TestSummaryReportsDisconnected(t *testing.T) {
	var h Health
	if got := h.Summary(); got != "disconnected" {
		t.Errorf("Summary() = %q, want %q", got, "disconnected")
	}
}
