package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ── Dispatcher ────────────────────────────────────────────────────────────────

func toXrayOutbound(configURL, protocol string) (string, string) {
	switch protocol {
	case "vmess":
		return parseVMess(configURL)
	case "vless":
		return parseVLess(configURL)
	case "trojan":
		return parseTrojan(configURL)
	case "ss":
		return parseShadowsocks(configURL)
	case "hy2", "hy", "tuic", "ssr":
		return "", "unsupported protocol by xray-core: " + protocol
	}
	return "", "unsupported protocol: " + protocol
}

// ── VMess ─────────────────────────────────────────────────────────────────────

func parseVMessURItoD(data string) (map[string]interface{}, string) {
	u, err := url.Parse("vmess://" + data)
	if err != nil {
		return nil, "uri parse: " + err.Error()
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, "missing uuid"
	}
	host := u.Hostname()
	if host == "" {
		return nil, "missing server"
	}
	portStr := u.Port()
	if portStr == "" {
		portStr = "443"
	}
	q := u.Query()
	sec := strings.ToLower(q.Get("security"))
	tlsVal := ""
	if sec == "tls" || sec == "xtls" {
		tlsVal = "tls"
	}
	d := map[string]interface{}{
		"id": uuid, "add": host, "port": portStr,
		"aid":         first(q.Get("aid"), q.Get("alterId"), "0"),
		"scy":         first(q.Get("encryption"), q.Get("scy"), "auto"),
		"net":         first(q.Get("type"), q.Get("net"), "tcp"),
		"tls":         tlsVal,
		"sni":         first(q.Get("sni"), q.Get("peer"), host),
		"path":        q.Get("path"),
		"host":        q.Get("host"),
		"serviceName": q.Get("serviceName"),
		"fp":          q.Get("fp"),
	}
	return d, ""
}

func parseVMess(raw string) (string, string) {
	data := strings.TrimPrefix(raw, "vmess://")
	if idx := strings.LastIndex(data, "#"); idx != -1 {
		data = data[:idx]
	}
	data = strings.TrimSpace(data)

	var d map[string]interface{}

	if strings.HasPrefix(data, "{") {
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return "", "json: " + err.Error()
		}
	} else {
		var tryB64 []string
		tryB64 = append(tryB64, data)
		if lastAt := strings.LastIndex(data, "@"); lastAt > 0 {
			tryB64 = append(tryB64, data[:lastAt])
		}
		{
			clean := data
			for i, c := range data {
				if c != '+' && c != '/' && c != '=' &&
					c != '-' && c != '_' &&
					!(c >= 'A' && c <= 'Z') &&
					!(c >= 'a' && c <= 'z') &&
					!(c >= '0' && c <= '9') {
					clean = data[:i]
					break
				}
			}
			if clean != data && clean != "" {
				tryB64 = append(tryB64, clean)
			}
		}

		var parsed bool
		var b64Err error
		for _, candidate := range tryB64 {
			var decoded string
			decoded, b64Err = decodeBase64([]byte(candidate))
			if b64Err != nil {
				continue
			}
			var tmp map[string]interface{}
			if json.Unmarshal([]byte(decoded), &tmp) == nil {
				d = tmp
				parsed = true
				break
			}
		}
		if !parsed {
			atIdx := strings.Index(data, "@")
			qIdx := strings.Index(data, "?")
			if atIdx != -1 && (qIdx == -1 || atIdx < qIdx) {
				sanitized := sanitizeProxyURL("vmess://" + data)
				sanitized = strings.TrimPrefix(sanitized, "vmess://")
				var parseErr string
				d, parseErr = parseVMessURItoD(sanitized)
				if parseErr != "" {
					return "", parseErr
				}
			} else {
				if b64Err != nil {
					return "", "base64: " + b64Err.Error()
				}
				return "", "json: invalid vmess payload"
			}
		}
	}

	server := strings.TrimSpace(fmt.Sprintf("%v", d["add"]))
	if server == "" {
		return "", "missing server"
	}
	port, err := toPort(fmt.Sprintf("%v", d["port"]))
	if err != nil {
		return "", "port: " + err.Error()
	}
	uuid := strings.TrimSpace(fmt.Sprintf("%v", d["id"]))
	if uuid == "" {
		return "", "missing uuid"
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
	security := "auto"
	if s, _ := d["scy"].(string); s != "" {
		security = s
	}
	network := "tcp"
	if n, _ := d["net"].(string); n != "" {
		network = strings.ToLower(n)
	}
	switch network {
	case "xhttp", "splithttp", "kcp", "mkcp", "quic":
		return "", "unsupported transport: " + network
	}
	tlsVal := ""
	if tls, _ := d["tls"].(string); tls == "tls" {
		tlsVal = "tls"
	}
	sni := server
	if s, _ := d["sni"].(string); s != "" {
		sni = s
	} else if h, _ := d["host"].(string); h != "" {
		sni = h
	}
	fp := strDefault(d["fp"], "")

	streamSettings := buildXrayStreamSettings(network, strDefault(d["path"], "/"), strDefault(d["host"], ""),
		strDefault(d["serviceName"], strDefault(d["path"], "")), tlsVal, sni, fp, "", "", "")

	return fmt.Sprintf(`{"protocol":"vmess","settings":{"vnext":[{"address":%q,"port":%d,"users":[{"id":%q,"alterId":%d,"security":%q}]}]}%s,"tag":"proxy"}`,
		server, port, uuid, alterId, security, streamSettings), ""
}

// ── VLess ─────────────────────────────────────────────────────────────────────

var singboxSupportedFlows = map[string]bool{
	"":                        true,
	"xtls-rprx-vision":        true,
	"xtls-rprx-vision-udp443": true,
}

func parseVLess(raw string) (string, string) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", "url parse: " + err.Error()
	}
	uuid := normalizeUUID(u.User.Username())
	if uuid == "" {
		return "", "missing uuid"
	}
	server := u.Hostname()
	if server == "" {
		return "", "missing server"
	}
	port, err := toPort(u.Port())
	if err != nil {
		return "", "port: " + err.Error()
	}
	q := u.Query()
	security := strings.TrimSpace(strings.ToLower(q.Get("security")))
	network := strings.ToLower(q.Get("type"))
	if network == "" {
		network = "tcp"
	}
	switch network {
	case "xhttp", "splithttp", "kcp", "mkcp", "quic":
		return "", "unsupported transport: " + network
	}
	sni := first(q.Get("sni"), q.Get("peer"), server)
	flow := q.Get("flow")
	if !singboxSupportedFlows[flow] {
		flow = ""
	}
	fp := first(q.Get("fp"), "chrome")
	alpnStr := q.Get("alpn")
	path := first(q.Get("path"), "/")
	host := q.Get("host")
	grpcService := first(q.Get("serviceName"), q.Get("path"))

	tlsType := ""
	realityPubKey := ""
	realityShortID := ""
	switch security {
	case "tls", "xtls":
		tlsType = "tls"
	case "reality":
		pbk := q.Get("pbk")
		if pbk == "" {
			return "", "reality: missing public key (pbk)"
		}
		tlsType = "reality"
		realityPubKey = pbk
		realityShortID = q.Get("sid")
	case "none", "":
	default:
		return "", "unknown security: " + security
	}

	streamSettings := buildXrayStreamSettings(network, path, host, grpcService, tlsType, sni, fp, realityPubKey, realityShortID, alpnStr)

	flowJSON := ""
	if flow != "" {
		flowJSON = fmt.Sprintf(`,"flow":%q`, flow)
	}

	return fmt.Sprintf(`{"protocol":"vless","settings":{"vnext":[{"address":%q,"port":%d,"users":[{"id":%q,"encryption":"none"%s}]}]}%s,"tag":"proxy"}`,
		server, port, uuid, flowJSON, streamSettings), ""
}

func vlessTLSSettingsUNUSED(security, sni, fp, alpnStr string, q url.Values) (string, string) {
	switch security {
	case "tls", "xtls":
		tlsSettings := fmt.Sprintf(`"security":"tls","tlsSettings":{"serverName":%q,"allowInsecure":true`, sni)
		if fp != "" {
			tlsSettings += fmt.Sprintf(`,"fingerprint":%q`, fp)
		}
		if alpnStr != "" {
			alpn, _ := json.Marshal(strings.Split(alpnStr, ","))
			tlsSettings += fmt.Sprintf(`,"alpn":%s`, alpn)
		}
		return "tls", tlsSettings + "}"
	case "reality":
		pbk := q.Get("pbk")
		if pbk == "" {
			return "", "reality: missing public key (pbk)"
		}
		sid := q.Get("sid")
		realSettings := fmt.Sprintf(`"security":"reality","realitySettings":{"serverName":%q,"fingerprint":%q,"publicKey":%q,"shortId":%q}`,
			sni, first(fp, "chrome"), pbk, sid)
		return "reality", realSettings + "}"
	case "none", "":
		return "", ""
	}
	return "", "unknown security: " + security
}

// ── Trojan ────────────────────────────────────────────────────────────────────

func parseTrojan(raw string) (string, string) {
	u, err := url.Parse(sanitizeProxyURL(raw))
	if err != nil {
		return "", "url parse: " + err.Error()
	}
	password := u.User.Username()
	if password == "" {
		return "", "missing password"
	}
	server := u.Hostname()
	if server == "" {
		return "", "missing server"
	}
	port, err := toPort(u.Port())
	if err != nil {
		return "", "port: " + err.Error()
	}
	q := u.Query()
	sni := first(q.Get("sni"), q.Get("peer"), server)
	fp := first(q.Get("fp"), "")
	network := strings.ToLower(q.Get("type"))
	switch network {
	case "xhttp", "splithttp", "kcp", "mkcp", "quic":
		return "", "unsupported transport: " + network
	}
	path := first(q.Get("path"), "/")
	host := q.Get("host")
	grpcService := first(q.Get("serviceName"), q.Get("path"))

	streamSettings := buildXrayStreamSettings(network, path, host, grpcService, "tls", sni, fp, "", "", "")

	return fmt.Sprintf(`{"protocol":"trojan","settings":{"servers":[{"address":%q,"port":%d,"password":%q}]}%s,"tag":"proxy"}`,
		server, port, password, streamSettings), ""
}

// ── Shadowsocks ───────────────────────────────────────────────────────────────

var singboxSupportedSSCiphers = map[string]bool{
	"aes-128-gcm": true, "aes-192-gcm": true, "aes-256-gcm": true,
	"aes-128-cfb": true, "aes-192-cfb": true, "aes-256-cfb": true,
	"aes-128-ctr": true, "aes-192-ctr": true, "aes-256-ctr": true,
	"chacha20-ietf-poly1305": true, "xchacha20-ietf-poly1305": true,
	"chacha20-ietf":                 true,
	"2022-blake3-aes-128-gcm":       true,
	"2022-blake3-aes-256-gcm":       true,
	"2022-blake3-chacha20-poly1305": true,
	"none": true, "plain": true,
}

// isUUID returns true only for standard UUID format (8-4-4-4-12 hex)
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// isUUIDOrToken checks a string that has ALREADY been decoded/resolved.
// Returns true if it looks like a UUID or has no colon (not a method:password pair).
func isUUIDOrToken(s string) bool {
	if isUUID(s) {
		return true
	}
	// after decoding, a valid SS userinfo must contain ":"
	if !strings.Contains(s, ":") {
		return true
	}
	return false
}

func parseShadowsocks(raw string) (string, string) {
	trimmed := strings.TrimPrefix(raw, "ss://")
	if idx := strings.LastIndex(trimmed, "#"); idx != -1 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSpace(trimmed)

	var method, password, server string
	var port int

	// ── Fast path: standard URI with @ ──────────────────────────────────────────
	fastPathOK := false
	if fastU, err := url.Parse("ss://" + trimmed); err == nil &&
		fastU.User != nil && fastU.Hostname() != "" {
		uname := fastU.User.Username()
		pwd, hasPwd := fastU.User.Password()
		host := fastU.Hostname()
		portStr := fastU.Port()
		if portStr == "" {
			portStr = "443"
		}

		var m, p string
		if hasPwd {
			// user:pass already split by url.Parse — uname is method, pwd is password
			// reject if method looks like UUID
			if isUUID(uname) {
				return "", "not a shadowsocks config (UUID/token-based, likely vless/trojan)"
			}
			m, p = uname, pwd
		} else {
			// uname might be base64(method:pass) OR base64(ss://base64(method:pass))
			if d, derr := decodeBase64([]byte(uname)); derr == nil {
				// double-encoded: decoded starts with ss://
				if strings.HasPrefix(d, "ss://") {
					inner := strings.TrimPrefix(d, "ss://")
					// strip any fragment/query from inner
					if qi := strings.Index(inner, "?"); qi != -1 {
						inner = inner[:qi]
					}
					if fi := strings.LastIndex(inner, "#"); fi != -1 {
						inner = inner[:fi]
					}
					// inner may have @host:port or just be base64(method:pass)
					if atI := strings.LastIndex(inner, "@"); atI != -1 {
						// decode the user part of the inner URI
						innerUser := inner[:atI]
						if d2, e2 := decodeBase64([]byte(innerUser)); e2 == nil && strings.Contains(d2, ":") {
							parts2 := strings.SplitN(d2, ":", 2)
							m, p = parts2[0], parts2[1]
						} else if strings.Contains(innerUser, ":") {
							parts2 := strings.SplitN(innerUser, ":", 2)
							m, p = parts2[0], parts2[1]
						}
					} else {
						// inner is just base64(method:pass), host:port from outer
						if d2, e2 := decodeBase64([]byte(inner)); e2 == nil && strings.Contains(d2, ":") {
							parts2 := strings.SplitN(d2, ":", 2)
							m, p = parts2[0], parts2[1]
						}
					}
				} else if strings.Contains(d, ":") {
					// normal base64(method:pass)
					parts := strings.SplitN(d, ":", 2)
					m, p = parts[0], parts[1]
				}
			}
		}
		if m != "" && host != "" {
			if pVal, perr := toPort(portStr); perr == nil {
				method, password, server, port = m, p, host, pVal
				fastPathOK = true
			}
		}
	}

	// ── Slow path: no @ in trimmed, try full base64 decode ───────────────────────
	if !fastPathOK {
		atIdx := strings.LastIndex(trimmed, "@")
		if atIdx == -1 {
			b64Src := trimmed
			if qi := strings.Index(b64Src, "?"); qi != -1 {
				b64Src = b64Src[:qi]
			}
			decoded, err := decodeBase64([]byte(b64Src))
			if err != nil {
				decoded = trimmed
			}
			// handle double-encoded: decoded starts with ss://
			if strings.HasPrefix(decoded, "ss://") {
				decoded = strings.TrimPrefix(decoded, "ss://")
				if d2, e2 := decodeBase64([]byte(decoded)); e2 == nil {
					decoded = d2
				}
			}
			atIdx2 := strings.LastIndex(decoded, "@")
			if atIdx2 == -1 {
				return "", "missing @"
			}
			userPart := decoded[:atIdx2]
			hostPart := decoded[atIdx2+1:]
			if idx := strings.Index(hostPart, "?"); idx != -1 {
				hostPart = hostPart[:idx]
			}
			// reject if userPart is literally a UUID (not base64)
			if isUUID(userPart) {
				return "", "not a shadowsocks config (UUID/token-based, likely vless/trojan)"
			}
			m, p, s, po, e := ssParseUserAndHost(userPart, hostPart)
			if e != "" {
				return "", e
			}
			method, password, server, port = m, p, s, po
		} else {
			userPart := trimmed[:atIdx]
			hostPart := trimmed[atIdx+1:]
			if idx := strings.Index(hostPart, "?"); idx != -1 {
				hostPart = hostPart[:idx]
			}
			// reject if userPart is literally a UUID (not base64)
			if isUUID(userPart) {
				return "", "not a shadowsocks config (UUID/token-based, likely vless/trojan)"
			}
			m, p, s, po, e := ssParseUserAndHost(userPart, hostPart)
			if e != "" {
				return "", e
			}
			method, password, server, port = m, p, s, po
		}
	}

	method = strings.ToLower(method)
	if !singboxSupportedSSCiphers[method] {
		return "", fmt.Sprintf("unsupported cipher: %s", method)
	}
	if server == "" {
		return "", "missing server"
	}
	return fmt.Sprintf(`{"protocol":"shadowsocks","settings":{"servers":[{"address":%q,"port":%d,"method":%q,"password":%q}]},"tag":"proxy"}`,
		server, port, method, password), ""
}

func ssParseUserAndHost(userPart, hostPart string) (method, password, server string, port int, errMsg string) {
	decodeUser := func(s string) string {
		if d, err := decodeBase64([]byte(s)); err == nil && strings.Contains(d, ":") {
			return d
		}
		if unescaped, err := url.PathUnescape(s); err == nil && unescaped != s {
			if d, err2 := decodeBase64([]byte(unescaped)); err2 == nil && strings.Contains(d, ":") {
				return d
			}
			if strings.Contains(unescaped, ":") {
				return unescaped
			}
		}
		if colonIdx := strings.Index(s, ":"); colonIdx != -1 {
			prefix := s[:colonIdx]
			suffix := s[colonIdx+1:]
			if d, err := decodeBase64([]byte(prefix)); err == nil && !strings.Contains(d, ":") {
				return d + ":" + suffix
			}
		}
		return s
	}

	decoded := decodeUser(userPart)
	// after decode, reject UUIDs and strings with no colon
	if isUUID(decoded) {
		return "", "", "", 0, "not a shadowsocks config (UUID/token-based)"
	}
	if !strings.Contains(decoded, ":") {
		return "", "", "", 0, "invalid user info (no method:password separator)"
	}
	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", "", 0, "invalid user info"
	}
	method = strings.TrimSpace(parts[0])
	password = parts[1]

	hostPart = strings.TrimSpace(hostPart)
	var portStr string
	if strings.HasPrefix(hostPart, "[") {
		closeBracket := strings.Index(hostPart, "]")
		if closeBracket == -1 {
			return "", "", "", 0, "invalid IPv6 host"
		}
		server = hostPart[1:closeBracket]
		rest := hostPart[closeBracket+1:]
		if strings.HasPrefix(rest, ":") {
			portStr = rest[1:]
		} else {
			portStr = "443"
		}
	} else {
		lastColon := strings.LastIndex(hostPart, ":")
		if lastColon == -1 {
			return "", "", "", 0, "missing port"
		}
		server = hostPart[:lastColon]
		portStr = hostPart[lastColon+1:]
	}

	if idx := strings.IndexFunc(portStr, func(r rune) bool { return r < '0' || r > '9' }); idx != -1 {
		portStr = portStr[:idx]
	}
	portStr = strings.TrimSpace(portStr)
	p, err := toPort(portStr)
	if err != nil {
		return "", "", "", 0, "port: " + err.Error()
	}
	return method, password, server, p, ""
}

// ── Transport builder (xray streamSettings) ───────────────────────────────────

func buildXrayStreamSettings(network, path, host, grpcService, tlsType, sni, fp string, realityPubKey, realityShortID, alpnStr string) string {
	if path == "" {
		path = "/"
	}

	networkSettings := ""
	switch network {
	case "ws":
		wsSettings := fmt.Sprintf(`"wsSettings":{"path":%q`, path)
		if host != "" {
			wsSettings += fmt.Sprintf(`,"headers":{"Host":%q}`, host)
		}
		wsSettings += "}"
		networkSettings = fmt.Sprintf(`,"network":"ws",%s`, wsSettings)
	case "grpc":
		networkSettings = fmt.Sprintf(`,"network":"grpc","grpcSettings":{"serviceName":%q}`, grpcService)
	case "h2", "http":
		h2Settings := fmt.Sprintf(`"httpSettings":{"path":%q`, path)
		if host != "" {
			h2Settings += fmt.Sprintf(`,"host":[%q]`, host)
		}
		h2Settings += "}"
		networkSettings = fmt.Sprintf(`,"network":"h2",%s`, h2Settings)
	case "httpupgrade":
		huSettings := fmt.Sprintf(`"httpupgradeSettings":{"path":%q`, path)
		if host != "" {
			huSettings += fmt.Sprintf(`,"host":%q`, host)
		}
		huSettings += "}"
		networkSettings = fmt.Sprintf(`,"network":"httpupgrade",%s`, huSettings)
	case "splithttp", "xhttp":
		stSettings := fmt.Sprintf(`"splithttpSettings":{"path":%q`, path)
		if host != "" {
			stSettings += fmt.Sprintf(`,"host":%q`, host)
		}
		stSettings += "}"
		networkSettings = fmt.Sprintf(`,"network":"splithttp",%s`, stSettings)
	}

	tlsSettings := ""
	switch tlsType {
	case "tls":
		tlsSettings = fmt.Sprintf(`,"security":"tls","tlsSettings":{"serverName":%q,"allowInsecure":true`, sni)
		if fp != "" {
			tlsSettings += fmt.Sprintf(`,"fingerprint":%q`, fp)
		}
		if alpnStr != "" {
			alpn, _ := json.Marshal(strings.Split(alpnStr, ","))
			tlsSettings += fmt.Sprintf(`,"alpn":%s`, alpn)
		}
		tlsSettings += "}"
	case "reality":
		tlsSettings = fmt.Sprintf(`,"security":"reality","realitySettings":{"serverName":%q,"fingerprint":%q,"publicKey":%q,"shortId":%q}`,
			sni, first(fp, "chrome"), realityPubKey, realityShortID)
	}

	if networkSettings == "" && tlsSettings == "" {
		return ""
	}
	// networkSettings starts with ",", tlsSettings starts with ","
	// Trim leading comma from tlsSettings if networkSettings is empty
	inner := strings.TrimPrefix(networkSettings, ",")
	if inner == "" {
		inner = strings.TrimPrefix(tlsSettings, ",")
	} else {
		inner += tlsSettings
	}
	return fmt.Sprintf(`,"streamSettings":{%s}`, inner)
}

// ── URL helpers ───────────────────────────────────────────────────────────────

func sanitizeProxyURL(raw string) string {
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	raw = strings.ReplaceAll(raw, "&lt;", "<")
	raw = strings.ReplaceAll(raw, "&gt;", ">")
	raw = strings.ReplaceAll(raw, "&quot;", `"`)
	raw = strings.ReplaceAll(raw, "&#39;", "'")

	raw = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, raw)

	schemeIdx := strings.Index(raw, "://")
	if schemeIdx == -1 {
		return raw
	}
	scheme := raw[:schemeIdx+3]
	rest := raw[schemeIdx+3:]

	const maxIter = 20
	for i := 0; i < maxIter; i++ {
		if !strings.Contains(rest, "%") {
			break
		}
		decoded, err := url.QueryUnescape(rest)
		if err != nil {
			decoded, err = url.PathUnescape(rest)
			if err != nil || decoded == rest {
				break
			}
		}
		if decoded == rest {
			break
		}
		if strings.ContainsAny(decoded, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f") {
			break
		}
		rest = decoded
	}

	frag := ""
	if fragIdx := strings.LastIndex(rest, "#"); fragIdx != -1 {
		frag = rest[fragIdx:]
		rest = rest[:fragIdx]
	}
	query := ""
	if queryIdx := strings.Index(rest, "?"); queryIdx != -1 {
		query = rest[queryIdx:]
		rest = rest[:queryIdx]
	}
	lastAt := strings.LastIndex(rest, "@")
	if lastAt == -1 {
		return scheme + rest + query + frag
	}
	return scheme + encodeUserInfo(rest[:lastAt]) + "@" + rest[lastAt+1:] + query + frag
}

func normalizeUUID(u string) string {
	if len(u) == 32 {
		allHex := true
		for _, c := range u {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				allHex = false
				break
			}
		}
		if allHex {
			return u[0:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:32]
		}
	}
	return u
}

func encodeUserInfo(s string) string {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '.' || b == '_' || b == '~' || b == '!' || b == '$' ||
			b == '&' || b == '\'' || b == '(' || b == ')' || b == '*' || b == '+' ||
			b == ',' || b == ';' || b == '=' || b == ':' {
			buf.WriteByte(b)
		} else {
			fmt.Fprintf(&buf, "%%%02X", b)
		}
	}
	return buf.String()
}

// ── coreIdentity ─────────────────────────────────────────────────────────────

func coreIdentity(line, protocol string) string {
	switch protocol {
	case "vmess":
		data := strings.TrimPrefix(line, "vmess://")
		if idx := strings.LastIndex(data, "#"); idx != -1 {
			data = data[:idx]
		}
		data = strings.TrimSpace(data)

		if !strings.HasPrefix(data, "{") {
			if decoded, err := decodeBase64([]byte(data)); err == nil {
				var d struct {
					Add  string      `json:"add"`
					Port interface{} `json:"port"`
					ID   string      `json:"id"`
				}
				if json.Unmarshal([]byte(decoded), &d) == nil && d.Add != "" {
					return fmt.Sprintf("vmess://%s:%v#%s", d.Add, d.Port, d.ID)
				}
			}
		}

		if strings.HasPrefix(data, "{") {
			var d struct {
				Add  string      `json:"add"`
				Port interface{} `json:"port"`
				ID   string      `json:"id"`
			}
			if json.Unmarshal([]byte(data), &d) == nil && d.Add != "" {
				return fmt.Sprintf("vmess://%s:%v#%s", d.Add, d.Port, d.ID)
			}
		}

		if atIdx := strings.Index(data, "@"); atIdx != -1 {
			u, err := url.Parse("vmess://" + data)
			if err == nil && u.Hostname() != "" {
				return fmt.Sprintf("vmess://%s:%s#%s", u.Hostname(), u.Port(), u.User.Username())
			}
		}

		return line
	case "ssr":
		data := strings.TrimPrefix(line, "ssr://")
		if idx := strings.LastIndex(data, "#"); idx != -1 {
			data = data[:idx]
		}
		decoded, err := decodeBase64([]byte(strings.TrimSpace(data)))
		if err != nil {
			return line
		}
		parts := strings.SplitN(decoded, ":", 6)
		if len(parts) < 2 {
			return line
		}
		return fmt.Sprintf("ssr://%s:%s", parts[0], parts[1])
	default:
		u, err := url.Parse(sanitizeProxyURL(line))
		if err != nil || u.Hostname() == "" {
			return line
		}
		return fmt.Sprintf("%s://%s@%s:%s", protocol, u.User.String(), u.Hostname(), u.Port())
	}
}

// ── renameTo ──────────────────────────────────────────────────────────────────

func renameTo(config, protocol, newName string) string {
	switch protocol {
	case "vmess":
		data := strings.TrimPrefix(config, "vmess://")
		fragIdx := strings.LastIndex(data, "#")
		if fragIdx != -1 {
			data = data[:fragIdx]
		}
		data = strings.TrimSpace(data)

		isURI := false
		if strings.HasPrefix(data, "{") {
			isURI = false
		} else {
			decoded, err := decodeBase64([]byte(data))
			if err == nil {
				var tmp map[string]interface{}
				if json.Unmarshal([]byte(decoded), &tmp) == nil {
					tmp["ps"] = newName
					keys := make([]string, 0, len(tmp))
					for k := range tmp {
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
						vj, _ := json.Marshal(tmp[k])
						buf.Write(kj)
						buf.WriteByte(':')
						buf.Write(vj)
					}
					buf.WriteByte('}')
					return "vmess://" + base64.StdEncoding.EncodeToString(buf.Bytes())
				}
			}
			if atIdx := strings.Index(data, "@"); atIdx != -1 {
				isURI = true
			}
		}

		if !isURI {
			var d map[string]interface{}
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				return config
			}
			d["ps"] = newName
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
			return "vmess://" + base64.StdEncoding.EncodeToString(buf.Bytes())
		}

		return "vmess://" + data + "#" + url.PathEscape(newName)

	default:
		if idx := strings.Index(config, "#"); idx != -1 {
			return config[:idx] + "#" + url.PathEscape(newName)
		}
		return config + "#" + url.PathEscape(newName)
	}
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func toPort(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return n, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func strDefault(v interface{}, def string) string {
	if v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}
