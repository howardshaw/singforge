#!/bin/sh
# Download digest-pinned validators into a new disposable Linux amd64 directory.
set -eu
dir=$1
mkdir -m 700 "$dir"
cd "$dir"
get() {
    curl -fLsS --proto '=https' --proto-redir '=https' --retry 3 "$1" -o "$2"
    printf '%s  %s\n' "$3" "$2" | sha256sum -c --status
}
get https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64.tar.gz core.tgz 699a1b510aa4396b2176d32d1181920399ebf1e267835da6f3b598d0f7c24968
tar -xOzf core.tgz sing-box-1.14.4-linux-amd64/sing-box > sing-box
get https://github.com/MetaCubeX/mihomo/releases/download/v1.19.34/mihomo-linux-amd64-compatible-v1.19.34.gz mihomo.gz 2763f5b414527f7baf6dc57e3f82f3ab561eedf34bfd7c2f2cde1f7e592efe06
gzip -dc mihomo.gz > mihomo
get https://github.com/cloudflare/cloudflared/releases/download/2026.11.1/cloudflared-linux-amd64 cloudflared 97bb3b02d23a5980fc9c15a1a270e692da1923f103f920d5b16d4931d5ab55e7
chmod 755 sing-box mihomo cloudflared
