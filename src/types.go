package main

import (
	"net/http"
	"sync"
	"time"
)

// ── Settings ─────────────────────────────────────────────────────────────────

type Settings struct {
	Validation    ValidationSettings `json:"validation"`
	Protocols     []string           `json:"protocols"`
	ProtocolOrder []string           `json:"protocol_order"`
	Base64Links   []string           `json:"base64_links"`
	TextLinks     []string           `json:"text_links"`
	Output        OutputSettings     `json:"output"`
}

type ValidationSettings struct {
	NumWorkers             int      `json:"num_workers"`
	GlobalTimeoutSec       float64  `json:"global_timeout_sec"`
	XrayStartTimeoutMs    int      `json:"xray_start_timeout_ms"`
	XrayStartIntervalMs   int      `json:"xray_start_interval_ms"`
	HTTPRequestTimeoutMs   int      `json:"http_request_timeout_ms"`
	HTTPDialTimeoutMs      int      `json:"http_dial_timeout_ms"`
	HTTPResponseTimeoutMs  int      `json:"http_response_timeout_ms"`
	PortCheckTimeoutMs     int      `json:"port_check_timeout_ms"`
	MaxRetries             int      `json:"max_retries"`
	ValidationRetries      int      `json:"validation_retries"`
	BasePort               int      `json:"base_port"`
	BatchRestMs            int      `json:"batch_rest_ms"`
	ProcessKillWaitMs      int      `json:"process_kill_wait_ms"`
	FetchRetryCount        int      `json:"fetch_retry_count"`
	FetchRetryDelayMs      int      `json:"fetch_retry_delay_ms"`
	VlessSpecificTimeoutMs int      `json:"vless_specific_timeout_ms"`
	TestURLs               []string `json:"test_urls"`

	// TCP ping settings (pre-validation)
	TCPPingTimeoutMs  int `json:"tcp_ping_timeout_ms"`
	TCPPingRetries    int `json:"tcp_ping_retries"`
	TCPPingWorkers    int `json:"tcp_ping_workers"`
	TCPPingBatchSize  int `json:"tcp_ping_batch_size"`
	TCPPingBatchRestMs int `json:"tcp_ping_batch_rest_ms"`
}

type OutputSettings struct {
	ConfigName   string `json:"config_name"`
	MainFile     string `json:"main_file"`
	ProtocolsDir string `json:"protocols_dir"`
}

// ── Clash proxy types ─────────────────────────────────────────────────────────

type ClashWSOpts struct {
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

type ClashGRPCOpts struct {
	ServiceName string `yaml:"grpc-service-name"`
}

type ClashH2Opts struct {
	Path []string `yaml:"path"`
	Host []string `yaml:"host"`
}

type ClashHTTPOpts struct {
	Method  string              `yaml:"method"`
	Path    []string            `yaml:"path"`
	Headers map[string][]string `yaml:"headers"`
}

type ClashHTTPUpgradeOpts struct {
	Path    string            `yaml:"path"`
	Host    string            `yaml:"host"`
	Headers map[string]string `yaml:"headers"`
}

type ClashSplitHTTPOpts struct {
	Path string `yaml:"path"`
	Host string `yaml:"host"`
}

type ClashRealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

type ClashProxy struct {
	Name   string      `yaml:"name"`
	Type   string      `yaml:"type"`
	Server string      `yaml:"server"`
	Port   interface{} `yaml:"port"`

	UUID     string      `yaml:"uuid"`
	Password string      `yaml:"password"`
	AlterID  interface{} `yaml:"alterId"`
	Cipher   string      `yaml:"cipher"`

	TLS            bool     `yaml:"tls"`
	SkipCertVerify bool     `yaml:"skip-cert-verify"`
	SNI            string   `yaml:"servername"`
	SniAlt         string   `yaml:"sni"`
	Fingerprint    string   `yaml:"client-fingerprint"`
	FingerprintAlt string   `yaml:"fingerprint"`
	ALPN           []string `yaml:"alpn"`

	Network         string                `yaml:"network"`
	WSOpts          *ClashWSOpts          `yaml:"ws-opts"`
	GRPCOpts        *ClashGRPCOpts        `yaml:"grpc-opts"`
	H2Opts          *ClashH2Opts          `yaml:"h2-opts"`
	HTTPOpts        *ClashHTTPOpts        `yaml:"http-opts"`
	HTTPUpgradeOpts *ClashHTTPUpgradeOpts `yaml:"httpupgrade-opts"`
	SplitHTTPOpts   *ClashSplitHTTPOpts   `yaml:"splithttp-opts"`

	Flow        string            `yaml:"flow"`
	RealityOpts *ClashRealityOpts `yaml:"reality-opts"`

	Plugin     string                 `yaml:"plugin"`
	PluginOpts map[string]interface{} `yaml:"plugin-opts"`

	AuthStr    string      `yaml:"auth-str"`
	AuthStrAlt string      `yaml:"auth_str"`
	Auth       string      `yaml:"auth"`
	Up         interface{} `yaml:"up"`
	Down       interface{} `yaml:"down"`

	Obfs         string `yaml:"obfs"`
	ObfsPassword string `yaml:"obfs-password"`

	Token string `yaml:"token"`

	Protocol      string `yaml:"protocol"`
	ObfsParam     string `yaml:"obfs-param"`
	ProtocolParam string `yaml:"protocol-param"`
}

type clashConfigWrapper struct {
	Proxies    []ClashProxy `yaml:"proxies"`
	ProxiesOld []ClashProxy `yaml:"Proxy"`
	ProxiesP   []ClashProxy `yaml:"proxy"`
}

// ── Result types ──────────────────────────────────────────────────────────────

type fetchResult struct {
	url        string
	content    string
	statusCode int
	err        error
}

type validationResult struct {
	passed     bool
	latency    time.Duration
	failReason string
}

type configResult struct {
	line  string
	proto string
}

// ── Protocol stats ────────────────────────────────────────────────────────────

type protoStat struct {
	mu         sync.Mutex
	tested     int
	passed     int
	parseFail  int
	startFail  int
	connFail   int
	totalLatMs int64
}

type failDetail struct {
	mu      sync.Mutex
	reasons map[string]int
	samples map[string][]string
}

// ── Globals ───────────────────────────────────────────────────────────────────

var cfg Settings

var fetchHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		DisableKeepAlives:     false,
	},
}

// SNI replacement constants
const sniHost = "127.0.0.1"
const sniPort = 40443
