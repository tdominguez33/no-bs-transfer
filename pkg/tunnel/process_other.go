//go:build !windows

package tunnel

import (
	"os"
	"os/exec"
)

// prepareCmdPlatform is a no-op on non-Windows platforms
func prepareCmdPlatform(cmd *exec.Cmd) {
	// No-op on POSIX systems
}

// killProcessTree terminates the process on non-Windows platforms
func killProcessTree(proc *os.Process) {
	if proc != nil {
		_ = proc.Kill()
	}
}
