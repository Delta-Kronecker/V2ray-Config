package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── Country code → flag emoji ─────────────────────────────────────────────────

func countryCodeToFlag(cc string) string {
	if len(cc) != 2 {
		return ""
	}
	cc = strings.ToUpper(cc)
	var buf strings.Builder
	for _, c := range cc {
		r := rune(c) - 'A' + 0x1F1E6
		buf.WriteRune(r)
	}
	return buf.String()
}

// ── GeoIP API types ──────────────────────────────────────────────────────────

type geoQuery struct {
	Query string `json:"query"`
}

type geoResult struct {
	Query       string `json:"query"`
	Status      string `json:"status"`
	CountryCode string `json:"countryCode"`
	Country     string `json:"country"`
}

// ── DNS resolution cache ─────────────────────────────────────────────────────

var dnsCacheMu sync.Mutex
var dnsCache = make(map[string]string)

func resolveHost(host string) string {
	dnsCacheMu.Lock()
	if ip, ok := dnsCache[host]; ok {
		dnsCacheMu.Unlock()
		return ip
	}
	dnsCacheMu.Unlock()

	// If already an IP, return as-is
	if net.ParseIP(host) != nil {
		dnsCacheMu.Lock()
		dnsCache[host] = host
		dnsCacheMu.Unlock()
		return host
	}

	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	ip := ips[0]
	dnsCacheMu.Lock()
	dnsCache[host] = ip
	dnsCacheMu.Unlock()
	return ip
}

// ── Extract server hostname from config ──────────────────────────────────────

func extractServerHost(configURL, protocol string) string {
	switch protocol {
	case "vmess":
		return extractVMessHost(configURL)
	case "vless", "trojan", "ss":
		return extractURIHost(configURL)
	case "ssr":
		return extractSSRHost(configURL)
	case "hy2":
		return extractHy2Host(configURL)
	default:
		return extractURIHost(configURL)
	}
}

func extractVMessHost(configURL string) string {
	data := strings.TrimPrefix(configURL, "vmess://")
	if idx := strings.LastIndex(data, "#"); idx != -1 {
		data = data[:idx]
	}
	data = strings.TrimSpace(data)

	// Try JSON format (base64 or raw)
	var d map[string]interface{}
	if strings.HasPrefix(data, "{") {
		if err := json.Unmarshal([]byte(data), &d); err == nil {
			if add, ok := d["add"].(string); ok && add != "" {
				return add
			}
		}
	} else {
		if decoded, err := decodeBase64([]byte(data)); err == nil {
			if json.Unmarshal([]byte(decoded), &d) == nil {
				if add, ok := d["add"].(string); ok && add != "" {
					return add
				}
			}
		}
		// URI format: vmess://uuid@host:port
		if atIdx := strings.Index(data, "@"); atIdx != -1 {
			rest := data[atIdx+1:]
			if colonIdx := strings.LastIndex(rest, ":"); colonIdx != -1 {
				return rest[:colonIdx]
			}
			return rest
		}
	}
	return ""
}

func extractURIHost(configURL string) string {
	sanitized := sanitizeProxyURL(configURL)
	u, err := url.Parse(sanitized)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func extractSSRHost(configURL string) string {
	trimmed := strings.TrimPrefix(configURL, "ssr://")
	if idx := strings.LastIndex(trimmed, "#"); idx != -1 {
		trimmed = trimmed[:idx]
	}
	decoded, err := decodeBase64([]byte(strings.TrimSpace(trimmed)))
	if err != nil {
		return ""
	}
	body := decoded
	if i := strings.Index(decoded, "/?"); i != -1 {
		body = decoded[:i]
	} else if i := strings.Index(decoded, "?"); i != -1 {
		body = decoded[:i]
	}
	parts := strings.SplitN(body, ":", 6)
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

func extractHy2Host(configURL string) string {
	trimmed := strings.TrimPrefix(configURL, "hy2://")
	if i := strings.LastIndex(trimmed, "#"); i != -1 {
		trimmed = trimmed[:i]
	}
	if i := strings.Index(trimmed, "?"); i != -1 {
		trimmed = trimmed[:i]
	}
	lastAt := strings.LastIndex(trimmed, "@")
	if lastAt == -1 {
		return ""
	}
	hostPort := trimmed[lastAt+1:]
	if i := strings.Index(hostPort, "/"); i != -1 {
		hostPort = hostPort[:i]
	}
	lastColon := strings.LastIndex(hostPort, ":")
	if lastColon == -1 {
		return hostPort
	}
	return hostPort[:lastColon]
}

// ── ip-api.com batch lookup ──────────────────────────────────────────────────

func lookupGeoIPBatch(ips []string, settings GeoSettings) map[string]geoResult {
	results := make(map[string]geoResult)
	if len(ips) == 0 {
		return results
	}

	apiURL := settings.APIURL
	if apiURL == "" {
		apiURL = "http://ip-api.com/batch"
	}
	timeout := time.Duration(settings.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxRetries := settings.MaxRetries
	if maxRetries < 0 {
		maxRetries = 2
	}
	batchSize := settings.BatchSize
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 100 // ip-api.com limit
	}
	batchDelay := time.Duration(settings.BatchDelayMs) * time.Millisecond
	if batchDelay <= 0 {
		batchDelay = 1200 * time.Millisecond
	}

	// Split into batches
	numBatches := (len(ips) + batchSize - 1) / batchSize
	geoClient := &http.Client{Timeout: timeout}

	for b := 0; b < numBatches; b++ {
		start := b * batchSize
		end := start + batchSize
		if end > len(ips) {
			end = len(ips)
		}
		batch := ips[start:end]

		queries := make([]geoQuery, len(batch))
		for i, ip := range batch {
			queries[i] = geoQuery{Query: ip}
		}

		body, err := json.Marshal(queries)
		if err != nil {
			continue
		}

		var lastErr error
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(batchDelay)
			}

			req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := geoClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
				continue
			}

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				lastErr = err
				continue
			}

			var batchResults []geoResult
			if err := json.Unmarshal(respBody, &batchResults); err != nil {
				lastErr = err
				continue
			}

			for _, gr := range batchResults {
				if gr.Status == "success" && gr.CountryCode != "" {
					results[gr.Query] = gr
				}
			}
			lastErr = nil
			break
		}

		if lastErr != nil && gLog != nil {
			gLog.writeLine(fmt.Sprintf("[GEO] batch %d/%d failed: %v", b+1, numBatches, lastErr))
		}

		// Rate limit: delay between batches (except after last)
		if b < numBatches-1 {
			time.Sleep(batchDelay)
		}
	}

	return results
}

// ── CDN detection (Cloudflare CIDR) ──────────────────────────────────────────

var cloudflareCIDRs = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

type cidrRange struct {
	network *net.IPNet
}

var parsedCIDRs []cidrRange

func init() {
	for _, cidr := range cloudflareCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			parsedCIDRs = append(parsedCIDRs, cidrRange{network: network})
		}
	}
}

func isCloudflareIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, c := range parsedCIDRs {
		if c.network.Contains(ip) {
			return true
		}
	}
	return false
}

func detectCDN(results []configResult) []configResult {
	for i := range results {
		host := extractServerHost(results[i].line, results[i].proto)
		if host == "" {
			continue
		}
		ip := resolveHost(host)
		if ip != "" && isCloudflareIP(ip) {
			results[i].cdn = true
		}
	}
	return results
}

// ── Real GeoIP through proxy ──────────────────────────────────────────────────

type realGeoIPResult struct {
	line        string
	countryCode string
	err         error
}

func queryGeoIPThroughProxy(configURL, protocol string, port int) realGeoIPResult {
	var result realGeoIPResult
	result.line = configURL

	outboundJSON, parseErr := toXrayOutbound(configURL, protocol)
	if parseErr != "" {
		result.err = fmt.Errorf("parse: %s", parseErr)
		return result
	}

	fullConfig := buildXrayConfig(outboundJSON, port)

	configFile, err := os.CreateTemp("", "geoip-*.json")
	if err != nil {
		result.err = err
		return result
	}
	configPath := configFile.Name()
	configFile.Close()
	defer os.Remove(configPath)

	if err := os.WriteFile(configPath, []byte(fullConfig), 0644); err != nil {
		result.err = err
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, xrayPath(), "run", "-c", configPath)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		result.err = err
		return result
	}
	defer killGroup(cmd)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if !waitForPort(addr, 8*time.Second, 200*time.Millisecond, 500*time.Millisecond) {
		result.err = fmt.Errorf("xray did not start")
		return result
	}

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 0,
			}).DialContext,
			MaxIdleConns:          1,
			MaxIdleConnsPerHost:   1,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	resp, err := client.Get("https://api.ip2location.io")
	if err != nil {
		result.err = err
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.err = err
		return result
	}

	var geoResp struct {
		CountryCode string `json:"country_code"`
		CountryName string `json:"country_name"`
		IP          string `json:"ip"`
	}
	if err := json.Unmarshal(body, &geoResp); err != nil {
		result.err = err
		return result
	}

	if geoResp.CountryCode == "" {
		result.err = fmt.Errorf("empty country code")
		return result
	}

	result.countryCode = geoResp.CountryCode
	return result
}

func verifyGeoIP(results []configResult) []configResult {
	geoIPResults := make(chan realGeoIPResult, len(results))

	var toCheck []struct {
		idx   int
		line  string
		proto string
	}
	for i, r := range results {
		if r.cdn {
			continue
		}
		toCheck = append(toCheck, struct {
			idx   int
			line  string
			proto string
		}{i, r.line, r.proto})
	}

	if len(toCheck) == 0 {
		return results
	}

	fmt.Printf(" Real GeoIP: verifying %d configs through proxy...\n", len(toCheck))

	v := cfg.Validation
	workers := 20
	if workers > len(toCheck) {
		workers = len(toCheck)
	}

	jobs := make(chan struct {
		idx   int
		line  string
		proto string
	}, len(toCheck))

	for _, tc := range toCheck {
		jobs <- tc
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			port := v.BasePort + 5000 + workerID
			for job := range jobs {
				r := queryGeoIPThroughProxy(job.line, job.proto, port)
				geoIPResults <- r
			}
		}(w)
	}

	wg.Wait()
	close(geoIPResults)

	matchCount := 0
	mismatchCount := 0
	for r := range geoIPResults {
		if r.err != nil {
			continue
		}
		for i := range results {
			if results[i].line == r.line {
				oldCC := results[i].country
				results[i].country = r.countryCode
				results[i].flagEmoji = countryCodeToFlag(r.countryCode)
				if oldCC != "" && oldCC != r.countryCode {
					mismatchCount++
				} else {
					matchCount++
				}
				break
			}
		}
	}

	fmt.Printf(" Real GeoIP: %d matched, %d location corrected\n", matchCount, mismatchCount)
	return results
}
// ── Main orchestrator ────────────────────────────────────────────────────────

func enrichResultsWithGeoIP(results []configResult, settings GeoSettings) []configResult {
	if len(results) == 0 {
		return results
	}

	fmt.Printf(" GeoIP: resolving %d configs...\n", len(results))

	// Step 1: Extract unique hostnames and resolve to IPs
	hostToIP := make(map[string]string)
	var uniqueHosts []string
	seenHosts := make(map[string]bool)

	for i := range results {
		host := extractServerHost(results[i].line, results[i].proto)
		if host == "" {
			continue
		}
		if seenHosts[host] {
			continue
		}
		seenHosts[host] = true

		ip := resolveHost(host)
		if ip != "" {
			hostToIP[host] = ip
			uniqueHosts = append(uniqueHosts, ip)
		}
	}

	// Deduplicate IPs
	var uniqueIPs []string
	seenIPs := make(map[string]bool)
	for _, ip := range uniqueHosts {
		if !seenIPs[ip] {
			seenIPs[ip] = true
			uniqueIPs = append(uniqueIPs, ip)
		}
	}

	fmt.Printf(" GeoIP: %d unique hosts → %d unique IPs\n", len(hostToIP), len(uniqueIPs))

	if len(uniqueIPs) == 0 {
		fmt.Println("  GeoIP: no IPs to lookup")
		return results
	}

	// Step 2: Batch lookup
	start := time.Now()
	geoResults := lookupGeoIPBatch(uniqueIPs, settings)
	elapsed := time.Since(start).Seconds()
	fmt.Printf(" GeoIP: got results for %d/%d IPs in %.1fs\n", len(geoResults), len(uniqueIPs), elapsed)

	// Step 3: Build IP→country map
	ipToCountry := make(map[string]string)
	for ip, gr := range geoResults {
		ipToCountry[ip] = gr.CountryCode
	}

	// Step 4: Assign country and flag to each result
	var matched, unmatched int
	for i := range results {
		host := extractServerHost(results[i].line, results[i].proto)
		if host == "" {
			unmatched++
			continue
		}
		ip, ok := hostToIP[host]
		if !ok {
			unmatched++
			continue
		}
		cc, ok := ipToCountry[ip]
		if !ok || cc == "" {
			unmatched++
			continue
		}
		results[i].country = cc
		results[i].flagEmoji = countryCodeToFlag(cc)
		matched++
	}

	fmt.Printf(" GeoIP: %d matched, %d unmatched\n", matched, unmatched)

	// Print country distribution
	countryCount := make(map[string]int)
	for _, r := range results {
		if r.country != "" {
			cc := r.country
			flag := r.flagEmoji
			countryCount[flag+" "+cc]++
		}
	}
	if len(countryCount) > 0 {
		fmt.Println(" Country distribution:")
		type ccEntry struct {
			name  string
			count int
		}
		var sorted []ccEntry
		for name, count := range countryCount {
			sorted = append(sorted, ccEntry{name, count})
		}
		// Simple sort by count desc
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j].count > sorted[i].count {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		for _, e := range sorted {
			fmt.Printf("   %s: %d\n", e.name, e.count)
		}
	}

	return results
}
