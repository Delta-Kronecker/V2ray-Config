package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ── TCP ping pre-check ────────────────────────────────────────────────────────
// Extracts the remote host:port from a proxy URI, does a raw TCP dial.
// Returns true if the endpoint is reachable.

func tcpPingConfig(configURL, protocol string, timeout time.Duration, retries int) bool {
	host, portNum := extractHostPort(configURL, protocol)
	if host == "" || portNum == 0 {
		// Can't extract — let it pass through to full validation
		return true
	}
	addr := fmt.Sprintf("%s:%d", host, portNum)
	for attempt := 0; attempt <= retries; attempt++ {
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

// extractHostPort gets the server host and port from a proxy URI without full parsing.
func extractHostPort(configURL, protocol string) (string, int) {
	switch protocol {
	case "vmess":
		// VMess can be base64-JSON or URI format — use the parser to get server/port
		outbound, parseErr := parseVMess(configURL)
		if parseErr != "" {
			return "", 0
		}
		return extractFromXrayJSON(outbound)

	case "ssr":
		// SSR: host is parts[0], port is parts[1] of decoded body
		trimmed := strings.TrimPrefix(configURL, "ssr://")
		if idx := strings.LastIndex(trimmed, "#"); idx != -1 {
			trimmed = trimmed[:idx]
		}
		decoded, err := decodeBase64([]byte(strings.TrimSpace(trimmed)))
		if err != nil {
			return "", 0
		}
		body := decoded
		if i := strings.Index(decoded, "/?"); i != -1 {
			body = decoded[:i]
		} else if i := strings.Index(decoded, "?"); i != -1 {
			body = decoded[:i]
		}
		parts := strings.SplitN(body, ":", 6)
		if len(parts) < 2 {
			return "", 0
		}
		port, err := toPort(parts[1])
		if err != nil {
			return "", 0
		}
		return parts[0], port

	case "hy2":
		trimmed := strings.TrimPrefix(configURL, "hy2://")
		if i := strings.LastIndex(trimmed, "#"); i != -1 {
			trimmed = trimmed[:i]
		}
		if i := strings.Index(trimmed, "?"); i != -1 {
			trimmed = trimmed[:i]
		}
		lastAt := strings.LastIndex(trimmed, "@")
		if lastAt == -1 {
			return "", 0
		}
		hostPort := trimmed[lastAt+1:]
		if i := strings.Index(hostPort, "/"); i != -1 {
			hostPort = hostPort[:i]
		}
		lastColon := strings.LastIndex(hostPort, ":")
		if lastColon == -1 {
			return hostPort, 443
		}
		port, err := toPort(hostPort[lastColon+1:])
		if err != nil {
			return "", 0
		}
		return hostPort[:lastColon], port

	default:
		// vless, trojan, ss, hy, tuic — standard URI host:port
		u, err := url.Parse(sanitizeProxyURL(configURL))
		if err != nil || u.Hostname() == "" {
			return "", 0
		}
		portStr := u.Port()
		if portStr == "" {
			portStr = "443"
		}
		port, err := toPort(portStr)
		if err != nil {
			return "", 0
		}
		return u.Hostname(), port
	}
}

// extractFromXrayJSON extracts server/server_port from a xray outbound JSON string.
func extractFromXrayJSON(jsonStr string) (string, int) {
	// Quick substring extraction — avoid full JSON unmarshal for speed
	getStr := func(key string) string {
		needle := `"` + key + `":"`
		idx := strings.Index(jsonStr, needle)
		if idx == -1 {
			return ""
		}
		start := idx + len(needle)
		end := strings.Index(jsonStr[start:], `"`)
		if end == -1 {
			return ""
		}
		return jsonStr[start : start+end]
	}
	getInt := func(key string) int {
		needle := `"` + key + `":`
		idx := strings.Index(jsonStr, needle)
		if idx == -1 {
			return 0
		}
		start := idx + len(needle)
		end := start
		for end < len(jsonStr) && (jsonStr[end] >= '0' && jsonStr[end] <= '9') {
			end++
		}
		if end == start {
			return 0
		}
		port, err := toPort(jsonStr[start:end])
		if err != nil {
			return 0
		}
		return port
	}
	return getStr("server"), getInt("server_port")
}

// ── tcpPingBatch ─────────────────────────────────────────────────────────────
// Runs TCP ping on a batch of configs and returns only those that passed.

func tcpPingBatch(lines []string, proto string, timeout time.Duration, retries, workers int) (passed []string, failed int) {
	if workers <= 0 {
		workers = 500 // default
	}
	if workers > len(lines) {
		workers = len(lines)
	}

	type job struct {
		idx  int
		line string
	}
	type result struct {
		idx  int
		line string
		ok   bool
	}

	jobs := make(chan job, len(lines))
	resultsCh := make(chan result, len(lines))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				ok := tcpPingConfig(j.line, proto, timeout, retries)
				resultsCh <- result{idx: j.idx, line: j.line, ok: ok}
			}
		}()
	}

	for i, line := range lines {
		jobs <- job{idx: i, line: line}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	ordered := make([]result, len(lines))
	for r := range resultsCh {
		ordered[r.idx] = r
	}

	for _, r := range ordered {
		if r.ok {
			passed = append(passed, r.line)
		} else {
			failed++
		}
	}
	return
}

// ── validateAll ───────────────────────────────────────────────────────────────

var gInputByProto = make(map[string]int)

func validateAll(lines []string) ([]configResult, []string) {
	byProto, duplicates := dedup(lines)

	for p, ls := range byProto {
		gInputByProto[p] = len(ls)
	}

	if gLog != nil {
		gLog.writeLine(fmt.Sprintf("[DEDUP] removed=%d duplicates", duplicates))
		total := 0
		for _, p := range cfg.ProtocolOrder {
			n := len(byProto[p])
			total += n
			if n > 0 {
				gLog.writeLine(fmt.Sprintf("[DEDUP] %-6s unique=%d", p, n))
			}
		}
		gLog.writeLine(fmt.Sprintf("[DEDUP] total unique=%d", total))
		gLog.writeLine("")
	}

	protoFails := make(map[string]*failDetail)
	for _, p := range cfg.ProtocolOrder {
		protoFails[p] = &failDetail{reasons: make(map[string]int), samples: make(map[string][]string)}
	}

	var testedCount int64
	var passedCount int64
	var failedParse int64
	var failedStart int64
	var failedConn int64
	var out []configResult
	var tcpPingPassedAll []string
	var tcpAllFailedMu sync.Mutex

	v := cfg.Validation
	batchSize := v.NumWorkers
	if batchSize <= 0 {
		batchSize = 50
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// TCP ping settings
	tcpTimeout := time.Duration(v.TCPPingTimeoutMs) * time.Millisecond
	if tcpTimeout <= 0 {
		tcpTimeout = 3000 * time.Millisecond
	}
	tcpRetries := v.TCPPingRetries
	if tcpRetries < 0 {
		tcpRetries = 0
	}
	tcpWorkers := v.TCPPingWorkers
	if tcpWorkers <= 0 {
		tcpWorkers = 500
	}
	tcpBatchSize := v.TCPPingBatchSize
	if tcpBatchSize <= 0 {
		tcpBatchSize = 0 // 0 = no batching, run all at once
	}
	tcpBatchRest := time.Duration(v.TCPPingBatchRestMs) * time.Millisecond

	for _, proto := range cfg.ProtocolOrder {
		protoLines := byProto[proto]
		if len(protoLines) == 0 {
			continue
		}

		// Shuffle before processing (same as original)
		rng.Shuffle(len(protoLines), func(i, j int) {
			protoLines[i], protoLines[j] = protoLines[j], protoLines[i]
		})

		// ── Phase 1: TCP ping pre-check ───────────────────────────────────────
		effBatchSize := tcpBatchSize
		if effBatchSize <= 0 || effBatchSize >= len(protoLines) {
			effBatchSize = len(protoLines) // single pass
		}
		numPingBatches := (len(protoLines) + effBatchSize - 1) / effBatchSize

		fmt.Printf("\n🔌 [%s] TCP ping pre-check — %d configs | batches=%d batch_size=%d workers=%d timeout=%dms\n",
			strings.ToUpper(proto), len(protoLines), numPingBatches, effBatchSize, tcpWorkers, v.TCPPingTimeoutMs)

		pingAllStart := time.Now()
		var pingPassed []string
		pingTotalFailed := 0

		for pb := 0; pb < numPingBatches; pb++ {
			pbStart := pb * effBatchSize
			pbEnd := pbStart + effBatchSize
			if pbEnd > len(protoLines) {
				pbEnd = len(protoLines)
			}
			pbLines := protoLines[pbStart:pbEnd]

			t0 := time.Now()
			pbPassed, pbFailed := tcpPingBatch(pbLines, proto, tcpTimeout, tcpRetries, tcpWorkers)
			pingPassed = append(pingPassed, pbPassed...)
			pingTotalFailed += pbFailed

			fmt.Printf("   📡 Ping batch %d/%d  ✅%d ❌%d  in %.1fs\n",
				pb+1, numPingBatches, len(pbPassed), pbFailed, time.Since(t0).Seconds())

			if pb < numPingBatches-1 && tcpBatchRest > 0 {
				time.Sleep(tcpBatchRest)
			}
		}

		pingElapsed := time.Since(pingAllStart).Seconds()
		fmt.Printf("   ✅ TCP ping total: passed=%d  skipped=%d  in %.1fs\n",
			len(pingPassed), pingTotalFailed, pingElapsed)
		if gLog != nil {
			gLog.writeLine(fmt.Sprintf("[TCP_PING] proto=%-6s total=%d passed=%d failed=%d batches=%d elapsed=%.1fs",
				proto, len(protoLines), len(pingPassed), pingTotalFailed, numPingBatches, pingElapsed))
		}

		// Track lines that passed TCP ping — used later to find xray failures
		tcpAllFailedMu.Lock()
		tcpPingPassedAll = append(tcpPingPassedAll, pingPassed...)
		tcpAllFailedMu.Unlock()

		if len(pingPassed) == 0 {
			fmt.Printf("⚠️  [%s] No configs passed TCP ping — skipping full validation\n", strings.ToUpper(proto))
			continue
		}

		// ── Phase 2: Full xray validation (with retry) ─────────────────────────
		protoTotal := len(pingPassed)
		protoStart := time.Now()
		var protoPassed int64
		maxRetries := v.ValidationRetries
		if maxRetries < 0 {
			maxRetries = 0
		}

		passedSet := make(map[string]bool)
		failedSet := make(map[string]bool)
		currentConfigs := pingPassed

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if len(currentConfigs) == 0 {
				break
			}
			totalBatches := (len(currentConfigs) + batchSize - 1) / batchSize

			if attempt == 0 {
				fmt.Printf("🔵 [%s] Full validation — %d configs in %d batches of %d\n",
					strings.ToUpper(proto), len(currentConfigs), totalBatches, batchSize)
			} else {
				fmt.Printf("🔄 [%s] Retry %d — %d configs in %d batches of %d\n",
					strings.ToUpper(proto), attempt, len(currentConfigs), totalBatches, batchSize)
			}

			if gLog != nil && attempt == 0 {
				gLog.logProtoStart(proto, len(currentConfigs))
			}

			var nextRetryConfigs []string

			for batchIdx := 0; batchIdx < totalBatches; batchIdx++ {
				start := batchIdx * batchSize
				end := start + batchSize
				if end > len(currentConfigs) {
					end = len(currentConfigs)
				}
				batch := currentConfigs[start:end]
				actualBatchSize := len(batch)

				localPorts := make(chan int, actualBatchSize)
				for i := 0; i < actualBatchSize; i++ {
					localPorts <- v.BasePort + i
				}

				bt := &batchTracker{}

				type workerResult struct {
					line string
					res  validationResult
				}
				workerResults := make([]workerResult, actualBatchSize)

				var wg sync.WaitGroup
				batchStart := time.Now()

			for i, line := range batch {
				wg.Add(1)
				go func(idx int, l string, urlIdx int) {
					defer wg.Done()
					globalIdx := atomic.AddInt64(&testedCount, 1)
					res := validateWithTracker(l, proto, localPorts, bt, urlIdx)
					if gLog != nil {
						gLog.logResult(globalIdx, proto, l, res)
					}
					workerResults[idx] = workerResult{line: l, res: res}
				}(i, line, attempt)
				}

				wg.Wait()

				bt.killAll()
				if v.ProcessKillWaitMs > 0 {
					time.Sleep(time.Duration(v.ProcessKillWaitMs) * time.Millisecond)
				}

				procsAfter := countXrayProcs()
				occupiedAfter := checkOccupiedPorts(v.BasePort, actualBatchSize)

				if procsAfter > 0 || len(occupiedAfter) > 0 {
					fmt.Printf("     ⚠️  After kill  — procs:%-3d  ports-busy:%-3d\n",
						procsAfter, len(occupiedAfter))
					if len(occupiedAfter) > 0 && len(occupiedAfter) <= 20 {
						fmt.Printf("     ⚠️  Still-busy ports: %v\n", occupiedAfter)
					}
				}

				var bPassed, bFailed, bParse, bStart, bConn int

				for _, wr := range workerResults {
					res := wr.res
					if res.passed {
						if !passedSet[wr.line] {
							passedSet[wr.line] = true
							delete(failedSet, wr.line)
							atomic.AddInt64(&passedCount, 1)
							atomic.AddInt64(&protoPassed, 1)
							out = append(out, configResult{line: wr.line, proto: proto})
						}
						bPassed++
					} else {
						bFailed++
						if !failedSet[wr.line] {
							failedSet[wr.line] = true
							reason := res.failReason
							norm := classifyFailReason(reason)
							fd := protoFails[proto]
							fd.mu.Lock()
							fd.reasons[norm]++
							if len(fd.samples[norm]) < 100 {
								fd.samples[norm] = append(fd.samples[norm], wr.line)
							}
							fd.mu.Unlock()

							if strings.HasPrefix(reason, "PARSE:") {
								atomic.AddInt64(&failedParse, 1)
							} else if strings.HasPrefix(reason, "XRAY_START:") || strings.HasPrefix(reason, "START:") {
								atomic.AddInt64(&failedStart, 1)
							} else {
								atomic.AddInt64(&failedConn, 1)
							}
						}
						if strings.HasPrefix(res.failReason, "PARSE:") {
							bParse++
						} else if strings.HasPrefix(res.failReason, "XRAY_START:") || strings.HasPrefix(res.failReason, "START:") {
							bStart++
							if attempt < maxRetries {
								nextRetryConfigs = append(nextRetryConfigs, wr.line)
							}
						} else {
							bConn++
							if attempt < maxRetries {
								nextRetryConfigs = append(nextRetryConfigs, wr.line)
							}
						}
					}
				}

				batchElapsed := time.Since(batchStart).Seconds()
				batchPassRate := 0.0
				if actualBatchSize > 0 {
					batchPassRate = float64(bPassed) / float64(actualBatchSize) * 100
				}
				totalDone := (batchIdx + 1) * batchSize
				if totalDone > len(currentConfigs) {
					totalDone = len(currentConfigs)
				}

				fmt.Printf("  📦 Batch %d/%d [%d configs]  ✅%d ❌%d  Rate:%.1f%%  Time:%.1fs\n",
					batchIdx+1, totalBatches, actualBatchSize, bPassed, bFailed, batchPassRate, batchElapsed)
				fmt.Printf("     Parse✗:%-5d  Start✗:%-5d  Conn✗:%-5d  Total:%d/%d\n",
					bParse, bStart, bConn, totalDone, len(currentConfigs))

				if batchIdx < totalBatches-1 && v.BatchRestMs > 0 {
					fmt.Printf("     💤 %dms rest...\n", v.BatchRestMs)
					time.Sleep(time.Duration(v.BatchRestMs) * time.Millisecond)
				}
			}

			currentConfigs = nextRetryConfigs
		}

		protoElapsed := time.Since(protoStart).Seconds()
		protoPassRate := float64(protoPassed) / float64(protoTotal) * 100
		fmt.Printf("✅ [%s] Done — passed=%d/%d (%.1f%%) in %.1fs\n",
			strings.ToUpper(proto), protoPassed, protoTotal, protoPassRate, protoElapsed)
	}

	fmt.Printf("\n📊 Tested=%d | Passed=%d | ParseFail=%d | StartFail=%d | ConnFail=%d\n",
		atomic.LoadInt64(&testedCount),
		atomic.LoadInt64(&passedCount),
		atomic.LoadInt64(&failedParse),
		atomic.LoadInt64(&failedStart),
		atomic.LoadInt64(&failedConn))

	printFailureReport(protoFails, func() map[string][]string {
		m := make(map[string][]string)
		for p, ls := range byProto {
			m[p] = ls
		}
		return m
	}())

	// tcpPass = ALL configs that passed TCP ping (regardless of xray result)
	return out, tcpPingPassedAll
}

// ── validateWithTracker ───────────────────────────────────────────────────────

func validateWithTracker(configURL, protocol string, localPorts chan int, bt *batchTracker, testURLIdx int) validationResult {
	var result validationResult

	outboundJSON, parseErr := toXrayOutbound(configURL, protocol)
	if parseErr != "" {
		result.failReason = "PARSE: " + parseErr
		return result
	}

	port := <-localPorts
	defer func() { localPorts <- port }()

	v := cfg.Validation
	fullConfig := buildXrayConfig(outboundJSON, port)

	configFile, err := os.CreateTemp("", "sb-*.json")
	if err != nil {
		result.failReason = "FILE: " + err.Error()
		return result
	}
	configPath := configFile.Name()
	configFile.Close()

	if err := os.WriteFile(configPath, []byte(fullConfig), 0644); err != nil {
		os.Remove(configPath)
		result.failReason = "FILE: " + err.Error()
		return result
	}
	defer os.Remove(configPath)

	timeoutSec := v.GlobalTimeoutSec
	if protocol == "vless" && v.VlessSpecificTimeoutMs > 0 {
		timeoutSec = float64(v.VlessSpecificTimeoutMs) / 1000.0
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(float64(time.Second)*(timeoutSec+2)))
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, xrayPath(), "run", "-c", configPath)
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		result.failReason = "START: " + err.Error()
		return result
	}

	bt.register(cmd)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	started := waitForPort(addr,
		time.Duration(v.XrayStartTimeoutMs)*time.Millisecond,
		time.Duration(v.XrayStartIntervalMs)*time.Millisecond,
		time.Duration(v.PortCheckTimeoutMs)*time.Millisecond,
	)

	if !started {
		killGroup(cmd)
		sbErr := extractErrVerbose(stderr.String())
		if sbErr == "" {
			sbErr = fmt.Sprintf("port not open after %dms", v.XrayStartTimeoutMs)
		}
		result.failReason = "XRAY_START: " + sbErr
		return result
	}

	testURL := v.TestURLs[0]
	if testURLIdx >= 0 && testURLIdx < len(v.TestURLs) {
		testURL = v.TestURLs[testURLIdx]
	}

	// First: TCP handshake to proxy port (fast check)
	handshakeTimeout := time.Duration(v.HTTPDialTimeoutMs) * time.Millisecond
	conn, dialErr := net.DialTimeout("tcp", addr, handshakeTimeout)
	if dialErr != nil {
		killGroup(cmd)
		sbErr := extractErrVerbose(stderr.String())
		if sbErr == "" {
			sbErr = fmt.Sprintf("handshake failed: %s", dialErr.Error())
		}
		result.failReason = "CONN: " + sbErr
		return result
	}
	conn.Close()

	// Second: HTTP test through proxy (full validation)
	httpTimeout := v.HTTPRequestTimeoutMs
	if protocol == "vless" && v.VlessSpecificTimeoutMs > 0 {
		httpTimeout = v.VlessSpecificTimeoutMs
	}

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{
		Timeout: time.Duration(httpTimeout) * time.Millisecond,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(v.HTTPDialTimeoutMs) * time.Millisecond,
				KeepAlive: 0,
			}).DialContext,
			MaxIdleConns:          1,
			MaxIdleConnsPerHost:   1,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: time.Duration(v.HTTPResponseTimeoutMs) * time.Millisecond,
		},
	}

	success, latency, httpErr := tryHTTP(ctx, client, []string{testURL}, v.MaxRetries)
	killGroup(cmd)

	if success {
		result.passed = true
		result.latency = latency
	} else {
		sbErr := extractErrVerbose(stderr.String())
		if sbErr != "" {
			result.failReason = "CONN: " + httpErr + " | SB:" + sbErr
		} else {
			result.failReason = "CONN: " + httpErr
		}
	}
	return result
}

// ── waitForPort ───────────────────────────────────────────────────────────────

func waitForPort(addr string, maxWait, interval, dialTimeout time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for {
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err == nil {
			conn.Close()
			return true
		}
		if time.Now().Add(interval).After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

// ── tryHTTP ───────────────────────────────────────────────────────────────────

func tryHTTP(ctx context.Context, client *http.Client, testURLs []string, maxRetries int) (bool, time.Duration, string) {
	effectiveURLs := make([]string, 0, len(testURLs))
	seen := make(map[string]bool)
	for _, u := range testURLs {
		if !seen[u] {
			effectiveURLs = append(effectiveURLs, u)
			seen[u] = true
		}
	}
	var lastErr string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return false, 0, "context expired"
		}
		for _, testURL := range effectiveURLs {
			if ctx.Err() != nil {
				return false, 0, "context expired"
			}
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
			if err != nil {
				lastErr = err.Error()
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				e := shortenErr(err.Error())
				lastErr = e
				if strings.Contains(e, "connection refused") ||
					strings.Contains(e, "connection reset") ||
					strings.Contains(e, "no route to host") ||
					strings.Contains(e, "network unreachable") {
					return false, 0, lastErr
				}
				continue
			}
			latency := time.Since(start)
			code := resp.StatusCode
			resp.Body.Close()

			if code == 200 || code == 204 {
				return true, latency, ""
			}
			if code == 301 || code == 302 || code == 307 || code == 308 {
				return true, latency, ""
			}
			if code == 400 || code == 403 || code == 404 || code == 429 {
				return true, latency, ""
			}
			lastErr = fmt.Sprintf("HTTP_%d", code)
		}
	}
	return false, 0, lastErr
}

// ── buildXrayConfig ──────────────────────────────────────────────────────────

func buildXrayConfig(outboundJSON string, port int) string {
	return fmt.Sprintf(`{"log":{"loglevel":"warning"},"dns":{"servers":["8.8.8.8","localhost"]},"inbounds":[{"port":%d,"listen":"127.0.0.1","protocol":"http","tag":"http-in"}],"outbounds":[%s,{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole","tag":"block"}],"routing":{"rules":[{"type":"field","inboundTag":["http-in"],"outboundTag":"proxy"}]}}`,
		port, outboundJSON)
}

// ── Failure classification ────────────────────────────────────────────────────

func classifyFailReason(reason string) string {
	stripANSI := func(s string) string {
		var out strings.Builder
		i := 0
		for i < len(s) {
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && !(s[j] >= 'A' && s[j] <= 'Z') && !(s[j] >= 'a' && s[j] <= 'z') {
					j++
				}
				if j < len(s) {
					j++
				}
				i = j
				continue
			}
			out.WriteByte(s[i])
			i++
		}
		return out.String()
	}
	r := stripANSI(reason)

	switch {
	case strings.HasPrefix(r, "PARSE: base64:"):
		return "PARSE › base64 decode error"
	case strings.HasPrefix(r, "PARSE: json:"):
		return "PARSE › json decode error"
	case strings.HasPrefix(r, "PARSE: url parse:"):
		return "PARSE › url parse error"
	case strings.HasPrefix(r, "PARSE: unsupported cipher:"):
		return "PARSE › unsupported SS cipher"
	case strings.HasPrefix(r, "PARSE: unsupported transport:"):
		msg := strings.TrimPrefix(r, "PARSE: unsupported transport: ")
		switch msg {
		case "xhttp", "splithttp":
			return "PARSE › unsupported transport (xhttp/splithttp)"
		default:
			return "PARSE › unsupported transport (kcp/quic/mkcp)"
		}
	case r == "PARSE: missing @" || r == "PARSE: missing server" ||
		r == "PARSE: missing uuid" || r == "PARSE: missing password" ||
		r == "PARSE: missing port" || r == "PARSE: missing auth":
		return "PARSE › " + strings.TrimPrefix(r, "PARSE: ")
	case strings.HasPrefix(r, "PARSE: port:"):
		return "PARSE › invalid port value"
	case strings.HasPrefix(r, "PARSE: reality:"):
		return "PARSE › reality missing public key"
	case strings.HasPrefix(r, "PARSE: unknown security:"):
		return "PARSE › unknown security type"
	case strings.HasPrefix(r, "PARSE:"):
		msg := strings.TrimPrefix(r, "PARSE: ")
		if len(msg) > 48 {
			msg = msg[:48] + "…"
		}
		return "PARSE › " + msg

	case strings.HasPrefix(r, "XRAY_START:"), strings.HasPrefix(r, "START:"):
		body := r
		if i := strings.Index(body, ": "); i != -1 {
			body = body[i+2:]
		}
		switch {
		case strings.Contains(body, "port not open"):
			return "START › port timeout (xray didn't listen)"
		case strings.Contains(body, "decode config"), strings.Contains(body, "outbound"):
			if strings.Contains(body, "flow") {
				return "START › invalid flow (requires TLS)"
			}
			return "START › invalid config JSON (xray rejected)"
		case strings.Contains(body, "address already in use"):
			return "START › port already in use"
		case strings.Contains(body, "no such file"), strings.Contains(body, "not found"):
			return "START › xray binary not found"
		case strings.Contains(body, "permission denied"):
			return "START › permission denied"
		case strings.Contains(body, "method"):
			return "START › unsupported SS method"
		default:
			if len(body) > 55 {
				body = body[:55] + "…"
			}
			return "START › " + body
		}

	case strings.HasPrefix(r, "CONN:"):
		body := strings.TrimPrefix(r, "CONN: ")
		if i := strings.Index(body, " | XRAY:"); i != -1 {
			body = body[:i]
		}
		if strings.HasPrefix(body, "Get ") {
			real := body
			if i := strings.Index(body, `": `); i != -1 {
				real = body[i+3:]
			} else if i := strings.LastIndex(body, ": "); i != -1 && i > 10 {
				real = body[i+2:]
			}
			body = real
		}
		switch {
		case strings.Contains(body, "context deadline exceeded"), strings.Contains(body, "context canceled"):
			return "CONN › request timed out (no response from proxy)"
		case strings.Contains(body, "connection refused"):
			return "CONN › connection refused (proxy died)"
		case body == "EOF" || strings.HasSuffix(body, ": EOF") || body == "unexpected EOF":
			return "CONN › EOF (proxy closed connection)"
		case strings.Contains(body, "EOF"):
			return "CONN › EOF (proxy closed connection)"
		case strings.Contains(body, "no such host"), strings.Contains(body, "lookup"):
			return "CONN › DNS resolution failed"
		case strings.Contains(body, "i/o timeout"):
			return "CONN › i/o timeout"
		case strings.Contains(body, "connection reset"):
			return "CONN › connection reset by peer"
		case strings.Contains(body, "no route to host"):
			return "CONN › no route to host"
		case strings.Contains(body, "network unreachable"):
			return "CONN › network unreachable"
		case strings.Contains(body, "tls:"), strings.Contains(body, "TLS"), strings.Contains(body, "certificate"):
			return "CONN › TLS handshake failed"
		case body == "HTTP_502":
			return "CONN › HTTP 502 (proxy rejected CONNECT)"
		case body == "HTTP_501":
			return "CONN › HTTP 501 (no CONNECT support)"
		case strings.Contains(body, "HTTP_"):
			return "CONN › unexpected HTTP status: " + body
		case strings.Contains(body, "proxyconnect"):
			return "CONN › proxy CONNECT failed"
		case strings.Contains(body, "context expired"):
			return "CONN › test URL timed out (proxy dead or unreachable)"
		default:
			if len(body) > 55 {
				body = body[:55] + "…"
			}
			return "CONN › " + body
		}

	case strings.HasPrefix(r, "FILE:"):
		return "OTHER › temp file error"
	default:
		if len(r) > 55 {
			r = r[:55] + "…"
		}
		return "OTHER › " + r
	}
}

// ── printFailureReport ────────────────────────────────────────────────────────

func printFailureReport(protoFails map[string]*failDetail, byProto map[string][]string) {
	type kv struct {
		key string
		val int
	}

	const W = 78

	hr := func(ch string) { fmt.Println(strings.Repeat(ch, W)) }

	fmt.Println()
	hr("═")
	title := "  FAILURE ANALYSIS REPORT"
	fmt.Printf("%-*s%s\n", W-len(title)-1, title, "")
	fmt.Printf("  %-*s\n", W-3, "Detailed breakdown of why each config failed, grouped by root cause.")
	hr("═")

	type protoRow struct {
		name      string
		total     int
		passed    int
		parseFail int
		startFail int
		connFail  int
		otherFail int
	}
	var rows []protoRow

	for _, proto := range cfg.ProtocolOrder {
		fd := protoFails[proto]
		if fd == nil {
			continue
		}
		total := len(byProto[proto])
		if total == 0 {
			continue
		}

		var pf, sf, cf, of int
		for key, cnt := range fd.reasons {
			switch {
			case strings.HasPrefix(key, "PARSE"):
				pf += cnt
			case strings.HasPrefix(key, "START"):
				sf += cnt
			case strings.HasPrefix(key, "CONN"):
				cf += cnt
			default:
				of += cnt
			}
		}
		totalFail := pf + sf + cf + of
		rows = append(rows, protoRow{proto, total, total - totalFail, pf, sf, cf, of})
	}

	fmt.Println()
	fmt.Printf("  %-7s %7s %7s %6s  %9s %9s %9s %8s  %s\n",
		"PROTO", "TOTAL", "PASSED", "PASS%", "PARSE✗", "START✗", "CONN✗", "OTHER✗", "PASS-RATE BAR")
	fmt.Println("  " + strings.Repeat("─", W-2))
	for _, row := range rows {
		passRate := float64(row.passed) / float64(row.total) * 100
		barLen := int(passRate / 5)
		if barLen > 20 {
			barLen = 20
		}
		if barLen < 0 {
			barLen = 0
		}
		bar := strings.Repeat("▓", barLen) + strings.Repeat("░", 20-barLen)
		fmt.Printf("  %-7s %7d %7d %5.1f%%  %9d %9d %9d %8d  %s\n",
			strings.ToUpper(row.name),
			row.total, row.passed, passRate,
			row.parseFail, row.startFail, row.connFail, row.otherFail,
			bar)
	}
	fmt.Println()

	for _, proto := range cfg.ProtocolOrder {
		fd := protoFails[proto]
		if fd == nil {
			continue
		}
		total := len(byProto[proto])
		if total == 0 {
			continue
		}

		totalFails := 0
		for _, c := range fd.reasons {
			totalFails += c
		}
		passed := total - totalFails
		passRate := float64(passed) / float64(total) * 100

		fmt.Printf("┌─ %-6s ─────────────────────────────────────────────────────────────\n",
			strings.ToUpper(proto))
		fmt.Printf("│  Total: %-6d  Passed: %-6d  Failed: %-6d  Pass rate: %.1f%%\n",
			total, passed, totalFails, passRate)

		if totalFails == 0 {
			fmt.Println("│  ✓ No failures recorded.")
			fmt.Println("└" + strings.Repeat("─", W-1))
			continue
		}

		sections := []struct{ prefix, label string }{
			{"PARSE", "Parse Failures  (config could not be decoded/interpreted)"},
			{"START", "Start Failures  (xray refused or couldn't start)"},
			{"CONN", "Conn Failures   (proxy started but connection failed)"},
			{"OTHER", "Other / Unknown"},
		}

		for _, sec := range sections {
			var items []kv
			secTotal := 0
			for k, v := range fd.reasons {
				if strings.HasPrefix(k, sec.prefix) {
					items = append(items, kv{k, v})
					secTotal += v
				}
			}
			if len(items) == 0 {
				continue
			}
			sort.Slice(items, func(i, j int) bool { return items[i].val > items[j].val })

			secPct := float64(secTotal) / float64(totalFails) * 100
			fmt.Printf("│\n│  ▶ %s\n", sec.label)
			fmt.Printf("│    Sub-total: %d configs (%.1f%% of all failures)\n", secTotal, secPct)
			fmt.Printf("│    %-52s %7s  %6s  %s\n", "Reason", "Count", "of-sec", "Bar")
			fmt.Printf("│    %s\n", strings.Repeat("·", 72))

			for _, item := range items {
				pct := float64(item.val) / float64(secTotal) * 100
				barLen := int(pct / 5)
				if barLen > 20 {
					barLen = 20
				}
				bar := strings.Repeat("█", barLen)

				displayKey := item.key
				if i := strings.Index(displayKey, " › "); i != -1 {
					displayKey = displayKey[i+3:]
				}
				if len(displayKey) > 51 {
					displayKey = displayKey[:51] + "…"
				}

				fmt.Printf("│    %-52s %7d  %5.1f%%  %s\n",
					displayKey, item.val, pct, bar)

				if samples := fd.samples[item.key]; len(samples) > 0 {
					fmt.Printf("│    ┌─ SAMPLE CONFIGS (%d) ──────────────────────────────────────────\n", len(samples))
					for i, s := range samples {
						if len(s) > 140 {
							s = s[:140] + "…"
						}
						fmt.Printf("│    │ [%3d] %s\n", i+1, s)
					}
					fmt.Printf("│    └──────────────────────────────────────────────────────────────\n")
				}
			}
		}

		fmt.Println("└" + strings.Repeat("─", W-1))
	}

	var grandTotal, grandPassed, grandFail int
	for _, row := range rows {
		grandTotal += row.total
		grandPassed += row.passed
		grandFail += row.total - row.passed
	}
	fmt.Println()
	hr("═")
	if grandTotal > 0 {
		fmt.Printf("  OVERALL  Total=%-7d  Passed=%-7d  Failed=%-7d  Pass rate=%.1f%%\n",
			grandTotal, grandPassed, grandFail,
			float64(grandPassed)/float64(grandTotal)*100)
	}
	hr("═")
	fmt.Println()
}
