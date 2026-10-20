#!/usr/bin/env bash
# Build the HighJack server binary into ./bin (gitignored).
#
# The build identifies itself. The version comes from the repository's VERSION
# file and the commit from git, so the binary can always say what it is:
#
#     curl localhost:8080/version
#     {"name":"highjack-server","version":"0.2.7","commit":"af8daa8",...}
#
# Nothing used to do this. `version.Version` defaulted to "dev" and
# `version.Commit` to "unknown", and no build path in the repository overrode
# them, so the /version endpoint could never identify a deployed build. The
# Dockerfile accepted the metadata as build arguments; nothing ever passed them.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(tr -d '[:space:]' < VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

mkdir -p bin
go build -trimpath \
  -ldflags "-s -w \
    -X github.com/analeis/highjack/server/internal/version.Version=${VERSION} \
    -X github.com/analeis/highjack/server/internal/version.Commit=${COMMIT}" \
  -o bin/highjack-server ./server/cmd/highjack

echo "highjack-server → bin/highjack-server (version ${VERSION}, commit ${COMMIT})"
