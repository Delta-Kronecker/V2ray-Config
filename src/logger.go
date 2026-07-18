package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Logger ────────────────────────────────────────────────────────────────────

type Logger struct {
	mu         sync.Mutex
	file       *os.File
	buf        *bufio.Writer
	passed     int64
	parseFail  int64
	startFail  int64
	connFail   int64
	totalTest  int64
	protoStats map[string]*protoStat
}

var gLog *Logger

func newLogger(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	ts := time.Now().Format("2006-01-02_15-04-05")
	f, err := os.Create(filepath.Join(dir, "validation_"+ts+".log"))
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:       f,
		buf:        bufio.NewWriterSize(f, 256*1024),
		protoStats: make(map[string]*protoStat),
	}, nil
}

func (l *Logger) writeLine(s string) {
	l.mu.Lock()
	l.buf.WriteString(s)
	l.buf.WriteByte('\n')
	l.mu.Unlock()
}

func (l *Logger) logStart(fetched, failedSrc int) {
	l.writeLine("==========================================================")
	l.writeLine("  VALIDATION RUN STARTED")
	l.writeLine(fmt.Sprintf("  Time      : %s", time.Now().Format("2006-01-02 15:04:05 MST")))
	l.writeLine(fmt.Sprintf("  Workers   : %d", cfg.Validation.NumWorkers))
	l.writeLine(fmt.Sprintf("  Timeout   : %.0fs per config", cfg.Validation.GlobalTimeoutSec))
	l.writeLine(fmt.Sprintf("  Fetched   : %d  |  FailedSrc: %d", fetched, failedSrc))
	l.writeLine("==========================================================")
	l.writeLine("")
}

func (l *Logger) logProtoStart(proto string, count int) {
	l.mu.Lock()
	if _, ok := l.protoStats[proto]; !ok {
		l.protoStats[proto] = &protoStat{}
	}
	l.mu.Unlock()
	l.writeLine(fmt.Sprintf("--- PROTOCOL: %s (%d unique) ---", strings.ToUpper(proto), count))
}

func (l *Logger) logResult(idx int64, proto, configURL string, res validationResult) {
	l.mu.Lock()
	st := l.protoStats[proto]
	if st == nil {
		st = &protoStat{}
		l.protoStats[proto] = st
	}
	l.mu.Unlock()

	st.mu.Lock()
	st.tested++
	if res.passed {
		st.passed++
		st.totalLatMs += res.latency.Milliseconds()
		atomic.AddInt64(&l.passed, 1)
	} else if strings.HasPrefix(res.failReason, "PARSE:") {
		st.parseFail++
		atomic.AddInt64(&l.parseFail, 1)
	} else if strings.HasPrefix(res.failReason, "XRAY_START:") || strings.HasPrefix(res.failReason, "START:") {
		st.startFail++
		atomic.AddInt64(&l.startFail, 1)
	} else {
		st.connFail++
		atomic.AddInt64(&l.connFail, 1)
	}
	atomic.AddInt64(&l.totalTest, 1)
	st.mu.Unlock()

	ts := time.Now().Format("15:04:05.000")
	if res.passed {
		l.writeLine(fmt.Sprintf("[%s] PASS  [%5d] %-6s lat=%dms  %s",
			ts, idx, proto, res.latency.Milliseconds(), truncate(configURL, 120)))
	} else {
		l.writeLine(fmt.Sprintf("[%s] FAIL  [%5d] %-6s %s  |  %s",
			ts, idx, proto, truncate(res.failReason, 80), truncate(configURL, 60)))
	}
}

func (l *Logger) logSummary(duration float64, results []configResult, failedLinks []string) {
	byProto := make(map[string]int)
	for _, r := range results {
		byProto[r.proto]++
	}

	l.writeLine("")
	l.writeLine("==========================================================")
	l.writeLine("  SUMMARY")
	l.writeLine("==========================================================")
	l.writeLine(fmt.Sprintf("  Duration    : %.2fs", duration))
	l.writeLine(fmt.Sprintf("  Total Tested: %d", atomic.LoadInt64(&l.totalTest)))
	l.writeLine(fmt.Sprintf("  Passed      : %d", atomic.LoadInt64(&l.passed)))
	l.writeLine(fmt.Sprintf("  Parse Fail  : %d", atomic.LoadInt64(&l.parseFail)))
	l.writeLine(fmt.Sprintf("  Start Fail  : %d", atomic.LoadInt64(&l.startFail)))
	l.writeLine(fmt.Sprintf("  Conn Fail   : %d", atomic.LoadInt64(&l.connFail)))
	l.writeLine("")
	l.writeLine("  Per-Protocol Breakdown:")
	l.writeLine(fmt.Sprintf("  %-6s  %6s  %6s  %7s  %9s  %9s  %9s  %8s",
		"Proto", "Tested", "Passed", "Pass%", "ParseFail", "StartFail", "ConnFail", "AvgLat"))

	for _, p := range cfg.ProtocolOrder {
		st := l.protoStats[p]
		if st == nil {
			continue
		}
		passRate := 0.0
		avgLat := int64(0)
		if st.tested > 0 {
			passRate = float64(st.passed) / float64(st.tested) * 100
		}
		if st.passed > 0 {
			avgLat = st.totalLatMs / int64(st.passed)
		}
		l.writeLine(fmt.Sprintf("  %-6s  %6d  %6d  %6.1f%%  %9d  %9d  %9d  %7dms",
			p, st.tested, st.passed, passRate, st.parseFail, st.startFail, st.connFail, avgLat))
	}

	tt := atomic.LoadInt64(&l.totalTest)
	if tt > 0 {
		overall := float64(atomic.LoadInt64(&l.passed)) / float64(tt) * 100
		l.writeLine(fmt.Sprintf("\n  Overall pass rate: %.1f%%", overall))
	}

	if len(failedLinks) > 0 {
		l.writeLine("\n  Failed Sources:")
		for _, fl := range failedLinks {
			l.writeLine("    - " + fl)
		}
	}

	l.writeLine("\n  Output Files:")
	for _, p := range cfg.ProtocolOrder {
		if n := byProto[p]; n > 0 {
			l.writeLine(fmt.Sprintf("    %-6s: %d → %s/%s.txt | %s/%s_clash.yaml | %s/%s_clash_advanced.yaml",
				p, n, cfg.Output.ProtocolsDir, p, cfg.Output.ProtocolsDir, p, cfg.Output.ProtocolsDir, p))
		}
	}
	l.writeLine(fmt.Sprintf("  Total  : %d → %s | clash.yaml | clash_advanced.yaml", len(results), cfg.Output.MainFile))
	l.writeLine("==========================================================")
}

func (l *Logger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buf != nil {
		l.buf.Flush()
	}
	if l.file != nil {
		l.file.Close()
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
