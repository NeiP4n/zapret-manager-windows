#!/bin/sh
# Builds dist/ZapretManager.exe (Windows x64) from any OS.
set -e
cd "$(dirname "$0")"
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/ZapretManager.exe ./cmd/zapret-manager
echo "dist/ZapretManager.exe"
