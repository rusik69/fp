# Android app

The Android app is a native WebView host for the existing embedded web UI and Go HTTP API. By default, the Activity starts the Go service in-process on `127.0.0.1:8080`, stores its cache under the app's private files directory, and opens the stable `http://localhost:8080` origin. Tap **Server** to use a separately administered FP server instead; remote server addresses must be HTTPS origins (host and optional port only), are saved in app preferences, and are restricted to that exact origin in the WebView. The bundled local server remains available through **Use on-device server**. Never embed an iBonav or other provider credential in the app; configure provider access only on a trusted Go server. WebView file/content access and mixed content are disabled; cleartext networking is permitted only for `localhost`. External HTTPS links open in the system browser.

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

The app bundles the UI and backend, so its shell opens without network access. WebView's service worker and browser storage persist on the device for the selected server origin; successful cached briefings remain readable when the connection is unavailable, subject to their displayed source and freshness. Switching server origins uses separate browser storage. A remote server's data, access logs, provider credentials, retention, and security are controlled by that server's operator. A first-time download, live refresh, charts, NOTAMs, and other external provider data still need connectivity and depend on provider coverage and redistribution terms. Bundling the app does not make those feeds available offline automatically. Verify current official aeronautical information before flight; this app is not an approved navigation source.
