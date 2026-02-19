## Statistics

### Per-Protocol Input & Output

| Protocol | Input (unique) | Output (valid) | Pass Rate |
|---|---|---|---|
| VMESS | 200 | 50 | 25.0% |
| VLESS | 530 | 142 | 26.8% |
| TROJAN | 750 | 122 | 16.3% |
| SS | 191 | 64 | 33.5% |
| HY2 | 16 | 1 | 6.2% |
| HY | 0 | 0 | 0.0% |
| TUIC | 2 | 0 | 0.0% |
| **Total** | **1689** | **379** | **22.4%** |

| Metric | Value |
|---|---|
| Raw fetched lines | 2387 |
| Unique after dedup | 1689 |
| Valid configs | 379 |
| Processing time | 33.23s |

---

## Main Files

### V2ray — All Configs

| File | Link |
|---|---|
| All configs (txt) | [all_configs.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/all_configs.txt) |

### V2ray — By Protocol

| Protocol | Count | Link |
|---|---|---|
| VMESS | 50 | [vmess.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess.txt) |
| VLESS | 142 | [vless.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless.txt) |
| TROJAN | 122 | [trojan.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan.txt) |
| SS | 64 | [ss.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss.txt) |
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

---

## Batch Files — Random 500-Config Groups

> Each file contains 500 randomly selected configs from all protocols.

### V2ray Batches

| Batch | Count | Link |
|---|---|---|
| Batch 001 | 379 | [batch_001.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/v2ray/batch_001.txt) |

### Clash Standard Batches

| Batch | Link |
|---|---|
| Batch 001 | [batch_001.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash/batch_001.yaml) |

