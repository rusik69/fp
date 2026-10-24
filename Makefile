GO ?= go
BIN_DIR ?= bin
ifndef ANDROID_SDK_ROOT
  ifdef ANDROID_HOME
    ANDROID_SDK_ROOT := $(ANDROID_HOME)
  else
    ANDROID_SDK_ROOT := $(HOME)/Library/Android/sdk
  endif
endif
ANDROID_HOME ?= $(ANDROID_SDK_ROOT)
ANDROID_NDK_HOME ?= $(ANDROID_SDK_ROOT)/ndk/30.0.16248370

PLATFORMS := \
	 darwin/amd64 darwin/arm64 \
	 linux/amd64 linux/arm64 \
	 windows/amd64 windows/arm64

.PHONY: test build build-all build-android build-android-apk android-test build-ios check clean

test:
	$(GO) test ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/fp ./cmd/fp


build-all:
	mkdir -p $(BIN_DIR)
	@set -eu; \
	for target in $(PLATFORMS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; [ "$$os" != windows ] || ext=".exe"; \
		echo "Building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -o $(BIN_DIR)/fp-$$os-$$arch$$ext ./cmd/fp; \
	done

build-android:
	mkdir -p $(BIN_DIR)
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o $(BIN_DIR)/fp-android-arm64 ./cmd/fp

build-android-apk:
	ANDROID_HOME="$(ANDROID_HOME)" ANDROID_SDK_ROOT="$(ANDROID_SDK_ROOT)" ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" PATH="$(HOME)/go/bin:$(PATH)" ./android/gradlew --project-dir android --no-daemon assembleDebug

android-test:
	ANDROID_HOME="$(ANDROID_HOME)" ANDROID_SDK_ROOT="$(ANDROID_SDK_ROOT)" ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" PATH="$(HOME)/go/bin:$(PATH)" ./android/gradlew --project-dir android --no-daemon connectedDebugAndroidTest

build-ios:
	mkdir -p $(BIN_DIR)
	GOOS=ios GOARCH=arm64 CGO_ENABLED=1 $(GO) build -o $(BIN_DIR)/httpapi-ios-arm64.a ./internal/httpapi

check: test build-all build-android


clean:
	rm -rf $(BIN_DIR)
