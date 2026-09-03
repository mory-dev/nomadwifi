package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dariomory/nomadwifi/pkg/cluster"
	"github.com/dariomory/nomadwifi/pkg/daemon"
	"github.com/dariomory/nomadwifi/pkg/tui"
	"github.com/dariomory/nomadwifi/pkg/wifi"
)

func main() {
	tui.PrintBanner()

	if len(os.Args) < 2 {
		runStatus()
		return
	}

	command := strings.ToLower(os.Args[1])
	switch command {
	case "status":
		runStatus()
	case "scan":
		runScan()
	case "optimize", "opt":
		runOptimize()
	case "watch", "daemon":
		runDaemon()
	case "connect":
		if len(os.Args) < 3 {
			fmt.Println("Usage: nomadwifi connect <SSID>")
			os.Exit(1)
		}
		runConnect(os.Args[2])
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Printf("Unknown command '%s'. Run 'nomadwifi help' for available commands.\n", command)
		os.Exit(1)
	}
}

func runStatus() {
	status, err := wifi.GetInterfaceStatus()
	if err != nil {
		fmt.Printf("Error querying Wi-Fi interface: %v\n", err)
		return
	}
	tui.PrintStatus(status)

	if status.Connected {
		// Look for quick optimization hint
		aps, err := wifi.ScanNetworks()
		if err == nil && len(aps) > 0 {
			betterAP, reason := cluster.FindAlternativeInCluster(status.SSID, aps, 10.0)
			if betterAP != nil {
				fmt.Printf("%s💡 Optimization Available:%s Found %s (%s, Score: %.1f)\n",
					tui.ColorBold+tui.ColorYellow, tui.ColorReset, betterAP.SSID, betterAP.Band, betterAP.QualityScore)
				fmt.Printf("   Reason: %s\n", reason)
				fmt.Printf("   Run %snomadwifi optimize%s to switch automatically.\n\n", tui.ColorBold+tui.ColorGreen, tui.ColorReset)
			}
		}
	}
}

func runScan() {
	status, _ := wifi.GetInterfaceStatus()
	currentBSSID := ""
	if status != nil {
		currentBSSID = status.BSSID
	}

	fmt.Println("🔍 Scanning surrounding Wi-Fi networks and scoring quality...")
	aps, err := wifi.ScanNetworks()
	if err != nil {
		fmt.Printf("Scan failed: %v\n", err)
		return
	}

	tui.PrintScanTable(aps, currentBSSID)
}

func runOptimize() {
	status, err := wifi.GetInterfaceStatus()
	if err != nil || !status.Connected {
		fmt.Println("❌ No active Wi-Fi connection to optimize.")
		return
	}

	fmt.Printf("🔍 Analyzing current connection '%s' (%s, %d Mbps)...\n", status.SSID, status.Band, status.RxMbps)
	aps, err := wifi.ScanNetworks()
	if err != nil || len(aps) == 0 {
		fmt.Println("❌ Failed to scan nearby networks.")
		return
	}

	betterAP, reason := cluster.FindAlternativeInCluster(status.SSID, aps, 5.0)
	if betterAP == nil {
		// If no cluster alternative, check global top-scored AP
		if len(aps) > 0 && aps[0].QualityScore > 75 && !strings.EqualFold(aps[0].SSID, status.SSID) {
			betterAP = &aps[0]
			reason = "Top-scoring access point in range"
		}
	}

	if betterAP == nil || strings.EqualFold(betterAP.SSID, status.SSID) {
		fmt.Printf("%s✅ You are already on the optimal access point in this venue!%s\n", tui.ColorGreen+tui.ColorBold, tui.ColorReset)
		return
	}

	fmt.Printf("%s🚀 Found better network: %s (%s, Score: %.1f)%s\n",
		tui.ColorGreen+tui.ColorBold, betterAP.SSID, betterAP.Band, betterAP.QualityScore, tui.ColorReset)
	fmt.Printf("   Reason: %s\n", reason)
	fmt.Printf("   Connecting to %s...\n", betterAP.SSID)

	if err := wifi.ConnectSSID(betterAP.SSID); err != nil {
		fmt.Printf("❌ Failed to switch: %v\n", err)
		return
	}

	fmt.Printf("%s🎉 Successfully switched to %s!%s\n\n", tui.ColorGreen+tui.ColorBold, betterAP.SSID, tui.ColorReset)
	newStatus, _ := wifi.GetInterfaceStatus()
	if newStatus != nil {
		tui.PrintStatus(newStatus)
	}
}

func runDaemon() {
	watchFlags := flag.NewFlagSet("watch", flag.ExitOnError)
	intervalSec := watchFlags.Int("interval", 20, "Seconds between connection health checks")
	noRoam := watchFlags.Bool("dry-run", false, "Log recommendations without automatically roaming")
	watchFlags.Parse(os.Args[2:])

	cfg := daemon.DefaultConfig()
	cfg.PollInterval = time.Duration(*intervalSec) * time.Second
	cfg.AutoRoam = !*noRoam

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("🛡️ Starting NomadWiFi Background Roaming Monitor (Interval: %ds, AutoRoam: %v)\n",
		*intervalSec, cfg.AutoRoam)
	fmt.Println("Press Ctrl+C to stop.")

	if err := daemon.StartMonitor(ctx, cfg); err != nil {
		fmt.Printf("Daemon error: %v\n", err)
	}
}

func runConnect(ssid string) {
	fmt.Printf("Connecting to '%s'...\n", ssid)
	if err := wifi.ConnectSSID(ssid); err != nil {
		fmt.Printf("❌ Connection failed: %v\n", err)
		return
	}
	fmt.Printf("✅ Connected to '%s'.\n", ssid)
}

func printHelp() {
	fmt.Println("Available Commands:")
	fmt.Println("  nomadwifi status            Show active link stats, band, gateway latency & captive portal")
	fmt.Println("  nomadwifi scan              Scan all surrounding APs and list them ranked by quality score")
	fmt.Println("  nomadwifi optimize          Analyze and auto-switch to the best 5GHz/high-speed hotel AP")
	fmt.Println("  nomadwifi watch [--dry-run] Run background daemon to actively monitor and auto-roam")
	fmt.Println("  nomadwifi connect <SSID>    Connect directly to a specific Wi-Fi network")
}
