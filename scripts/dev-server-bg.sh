#!/usr/bin/env bash
# Start the HighJack server in the background for local verification.
# Kept as a script so the exact same startup can be used by hand and by
# the Playwright harness.
set -euo pipefail
cd "$(dirname "$0")/.."

LOG=${HIGHJACK_LOG_FILE:-/tmp/highjack-dev-server.log}
ADDR=${HIGHJACK_ADDR:-127.0.0.1:8080}

# Stop any previous instance. `go run` executes a compiled binary from the
# build cache, so matching only the package path would leave a stale server
# holding the port (and the new one would fail to bind). Match both forms.
pkill -f '[s]erver/cmd/highjack' 2>/dev/null || true
pkill -f '[g]o-build.*/highjack$' 2>/dev/null || true
sleep 1

setsid env \
  HIGHJACK_ENV=${HIGHJACK_ENV:-development} \
  HIGHJACK_ADDR="$ADDR" \
  HIGHJACK_LOG_LEVEL=${HIGHJACK_LOG_LEVEL:-info} \
  HIGHJACK_ALLOWED_ORIGINS=${HIGHJACK_ALLOWED_ORIGINS:-http://127.0.0.1:5173,http://localhost:5173} \
  go run ./server/cmd/highjack >"$LOG" 2>&1 </dev/null &

for _ in $(seq 1 40); do
  if curl -fsS -m 2 "http://$ADDR/health" >/dev/null 2>&1; then
    echo "server ready on $ADDR (log: $LOG)"
    exit 0
  fi
  sleep 1
done

echo "server failed to start; last log lines:" >&2
tail -20 "$LOG" >&2
exit 1
