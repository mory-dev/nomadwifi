package main

import (
	"bufio"
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
	if len(os.Args) < 2 {
		runInteractiveMenu()
		return
	}

	tui.PrintBanner()

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
	case "logs", "log":
		runLogs()
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

func runInteractiveMenu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		tui.PrintBanner()
		status, err := wifi.GetInterfaceStatus()
		if err == nil && status != nil {
			tui.PrintStatus(status)
		}

		fmt.Println("Select an option:")
		fmt.Println("  [1] Refresh Status")
		fmt.Println("  [2] Scan All Nearby Wi-Fi Networks")
		fmt.Println("  [3] One-Click Auto-Optimize (Switch to fastest 5GHz AP)")
		fmt.Println("  [4] Start Background Auto-Roam Watcher")
		fmt.Println("  [5] View Logs")
		fmt.Println("  [6] Exit")
		fmt.Print("\nEnter choice (1-6): ")

		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		choice := strings.TrimSpace(input)

		switch choice {
		case "1":
			continue
		case "2":
			runScan()
			pausePrompt(reader)
		case "3":
			runOptimize()
			pausePrompt(reader)
		case "4":
			runDaemon()
			pausePrompt(reader)
		case "5":
			runLogs()
			pausePrompt(reader)
		case "6", "q", "exit":
			fmt.Println("Exiting NomadWiFi.")
			return
		default:
			fmt.Println("Invalid selection.")
			time.Sleep(1 * time.Second)
		}
	}
}

func pausePrompt(reader *bufio.Reader) {
	fmt.Print("\nPress Enter to return to menu...")
	_, _ = reader.ReadString('\n')
}

func runStatus() {
	status, err := wifi.GetInterfaceStatus()
	if err != nil {
		fmt.Printf("Error querying Wi-Fi interface: %v\n", err)
		return
	}
	tui.PrintStatus(status)

	if status.Connected {
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
	fmt.Printf("📁 Log file: %s\n", daemon.GetLogPath())
	fmt.Println("Press Ctrl+C to stop.")

	if err := daemon.StartMonitor(ctx, cfg); err != nil {
		fmt.Printf("Daemon error: %v\n", err)
	}
}

func runLogs() {
	logPath := daemon.GetLogPath()
	fmt.Printf("📁 Log file path: %s\n\n", logPath)

	data, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Println("No logs recorded yet. Run 'nomadwifi watch' to start logging.")
		return
	}

	lines := strings.Split(string(data), "\n")
	start := 0
	if len(lines) > 30 {
		start = len(lines) - 30
	}

	fmt.Println("--- Recent Log Entries ---")
	for _, l := range lines[start:] {
		if strings.TrimSpace(l) != "" {
			fmt.Println(l)
		}
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
	fmt.Println("  nomadwifi                   Open interactive terminal menu (safe for double-click)")
	fmt.Println("  nomadwifi status            Show active link stats, band, gateway latency & captive portal")
	fmt.Println("  nomadwifi scan              Scan all surrounding APs and list them ranked by quality score")
	fmt.Println("  nomadwifi optimize          Analyze and auto-switch to the best 5GHz/high-speed hotel AP")
	fmt.Println("  nomadwifi watch [--dry-run] Run background daemon to actively monitor and auto-roam")
	fmt.Println("  nomadwifi logs              Display recent activity logs and log file path")
	fmt.Println("  nomadwifi connect <SSID>    Connect directly to a specific Wi-Fi network")
}
