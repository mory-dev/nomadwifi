<div align="center">

# 🧭 NomadWiFi

**The open-source intelligent Wi-Fi auto-roamer and band optimizer for digital nomads and travelers.**

[![Go Report Card](https://goreportcard.com/badge/github.com/dariomory/nomadwifi)](https://goreportcard.com/report/github.com/dariomory/nomadwifi)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Fixes Windows "Sticky AP syndrome", forces clean 5 GHz/6 GHz bands, auto-groups multi-AP hotel networks, and keeps your remote work connection fast and stable.

</div>

---

## 💥 The Problem NomadWiFi Solves

When traveling in hotels, co-working spaces, Airbnbs, and cafes:
* **The "Sticky AP" Flaw**: Windows stubbornly stays locked onto a distant, weak 2.4 GHz router (throttled at 24 Mbps) even when a 250+ Mbps 5 GHz router is in your room.
* **Hotel Multi-SSID Fragmentation**: Venues broadcast multiple SSIDs (`Hotel_2F`, `Hotel_3F`, `Hotel_5G`, `Lobby`). Windows treats them as separate networks and refuses to switch until your connection completely dies.
* **Hidden Packet Loss & Latency Spikes**: Routers get overloaded. Windows shows 4 bars of Wi-Fi while packets drop to 50%.

## ✨ Key Features

- ⚡ **Auto-Optimized 5 GHz & Wi-Fi Standard Roaming**: Automatically scores every visible access point by band (5GHz/6GHz > 2.4GHz), standard (802.11be/ax/ac > n/g), cipher, and channel cleanliness.
- 🏨 **Multi-AP Hotel Cluster Detection**: Groups related hotel floor networks (`SMFloor21`, `SMFloor21_5G`, `Hotel_3F`) and automatically routes you to the fastest AP in the cluster.
- 🛡️ **Background Auto-Roamer Daemon**: Monitors gateway latency, link speed, and packet loss every 20s. If your link degrades or a 5 GHz band becomes available, it seamlessly roams.
- 🚪 **Captive Portal Detection**: Detects hotel login portals instantly via Google 204 connectivity probes.
- 📦 **Zero External Dependencies**: Single lightweight portable executable.

---

## 🚀 Quick Start

### Installation

```bash
# Clone and build
git clone https://github.com/dariomory/nomadwifi.git
cd nomadwifi
go build -o nomadwifi.exe ./cmd/nomadwifi
```

### Usage

#### 1. Check Active Connection Diagnostics
```bash
nomadwifi status
```

#### 2. Scan & Rank Surrounding Access Points
```bash
nomadwifi scan
```

#### 3. One-Click Auto-Optimize
Automatically switches to the highest-speed band / AP in your current venue:
```bash
nomadwifi optimize
```

#### 4. Run Background Auto-Roam Daemon
Keeps your connection optimized continuously while you work:
```bash
nomadwifi watch
```

---

## 📊 Quality Scoring Algorithm

NomadWiFi calculates a real-time **Quality Score (0–100+)** for each access point:

$$\text{Score} = \text{RadioBonus} + \text{BandBonus} + (\text{Signal} \times 0.40) + \text{CipherBonus}$$

* **Wi-Fi 7 / 6 / 5 (802.11be / ax / ac)**: +25 to +45 pts
* **5 GHz / 6 GHz Band**: +25 to +30 pts (Immune to 2.4 GHz microwave/neighbor congestion)
* **2.4 GHz Penalty**: -10 pts
* **Legacy Ciphers (TKIP/WEP)**: -20 pts

---

## 📄 License
MIT License. Free and open source for all nomads and travelers.
