#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
output=${1:?usage: build-go-bridge.sh output.aar}
sdk_root=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-"$HOME/Library/Android/sdk"}}
ndk_root=${ANDROID_NDK_HOME:-"$sdk_root/ndk/30.0.16248370"}

mkdir -p "$(dirname -- "$output")"
cd "$repo_root"
export ANDROID_HOME="$sdk_root"
export ANDROID_SDK_ROOT="$sdk_root"
export ANDROID_NDK_HOME="$ndk_root"
export PATH="$(go env GOPATH)/bin:$PATH"
go tool gomobile init
go tool gomobile bind -target=android -androidapi=26 -javapkg=com.fp.mobile -o "$output" ./mobile
