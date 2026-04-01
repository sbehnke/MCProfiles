#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="mcprofiles"
VERSION="${VERSION:-$(cat "${SCRIPT_DIR}/VERSION")}"
BUILD_DIR="${SCRIPT_DIR}/build"
APP_DIR="${BUILD_DIR}/${APP_NAME}-windows"

echo "==> Cleaning build directory..."
rm -rf "${BUILD_DIR}"
mkdir -p "${APP_DIR}"

# --- Determine build mode ---
# Native build on Windows (via Git Bash/MSYS2) or cross-compile from macOS/Linux
if [[ "$(uname -s)" == MINGW* ]] || [[ "$(uname -s)" == MSYS* ]] || [[ "$(uname -s)" == CYGWIN* ]]; then
    echo "==> Building natively on Windows..."
    CGO_ENABLED=1 go build -ldflags "-H windowsgui" -o "${APP_DIR}/${APP_NAME}.exe" "${SCRIPT_DIR}"
else
    echo "==> Cross-compiling for Windows..."

    # Check for MinGW cross-compiler
    CROSS_CC=""
    for cc in x86_64-w64-mingw32-gcc x86_64-w64-mingw32-cc; do
        if command -v "$cc" &>/dev/null; then
            CROSS_CC="$cc"
            break
        fi
    done

    if [ -z "$CROSS_CC" ]; then
        echo "ERROR: MinGW cross-compiler not found."
        echo "Install it:"
        echo "  macOS:         brew install mingw-w64"
        echo "  Debian/Ubuntu: sudo apt install gcc-mingw-w64-x86-64"
        echo "  Fedora:        sudo dnf install mingw64-gcc"
        exit 1
    fi

    echo "    Using cross-compiler: ${CROSS_CC}"
    CGO_ENABLED=1 CC="${CROSS_CC}" GOOS=windows GOARCH=amd64 \
        go build -ldflags "-H windowsgui" -o "${APP_DIR}/${APP_NAME}.exe" "${SCRIPT_DIR}"
fi

# --- Copy icon ---
if [ -f "${SCRIPT_DIR}/resources/AppIcon.icon/Assets/mc_block.svg" ]; then
    echo "==> Copying icon..."
    cp "${SCRIPT_DIR}/resources/AppIcon.icon/Assets/mc_block.svg" "${APP_DIR}/${APP_NAME}.svg"
fi

echo ""
echo "==> Build complete: ${APP_DIR}/"
echo "    Executable: ${APP_DIR}/${APP_NAME}.exe"
echo ""
echo "    To embed a Windows icon (.ico), install fyne and run:"
echo "      go install fyne.io/fyne/v2/cmd/fyne@latest"
echo "      fyne package -os windows -icon resources/AppIcon.icon/Assets/mc_block.svg"
