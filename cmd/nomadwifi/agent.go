package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mory-dev/nomadwifi/pkg/roam"
	"github.com/mory-dev/nomadwifi/pkg/vpn"
	"github.com/mory-dev/nomadwifi/pkg/wifi"
)

// The agent is a long-lived process the GUI talks to over stdin and stdout
// using newline-delimited JSON.
//
// The alternative, launching the CLI once per query, cost a process start for
// every refresh and gave the UI no way to hear about a disconnect: it could
// only find out on its next poll. A persistent channel also lets the roaming
// engine live in one place instead of being reimplemented in the UI.

type agentRequest struct {
	ID     int                    `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type agentResponse struct {
	ID     int         `json:"id,omitempty"`
	OK     bool        `json:"ok"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
	Event  string      `json:"event,omitempty"`
}

type agent struct {
	mu     sync.Mutex
	out    *json.Encoder
	engine *roam.Engine
}

func runAgent() {
	a := &agent{out: json.NewEncoder(os.Stdout)}

	cfg := roam.DefaultConfig()
	a.engine = roam.New(cfg, a.logEvent)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := a.engine.Run(ctx); err != nil {
			a.logEvent("[agent] roaming engine stopped: %v", err)
		}
	}()

	a.push("ready", map[string]interface{}{
		"version":     Version,
		"native_wifi": wifi.NativeAvailable(),
	})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Requests are handled concurrently so a slow scan cannot block a status
	// query, and the wait group makes sure replies are still written when
	// stdin closes mid-flight.
	var inFlight sync.WaitGroup

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req agentRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			a.send(agentResponse{OK: false, Error: "malformed request: " + err.Error()})
			continue
		}

		inFlight.Add(1)
		go func(r agentRequest) {
			defer inFlight.Done()
			a.handle(ctx, r)
		}(req)
	}

	inFlight.Wait()
}

func (a *agent) send(resp agentResponse) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.out.Encode(resp)
}

func (a *agent) push(event string, data interface{}) {
	a.send(agentResponse{OK: true, Event: event, Result: data})
}

func (a *agent) logEvent(format string, args ...interface{}) {
	a.push("log", map[string]interface{}{
		"message": fmt.Sprintf(format, args...),
		"at":      time.Now().Format(time.RFC3339),
	})
}

func (a *agent) ok(id int, result interface{}) {
	a.send(agentResponse{ID: id, OK: true, Result: result})
}

func (a *agent) err(id int, format string, args ...interface{}) {
	a.send(agentResponse{ID: id, OK: false, Error: fmt.Sprintf(format, args...)})
}

func (a *agent) handle(ctx context.Context, req agentRequest) {
	switch strings.ToLower(req.Method) {
	case "status":
		status, err := wifi.GetInterfaceStatus()
		if err != nil {
			a.err(req.ID, "failed to query the Wi-Fi adapter: %v", err)
			return
		}
		a.ok(req.ID, status)

	case "scan":
		aps, err := wifi.ScanNetworks()
		if err != nil {
			a.err(req.ID, "scan failed: %v", err)
			return
		}
		a.ok(req.ID, aps)

	case "optimize":
		a.handleOptimize(ctx, req)

	case "connect":
		ssid := stringParam(req.Params, "ssid")
		if ssid == "" {
			a.err(req.ID, "connect requires an ssid")
			return
		}
		password := stringParam(req.Params, "password")

		var err error
		if password != "" {
			err = wifi.ConnectSSIDWithPassword(ssid, password)
		} else {
			err = wifi.ConnectSSID(ssid)
		}
		if err != nil {
			a.err(req.ID, "%v", err)
			return
		}
		a.ok(req.ID, map[string]interface{}{"ssid": ssid})

	case "vpn_status":
		a.ok(req.ID, vpn.Detect())

	case "vpn_hold":
		a.ok(req.ID, vpn.NewManager(true, a.logEvent).Hold("requested from the app"))

	case "vpn_resume":
		m := vpn.NewManager(true, a.logEvent)
		m.Repair()
		m.Resume()
		a.ok(req.ID, map[string]bool{"resumed": true})

	case "warm":
		aps, err := wifi.ScanNetworks()
		if err != nil {
			a.err(req.ID, "scan failed: %v", err)
			return
		}
		a.ok(req.ID, map[string]interface{}{
			"warmed":  wifi.WarmVenue(aps, wifi.DefaultWarmSetSize),
			"removed": wifi.CleanupProvisionedProfiles(),
		})

	case "set_autoroam":
		enabled := boolParam(req.Params, "enabled")
		a.engine.SetAutoRoam(enabled)
		a.ok(req.ID, map[string]bool{"auto_roam": enabled})

	case "get_autoroam":
		a.ok(req.ID, map[string]bool{"auto_roam": a.engine.AutoRoam()})

	case "version":
		a.ok(req.ID, map[string]interface{}{
			"version":     Version,
			"native_wifi": wifi.NativeAvailable(),
		})

	case "ping":
		a.ok(req.ID, map[string]string{"pong": time.Now().Format(time.RFC3339)})

	default:
		a.err(req.ID, "unknown method %q", req.Method)
	}
}

func (a *agent) handleOptimize(ctx context.Context, req agentRequest) {
	status, err := wifi.GetLinkStatus()
	if err != nil || status == nil || !status.Connected {
		a.err(req.ID, "no active Wi-Fi connection to optimize")
		return
	}

	ladder := a.engine.Ladder(status.SSID)
	current := currentScoreFor(status.SSID)

	if len(ladder) == 0 || ladder[0].AP.QualityScore <= current {
		a.ok(req.ID, OptimizeResult{
			Success:     true,
			CurrentSSID: status.SSID,
			Reason:      "Already on the best access point in range",
		})
		return
	}

	best := ladder[0]
	switchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	res := a.engine.SwitchTo(switchCtx, status.SSID, best.AP.SSID, best.Reason)
	a.ok(req.ID, OptimizeResult{
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

	if res.Switched {
		a.push("roamed", res)
	}
}

func stringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key].(string); ok {
		return v
	}
	return ""
}

func boolParam(params map[string]interface{}, key string) bool {
	if params == nil {
		return false
	}
	if v, ok := params[key].(bool); ok {
		return v
	}
	return false
}
