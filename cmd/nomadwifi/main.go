package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/daemon"
	"github.com/mory-dev/nomadwifi/pkg/roam"
	"github.com/mory-dev/nomadwifi/pkg/state"
	"github.com/mory-dev/nomadwifi/pkg/tui"
	"github.com/mory-dev/nomadwifi/pkg/vpn"
	"github.com/mory-dev/nomadwifi/pkg/wifi"
)

// Version is stamped at build time with -ldflags "-X main.Version=...".
var Version = "1.2.0"

type flags struct {
	json     bool
	all      bool
	dryRun   bool
	noVPN    bool
	interval int
	password string
}

func parseFlags(args []string) (rest []string, f flags) {
	f.interval = 20
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "--json", "-j":
			f.json = true
		case "--all", "-a":
			f.all = true
		case "--dry-run":
			f.dryRun = true
		case "--no-vpn":
			f.noVPN = true
		case "--interval":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					f.interval = n
				}
				i++
			}
		case "--password", "-p":
			if i+1 < len(args) {
				f.password = args[i+1]
				i++
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, f
}

func main() {
	if len(os.Args) < 2 {
		runInteractiveMenu()
		return
	}

	command := strings.ToLower(os.Args[1])
	rest, f := parseFlags(os.Args[2:])

	switch command {
	case "status":
		runStatus(f)
	case "scan":
		runScan(f)
	case "optimize", "opt":
		runOptimize(f)
	case "connect":
		if len(rest) == 0 {
			fail(f.json, "Missing SSID. Usage: nomadwifi connect <SSID> [--password <key>]")
			return
		}
		runConnect(rest[0], f)
	case "watch", "daemon":
		runDaemon(f)
	case "vpn":
		sub := "status"
		if len(rest) > 0 {
			sub = strings.ToLower(rest[0])
		}
		runVPN(sub, f)
	case "warm":
		runWarm(f)
	case "logs", "log":
		runLogs(f)
	case "agent":
		runAgent()
	case "version", "--version", "-v":
		runVersion(f)
	case "help", "-h", "--help":
		printHelp()
	default:
		fail(f.json, fmt.Sprintf("Unknown command %q. Run 'nomadwifi help' for available commands.", command))
	}
}

func fail(asJSON bool, message string) {
	if asJSON {
		out, _ := json.Marshal(map[string]string{"error": message})
		fmt.Println(string(out))
	} else {
		fmt.Printf("%s%s%s\n", tui.ColorRed, message, tui.ColorReset)
	}
	os.Exit(1)
}

func emit(v interface{}) {
	out, err := json.Marshal(v)
	if err != nil {
		fmt.Printf(`{"error":%q}`+"\n", err.Error())
		return
	}
	fmt.Println(string(out))
}

func runVersion(f flags) {
	if f.json {
		emit(map[string]interface{}{"version": Version, "native_wifi": wifi.NativeAvailable()})
		return
	}
	fmt.Printf("NomadWiFi %s\n", Version)
	fmt.Printf("Native Wi-Fi API: %v\n", wifi.NativeAvailable())
	fmt.Printf("State: %s\n", state.Path())
}

func runStatus(f flags) {
	status, err := wifi.GetInterfaceStatus()
	if err != nil {
		if f.json {
			emit(map[string]interface{}{"connected": false, "error": "Failed to query Wi-Fi adapter"})
			return
		}
		fail(false, fmt.Sprintf("Error: %v", err))
		return
	}

	if f.json {
		emit(status)
		return
	}
	tui.PrintBanner()
	tui.PrintStatus(status)
	tui.PrintVPN(vpn.Detect())
}

func runScan(f flags) {
	var aps []wifi.AccessPoint
	var err error
	if f.all {
		aps, err = wifi.ScanAllBSSIDs()
	} else {
		aps, err = wifi.ScanNetworks()
	}
	if err != nil {
		if f.json {
			emit(map[string]string{"error": "Failed to scan Wi-Fi networks"})
			return
		}
		fail(false, fmt.Sprintf("Scan error: %v", err))
		return
	}

	if f.json {
		emit(aps)
		return
	}

	current := ""
	if s, err := wifi.GetLinkStatus(); err == nil && s.Connected {
		current = s.SSID
	}
	tui.PrintBanner()
	tui.PrintScanTable(aps, current)
}

// OptimizeResult is the JSON shape returned by the optimize command.
type OptimizeResult struct {
	Success     bool    `json:"success"`
	Switched    bool    `json:"switched"`
	CurrentSSID string  `json:"current_ssid"`
	TargetSSID  string  `json:"target_ssid,omitempty"`
	Band        string  `json:"band,omitempty"`
	Score       float64 `json:"score,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	RolledBack  bool    `json:"rolled_back,omitempty"`
	Error       string  `json:"error,omitempty"`
}

func runOptimize(f flags) {
	status, err := wifi.GetLinkStatus()
	if err != nil || status == nil || !status.Connected {
		if f.json {
			emit(OptimizeResult{Error: "No active Wi-Fi connection"})
			return
		}
		fail(false, "No active Wi-Fi connection to optimize.")
		return
	}

	logf := func(format string, args ...interface{}) {
		if !f.json {
			fmt.Printf("  "+format+"\n", args...)
		}
	}

	cfg := roam.DefaultConfig()
	cfg.ManageVPN = !f.noVPN
	engine := roam.New(cfg, logf)

	ladder := engine.Ladder(status.SSID)
	currentScore := currentScoreFor(status.SSID)

	if len(ladder) == 0 || ladder[0].AP.QualityScore <= currentScore {
		if f.json {
			emit(OptimizeResult{
				Success:     true,
				CurrentSSID: status.SSID,
				Reason:      "Already on the best access point in range",
			})
			return
		}
		fmt.Printf("%sYou are already on the best access point in range.%s\n",
			tui.ColorGreen+tui.ColorBold, tui.ColorReset)
		return
	}

	best := ladder[0]
	if !f.json {
		fmt.Printf("%sSwitching to %s (%s, score %.1f vs %.1f)%s\n",
			tui.ColorBold, best.AP.SSID, best.AP.Band, best.AP.QualityScore, currentScore, tui.ColorReset)
		fmt.Printf("  Reason: %s\n", best.Reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	res := engine.SwitchTo(ctx, status.SSID, best.AP.SSID, best.Reason)

	if f.json {
		emit(OptimizeResult{
			Success:     res.Switched,
			Switched:    res.Switched,
			CurrentSSID: status.SSID,
			TargetSSID:  res.To,
			Band:        string(best.AP.Band),
			Score:       best.AP.QualityScore,
			Reason:      best.Reason,
			RolledBack:  res.RolledBack,
			Error:       res.Error,
		})
		return
	}

	switch {
	case res.Switched:
		fmt.Printf("%sConnected to %s.%s\n\n", tui.ColorGreen+tui.ColorBold, res.To, tui.ColorReset)
		if newStatus, err := wifi.GetInterfaceStatus(); err == nil {
			tui.PrintStatus(newStatus)
		}
	case res.RolledBack:
		fmt.Printf("%s%s did not carry traffic, so %s was restored.%s\n",
			tui.ColorYellow, best.AP.SSID, res.From, tui.ColorReset)
	default:
		fail(false, fmt.Sprintf("Could not switch: %s", res.Error))
	}
}

func currentScoreFor(ssid string) float64 {
	aps, err := wifi.ScanNetworks()
	if err != nil {
		return 0
	}
	for _, ap := range aps {
		if strings.EqualFold(ap.SSID, ssid) {
			return ap.QualityScore
		}
	}
	return 0
}

func runConnect(ssid string, f flags) {
	if !f.json {
		fmt.Printf("Connecting to %q...\n", ssid)
	}

	var err error
	if f.password != "" {
		err = wifi.ConnectSSIDWithPassword(ssid, f.password)
	} else {
		err = wifi.ConnectSSID(ssid)
	}

	if err != nil {
		if f.json {
			emit(map[string]interface{}{"success": false, "ssid": ssid, "error": err.Error()})
			return
		}
		fail(false, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	if f.json {
		emit(map[string]interface{}{"success": true, "ssid": ssid})
		return
	}
	fmt.Printf("%sConnected to %q.%s\n", tui.ColorGreen, ssid, tui.ColorReset)
}

func runDaemon(f flags) {
	cfg := daemon.DefaultConfig()
	cfg.PollInterval = time.Duration(f.interval) * time.Second
	cfg.AutoRoam = !f.dryRun
	cfg.ManageVPN = !f.noVPN
	cfg.Quiet = f.json

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if !f.json {
		tui.PrintBanner()
		fmt.Printf("Monitoring every %ds   auto-roam: %v   VPN coordination: %v\n",
			f.interval, cfg.AutoRoam, cfg.ManageVPN)
		fmt.Printf("Log file: %s\n\n", daemon.GetLogPath())
		fmt.Println("Press Ctrl+C to stop.")
		fmt.Println()
	}

	if err := daemon.StartMonitor(ctx, cfg); err != nil {
		fail(f.json, fmt.Sprintf("Daemon error: %v", err))
	}
}

func runVPN(sub string, f flags) {
	manager := vpn.NewManager(true, nil)

	switch sub {
	case "hold", "pause", "down":
		outcome := manager.Hold("requested from the command line")
		if f.json {
			emit(outcome)
			return
		}
		if len(outcome.Held) > 0 {
			fmt.Printf("%sPaused: %s%s\n", tui.ColorGreen, strings.Join(outcome.Held, ", "), tui.ColorReset)
		}
		if outcome.Advice != "" {
			fmt.Printf("%s%s%s\n", tui.ColorYellow, outcome.Advice, tui.ColorReset)
		}
		if len(outcome.Held) == 0 && outcome.Advice == "" {
			fmt.Println("No active VPN tunnel to pause.")
		}

	case "resume", "up":
		manager.Repair()
		manager.Resume()
		if f.json {
			emit(map[string]bool{"resumed": true})
			return
		}
		fmt.Printf("%sResumed any tunnel NomadWiFi paused and flushed the DNS cache.%s\n",
			tui.ColorGreen, tui.ColorReset)

	default:
		status := manager.Status()
		if f.json {
			emit(status)
			return
		}
		tui.PrintBanner()
		tui.PrintVPN(status)
	}
}

func runWarm(f flags) {
	aps, err := wifi.ScanNetworks()
	if err != nil {
		fail(f.json, fmt.Sprintf("Scan error: %v", err))
		return
	}

	results := wifi.WarmVenue(aps, wifi.DefaultWarmSetSize)
	removed := wifi.CleanupProvisionedProfiles()

	if f.json {
		emit(map[string]interface{}{"warmed": results, "removed": removed})
		return
	}

	tui.PrintBanner()
	if len(results) == 0 {
		fmt.Println("Every nearby network NomadWiFi can join is already prepared.")
	}
	for _, r := range results {
		switch {
		case r.Warmed:
			fmt.Printf("  %sready%s    %-26s %s\n", tui.ColorGreen, tui.ColorReset, r.SSID, r.Source)
		case r.Error != "":
			fmt.Printf("  %sfailed%s   %-26s %s\n", tui.ColorRed, tui.ColorReset, r.SSID, r.Error)
		default:
			fmt.Printf("  skipped   %-26s %s\n", r.SSID, r.Skipped)
		}
	}
	for _, ssid := range removed {
		fmt.Printf("  %sremoved%s  %-26s stale profile from a failed password guess\n",
			tui.ColorYellow, tui.ColorReset, ssid)
	}
}

func runLogs(f flags) {
	logPath := daemon.GetLogPath()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if f.json {
			emit(map[string]interface{}{"log_path": logPath, "logs": []string{}})
			return
		}
		fmt.Println("No logs recorded yet. Run 'nomadwifi watch' to start logging.")
		return
	}

	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}

	if f.json {
		emit(map[string]interface{}{"log_path": logPath, "logs": lines})
		return
	}
	fmt.Printf("Log file: %s\n\n", logPath)
	for _, l := range lines {
		fmt.Println(l)
	}
}

func printHelp() {
	tui.PrintBanner()
	fmt.Println("Usage: nomadwifi <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  (no command)              Open the interactive terminal menu")
	fmt.Println("  status                    Link stats, gateway latency, captive portal and VPN state")
	fmt.Println("  scan [--all]              Rank nearby networks; --all lists every radio separately")
	fmt.Println("  optimize                  Switch to the best access point, rolling back if it fails")
	fmt.Println("  connect <SSID>            Connect, optionally with --password <key>")
	fmt.Println("  watch [--interval N]      Monitor continuously and roam automatically")
	fmt.Println("  vpn [status|hold|resume]  Inspect or coordinate the VPN tunnel")
	fmt.Println("  warm                      Prepare profiles for instant failover, clean up stale ones")
	fmt.Println("  logs                      Show recent activity")
	fmt.Println("  version                   Print the version")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --json                    Machine-readable output, supported by every command")
	fmt.Println("  --dry-run                 With watch: report what it would do without roaming")
	fmt.Println("  --no-vpn                  Do not pause or resume the VPN around a roam")
	fmt.Println("  --password <key>          With connect: the network key")
	fmt.Println("  --interval <seconds>      With watch: how often to measure (default 20)")
}
