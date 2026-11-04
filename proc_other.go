//go:build !unix

package main

import "os/exec"

// setProcessGroup does nothing on this platform: process groups are only
// used on Unix-like systems.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills only the referee process on this platform. Bots the
// referee started are not killed.
func killProcessGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
