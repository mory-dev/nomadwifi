// Package daemon runs the roaming engine as a long-lived background monitor
// with persistent logging.
package daemon

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/dariomory/nomadwifi/pkg/roam"
	"github.com/dariomory/nomadwifi/pkg/state"
)

// Config re-exports the roaming settings the CLI can adjust.
type Config struct {
	PollInterval time.Duration
	AutoRoam     bool
	ManageVPN    bool
	Quiet        bool
}

// DefaultConfig returns the defaults used by "nomadwifi watch".
func DefaultConfig() Config {
	base := roam.DefaultConfig()
	return Config{
		PollInterval: base.PollInterval,
		AutoRoam:     base.AutoRoam,
		ManageVPN:    base.ManageVPN,
	}
}

// GetLogPath returns the path to the persistent NomadWiFi log file.
func GetLogPath() string {
	return filepath.Join(state.Dir(), "nomadwifi.log")
}

// StartMonitor runs the roaming engine until the context is cancelled,
// logging to both the console and the log file.
func StartMonitor(ctx context.Context, cfg Config) error {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	if f, err := os.OpenFile(GetLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer f.Close()
		if cfg.Quiet {
			logger.SetOutput(f)
		} else {
			logger.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}

	engineCfg := roam.DefaultConfig()
	engineCfg.PollInterval = cfg.PollInterval
	engineCfg.AutoRoam = cfg.AutoRoam
	engineCfg.ManageVPN = cfg.ManageVPN

	return roam.New(engineCfg, logger.Printf).Run(ctx)
}
