#!/bin/sh
set -eu

REPO="nisrulz/side-eye"
BIN="side-eye"

# The release sources are overridable so the E2E test can run the script
# against a local mock server (also useful for self-hosted mirrors).
api_base="${SIDE_EYE_INSTALL_API_BASE:-https://api.github.com/repos/$REPO}"
download_base="${SIDE_EYE_INSTALL_DOWNLOAD_BASE:-https://github.com/$REPO}"

arch=$(uname -m)
os=$(uname -s | tr '[:upper:]' '[:lower:]')

case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch"
    exit 1
    ;;
esac

case "$os" in
  darwin | linux) ;;
  *)
    echo "Unsupported OS: $os"
    exit 1
    ;;
esac

tag=$(curl -sfL "$api_base/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
[ -z "$tag" ] && { echo "Could not fetch latest release"; exit 1; }

# Strip leading 'v' from tag for asset names (GoReleaser default)
version=${tag#v}

archive="${BIN}_${version}_${os}_${arch}.tar.gz"
url="$download_base/releases/download/$tag/$archive"

# Do all work in a temp dir so the script never depends on (or pollutes) the cwd
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading $BIN $tag ($os/$arch)..."
curl -sfL "$url" -o "$tmpdir/$archive"

checksums_url="$download_base/releases/download/$tag/checksums.txt"
expected=$(curl -sfL "$checksums_url" | awk -v archive="$archive" '$2 == archive { print $1; exit }')
if [ ${#expected} -ne 64 ] || [ -n "$(printf '%s' "$expected" | tr -d '0-9a-fA-F')" ]; then
  echo "  ! Missing or invalid checksum for $archive"
  exit 1
fi
expected=$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmpdir/$archive" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmpdir/$archive" | cut -d' ' -f1)
fi
if [ "$actual" != "$expected" ]; then
  echo "  ! Checksum mismatch. Aborting."
  exit 1
fi
echo "  ✓ Checksum verified"

# Extract (binary may be in a versioned subdirectory)
tar xzf "$tmpdir/$archive" -C "$tmpdir"

dst_dir="$HOME/go/bin"
dst="$dst_dir/$BIN"
mkdir -p "$dst_dir"

if [ -d "$dst" ]; then
  echo "  ! $dst exists as a directory — please remove it and re-run"
  exit 1
fi

bin_file=$(find "$tmpdir" -name "$BIN" -type f -print -quit)
if [ -z "$bin_file" ]; then
  echo "  ! Could not find $BIN in the release archive"
  exit 1
fi

mv "$bin_file" "$dst"
chmod +x "$dst"
echo "  ✓ Installed $BIN to $dst"

# Ensure go/bin is on PATH
go_bin_expanded="${HOME}/go/bin"
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$go_bin_expanded"; then
  rc_name=""
  for f in ".zshrc" ".bashrc" ".bash_profile" ".zprofile"; do
    [ -f "${HOME}/$f" ] && rc_name="$f" && break
  done
  [ -z "$rc_name" ] && rc_name=".zshrc"
  rc="${HOME}/$rc_name"
  if ! grep -qE "(export PATH=.*(go/bin|${HOME}/go/bin))" "$rc" 2>/dev/null; then
    echo "export PATH=\"\$HOME/go/bin:\$PATH\"" >> "$rc"
    echo "  ➜ Added ~/go/bin to ~/$rc_name (run: source ~/$rc_name)"
  fi
fi

echo "  ➜ Run '$BIN --help' to see usage"
