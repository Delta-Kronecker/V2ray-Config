//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pgid, err := syscall.Getpgid(pid); err == nil {
		syscall.Kill(-pgid, syscall.SIGKILL)
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
		if pgid, err := syscall.Getpgid(pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
		syscall.Kill(pid, syscall.SIGKILL)
	}
}

func countXrayProcs() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		out, err2 := exec.Command("pgrep", "-c", "xray").Output()
		if err2 != nil {
			return -1
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		return n
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		allDigit := true
		for _, c := range name {
			if c < '0' || c > '9' {
				allDigit = false
				break
			}
		}
		if !allDigit {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + name + "/cmdline")
		if err != nil {
			continue
		}
		comm := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if strings.Contains(comm, "xray") {
			count++
		}
	}
	return count
}

func readProcNetTCPPorts() map[int]bool {
	ports := make(map[int]bool)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			stateHex := fields[3]
			if stateHex != "0A" {
				continue
			}
			localAddr := fields[1]
			colonIdx := strings.LastIndex(localAddr, ":")
			if colonIdx == -1 {
				continue
			}
			portHex := localAddr[colonIdx+1:]
			portVal, err := strconv.ParseInt(portHex, 16, 32)
			if err != nil {
				continue
			}
			ports[int(portVal)] = true
		}
	}
	return ports
}

func checkOccupiedPorts(basePort, count int) []int {
	listeningPorts := readProcNetTCPPorts()
	var occupied []int
	for i := 0; i < count; i++ {
		p := basePort + i
		if listeningPorts[p] {
			occupied = append(occupied, p)
		}
	}
	return occupied
}
