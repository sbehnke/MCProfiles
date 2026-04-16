#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="mcprofiles-tui"
VERSION="${VERSION:-$(cat "${SCRIPT_DIR}/VERSION")}"
BUILD_DIR="${SCRIPT_DIR}/build"
OUT_DIR="${BUILD_DIR}/${APP_NAME}"

echo "==> Cleaning build directory..."
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

# Static, CGO-less builds — no Fyne/OpenGL deps are pulled in under -tags headless.
build_one() {
    local arch="$1"
    local out="${OUT_DIR}/${APP_NAME}-linux-${arch}"
    echo "==> Building ${arch}..."
    CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" \
        go build -tags headless \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o "${out}" "${SCRIPT_DIR}"
}

build_one amd64
build_one arm64

echo ""
echo "==> Build complete: ${OUT_DIR}/"
ls -lh "${OUT_DIR}"
echo ""
echo "Config lives at \$XDG_CONFIG_HOME/mcprofiles/servers.toml"
echo "(or ~/.config/mcprofiles/servers.toml on Linux)."
