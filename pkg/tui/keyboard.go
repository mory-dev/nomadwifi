package tui

import (
	"fmt"
	"syscall"
)


var (
	msvcrt    = syscall.NewLazyDLL("msvcrt.dll")
	procGetch = msvcrt.NewProc("_getch")
)

// ClearScreen clears the terminal screen instantly without spawning any subprocesses.
func ClearScreen() {
	// ANSI sequence: clear screen + clear scrollback + cursor to (0,0)
	fmt.Print("\033[2J\033[3J\033[H")
}


// ReadKey reads a single character immediately upon keypress without waiting for Enter.
func ReadKey() rune {
	r, _, _ := procGetch.Call()
	return rune(r)
}
