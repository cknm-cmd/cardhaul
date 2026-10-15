#!/bin/bash
# Builds every CardHaul download into dist/:
#   CardHaul-<ver>-mac.dmg               drag-to-Applications disk image
#   CardHaul-<ver>-mac.zip               the same app, zipped
#   CardHaul-<ver>-windows-setup.exe     per-user installer (no admin needed)
#   CardHaul-<ver>-windows-portable.exe  single file, runs from anywhere
#
# Needs: Go, Node, wails3, makensis (brew install makensis), and .env.local with the
# Google OAuth client (CARDHAUL_GOOGLE_CLIENT_ID / CARDHAUL_GOOGLE_CLIENT_SECRET).
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$HOME/go/bin:$PATH"
# makensis crashes (std::bad_alloc) when no UTF-8 locale is set.
export LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8

VERSION=$(sed -nE 's/^ *version: "([^"]+)".*/\1/p' build/config.yml | head -1)
[ -f .env.local ] || { echo "Missing .env.local (Google OAuth client). See README."; exit 1; }
command -v makensis >/dev/null || { echo "Missing makensis: brew install makensis"; exit 1; }

echo "Building CardHaul $VERSION"
rm -rf dist && mkdir -p dist

echo "• macOS app + disk image"
wails3 task darwin:package:dmg >/dev/null
cp "bin/CardHaul.dmg" "dist/CardHaul-$VERSION-mac.dmg"
ditto -c -k --keepParent "bin/CardHaul.app" "dist/CardHaul-$VERSION-mac.zip"

echo "• Windows installer + portable"
wails3 task windows:create:nsis:installer ARCH=amd64 INSTALL_SCOPE=user >/dev/null
cp "bin/CardHaul-$VERSION-windows-setup.exe" dist/
cp "bin/CardHaul.exe" "dist/CardHaul-$VERSION-windows-portable.exe"

echo
ls -lh dist | awk 'NR>1 {printf "  %-42s %s\n", $9, $5}'
