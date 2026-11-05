#!/bin/bash
set -euo pipefail
version="${1:-0.0.0-dev}"
architecture="$(go env GOARCH)"
case "$architecture" in arm64|amd64) ;; *) echo 'Unsupported macOS architecture' >&2; exit 1;; esac
bundle="dist/BurnBar.app"
mkdir -p "$bundle/Contents/MacOS"
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$bundle/Contents/MacOS/burnbar" ./cmd/burnbar
cat > "$bundle/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>io.github.nooblk-98.burnbar</string>
<key>CFBundleName</key><string>BurnBar</string>
<key>CFBundleExecutable</key><string>burnbar</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>${version%%-*}</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>15.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
# Local ad-hoc signing makes the bundle structurally valid. This is NOT a
# Developer ID signature, notarization or a claim of Gatekeeper trust.
codesign --force --deep --sign - "$bundle"
archive="burnbar-macos-${architecture}.zip"
(cd dist && /usr/bin/ditto -c -k --keepParent BurnBar.app "$archive")
(cd dist && shasum -a 256 "$archive" > "$archive.sha256")
