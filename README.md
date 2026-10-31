# FP — pilot briefing tools

FP is a cross-platform, offline-capable aviation information app with a Go backend and embedded web interface. It combines weather briefings, airport reference data, FAA procedures and VFR chart PDFs, hazard/map overlays, regional AIS links, and local chart/briefing storage. Availability and currency vary by source and region.

## Run the web app

Requires Go 1.26 or newer:

```sh
go run ./cmd/fp
```

The server binds to `127.0.0.1:8080` by default. Set `FP_LISTEN_ADDR` to change the bind address and `FP_DATA_DIR` to choose the server's cache/data directory. If you expose it beyond the local machine, configure the bind deliberately and use an HTTPS reverse proxy.

### iBonav chart map and NOTAM API

The optional iBonav integration requires a provider-issued personal token and assigned developer roles. Set the token only in the trusted Go server's environment:

```sh
FP_IBONAV_PERSONAL_TOKEN='ibp_…' go run ./cmd/fp
```

Request the `ais-data:map-frame`, `ais-data:pib-api`, and `ais-data:charts-api` roles from [iBonav's developer portal](https://dev.ibonav.aero/general/roles). The app exchanges the personal token server-side; never put it in browser JavaScript, a mobile package, or source control. For remote use, place the server behind HTTPS. The provider's [chart coverage list](https://dev.ibonav.aero/charts-api/coverage-match-list) is partial and can change; access terms and service availability must be confirmed with the provider.

The Android and iOS packages run the Go server locally and do not include a credential or a mobile token-entry flow. The iBonav integration is therefore unavailable in those packages unless a trusted configured-server path is added; do not work around this by embedding a personal token in an app build.

## Build and test

```sh
make check             # Go tests, desktop cross-builds, Android Go backend
make build-android-apk # Debug-signed Android APK
make android-test      # Instrumentation test; requires emulator/device
make build-ios         # Simulator and unsigned device development apps
```

Desktop builds target macOS, Linux, and Windows on amd64 and arm64. Platform packaging instructions and prerequisites are in [`android/README.md`](android/README.md) and [`ios/README.md`](ios/README.md).

## Offline and aviation-data limits

The app shell and previously saved browser/device data can work without a connection. Live weather, external charts, the iBonav map, and provider refreshes require internet. Exact iBonav NOTAM searches may be served from a local cache when offline; cached data is marked as potentially stale. Browser/device storage can be cleared or evicted.

Coverage is not globally complete: FAA procedures and VFR charts are U.S.-focused; international chart availability depends on provider coverage and account access; regional AIS links lead to external services with their own requirements. No provider is guaranteed free, complete, or continuously available. FP is supplemental information, not an approved navigation system or replacement for current State AIS/AIP publications and official preflight briefings. Verify source, edition, scope, validity, and currency before flight.
