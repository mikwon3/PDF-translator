//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

// hideChildConsole prevents the Python sidecar from opening a console window
// when launched from the GUI app (the black window). CREATE_NO_WINDOW (0x08000000)
// runs the console child without allocating a console.
func hideChildConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
