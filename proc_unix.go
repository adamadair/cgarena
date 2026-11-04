//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes cmd start in its own process group, so that
// killProcessGroup can also reach the bots the referee starts.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the started command's whole process
// group: the referee and every process it started that is still in the group.
func killProcessGroup(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
