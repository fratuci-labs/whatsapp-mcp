//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	windowsDetachedProcess       = 0x00000008
	windowsCreateNewProcessGroup = 0x00000200
)

func applyDaemonUmask() {
	// Windows relies on per-user %LOCALAPPDATA% directory ACLs and Bearer token auth.
}

func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windowsCreateNewProcessGroup | windowsDetachedProcess,
	}
}

func daemonSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func preferUnixSocket(_ string) bool {
	return false
}
