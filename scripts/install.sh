#!/bin/sh
# Bootstrap from a locally built executable or a trusted release artifact.
set -eu
umask 077
if [ "$#" -ne 2 ]; then
    echo "usage: sudo sh scripts/install.sh --release vX.Y.Z | BINARY_OR_HTTPS_URL TRUSTED_SHA256" >&2
    exit 2
fi
[ "$(id -u)" = 0 ] && [ "$(uname -s)" = Linux ] || { echo "Linux root required" >&2; exit 1; }
if [ "$1" = --release ]; then
    version=$2
    printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || { echo "expected vMAJOR.MINOR.PATCH" >&2; exit 2; }
    case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo "unsupported architecture" >&2; exit 2;; esac
    base="https://github.com/howardshaw/singforge/releases/download/$version"
    asset="singforge-linux-$arch"
    sums=$(curl --proto '=https' --proto-redir '=https' -fsSL --retry 2 --max-time 60 "$base/SHA256SUMS")
    checksum=$(printf '%s\n' "$sums" | awk -v asset="$asset" '$2 == asset {print $1}')
    set -- "$base/$asset" "$checksum"
fi
case "$2" in *[!a-f0-9]*|'') echo "invalid SHA-256" >&2; exit 2;; esac
[ "${#2}" = 64 ] || exit 2
dest=/usr/local/bin/singforge
if [ -e "$dest" ] || [ -L "$dest" ]; then echo "destination exists; use singforge self update" >&2; exit 1; fi
mkdir -p /usr/local/bin
tmp=$(mktemp /usr/local/bin/.singforge.XXXXXXXX)
trap 'rm -f "$tmp"' EXIT
case "$1" in
 https://*) curl --proto '=https' --proto-redir '=https' --fail --location --retry 2 --max-time 180 "$1" -o "$tmp";;
 *) cat -- "$1" > "$tmp";;
esac
printf '%s  %s\n' "$2" "$tmp" | sha256sum -c --status
chmod 755 "$tmp"
"$tmp" --version
ln "$tmp" "$dest"
