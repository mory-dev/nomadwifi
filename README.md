# NomadWiFi

**Wi-Fi roaming and band optimizer for Windows.**
[nomadwifi.mory.dev](https://nomadwifi.mory.dev) · [Documentation](https://nomadwifi.mory.dev/docs)

Windows clings to the first access point it associated with. You walk to the far end of the cafe,
the signal drops to −80 dBm, and it still will not move — while a 5 GHz radio two metres away sits
idle. NomadWiFi measures every access point in range, moves you to the one that is actually good,
recovers in about a second when the link drops, and gets your VPN out of the way while it does it.

![The NomadWiFi desktop app](site/assets/screenshots/gui-main.png)

## What it does

- **Ranks access points honestly.** The driver reports the AP you are joined to at ~99% link quality
  regardless of its real signal, which is why the incumbent always wins. NomadWiFi scores every
  radio from measured RSSI, band, 802.11 standard and BSS-Load airtime congestion.
- **Recovers when the link drops.** It subscribes to the wireless service's ACM notifications rather
  than polling, so a disconnect is noticed in ~100 ms with the candidate ladder already built. A
  roam that fails to carry traffic is rolled back.
- **Prepares the venue in advance.** Hotels publish one key across `Lobby`, `_5G` and `Floor2`.
  NomadWiFi recognises the family, writes the profiles before they are needed (per-user, so no
  elevation), verifies them on first association, and deletes guesses that fail twice.
- **Cooperates with your VPN.** Hold the tunnel → switch → verify → resume → flush DNS. The
  off-and-on-again dance, automated. See [the VPN page](https://nomadwifi.mory.dev/docs/vpn).
- **Understands captive portals.** A sign-in page cannot load through a tunnel; NomadWiFi says so and
  offers one click to pause, sign in and resume. A portal is never mistaken for a bad access point.

## Install

Download a release and unzip it anywhere. Nothing is installed and nothing is written outside your
user profile.

```
NomadWiFi\
  nomadwifi.exe          # the desktop app
  core\nomadwifi.exe     # the engine it drives
cli\
  nomadwifi.exe          # the standalone command line tool
```

Both executables are called `nomadwifi.exe` deliberately, which is why they live in separate
folders; the app resolves its engine from `core\` and refuses any candidate that resolves to itself.

Administrator rights are not required. The only action that asks for elevation is pausing a VPN
client that has no command line of its own.

## Use it

```
nomadwifi                     # interactive terminal menu
nomadwifi status              # link stats, gateway latency, captive portal, VPN
nomadwifi scan [--all]        # rank nearby networks; --all lists every radio
nomadwifi optimize            # switch to the best AP, rolling back if it fails
nomadwifi connect <SSID>      # optionally with --password <key>
nomadwifi watch [--interval N]  # monitor continuously and roam automatically
nomadwifi vpn [status|hold|resume]
nomadwifi warm                # prepare profiles for instant failover
nomadwifi logs
nomadwifi version
```

Every command accepts `--json`. `--dry-run`, `--no-vpn`, `--password` and `--interval` are
documented in the [CLI reference](https://nomadwifi.mory.dev/docs/cli).

```console
$ nomadwifi scan --json | jq '.[0] | {ssid, band, rssi, quality_score, reasons}'
{
  "ssid": "INDY_5G",
  "band": "5 GHz",
  "rssi": -43,
  "quality_score": 126,
  "reasons": ["Wi-Fi 6 (802.11ax)", "5 GHz band", "AP airtime nearly idle", "Saved network"]
}
```

## Build

Requires Go 1.21+ and, for the desktop app, MSBuild with .NET Framework 4.8.

```powershell
.\build.ps1            # icons, tests, both binaries, dist\ layout
.\build.ps1 -SkipGui   # command line only
.\build.ps1 -Zip       # also produce release archives
```

Output lands in `dist\`. `go build ./cmd/nomadwifi` alone works too — the icon and manifest come
from the committed `resource_windows.syso`, so no extra tooling is needed.

```
go vet ./... && go test ./...
```

## Layout

| Path | What lives there |
| --- | --- |
| `cmd/nomadwifi` | CLI entry point, interactive menu, and the `agent --stdio` JSON protocol |
| `pkg/wifi` | Native Wifi API bindings, scanning, the rolling BSSID cache, profiles, warming |
| `pkg/roam` | Health model and the roaming engine: ladder, switch, verify, rollback |
| `pkg/vpn` | Tunnel detection, per-client control, hold/resume/repair |
| `pkg/state` | `~/.nomadwifi/state.json` — provisioned profiles, verification, penalty box |
| `pkg/tui` | Terminal rendering |
| `tools/NomadWiFi.UI` | The WPF desktop app |
| `tools/genicon` | Pure-Go ICO generator for the app icon |
| `site` | The documentation site published at nomadwifi.mory.dev |

## Where it keeps things

| Path | Contents |
| --- | --- |
| `%USERPROFILE%\.nomadwifi\state.json` | Profiles NomadWiFi created, verification, failures, penalties |
| `%USERPROFILE%\.nomadwifi\scan-cache.json` | Rolling view of access points, so one-shot commands see the whole venue |

Delete either to start fresh; both are rebuilt automatically.

## Scope

Windows 10 and 11 only. NomadWiFi does not patch drivers, edit the registry beyond a single
`Run` value for start-with-Windows, or install a service. Releases are not code-signed, so
SmartScreen will warn on first run.

## License

MIT.
