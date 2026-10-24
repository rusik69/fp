# iOS app

The iOS app is a native `WKWebView` host for the Go HTTP service and shared web UI. It stores backend data under the app's Application Support directory and uses the stable `http://localhost:8080` origin, so WebKit's service-worker cache can persist across launches. External HTTPS links open in the system browser. The generated Go framework and Xcode products stay under `ios/build/` and are not checked in.

## Prerequisites

- macOS with Xcode and its iOS Simulator SDK
- XcodeGen (`brew install xcodegen`)
- Go 1.26 or newer, with `golang.org/x/mobile/cmd/gomobile` supplied by the repository's Go tool directive
- Internet access for the first Go module and tool downloads

## Build

From the repository root:

```sh
make build-ios
```

This generates `ios/build/Mobile.xcframework` for iOS devices and simulators, then builds the simulator app with code signing disabled. The app bundle is `ios/build/DerivedData/Build/Products/Debug-iphonesimulator/FlightPicture.app`. It is a simulator build, not a signed App Store distribution.

To install and launch in a booted simulator:

```sh
xcrun simctl install booted ios/build/DerivedData/Build/Products/Debug-iphonesimulator/FlightPicture.app
xcrun simctl launch booted com.fp.flightpicture
```

The Go service lifecycle and HTTP behavior are covered by `go test ./mobile`; the iOS target links the same bridge and API.

## Offline and operational-data limits

The app bundles the UI and backend, so its shell opens without network access. WebKit's service worker and browser storage persist on the device; successful cached briefings remain readable when the connection is unavailable, subject to their displayed source and freshness. A first-time download, live refresh, charts, NOTAMs, and other external provider data still need connectivity and depend on provider coverage and redistribution terms. Bundling the app does not make those feeds available offline automatically. Verify current official aeronautical information before flight; this app is not an approved navigation source.
