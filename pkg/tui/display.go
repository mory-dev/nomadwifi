// Package tui renders NomadWiFi's terminal output.
package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/dariomory/nomadwifi/pkg/vpn"
	"github.com/dariomory/nomadwifi/pkg/wifi"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorGrey   = "\033[90m"
	ColorBold   = "\033[1m"
)

const banner = `  ███╗   ██╗ ██████╗ ███╗   ███╗ █████╗ ██████╗
  ████╗  ██║██╔═══██╗████╗ ████║██╔══██╗██╔══██╗
  ██╔██╗ ██║██║   ██║██╔████╔██║███████║██║  ██║
  ██║╚██╗██║██║   ██║██║╚██╔╝██║██╔══██║██║  ██║
  ██║ ╚████║╚██████╔╝██║ ╚═╝ ██║██║  ██║██████╔╝
  ╚═╝  ╚═══╝ ╚═════╝ ╚═╝     ╚═╝╚═╝  ╚═╝╚═════╝`

// PrintBanner prints the NomadWiFi header.
func PrintBanner() {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s%s%s%s WIFI%s", ColorCyan, banner, ColorReset, ColorGreen+ColorBold, ColorReset)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  Wi-Fi roaming and band optimizer for travel networks%s", ColorGrey, ColorReset)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout)
}

// PrintStatus displays the active connection and its measured health.
func PrintStatus(status *wifi.InterfaceStatus) {
	fmt.Printf("%sConnection%s\n", ColorBold, ColorReset)

	if status == nil || !status.Connected {
		fmt.Printf("  %sDisconnected%s  no active Wi-Fi association\n\n", ColorRed+ColorBold, ColorReset)
		return
	}

	bandColor := ColorCyan
	if status.Band == wifi.Band24GHz {
		bandColor = ColorYellow
	}

	field := func(label, value string) {
		fmt.Printf("  %s%-16s%s %s\n", ColorGrey, label, ColorReset, value)
	}

	field("Network", fmt.Sprintf("%s%s%s", ColorBold+ColorWhite, status.SSID, ColorReset))
	field("Access point", fmt.Sprintf("%s%s", status.BSSID, dim(fmt.Sprintf("  channel %d", status.Channel))))
	field("Band", fmt.Sprintf("%s%s%s%s", bandColor, status.Band, ColorReset, dim("  "+status.RadioType)))
	field("Signal", signalBar(status.SignalPercent, status.RSSI))
	field("Link speed", fmt.Sprintf("%d Mbps down / %d Mbps up", status.RxMbps, status.TxMbps))

	if status.GatewayIP != "" {
		field("Gateway", fmt.Sprintf("%s   %s   %s",
			status.GatewayIP,
			latencyText(status.GatewayLatencyMs),
			lossText(status.PacketLossPercent)))
	}

	if status.CaptivePortal {
		field("Internet", fmt.Sprintf("%sblocked by a login portal%s", ColorYellow+ColorBold, ColorReset))
		field("Sign in at", status.CaptivePortalURL)
	} else {
		field("Internet", fmt.Sprintf("%sreachable%s", ColorGreen, ColorReset))
	}
	fmt.Println()
}

// PrintVPN shows tunnel state and whether it is carrying traffic.
func PrintVPN(status vpn.Status) {
	if len(status.Tunnels) == 0 {
		return
	}

	fmt.Printf("%sVPN%s\n", ColorBold, ColorReset)
	for _, t := range status.Tunnels {
		state := dim("installed, not connected")
		if t.Up && t.OwnsDefaultRoute {
			state = fmt.Sprintf("%scarrying all traffic%s", ColorGreen+ColorBold, ColorReset)
		} else if t.Up {
			state = fmt.Sprintf("%sup, not the default route%s", ColorCyan, ColorReset)
		}

		control := dim("manual control only")
		if t.Controllable {
			control = fmt.Sprintf("%sNomadWiFi can pause it%s", ColorGreen, ColorReset)
		}
		fmt.Printf("  %s%s%s %s %s\n", ColorGrey, padVisible(t.Provider, 22), ColorReset, padVisible(state, 34), control)
	}
	fmt.Println()
}

// PrintScanTable prints ranked access points.
func PrintScanTable(aps []wifi.AccessPoint, currentSSID string) {
	if len(aps) == 0 {
		fmt.Printf("%sNo networks in range.%s\n\n", ColorYellow, ColorReset)
		return
	}

	fmt.Printf("%s  %-24s %-8s %-5s %-10s %-22s %-7s %s%s\n",
		ColorBold, "NETWORK", "BAND", "CH", "STANDARD", "SIGNAL", "SCORE", "STATUS", ColorReset)
	fmt.Printf("%s%s%s\n", ColorGrey, strings.Repeat("-", 98), ColorReset)

	for _, ap := range aps {
		marker := "  "
		name := truncate(ap.SSID, 24)
		if strings.EqualFold(ap.SSID, currentSSID) {
			marker = fmt.Sprintf("%s>%s ", ColorGreen+ColorBold, ColorReset)
			name = ColorBold + name + ColorReset
		}

		bandColor := ColorCyan
		if ap.Band == wifi.Band24GHz {
			bandColor = ColorYellow
		}

		radios := ""
		if ap.BSSIDCount > 1 {
			radios = dim(fmt.Sprintf(" x%d", ap.BSSIDCount))
		}

		fmt.Printf("%s%s %s %-5d %-10s %s %s%-7.1f%s %s%s\n",
			marker, padVisible(name, 24),
			padVisible(bandColor+string(ap.Band)+ColorReset, 8),
			ap.Channel,
			orDash(ap.RadioType),
			padVisible(signalBar(ap.SignalPercent, ap.RSSI), 22),
			scoreColor(ap.QualityScore), ap.QualityScore, ColorReset,
			authLabel(ap), radios)
	}
	fmt.Println()
	fmt.Printf("%s  ready = Windows can connect now   venue key = password inferred from a sibling network%s\n\n",
		ColorGrey, ColorReset)
}

func authLabel(ap wifi.AccessPoint) string {
	switch ap.AuthStatus {
	case wifi.AuthStatusSaved:
		return fmt.Sprintf("%sready%s", ColorGreen+ColorBold, ColorReset)
	case wifi.AuthStatusInferred:
		if ap.IsWarm {
			return fmt.Sprintf("%svenue key (warmed)%s", ColorPurple+ColorBold, ColorReset)
		}
		return fmt.Sprintf("%svenue key%s", ColorPurple, ColorReset)
	case wifi.AuthStatusOpen:
		return fmt.Sprintf("%sopen%s", ColorBlue, ColorReset)
	default:
		return fmt.Sprintf("%spassword needed%s", ColorGrey, ColorReset)
	}
}

// signalBar renders strength as a short meter plus the measured dBm.
func signalBar(percent, rssi int) string {
	const width = 5
	filled := percent * width / 100
	if filled > width {
		filled = width
	}

	color := ColorGreen
	switch {
	case percent < 30:
		color = ColorRed
	case percent < 55:
		color = ColorYellow
	}

	bar := color + strings.Repeat("#", filled) + ColorGrey + strings.Repeat(".", width-filled) + ColorReset
	if rssi != 0 {
		return fmt.Sprintf("%s %3d%% %s", bar, percent, dim(fmt.Sprintf("%d dBm", rssi)))
	}
	return fmt.Sprintf("%s %3d%%", bar, percent)
}

func latencyText(ms float64) string {
	color := ColorGreen
	switch {
	case ms > 250:
		color = ColorRed
	case ms > 100:
		color = ColorYellow
	}
	return fmt.Sprintf("%s%.0f ms%s", color, ms, ColorReset)
}

func lossText(pct float64) string {
	if pct <= 0 {
		return dim("no loss")
	}
	color := ColorYellow
	if pct > 10 {
		color = ColorRed
	}
	return fmt.Sprintf("%s%.0f%% loss%s", color, pct, ColorReset)
}

func scoreColor(score float64) string {
	switch {
	case score >= 80:
		return ColorGreen + ColorBold
	case score >= 50:
		return ColorCyan
	case score >= 0:
		return ColorYellow
	}
	return ColorGrey
}

func dim(s string) string { return ColorGrey + s + ColorReset }

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

// reANSI matches the SGR colour sequences this package emits. They occupy no
// columns on screen, so padding has to measure the text without them: padding a
// coloured cell with %-16s counts the escapes and the row loses its alignment.
var reANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visibleWidth(s string) int {
	return len([]rune(reANSI.ReplaceAllString(s, "")))
}

// padVisible left-aligns s in a field of the given width, ignoring colour codes.
func padVisible(s string, width int) string {
	if gap := width - visibleWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
