# Android app

The Android app is a native WebView host for the existing embedded web UI and Go HTTP API. The Activity starts the Go service in-process on `127.0.0.1:8080`, stores its cache under the app's private files directory, and opens the stable `http://localhost:8080` origin. The stable origin lets WebView's service-worker cache survive process restarts. WebView file/content access and mixed content are disabled; cleartext networking is permitted only for `localhost`. External HTTPS links open in the system browser.

## Prerequisites

- Go 1.26 or newer, with `golang.org/x/mobile/cmd/gomobile` provided by the repository's Go tool directive
- JDK 17
- Android SDK Platform 37, Build Tools 37.0.0, and NDK 30.0.16248370
- Minimum supported Android version: API 26.
- Android SDK path in `ANDROID_SDK_ROOT` or `ANDROID_HOME`; the Make target defaults to `$HOME/Library/Android/sdk` on macOS
- Internet access for Gradle, Go module, and Android SDK downloads on the first build

The Gradle wrapper pins Gradle 9.6.0 and verifies the distribution checksum. The Gradle build invokes `gomobile bind` to package the Go service for `arm64-v8a`, `armeabi-v7a`, `x86`, and `x86_64`; the AAR is generated under `android/app/build/` and is not checked in.

## Build and test

From the repository root:

```sh
make build-android-apk
```

The debug APK is `android/app/build/outputs/apk/debug/app-debug.apk`. It is debug-signed and intended for local installation, not store distribution. Install on a connected device with:

```sh
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

Run the Android instrumentation smoke test on a booted API 37 emulator or connected device:

```sh
make android-test
```

The test launches the Activity and verifies that the embedded home page is served from the local Go backend. Go service lifecycle and HTTP behavior are covered by `go test ./mobile`.

## Offline and operational-data limits

The app bundles the UI and backend, so its shell opens without network access. WebView's service worker and browser storage persist on the device; successful cached briefings remain readable when the connection is unavailable, subject to their displayed source and freshness. A first-time download, live refresh, charts, NOTAMs, and other external provider data still need connectivity and depend on provider coverage and redistribution terms. Bundling the app does not make those feeds available offline automatically. Verify current official aeronautical information before flight; this app is not an approved navigation source.

The Go server listens only on IPv4 loopback. The Android network security policy allows HTTP only to the app's `localhost` origin; provider requests use HTTPS. App data and cache files live in the app's private storage and are removed when Android uninstalls the app.
