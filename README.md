# SingForge

SingForge deploys sing-box on a single Linux host. Its Go CLI uses Cobra. A direct installation runs one systemd
service. CDN access adds a dedicated Nginx process, named tunnels use cloudflared, and public subscriptions add a
loopback-only publisher behind HTTPS.

[中文说明](README.zh-CN.md) · [CLI reference](docs/cli.md) · [Operator guide](docs/operations.md) · [Protocol compatibility](docs/protocols.md) · [Architecture](docs/architecture.md) · [Validation](docs/validation.md)

## Install from this checkout

Build on a machine with Go 1.26.11 or newer. Production binaries target Linux amd64 and arm64. The build host can run
macOS. Docker is optional for development.

```sh
make check
make release
```

Copy the appropriate binary and the reviewed bootstrap script to the VPS. Supply the SHA-256 from your trusted build or
authenticated release channel. The bootstrap refuses to replace an existing executable. For an update, use the CLI.

```sh
sudo sh scripts/install.sh ./singforge-linux-amd64 <trusted-sha256>
sudo singforge install --mode direct --preset recommended --host 203.0.113.10
sudo singforge status
sudo singforge doctor
```

Replace the example address. On a VPS with a public address on its interface, omit `--host` for detection. On a NATed
VPS, pass its public address. The recommended preset listens on TCP 443 (Reality), UDP 8443 (Hysteria2) and TCP/UDP 8388
(Shadowsocks). Allow the selected ports in your host firewall and provider security group. Keep SSH open. The CLI leaves
firewall administration to the operator.

For a declarative installation, edit [direct.yaml](configs/direct.yaml) or [hybrid.yaml](configs/hybrid.yaml) and run
`sudo singforge install --config FILE`. Explicit flags override manifest values.

## Import a client

```sh
sudo singforge subscription show --format mihomo --output client.yaml
sudo singforge subscription show --format sing-box --output client.json
sudo singforge subscription show --format shadowsocks --filter direct-ss --qr
```

These commands reveal credentials. Files created through `--output` are mode 0600 and must be new. Transfer them
privately. Ordinary status commands expose endpoint metadata. Direct TLS uses a locally issued certificate with explicit
client trust. URI exports omit transports that cannot carry this trust. Reimport after certificate rotation.

## Operate

```sh
sudo singforge protocol configure --id ss --port 8892
sudo singforge protocol disable --id tuic
sudo singforge endpoint list --json
sudo singforge backup create --file /root/singforge.backup
singforge subscription show --help
singforge completion zsh
```

Disable requires a previously configured protocol. Use `protocol enable --id tuic` to add it. Commands support English
help and Chinese command summaries with `SINGFORGE_LANG=zh`. Running the CLI in an interactive terminal opens a vertical
keyboard menu. Use ↑/↓ or j/k, Enter, Esc, q and L for language. NO_COLOR disables highlights, and non-TTY output
retains plain help. A workspace can validate configurations on a development machine using an official sing-box binary.

```sh
make build
bin/singforge --workspace "$PWD/lab" --core /absolute/path/to/sing-box install --host 127.0.0.1 --listen 127.0.0.1
```

[Cloudflare and subscriptions](docs/operations.md) · [DNS and routing](docs/privacy-routing.md) · [Threat model](docs/security.md) · [Upstream research and licensing](docs/research.md)
