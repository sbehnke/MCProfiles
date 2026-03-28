# MC Profile Editor

A cross-platform GUI application for editing Minecraft's `launcher_profiles.json`.

Built with [Go](https://go.dev) and [Fyne](https://fyne.io).

## Features

- **Load & save** `launcher_profiles.json` with lossless round-trip (preserves all fields)
- **Auto-detects** the profiles file across OS-specific and launcher-variant paths
- **View and edit** all profile fields: name, version, type, game directory, Java path, Java args, resolution
- **Profile icons**: displays base64-encoded custom icons and generates colored placeholders for named icons (Grass, Dirt, Diamond, etc.)
- **Icon picker**: choose from 25 built-in Minecraft icon names or select a custom image
- **Mods folder detection**: resolves the effective mods folder by inspecting version JSON for `-Dfabric.modsFolder=` overrides, with an **Open in Finder/Explorer/Files** button
- **Add and delete** profiles with confirmation dialogs
- **Remembers** the last opened file between sessions

## Detected Profile Paths

The app automatically searches these locations:

| OS | Paths |
|----|-------|
| **macOS** | `~/Library/Application Support/minecraft/` |
| **Windows** | `%APPDATA%\.minecraft/` |
| **Windows** (MS Store) | `%LOCALAPPDATA%\Packages\Microsoft.4297127D64EC6_8wekyb3d8bbwe\LocalCache\Local\minecraft\` |
| **Linux** | `~/.minecraft/` |
| **Linux** (Flatpak) | `~/.var/app/com.mojang.Minecraft/.minecraft/` |
| **Linux** (Snap) | `~/snap/mc-installer/current/.minecraft/` |

Both `launcher_profiles.json` and `launcher_profiles_microsoft_store.json` are checked at each location. You can also open any file manually via the toolbar.

## Prerequisites

- [Go](https://go.dev/dl/) 1.21+
- C compiler (required by Fyne's OpenGL backend)
  - **macOS**: Xcode Command Line Tools (`xcode-select --install`)
  - **Linux**: `gcc`, `libgl1-mesa-dev`, `xorg-dev` (Debian/Ubuntu) or equivalent
  - **Windows**: [MSYS2](https://www.msys2.org/) with MinGW-w64, or TDM-GCC

## Building

### macOS

```bash
./build-macos.sh
open "build/MC Profile Editor.app"
```

Produces a full `.app` bundle with:
- `AppIcon.icon` for macOS Tahoe (layered, gradient, dynamic rendering)
- `AppIcon.icns` fallback for pre-Tahoe (gradient baked in from `icon.json`)

#### Code signing & notarization

The build script supports signing and notarization via environment variables:

```bash
# Find your signing identity
security find-identity -v -p codesigning

# Store notarization credentials (one-time setup)
xcrun notarytool store-credentials "MCProfiles" \
  --apple-id you@example.com \
  --team-id YOURTEAMID \
  --password <app-specific-password>

# Build with signing + notarization
CODESIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
NOTARIZE_PROFILE="MCProfiles" \
./build-macos.sh
```

| Variable | Effect |
|----------|--------|
| `CODESIGN_IDENTITY` | Signs with the given identity. Use `"-"` for ad-hoc signing. |
| `NOTARIZE_PROFILE` | Submits to Apple, waits, and staples the ticket. Requires a valid (non-ad-hoc) signing identity. |

Both are optional — omit them for an unsigned development build.

### Linux

```bash
./build-linux.sh
./build/mcprofiles/mcprofiles
```

Produces a directory with the binary, `.desktop` file, SVG icon, and an `install.sh` script:

```bash
cd build/mcprofiles
./install.sh              # installs to /usr/local
./install.sh /usr         # or specify a prefix
```

### Windows

**Native build** (from Git Bash, MSYS2, or WSL):

```bash
./build-windows.sh
```

**Cross-compile from macOS or Linux** (requires MinGW):

```bash
# Install cross-compiler
brew install mingw-w64          # macOS
sudo apt install gcc-mingw-w64  # Debian/Ubuntu

./build-windows.sh
```

Produces `build/mcprofiles-windows/mcprofiles.exe`.

### Docker multi-arch build (all platforms)

Builds Linux and Windows binaries for both amd64 and arm64 using Docker:

```bash
./build-docker.sh
```

Produces 4 archives in `build/release/`:

| Artifact | Platform |
|----------|----------|
| `mcprofiles-linux-amd64.tar.gz` | Linux x86_64 |
| `mcprofiles-linux-arm64.tar.gz` | Linux aarch64 |
| `mcprofiles-windows-amd64.zip` | Windows x86_64 |
| `mcprofiles-windows-arm64.zip` | Windows ARM64 |

Requires Docker with buildx. On Apple Silicon, arm64 containers run natively and amd64 via Rosetta.

### Quick build (any platform, no packaging)

```bash
go build -o mcprofiles .
./mcprofiles
```

## Project Structure

```
MCProfiles/
  main.go            Entry point, window layout, toolbar, state management
  profiles.go        JSON types, custom marshal/unmarshal, load/save, path detection
  icons.go           Base64 icon decoding, placeholder generation
  ui_list.go         Sidebar profile list with icons
  ui_detail.go       Detail editing panel, icon picker, mods folder
  resources/
    AppIcon.icon/    macOS Icon Composer bundle (Tahoe-style layered icon)
  build-macos.sh     macOS .app bundle with codesign & notarization
  build-linux.sh     Linux build with .desktop integration
  build-windows.sh   Windows build (native or cross-compile)
  build-docker.sh    Docker multi-arch build (Linux + Windows, amd64 + arm64)
  Dockerfile         Build container with Fyne deps + MinGW + llvm-mingw
```

## License

MIT
