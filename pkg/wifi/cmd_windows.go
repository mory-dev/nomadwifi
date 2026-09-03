package wifi

import (
	"os/exec"
	"syscall"
)

// SilentCommand creates an exec.Cmd with Windows CREATE_NO_WINDOW flags so zero console windows ever appear.
func SilentCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}
