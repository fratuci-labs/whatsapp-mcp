//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func applyDaemonUmask() {
	syscall.Umask(0o077)
}

func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func daemonSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}

func preferUnixSocket(sockPath string) bool {
	// macOS sockaddr_un.sun_path max length is 104 bytes; fall back to authenticated
	// 127.0.0.1 loopback TCP when directory paths exceed safe socket length.
	return len(sockPath) < 100
}
