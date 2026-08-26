#!/usr/bin/env bash
# Build the HighJack server binary into ./bin (gitignored).
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/highjack-server ./server/cmd/highjack
echo "highjack-server → bin/highjack-server"
