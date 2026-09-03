package tui

import (
	"fmt"
	"os"
	"strings"

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
	ColorBold   = "\033[1m"
)

// PrintBanner prints the NomadWiFi ASCII art header.
func PrintBanner() {
	banner := `
  _   _                       _ _       ___ _____ _ 
 | \ | | ___  _ __ ___   __ _| | |__   / _ \___  (_)
 |  \| |/ _ \| '_ ' _ \ / _' | | '_ \ / /_\ \ / /| |
 | |\  | (_) | | | | | | (_| | | |_) / /_\\ V /  | |
 |_| \_|\___/|_| |_| |_|\__,_|_|_.__/_/   \_/_/   |_|
    Automated Hotel & Travel Wi-Fi Roaming Optimizer
`
	fmt.Fprintf(os.Stdout, "%s%s%s\n", ColorCyan, banner, ColorReset)
}

// PrintStatus displays detailed diagnostic status of the active connection.
func PrintStatus(status *wifi.InterfaceStatus) {
	fmt.Printf("%s=== Active Wi-Fi Connection ===%s\n", ColorBold, ColorReset)
	if !status.Connected {
		fmt.Printf("Status: %sDisconnected%s\n", ColorRed, ColorReset)
		return
	}

	stateColor := ColorGreen
	bandColor := ColorCyan
	if status.Band == wifi.Band24GHz {
		bandColor = ColorYellow
	}

	fmt.Printf("SSID:            %s%s%s\n", ColorBold+ColorWhite, status.SSID, ColorReset)
	fmt.Printf("BSSID:           %s\n", status.BSSID)
	fmt.Printf("Band:            %s%s%s\n", bandColor, status.Band, ColorReset)
	fmt.Printf("Channel:         %d\n", status.Channel)
	fmt.Printf("Radio:           %s\n", status.RadioType)
	fmt.Printf("Signal:          %d%%\n", status.SignalPercent)
	fmt.Printf("Link Speed:      Rx %d Mbps / Tx %d Mbps\n", status.RxMbps, status.TxMbps)

	if status.GatewayIP != "" {
		latColor := ColorGreen
		if status.GatewayLatencyMs > 100 {
			latColor = ColorYellow
		}
		if status.GatewayLatencyMs > 250 {
			latColor = ColorRed
		}
		fmt.Printf("Gateway:         %s (%s%.1f ms%s, %.0f%% loss)\n",
			status.GatewayIP, latColor, status.GatewayLatencyMs, ColorReset, status.PacketLossPercent)
	}

	if status.CaptivePortal {
		fmt.Printf("Captive Portal:  %sACTION REQUIRED (Login intercepted)%s\n", ColorRed+ColorBold, ColorReset)
	} else {
		fmt.Printf("Internet Access: %sDirect / Online%s\n", stateColor, ColorReset)
	}
	fmt.Println()
}

// PrintScanTable outputs the ranked list of discovered access points.
func PrintScanTable(aps []wifi.AccessPoint, currentBSSID string) {
	fmt.Printf("%s%-22s | %-7s | %-4s | %-8s | %-7s | %-7s | %s%s\n",
		ColorBold, "SSID", "Band", "Ch", "Radio", "Signal", "Score", "Rating", ColorReset)
	fmt.Println(strings.Repeat("-", 80))

	for _, ap := range aps {
		marker := "  "
		if ap.BSSID == currentBSSID && currentBSSID != "" {
			marker = "-> "
		}

		bandCol := ColorCyan
		if ap.Band == wifi.Band24GHz {
			bandCol = ColorYellow
		}

		rating := "Good"
		ratingCol := ColorGreen
		if ap.QualityScore >= 80 {
			rating = "Optimal"
			ratingCol = ColorBold + ColorGreen
		} else if ap.QualityScore < 50 {
			rating = "Slow/Crowded"
			ratingCol = ColorYellow
		} else if ap.QualityScore < 30 {
			rating = "Poor"
			ratingCol = ColorRed
		}

		ssidDisplay := ap.SSID
		if len(ssidDisplay) > 20 {
			ssidDisplay = ssidDisplay[:17] + "..."
		}

		fmt.Printf("%s%-20s | %s%-7s%s | %-4d | %-8s | %3d%%    | %-7.1f | %s%s%s\n",
			marker, ssidDisplay, bandCol, ap.Band, ColorReset,
			ap.Channel, ap.RadioType, ap.SignalPercent, ap.QualityScore,
			ratingCol, rating, ColorReset)
	}
	fmt.Println()
}
