# SingForge

Deploy and manage a single Linux sing-box server from a numbered terminal dashboard or the Cobra CLI. Direct mode provides Reality, Hysteria2 and Shadowsocks. Add a Cloudflare Named Tunnel for WebSocket fallback and HTTPS publication for client subscriptions.

[中文](README.zh-CN.md) · [Releases](https://github.com/howardshaw/singforge/releases) · [CLI](docs/cli.md) · [Operations](docs/operations.md)

## Install with an agent

```text
Read AGENTS.md. Install the latest stable SingForge release on <SSH host> using <private .env path>, including its DNS, Tunnel and HTTPS subscription settings. Preserve existing services and grants. Verify the deployment and privately return working subscription URLs for Mihomo/Clash, Shadowsocks and v2rayN.
```

## Install a release on the VPS

Use Debian 12+ or Ubuntu 22.04+, systemd, sudo/root and Linux amd64 or arm64. The VPS downloads the compiled CLI from GitHub Actions releases. It also needs outbound HTTPS to obtain the verified upstream core.

```sh
VERSION=v0.1.1
BASE="https://github.com/howardshaw/singforge/releases/download/$VERSION"
SETUP=$(mktemp -d)
cd "$SETUP"
for file in install.sh singforge.env.example SHA256SUMS; do
  curl --proto '=https' --proto-redir '=https' -fSL "$BASE/$file" -o "$file" || exit 1
done
sha256sum -c --ignore-missing SHA256SUMS && sudo sh install.sh --release "$VERSION"
sudo install -d -m 700 /root/singforge
sudo install -m 600 singforge.env.example /root/singforge/.env  # fresh host only
sudoedit /root/singforge/.env
```

The installer selects the architecture and checks the binary against the SHA-256 manifest from the same GitHub release. For an existing installation use `sudo singforge self update --yes`.

## Configure the environment file

For **direct IP only**, fill in `PUBLIC_IP` with the VPS public address (use the public mapping on NAT hosts). Leave `DOMAIN` empty and the two feature switches false. Continue to **Apply and connect**.

For **direct + Tunnel + public subscriptions**, use an active Cloudflare zone. For a newly registered domain, set the registrar's nameservers to the pair assigned by Cloudflare and wait for Active. Pick a different `PREFIX` for every server. [Zone setup](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/)

1. In Cloudflare **My Profile → API Tokens → Create Token**, create a management token with **Zone Read** and **DNS Edit** for this zone. When enabling Tunnel, add **Cloudflare Tunnel Write** (or **Cloudflare One Connector: cloudflared Edit**) on the owning account.
2. For certificate renewal, create a separate **DNS Edit** token for the same zone. Install the certificate and web-server dependencies on the VPS and prepare the files below. [Token setup](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/) · [Certbot DNS plugin](https://certbot-dns-cloudflare.readthedocs.io/en/stable/)

```sh
sudo apt-get update
sudo apt-get install -y nginx certbot python3-certbot-dns-cloudflare
sudo touch /root/singforge/cloudflare.token /root/singforge/certbot.ini
sudo chmod 600 /root/singforge/cloudflare.token /root/singforge/certbot.ini
sudoedit /root/singforge/cloudflare.token   # one line: raw management token
sudoedit /root/singforge/certbot.ini        # dns_cloudflare_api_token = RENEWAL_TOKEN
```

Edit the corresponding lines in the private `.env`. Credential fields point to root-owned private files. The parser reads literal values, rejects unknown keys and never evaluates shell commands.

```dotenv
PUBLIC_IP=203.0.113.10
DOMAIN=example.org
PREFIX=vps2
CF_API_TOKEN_FILE=/root/singforge/cloudflare.token
ENABLE_TUNNEL=true
ENABLE_SUBSCRIPTION=true
CERTBOT_CREDENTIALS_FILE=/root/singforge/certbot.ini
ACME_EMAIL=admin@example.org
ACME_AGREE_TOS=true
```

Replace the example IP, zone and email. Set `ACME_AGREE_TOS=true` after accepting the [Let's Encrypt subscriber agreement](https://letsencrypt.org/repository/). This example creates the following endpoints:

| Hostname | DNS | Purpose |
|---|---|---|
| `vps2-fast.example.org` | DNS-only A or AAAA | Direct protocols |
| `vps2-edge.example.org` | Proxied CNAME to a dedicated Tunnel | VLESS-WS and Trojan-WS |
| `vps2-sub.example.org` | DNS-only A or AAAA | HTTPS subscription |

## Apply and connect

Allow inbound TCP 8881 (Reality), UDP 8882 (Hysteria2), TCP/UDP 8892 (Shadowsocks) and TCP 443 for publication in the host firewall and cloud security group. Keep the SSH port allowed. The env template lets you change the three direct ports. Tunnel uses outbound TCP/UDP 7844. [Cloudflare firewall requirements](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/)

```sh
sudo singforge provision --env-file /root/singforge/.env --dry-run
sudo singforge provision --env-file /root/singforge/.env
sudo singforge doctor
sudo singforge
```

The preview shows local intent. Apply checks DNS conflicts, registers the connector, obtains a DNS-01 certificate and configures renewal for the enabled features. Rerun the same file after an interruption. Existing credentials and subscription grants are retained.

For a private file export use `sudo singforge subscription show --format mihomo --output client.yaml`. For a published URL use `sudo singforge subscription issue --format mihomo` and save the URL privately when displayed. Import it in Clash Verge and test individual nodes under **Main**. The dashboard supports arrows or a number followed by Enter, and 0 to select Exit on the main menu.

In Smart routing, the AI-CN group defaults to DIRECT on fresh imports. Clients with a saved selection can choose DIRECT under AI-CN. International AI stays proxied and strict DNS keeps its encrypted proxy path.

## Development and releases

Run `make check` locally. Choose **Actions → Publish tagged release → Run workflow** on main and enter the next version, or push a tag. Pushing a semantic version tag such as `v0.1.1` starts the release workflow. GitHub verifies the code, tests a systemd lifecycle on Ubuntu, builds both Linux architectures and publishes binaries, installer, env template, SHA256SUMS and license notices. [Workflow](.github/workflows/release.yml) · [Validation](docs/validation.md) · [Security](docs/security.md)
