//go:build windows

package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// prepareCmdPlatform sets Windows-specific flags to hide command window
func prepareCmdPlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

// killProcessTree terminates the process and its child processes on Windows
func killProcessTree(proc *os.Process) {
	if proc != nil && proc.Pid > 0 {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", proc.Pid)).Run()
	}
}
