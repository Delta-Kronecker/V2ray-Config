package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Name counter ──────────────────────────────────────────────────────────────

var gNameCountMu sync.Mutex
var gNameCount = make(map[string]int)

func generateUniqueName(base string) string {
	gNameCountMu.Lock()
	defer gNameCountMu.Unlock()

	base = strings.TrimSpace(base)
	if base == "" {
		base = "proxy"
	}
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "|", "_")

	gNameCount[base]++
	count := gNameCount[base]

	return base + "_" + strconv.Itoa(count)
}

// ── Clash base content ────────────────────────────────────────────────────────

type clashBase struct {
	simple   string
	advanced string
}

var gClash clashBase

func loadClashBase(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("clash_base.yaml: %w", err)
	}
	gClash.simple = string(data)
	return nil
}

func loadClashBaseAdvanced(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("clash_base_advanced.yaml: %w", err)
	}
	gClash.advanced = string(data)
	return nil
}

func injectClashProxies(baseContent string, proxyEntries []string, proxyNames []string) string {
	const proxiesPlaceholder = "# ---PROXIES---\n"
	const namesPlaceholder = "# ---PROXY-NAMES---\n"

	var proxyBlock strings.Builder
	for _, e := range proxyEntries {
		proxyBlock.WriteString(e)
	}

	var namesBlock strings.Builder
	for _, n := range proxyNames {
		fmt.Fprintf(&namesBlock, "      - %s\n", yamlQuote(n))
	}

	result := strings.ReplaceAll(baseContent, proxiesPlaceholder, proxyBlock.String())
	result = strings.ReplaceAll(result, namesPlaceholder, namesBlock.String())
	return result
}

// ── prepareOutputDirs ─────────────────────────────────────────────────────────

func prepareOutputDirs() error {
	os.RemoveAll("config")
	dirs := []string{
		"config",
		cfg.Output.ProtocolsDir,
		"config/batches/v2ray",
		"config/batches/clash",
		"config/batches/clash_advanced",
		"config/sni",
		"config/sni/protocols",
		"config/batches/sni_v2ray",
		"config/batches/sni_clash",
		"config/batches/sni_clash_advanced",
		"config/tcp-pass",
		"config/tcp-pass-sni",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// ── writeOutputFiles ──────────────────────────────────────────────────────────

func writeOutputFiles(results []configResult, tcpFailedLines []string) {
	byProto := make(map[string][]string)
	byProtoClash := make(map[string][]string)
	byProtoClashNames := make(map[string][]string)
	var all []string
	var allClash []string
	var allClashNames []string

	bySNIProto := make(map[string][]string)
	bySNIProtoClash := make(map[string][]string)
	bySNIProtoClashNames := make(map[string][]string)
	var allSNI []string
	var allSNIClash []string
	var allSNIClashNames []string

	const v2rayName = "@DeltaKroneckerGithub"
	const clashBaseN = "@DeltaKroneckerGithub_Clash"

	for i, r := range results {
		// v2ray: fixed name (no counter)
		named := renameTo(r.line, r.proto, v2rayName)
		all = append(all, named)
		byProto[r.proto] = append(byProto[r.proto], named)

		// clash: numbered name
		cname := generateUniqueName(clashBaseN)
		if entry, ok := configToClashYAML(r.line, r.proto, cname); ok {
			allClash = append(allClash, entry)
			allClashNames = append(allClashNames, cname)
			byProtoClash[r.proto] = append(byProtoClash[r.proto], entry)
			byProtoClashNames[r.proto] = append(byProtoClashNames[r.proto], cname)
		}

		if sniLine := toSNIConfig(r.line, r.proto); sniLine != "" && isWSTransport(r.line, r.proto) {
			sniNamed := renameTo(sniLine, r.proto, v2rayName)
			allSNI = append(allSNI, sniNamed)
			bySNIProto[r.proto] = append(bySNIProto[r.proto], sniNamed)

			sniCname := generateUniqueName(clashBaseN)
			if sniEntry, ok := configToClashYAML(sniLine, r.proto, sniCname); ok {
				allSNIClash = append(allSNIClash, sniEntry)
				allSNIClashNames = append(allSNIClashNames, sniCname)
				bySNIProtoClash[r.proto] = append(bySNIProtoClash[r.proto], sniEntry)
				bySNIProtoClashNames[r.proto] = append(bySNIProtoClashNames[r.proto], sniCname)
			}
		}
		_ = i
	}

	// ── Original output files ──────────────────────────────────────────────────
	writeFile(cfg.Output.MainFile, all)
	for proto, lines := range byProto {
		writeFile(filepath.Join(cfg.Output.ProtocolsDir, proto+".txt"), lines)
	}

	if gClash.simple != "" {
		writeClashConfigSimple(filepath.Join(filepath.Dir(cfg.Output.MainFile), "clash.yaml"), allClash, allClashNames)
		for proto, entries := range byProtoClash {
			writeClashConfigSimple(filepath.Join(cfg.Output.ProtocolsDir, proto+"_clash.yaml"), entries, byProtoClashNames[proto])
		}
	}
	if gClash.advanced != "" {
		writeClashConfigAdvanced(filepath.Join(filepath.Dir(cfg.Output.MainFile), "clash_advanced.yaml"), allClash, allClashNames)
		for proto, entries := range byProtoClash {
			writeClashConfigAdvanced(filepath.Join(cfg.Output.ProtocolsDir, proto+"_clash_advanced.yaml"), entries, byProtoClashNames[proto])
		}
	}

	// ── SNI output files ───────────────────────────────────────────────────────
	sniDir := "config/sni"
	sniProtosDir := "config/sni/protocols"

	writeFile(filepath.Join(sniDir, "all_configs_sni.txt"), allSNI)
	for proto, lines := range bySNIProto {
		writeFile(filepath.Join(sniProtosDir, proto+"_sni.txt"), lines)
	}

	if gClash.simple != "" {
		writeClashConfigSimple(filepath.Join(sniDir, "clash_sni.yaml"), allSNIClash, allSNIClashNames)
		for proto, entries := range bySNIProtoClash {
			writeClashConfigSimple(filepath.Join(sniProtosDir, proto+"_clash_sni.yaml"), entries, bySNIProtoClashNames[proto])
		}
	}
	if gClash.advanced != "" {
		writeClashConfigAdvanced(filepath.Join(sniDir, "clash_advanced_sni.yaml"), allSNIClash, allSNIClashNames)
		for proto, entries := range bySNIProtoClash {
			writeClashConfigAdvanced(filepath.Join(sniProtosDir, proto+"_clash_advanced_sni.yaml"), entries, bySNIProtoClashNames[proto])
		}
	}

	writeBatchFiles(all, allClash, allClashNames, allSNI, allSNIClash, allSNIClashNames)
	writeTCPPassFiles(tcpFailedLines)
}

func isWSTransport(line, proto string) bool {
	switch proto {
	case "vmess":
		trimmed := strings.TrimPrefix(line, "vmess://")
		payload := trimmed
		if decoded, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
			payload = string(decoded)
		}
		if !strings.HasPrefix(payload, "{") {
			if decoded2, err2 := base64.StdEncoding.DecodeString(trimmed); err2 == nil {
				payload = string(decoded2)
			}
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(payload), &d) == nil {
			if n, ok := d["net"].(string); ok && strings.ToLower(n) == "ws" {
				return true
			}
		}
	case "vless", "trojan", "ss":
		u, err := url.Parse(line)
		if err != nil {
			return false
		}
		if strings.ToLower(u.Query().Get("type")) == "ws" {
			return true
		}
	case "ssr":
		sanitized := strings.TrimPrefix(line, "ssr://")
		if decoded, err := base64.RawURLEncoding.DecodeString(sanitized); err == nil {
			sanitized = string(decoded)
		}
		parts := strings.Split(sanitized, ":")
		if len(parts) >= 7 {
			params := parts[len(parts)-1]
			if decoded2, err2 := base64.RawURLEncoding.DecodeString(params); err2 == nil {
				params = string(decoded2)
			}
			if strings.Contains(params, "type=ws") || strings.Contains(params, "obfs=ws") {
				return true
			}
		}
	}
	return false
}

// ── writeBatchFiles ───────────────────────────────────────────────────────────

func writeBatchFiles(
	allV2ray []string, allClash []string, allClashNames []string,
	allSNIV2ray []string, allSNIClash []string, allSNIClashNames []string,
) {
	const batchSize = 1000

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	shuffledV2ray := make([]string, len(allV2ray))
	copy(shuffledV2ray, allV2ray)
	rng.Shuffle(len(shuffledV2ray), func(i, j int) { shuffledV2ray[i], shuffledV2ray[j] = shuffledV2ray[j], shuffledV2ray[i] })

	type clashPair struct {
		entry string
		name  string
	}
	shuffledClash := make([]clashPair, len(allClash))
	for i := range allClash {
		shuffledClash[i] = clashPair{entry: allClash[i], name: allClashNames[i]}
	}
	rng.Shuffle(len(shuffledClash), func(i, j int) { shuffledClash[i], shuffledClash[j] = shuffledClash[j], shuffledClash[i] })

	for batchIdx := 0; batchIdx*batchSize < len(shuffledV2ray); batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > len(shuffledV2ray) {
			end = len(shuffledV2ray)
		}
		writeFile(fmt.Sprintf("config/batches/v2ray/batch_%03d.txt", batchIdx+1), shuffledV2ray[start:end])
	}

	if len(shuffledClash) > 0 {
		for batchIdx := 0; batchIdx*batchSize < len(shuffledClash); batchIdx++ {
			start := batchIdx * batchSize
			end := start + batchSize
			if end > len(shuffledClash) {
				end = len(shuffledClash)
			}
			batch := shuffledClash[start:end]
			entries := make([]string, len(batch))
			names := make([]string, len(batch))
			for i, p := range batch {
				entries[i] = p.entry
				names[i] = p.name
			}
			if gClash.simple != "" {
				writeClashConfigSimple(fmt.Sprintf("config/batches/clash/batch_%03d.yaml", batchIdx+1), entries, names)
			}
			if gClash.advanced != "" {
				writeClashConfigAdvanced(fmt.Sprintf("config/batches/clash_advanced/batch_%03d.yaml", batchIdx+1), entries, names)
			}
		}
	}

	// SNI batches
	shuffledSNIV2ray := make([]string, len(allSNIV2ray))
	copy(shuffledSNIV2ray, allSNIV2ray)
	rng.Shuffle(len(shuffledSNIV2ray), func(i, j int) { shuffledSNIV2ray[i], shuffledSNIV2ray[j] = shuffledSNIV2ray[j], shuffledSNIV2ray[i] })

	shuffledSNIClash := make([]clashPair, len(allSNIClash))
	for i := range allSNIClash {
		shuffledSNIClash[i] = clashPair{entry: allSNIClash[i], name: allSNIClashNames[i]}
	}
	rng.Shuffle(len(shuffledSNIClash), func(i, j int) { shuffledSNIClash[i], shuffledSNIClash[j] = shuffledSNIClash[j], shuffledSNIClash[i] })

	for batchIdx := 0; batchIdx*batchSize < len(shuffledSNIV2ray); batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > len(shuffledSNIV2ray) {
			end = len(shuffledSNIV2ray)
		}
		writeFile(fmt.Sprintf("config/batches/sni_v2ray/batch_%03d.txt", batchIdx+1), shuffledSNIV2ray[start:end])
	}

	if len(shuffledSNIClash) > 0 {
		for batchIdx := 0; batchIdx*batchSize < len(shuffledSNIClash); batchIdx++ {
			start := batchIdx * batchSize
			end := start + batchSize
			if end > len(shuffledSNIClash) {
				end = len(shuffledSNIClash)
			}
			batch := shuffledSNIClash[start:end]
			entries := make([]string, len(batch))
			names := make([]string, len(batch))
			for i, p := range batch {
				entries[i] = p.entry
				names[i] = p.name
			}
			if gClash.simple != "" {
				writeClashConfigSimple(fmt.Sprintf("config/batches/sni_clash/batch_%03d.yaml", batchIdx+1), entries, names)
			}
			if gClash.advanced != "" {
				writeClashConfigAdvanced(fmt.Sprintf("config/batches/sni_clash_advanced/batch_%03d.yaml", batchIdx+1), entries, names)
			}
		}
	}
}

// ── Low-level file writers ────────────────────────────────────────────────────

func writeFile(path string, lines []string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("❌ Cannot write %s: %v\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256*1024)
	for _, line := range lines {
		w.WriteString(line + "\n")
	}
	w.Flush()
}

func writeClashConfigSimple(path string, proxyEntries, proxyNames []string) {
	if len(proxyEntries) == 0 || gClash.simple == "" {
		return
	}
	content := injectClashProxies(gClash.simple, proxyEntries, proxyNames)
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("❌ Cannot write %s: %v\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 512*1024)
	defer w.Flush()
	w.WriteString(content)
}

func writeClashConfigAdvanced(path string, proxyEntries, proxyNames []string) {
	if len(proxyEntries) == 0 || gClash.advanced == "" {
		return
	}
	content := injectClashProxies(gClash.advanced, proxyEntries, proxyNames)
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("❌ Cannot write %s: %v\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 512*1024)
	defer w.Flush()
	w.WriteString(content)
}

// ── configToClashYAML ─────────────────────────────────────────────────────────

func configToClashYAML(line, proto, name string) (string, bool) {
	switch proto {
	case "vmess":
		return vmessClashYAML(line, name)
	case "vless":
		return vlessClashYAML(line, name)
	case "trojan":
		return trojanClashYAML(line, name)
	case "ss":
		return ssClashYAML(line, name)
	case "hy2":
		return hy2ClashYAML(line, name)
	case "hy":
		return hyClashYAML(line, name)
	case "tuic":
		return tuicClashYAML(line, name)
	case "ssr":
		return "", false
	}
	return "", false
}

// ── Clash YAML builders ───────────────────────────────────────────────────────

func vmessClashYAML(raw, name string) (string, bool) {
	data := strings.TrimPrefix(raw, "vmess://")
	if idx := strings.LastIndex(data, "#"); idx != -1 {
		data = data[:idx]
	}
	data = strings.TrimSpace(data)

	var d map[string]interface{}
	if strings.HasPrefix(data, "{") {
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return "", false
		}
	} else {
		decoded, err := decodeBase64([]byte(data))
		if err != nil {
			return "", false
		}
		if err := json.Unmarshal([]byte(decoded), &d); err != nil {
			return "", false
		}
	}

	server := fmt.Sprintf("%v", d["add"])
	portStr := fmt.Sprintf("%v", d["port"])
	uuid := fmt.Sprintf("%v", d["id"])
	if server == "" || uuid == "" {
		return "", false
	}
	port, err := toPort(portStr)
	if err != nil {
		return "", false
	}

	alterId := 0
	if v, ok := d["aid"]; ok {
		switch x := v.(type) {
		case float64:
			alterId = int(x)
		case string:
			alterId, _ = strconv.Atoi(x)
		}
	}
	cipher := strDefault(d["scy"], "auto")
	network := strings.ToLower(strDefault(d["net"], "tcp"))
	tlsVal := strDefault(d["tls"], "")
	sni := strDefault(d["sni"], server)
	path := strDefault(d["path"], "/")
	host := strDefault(d["host"], "")
	grpcService := strDefault(d["serviceName"], "")

	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: vmess\n    server: %s\n    port: %d\n    uuid: %s\n    alterId: %d\n    cipher: %s\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(uuid), alterId, yamlQuote(cipher))

	if tlsVal == "tls" {
		fmt.Fprintf(&sb, "    tls: true\n    skip-cert-verify: true\n    servername: %s\n", yamlQuote(sni))
	}
	appendNetworkClash(&sb, network, path, host, grpcService)
	return sb.String(), true
}

func vlessClashYAML(raw, name string) (string, bool) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", false
	}
	uuid := normalizeUUID(u.User.Username())
	server := u.Hostname()
	port, err := toPort(u.Port())
	if err != nil || uuid == "" || server == "" {
		return "", false
	}
	q := u.Query()
	security := strings.ToLower(q.Get("security"))
	network := strings.ToLower(q.Get("type"))
	if network == "" {
		network = "tcp"
	}
	sni := first(q.Get("sni"), q.Get("peer"), server)
	fp := first(q.Get("fp"), "")
	flow := q.Get("flow")
	path := first(q.Get("path"), "/")
	host := q.Get("host")
	grpcService := first(q.Get("serviceName"), q.Get("path"))

	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: vless\n    server: %s\n    port: %d\n    uuid: %s\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(uuid))

	if security == "reality" {
		pbk := q.Get("pbk")
		sid := q.Get("sid")
		fmt.Fprintf(&sb, "    tls: true\n    skip-cert-verify: true\n    servername: %s\n", yamlQuote(sni))
		if fp != "" {
			fmt.Fprintf(&sb, "    client-fingerprint: %s\n", yamlQuote(fp))
		}
		fmt.Fprintf(&sb, "    reality-opts:\n      public-key: %s\n      short-id: %s\n", yamlQuote(pbk), yamlQuote(sid))
	} else if security == "tls" || security == "xtls" {
		fmt.Fprintf(&sb, "    tls: true\n    skip-cert-verify: true\n    servername: %s\n", yamlQuote(sni))
		if fp != "" {
			fmt.Fprintf(&sb, "    client-fingerprint: %s\n", yamlQuote(fp))
		}
	}
	if flow != "" && singboxSupportedFlows[flow] {
		fmt.Fprintf(&sb, "    flow: %s\n", yamlQuote(flow))
	}
	appendNetworkClash(&sb, network, path, host, grpcService)
	return sb.String(), true
}

func trojanClashYAML(raw, name string) (string, bool) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", false
	}
	password := u.User.Username()
	server := u.Hostname()
	port, err := toPort(u.Port())
	if err != nil || password == "" || server == "" {
		return "", false
	}
	q := u.Query()
	sni := first(q.Get("sni"), q.Get("peer"), server)
	network := strings.ToLower(q.Get("type"))
	path := first(q.Get("path"), "/")
	host := q.Get("host")
	grpcService := first(q.Get("serviceName"), q.Get("path"))

	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: trojan\n    server: %s\n    port: %d\n    password: %s\n    sni: %s\n    skip-cert-verify: true\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(password), yamlQuote(sni))
	appendNetworkClash(&sb, network, path, host, grpcService)
	return sb.String(), true
}

func ssClashYAML(raw, name string) (string, bool) {
	trimmed := strings.TrimPrefix(raw, "ss://")
	if idx := strings.LastIndex(trimmed, "#"); idx != -1 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSpace(trimmed)

	queryStr := ""
	if qi := strings.Index(trimmed, "?"); qi != -1 {
		queryStr = trimmed[qi+1:]
		trimmed = trimmed[:qi]
	}

	var method, password, server string
	var port int

	parsed := false
	if u, err := url.Parse("ss://" + trimmed); err == nil && u.User != nil && u.Hostname() != "" {
		uname := u.User.Username()
		pwd, hasPwd := u.User.Password()
		if hasPwd {
			// uname is plaintext method — reject if UUID
			if isUUID(uname) {
				return "", false
			}
			method, password = uname, pwd
		} else {
			if d, derr := decodeBase64([]byte(uname)); derr == nil {
				if strings.HasPrefix(d, "ss://") {
					inner := strings.TrimPrefix(d, "ss://")
					if d2, e2 := decodeBase64([]byte(inner)); e2 == nil && strings.Contains(d2, ":") {
						parts := strings.SplitN(d2, ":", 2)
						method, password = parts[0], parts[1]
					}
				} else if strings.Contains(d, ":") {
					parts := strings.SplitN(d, ":", 2)
					method, password = parts[0], parts[1]
				}
			}
		}
		if method != "" {
			portStr := u.Port()
			if portStr == "" {
				portStr = "443"
			}
			if p, perr := toPort(portStr); perr == nil {
				server = u.Hostname()
				port = p
				parsed = true
			}
		}
	}

	if !parsed {
		atIdx := strings.LastIndex(trimmed, "@")
		if atIdx == -1 {
			return "", false
		}
		m, p, s, po, e := ssParseUserAndHost(trimmed[:atIdx], trimmed[atIdx+1:])
		if e != "" {
			return "", false
		}
		method, password, server, port = m, p, s, po
	}

	method = strings.ToLower(method)
	if server == "" {
		return "", false
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: ss\n    server: %s\n    port: %d\n    cipher: %s\n    password: %s\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(method), yamlQuote(password))

	if queryStr != "" {
		q, _ := url.ParseQuery(queryStr)
		if plugin := q.Get("plugin"); plugin != "" {
			pluginParts := strings.SplitN(plugin, ";", 2)
			if len(pluginParts) > 0 {
				pluginName := pluginParts[0]
				switch {
				case pluginName == "obfs-local" || pluginName == "obfs":
					fmt.Fprintf(&sb, "    plugin: obfs\n    plugin-opts:\n")
					if len(pluginParts) > 1 {
						opts := parsePluginOpts(pluginParts[1])
						if mode, ok := opts["obfs"]; ok {
							fmt.Fprintf(&sb, "      mode: %s\n", yamlQuote(mode))
						}
						if h, ok := opts["obfs-host"]; ok {
							fmt.Fprintf(&sb, "      host: %s\n", yamlQuote(h))
						}
					}
				case pluginName == "v2ray-plugin":
					fmt.Fprintf(&sb, "    plugin: v2ray-plugin\n    plugin-opts:\n")
					if len(pluginParts) > 1 {
						opts := parsePluginOpts(pluginParts[1])
						mode := first(opts["mode"], "websocket")
						fmt.Fprintf(&sb, "      mode: %s\n", yamlQuote(mode))
						if path, ok := opts["path"]; ok {
							fmt.Fprintf(&sb, "      path: %s\n", yamlQuote(path))
						}
						if h, ok := opts["host"]; ok {
							fmt.Fprintf(&sb, "      host: %s\n", yamlQuote(h))
						}
						if _, hasTLS := opts["tls"]; hasTLS {
							fmt.Fprintf(&sb, "      tls: true\n")
						}
					}
				}
			}
		}
	}
	return sb.String(), true
}

func parsePluginOpts(s string) map[string]string {
	opts := make(map[string]string)
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx != -1 {
			opts[part[:idx]] = part[idx+1:]
		} else {
			opts[part] = ""
		}
	}
	return opts
}

func hy2ClashYAML(raw, name string) (string, bool) {
	trimmed := strings.TrimPrefix(raw, "hy2://")
	if i := strings.LastIndex(trimmed, "#"); i != -1 {
		trimmed = trimmed[:i]
	}
	queryStr := ""
	if i := strings.Index(trimmed, "?"); i != -1 {
		queryStr = trimmed[i+1:]
		trimmed = trimmed[:i]
	}
	lastAt := strings.LastIndex(trimmed, "@")
	if lastAt == -1 {
		return "", false
	}
	password := trimmed[:lastAt]
	hostPort := trimmed[lastAt+1:]
	if password == "" {
		return "", false
	}
	if i := strings.Index(hostPort, "/"); i != -1 {
		hostPort = hostPort[:i]
	}
	lastColon := strings.LastIndex(hostPort, ":")
	if lastColon == -1 {
		return "", false
	}
	server := hostPort[:lastColon]
	port, err := toPort(hostPort[lastColon+1:])
	if err != nil || server == "" {
		return "", false
	}
	q, _ := url.ParseQuery(queryStr)
	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: hysteria2\n    server: %s\n    port: %d\n    password: %s\n    sni: %s\n    skip-cert-verify: true\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(password), yamlQuote(first(q.Get("sni"), server)))
	if obfs := q.Get("obfs"); obfs != "" {
		fmt.Fprintf(&sb, "    obfs: %s\n", yamlQuote(obfs))
		if obfsPwd := q.Get("obfs-password"); obfsPwd != "" {
			fmt.Fprintf(&sb, "    obfs-password: %s\n", yamlQuote(obfsPwd))
		}
	}
	return sb.String(), true
}

func hyClashYAML(raw, name string) (string, bool) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", false
	}
	server := u.Hostname()
	if server == "" {
		return "", false
	}
	port, err := toPort(u.Port())
	if err != nil {
		return "", false
	}
	q := u.Query()
	auth := first(q.Get("auth"), u.User.Username())
	if auth == "" {
		return "", false
	}
	up, _ := strconv.Atoi(first(q.Get("upmbps"), "10"))
	down, _ := strconv.Atoi(first(q.Get("downmbps"), "50"))
	if up <= 0 {
		up = 10
	}
	if down <= 0 {
		down = 50
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: hysteria\n    server: %s\n    port: %d\n    auth-str: %s\n    up: %d\n    down: %d\n    sni: %s\n    skip-cert-verify: true\n    udp: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(auth), up, down,
		yamlQuote(first(q.Get("peer"), q.Get("sni"), server)))
	if obfs := q.Get("obfs"); obfs != "" {
		fmt.Fprintf(&sb, "    obfs: %s\n", yamlQuote(obfs))
	}
	if proto := q.Get("protocol"); proto != "" {
		fmt.Fprintf(&sb, "    protocol: %s\n", yamlQuote(proto))
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		parts := strings.Split(alpnStr, ",")
		quoted := make([]string, len(parts))
		for i, a := range parts {
			quoted[i] = yamlQuote(strings.TrimSpace(a))
		}
		fmt.Fprintf(&sb, "    alpn: [%s]\n", strings.Join(quoted, ", "))
	}
	return sb.String(), true
}

func tuicClashYAML(raw, name string) (string, bool) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", false
	}
	uuid := u.User.Username()
	password, _ := u.User.Password()
	server := u.Hostname()
	port, err := toPort(u.Port())
	if err != nil || uuid == "" || server == "" {
		return "", false
	}
	q := u.Query()
	congestion := first(q.Get("congestion_control"), q.Get("congestion-controller"), "bbr")
	var sb strings.Builder
	fmt.Fprintf(&sb, "  - name: %s\n    type: tuic\n    server: %s\n    port: %d\n    uuid: %s\n    password: %s\n    sni: %s\n    skip-cert-verify: true\n    udp: true\n    congestion-controller: %s\n    reduce-rtt: true\n",
		yamlQuote(name), yamlQuote(server), port, yamlQuote(uuid), yamlQuote(password),
		yamlQuote(first(q.Get("sni"), server)), congestion)
	if udpRelay := first(q.Get("udp_relay_mode"), q.Get("udp-relay-mode")); udpRelay != "" {
		fmt.Fprintf(&sb, "    udp-relay-mode: %s\n", yamlQuote(udpRelay))
	}
	return sb.String(), true
}

func appendNetworkClash(sb *strings.Builder, network, path, host, grpcService string) {
	if path == "" {
		path = "/"
	}
	switch network {
	case "ws":
		fmt.Fprintf(sb, "    network: ws\n    ws-opts:\n      path: %s\n", yamlQuote(path))
		if host != "" {
			fmt.Fprintf(sb, "      headers:\n        Host: %s\n", yamlQuote(host))
		}
	case "grpc":
		fmt.Fprintf(sb, "    network: grpc\n    grpc-opts:\n      grpc-service-name: %s\n", yamlQuote(grpcService))
	case "h2", "http":
		fmt.Fprintf(sb, "    network: h2\n    h2-opts:\n      path: %s\n", yamlQuote(path))
		if host != "" {
			fmt.Fprintf(sb, "      host: [%s]\n", yamlQuote(host))
		}
	case "httpupgrade":
		fmt.Fprintf(sb, "    network: httpupgrade\n    httpupgrade-opts:\n      path: %s\n", yamlQuote(path))
		if host != "" {
			fmt.Fprintf(sb, "      host: %s\n", yamlQuote(host))
		}
	case "splithttp", "xhttp":
		fmt.Fprintf(sb, "    network: splithttp\n    splithttp-opts:\n      path: %s\n", yamlQuote(path))
		if host != "" {
			fmt.Fprintf(sb, "      host: %s\n", yamlQuote(host))
		}
	}
}

// ── Clash URI converters (used by fetcher/smartDecode) ────────────────────────

func clashPortStr(v interface{}) string {
	if v == nil {
		return "443"
	}
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.Itoa(int(x))
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "443"
		}
		return s
	}
	return "443"
}

func clashBandwidthMbps(v interface{}) int {
	if v == nil {
		return 10
	}
	switch x := v.(type) {
	case int:
		if x <= 0 {
			return 10
		}
		return x
	case float64:
		if int(x) <= 0 {
			return 10
		}
		return int(x)
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		for _, suffix := range []string{" mbps", "mbps", " mb/s", "mb/s", " mbit/s"} {
			s = strings.TrimSuffix(s, suffix)
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n <= 0 {
			return 10
		}
		return n
	}
	return 10
}

func clashWsPath(opts *ClashWSOpts) string {
	if opts == nil || opts.Path == "" {
		return "/"
	}
	return opts.Path
}

func clashWsHost(opts *ClashWSOpts) string {
	if opts == nil {
		return ""
	}
	if h := opts.Headers["Host"]; h != "" {
		return h
	}
	if h := opts.Headers["host"]; h != "" {
		return h
	}
	return ""
}

func clashGRPCService(opts *ClashGRPCOpts) string {
	if opts == nil {
		return ""
	}
	return opts.ServiceName
}

func clashSNI(p ClashProxy) string {
	if p.SNI != "" {
		return p.SNI
	}
	if p.SniAlt != "" {
		return p.SniAlt
	}
	return p.Server
}

func clashFingerprint(p ClashProxy) string {
	if p.Fingerprint != "" {
		return p.Fingerprint
	}
	return p.FingerprintAlt
}

func clashTransportParams(p ClashProxy, q url.Values) {
	network := strings.ToLower(p.Network)
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)
	switch network {
	case "ws":
		q.Set("path", clashWsPath(p.WSOpts))
		if h := clashWsHost(p.WSOpts); h != "" {
			q.Set("host", h)
		}
	case "grpc":
		if svc := clashGRPCService(p.GRPCOpts); svc != "" {
			q.Set("serviceName", svc)
			q.Set("path", svc)
		}
	case "h2", "http":
		if p.H2Opts != nil {
			if len(p.H2Opts.Path) > 0 {
				q.Set("path", p.H2Opts.Path[0])
			}
			if len(p.H2Opts.Host) > 0 {
				q.Set("host", p.H2Opts.Host[0])
			}
		}
	case "httpupgrade":
		if p.HTTPUpgradeOpts != nil {
			if p.HTTPUpgradeOpts.Path != "" {
				q.Set("path", p.HTTPUpgradeOpts.Path)
			}
			if p.HTTPUpgradeOpts.Host != "" {
				q.Set("host", p.HTTPUpgradeOpts.Host)
			}
		}
	case "splithttp":
		if p.SplitHTTPOpts != nil {
			if p.SplitHTTPOpts.Path != "" {
				q.Set("path", p.SplitHTTPOpts.Path)
			}
			if p.SplitHTTPOpts.Host != "" {
				q.Set("host", p.SplitHTTPOpts.Host)
			}
		}
	}
}

func clashVMessToURI(p ClashProxy) string {
	if p.Server == "" || p.UUID == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	alterId := 0
	if p.AlterID != nil {
		switch x := p.AlterID.(type) {
		case int:
			alterId = x
		case float64:
			alterId = int(x)
		case string:
			alterId, _ = strconv.Atoi(x)
		}
	}
	cipher := p.Cipher
	if cipher == "" {
		cipher = "auto"
	}
	network := strings.ToLower(p.Network)
	if network == "" {
		network = "tcp"
	}
	tlsVal := ""
	if p.TLS {
		tlsVal = "tls"
	}
	sni := clashSNI(p)
	path := "/"
	host := ""
	grpcService := ""
	switch network {
	case "ws":
		path = clashWsPath(p.WSOpts)
		host = clashWsHost(p.WSOpts)
	case "grpc":
		grpcService = clashGRPCService(p.GRPCOpts)
	case "h2", "http":
		if p.H2Opts != nil {
			if len(p.H2Opts.Path) > 0 {
				path = p.H2Opts.Path[0]
			}
			if len(p.H2Opts.Host) > 0 {
				host = p.H2Opts.Host[0]
			}
		}
	case "httpupgrade":
		if p.HTTPUpgradeOpts != nil {
			path = p.HTTPUpgradeOpts.Path
			host = p.HTTPUpgradeOpts.Host
		}
	case "splithttp":
		if p.SplitHTTPOpts != nil {
			path = p.SplitHTTPOpts.Path
			host = p.SplitHTTPOpts.Host
		}
	}
	if path == "" {
		path = "/"
	}
	d := map[string]interface{}{
		"v": "2", "ps": p.Name, "add": p.Server, "port": portStr,
		"id": p.UUID, "aid": alterId, "scy": cipher, "net": network,
		"tls": tlsVal, "sni": sni, "host": host, "path": path, "serviceName": grpcService,
	}
	data, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data)
}

func clashVLessToURI(p ClashProxy) string {
	if p.Server == "" || p.UUID == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	security := "none"
	if p.RealityOpts != nil {
		security = "reality"
	} else if p.TLS {
		security = "tls"
	}
	sni := clashSNI(p)
	q := url.Values{}
	clashTransportParams(p, q)
	q.Set("security", security)
	if security != "none" {
		q.Set("sni", sni)
		if fp := clashFingerprint(p); fp != "" {
			q.Set("fp", fp)
		}
		if len(p.ALPN) > 0 {
			q.Set("alpn", strings.Join(p.ALPN, ","))
		}
	}
	if security == "reality" && p.RealityOpts != nil {
		q.Set("pbk", p.RealityOpts.PublicKey)
		q.Set("sid", p.RealityOpts.ShortID)
	}
	if p.Flow != "" {
		q.Set("flow", p.Flow)
	}
	return fmt.Sprintf("vless://%s@%s:%s?%s#%s",
		url.PathEscape(p.UUID), p.Server, portStr, q.Encode(), url.PathEscape(p.Name))
}

func clashTrojanToURI(p ClashProxy) string {
	if p.Server == "" || p.Password == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	sni := clashSNI(p)
	q := url.Values{}
	clashTransportParams(p, q)
	q.Set("sni", sni)
	if fp := clashFingerprint(p); fp != "" {
		q.Set("fp", fp)
	}
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	return fmt.Sprintf("trojan://%s@%s:%s?%s#%s",
		url.PathEscape(p.Password), p.Server, portStr, q.Encode(), url.PathEscape(p.Name))
}

func clashSSToURI(p ClashProxy) string {
	if p.Server == "" || p.Cipher == "" || p.Password == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	userInfo := base64.StdEncoding.EncodeToString([]byte(p.Cipher + ":" + p.Password))
	q := url.Values{}
	switch p.Plugin {
	case "obfs":
		mode := ""
		host := ""
		if p.PluginOpts != nil {
			if m, ok := p.PluginOpts["mode"].(string); ok {
				mode = m
			}
			if h, ok := p.PluginOpts["host"].(string); ok {
				host = h
			}
		}
		pluginStr := "obfs-local"
		if mode != "" {
			pluginStr += ";obfs=" + mode
		}
		if host != "" {
			pluginStr += ";obfs-host=" + host
		}
		q.Set("plugin", pluginStr)
	case "v2ray-plugin":
		if p.PluginOpts != nil {
			mode, _ := p.PluginOpts["mode"].(string)
			if mode == "websocket" || mode == "" {
				pluginStr := "v2ray-plugin"
				wsPath := "/"
				if pt, ok := p.PluginOpts["path"].(string); ok && pt != "" {
					wsPath = pt
				}
				wsHost := p.Server
				if h, ok := p.PluginOpts["host"].(string); ok && h != "" {
					wsHost = h
				}
				tls, _ := p.PluginOpts["tls"].(bool)
				pluginStr += ";mode=websocket;path=" + wsPath + ";host=" + wsHost
				if tls {
					pluginStr += ";tls"
				}
				q.Set("plugin", pluginStr)
			} else {
				return ""
			}
		}
	}
	uri := fmt.Sprintf("ss://%s@%s:%s", userInfo, p.Server, portStr)
	if len(q) > 0 {
		uri += "?" + q.Encode()
	}
	return uri + "#" + url.PathEscape(p.Name)
}

func clashSSRToURI(p ClashProxy) string {
	if p.Server == "" || p.Password == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	protocol := p.Protocol
	if protocol == "" {
		protocol = "origin"
	}
	cipher := p.Cipher
	if cipher == "" {
		cipher = "none"
	}
	obfs := p.Obfs
	if obfs == "" {
		obfs = "plain"
	}
	b64pass := base64.RawURLEncoding.EncodeToString([]byte(p.Password))
	body := fmt.Sprintf("%s:%s:%s:%s:%s:%s", p.Server, portStr, protocol, cipher, obfs, b64pass)
	b64obfsParam := base64.RawURLEncoding.EncodeToString([]byte(p.ObfsParam))
	b64protoParam := base64.RawURLEncoding.EncodeToString([]byte(p.ProtocolParam))
	b64name := base64.RawURLEncoding.EncodeToString([]byte(p.Name))
	params := fmt.Sprintf("obfsparam=%s&protoparam=%s&remarks=%s", b64obfsParam, b64protoParam, b64name)
	full := body + "/?" + params
	return "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(full))
}

func clashHy2ToURI(p ClashProxy) string {
	if p.Server == "" || p.Password == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	sni := clashSNI(p)
	q := url.Values{}
	q.Set("sni", sni)
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	if p.ObfsPassword != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", p.ObfsPassword)
	}
	return fmt.Sprintf("hy2://%s@%s:%s?%s#%s",
		url.PathEscape(p.Password), p.Server, portStr, q.Encode(), url.PathEscape(p.Name))
}

func clashHyToURI(p ClashProxy) string {
	if p.Server == "" {
		return ""
	}
	auth := first(p.AuthStr, p.AuthStrAlt, p.Auth)
	if auth == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	sni := clashSNI(p)
	up := clashBandwidthMbps(p.Up)
	down := clashBandwidthMbps(p.Down)
	q := url.Values{}
	q.Set("peer", sni)
	q.Set("sni", sni)
	q.Set("upmbps", strconv.Itoa(up))
	q.Set("downmbps", strconv.Itoa(down))
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	if p.Obfs != "" {
		q.Set("obfs", p.Obfs)
	}
	if p.Protocol != "" {
		q.Set("protocol", p.Protocol)
	}
	return fmt.Sprintf("hy://%s@%s:%s?%s#%s",
		url.PathEscape(auth), p.Server, portStr, q.Encode(), url.PathEscape(p.Name))
}

func clashTUICToURI(p ClashProxy) string {
	if p.Server == "" || p.UUID == "" {
		return ""
	}
	portStr := clashPortStr(p.Port)
	password := p.Password
	if password == "" {
		password = p.Token
	}
	sni := clashSNI(p)
	q := url.Values{}
	q.Set("sni", sni)
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	if p.PluginOpts != nil {
		if congestion, ok := p.PluginOpts["congestion-controller"].(string); ok && congestion != "" {
			q.Set("congestion_control", congestion)
		}
	}
	return fmt.Sprintf("tuic://%s:%s@%s:%s?%s#%s",
		url.PathEscape(p.UUID), url.PathEscape(password),
		p.Server, portStr, q.Encode(), url.PathEscape(p.Name))
}

func clashProxyToURI(p ClashProxy) string {
	ptype := strings.ToLower(strings.TrimSpace(p.Type))
	switch ptype {
	case "vmess":
		return clashVMessToURI(p)
	case "vless":
		return clashVLessToURI(p)
	case "trojan":
		return clashTrojanToURI(p)
	case "ss", "shadowsocks":
		return clashSSToURI(p)
	case "ssr", "shadowsocksr":
		return clashSSRToURI(p)
	case "hy2", "hysteria2":
		return clashHy2ToURI(p)
	case "hy", "hysteria":
		return clashHyToURI(p)
	case "tuic":
		return clashTUICToURI(p)
	}
	return ""
}

// ── SNI config conversion ─────────────────────────────────────────────────────

func toSNIConfig(line, proto string) string {
	switch proto {
	case "vmess":
		return toSNIVMess(line)
	case "vless", "trojan", "hy2", "hy", "tuic", "ss":
		return toSNIGeneric(line, proto)
	case "ssr":
		return toSNISSR(line)
	}
	return ""
}

func toSNIVMess(line string) string {
	data := strings.TrimPrefix(line, "vmess://")
	fragSuffix := ""
	if idx := strings.LastIndex(data, "#"); idx != -1 {
		fragSuffix = data[idx:]
		data = data[:idx]
	}
	data = strings.TrimSpace(data)

	var d map[string]interface{}
	var isURI bool

	if strings.HasPrefix(data, "{") {
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return ""
		}
	} else {
		decoded, err := decodeBase64([]byte(data))
		if err == nil {
			if json.Unmarshal([]byte(decoded), &d) != nil {
				d = nil
			}
		}
		if d == nil {
			if atIdx := strings.Index(data, "@"); atIdx != -1 {
				isURI = true
			}
		}
	}

	if isURI {
		atIdx := strings.LastIndex(data, "@")
		if atIdx == -1 {
			return ""
		}
		userPart := data[:atIdx]
		rest := data[atIdx+1:]
		qIdx := strings.Index(rest, "?")
		querySuffix := ""
		if qIdx != -1 {
			querySuffix = rest[qIdx:]
		}
		return fmt.Sprintf("vmess://%s@%s:%d%s%s", userPart, sniHost, sniPort, querySuffix, fragSuffix)
	}

	if d == nil {
		return ""
	}
	d["add"] = sniHost
	d["port"] = strconv.Itoa(sniPort)

	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, _ := json.Marshal(d[k])
		buf.Write(kj)
		buf.WriteByte(':')
		buf.Write(vj)
	}
	buf.WriteByte('}')
	return "vmess://" + base64.StdEncoding.EncodeToString(buf.Bytes()) + fragSuffix
}

func toSNIGeneric(line, proto string) string {
	schemeEnd := strings.Index(line, "://")
	if schemeEnd == -1 {
		return ""
	}
	scheme := line[:schemeEnd+3]
	rest := line[schemeEnd+3:]

	frag := ""
	if idx := strings.LastIndex(rest, "#"); idx != -1 {
		frag = rest[idx:]
		rest = rest[:idx]
	}
	query := ""
	if idx := strings.Index(rest, "?"); idx != -1 {
		query = rest[idx:]
		rest = rest[:idx]
	}
	path := ""
	if idx := strings.Index(rest, "/"); idx != -1 {
		path = rest[idx:]
		rest = rest[:idx]
	}

	atIdx := strings.LastIndex(rest, "@")
	var userInfo, hostPort string
	if atIdx != -1 {
		userInfo = rest[:atIdx+1]
		hostPort = rest[atIdx+1:]
	} else {
		hostPort = rest
	}

	newHostPort := fmt.Sprintf("%s:%d", sniHost, sniPort)
	_ = hostPort
	return scheme + userInfo + newHostPort + path + query + frag
}

func toSNISSR(line string) string {
	trimmed := strings.TrimPrefix(line, "ssr://")
	decoded, err := decodeBase64([]byte(trimmed))
	if err != nil {
		return ""
	}
	params := ""
	body := decoded
	if i := strings.Index(decoded, "/?"); i != -1 {
		params = decoded[i:]
		body = decoded[:i]
	} else if i := strings.Index(decoded, "?"); i != -1 {
		params = decoded[i:]
		body = decoded[:i]
	}
	parts := strings.SplitN(body, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	parts[0] = sniHost
	parts[1] = strconv.Itoa(sniPort)
	newFull := strings.Join(parts, ":") + params
	return "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(newFull))
}

// ── detectProto ──────────────────────────────────────────────────────────────

func detectProto(line string) string {
	for _, p := range cfg.Protocols {
		if strings.HasPrefix(line, p+"://") {
			return p
		}
	}
	return ""
}

// ── writeOnlyTCPPassFiles ────────────────────────────────────────────────────
// Writes configs that passed TCP ping but failed xray validation
// into 10000-line batches under config/tcp-pass/ and config/tcp-pass-sni/

func writeTCPPassFiles(lines []string) {
	if len(lines) == 0 {
		return
	}
	const fixedName = "@DeltaKroneckerGithub"
	const batchSize = 10000

	// rename all to fixed name + build SNI versions
	named := make([]string, 0, len(lines))
	sniNamed := make([]string, 0, len(lines))
	for _, line := range lines {
		proto := detectProto(line)
		n := renameTo(line, proto, fixedName)
		named = append(named, n)

		sniLine := toSNIConfig(line, proto)
		if sniLine != "" {
			sn := renameTo(sniLine, proto, fixedName)
			sniNamed = append(sniNamed, sn)
		}
	}

	// write original batches
	total := len(named)
	numBatches := (total + batchSize - 1) / batchSize
	for i := 0; i < numBatches; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > total {
			end = total
		}
		writeFile(fmt.Sprintf("config/tcp-pass/batch_%03d.txt", i+1), named[start:end])
	}
	fmt.Printf("📁 TCP-Pass: wrote %d configs into %d files\n", total, numBatches)

	// write SNI batches
	if len(sniNamed) > 0 {
		sniTotal := len(sniNamed)
		sniNumBatches := (sniTotal + batchSize - 1) / batchSize
		for i := 0; i < sniNumBatches; i++ {
			start := i * batchSize
			end := start + batchSize
			if end > sniTotal {
				end = sniTotal
			}
			writeFile(fmt.Sprintf("config/tcp-pass-sni/batch_%03d.txt", i+1), sniNamed[start:end])
		}
		fmt.Printf("📁 TCP-Pass-SNI: wrote %d configs into %d files\n", sniTotal, sniNumBatches)
	}
}

// ── writeSummary (README) ─────────────────────────────────────────────────────

const autoGenMarker = "<!-- AUTO-GENERATED: DO NOT EDIT BELOW THIS LINE -->\n"

func writeSummary(results []configResult, failedLinks []string, duration float64, originalTotal int, onlyTCPPassCount int) {
	byProtoOut := make(map[string]int)
	for _, r := range results {
		byProtoOut[r.proto]++
	}

	repoBase := "https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main"

	var gen strings.Builder
	gen.WriteString(autoGenMarker)
	gen.WriteString("\n")

	// ── 1. V2ray ────────────────────────────────────────────────────────────────
	gen.WriteString("## 🔥 V2ray\n\n")
	fmt.Fprintf(&gen, "V2ray All (%d config):\n```\n%s/config/all_configs.txt\n```\n\n", len(results), repoBase)
	for _, p := range cfg.ProtocolOrder {
		if n := byProtoOut[p]; n > 0 {
			fmt.Fprintf(&gen, "V2ray %s (%d config):\n```\n%s/config/protocols/%s.txt\n```\n\n",
				strings.ToUpper(p), n, repoBase, p)
		}
	}
	gen.WriteString("WARP All:\n```\nhttps://github.com/Delta-Kronecker/WARP-Config/raw/refs/heads/main/ALL.txt\n```\n\n")

	// ── 2. V2ray Batches ────────────────────────────────────────────────────────
	v2rayBatches := countBatchFiles("config/batches/v2ray")
	if v2rayBatches > 0 {
		gen.WriteString("## 🔥 V2ray Batches\n\n")
		for i := 1; i <= v2rayBatches; i++ {
			cnt := min500(i, len(results))
			fmt.Fprintf(&gen, "V2ray Batch %03d (%d config):\n```\n%s/config/batches/v2ray/batch_%03d.txt\n```\n\n",
				i, cnt, repoBase, i)
		}
	}

	// ── 3. Clash ────────────────────────────────────────────────────────────────
	gen.WriteString("## 🔥 Clash\n\n")
	fmt.Fprintf(&gen, "Clash All (%d config):\n```\n%s/config/clash.yaml\n```\n\n", len(results), repoBase)
	for _, p := range cfg.ProtocolOrder {
		if n := byProtoOut[p]; n > 0 {
			fmt.Fprintf(&gen, "Clash %s (%d config):\n```\n%s/config/protocols/%s_clash.yaml\n```\n\n",
				strings.ToUpper(p), n, repoBase, p)
		}
	}
	gen.WriteString("WARP All:\n```\nhttps://github.com/Delta-Kronecker/WARP-Config/raw/refs/heads/main/ALL.yaml\n```\n\n")

	// ── 4. Clash Batches ────────────────────────────────────────────────────────
	clashBatches := countBatchFiles("config/batches/clash")
	if clashBatches > 0 {
		gen.WriteString("## 🔥 Clash Batches\n\n")
		for i := 1; i <= clashBatches; i++ {
			cnt := min500(i, len(results))
			fmt.Fprintf(&gen, "Clash Batch %03d (%d config):\n```\n%s/config/batches/clash/batch_%03d.yaml\n```\n\n",
				i, cnt, repoBase, i)
		}
	}

	// ── 5. 127.0.0.1:40443 ─────────────────────────────────────────────────────
	gen.WriteString("## 🔥 127.0.0.1:40443\n\n")
	fmt.Fprintf(&gen, "SNI All (%d config):\n```\n%s/config/sni/all_configs_sni.txt\n```\n\n", len(results), repoBase)
	for _, p := range cfg.ProtocolOrder {
		if n := byProtoOut[p]; n > 0 {
			fmt.Fprintf(&gen, "SNI %s (%d config):\n```\n%s/config/sni/protocols/%s_sni.txt\n```\n\n",
				strings.ToUpper(p), n, repoBase, p)
		}
	}

	// ── 6. 127.0.0.1:40443 Batches ─────────────────────────────────────────────
	sniV2rayBatches := countBatchFiles("config/batches/sni_v2ray")
	if sniV2rayBatches > 0 {
		gen.WriteString("## 🔥 127.0.0.1:40443 Batches\n\n")
		for i := 1; i <= sniV2rayBatches; i++ {
			cnt := min500(i, len(results))
			fmt.Fprintf(&gen, "SNI Batch %03d (%d config):\n```\n%s/config/batches/sni_v2ray/batch_%03d.txt\n```\n\n",
				i, cnt, repoBase, i)
		}
	}

	// ── 7. TCP Pass ─────────────────────────────────────────────────────────────
	onlyTCPBatches := countBatchFiles("config/tcp-pass")
	if onlyTCPBatches > 0 {
		gen.WriteString("## 🔥 TCP Pass (for advanced users)\n\n")
		fmt.Fprintf(&gen, "> All configs that passed TCP ping. Total: **%d**\n\n", onlyTCPPassCount)
		for i := 1; i <= onlyTCPBatches; i++ {
			fmt.Fprintf(&gen, "TCP Pass Batch %03d:\n```\n%s/config/tcp-pass/batch_%03d.txt\n```\n\n",
				i, repoBase, i)
		}
	}

	// ── 8. TCP Pass 127.0.0.1:40443 ───────────────────────────────────────────
	onlyTCPSNIBatches := countBatchFiles("config/tcp-pass-sni")
	if onlyTCPSNIBatches > 0 {
		gen.WriteString("## 🔥 TCP Pass 127.0.0.1:40443 (for advanced users)\n\n")
		fmt.Fprintf(&gen, "> SNI version of TCP Pass configs. Total: **%d**\n\n", onlyTCPPassCount)
		for i := 1; i <= onlyTCPSNIBatches; i++ {
			fmt.Fprintf(&gen, "TCP Pass SNI Batch %03d:\n```\n%s/config/tcp-pass-sni/batch_%03d.txt\n```\n\n",
				i, repoBase, i)
		}
	}

	existingContent := ""
	if raw, err := os.ReadFile("templates/read.md"); err == nil {
		existing := string(raw)
		if idx := strings.Index(existing, autoGenMarker); idx != -1 {
			existingContent = strings.TrimRight(existing[:idx], "\n\r ")
		} else {
			existingContent = strings.TrimRight(existing, "\n\r ")
		}
	}

	f, err := os.Create("README.md")
	if err != nil {
		fmt.Printf("❌ Cannot write README.md: %v\n", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	if existingContent != "" {
		w.WriteString(existingContent)
		w.WriteString("\n\n")
	}
	w.WriteString(gen.String())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func countBatchFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

func min500(batchIdx, total int) int {
	start := (batchIdx - 1) * 1000
	if start >= total {
		return 0
	}
	end := start + 1000
	if end > total {
		return total - start
	}
	return end - start
}
