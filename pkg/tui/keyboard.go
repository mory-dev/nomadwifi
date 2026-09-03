package tui

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

var (
	msvcrt    = syscall.NewLazyDLL("msvcrt.dll")
	procGetch = msvcrt.NewProc("_getch")
)

// ClearScreen clears the terminal screen and resets cursor position to top.
func ClearScreen() {
	fmt.Print("\033[H\033[2J")
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

// ReadKey reads a single character immediately upon keypress without waiting for Enter.
func ReadKey() rune {
	r, _, _ := procGetch.Call()
	return rune(r)
}
