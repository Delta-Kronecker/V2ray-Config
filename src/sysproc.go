package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ── batchTracker ──────────────────────────────────────────────────────────────

type batchTracker struct {
	mu   sync.Mutex
	cmds []*exec.Cmd
}

func (bt *batchTracker) register(cmd *exec.Cmd) {
	bt.mu.Lock()
	bt.cmds = append(bt.cmds, cmd)
	bt.mu.Unlock()
}

func (bt *batchTracker) killAll() {
	bt.mu.Lock()
	cmds := make([]*exec.Cmd, len(bt.cmds))
	copy(cmds, bt.cmds)
	bt.cmds = bt.cmds[:0]
	bt.mu.Unlock()

	var wg sync.WaitGroup
	for _, cmd := range cmds {
		cmd := cmd
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
	}
	wg.Wait()
}

// ── killGroup ─────────────────────────────────────────────────────────────────

func killGroup(cmd *exec.Cmd) {
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

// ── Xray helpers ─────────────────────────────────────────────────────────────

func xrayPath() string {
	for _, p := range []string{"./xray", "/usr/local/bin/xray"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "xray"
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

// ── Stderr extraction ─────────────────────────────────────────────────────────

func extractErr(stderr string) string {
	var errs []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "warn") || strings.Contains(lower, "deprecated") {
			continue
		}
		if strings.Contains(lower, `"level":"info"`) || strings.Contains(lower, `"level":"debug"`) ||
			strings.Contains(lower, "level=info") || strings.Contains(lower, "level=debug") {
			continue
		}
		if len(line) > 120 {
			line = line[:120] + "..."
		}
		errs = append(errs, line)
		if len(errs) >= 3 {
			break
		}
	}
	return strings.Join(errs, " | ")
}

func extractErrVerbose(stderr string) string {
	var first, best string
	priority := []string{"invalid", "failed", "decode", "unsupported", "error"}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "warn") || strings.Contains(lower, "deprecated") {
			continue
		}
		if strings.Contains(lower, `"level":"info"`) || strings.Contains(lower, `"level":"debug"`) ||
			strings.Contains(lower, "level=info") || strings.Contains(lower, "level=debug") {
			continue
		}
		if idx := strings.Index(line, `"msg":"`); idx != -1 {
			end := strings.Index(line[idx+7:], `"`)
			if end != -1 {
				line = line[idx+7 : idx+7+end]
				lower = strings.ToLower(line)
			}
		}
		if first == "" {
			first = line
		}
		if best == "" {
			for _, kw := range priority {
				if strings.Contains(lower, kw) {
					best = line
					break
				}
			}
		}
	}
	r := best
	if r == "" {
		r = first
	}
	if len(r) > 180 {
		r = r[:180] + "..."
	}
	return r
}

func shortenErr(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	if strings.HasPrefix(s, "Get ") {
		if i := strings.Index(s, ": "); i != -1 && i > 10 {
			s = s[i+2:]
		}
	}
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}

// ── Misc ──────────────────────────────────────────────────────────────────────

// suppress unused fmt import warning
var _ = fmt.Sprintf
