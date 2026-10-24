#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
IOS_DIR="$ROOT/ios"
BUILD_DIR="$IOS_DIR/build"
FRAMEWORK="$BUILD_DIR/Mobile.xcframework"
DERIVED_DATA="$BUILD_DIR/DerivedData"

command -v xcodegen >/dev/null 2>&1 || {
  echo "xcodegen is required; install it with 'brew install xcodegen'" >&2
  exit 1
}
xcodegen generate --spec "$IOS_DIR/project.yml" --project "$IOS_DIR"
export PATH="$(go env GOPATH)/bin:$PATH"
go tool gomobile init
go tool gomobile bind -target=ios -o "$FRAMEWORK" "$ROOT/mobile"
xcodebuild \
  -project "$IOS_DIR/FlightPicture.xcodeproj" \
  -scheme FlightPicture \
  -configuration Debug \
  -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath "$DERIVED_DATA" \
  CODE_SIGNING_ALLOWED=NO \
  build
xcodebuild \
  -project "$IOS_DIR/FlightPicture.xcodeproj" \
  -scheme FlightPicture \
  -configuration Debug \
  -destination 'generic/platform=iOS' \
  -derivedDataPath "$DERIVED_DATA" \
  CODE_SIGNING_ALLOWED=NO \
  build
