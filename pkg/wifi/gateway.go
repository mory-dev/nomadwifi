package wifi

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GetDefaultGateway parses the active IPv4 default gateway.
func GetDefaultGateway() string {
	cmd := SilentCommand("netsh", "interface", "ipv4", "show", "config", "name=Wi-Fi")
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

// PingGateway measures gateway round-trip time in milliseconds.
func PingGateway(gwIP string) (avgMs float64, packetLoss float64) {
	if gwIP == "" {
		return 0, 100.0
	}

	// Try ultra-fast native TCP check to gateway DNS (port 53) or HTTP (port 80)
	ports := []string{"53", "80", "443"}
	for _, port := range ports {
		target := net.JoinHostPort(gwIP, port)
		start := time.Now()
		conn, err := net.DialTimeout("tcp", target, 600*time.Millisecond)
		if err == nil {
			conn.Close()
			rtt := float64(time.Since(start).Microseconds()) / 1000.0
			return rtt, 0.0
		}
	}

	// Fallback to silent ping
	cmd := SilentCommand("ping", "-n", "2", "-w", "500", gwIP)
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

// CheckCaptivePortal probes connectivity and returns whether a captive portal was intercepted, along with the login URL.
func CheckCaptivePortal() (isIntercepted bool, portalURL string) {
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://connectivitycheck.gstatic.com/generate_204", nil)
	if err != nil {
		return false, ""
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return false, ""
	}

	// Intercepted! Extract redirect location or fallback to neverssl
	redirectLoc := resp.Header.Get("Location")
	if redirectLoc == "" {
		redirectLoc = "http://neverssl.com"
	}
	return true, redirectLoc
}
