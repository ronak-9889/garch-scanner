#!/bin/sh
# Builds the app for every platform into ../software. Needs Go 1.22+ (https://go.dev/dl). No other dependencies.
set -e
cd "$(dirname "$0")"
OUT=../software
mkdir -p "$OUT"
build() { GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/$3" . && echo "built $3"; }
build windows amd64 garch-volatility-windows-amd64.exe
build windows arm64 garch-volatility-windows-arm64.exe
build darwin  arm64 garch-volatility-mac-arm64
build darwin  amd64 garch-volatility-mac-intel
build linux   amd64 garch-volatility-linux-amd64
