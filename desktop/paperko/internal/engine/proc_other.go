//go:build !windows

package engine

import "os/exec"

// hideChildConsole is a no-op on non-Windows platforms.
func hideChildConsole(cmd *exec.Cmd) {}
