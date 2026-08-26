#!/usr/bin/env bash
# Run the HighJack server in development mode.
# Without a database configured this serves health/version only.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./server/cmd/highjack "$@"
