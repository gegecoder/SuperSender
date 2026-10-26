#!/bin/sh
# Builds a macOS-installable .dmg from an assembled .app bundle on Linux.
# Runs inside the wails-cross container so it works from Windows/Linux hosts
# (hdiutil is macOS-only). Toolchain:
# xorriso (libisofs)    -> ISO9660 + Rock Ridge + native HFS+ hybrid image
#                            with an Apple Partition Map. Unlike cdrkit's
#                            genisoimage -hfs (which only records symlinks in
#                            Rock Ridge metadata), libisofs writes real HFS+
#                            catalog symlink records (UNIX_SYMLINK), so the
#                            drag-to-install Applications link is visible when
#                            Finder mounts the HFS+ partition.
#   dmg (libdmg-hfsplus) -> UDIF UDZO compressed .dmg (koly trailer),
#                            mountable by Finder / DiskImageMounter.
#                            Use the "dmg dmg" subcommand; "dmg build" is
#                            buggy (segfault) in mozilla's master build.
#
# Usage: darwin-dmg.sh <out_dir> <app_name> [arch]
#   out_dir  e.g. build/darwin/build (relative to WORKDIR /app)
#   app_name e.g. XinText
#   arch     universal (default) | amd64 | arm64, appended to the artifact name
set -eu

OUT_DIR="$1"
APP="$2"
ARCH="${3:-universal}"

if [ -z "$OUT_DIR" ] || [ -z "$APP" ]; then
  echo "usage: darwin-dmg.sh <out_dir> <app_name> [arch]" >&2
  exit 1
fi

BUNDLE="$OUT_DIR/$APP.app"
FINAL="$OUT_DIR/$APP-darwin-$ARCH.dmg"

if [ ! -d "$BUNDLE" ]; then
  echo "error: $BUNDLE not found; run create:app:bundle + codesign first" >&2
  exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM

# Drag-to-install layout: XinText.app + Applications symlink.
# -R preserves symlink metadata; the Applications entry is created as a link.
cp -R "$BUNDLE" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

RAW="$OUT_DIR/.$APP-uncompressed.img"
rm -f "$RAW" "$FINAL"

# Build the hybrid image: -r Rock Ridge for non-Apple systems, -hfsplus the
# native HFS+ partition (with APM) that Finder mounts, -V sets the volume name
# shown in Finder. mtime is normalised (-uid/-gid root, -r sets sane perms).
xorriso -as mkisofs -r -hfsplus -V "$APP" -o "$RAW" "$STAGE" >/dev/null

# Convert to read-only UDZO compressed UDIF image.
dmg dmg "$RAW" "$FINAL"
rm -f "$RAW"

if [ ! -s "$FINAL" ]; then
  echo "error: $FINAL was not created" >&2
  exit 1
fi

# Sanity check: UDIF images end with a 512-byte koly trailer.
TRAILER=$(dd if="$FINAL" bs=1 skip=$(( $(stat -c %s "$FINAL") - 512 )) count=4 2>/dev/null)
if [ "$TRAILER" != "koly" ]; then
  echo "error: $FINAL is missing the UDIF koly trailer" >&2
  exit 1
fi

echo "Created $FINAL"
