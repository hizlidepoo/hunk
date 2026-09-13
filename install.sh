#!/bin/sh
# Install hunk, a themeable diff viewer for the terminal.
#
#   curl -fsSL https://raw.githubusercontent.com/wmarquardt/hunk/main/install.sh | sh
#
# Environment:
#   HUNK_VERSION       tag to install, e.g. v0.1.0 (default: the latest release)
#   HUNK_INSTALL_DIR   where the binary lands (default: ~/.local/bin)
set -eu

REPO="wmarquardt/hunk"
BIN="hunk"
INSTALL_DIR="${HUNK_INSTALL_DIR:-$HOME/.local/bin}"

die() {
	echo "install: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"
}

need tar
need uname
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
	resolve() { curl -fsSL -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
	resolve() { wget -qS -O /dev/null "$1" 2>&1 | awk '/^  Location: /{u=$2} END{print u}'; }
else
	die "curl or wget is required"
fi

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported OS: $(uname -s) — build from source with: go install github.com/$REPO@latest" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

version="${HUNK_VERSION:-}"
if [ -z "$version" ]; then
	# The /latest page redirects to the newest tag, which costs no API quota.
	version=$(resolve "https://github.com/$REPO/releases/latest" | sed 's|.*/tag/||')
	[ -n "$version" ] || die "could not determine the latest version — set HUNK_VERSION"
fi

archive="${BIN}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "downloading $BIN $version ($os/$arch)"
fetch "$base/$archive" "$tmp/$archive" || die "no release asset for $os/$arch at $version"

# Verify against the release checksums when a hashing tool is available.
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	sha256() { echo skip; }
fi

if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	want=$(awk -v f="$archive" '$2 == f || $2 == "*"f {print $1}' "$tmp/checksums.txt")
	got=$(sha256 "$tmp/$archive")
	if [ "$got" != skip ] && [ -n "$want" ] && [ "$want" != "$got" ]; then
		die "checksum mismatch for $archive"
	fi
fi

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BIN" ] || die "archive did not contain $BIN"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN" 2>/dev/null ||
	die "could not write to $INSTALL_DIR — set HUNK_INSTALL_DIR to a writable path"

echo "installed: $INSTALL_DIR/$BIN"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "note: $INSTALL_DIR is not on your PATH — add it to your shell profile" ;;
esac
