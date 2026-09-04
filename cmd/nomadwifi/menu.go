package main

import (
	"fmt"
	"strings"

	"github.com/dariomory/nomadwifi/pkg/tui"
	"github.com/dariomory/nomadwifi/pkg/vpn"
	"github.com/dariomory/nomadwifi/pkg/wifi"
)

func runInteractiveMenu() {
	for {
		tui.ClearScreen()
		tui.PrintBanner()

		if status, err := wifi.GetInterfaceStatus(); err == nil {
			tui.PrintStatus(status)
			tui.PrintVPN(vpn.Detect())
		}

		fmt.Printf("%sWhat would you like to do?%s\n", tui.ColorBold, tui.ColorReset)
		item := func(key, label string) {
			fmt.Printf("  %s[%s]%s %s\n", tui.ColorBold+tui.ColorCyan, key, tui.ColorReset, label)
		}
		item("1", "Refresh this view")
		item("2", "Scan nearby networks")
		item("3", "Switch to the best access point")
		item("4", "Prepare networks for instant failover")
		item("5", "Watch and roam automatically")
		item("6", "View recent activity")
		item("7", "Quit")
		fmt.Print("\n> ")

		switch strings.ToLower(string(tui.ReadKey())) {
		case "1", "r":
			continue
		case "2", "s":
			section(func() { runScan(flags{}) })
		case "3", "o":
			section(func() { runOptimize(flags{}) })
		case "4", "w":
			section(func() { runWarm(flags{}) })
		case "5":
			tui.ClearScreen()
			runDaemon(flags{interval: 20})
		case "6", "l":
			section(func() { runLogs(flags{}) })
		case "7", "q":
			fmt.Println("\nGoodbye.")
			return
		}
	}
}

func section(run func()) {
	tui.ClearScreen()
	tui.PrintBanner()
	run()
	fmt.Printf("%sPress any key to return to the menu.%s", tui.ColorGrey, tui.ColorReset)
	tui.ReadKey()
}
