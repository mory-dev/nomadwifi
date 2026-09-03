package wifi

import (
	"sync"
	"time"
)

var (
	cachedStatus *InterfaceStatus
	statusMu     sync.RWMutex
	pollerOnce   sync.Once
)

// InitBackgroundPoller starts an async background poller so GUI/CLI calls never block on netsh/ping.
func InitBackgroundPoller() {
	pollerOnce.Do(func() {
		// Initial sync fetch
		s, err := GetInterfaceStatus()
		if err == nil {
			statusMu.Lock()
			cachedStatus = s
			statusMu.Unlock()
		}

		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				s, err := GetInterfaceStatus()
				if err == nil {
					statusMu.Lock()
					cachedStatus = s
					statusMu.Unlock()
				}
			}
		}()
	})
}

// GetCachedStatus returns the latest status instantly from memory (0ms latency).
func GetCachedStatus() *InterfaceStatus {
	statusMu.RLock()
	defer statusMu.RUnlock()
	if cachedStatus == nil {
		s, _ := GetInterfaceStatus()
		return s
	}
	return cachedStatus
}
