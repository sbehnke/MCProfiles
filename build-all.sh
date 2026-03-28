#!/bin/bash
set -euo pipefail

# Build script that runs inside the Docker container.
# Produces Linux and Windows binaries for the current container architecture,
# plus Windows cross-compiled binaries.

VERSION="${VERSION:-1.0.1}"
OUT="/out"
mkdir -p "${OUT}"

ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    *)       echo "Unsupported arch: ${ARCH}"; exit 1 ;;
esac

echo "=== Building on ${ARCH} (GOARCH=${GOARCH}) ==="

# --- Linux (native, CGO enabled for Fyne GL) ---
echo ""
echo "==> Linux ${GOARCH}..."
CGO_ENABLED=1 GOOS=linux GOARCH="${GOARCH}" \
    go build -o "${OUT}/mcprofiles-linux-${GOARCH}" .
echo "    Done: mcprofiles-linux-${GOARCH}"

# --- Windows amd64 (MinGW cross-compile) ---
echo ""
echo "==> Windows amd64..."
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
    go build -ldflags "-H windowsgui" -o "${OUT}/mcprofiles-windows-amd64.exe" .
echo "    Done: mcprofiles-windows-amd64.exe"

# --- Windows arm64 (llvm-mingw cross-compile) ---
echo ""
echo "==> Windows arm64..."
CGO_ENABLED=1 CC=aarch64-w64-mingw32-gcc GOOS=windows GOARCH=arm64 \
    go build -ldflags "-H windowsgui" -o "${OUT}/mcprofiles-windows-arm64.exe" .
echo "    Done: mcprofiles-windows-arm64.exe"

# --- Create archives ---
echo ""
echo "==> Creating archives..."
cd "${OUT}"
for f in mcprofiles-*.exe; do
    base="${f%.exe}"
    zip "${base}.zip" "${f}"
    rm "${f}"
done
for f in mcprofiles-linux-*; do
    [ -f "$f" ] || continue
    # Skip already-archived files
    case "$f" in *.tar.gz) continue ;; esac
    tar czf "${f}.tar.gz" "${f}"
    rm "${f}"
done

echo ""
echo "=== Build complete ==="
ls -lh "${OUT}/"
