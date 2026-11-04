#!/bin/sh
# Builds the Kanban binary into ./build on Linux/macOS (or Git Bash on Windows).
#
#   ./build.sh          build for this computer only
#   ./build.sh all      build for Windows, Linux and macOS (amd64 + arm64)
#   ./build.sh test     run the test suite
#
# Requires Go 1.26+. No C compiler is needed (the SQLite driver is pure Go).
set -eu

ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="$ROOT/build"
VERSION="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X main.version=$VERSION"

mkdir -p "$OUT"
cd "$ROOT/code"

build() { # build GOOS GOARCH
  ext=""
  [ "$1" = "windows" ] && ext=".exe"
  name="kanban-$1-$2$ext"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$name" ./cmd/web
}

case "${1:-native}" in
  all)
    for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
      build "${target%/*}" "${target#*/}"
    done
    ;;
  test)
    go vet ./...
    go test ./...
    exit 0
    ;;
  native)
    build "$(go env GOOS)" "$(go env GOARCH)"
    ;;
  *)
    echo "usage: $0 [native|all|test]" >&2
    exit 2
    ;;
esac
echo "done -> $OUT"
