#!/usr/bin/env bash
# Fetch a prebuilt llama.cpp `llama-server` (Apple Silicon, Metal) and stage it +
# its shared libraries into resources/llama/ so the app can run models offline.
#
#   bash scripts/fetch-llama-macos.sh            # latest release, macos-arm64
#   LLAMA_ASSET=macos-x64 bash scripts/...       # Intel Mac
#   LLAMA_TAG=b4321 bash scripts/...             # pin a release
set -euo pipefail
cd "$(dirname "$0")/.."                            # desktop/paperko

ASSET_PAT="${LLAMA_ASSET:-macos-arm64}"
DEST="resources/llama"
API="https://api.github.com/repos/ggml-org/llama.cpp/releases/${LLAMA_TAG:+tags/$LLAMA_TAG}"
[ -n "${LLAMA_TAG:-}" ] || API="https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"

echo "==> resolving llama.cpp release ($ASSET_PAT)"
URL="$(curl -fsSL "$API" \
  | grep -oE '"browser_download_url":\s*"[^"]+"' \
  | sed -E 's/.*"(https[^"]+)"/\1/' \
  | grep -iE "bin-${ASSET_PAT}\.(zip|tar\.gz)$" | head -1 || true)"
[ -n "$URL" ] || { echo "no macos asset found (pattern: bin-${ASSET_PAT}.(zip|tar.gz))"; exit 1; }
echo "    $URL"

TMP="$(mktemp -d)"
curl -fsSL -o "$TMP/pkg" "$URL"
mkdir -p "$TMP/x"
case "$URL" in
  *.tar.gz|*.tgz) tar -xzf "$TMP/pkg" -C "$TMP/x" ;;
  *.zip)          unzip -q -o "$TMP/pkg" -d "$TMP/x" ;;
  *)              echo "unknown archive type"; exit 1 ;;
esac

BIN="$(find "$TMP/x" -type f -name llama-server | head -1)"
[ -n "$BIN" ] || { echo "llama-server not found in archive"; exit 1; }
SRC="$(dirname "$BIN")"

rm -rf "$DEST"; mkdir -p "$DEST"
# copy the server + every shared library beside it. Use -R to PRESERVE the
# versioned symlinks (libX.0.dylib -> libX.0.1.1.dylib) the binary loads via @rpath.
cp -R "$SRC"/llama-server "$DEST"/
cp -R "$SRC"/*.dylib "$DEST"/ 2>/dev/null || true
cp -R "$SRC"/*.so "$DEST"/ 2>/dev/null || true
chmod +x "$DEST"/llama-server
# make the binary find its sibling libs (rpath = same dir)
install_name_tool -add_rpath "@loader_path" "$DEST/llama-server" 2>/dev/null || true
codesign --force --sign - "$DEST/llama-server" 2>/dev/null || true

echo "==> staged into $DEST:"
ls -1 "$DEST" | sed 's/^/    /'
rm -rf "$TMP"
echo "OK. Test:  DYLD_LIBRARY_PATH=$DEST $DEST/llama-server --version"
