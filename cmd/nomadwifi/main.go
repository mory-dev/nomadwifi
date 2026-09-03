package main

import (
	"context"
	"encoding/json"
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

func hasFlag(args []string, target string) bool {
	for _, a := range args {
		if strings.EqualFold(a, target) {
			return true
		}
	}
	return false
}

func main() {
	if len(os.Args) < 2 {
		runInteractiveMenu()
		return
	}

	isJSON := hasFlag(os.Args, "--json") || hasFlag(os.Args, "-j")
	command := strings.ToLower(os.Args[1])

	switch command {
	case "status":
		runStatus(!isJSON, isJSON)
	case "scan":
		runScan(isJSON)
	case "optimize", "opt":
		runOptimize(isJSON)
	case "watch", "daemon":
		runDaemon(isJSON)
	case "logs", "log":
		runLogs(isJSON)
	case "connect":
		if len(os.Args) < 3 {
			if isJSON {
				fmt.Println(`{"error":"Missing SSID parameter"}`)
			} else {
				fmt.Println("Usage: nomadwifi connect <SSID> [--password <password>]")
			}
			os.Exit(1)
		}
		ssid := os.Args[2]
		var customPwd string
		for i := 3; i < len(os.Args)-1; i++ {
			if os.Args[i] == "--password" || os.Args[i] == "-p" {
				customPwd = os.Args[i+1]
				break
			}
		}
		runConnectWithPassword(ssid, customPwd, isJSON)
	case "help", "-h", "--help":
		printHelp()
	default:
		if isJSON {
			fmt.Printf(`{"error":"Unknown command %q"}`+"\n", command)
		} else {
			fmt.Printf("Unknown command '%s'. Run 'nomadwifi help' for available commands.\n", command)
		}
		os.Exit(1)
	}
}

func runInteractiveMenu() {
	for {
		tui.ClearScreen()
		tui.PrintBanner()

		status, err := wifi.GetInterfaceStatus()
		if err == nil && status != nil {
			tui.PrintStatus(status)
		}

		fmt.Println("Select an option (Press key):")
		fmt.Printf("  %s[1]%s Refresh Status                 %s[R]%s\n", tui.ColorBold+tui.ColorCyan, tui.ColorReset, tui.ColorBold+tui.ColorCyan, tui.ColorReset)
		fmt.Printf("  %s[2]%s Scan All Nearby Networks       %s[S]%s\n", tui.ColorBold+tui.ColorCyan, tui.ColorReset, tui.ColorBold+tui.ColorCyan, tui.ColorReset)
		fmt.Printf("  %s[3]%s One-Click Auto-Optimize (5GHz) %s[O]%s\n", tui.ColorBold+tui.ColorGreen, tui.ColorReset, tui.ColorBold+tui.ColorGreen, tui.ColorReset)
		fmt.Printf("  %s[4]%s Auto-Roam Watcher Daemon       %s[W]%s\n", tui.ColorBold+tui.ColorCyan, tui.ColorReset, tui.ColorBold+tui.ColorCyan, tui.ColorReset)
		fmt.Printf("  %s[5]%s View Roam Logs                 %s[L]%s\n", tui.ColorBold+tui.ColorCyan, tui.ColorReset, tui.ColorBold+tui.ColorCyan, tui.ColorReset)
		fmt.Printf("  %s[6]%s Exit                           %s[Q]%s\n", tui.ColorBold+tui.ColorRed, tui.ColorReset, tui.ColorBold+tui.ColorRed, tui.ColorReset)
		fmt.Print("\nChoice > ")

		key := tui.ReadKey()
		choice := strings.ToLower(string(key))

		switch choice {
		case "1", "r":
			continue
		case "2", "s":
			tui.ClearScreen()
			tui.PrintBanner()
			runScan(false)
			pauseKey()
		case "3", "o":
			tui.ClearScreen()
			tui.PrintBanner()
			runOptimize(false)
			pauseKey()
		case "4", "w":
			tui.ClearScreen()
			tui.PrintBanner()
			runDaemon(false)
		case "5", "l":
			tui.ClearScreen()
			tui.PrintBanner()
			runLogs(false)
			pauseKey()
		case "6", "q":
			fmt.Println("\nExiting NomadWiFi.")
			return
		}
	}
}

func pauseKey() {
	fmt.Println("Press any key to return to menu...")
	tui.ReadKey()
}

func runStatus(printBanner bool, asJSON bool) {
	status, err := wifi.GetInterfaceStatus()
	if err != nil {
		if asJSON {
			fmt.Println(`{"connected":false,"error":"Failed to query Wi-Fi adapter"}`)
		} else {
			fmt.Printf("Error: %v\n", err)
		}
		return
	}

	if asJSON {
		data, _ := json.Marshal(status)
		fmt.Println(string(data))
		return
	}

	if printBanner {
		tui.PrintBanner()
	}
	tui.PrintStatus(status)
}

func runScan(asJSON bool) {
	status, _ := wifi.GetInterfaceStatus()
	currentBSSID := ""
	if status != nil && status.Connected {
		currentBSSID = status.BSSID
	}

	if !asJSON {
		fmt.Println("🔍 Scanning surrounding Wi-Fi networks and scoring quality...")
	}
	aps, err := wifi.ScanNetworks()
	if err != nil {
		if asJSON {
			fmt.Println(`{"error":"Failed to scan Wi-Fi networks"}`)
		} else {
			fmt.Printf("Scan error: %v\n", err)
		}
		return
	}

	if asJSON {
		data, _ := json.Marshal(aps)
		fmt.Println(string(data))
		return
	}

	tui.PrintScanTable(aps, currentBSSID)
}

type OptResult struct {
	Success     bool    `json:"success"`
	Switched    bool    `json:"switched"`
	CurrentSSID string  `json:"current_ssid"`
	TargetSSID  string  `json:"target_ssid,omitempty"`
	Band        string  `json:"band,omitempty"`
	Score       float64 `json:"score,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Error       string  `json:"error,omitempty"`
}

func runOptimize(asJSON bool) {
	status, err := wifi.GetInterfaceStatus()
	if err != nil || !status.Connected {
		if asJSON {
			fmt.Println(`{"success":false,"error":"No active Wi-Fi connection"}`)
		} else {
			fmt.Println("❌ No active Wi-Fi connection to optimize.")
		}
		return
	}

	aps, err := wifi.ScanNetworks()
	if err != nil || len(aps) == 0 {
		if asJSON {
			fmt.Println(`{"success":false,"error":"No nearby networks found"}`)
		} else {
			fmt.Println("❌ Failed to scan nearby networks.")
		}
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
		if asJSON {
			res := OptResult{
				Success:     true,
				Switched:    false,
				CurrentSSID: status.SSID,
				Reason:      "Already on optimal access point",
			}
			data, _ := json.Marshal(res)
			fmt.Println(string(data))
		} else {
			fmt.Printf("%s✅ You are already on the optimal access point in this venue!%s\n", tui.ColorGreen+tui.ColorBold, tui.ColorReset)
		}
		return
	}

	if err := wifi.ConnectSSID(betterAP.SSID); err != nil {
		if asJSON {
			res := OptResult{
				Success: false,
				Error:   err.Error(),
			}
			data, _ := json.Marshal(res)
			fmt.Println(string(data))
		} else {
			fmt.Printf("❌ Failed to switch: %v\n", err)
		}
		return
	}

	if asJSON {
		res := OptResult{
			Success:     true,
			Switched:    true,
			CurrentSSID: status.SSID,
			TargetSSID:  betterAP.SSID,
			Band:        string(betterAP.Band),
			Score:       betterAP.QualityScore,
			Reason:      reason,
		}
		data, _ := json.Marshal(res)
		fmt.Println(string(data))
		return
	}

	fmt.Printf("%s🚀 Found better network: %s (%s, Score: %.1f)%s\n",
		tui.ColorGreen+tui.ColorBold, betterAP.SSID, betterAP.Band, betterAP.QualityScore, tui.ColorReset)
	fmt.Printf("   Reason: %s\n", reason)
	fmt.Printf("%s🎉 Successfully switched to %s!%s\n\n", tui.ColorGreen+tui.ColorBold, betterAP.SSID, tui.ColorReset)
	newStatus, _ := wifi.GetInterfaceStatus()
	if newStatus != nil {
		tui.PrintStatus(newStatus)
	}
}

func runDaemon(asJSON bool) {
	watchFlags := flag.NewFlagSet("watch", flag.ExitOnError)
	intervalSec := watchFlags.Int("interval", 20, "Seconds between connection health checks")
	noRoam := watchFlags.Bool("dry-run", false, "Log recommendations without automatically roaming")
	watchFlags.Parse(os.Args[2:])

	cfg := daemon.DefaultConfig()
	cfg.PollInterval = time.Duration(*intervalSec) * time.Second
	cfg.AutoRoam = !*noRoam

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if !asJSON {
		fmt.Printf("🛡️ Starting NomadWiFi Background Roaming Monitor (Interval: %ds, AutoRoam: %v)\n",
			*intervalSec, cfg.AutoRoam)
		fmt.Printf("📁 Log file: %s\n", daemon.GetLogPath())
		fmt.Println("Press Ctrl+C to stop.")
	}

	if err := daemon.StartMonitor(ctx, cfg); err != nil {
		if !asJSON {
			fmt.Printf("Daemon error: %v\n", err)
		}
	}
}

func runLogs(asJSON bool) {
	logPath := daemon.GetLogPath()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if asJSON {
			fmt.Println(`{"logs":[]}`)
		} else {
			fmt.Println("No logs recorded yet. Run 'nomadwifi watch' to start logging.")
		}
		return
	}

	lines := strings.Split(string(data), "\n")
	var cleaned []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			cleaned = append(cleaned, l)
		}
	}

	start := 0
	if len(cleaned) > 30 {
		start = len(cleaned) - 30
	}
	recent := cleaned[start:]

	if asJSON {
		data, _ := json.Marshal(map[string]interface{}{
			"log_path": logPath,
			"logs":     recent,
		})
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📁 Log file path: %s\n\n", logPath)
	fmt.Println("--- Recent Log Entries ---")
	for _, l := range recent {
		fmt.Println(l)
	}
}

func runConnectWithPassword(ssid, password string, asJSON bool) {
	if !asJSON {
		fmt.Printf("Connecting to '%s'...\n", ssid)
	}
	var err error
	if password != "" {
		err = wifi.ConnectSSIDWithPassword(ssid, password)
	} else {
		err = wifi.ConnectSSID(ssid)
	}

	if err != nil {
		if asJSON {
			fmt.Printf(`{"success":false,"error":%q}`+"\n", err.Error())
		} else {
			fmt.Printf("❌ Connection failed: %v\n", err)
		}
		return
	}
	if asJSON {
		fmt.Printf(`{"success":true,"ssid":%q}`+"\n", ssid)
	} else {
		fmt.Printf("✅ Connected to '%s'.\n", ssid)
	}
}

func printHelp() {
	tui.PrintBanner()
	fmt.Println("Available Commands:")
	fmt.Println("  nomadwifi                   Open interactive terminal menu (instant keypress)")
	fmt.Println("  nomadwifi status [--json]   Show active link stats, band, gateway latency & captive portal")
	fmt.Println("  nomadwifi scan [--json]     Scan all surrounding APs and list them ranked by quality score")
	fmt.Println("  nomadwifi optimize [--json] Analyze and auto-switch to the best 5GHz/high-speed hotel AP")
	fmt.Println("  nomadwifi watch [--dry-run] Run background daemon to actively monitor and auto-roam")
	fmt.Println("  nomadwifi logs [--json]     Display recent activity logs and log file path")
	fmt.Println("  nomadwifi connect <SSID> [--password <pwd>] Connect directly with optional password")
}
