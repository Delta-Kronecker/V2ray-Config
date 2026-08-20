package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ── Farg (custom xray JSON) output ────────────────────────────────────────────
// Writes compact xray-core JSON configs (with finalmask fragment) under
// config/farg/. Structure follows the short "F" fragment layout.

type fargConfig struct {
	DNS       any   `json:"dns"`
	Inbounds  []any `json:"inbounds"`
	Outbounds []any `json:"outbounds"`
	Routing   any   `json:"routing"`
}

type fargOutbound struct {
	Protocol       string       `json:"protocol"`
	Settings       fargSettings `json:"settings"`
	StreamSettings *fargStream  `json:"streamSettings,omitempty"`
	Tag            string       `json:"tag"`
}

type fargSettings struct {
	VNext   []fargVNext  `json:"vnext,omitempty"`
	Servers []fargServer `json:"servers,omitempty"`
}

type fargVNext struct {
	Address string     `json:"address"`
	Port    int        `json:"port"`
	Users   []fargUser `json:"users"`
}

type fargUser struct {
	ID         string `json:"id"`
	AlterID    *int   `json:"alterId,omitempty"`
	Security   string `json:"security,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	Flow       string `json:"flow,omitempty"`
}

type fargServer struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Password string `json:"password,omitempty"`
	Method   string `json:"method,omitempty"`
}

type fargStream struct {
	Network             string                   `json:"network,omitempty"`
	WSSettings          *fargWSSettings          `json:"wsSettings,omitempty"`
	GRPCSettings        *fargGRPCSettings        `json:"grpcSettings,omitempty"`
	HTTPSettings        *fargHTTPSettings        `json:"httpSettings,omitempty"`
	HTTPUpgradeSettings *fargHTTPUpgradeSettings `json:"httpupgradeSettings,omitempty"`
	SplitHTTPSettings   *fargSplitHTTPSettings   `json:"splithttpSettings,omitempty"`
	Security            string                   `json:"security,omitempty"`
	TLSSettings         *fargTLSSettings         `json:"tlsSettings,omitempty"`
	RealitySettings     *fargRealitySettings     `json:"realitySettings,omitempty"`
	Sockopt             *fargSockopt             `json:"sockopt,omitempty"`
	FinalMask           *fargFinalMask           `json:"finalmask,omitempty"`
}

type fargWSSettings struct {
	Host string `json:"host,omitempty"`
	Path string `json:"path"`
}

type fargGRPCSettings struct {
	ServiceName string `json:"serviceName"`
}

type fargHTTPSettings struct {
	Path string   `json:"path"`
	Host []string `json:"host,omitempty"`
}

type fargHTTPUpgradeSettings struct {
	Path string `json:"path"`
	Host string `json:"host,omitempty"`
}

type fargSplitHTTPSettings struct {
	Path string `json:"path"`
	Host string `json:"host,omitempty"`
}

type fargTLSSettings struct {
	ServerName  string   `json:"serverName"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
}

type fargRealitySettings struct {
	ServerName  string `json:"serverName"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
}

type fargSockopt struct {
	DomainStrategy string `json:"domainStrategy"`
}

type fargFinalMask struct {
	TCP []fargFragment `json:"tcp"`
}

type fargFragment struct {
	Type     string               `json:"type"`
	Settings fargFragmentSettings `json:"settings"`
}

type fargFragmentSettings struct {
	Packets  string `json:"packets"`
	Length   string `json:"length"`
	Delay    string `json:"delay"`
	MaxSplit string `json:"maxSplit"`
}

type fargDNS struct {
	Servers       []any  `json:"servers"`
	QueryStrategy string `json:"queryStrategy"`
}

type fargRouting struct {
	DomainStrategy string     `json:"domainStrategy"`
	Rules          []fargRule `json:"rules"`
}

type fargRule struct {
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	OutboundTag string   `json:"outboundTag"`
	Type        string   `json:"type"`
}

// ── writeFargFiles ────────────────────────────────────────────────────────────

func writeFargFiles(results []configResult) {
	supported := map[string]bool{"vless": true, "trojan": true, "vmess": true, "ss": true}
	byProto := make(map[string][]fargConfig)
	var all []fargConfig

	for _, r := range results {
		if !supported[r.proto] {
			continue
		}
		entry, ok := buildFargConfig(r.line, r.proto)
		if !ok {
			continue
		}
		all = append(all, entry)
		byProto[r.proto] = append(byProto[r.proto], entry)
	}

	if len(all) == 0 {
		return
	}

	writeJSONArray(filepath.Join("config", "farg", "all_configs.json"), all)
	for proto, entries := range byProto {
		writeJSONArray(filepath.Join("config", "farg", "protocols", proto+".json"), entries)
	}
	numBatches := writeFargBatches(all)
	fmt.Printf(" Farg: wrote %d xray configs into config/farg (all) + %d protocol files + %d batch files\n",
		len(all), len(byProto), numBatches)
}

func writeFargBatches(entries []fargConfig) int {
	const batchSize = 500

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	shuffled := make([]fargConfig, len(entries))
	copy(shuffled, entries)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	numBatches := (len(shuffled) + batchSize - 1) / batchSize
	for i := 0; i < numBatches; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(shuffled) {
			end = len(shuffled)
		}
		writeJSONArray(fmt.Sprintf("config/farg/batches/batch_%03d.json", i+1), shuffled[start:end])
	}
	return numBatches
}

func buildFargConfig(line, proto string) (fargConfig, bool) {
	outJSON, parseErr := toXrayOutbound(line, proto)
	if parseErr != "" || outJSON == "" {
		return fargConfig{}, false
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(outJSON), &out); err != nil {
		return fargConfig{}, false
	}

	settings, server := buildFargSettings(out, proto)
	if server == "" {
		return fargConfig{}, false
	}

	outProto := fargStr(out, "protocol")
	if outProto == "" {
		outProto = proto
	}
	stream := buildFargStream(out, server)

	return fargConfig{
		DNS: buildFargDNS(),
		Inbounds: []any{
			buildFargMixedIn(),
		},
		Outbounds: []any{
			fargOutbound{Protocol: outProto, Settings: settings, StreamSettings: stream, Tag: "proxy"},
			buildFargDirect(),
		},
		Routing: buildFargRouting(),
	}, true
}

// ── Settings / credentials extraction ─────────────────────────────────────────

func buildFargSettings(out map[string]interface{}, proto string) (fargSettings, string) {
	settings := fargMap(out, "settings")
	if settings == nil {
		return fargSettings{}, ""
	}
	switch proto {
	case "vless", "vmess":
		vnext := fargList(settings, "vnext")
		if len(vnext) == 0 {
			return fargSettings{}, ""
		}
		v0, _ := vnext[0].(map[string]interface{})
		if v0 == nil {
			return fargSettings{}, ""
		}
		server := fargStr(v0, "address")
		port := fargInt(v0, "port")
		u := fargUser{ID: fargStr(v0, "id")}
		if users := fargList(v0, "users"); len(users) > 0 {
			if um, ok := users[0].(map[string]interface{}); ok {
				u.ID = fargStr(um, "id")
				if proto == "vmess" {
					aid := fargInt(um, "alterId")
					u.AlterID = &aid
					u.Security = first(fargStr(um, "security"), "auto")
				} else {
					u.Encryption = first(fargStr(um, "encryption"), "none")
					u.Flow = fargStr(um, "flow")
				}
			}
		}
		return fargSettings{VNext: []fargVNext{{Address: server, Port: port, Users: []fargUser{u}}}}, server
	case "trojan", "ss":
		servers := fargList(settings, "servers")
		if len(servers) == 0 {
			return fargSettings{}, ""
		}
		s0, _ := servers[0].(map[string]interface{})
		if s0 == nil {
			return fargSettings{}, ""
		}
		s := fargServer{
			Address:  fargStr(s0, "address"),
			Port:     fargInt(s0, "port"),
			Password: fargStr(s0, "password"),
			Method:   fargStr(s0, "method"),
		}
		return fargSettings{Servers: []fargServer{s}}, s.Address
	}
	return fargSettings{}, ""
}

// ── streamSettings builder (finalmask) ────────────────────────────────────────

func buildFargStream(out map[string]interface{}, server string) *fargStream {
	stream := fargMap(out, "streamSettings")
	if stream == nil {
		stream = map[string]interface{}{}
	}

	network := fargStr(stream, "network")

	wsPath, wsHost := "/", ""
	if ws := fargMap(stream, "wsSettings"); ws != nil {
		wsPath = first(fargStr(ws, "path"), "/")
		wsHost = fargStr(ws, "host")
		if wsHost == "" {
			if h := fargMap(ws, "headers"); h != nil {
				wsHost = first(fargStr(h, "Host"), fargStr(h, "host"))
			}
		}
	}

	grpcService := ""
	if g := fargMap(stream, "grpcSettings"); g != nil {
		grpcService = fargStr(g, "serviceName")
	}

	h2Path, h2Host := "/", ""
	if h := fargMap(stream, "httpSettings"); h != nil {
		h2Path = first(fargStr(h, "path"), "/")
		if hosts := fargList(h, "host"); len(hosts) > 0 {
			if hs, ok := hosts[0].(string); ok {
				h2Host = hs
			}
		}
	}

	huPath, huHost := "/", ""
	if h := fargMap(stream, "httpupgradeSettings"); h != nil {
		huPath = first(fargStr(h, "path"), "/")
		huHost = fargStr(h, "host")
	}

	shPath, shHost := "/", ""
	if h := fargMap(stream, "splithttpSettings"); h != nil {
		shPath = first(fargStr(h, "path"), "/")
		shHost = fargStr(h, "host")
	}

	security := fargStr(stream, "security")

	res := &fargStream{Network: network}

	switch network {
	case "ws":
		res.WSSettings = &fargWSSettings{Path: wsPath, Host: wsHost}
	case "grpc":
		res.GRPCSettings = &fargGRPCSettings{ServiceName: grpcService}
	case "h2", "http":
		hs := &fargHTTPSettings{Path: h2Path}
		if h2Host != "" {
			hs.Host = []string{h2Host}
		}
		res.HTTPSettings = hs
	case "httpupgrade":
		res.HTTPUpgradeSettings = &fargHTTPUpgradeSettings{Path: huPath, Host: huHost}
	case "splithttp", "xhttp":
		res.SplitHTTPSettings = &fargSplitHTTPSettings{Path: shPath, Host: shHost}
	}

	switch security {
	case "tls":
		res.Security = "tls"
		t := &fargTLSSettings{
			ServerName:  first(fargStr(fargMap(stream, "tlsSettings"), "serverName"), server),
			Fingerprint: first(fargStr(fargMap(stream, "tlsSettings"), "fingerprint"), "chrome"),
		}
		if alpn := fargStringList(fargMap(stream, "tlsSettings")["alpn"]); len(alpn) > 0 {
			t.ALPN = alpn
		} else {
			t.ALPN = []string{"http/1.1"}
		}
		res.TLSSettings = t
	case "reality":
		res.Security = "reality"
		r := fargMap(stream, "realitySettings")
		res.RealitySettings = &fargRealitySettings{
			ServerName:  first(fargStr(r, "serverName"), server),
			Fingerprint: first(fargStr(r, "fingerprint"), "chrome"),
			PublicKey:   fargStr(r, "publicKey"),
			ShortID:     fargStr(r, "shortId"),
		}
	}

	res.Sockopt = &fargSockopt{
		DomainStrategy: "UseIP",
	}
	res.FinalMask = &fargFinalMask{
		TCP: []fargFragment{{
			Type: "fragment",
			Settings: fargFragmentSettings{
				Packets:  "1-3",
				Length:   "5-40",
				Delay:    "1",
				MaxSplit: "5",
			},
		}},
	}

	return res
}

// ── DNS / inbounds / routing builders ─────────────────────────────────────────

func buildFargDNS() fargDNS {
	return fargDNS{
		Servers: []any{
			"fakedns",
			map[string]any{"address": "https://8.8.8.8/dns-query", "tag": "remote-dns"},
		},
		QueryStrategy: "UseIP",
	}
}

func buildFargMixedIn() map[string]any {
	return map[string]any{
		"listen":   "127.0.0.1",
		"port":     10808,
		"protocol": "mixed",
		"settings": map[string]any{"auth": "noauth", "udp": true},
		"sniffing": map[string]any{
			"destOverride": []string{"http", "tls", "fakedns"},
			"enabled":      true,
			"routeOnly":    true,
		},
		"tag": "mixed-in",
	}
}

func buildFargDirect() map[string]any {
	return map[string]any{
		"protocol": "freedom",
		"tag":      "direct",
	}
}

func buildFargRouting() fargRouting {
	return fargRouting{
		DomainStrategy: "IPIfNonMatch",
		Rules: []fargRule{
			{Domain: []string{"geosite:private"}, OutboundTag: "direct", Type: "field"},
			{IP: []string{"geoip:private"}, OutboundTag: "direct", Type: "field"},
		},
	}
}

// ── Low-level writer + helpers ────────────────────────────────────────────────

func writeJSONArray(path string, entries any) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf(" Cannot write %s: %v\n", path, err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(entries); err != nil {
		fmt.Printf(" Cannot encode %s: %v\n", path, err)
	}
}

func fargMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]interface{})
	return v
}

func fargStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func fargInt(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func fargList(m map[string]interface{}, key string) []interface{} {
	if m == nil {
		return nil
	}
	v, _ := m[key].([]interface{})
	return v
}

func fargStringList(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
