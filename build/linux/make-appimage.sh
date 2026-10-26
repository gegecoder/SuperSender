#!/bin/sh
# Build the Linux AppImage inside the wails-cross container.
#
# This replaces `wails3 generate appimage`, which wipes the AppDir on every
# run and re-downloads AppRun from GitHub with no retry and no mirror
# fallback — flaky networks therefore abort the whole packaging run (and on
# this Windows host the task was previously skipped entirely). Here the
# AppRun / linuxdeploy / runtime downloads are cached under
# build/linux/dependency (local-first) and fall back to build/linux/temp and a
# GitHub proxy mirror, so repeated runs are fast and reliable.
#
# Usage: sh make-appimage.sh <app-name> <out-dir-relative-to-/app> [target-arch]
set -e

APP_NAME="${1:?usage: make-appimage.sh <app-name> <out-dir> [arch]}"
OUT_DIR="${2:?usage: make-appimage.sh <app-name> <out-dir> [arch]}"
case "${3:-amd64}" in
    ""|amd64|x86_64) ARCH=x86_64 ;;
    *) echo "AppImage packaging for '$3' is not supported by the amd64 wails-cross container (linuxdeploy runs on the container host arch)." >&2; exit 1 ;;
esac

ROOT=/app
# TEMP lives on the Windows bind mount (Docker Desktop virtiofs/gRPC-FUSE):
# keep it ONLY as the download cache (AppRun/linuxdeploy persist across runs).
# Building the AppDir + squashfs directly on the bind mount is flaky — rm -rf
# followed by mkdir intermittently fails with ENOENT ("cannot create
# directory ... No such file or directory") due to attribute-cache lag. All
# write-heavy work therefore happens in container-local /tmp; only the final
# AppImage is copied back to /app.
TEMP="$ROOT/build/linux/temp"
# 手工下载的打包依赖（AppRun / linuxdeploy / runtime）放在 dependency 目录，
# 打包时优先引用，缺失时才联网下载，并把下载结果回存该目录供下次离线使用。
DEP="$ROOT/build/linux/dependency"
WORK=/tmp/supersender-appimage
APPDIR="$WORK/$APP_NAME-$ARCH.AppDir"
OUTPUT="$APP_NAME-$ARCH.AppImage"

mkdir -p "$TEMP"
rm -rf "$WORK"
mkdir -p "$WORK"
cd "$WORK"

# --- Assemble the AppDir (mirrors `wails3 generate appimage`) --------------
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin"
cp "$ROOT/$OUT_DIR/$APP_NAME" "$APPDIR/usr/bin/$APP_NAME"
chmod 0755 "$APPDIR/usr/bin/$APP_NAME"
cp "$ROOT/build/icon.png" "$APPDIR/.DirIcon"
ln -sf .DirIcon "$APPDIR/$APP_NAME.png"
cp "$ROOT/build/linux/$APP_NAME.desktop" "$APPDIR/$APP_NAME.desktop"

# --- Tooling downloads (cached in temp, mirror fallback) -------------------
# Try the direct GitHub URL first, then a list of proxies — which one works
# varies over time and per network, so rotate through all of them.
MIRRORS="direct https://gh-proxy.com/ https://mirror.gh-proxy.com/ https://ghproxy.net/"
download() {
    for m in $MIRRORS; do
        if [ "$m" = "direct" ]; then url="$1"; else url="$m$1"; fi
        echo "Downloading: $url"
        if curl -fL --connect-timeout 10 --retry 2 --retry-delay 2 --retry-all-errors -o "$2" "$url"; then
            return 0
        fi
    done
    echo "ERROR: failed to download $1 from all mirrors" >&2
    return 1
}

# 获取一个打包依赖：dependency/ 本地优先 → temp 缓存 → 联网下载（并回存 dependency/）
fetch_tool() {
    name="$1"
    url="$2"
    if [ -f "$DEP/$name" ]; then
        echo "Using local dependency: $DEP/$name"
        cp -f "$DEP/$name" "$TEMP/$name"
        return 0
    fi
    if [ -f "$TEMP/$name" ]; then
        echo "Using cached tool: $TEMP/$name"
        return 0
    fi
    download "$url" "$TEMP/$name"
    mkdir -p "$DEP" 2>/dev/null || true
    cp -f "$TEMP/$name" "$DEP/$name" 2>/dev/null || true
}

# The AppRun binary is NOT installed here: we replace it with a shell script
# after linuxdeploy (see "Bundle the C runtime" below). The downloaded binary
# is only kept as a fallback reference.
fetch_tool "AppRun-$ARCH" \
    "https://github.com/AppImage/AppImageKit/releases/download/continuous/AppRun-$ARCH"

fetch_tool "linuxdeploy-$ARCH.AppImage" \
    "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-$ARCH.AppImage"
chmod 0755 "$TEMP/linuxdeploy-$ARCH.AppImage"

# The type-2 runtime (the ELF stub prepended to the squashfs) is normally
# downloaded by appimagetool itself — un-cached, no retry, GitHub only.
# Cache it with the same mirror rotation; appimagetool gets --runtime-file.
fetch_tool "runtime-$ARCH" \
    "https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-$ARCH"

# linuxdeploy discovers `--plugin gtk` as linuxdeploy-plugin-gtk.sh in the CWD.
cp "$ROOT/build/linux/linuxdeploy-plugin-gtk.sh" "$WORK/linuxdeploy-plugin-gtk.sh"
chmod 0755 "$WORK/linuxdeploy-plugin-gtk.sh"

# --- GTK detection + WebKit helper processes -------------------------------
# linuxdeploy only bundles shared libraries. WebKitWebProcess /
# WebKitNetworkProcess / the injected-bundle lib are executables loaded at
# runtime, so they must be copied into the AppDir explicitly (this is what
# wails3's findGTKFiles does); without them the webview cannot start.
LDD_OUT=$(ldd "$APPDIR/usr/bin/$APP_NAME")
case "$LDD_OUT" in
    *libgtk-4.so*) GTKV=4; BUNDLE=libwebkitgtkinjectedbundle.so ;;
    *libgtk-3.so*) GTKV=3; BUNDLE=libwebkit2gtkinjectedbundle.so ;;
    *) echo "unable to determine GTK version from ldd output" >&2; exit 1 ;;
esac
for f in WebKitWebProcess WebKitNetworkProcess WebKitGPUProcess "$BUNDLE"; do
    src=$(find /usr -type f -name "$f" 2>/dev/null | head -n 1)
    [ -n "$src" ] || { echo "required WebKit component not found under /usr: $f" >&2; exit 1; }
    mkdir -p "$APPDIR$(dirname "$src")"
    cp "$src" "$APPDIR$(dirname "$src")/"
done

# --- Run linuxdeploy (bundle deps + GTK hook, no final AppImage yet) ---------
# NO_STRIP: trixie toolchains emit .relr.dyn sections that linuxdeploy's
# bundled strip cannot handle. We deliberately omit --output appimage here so
# linuxdeploy does not call appimagetool yet; the C runtime is added next and
# must not be patchelf'd by a second linuxdeploy pass (patchelf corrupts
# libc.so.6 / ld-linux). The final AppImage is produced by appimagetool below.
DEPLOY_GTK_VERSION="$GTKV" NO_STRIP=1 \
    "$TEMP/linuxdeploy-$ARCH.AppImage" --appimage-extract-and-run \
    --appdir "$APPDIR" --plugin gtk

# --- Bundle the C runtime (glibc) so the AppImage runs on older distros ------
# linuxdeploy deliberately excludes libc/libm/libstdc++/ld-linux, but the
# bundled GTK/WebKit stack was built against trixie's glibc 2.38 and refuses
# to load against a host's older glibc (e.g. RHEL 8 ships 2.28). We bundle
# the trixie C runtime and invoke its dynamic linker explicitly from a
# custom AppRun so the host loader is never used.
APPLIB="$APPDIR/usr/lib"
LIBDIR=$(getconf LIBDIR 2>/dev/null || echo /usr/lib/x86_64-linux-gnu)
# Debian puts the runtime loader under /lib64 and the libs under the multiarch
# dir; fall back to /usr/lib if either is missing.
LD_SRC="/lib64/ld-linux-x86-64.so.2"
[ -f "$LD_SRC" ] || LD_SRC="/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2"
cp -L "$LD_SRC" "$APPLIB/ld-linux-x86-64.so.2"

# Core C runtime libraries. On glibc 2.34+ libpthread/librt/libdl are merged
# into libc, but their SONAME stubs are still referenced by ELFs and must be
# present for the dynamic linker to resolve them.
for l in libc.so.6 libm.so.6 libstdc++.so.6 libdl.so.2 libpthread.so.0 \
         librt.so.1 libresolv.so.2 libutil.so.1 libgcc_s.so.1 libanl.so.1 \
         libcrypt.so.1; do
    for src in "$LIBDIR/$l" "/lib/x86_64-linux-gnu/$l" "/usr/lib/x86_64-linux-gnu/$l"; do
        if [ -f "$src" ]; then cp -L "$src" "$APPLIB/"; break; fi
    done
done

# NSS modules: the bundled glibc loads these via dlopen at runtime for DNS,
# passwd/group lookups. Without them networking and user resolution break.
for l in libnss_files.so.2 libnss_dns.so.2 libnss_compat.so.2 \
         libnss_myhostname.so.2 libnss_mdns.so.2 libnss_mdns4.so.2 \
         libnss_mdns6.so.2 libnss_mdns4_minimal.so.2 libnss_mdns6_minimal.so.2 \
         libnss_mdns_minimal.so.2 libnss_resolve.so.2 libnss_systemd.so.2; do
    for src in "$LIBDIR/$l" "/lib/x86_64-linux-gnu/$l" "/usr/lib/x86_64-linux-gnu/$l"; do
        if [ -f "$src" ]; then cp -L "$src" "$APPLIB/"; break; fi
    done
done

# iconv gconv modules (character-set conversion used by GLib/WebKit).
GCONV_SRC="/usr/lib/x86_64-linux-gnu/gconv"
[ -d "$GCONV_SRC" ] || GCONV_SRC="/usr/lib/gconv"
if [ -d "$GCONV_SRC" ]; then
    cp -a "$GCONV_SRC" "$APPLIB/gconv"
fi

# Support libraries that must NOT silently resolve to the host's older
# copies (e.g. RHEL8 freetype lacks FT_Get_Transform used by trixie cairo;
# old libX11/libgpg-error would break bundled cairo/gcrypt; Kylin V10's
# Mesa-20 libgbm lacks gbm_bo_create_with_modifiers2 required by trixie
# WebKit). Harmless if linuxdeploy already bundled them (same file re-copied).
# The hardware GL/GLX stack (libGL/libGLX) is NOT bundled on purpose: it must
# match the host GPU drivers. A software EGL fallback is bundled separately
# below and only used when the host has no usable DRI driver.
for l in libfreetype.so.6 libfontconfig.so.1 libexpat.so.1 libpng16.so.16 \
         libbz2.so.1.0 libbrotlidec.so.1 libbrotlicommon.so.1 \
         libharfbuzz.so.0 libharfbuzz-subset.so.0 libgraphite2.so.3 \
         libthai.so.0 libdatrie.so.1 libz.so.1 liblzma.so.5 libffi.so.8 \
         libpcre2-8.so.0 libmount.so.1 libblkid.so.1 libselinux.so.1 \
         libcap.so.2 \
         libX11.so.6 libX11-xcb.so.1 libxcb.so.1 \
         libwayland-client.so.0 libwayland-cursor.so.0 \
         libfribidi.so.0 libasound.so.2 libdrm.so.2 libgbm.so.1 \
         libcom_err.so.2 libgmp.so.10 libgpg-error.so.0; do
    for src in "$LIBDIR/$l" "/lib/x86_64-linux-gnu/$l" "/usr/lib/x86_64-linux-gnu/$l"; do
        if [ -f "$src" ]; then cp -L "$src" "$APPLIB/"; break; fi
    done
done

# --- Bundle a software EGL fallback (Mesa llvmpipe) ------------------------
# WebKitGTK 2.52 always initialises an EGL display in the WebProcess
# (WebProcess::initializePlatformDisplayIfNeeded, called from
# DrawingAreaCoordinatedGraphics::updatePreferences) and CRASHes when no EGL
# display can be created:
#   "Could not create default EGL display: ... Aborting..."
# The webview then stays white. Minimal/old distros without mesa-dri-drivers
# (CentOS 7) and VMs without a GPU driver hit exactly that. Verified on 2.52.6:
# neither WebKitSettings hardware-acceleration-policy=never nor
# WEBKIT_DISABLE_COMPOSITING_MODE skips that code path, so a working EGL is
# simply required. Ship Mesa's software rasteriser as a self-contained stack.
#
# Hardware drivers are still taken from the host and preferred at runtime:
# AppRun only puts this directory on the loader path when the host has no
# usable DRI driver. libLLVM dominates the size (~130 MB raw, ~30 MB in the
# zstd squashfs).
MESALIB="$APPLIB/mesa"
MESADRI="$MESALIB/dri"
mkdir -p "$MESADRI"
mesa_copy() {
    for src in "$LIBDIR/$1" "/lib/x86_64-linux-gnu/$1" "/usr/lib/x86_64-linux-gnu/$1"; do
        if [ -f "$src" ]; then cp -L "$src" "$MESALIB/"; return 0; fi
    done
    echo "missing Mesa component: $1" >&2
    return 1
}
# glvnd front-end (SONAME libEGL.so.1) + Mesa vendor back-end.
mesa_copy libEGL.so.1
mesa_copy libGLdispatch.so.0
mesa_copy libEGL_mesa.so.0
# llvmpipe/softpipe live in libgallium; dri/libdril_dri.so is only the shim.
GALLIUM=$(ls "$LIBDIR"/libgallium-*.so "/usr/lib/x86_64-linux-gnu"/libgallium-*.so 2>/dev/null | head -n1)
[ -n "$GALLIUM" ] || { echo "libgallium not found; cannot bundle software EGL fallback" >&2; exit 1; }
cp -L "$GALLIUM" "$MESALIB/"
SWRAST_SRC="$LIBDIR/dri/libdril_dri.so"
[ -f "$SWRAST_SRC" ] || SWRAST_SRC="$LIBDIR/dri/swrast_dri.so"
[ -f "$SWRAST_SRC" ] || { echo "Mesa software driver not found in $LIBDIR/dri" >&2; exit 1; }
cp -L "$SWRAST_SRC" "$MESADRI/libdril_dri.so"
ln -sf libdril_dri.so "$MESADRI/swrast_dri.so"
ln -sf libdril_dri.so "$MESADRI/kms_swrast_dri.so"
# Fill in the remaining closure (libLLVM, libz3, libelf, libxcb-*, ...),
# skipping whatever the AppDir already ships at usr/lib (glibc, libgbm, ...).
for so in "$MESALIB/libEGL_mesa.so.0" "$MESALIB/"libgallium-*.so "$MESADRI/libdril_dri.so"; do
    ldd "$so" 2>/dev/null | sed -n 's/.*=> \(\/[^ ]*\).*/\1/p'
done | sort -u | while read -r dep; do
    base=$(basename "$dep")
    case "$base" in ld-linux*|linux-vdso*) continue ;; esac
    [ -e "$APPLIB/$base" ] && continue
    [ -e "$MESALIB/$base" ] && continue
    cp -L "$dep" "$MESALIB/" 2>/dev/null || echo "warning: cannot copy $dep" >&2
done

# --- Patch the hardcoded WebKit helper path --------------------------------
# WebKitGTK 6.0 removed the WEBKIT_EXEC_PATH environment variable; the path to
# WebKitWebProcess / WebKitNetworkProcess is now a compile-time constant
# (/usr/lib/x86_64-linux-gnu/webkitgtk-6.0). On the target machine that
# directory does not exist, so spawn fails with "no such file or directory".
# We rewrite the constant in the bundled .so to a short fixed path and create
# a symlink at runtime (in AppRun) pointing into the AppDir.
WEBSO=$(find "$APPLIB" -name 'libwebkitgtk-6.0.so*' -type f | head -n1)
if [ -n "$WEBSO" ]; then
    python3 - "$WEBSO" <<'PY'
import sys
so = sys.argv[1]
data = open(so, "rb").read()
needle = b"/usr/lib/x86_64-linux-gnu/webkitgtk-6.0"
repl   = b"/tmp/.supersender-webkit6"
assert len(repl) < len(needle), "replacement path too long"
repl = repl + b"\x00" * (len(needle) - len(repl))
n = data.count(needle)
data = data.replace(needle, repl)
open(so, "wb").write(data)
print(f"patched {n} occurrence(s) of webkit exec path in {so}")
PY
fi
# Helpers are spawned by the app via fork+exec, so the kernel loads them with
# their PT_INTERP — the HOST's old ld-linux — which then picks up bundled
# libs from --library-path-less env and crashes on loader/libc version
# checks. Replace each helper with a tiny wrapper that execs the real ELF
# through the BUNDLED loader; no environment pollution needed.
HELPERS=$(find "$APPDIR/usr/lib" -type f \( -name WebKitWebProcess -o -name WebKitNetworkProcess -o -name WebKitGPUProcess \) 2>/dev/null)
for f in $HELPERS; do
    base=$(basename "$f")
    mv "$f" "$f.real"
    cat > "$f" <<WRAPPER_EOF
#!/bin/sh
d="\$(dirname "\$(readlink -f "\$0")")"
lp="\$d/../.."
# AppRun exports this only when the bundled software EGL fallback is in use.
[ -n "\$SUPERSENDER_LIB_EXTRA" ] && lp="\$lp:\$SUPERSENDER_LIB_EXTRA"
exec "\$d/../../ld-linux-x86-64.so.2" --library-path "\$lp" "\$d/${base}.real" "\$@"
WRAPPER_EOF
    chmod 0755 "$f"
done

# Custom AppRun: exec the bundled dynamic linker with the bundled library
# path. The host's older ld-linux/libc are never touched.
# CRITICAL: LD_LIBRARY_PATH must never be exported — the GTK hooks (and the
# app itself) spawn host binaries (dbus-send|tail|cut, pandoc, ...), and with
# LD_LIBRARY_PATH set those would load the bundled libc through the host's
# old loader and die on version checks. --library-path below scopes the
# bundled search to this process tree only.
cat > "$APPDIR/AppRun" <<'APPRUN_EOF'
#!/bin/sh
set -e
SELF="$(readlink -f "$0" 2>/dev/null || printf '%s' "$0")"
HERE="$(dirname "$SELF")"
export APPDIR="$HERE"
export PATH="$HERE/usr/bin:$PATH"

# GTK environment hooks (theme detection, schemas, pixbuf loaders, typelibs).
# Sourced while the environment is still clean.
if [ -d "$HERE/apprun-hooks" ]; then
    for hook in "$HERE"/apprun-hooks/*.sh; do
        [ -e "$hook" ] || continue
        . "$hook"
    done
fi

# iconv modules for the bundled glibc (GLib charset conversion); only
# processes running on the bundled libc consume it.
[ -d "$HERE/usr/lib/gconv" ] && export GCONV_PATH="$HERE/usr/lib/gconv${GCONV_PATH:+:$GCONV_PATH}"

# WebKitGTK 6.0 hardcodes its helper-process path (see the .so patch above).
# Create the fixed symlink target pointing at the AppDir's webkitgtk dir so
# WebKit finds WebKitWebProcess / WebKitNetworkProcess at runtime.
WEBDIR="$(find "$HERE/usr/lib" -type d -name 'webkitgtk-*' 2>/dev/null | head -n1)"
if [ -n "$WEBDIR" ] && [ -d "$WEBDIR" ]; then
    rm -f /tmp/.supersender-webkit6
    ln -s "$WEBDIR" /tmp/.supersender-webkit6 2>/dev/null || true
    # The JS injected-bundle lives in webkitgtk-6.0/injected-bundle/.
    [ -d "$WEBDIR/injected-bundle" ] && export WEBKIT_INJECTED_BUNDLE_PATH="$WEBDIR/injected-bundle"
fi

# Older/VM graphics stacks (RHEL8, virtio-gpu) often render a blank webview
# with the DMABUF renderer; the EGL compositing path is the safe fallback.
export WEBKIT_DISABLE_DMABUF_RENDERER=1

# GTK4's default GSK renderer (ngl/gl) needs a working GL driver too; 'cairo'
# is the pure-software renderer that works everywhere (an EGL fallback below
# still covers WebKit, which cannot avoid EGL).
export GSK_RENDERER="${GSK_RENDERER:-cairo}"

# Minimal/older distros ship no accessibility bus service; GTK then prints a
# GDBus.Error.ServiceUnknown warning on every start. 'none' disables the a11y
# bridge entirely (a plain text editor loses nothing).
export GTK_A11Y=none

# WebKit sandboxes its child processes with bwrap (/usr/bin/bwrap), which is
# absent on minimal/older distros (RHEL8 ships no bubblewrap). Disabling the
# sandbox avoids the spawn failure; for a local markdown editor this is an
# acceptable trade-off.
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1

# WebKitGTK 2.52 unconditionally needs an EGL display in its WebProcess and
# aborts ("Could not create default EGL display ... Aborting...", white
# window) when the host has none — e.g. CentOS 7 without mesa-dri-drivers or a
# VM without a GPU driver. Use the host's EGL (hardware, fast) when a DRI
# driver is installed; otherwise switch to the bundled Mesa llvmpipe stack.
# Override with SUPERSENDER_GL=host|soft|auto.
MESA_DIR="$HERE/usr/lib/mesa"
SOFT_GL=0
case "${SUPERSENDER_GL:-auto}" in
    soft)
        SOFT_GL=1
        ;;
    host)
        SOFT_GL=0
        ;;
    *)
        SOFT_GL=1
        for d in /usr/lib64/dri /usr/lib/x86_64-linux-gnu/dri /usr/lib/dri; do
            if [ -e "$d/swrast_dri.so" ] || [ -e "$d/libdril_dri.so" ] || [ -e "$d/kms_swrast_dri.so" ]; then
                SOFT_GL=0
                break
            fi
        done
        ;;
esac
if [ "$SOFT_GL" = 1 ] && [ -d "$MESA_DIR" ]; then
    export SUPERSENDER_LIB_EXTRA="$MESA_DIR"
    export LIBGL_DRIVERS_PATH="$MESA_DIR/dri"
    export MESA_LOADER_DRIVER_OVERRIDE="${MESA_LOADER_DRIVER_OVERRIDE:-swrast}"
    export GALLIUM_DRIVER="${GALLIUM_DRIVER:-llvmpipe}"
    export LIBGL_ALWAYS_SOFTWARE=1
    # glvnd resolves its vendor library through a JSON file that needs an
    # absolute path, and the AppImage mount point differs on every run, so the
    # file is generated here (fixed path: overwritten on each start).
    EGLV=/tmp/.supersender-egl
    if mkdir -p "$EGLV" 2>/dev/null; then
        printf '%s\n' \
            '{' \
            '    "file_format_version": "1.0.0",' \
            '    "ICD": {' \
            "        \"library_path\": \"$MESA_DIR/libEGL_mesa.so.0\"" \
            '    }' \
            '}' > "$EGLV/50_mesa.json" 2>/dev/null || true
        export __EGL_VENDOR_LIBRARY_DIRS="$EGLV"
    fi
fi

# NOTE: the "Overriding existing handler for signal 10" line is a harmless
# JSC notice (it wants SIGUSR1 for GC; the Go runtime already holds that
# signal). Do NOT set JSC_SIGNAL_FOR_GC to silence it — that variable is not
# a registered JSC option (WebKit bug #295276); any value prints
# "invalid option" and CRASHes when option validation is enabled.

# Taskbar/dock icon: without an installed .desktop entry matching the window's
# app-id (com.supersender.net), UKUI/GNOME taskbars show a generic gear. Do an
# AppImageLauncher-style runtime integration: install a user-level desktop
# entry + hicolor icon pointing at this AppImage. Idempotent; Exec is
# refreshed on every start in case the AppImage file was moved. Note the icon
# appears from the next launch (taskbars cache window-icon lookups).
if [ -n "$APPIMAGE" ] && [ -d "$HOME" ]; then
    APPS_DIR="$HOME/.local/share/applications"
    ICON_DIR="$HOME/.local/share/icons/hicolor/512x512/apps"
    DESK="$APPS_DIR/com.supersender.net.desktop"
    if mkdir -p "$APPS_DIR" "$ICON_DIR" 2>/dev/null; then
        cp -f "$HERE/.DirIcon" "$ICON_DIR/com.supersender.net.png" 2>/dev/null || true
        printf '%s\n' \
            '[Desktop Entry]' \
            'Type=Application' \
            'Name=SuperSender' \
            "Exec=\"$APPIMAGE\"" \
            'Icon=com.supersender.net' \
            'Terminal=false' \
            'Categories=Network;' \
            'StartupNotify=false' \
            'StartupWMClass=com.supersender.net' > "$DESK" 2>/dev/null || true
        command -v update-desktop-database >/dev/null 2>&1 && \
            update-desktop-database "$APPS_DIR" 2>/dev/null || true
    fi
fi

LIBPATH="$HERE/usr/lib"
[ -n "$SUPERSENDER_LIB_EXTRA" ] && LIBPATH="$LIBPATH:$SUPERSENDER_LIB_EXTRA"
exec "$HERE/usr/lib/ld-linux-x86-64.so.2" --library-path "$LIBPATH" \
    "$HERE/usr/bin/SuperSender" "$@"
APPRUN_EOF
chmod 0755 "$APPDIR/AppRun"

# --- Self-containment check ---------------------------------------------------
# Every shipped ELF must resolve ALL dependencies from inside the AppDir.
# Anything falling back to a host path would silently pick up an older
# library on the target system (the FT_Get_Transform failure class). Run on a
# /tmp copy: executing ELFs off the Windows bind mount can segfault.
CHECKROOT=/tmp/appdir-selfcheck
rm -rf "$CHECKROOT"
cp -a "$APPDIR" "$CHECKROOT"
CHECK_LD="$CHECKROOT/usr/lib/ld-linux-x86-64.so.2"
CHECK_LIB="$CHECKROOT/usr/lib"
CHECK_TARGETS="$(
    find "$CHECKROOT/usr/bin" -maxdepth 1 -type f
    find "$CHECKROOT/usr/lib" -type f -path '*webkitgtk*' \( -name '*.real' -o -name '*.so*' \)
    find "$CHECKROOT/usr/lib" -type f -name '*.so' -path '*gdk-pixbuf*'
    find "$CHECKROOT/usr/lib" -type f -name '*.so' -path '*gtk-4.0*'
    find "$CHECKROOT/usr/lib" -type f -name '*.so' -path '*gtk-3.0*'
)"
status=0
for e in $CHECK_TARGETS; do
    out=$("$CHECK_LD" --library-path "$CHECK_LIB" --list "$e" 2>&1) || { echo "ld --list failed for $e"; status=1; continue; }
    if printf '%s\n' "$out" | grep -q 'not found'; then
        echo "missing deps for $e:"
        printf '%s\n' "$out" | grep 'not found'
        status=1
    fi
    # libEGL/libGL/libGLX/libGLdispatch are intentionally host-provided
    # (glvnd + host GPU drivers); libgbm is bundled and must resolve internally.
    bad=$(printf '%s\n' "$out" | sed -n 's/.*=> \(\/[^ ]*\).*/\1/p' \
        | grep -v "^$CHECKROOT/" \
        | grep -vE '/lib(EGL\.so|GL\.so|GLX\.so|GLdispatch\.so)' || true)
    if [ -n "$bad" ]; then
        echo "host-resolved deps for $e:"
        echo "$bad"
        status=1
    fi
done
# The bundled software EGL fallback must be self-contained as well (it is only
# activated through the loader path, so validate it explicitly).
for e in "$CHECKROOT/usr/lib/mesa/libEGL_mesa.so.0" "$CHECKROOT/usr/lib/mesa/"libgallium-*.so "$CHECKROOT/usr/lib/mesa/dri/libdril_dri.so"; do
    [ -e "$e" ] || continue
    out=$("$CHECK_LD" --library-path "$CHECK_LIB:$CHECKROOT/usr/lib/mesa" --list "$e" 2>&1) || {
        echo "ld --list failed for $e"; status=1; continue; }
    if printf '%s\n' "$out" | grep -q 'not found'; then
        echo "missing deps for $e:"
        printf '%s\n' "$out" | grep 'not found'
        status=1
    fi
done
rm -rf "$CHECKROOT"
if [ "$status" -ne 0 ]; then
    echo "AppDir is not self-contained; aborting" >&2
    exit 1
fi

# --- Produce the final AppImage with appimagetool ---------------------------
# appimagetool is shipped inside the linuxdeploy AppImage's appimage plugin.
# Extract it once and cache it in temp, then run it directly on the AppDir so
# the bundled glibc runtime is packaged as-is (no patchelf, no re-deploy).
APPIMAGETOOL_DIR="$WORK/appimagetool-bin"
if [ ! -x "$APPIMAGETOOL_DIR/appimagetool" ]; then
    "$TEMP/linuxdeploy-$ARCH.AppImage" --appimage-extract >/dev/null 2>&1
    mkdir -p "$APPIMAGETOOL_DIR"
    # Copy appimagetool + its runtime deps (mksquashfs, desktop-file-validate).
    # appimagetool shells out to these; none are on the container PATH.
    cp squashfs-root/plugins/linuxdeploy-plugin-appimage/appimagetool-prefix/usr/bin/* "$APPIMAGETOOL_DIR/"
    chmod 0755 "$APPIMAGETOOL_DIR"/*
fi

PATH="$APPIMAGETOOL_DIR:$PATH" "$APPIMAGETOOL_DIR/appimagetool" --comp zstd \
    --runtime-file "$TEMP/runtime-$ARCH" "$APPDIR" "$WORK/$OUTPUT"

mv -f "$WORK/$OUTPUT" "$ROOT/$OUT_DIR/$OUTPUT"
echo "AppImage created: $OUT_DIR/$OUTPUT"
