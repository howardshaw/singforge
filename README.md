# SingForge

SingForge deploys sing-box on a single Linux host. Its Go CLI uses Cobra. A direct installation runs one systemd
service. CDN access adds a dedicated Nginx process, named tunnels use cloudflared, and public subscriptions add a
loopback-only publisher behind HTTPS.

[中文说明](README.zh-CN.md) · [CLI reference](docs/cli.md) · [Operator guide](docs/operations.md) · [Protocol compatibility](docs/protocols.md) · [Architecture](docs/architecture.md) · [Validation](docs/validation.md)

## Before you install

Prepare a Debian 12+ or Ubuntu 22.04+ VPS with systemd, SSH and sudo/root access. Check its public IP and CPU architecture (`uname -m`). Select the prerequisites for the access modes you intend to use.

| Access mode | What to prepare |
|---|---|
| Direct IP and private client export | A public IP and open TCP/UDP ports for the selected protocols. No domain or Cloudflare account is required. |
| Direct hostname | Point a DNS-only A/AAAA record such as `fast.example.org` at the VPS. Manual DNS setup requires no API token. |
| Cloudflare CDN | An active Cloudflare zone, proxied hostname, Nginx, reachable origin HTTPS port and matching origin certificate/key for Full (strict). |
| Named Tunnel | An active zone, a proxied CNAME to `<UUID>.cfargotunnel.com` and outbound connectivity. Use a credentials JSON for a locally managed tunnel or a connector token for a dashboard-managed tunnel. |
| Public subscriptions | A separate hostname, trusted certificate/key, Nginx and public TCP 443. Plan for a port conflict if Reality already occupies TCP 443 on the same address. |

An existing domain works. For a low-cost option, search Spaceship for a 6–9 digit numeric `.xyz` in the registry’s 1.111B Class. Compare registration, renewal and applicable fees for the exact name at checkout. [Spaceship pricing](https://www.spaceship.com/domains/gtld/xyz/) · [XYZ registry](https://gen.xyz/1111b)

For Cloudflare DNS hosting, add the root domain to your account, verify imported records including mail records, set the assigned nameservers at your registrar and wait for **Active** before creating endpoint records. For SingForge DNS automation, scope Zone Read and DNS Edit to the selected zone. Add account Tunnel Write only for API tunnel management. For Certbot DNS-01 use a separate zone-scoped DNS Edit token in a root-private INI file (0600). [Cloudflare zone setup](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/) · [Operator commands](docs/operations.md#cloudflare)

### Using your own endpoint names

Copy [hybrid.yaml](configs/hybrid.yaml) and replace the endpoint hostnames, certificate paths and tunnel credentials path. If direct international DNS fails during node setup, `sudo singforge dns set --mihomo-node-dns alidns` enables AliDNS for node bootstrap. Full Mihomo exports automatically populate `proxy-server-nameserver-policy` with the exact hostnames of their exported nodes. Names are deduplicated, IP literals are skipped and endpoints may belong to different zones. A zone wildcard would also match unrelated node names under the same suffix. Application DNS uses its own policy.

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

### Cloudflare Preferred Edge

On the VPS, run `sudo singforge edge set --mode ips` to add two candidate Cloudflare IPs, or specify up to eight addresses in priority order with `--ips 188.114.96.1,104.16.132.229`. Refresh the existing full Mihomo subscription and choose **Main → Preferred Edge**. The selector checks for an HTTP 204 over each authenticated proxy every 120 seconds. It tries healthy candidates first and retains your original CDN or Tunnel WebSocket endpoints at the end of the list. The dial address changes while TLS SNI, certificate verification, WS Host, path and credentials follow your endpoint. IPv6 candidates require client IPv6. Performance depends on the client network.

Use `edge set --mode domains` for the eight community domains, or pass an ordered subset with `--domains`. View the lists with `edge status` and restore hostname-only access with `edge set --mode default`. The server menu exposes these choices under **Subscriptions & QR codes → Preferred Edge selector**. Other export formats continue to use the original nodes. [Mihomo fallback](https://wiki.metacubex.one/en/config/proxy-groups/fallback/) · [Cloudflare IP ranges](https://www.cloudflare.com/ips/)

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
