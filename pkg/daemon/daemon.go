package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dariomory/nomadwifi/pkg/cluster"
	"github.com/dariomory/nomadwifi/pkg/wifi"
)

// Config defines daemon monitoring parameters.
type Config struct {
	PollInterval    time.Duration
	MinScoreDelta   float64
	MinRxMbps       int
	MaxLatencyMs    float64
	MaxPacketLoss   float64
	AutoRoam        bool
	Prefer5GHz      bool
	CooldownPeriod  time.Duration
}

// DefaultConfig returns optimal defaults for nomad hotel travel.
func DefaultConfig() Config {
	return Config{
		PollInterval:   20 * time.Second,
		MinScoreDelta:  15.0,
		MinRxMbps:      50,
		MaxLatencyMs:   150.0,
		MaxPacketLoss:  10.0,
		AutoRoam:       true,
		Prefer5GHz:     true,
		CooldownPeriod: 45 * time.Second,
	}
}

// StartMonitor runs the background monitoring loop.
func StartMonitor(ctx context.Context, cfg Config) error {
	log.Printf("[NomadWiFi] Monitoring active Wi-Fi every %v (AutoRoam: %v, Prefer5GHz: %v)\n",
		cfg.PollInterval, cfg.AutoRoam, cfg.Prefer5GHz)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	var lastRoamTime time.Time

	for {
		select {
		case <-ctx.Done():
			log.Println("[NomadWiFi] Daemon stopped.")
			return nil
		case <-ticker.C:
			status, err := wifi.GetInterfaceStatus()
			if err != nil || !status.Connected {
				continue
			}

			// Check if we are in cooldown
			if time.Since(lastRoamTime) < cfg.CooldownPeriod {
				continue
			}

			// Evaluate if current connection is degraded
			degraded := false
			degradedReason := ""

			if cfg.Prefer5GHz && status.Band == wifi.Band24GHz {
				degraded = true
				degradedReason = "Connected on 2.4 GHz band instead of 5 GHz"
			} else if status.RxMbps > 0 && status.RxMbps < cfg.MinRxMbps {
				degraded = true
				degradedReason = fmt.Sprintf("Low link speed (%d Mbps < %d Mbps)", status.RxMbps, cfg.MinRxMbps)
			} else if status.PacketLossPercent > cfg.MaxPacketLoss {
				degraded = true
				degradedReason = fmt.Sprintf("High packet loss (%.1f%% > %.1f%%)", status.PacketLossPercent, cfg.MaxPacketLoss)
			} else if status.GatewayLatencyMs > cfg.MaxLatencyMs {
				degraded = true
				degradedReason = fmt.Sprintf("High gateway latency (%.1fms > %.1fms)", status.GatewayLatencyMs, cfg.MaxLatencyMs)
			}

			if !degraded {
				continue
			}

			// Scan for alternatives
			aps, err := wifi.ScanNetworks()
			if err != nil || len(aps) == 0 {
				continue
			}

			betterAP, reason := cluster.FindAlternativeInCluster(status.SSID, aps, cfg.MinScoreDelta)
			if betterAP != nil {
				log.Printf("[NomadWiFi] Degraded condition: %s\n", degradedReason)
				log.Printf("[NomadWiFi] Found better AP: %s (%s, Score: %.1f) - Reason: %s\n",
					betterAP.SSID, betterAP.Band, betterAP.QualityScore, reason)

				if cfg.AutoRoam {
					log.Printf("[NomadWiFi] Roaming to %s...\n", betterAP.SSID)
					if err := wifi.ConnectSSID(betterAP.SSID); err != nil {
						log.Printf("[NomadWiFi] Roaming failed: %v\n", err)
					} else {
						log.Printf("[NomadWiFi] Successfully roamed to %s!\n", betterAP.SSID)
						lastRoamTime = time.Now()
					}
				}
			}
		}
	}
}
