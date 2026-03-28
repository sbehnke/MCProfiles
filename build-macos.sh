#!/bin/bash
set -euo pipefail

# Load .env if present (values can still be overridden via environment)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "${SCRIPT_DIR}/.env" ]; then
    set -a
    source "${SCRIPT_DIR}/.env"
    set +a
fi

APP_NAME="MC Profile Editor"
BUNDLE_ID="dev.moat.mcprofiles"
EXECUTABLE="mcprofiles"
VERSION="1.0.1"

# --- Codesigning & Notarization ---
# Set these via environment variables or edit here.
# CODESIGN_IDENTITY: "Developer ID Application: Your Name (TEAMID)"
#   Find yours with: security find-identity -v -p codesigning
#   Set to "-" for ad-hoc signing (no notarization possible)
#   Leave empty to skip signing entirely.
CODESIGN_IDENTITY="${CODESIGN_IDENTITY:-}"

# NOTARIZE_PROFILE: a stored notarytool credential profile name.
#   Create one with:
#     xcrun notarytool store-credentials "MCProfiles"
#       --apple-id you@example.com
#       --team-id YOURTEAMID
#       --password <app-specific-password>
#   Leave empty to skip notarization.
NOTARIZE_PROFILE="${NOTARIZE_PROFILE:-}"

BUILD_DIR="${SCRIPT_DIR}/build"
APP_DIR="${BUILD_DIR}/${APP_NAME}.app"
CONTENTS="${APP_DIR}/Contents"
MACOS_DIR="${CONTENTS}/MacOS"
RESOURCES_DIR="${CONTENTS}/Resources"
ICON_BUNDLE="${SCRIPT_DIR}/resources/AppIcon.icon"

echo "==> Cleaning build directory..."
rm -rf "${BUILD_DIR}"
mkdir -p "${MACOS_DIR}" "${RESOURCES_DIR}"

# --- Build the Go binary ---
echo "==> Building Go binary..."
CGO_ENABLED=1 go build -o "${MACOS_DIR}/${EXECUTABLE}" "${SCRIPT_DIR}"

# --- Copy the .icon bundle (primary icon for Tahoe) ---
echo "==> Copying Icon Composer bundle for Tahoe..."
cp -R "${ICON_BUNDLE}" "${RESOURCES_DIR}/AppIcon.icon"

# --- Generate .icns fallback (pre-Tahoe) from icon.json + SVG ---
echo "==> Generating .icns fallback with gradient background..."
ICONSET_DIR="${BUILD_DIR}/AppIcon.iconset"
mkdir -p "${ICONSET_DIR}"

# Swift script that reads icon.json, renders gradient + SVG composite
cat > "${BUILD_DIR}/render_icon.swift" << 'SWIFT'
import AppKit
import Foundation

// --- Parse arguments ---
let args = CommandLine.arguments
guard args.count == 4 else {
    fputs("Usage: render_icon <icon_bundle_path> <output_png> <size>\n", stderr)
    exit(1)
}

let iconBundlePath = args[1]
let outputPath = args[2]
guard let size = Int(args[3]) else {
    fputs("Invalid size\n", stderr)
    exit(1)
}

// --- Parse icon.json ---
let iconJsonURL = URL(fileURLWithPath: iconBundlePath).appendingPathComponent("icon.json")
guard let jsonData = try? Data(contentsOf: iconJsonURL),
      let json = try? JSONSerialization.jsonObject(with: jsonData) as? [String: Any] else {
    fputs("Cannot parse icon.json\n", stderr)
    exit(1)
}

// Extract gradient color (Display P3)
var gradientColor = NSColor(displayP3Red: 0.15, green: 0.36, blue: 0.92, alpha: 1.0)
if let fill = json["fill"] as? [String: Any],
   let colorStr = fill["automatic-gradient"] as? String {
    // Parse "display-p3:R,G,B,A"
    let parts = colorStr.split(separator: ":")
    if parts.count == 2 {
        let components = parts[1].split(separator: ",").compactMap { Double($0) }
        if components.count == 4 {
            gradientColor = NSColor(displayP3Red: components[0],
                                     green: components[1],
                                     blue: components[2],
                                     alpha: components[3])
        }
    }
}

// Extract gradient orientation
var gradStartY: CGFloat = 0.0
var gradStopY: CGFloat = 0.7
if let fill = json["fill"] as? [String: Any],
   let orientation = fill["orientation"] as? [String: Any],
   let start = orientation["start"] as? [String: Double],
   let stop = orientation["stop"] as? [String: Double] {
    gradStartY = CGFloat(start["y"] ?? 0.0)
    gradStopY = CGFloat(stop["y"] ?? 0.7)
}

// Extract layer info
var svgName = "mc_block.svg"
var layerScale: CGFloat = 7.0
var translationX: CGFloat = 0.0
var translationY: CGFloat = 0.0

if let groups = json["groups"] as? [[String: Any]],
   let firstGroup = groups.first,
   let layers = firstGroup["layers"] as? [[String: Any]],
   let firstLayer = layers.first {
    if let imageName = firstLayer["image-name"] as? String {
        svgName = imageName
    }
    if let position = firstLayer["position"] as? [String: Any] {
        if let scale = position["scale"] as? Double {
            layerScale = CGFloat(scale)
        }
        if let trans = position["translation-in-points"] as? [Double], trans.count == 2 {
            translationX = CGFloat(trans[0])
            translationY = CGFloat(trans[1])
        }
    }
}

// --- Load SVG asset ---
let svgPath = URL(fileURLWithPath: iconBundlePath)
    .appendingPathComponent("Assets")
    .appendingPathComponent(svgName)
guard let svgData = try? Data(contentsOf: svgPath),
      let svgImage = NSImage(data: svgData) else {
    fputs("Cannot load SVG: \(svgName)\n", stderr)
    exit(1)
}

// --- Render composite icon ---
let sz = CGFloat(size)
let image = NSImage(size: NSSize(width: sz, height: sz))
image.lockFocus()

guard let ctx = NSGraphicsContext.current?.cgContext else {
    fputs("No graphics context\n", stderr)
    exit(1)
}

// 1. Draw gradient background (full square — macOS applies the squircle mask)
let colorSpace = CGColorSpace(name: CGColorSpace.displayP3) ?? CGColorSpaceCreateDeviceRGB()

// Create lighter and darker variants for gradient
let baseComponents = gradientColor.usingColorSpace(.displayP3)
let r = baseComponents?.redComponent ?? 0.15
let g = baseComponents?.greenComponent ?? 0.36
let b = baseComponents?.blueComponent ?? 0.92

// Lighter at top, base color at gradient stop point
let lightColor = CGColor(colorSpace: colorSpace, components: [
    min(r + 0.15, 1.0), min(g + 0.15, 1.0), min(b + 0.08, 1.0), 1.0
])!
let darkColor = CGColor(colorSpace: colorSpace, components: [r, g, b, 1.0])!

// Fill entire canvas with dark color first to ensure no transparent pixels
ctx.setFillColor(darkColor)
ctx.fill(CGRect(x: 0, y: 0, width: sz, height: sz))

// Draw gradient from top to the stop point; dark color already covers the rest
// NSImage coords: y=0 is bottom, y=sz is top
if let gradient = CGGradient(colorsSpace: colorSpace, colors: [darkColor, lightColor] as CFArray,
                              locations: [0.0, 1.0]) {
    // Gradient goes from stop point (bottom of gradient) up to start point (top)
    let gradBottom = sz * (1.0 - gradStopY)  // 30% up from bottom
    let gradTop = sz * (1.0 - gradStartY)    // top of canvas
    ctx.drawLinearGradient(gradient,
                           start: CGPoint(x: sz / 2, y: gradBottom),
                           end: CGPoint(x: sz / 2, y: gradTop),
                           options: [])
}

// 2. Draw SVG layer centered with scale
// Scale value from icon.json is relative to a reference icon size.
// The SVG should fill a portion of the icon based on the scale factor.
// Scale 7 in a 1024pt icon means ~7/10 of the icon size.
let svgDrawSize = sz * (layerScale / 10.0)
let svgX = (sz - svgDrawSize) / 2.0 + (translationX / 1024.0 * sz)
let svgY = (sz - svgDrawSize) / 2.0 - (translationY / 1024.0 * sz)

ctx.setAlpha(1.0)
svgImage.draw(in: NSRect(x: svgX, y: svgY, width: svgDrawSize, height: svgDrawSize),
              from: NSRect(origin: .zero, size: svgImage.size),
              operation: .sourceOver, fraction: 1.0)

image.unlockFocus()

// --- Export PNG ---
guard let tiffData = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiffData),
      let pngData = bitmap.representation(using: .png, properties: [:]) else {
    fputs("Failed to render PNG\n", stderr)
    exit(1)
}

try! pngData.write(to: URL(fileURLWithPath: outputPath))
SWIFT

# Required icon sizes: name -> pixel size
declare -a ICON_SPECS=(
    "icon_16x16:16"
    "icon_16x16@2x:32"
    "icon_32x32:32"
    "icon_32x32@2x:64"
    "icon_128x128:128"
    "icon_128x128@2x:256"
    "icon_256x256:256"
    "icon_256x256@2x:512"
    "icon_512x512:512"
    "icon_512x512@2x:1024"
)

for spec in "${ICON_SPECS[@]}"; do
    name="${spec%%:*}"
    size="${spec##*:}"
    echo "    ${name}.png (${size}x${size})"
    swift "${BUILD_DIR}/render_icon.swift" "${ICON_BUNDLE}" "${ICONSET_DIR}/${name}.png" "${size}"
done

echo "==> Creating .icns..."
iconutil -c icns "${ICONSET_DIR}" -o "${RESOURCES_DIR}/AppIcon.icns"

# --- Write Info.plist ---
echo "==> Writing Info.plist..."
cat > "${CONTENTS}/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleExecutable</key>
    <string>${EXECUTABLE}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIconName</key>
    <string>AppIcon</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSSupportsAutomaticGraphicsSwitching</key>
    <true/>
</dict>
</plist>
PLIST

# --- Clean up temp files ---
rm -f "${BUILD_DIR}/render_icon.swift"
rm -rf "${ICONSET_DIR}"

# --- Codesign ---
if [ -n "${CODESIGN_IDENTITY}" ]; then
    echo "==> Code signing with: ${CODESIGN_IDENTITY}"

    # Sign the binary with hardened runtime (required for notarization)
    codesign --force --options runtime \
        --sign "${CODESIGN_IDENTITY}" \
        --timestamp \
        --entitlements /dev/stdin \
        "${MACOS_DIR}/${EXECUTABLE}" << 'ENTITLEMENTS'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.security.cs.allow-unsigned-executable-memory</key>
    <true/>
    <key>com.apple.security.cs.disable-library-validation</key>
    <true/>
</dict>
</plist>
ENTITLEMENTS

    # Sign the app bundle
    codesign --force --options runtime \
        --sign "${CODESIGN_IDENTITY}" \
        --timestamp \
        "${APP_DIR}"

    echo "    Verifying signature..."
    codesign --verify --deep --strict --verbose=2 "${APP_DIR}" 2>&1 | tail -1
else
    echo "==> Skipping code signing (set CODESIGN_IDENTITY to enable)"
fi

# --- Notarize ---
if [ -n "${CODESIGN_IDENTITY}" ] && [ "${CODESIGN_IDENTITY}" != "-" ] && [ -n "${NOTARIZE_PROFILE}" ]; then
    echo "==> Creating zip for notarization..."
    ZIP_PATH="${BUILD_DIR}/${APP_NAME}.zip"
    ditto -c -k --keepParent "${APP_DIR}" "${ZIP_PATH}"

    echo "==> Submitting for notarization (profile: ${NOTARIZE_PROFILE})..."
    xcrun notarytool submit "${ZIP_PATH}" \
        --keychain-profile "${NOTARIZE_PROFILE}" \
        --wait

    echo "==> Stapling notarization ticket..."
    xcrun stapler staple "${APP_DIR}"

    echo "    Verifying notarization..."
    spctl --assess --type execute --verbose=2 "${APP_DIR}" 2>&1 | tail -1

    # Re-create zip with stapled ticket
    rm -f "${ZIP_PATH}"
    ditto -c -k --keepParent "${APP_DIR}" "${ZIP_PATH}"
    echo "    Distribution zip: ${ZIP_PATH}"
elif [ -n "${CODESIGN_IDENTITY}" ] && [ "${CODESIGN_IDENTITY}" = "-" ]; then
    echo "==> Skipping notarization (ad-hoc signing)"
elif [ -n "${CODESIGN_IDENTITY}" ]; then
    echo "==> Skipping notarization (set NOTARIZE_PROFILE to enable)"
    echo "    Create a profile with:"
    echo "      xcrun notarytool store-credentials \"MCProfiles\" \\"
    echo "        --apple-id you@example.com \\"
    echo "        --team-id YOURTEAMID \\"
    echo "        --password <app-specific-password>"
fi

echo ""
echo "==> Build complete: ${APP_DIR}"
echo "    Tahoe: uses AppIcon.icon (layered, gradient, dynamic)"
echo "    Pre-Tahoe: uses AppIcon.icns (gradient baked in)"
echo "    Run with: open \"${APP_DIR}\""
