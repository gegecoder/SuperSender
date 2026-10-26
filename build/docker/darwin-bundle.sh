#!/bin/sh
# Assembles a macOS .app bundle from cross-compiled artifacts.
# Runs inside the wails-cross container so POSIX tools + exec bits are
# available regardless of the host OS (Windows hosts lack cp/mkdir semantics
# and cannot preserve the Mach-O executable bit).
#
# Usage: darwin-bundle.sh <out_dir> <app_name>
#   out_dir  e.g. build/darwin/build (relative to WORKDIR /app)
#   app_name e.g. XinText
set -e

OUT_DIR="$1"
APP="$2"

if [ -z "$OUT_DIR" ] || [ -z "$APP" ]; then
  echo "usage: darwin-bundle.sh <out_dir> <app_name>" >&2
  exit 1
fi

BUNDLE="$OUT_DIR/$APP.app"

rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp "build/darwin/icon.icns" "$BUNDLE/Contents/Resources/"
if [ -f "build/darwin/Assets.car" ]; then
  cp "build/darwin/Assets.car" "$BUNDLE/Contents/Resources/"
fi
cp "$OUT_DIR/$APP" "$BUNDLE/Contents/MacOS/$APP"
cp "build/darwin/Info.plist" "$BUNDLE/Contents/Info.plist"

echo "Assembled $BUNDLE"
