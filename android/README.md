# Aiptimize Companion (Android)

A thin companion for one or more Aiptimize installations. The server stays where
it is and performs every hardware write; the app only chooses which URL to show
and displays the server's own web UI in a WebView.

## What it does

- **Discovery** — sends the UDP probe to `255.255.255.255:8099` (and every
  interface broadcast address), collects unicast replies and lists the servers
  by name, version and API URL.
- **Manual address** — enter `192.168.0.154:8080` or any URL, press **Test**
  (`GET /api/v1/instance`) and save it as the LAN or the remote address.
- **Multiple servers** — profiles are keyed by the server's `instance_id`, so a
  DHCP change updates the LAN address instead of creating a duplicate.
- **Automatic transport** — LAN first (2–3 s), then the remote URL (10 s). Every
  candidate must return the profile's `instance_id`; a different server behind
  the address is rejected. `AUTO`, `LAN` and `WAN` can be pinned per profile.
- **Viewer** — the server SPA in a WebView: login, plans, EV control, live data,
  everything the web UI does, including its write gates and confirmations.

## Discovery protocol

```
→ broadcast 255.255.255.255:8099   {"aiptimize":"discover","v":1}
← unicast                          {"aiptimize":"server","v":1,
                                    "instance_id":"28addbf6-…","name":"BaracHome",
                                    "version":"dev-0.12.008",
                                    "api":"http://192.168.0.154:8080",
                                    "setup_required":false}
```

The server must publish UDP 8099 (the shipped systemd unit uses host networking,
so 8080 and 8099 are both reachable directly).

## Build

Verified with Gradle 8.10.2, Temurin JDK 17, Android SDK 35 (platform 35,
build-tools 35.0.0):

```
BUILD SUCCESSFUL — app/build/outputs/apk/debug/app-debug.apk (11 MB)
package io.aiptimize.companion, minSdk 26, target 35
```

A debug APK is published at the repository root as
`aiptimize-companion-0.1.0-debug.apk` for sideloading.

```sh
cd android
./gradlew assembleDebug          # app/build/outputs/apk/debug/app-debug.apk
./gradlew installDebug           # with a device attached
```

Android Studio only needs to open `android/` and sync; the wrapper is checked
in.

## Remote access

`https://home.aiptimize.io` is served through Cloudflare Tunnel. The WebView
follows Cloudflare Access's email one-time-PIN flow once per host and keeps the
session cookie, so remote use needs no extra configuration. Set the Access
application session duration to about a month to avoid re-authenticating daily.

## Not implemented yet (phase 3+)

- Cloudflare Access service-token injection (needs a spike: SSE through
  `shouldInterceptRequest` is unproven).
- Pull-to-refresh, downloads, deep links, notifications.
- Play Store packaging (TWA wrapper is the fallback if a WebView app is rejected
  under the minimum-functionality policy).
