package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
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
			killProcessGroup(cmd)
		}()
	}
	wg.Wait()
}

// ── killGroup ─────────────────────────────────────────────────────────────────

func killGroup(cmd *exec.Cmd) {
	killProcessGroup(cmd)
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
