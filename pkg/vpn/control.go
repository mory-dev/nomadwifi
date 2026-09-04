package vpn

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// controller knows how to take one VPN client down and bring it back up.
type controller interface {
	// Available reports whether this controller can act right now.
	Available() bool
	// Hint explains what the user would have to do when it cannot.
	Hint() string
	Down() error
	Up() error
}

func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

func controllerFor(provider, adapter string) controller {
	switch {
	case strings.Contains(provider, "Tailscale"):
		return &tailscaleController{}
	case strings.Contains(provider, "WireGuard"):
		return &wireguardController{tunnel: adapter}
	default:
		// Everything else, NordVPN included, has no supported command line on
		// Windows. Cycling the tunnel adapter works for any client, but needs
		// an administrator token.
		return &adapterController{adapter: adapter}
	}
}

// tailscaleController drives the Tailscale CLI, which supports up/down cleanly.
type tailscaleController struct{ path string }

func (c *tailscaleController) exePath() string {
	if c.path != "" {
		return c.path
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Tailscale", "tailscale.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			c.path = p
			return p
		}
	}
	if p, err := exec.LookPath("tailscale.exe"); err == nil {
		c.path = p
	}
	return c.path
}

func (c *tailscaleController) Available() bool { return c.exePath() != "" }

func (c *tailscaleController) Hint() string {
	if c.Available() {
		return "Managed automatically with the Tailscale CLI"
	}
	return "Install the Tailscale CLI to let NomadWiFi pause it during a roam"
}

func (c *tailscaleController) Down() error {
	if !c.Available() {
		return fmt.Errorf("tailscale CLI not found")
	}
	out, err := hidden(c.exePath(), "down").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale down: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *tailscaleController) Up() error {
	if !c.Available() {
		return fmt.Errorf("tailscale CLI not found")
	}
	out, err := hidden(c.exePath(), "up").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale up: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// wireguardController drives wireguard.exe, which activates and deactivates
// tunnels by name.
type wireguardController struct {
	tunnel string
	path   string
}

func (c *wireguardController) exePath() string {
	if c.path != "" {
		return c.path
	}
	p := filepath.Join(os.Getenv("ProgramFiles"), "WireGuard", "wireguard.exe")
	if _, err := os.Stat(p); err == nil {
		c.path = p
	}
	return c.path
}

func (c *wireguardController) Available() bool { return c.exePath() != "" }

func (c *wireguardController) Hint() string {
	if c.Available() {
		return "Managed automatically with wireguard.exe"
	}
	return "WireGuard for Windows not found"
}

func (c *wireguardController) Down() error {
	if !c.Available() {
		return fmt.Errorf("wireguard.exe not found")
	}
	return hidden(c.exePath(), "/deactivate", c.tunnel).Run()
}

func (c *wireguardController) Up() error {
	if !c.Available() {
		return fmt.Errorf("wireguard.exe not found")
	}
	return hidden(c.exePath(), "/activate", c.tunnel).Run()
}

// adapterController disables and re-enables the tunnel's network adapter. It
// works with any client but requires elevation, so it is never attempted
// silently from an ordinary session.
type adapterController struct{ adapter string }

func (c *adapterController) Available() bool { return IsElevated() }

func (c *adapterController) Hint() string {
	if c.Available() {
		return "Managed by cycling the " + c.adapter + " adapter"
	}
	return "This client has no command line on Windows. Run NomadWiFi as administrator to let it pause the tunnel for you, or toggle the VPN yourself when prompted."
}

func (c *adapterController) set(enabled bool) error {
	if !c.Available() {
		return fmt.Errorf("administrator rights are required to change the %s adapter", c.adapter)
	}
	state := "disable"
	if enabled {
		state = "enable"
	}
	out, err := hidden("netsh", "interface", "set", "interface",
		fmt.Sprintf("name=%s", c.adapter), fmt.Sprintf("admin=%s", state)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to %s %s: %w (%s)", state, c.adapter, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *adapterController) Down() error { return c.set(false) }
func (c *adapterController) Up() error   { return c.set(true) }

var (
	modadvapi32          = syscall.NewLazyDLL("advapi32.dll")
	procOpenProcessToken = modadvapi32.NewProc("OpenProcessToken")
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProc   = modkernel32.NewProc("GetCurrentProcess")
)

// IsElevated reports whether this process holds an administrator token.
func IsElevated() bool {
	const tokenQuery = 0x0008
	const tokenElevation = 20

	proc, _, _ := procGetCurrentProc.Call()
	var token syscall.Handle
	r, _, _ := procOpenProcessToken.Call(proc, tokenQuery, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return false
	}
	defer syscall.CloseHandle(token)

	var elevation uint32
	var returned uint32
	err := syscall.GetTokenInformation(syscall.Token(token), tokenElevation,
		(*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &returned)
	if err != nil {
		return false
	}
	return elevation != 0
}
