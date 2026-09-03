package gui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dariomory/nomadwifi/pkg/cluster"
	"github.com/dariomory/nomadwifi/pkg/wifi"
	webview "github.com/jchv/go-webview2"
)

//go:embed index.html
var htmlContent string

// Launch opens the native Windows WebView2 desktop GUI window.
func Launch() error {
	w := webview.NewWithOptions(webview.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview.WindowOptions{
			Title:  "NomadWiFi - Hotel & Travel Roaming Optimizer",
			Width:  820,
			Height: 650,
			IconId: 0,
		},
	})
	if w == nil {
		return fmt.Errorf("failed to create WebView2 window")
	}
	defer w.Destroy()

	// Bind Go callbacks to JS
	_ = w.Bind("getStatus", func() string {
		status, err := wifi.GetInterfaceStatus()
		if err != nil {
			return "{}"
		}
		data, _ := json.Marshal(status)
		return string(data)
	})

	_ = w.Bind("scanNetworks", func() string {
		aps, err := wifi.ScanNetworks()
		if err != nil {
			return "[]"
		}
		data, _ := json.Marshal(aps)
		return string(data)
	})

	_ = w.Bind("optimizeConnection", func() string {
		status, err := wifi.GetInterfaceStatus()
		if err != nil || !status.Connected {
			return "No active connection to optimize"
		}

		aps, err := wifi.ScanNetworks()
		if err != nil || len(aps) == 0 {
			return "No nearby access points found"
		}

		betterAP, reason := cluster.FindAlternativeInCluster(status.SSID, aps, 5.0)
		if betterAP == nil || strings.EqualFold(betterAP.SSID, status.SSID) {
			return fmt.Sprintf("Already connected to optimal AP (%s on %s)", status.SSID, status.Band)
		}

		if err := wifi.ConnectSSID(betterAP.SSID); err != nil {
			return fmt.Sprintf("Switch failed: %v", err)
		}
		return fmt.Sprintf("Switched to %s (%s, Score: %.1f) - %s", betterAP.SSID, betterAP.Band, betterAP.QualityScore, reason)
	})

	_ = w.Bind("connectSSID", func(ssid string) string {
		if err := wifi.ConnectSSID(ssid); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("Connected to %s", ssid)
	})

	w.SetHtml(htmlContent)
	w.Run()
	return nil
}
