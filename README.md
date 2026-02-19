# V2Ray Config Aggregator

**آخرین بروزرسانی:** 2026-02-19 01:04:53 UTC

این پروژه به صورت خودکار کانفیگ‌های V2Ray را از منابع مختلف جمع‌آوری، اعتبارسنجی و دسته‌بندی می‌کند.

---

## آمار کلی

| شاخص | مقدار |
|------|-------|
| کل دریافت‌شده (ورودی) | 2248 |
| کانفیگ‌های معتبر (خروجی) | 376 |
| کاهش (تکراری + نامعتبر) | 83.3% |
| زمان پردازش | 31.38 ثانیه |
| تعداد دسته‌های ۵۰۰تایی | 1 |

## آمار به تفکیک پروتکل

| پروتکل | تعداد ورودی (تخمین) | تعداد خروجی (معتبر) |
|--------|---------------------|---------------------|
| VMESS | - | 29 |
| VLESS | - | 125 |
| TROJAN | - | 138 |
| SS | - | 83 |
| HY2 | - | 1 |
| **مجموع** | **2248** | **376** |

---

## فایل‌های اصلی

### V2Ray — همه پروتکل‌ها

| توضیح | لینک دانلود |
|-------|-------------|
| همه کانفیگ‌ها (text) | [all_configs.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/all_configs.txt) |

### Clash / Mihomo — ساختار معمولی

| توضیح | لینک دانلود |
|-------|-------------|
| Clash همه پروتکل‌ها | [clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash.yaml) |

### Clash / Mihomo — ساختار پیشرفته (بهینه‌شده برای ایران)

> دارای DNS چندلایه با سرورهای ایرانی (Shecan/TCI/403online)، تشخیص خودکار سناریو فیلترینگ، load-balancing و fallback هوشمند

| توضیح | لینک دانلود |
|-------|-------------|
| Clash Advanced همه پروتکل‌ها | [clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash_advanced.yaml) |

---

## فایل‌های جداگانه به تفکیک پروتکل

| پروتکل | V2Ray Text | Clash معمولی | Clash پیشرفته |
|--------|-----------|--------------|----------------|
| **VMESS** | [vmess.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess.txt) | [vmess_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess_clash.yaml) | [vmess_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vmess_clash_advanced.yaml) |
| **VLESS** | [vless.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless.txt) | [vless_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless_clash.yaml) | [vless_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/vless_clash_advanced.yaml) |
| **TROJAN** | [trojan.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan.txt) | [trojan_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan_clash.yaml) | [trojan_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/trojan_clash_advanced.yaml) |
| **SS** | [ss.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss.txt) | [ss_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss_clash.yaml) | [ss_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/ss_clash_advanced.yaml) |
| **HY2** | [hy2.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2.txt) | [hy2_clash.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2_clash.yaml) | [hy2_clash_advanced.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/protocols/hy2_clash_advanced.yaml) |

---

## فایل‌های دسته‌ای — ۵۰۰ کانفیگ تصادفی

هر دسته شامل ۵۰۰ کانفیگ به صورت تصادفی از میان تمام کانفیگ‌های معتبر انتخاب شده است.

### V2Ray Batches

| دسته | تعداد | لینک دانلود |
|------|-------|-------------|
| دسته 1 | 376 | [batch_001.txt](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/v2ray/batch_001.txt) |

### Clash Batches — معمولی

| دسته | تعداد | لینک دانلود |
|------|-------|-------------|
| دسته 1 | 376 | [batch_001.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash/batch_001.yaml) |

### Clash Batches — پیشرفته

| دسته | تعداد | لینک دانلود |
|------|-------|-------------|
| دسته 1 | 376 | [batch_001.yaml](https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/batches/clash_advanced/batch_001.yaml) |

---

## نحوه استفاده

### V2Ray / Xray / Nekoray / Hiddify

لینک subscription زیر را در اپلیکیشن وارد کنید:

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/all_configs.txt
```

### Clash / Mihomo — ساختار معمولی

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash.yaml
```

### Clash / Mihomo — ساختار پیشرفته

این نسخه پیشنهادی برای کاربران ایران است. دارای گروه‌بندی هوشمند:

- **PROXY-BEST**: انتخاب خودکار بهترین مسیر
- **SCEN-OPEN / SCEN-CDN / SCEN-IRAN-ONLY**: تشخیص سناریو فیلترینگ
- **LB-OPEN / LB-CDN**: توزیع بار چندمسیره
- **UDP-BEST**: بهترین پروکسی برای تماس صوتی/تصویری
- **MANUAL**: انتخاب دستی برای کاربران پیشرفته

```
https://github.com/Delta-Kronecker/V2ray-Config/raw/refs/heads/main/config/clash_advanced.yaml
```

---

## منابع ناموفق در آخرین اجرا

- https://github.com/Argh94/Proxy-List/raw/refs/heads/main/All_Config.txt (error)

---

*این فایل به صورت خودکار توسط GitHub Actions تولید می‌شود.*
