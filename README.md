# V2ray-Config — Delta-Kronecker

> Last updated: 2026-02-19 09:42 UTC

## About

Automatically updated every 24 hours. Configs are fetched from multiple sources, validated with sing-box, deduplicated, and published.

**Supported protocols:** VMess · VLess · Trojan · Shadowsocks · Hysteria2 · Hysteria · TUIC

Clash configs are Iran-optimized with layered DNS, GeoIP rules, and intelligent proxy groups.

---

## Statistics

### Per-Protocol Input & Output

| Protocol | Input (unique) | Output (valid) | Pass Rate |
|---|---|---|---|
| VMESS | 240 | 66 | 27.5% |
| VLESS | 507 | 106 | 20.9% |
| TROJAN | 762 | 142 | 18.6% |
| SS | 207 | 70 | 33.8% |
| HY2 | 16 | 1 | 6.2% |
| HY | 0 | 0 | 0.0% |
| TUIC | 2 | 0 | 0.0% |
| **Total** | **1734** | **385** | **22.2%** |

| Metric | Value |
|---|---|
| Raw fetched lines | 2327 |
| Unique after dedup | 1734 |
| Valid configs | 385 |
| Processing time | 32.18s |

---

## Main Files

### V2ray — All Configs

| File | Link |
|---|---|
| All configs (txt) | [all_configs.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/all_configs.txt) |

### V2ray — By Protocol

| Protocol | Count | Link |
|---|---|---|
| VMESS | 66 | [vmess.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess.txt) |
| VLESS | 106 | [vless.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless.txt) |
| TROJAN | 142 | [trojan.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan.txt) |
| SS | 70 | [ss.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss.txt) |
| HY2 | 1 | [hy2.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2.txt) |

### Clash — Standard Structure

Groups: **PROXY** (selector) → **Load-Balance** · **Auto** · **Fallback**

| File | Link |
|---|---|
| clash.yaml (all protocols) | [clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash.yaml) |
| vmess_clash.yaml | [vmess_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess_clash.yaml) |
| vless_clash.yaml | [vless_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless_clash.yaml) |
| trojan_clash.yaml | [trojan_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan_clash.yaml) |
| ss_clash.yaml | [ss_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss_clash.yaml) |
| hy2_clash.yaml | [hy2_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2_clash.yaml) |

### Clash — Advanced Structure (Recommended)

Groups: **PROXY-BEST** → SCEN-OPEN · SCEN-CDN → LB-ALL · LB-CDN · FAST-ALL · FAST-CDN · UDP-BEST · MANUAL

| File | Link |
|---|---|
| clash_advanced.yaml (all protocols) | [clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash_advanced.yaml) |
| vmess_clash_advanced.yaml | [vmess_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess_clash_advanced.yaml) |
| vless_clash_advanced.yaml | [vless_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless_clash_advanced.yaml) |
| trojan_clash_advanced.yaml | [trojan_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan_clash_advanced.yaml) |
| ss_clash_advanced.yaml | [ss_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss_clash_advanced.yaml) |
| hy2_clash_advanced.yaml | [hy2_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2_clash_advanced.yaml) |

---

## Batch Files — Random 500-Config Groups

> Each file contains 500 randomly selected configs from all protocols.

### V2ray Batches

| Batch | Count | Link |
|---|---|---|
| Batch 001 | 385 | [batch_001.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/v2ray/batch_001.txt) |

### Clash Standard Batches

| Batch | Link |
|---|---|
| Batch 001 | [batch_001.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash/batch_001.yaml) |

### Clash Advanced Batches

| Batch | Link |
|---|---|
| Batch 001 | [batch_001.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash_advanced/batch_001.yaml) |

---

## Usage

### V2ray / Xray / Sing-box

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/all_configs.txt
```

### Clash / Mihomo — Standard

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash.yaml
```

### Clash / Mihomo — Advanced (Recommended)

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash_advanced.yaml
```

### 500-Config Batches

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/v2ray/batch_001.txt
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash/batch_001.yaml
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash_advanced/batch_001.yaml
```

---

## Advanced Clash Architecture

```
PROXY-BEST (final fallback)
├── SCEN-OPEN  → LB-ALL + FAST-ALL   (open internet)
├── SCEN-CDN   → LB-CDN + FAST-CDN   (CDN whitelist)
├── FAST-ALL   → url-test all proxies
└── FAST-CDN   → url-test via Cloudflare endpoint

UDP-BEST  → url-test for UDP traffic
LB-ALL    → load-balance consistent-hashing (session-aware)
LB-CDN    → load-balance round-robin (stateless CDN)
MANUAL    → manual select (all groups + DIRECT)
```

## Standard Clash Architecture

```
PROXY (manual selector)
├── Load-Balance  → consistent-hashing across all proxies
├── Auto          → url-test (fastest)
└── Fallback      → automatic failover
```

---

## Iran-Optimized DNS

| Server | Address |
|---|---|
| Shecan (primary) | 178.22.122.100 / 185.51.200.2 |
| TCI / Zirsakht | 217.218.155.155 / 217.218.127.127 |
| 403.online | 185.55.225.25 / 185.55.227.22 |

`.ir` domains resolve via Iranian DNS servers directly. Foreign domains use DoH/DoT.

---

## File Structure

```
config/
├── all_configs.txt              ← all V2ray configs
├── clash.yaml                   ← Clash standard
├── clash_advanced.yaml          ← Clash advanced (recommended)
├── protocols/
│   ├── vmess.txt
│   ├── vmess_clash.yaml
│   ├── vmess_clash_advanced.yaml
│   └── (other protocols)
└── batches/
    ├── v2ray/batch_001.txt        ← 500 random V2ray configs
    ├── clash/batch_001.yaml       ← 500 random Clash standard
    └── clash_advanced/batch_001.yaml
```

---

## Failed Sources

- https://github.com/Argh94/Proxy-List/raw/refs/heads/main/All_Config.txt (error)

---

*Auto-generated by GitHub Actions.*
