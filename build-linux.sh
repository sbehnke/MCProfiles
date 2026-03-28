#!/bin/bash
set -euo pipefail

APP_NAME="mcprofiles"
VERSION="1.0.0"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="${SCRIPT_DIR}/build"
APP_DIR="${BUILD_DIR}/${APP_NAME}"

echo "==> Cleaning build directory..."
rm -rf "${BUILD_DIR}"
mkdir -p "${APP_DIR}"

# --- Check for Fyne build dependencies ---
echo "==> Checking dependencies..."
for cmd in gcc pkg-config; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd is required but not found."
        echo "Install build dependencies:"
        echo "  Debian/Ubuntu: sudo apt install gcc libgl1-mesa-dev xorg-dev"
        echo "  Fedora:        sudo dnf install gcc mesa-libGL-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel"
        echo "  Arch:          sudo pacman -S gcc mesa libxcursor libxrandr libxinerama libxi"
        exit 1
    fi
done

# --- Build the Go binary ---
echo "==> Building Go binary..."
CGO_ENABLED=1 GOOS=linux go build -o "${APP_DIR}/${APP_NAME}" "${SCRIPT_DIR}"

# --- Create .desktop file ---
echo "==> Creating .desktop entry..."
cat > "${APP_DIR}/${APP_NAME}.desktop" << DESKTOP
[Desktop Entry]
Name=MC Profile Editor
Comment=Edit Minecraft launcher profiles
Exec=${APP_NAME}
Icon=${APP_NAME}
Terminal=false
Type=Application
Categories=Game;Utility;
DESKTOP

# --- Copy icon (SVG works directly for Linux desktop entries) ---
if [ -f "${SCRIPT_DIR}/resources/AppIcon.icon/Assets/mc_block.svg" ]; then
    echo "==> Copying icon..."
    cp "${SCRIPT_DIR}/resources/AppIcon.icon/Assets/mc_block.svg" "${APP_DIR}/${APP_NAME}.svg"
fi

# --- Create install script ---
cat > "${APP_DIR}/install.sh" << 'INSTALL'
#!/bin/bash
set -euo pipefail

PREFIX="${1:-/usr/local}"
echo "Installing to ${PREFIX}..."

sudo install -Dm755 mcprofiles "${PREFIX}/bin/mcprofiles"

if [ -f mcprofiles.svg ]; then
    sudo install -Dm644 mcprofiles.svg "${PREFIX}/share/icons/hicolor/scalable/apps/mcprofiles.svg"
fi

if [ -f mcprofiles.desktop ]; then
    sudo install -Dm644 mcprofiles.desktop "${PREFIX}/share/applications/mcprofiles.desktop"
fi

echo "Installed. Run 'mcprofiles' to launch."
INSTALL
chmod +x "${APP_DIR}/install.sh"

echo ""
echo "==> Build complete: ${APP_DIR}/"
echo "    Run directly:  ${APP_DIR}/${APP_NAME}"
echo "    Install:       cd ${APP_DIR} && ./install.sh [PREFIX]"
