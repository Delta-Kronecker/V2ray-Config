//go:build windows

package main

import (
	"os/exec"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
	}
}

func countXrayProcs() int {
	return -1
}

func checkOccupiedPorts(basePort, count int) []int {
	return nil
}
