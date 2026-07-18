package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Sub file loader ───────────────────────────────────────────────────────────

func loadSubsFromFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls
}

// ── fetchAll (legacy base64/text links) ──────────────────────────────────────

func fetchAll(base64Links, textLinks []string) ([]string, []string) {
	const batchSize = 20
	const fetchTimeout = 5 * time.Second

	retryCount := cfg.Validation.FetchRetryCount
	retryDelay := time.Duration(cfg.Validation.FetchRetryDelayMs) * time.Millisecond
	if retryCount < 0 {
		retryCount = 0
	}

	type urlJob struct {
		url      string
		isBase64 bool
	}

	var jobs []urlJob
	for _, u := range base64Links {
		jobs = append(jobs, urlJob{u, true})
	}
	for _, u := range textLinks {
		jobs = append(jobs, urlJob{u, false})
	}

	total := len(jobs)
	numBatches := (total + batchSize - 1) / batchSize
	fmt.Printf("📡 Fetching %d sources in %d batches of %d (timeout=%s  retries=%d)\n",
		total, numBatches, batchSize, fetchTimeout, retryCount)

	var mu sync.Mutex
	var lines []string
	var failed []string
	var okCount, failCount int

	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := jobs[start:end]

		var wg sync.WaitGroup
		results := make([]fetchResult, len(batch))

		for i, job := range batch {
			wg.Add(1)
			go func(idx int, j urlJob) {
				defer wg.Done()
				var r fetchResult
				for attempt := 0; attempt <= retryCount; attempt++ {
					if attempt > 0 && retryDelay > 0 {
						time.Sleep(retryDelay)
					}
					r = fetchRaw(j.url, fetchTimeout)
					if r.err == nil && r.statusCode == http.StatusOK {
						break
					}
				}
				if r.err == nil && r.statusCode == http.StatusOK && j.isBase64 {
					decoded, err := decodeBase64([]byte(r.content))
					if err != nil {
						r.err = err
					} else {
						r.content = decoded
					}
				}
				results[idx] = r
			}(i, job)
		}
		wg.Wait()

		mu.Lock()
		for _, r := range results {
			if r.err != nil || r.statusCode != http.StatusOK {
				status := "error"
				if r.statusCode > 0 {
					status = fmt.Sprintf("HTTP %d", r.statusCode)
				}
				failCount++
				failed = append(failed, fmt.Sprintf("%s (%s)", r.url, status))
				if gLog != nil {
					gLog.writeLine(fmt.Sprintf("[FETCH] FAIL  %s  status=%s", r.url, status))
				}
				continue
			}
			okCount++
			extracted := smartDecode(r.content)
			if gLog != nil {
				gLog.writeLine(fmt.Sprintf("[FETCH] OK    %s  lines=%d", r.url, len(extracted)))
			}
			lines = append(lines, extracted...)
		}
		mu.Unlock()
	}
	fmt.Printf("  ✅ Fetch done — ok=%d  fail=%d  total_lines=%d\n", okCount, failCount, len(lines))
	return lines, failed
}

// ── fetchAllFromSubs (sub.txt mode) ──────────────────────────────────────────

func fetchAllFromSubs(subURLs []string) ([]string, []string) {
	const batchSize = 20
	const fetchTimeout = 8 * time.Second

	retryCount := cfg.Validation.FetchRetryCount
	retryDelay := time.Duration(cfg.Validation.FetchRetryDelayMs) * time.Millisecond
	if retryCount < 0 {
		retryCount = 0
	}
	if retryDelay < 0 {
		retryDelay = 0
	}

	total := len(subURLs)
	numBatches := (total + batchSize - 1) / batchSize
	fmt.Printf("📡 Fetching %d sources in %d batches (timeout=%s  retries=%d)\n",
		total, numBatches, fetchTimeout, retryCount)

	var mu sync.Mutex
	var lines []string
	var failed []string
	var okCount, failCount int

	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := subURLs[start:end]

		var wg sync.WaitGroup
		type batchResult struct {
			url    string
			lines  []string
			err    error
			status int
		}
		results := make([]batchResult, len(batch))

		for i, u := range batch {
			wg.Add(1)
			go func(idx int, rawURL string) {
				defer wg.Done()
				var fr fetchResult
				for attempt := 0; attempt <= retryCount; attempt++ {
					if attempt > 0 && retryDelay > 0 {
						time.Sleep(retryDelay)
					}
					fr = fetchRaw(rawURL, fetchTimeout)
					if fr.err == nil && fr.statusCode == http.StatusOK {
						break
					}
				}
				if fr.err != nil || fr.statusCode != http.StatusOK {
					results[idx] = batchResult{url: rawURL, err: fr.err, status: fr.statusCode}
					return
				}
				extracted := smartDecode(fr.content)
				results[idx] = batchResult{url: rawURL, lines: extracted, status: fr.statusCode}
			}(i, u)
		}
		wg.Wait()

		mu.Lock()
		for _, r := range results {
			if r.err != nil || r.status != http.StatusOK {
				status := "error"
				if r.status > 0 {
					status = fmt.Sprintf("HTTP %d", r.status)
				}
				failCount++
				failed = append(failed, fmt.Sprintf("%s (%s)", r.url, status))
				if gLog != nil {
					gLog.writeLine(fmt.Sprintf("[FETCH] FAIL  %s  status=%s", r.url, status))
				}
				continue
			}
			okCount++
			if gLog != nil {
				gLog.writeLine(fmt.Sprintf("[FETCH] OK    %s  lines=%d", r.url, len(r.lines)))
			}
			lines = append(lines, r.lines...)
		}
		mu.Unlock()
	}
	fmt.Printf("  ✅ Fetch done — ok=%d  fail=%d  total_lines=%d\n", okCount, failCount, len(lines))
	return lines, failed
}

// ── fetchRaw ──────────────────────────────────────────────────────────────────

func fetchRaw(rawURL string, timeout time.Duration) fetchResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return fetchResult{url: rawURL, err: err}
	}
	resp, err := fetchHTTPClient.Do(req)
	if err != nil {
		return fetchResult{url: rawURL, err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fetchResult{url: rawURL, statusCode: resp.StatusCode, err: err}
	}
	return fetchResult{url: rawURL, statusCode: resp.StatusCode, content: string(body)}
}

// ── smartDecode ───────────────────────────────────────────────────────────────

func smartDecode(content string) []string {
	trimmed := strings.TrimSpace(content)
	if isClashYAML(trimmed) {
		if lines := parseClashYAML(trimmed); len(lines) > 0 {
			return lines
		}
	}
	if hasProtoPrefix(trimmed) {
		return extractLines(trimmed)
	}
	if isLikelyBase64(trimmed) {
		if decoded, err := decodeBase64([]byte(trimmed)); err == nil {
			decoded = strings.TrimSpace(decoded)
			if isClashYAML(decoded) {
				if lines := parseClashYAML(decoded); len(lines) > 0 {
					return lines
				}
			}
			if hasProtoPrefix(decoded) {
				return extractLines(decoded)
			}
			return extractLines(decoded)
		}
	}
	lines := extractLines(trimmed)
	var result []string
	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)
		if hasProtoPrefix(lineTrimmed) {
			result = append(result, lineTrimmed)
			continue
		}
		if isLikelyBase64(lineTrimmed) {
			if decoded, err := decodeBase64([]byte(lineTrimmed)); err == nil {
				for _, dl := range extractLines(decoded) {
					if hasProtoPrefix(dl) || dl != "" {
						result = append(result, dl)
					}
				}
				continue
			}
		}
		result = append(result, lineTrimmed)
	}
	return result
}

// ── Clash YAML parsing (used by smartDecode) ──────────────────────────────────

func isClashYAML(content string) bool {
	limit := len(content)
	if limit > 8192 {
		limit = 8192
	}
	head := content[:limit]
	for _, line := range strings.Split(head, "\n") {
		t := strings.TrimSpace(line)
		switch t {
		case "proxies:", "Proxies:", "proxy:", "Proxy:":
			return true
		}
		if strings.HasPrefix(t, "proxies:") || strings.HasPrefix(t, "Proxy:") {
			return true
		}
	}
	for _, marker := range []string{
		"type: vmess", "type: vless", "type: trojan",
		"type: ss\n", "type: ss\r", "type: ssr",
		"type: hysteria2", "type: hysteria\n", "type: hysteria\r",
		"type: tuic",
	} {
		if strings.Contains(head, marker) {
			return true
		}
	}
	return false
}

func parseClashYAML(content string) []string {
	var wrapper clashConfigWrapper
	if err := yaml.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil
	}

	proxies := wrapper.Proxies
	if len(proxies) == 0 {
		proxies = wrapper.ProxiesOld
	}
	if len(proxies) == 0 {
		proxies = wrapper.ProxiesP
	}
	if len(proxies) == 0 {
		return nil
	}

	var lines []string
	for _, p := range proxies {
		uri := clashProxyToURI(p)
		if uri != "" {
			lines = append(lines, uri)
		}
	}
	return lines
}

// ── dedup ─────────────────────────────────────────────────────────────────────

func dedup(lines []string) (map[string][]string, int) {
	seen := make(map[string]bool)
	byProto := make(map[string][]string)
	duplicates := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, proto := range cfg.Protocols {
			if strings.HasPrefix(line, proto+"://") {
				id := coreIdentity(line, proto)
				if !seen[id] {
					seen[id] = true
					byProto[proto] = append(byProto[proto], line)
				} else {
					duplicates++
				}
				break
			}
		}
	}
	return byProto, duplicates
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func isLikelyBase64(s string) bool {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if len(s) < 16 {
		return false
	}
	valid := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '-' || c == '_' {
			valid++
		} else if c == '\n' || c == '\r' || c == ' ' || c == '\t' {
			continue
		} else {
			return false
		}
	}
	return valid > len(s)/2
}

func hasProtoPrefix(s string) bool {
	protos := []string{"vmess://", "vless://", "trojan://", "ss://", "ssr://",
		"hy2://", "hysteria2://", "hy://", "hysteria://", "tuic://"}
	for _, p := range protos {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func extractLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func decodeBase64(encoded []byte) (string, error) {
	s := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, string(encoded))

	stripped := strings.TrimRight(s, "=")

	padded := stripped
	if r := len(padded) % 4; r != 0 {
		padded += strings.Repeat("=", 4-r)
	}

	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(padded); err == nil {
			return string(b), nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(stripped); err == nil {
			return string(b), nil
		}
	}
	_, err := base64.RawURLEncoding.DecodeString(stripped)
	return "", err
}
