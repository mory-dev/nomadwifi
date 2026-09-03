package wifi

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GetDefaultGateway parses the active IPv4 default gateway.
func GetDefaultGateway() string {
	cmd := exec.Command("netsh", "interface", "ipv4", "show", "config", "name=Wi-Fi")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	reGW := regexp.MustCompile(`(?i)Default\s+Gateway\s*:\s*([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if m := reGW.FindStringSubmatch(line); len(m) > 1 {
			gw := strings.TrimSpace(m[1])
			if gw != "0.0.0.0" && gw != "" {
				return gw
			}
		}
	}
	return ""
}

// PingGateway pings the default gateway and returns average round-trip ms and packet loss percentage.
func PingGateway(gwIP string) (avgMs float64, packetLoss float64) {
	if gwIP == "" {
		return 0, 100.0
	}

	cmd := exec.Command("ping", "-n", "3", "-w", "1000", gwIP)
	out, err := cmd.Output()
	if err != nil {
		return 0, 100.0
	}

	reLoss := regexp.MustCompile(`\((\d+)%\s+loss\)`)
	reAvg := regexp.MustCompile(`Average\s*=\s*(\d+)ms`)

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if m := reLoss.FindStringSubmatch(line); len(m) > 1 {
			if l, err := strconv.ParseFloat(m[1], 64); err == nil {
				packetLoss = l
			}
		}
		if m := reAvg.FindStringSubmatch(line); len(m) > 1 {
			if a, err := strconv.ParseFloat(m[1], 64); err == nil {
				avgMs = a
			}
		}
	}

	return avgMs, packetLoss
}

// CheckCaptivePortal probes Google HTTP 204 endpoint.
// Returns true if captive portal is intercepting HTTP requests.
func CheckCaptivePortal() bool {
	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// If redirect occurs on 204 endpoint, it's definitely a captive portal!
			return http.ErrUseLastResponse
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://connectivitycheck.gstatic.com/generate_204", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Google 204 endpoint returns HTTP 204 No Content with 0 bytes when clean.
	// If it returns HTTP 200, 302, or 307 with HTML body, a captive portal has intercepted it.
	return resp.StatusCode != http.StatusNoContent
}
