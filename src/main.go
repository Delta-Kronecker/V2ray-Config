package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func loadSettings(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cfg)
}

func main() {
	gNameCountMu.Lock()
	gNameCount = make(map[string]int)
	gNameCountMu.Unlock()

	if err := loadSettings("data/settings.json"); err != nil {
		fmt.Printf("❌ Failed to load settings.json: %v\n", err)
		os.Exit(1)
	}

	if err := loadClashBase("templates/clash_base.yaml"); err != nil {
		fmt.Printf("⚠️  clash_base.yaml: %v\n", err)
	}
	if err := loadClashBaseAdvanced("templates/clash_base_advanced.yaml"); err != nil {
		fmt.Printf("⚠️  clash_base_advanced.yaml: %v\n", err)
	}

	var logErr error
	gLog, logErr = newLogger("logs")
	if logErr != nil {
		fmt.Printf("⚠️  Log file error: %v\n", logErr)
	}
	if gLog != nil {
		defer gLog.close()
	}

	start := time.Now()
	v := cfg.Validation
	fmt.Println("🚀 Starting V2Ray config aggregator...")
	fmt.Printf("⚙️  Workers=%d | Timeout=%.0fs | Retries=%d\n",
		v.NumWorkers, v.GlobalTimeoutSec, v.MaxRetries)

	// TCP ping defaults info
	tcpTimeout := v.TCPPingTimeoutMs
	if tcpTimeout <= 0 {
		tcpTimeout = 3000
	}
	tcpRetries := v.TCPPingRetries
	if tcpRetries < 0 {
		tcpRetries = 0
	}
	fmt.Printf("🔌 TCP-Ping timeout=%dms | retries=%d\n", tcpTimeout, tcpRetries)

	if err := prepareOutputDirs(); err != nil {
		fmt.Printf("❌ Error creating directories: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("📡 Fetching configurations from sources...")
	var allConfigs []string
	var failedLinks []string
	subURLs := loadSubsFromFile("data/sub.txt")
	if len(subURLs) > 0 {
		fmt.Printf("📋 Loaded %d sources from sub.txt\n", len(subURLs))
		allConfigs, failedLinks = fetchAllFromSubs(subURLs)
	} else {
		fmt.Println("⚠️  sub.txt not found or empty — falling back to settings.json links")
		allConfigs, failedLinks = fetchAll(cfg.Base64Links, cfg.TextLinks)
	}
	fmt.Printf("📊 Total fetched: %d | Failed sources: %d\n", len(allConfigs), len(failedLinks))

	if gLog != nil {
		gLog.logStart(len(allConfigs), len(failedLinks))
	}

	fmt.Println("🔍 Validating...")
	results, onlyTCPPass := validateAll(allConfigs)

	elapsed := time.Since(start).Seconds()
	fmt.Printf("\n✅ Valid configurations: %d\n", len(results))
	fmt.Printf("🔶 TCP Pass (advanced): %d\n", len(onlyTCPPass))

	if gLog != nil {
		gLog.logSummary(elapsed, results, failedLinks)
	}

	writeOutputFiles(results, onlyTCPPass)
	writeSummary(results, failedLinks, elapsed, len(allConfigs), len(onlyTCPPass))
	fmt.Println("✅ Done!")
}
