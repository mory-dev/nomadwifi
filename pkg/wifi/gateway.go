package wifi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	reGateway = regexp.MustCompile(`(?i)Default\s+Gateway\s*:\s*([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	reIPAddr  = regexp.MustCompile(`(?i)IP\s+Address\s*:\s*([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
)

// GetDefaultGateway returns the IPv4 default gateway for the named adapter.
// The adapter name must be passed in (from InterfaceStatus.InterfaceName) --
// it is not always "Wi-Fi" on localized or multi-adapter systems.
func GetDefaultGateway(interfaceName string) string {
	ip, _ := interfaceIPv4Config(interfaceName)
	return ip
}

// GetInterfaceIPv4 returns the adapter's own IPv4 address. Probes bound to this
// address measure the Wi-Fi path itself rather than whatever VPN tunnel
// currently owns the default route.
func GetInterfaceIPv4(interfaceName string) string {
	_, ip := interfaceIPv4Config(interfaceName)
	return ip
}

// LocalIPv4 returns the adapter's own IPv4 address using the standard
// library, which is far cheaper than shelling out to netsh.
func LocalIPv4(interfaceName string) string {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
				return v4.String()
			}
		}
	}
	return ""
}

type gatewayCacheEntry struct {
	gateway string
	local   string
	at      time.Time
}

var (
	gatewayCacheMu sync.Mutex
	gatewayCache   = map[string]gatewayCacheEntry{}
)

const gatewayCacheTTL = 20 * time.Second

// InvalidateNetworkCaches clears cached IP configuration. Call this whenever
// the association changes, since the gateway changes with it.
func InvalidateNetworkCaches() {
	gatewayCacheMu.Lock()
	gatewayCache = map[string]gatewayCacheEntry{}
	gatewayCacheMu.Unlock()
	scanCache.reset()
}

func interfaceIPv4Config(interfaceName string) (gateway string, local string) {
	if strings.TrimSpace(interfaceName) == "" {
		interfaceName = "Wi-Fi"
	}

	gatewayCacheMu.Lock()
	if e, ok := gatewayCache[interfaceName]; ok && time.Since(e.at) < gatewayCacheTTL {
		gatewayCacheMu.Unlock()
		return e.gateway, e.local
	}
	gatewayCacheMu.Unlock()

	local = LocalIPv4(interfaceName)

	cmd := SilentCommand("netsh", "interface", "ipv4", "show", "config",
		fmt.Sprintf("name=%s", interfaceName))
	out, err := cmd.Output()
	if err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			line := scanner.Text()
			if m := reGateway.FindStringSubmatch(line); len(m) > 1 {
				if gw := strings.TrimSpace(m[1]); gw != "0.0.0.0" {
					gateway = gw
				}
				continue
			}
			if local == "" {
				if m := reIPAddr.FindStringSubmatch(line); len(m) > 1 {
					if ip := strings.TrimSpace(m[1]); ip != "0.0.0.0" {
						local = ip
					}
				}
			}
		}
	}

	gatewayCacheMu.Lock()
	gatewayCache[interfaceName] = gatewayCacheEntry{gateway: gateway, local: local, at: time.Now()}
	gatewayCacheMu.Unlock()

	return gateway, local
}

// ProbeResult describes the measured health of a network path.
type ProbeResult struct {
	AvgMs      float64
	JitterMs   float64
	LossPct    float64
	Reachable  bool
	SampleSize int
}

// defaultProbePorts are ports a consumer gateway is likely to answer on.
var defaultProbePorts = []string{"80", "53", "443", "8080"}

// ProbeGateway measures round-trip time, jitter, and loss to the gateway.
//
// A responsive port is discovered once and then reused for every sample. That
// matters because routers commonly drop traffic to closed ports rather than
// refusing it, so rotating across ports would score a perfectly healthy
// gateway as heavily lossy.
//
// Either a completed handshake or an explicit refusal proves the host is
// reachable; only a timeout counts as a lost probe. This yields real loss
// figures without the raw socket privileges ICMP would require.
func ProbeGateway(gwIP string, samples int) ProbeResult {
	return probeHost(gwIP, samples, "")
}

// ProbeGatewayFrom is ProbeGateway bound to a specific local source address,
// so an active VPN tunnel cannot skew the measurement.
func ProbeGatewayFrom(gwIP, localIP string, samples int) ProbeResult {
	return probeHost(gwIP, samples, localIP)
}

type probePortCacheEntry struct {
	port string
	at   time.Time
}

var (
	probePortMu    sync.Mutex
	probePortCache = map[string]probePortCacheEntry{}
)

const probePortTTL = 2 * time.Minute

// discoverProbePort finds a port the host actually answers on.
func discoverProbePort(dialer *net.Dialer, host string) string {
	probePortMu.Lock()
	if e, ok := probePortCache[host]; ok && time.Since(e.at) < probePortTTL {
		probePortMu.Unlock()
		return e.port
	}
	probePortMu.Unlock()

	found := ""
	for _, port := range defaultProbePorts {
		conn, err := dialer.Dial("tcp", net.JoinHostPort(host, port))
		if err == nil {
			conn.Close()
			found = port
			break
		}
		if isRefused(err) {
			found = port
			break
		}
	}

	if found != "" {
		probePortMu.Lock()
		probePortCache[host] = probePortCacheEntry{port: found, at: time.Now()}
		probePortMu.Unlock()
	}
	return found
}

func probeHost(host string, samples int, localIP string) ProbeResult {
	res := ProbeResult{}
	if host == "" || samples <= 0 {
		res.LossPct = 100.0
		return res
	}

	dialer := &net.Dialer{Timeout: 800 * time.Millisecond}
	if localIP != "" {
		if ip := net.ParseIP(localIP); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	port := discoverProbePort(dialer, host)
	if port == "" {
		// The gateway answers on no TCP port at all, which is normal for a
		// hardened router. ICMP is the only remaining signal.
		return pingHost(host, samples)
	}

	target := net.JoinHostPort(host, port)
	var rtts []float64
	lost := 0

	for i := 0; i < samples; i++ {
		start := time.Now()
		conn, err := dialer.Dial("tcp", target)
		elapsed := float64(time.Since(start).Microseconds()) / 1000.0

		switch {
		case err == nil:
			conn.Close()
			rtts = append(rtts, elapsed)
		case isRefused(err):
			rtts = append(rtts, elapsed)
		default:
			lost++
		}

		if i < samples-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	return summarize(rtts, lost, samples)
}

func summarize(rtts []float64, lost, samples int) ProbeResult {
	res := ProbeResult{SampleSize: samples}
	res.LossPct = float64(lost) / float64(samples) * 100.0
	res.Reachable = len(rtts) > 0
	if !res.Reachable {
		return res
	}

	var sum float64
	for _, r := range rtts {
		sum += r
	}
	res.AvgMs = sum / float64(len(rtts))

	var dev float64
	for _, r := range rtts {
		d := r - res.AvgMs
		if d < 0 {
			d = -d
		}
		dev += d
	}
	res.JitterMs = dev / float64(len(rtts))
	return res
}

var (
	rePingLoss = regexp.MustCompile(`\((\d+)%\s+loss\)`)
	rePingAvg  = regexp.MustCompile(`(?i)Average\s*=\s*(\d+)ms`)
)

// pingHost falls back to ICMP for gateways that answer no TCP port.
func pingHost(host string, samples int) ProbeResult {
	res := ProbeResult{SampleSize: samples, LossPct: 100.0}

	out, err := SilentCommand("ping", "-n", strconv.Itoa(samples), "-w", "800", host).Output()
	if err != nil {
		return res
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if m := rePingLoss.FindStringSubmatch(line); len(m) > 1 {
			if l, err := strconv.ParseFloat(m[1], 64); err == nil {
				res.LossPct = l
			}
		}
		if m := rePingAvg.FindStringSubmatch(line); len(m) > 1 {
			if a, err := strconv.ParseFloat(m[1], 64); err == nil {
				res.AvgMs = a
			}
		}
	}
	res.Reachable = res.LossPct < 100.0
	return res
}

func isRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// PingGateway reports average latency and packet loss to the gateway.
func PingGateway(gwIP string) (avgMs float64, packetLoss float64) {
	r := ProbeGateway(gwIP, 4)
	return r.AvgMs, r.LossPct
}

const captivePortalProbeURL = "http://connectivitycheck.gstatic.com/generate_204"

// CheckCaptivePortal probes connectivity and reports whether a captive portal
// intercepted the request, along with the login URL to open.
func CheckCaptivePortal() (isIntercepted bool, portalURL string) {
	return CheckCaptivePortalFrom("")
}

// CheckCaptivePortalFrom runs the captive-portal probe from a specific local
// source address so the check follows the Wi-Fi path rather than a VPN tunnel.
func CheckCaptivePortalFrom(localIP string) (isIntercepted bool, portalURL string) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	if localIP != "" {
		if ip := net.ParseIP(localIP); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", captivePortalProbeURL, nil)
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

	redirect := resp.Header.Get("Location")
	if redirect == "" {
		redirect = "http://neverssl.com"
	}
	return true, redirect
}

// HasInternet reports whether the probe endpoint is reachable unmodified,
// which is the definition of a usable connection (portal-free, routed).
func HasInternet(localIP string) bool {
	intercepted, _ := CheckCaptivePortalFrom(localIP)
	if intercepted {
		return false
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	if localIP != "" {
		if ip := net.ParseIP(localIP); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	conn, err := dialer.Dial("tcp", "1.1.1.1:443")
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// InterfaceAddresses returns the adapter's default gateway and its own IPv4
// address in one lookup.
func InterfaceAddresses(interfaceName string) (gateway string, local string) {
	return interfaceIPv4Config(interfaceName)
}
