#!/bin/bash
set -euo pipefail

# Builds Linux and Windows binaries for amd64 and arm64 using Docker.
# Produces 5 archives in build/release/:
#   mcprofiles-linux-amd64.tar.gz
#   mcprofiles-linux-arm64.tar.gz
#   mcprofiles-windows-amd64.zip
#   mcprofiles-windows-arm64.zip
#
# The Windows cross-compiles run in both containers (redundant but harmless),
# and only one copy of each is kept.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="${SCRIPT_DIR}/build/release"
IMAGE_NAME="mcprofiles-builder"

echo "==> Cleaning release directory..."
rm -rf "${BUILD_DIR}"
mkdir -p "${BUILD_DIR}"

# --- Build Docker image for each platform ---
for PLATFORM in linux/amd64 linux/arm64; do
    ARCH="${PLATFORM#*/}"
    echo ""
    echo "============================================"
    echo "==> Building container for ${PLATFORM}..."
    echo "============================================"

    docker build --platform "${PLATFORM}" -t "${IMAGE_NAME}:${ARCH}" "${SCRIPT_DIR}"

    echo ""
    echo "==> Running build for ${PLATFORM}..."
    docker run --rm --platform "${PLATFORM}" \
        -v "${BUILD_DIR}:/out" \
        "${IMAGE_NAME}:${ARCH}"
done

echo ""
echo "============================================"
echo "==> All builds complete"
echo "============================================"
ls -lh "${BUILD_DIR}/"
echo ""
echo "Artifacts ready in: ${BUILD_DIR}/"
