# Operating a host

## Layout and installation

Use Debian 12+ or Ubuntu 22.04+ with systemd as PID 1. Run host commands as root or with sudo. The data plane runs under
dynamic service identities. Install the distro Nginx package if enabling CDN or public subscriptions. A separate Nginx
master uses SingForge's configuration and runtime directory. Its listening ports must be free. Existing sites, default
Nginx unit, firewall and provider security groups remain operator-managed.

The CLI holds a nonblocking lock under `/var/lib/singforge`. A candidate generation contains state, native configs and
unit files. The private `current` symlink selects the active generation. Candidate activation uses real binary checks,
systemd and PID/socket inspection. An interrupted activation leaves a journal; the next operation restores the preceding
generation before continuing. Repeating install on initialized state reapplies the saved configuration and boot links.

Preset `minimal` enables SS. `recommended` adds Vision and Hysteria2. `full` enables the direct catalog. Use
`--preset custom --protocols ss,tuic,anytls` to specify direct protocols. For an IPv4-only kernel use
`--listen 0.0.0.0`; the default `::` uses the host's IPv6 dual-stack behavior. The public endpoint host and local listen
address serve different purposes. NAT commonly requires `--host`.

## Changing endpoints

```sh
sudo singforge endpoint add --id ss --mode direct --host 203.0.113.25
sudo singforge endpoint add --id ss --mode direct --host 2001:db8::25 --name ipv6-ss
sudo singforge protocol configure --id ss --port 8892
sudo singforge protocol disable --id ss
sudo singforge protocol enable --id ss
```

An extra named endpoint references the same inbound. Removing an endpoint removes its export. Disable the protocol to
stop listening. Select a trusted public TLS certificate for direct TLS endpoints with
`certificate import --host HOST --cert FULLCHAIN --key KEY`. Renewing the locally issued certificate with
`certificate renew --yes` changes client pins and requires distribution of new client configs.

## Cloudflare

The sample names below illustrate a zone. Substitute your own. A scoped token needs Zone Read and DNS Edit for the zone;
add account Tunnel permissions only for tunnel management. Provide a protected token file. The saved token is root-only.

```sh
sudo singforge cloudflare configure --domain 136028.xyz --token-file /root/cf.token
sudo singforge domain configure --domain 136028.xyz --hostname fast --mode dns-only
sudo singforge cloudflare enable --mode cdn --host cdn.136028.xyz \
  --cert /root/cdn.fullchain.pem --key /root/cdn.key --port 2053
sudo singforge cloudflare tunnel create --name singforge-edge
sudo singforge cloudflare enable --mode tunnel --host edge.136028.xyz
sudo singforge cloudflare dns sync --address 203.0.113.10 --dry-run
sudo singforge cloudflare dns sync --address 203.0.113.10
```

For an existing **locally managed** named tunnel, pass `--credentials /root/TUNNEL-ID.json` to the enable command. One
tunnel carries anchored VLESS and Trojan WebSocket paths to separate loopback ports, followed by a 404 catch-all. Native
ingress requires no Nginx or public HTTP listener. For a dashboard-managed tunnel, use
`cloudflare enable --mode tunnel --host HOST --connector-token-file PRIVATE_FILE`. The connector reads its credential
through systemd. Configure the dashboard’s Published application routes to match `cloudflare tunnel routes`, including
the anchored path expressions. Repeat this reconciliation after route changes. `cloudflare tunnel status` checks the
local connector readiness endpoint without requiring an API token. Temporary Quick Tunnels are outside the managed
deployment flow.

CDN requires strict origin TLS, matching DNS names, WebSockets enabled and a compatible edge HTTPS port. Select **Full (
strict)** in the Cloudflare zone. SingForge checks the origin key pair, name and expiry; the provider validates its
trust chain. The direct hostname uses DNS-only A/AAAA. CDN uses proxied A/AAAA. Tunnel uses a proxied CNAME to
`UUID.cfargotunnel.com`. The DNS provider alone does not accelerate direct traffic. Configure edge HTTPS redirects and
disable third-party request logging/caching wherever appropriate.

Pass public IPv4 and IPv6 comma-separated to sync both families. The API adapter checks the entire desired set before
mutation. An external record with different values requires `--replace-id` with its exact ID. Replacing an external
record preserves external ownership. Multi-record API changes may partially succeed during an outage; rerun sync. The
local ledger records successful creates. Review an interrupted remote create in the dashboard if it succeeded before
ownership could be written.

A CDN-only or tunnel-only install accepts `install --mode cdn --cdn HOST --cert CERT --key KEY` or
`install --mode tunnel --tunnel HOST --credentials FILE`. A fresh combined install accepts
`install --mode hybrid --host IP --cdn CDN_HOST --cert CERT --key KEY --tunnel TUNNEL_HOST --credentials FILE`. DNS
synchronization remains explicit.

## Subscription publication

Use a separate hostname with a certificate trusted by clients. It needs TCP 443 free on the host. Move a default Reality
TCP listener first. A Cloudflare DNS-only record for this host is included in DNS sync.

```sh
sudo singforge protocol configure --id reality --port 18443
sudo singforge subscription publish --host sub.136028.xyz --cert /root/sub.fullchain.pem --key /root/sub.key
sudo singforge subscription rotate --format mihomo --ttl 720h --grace 15m
sudo singforge subscription list
sudo singforge subscription revoke --yes
```

The issued HTTPS URL contains a bearer secret. Copy it when issued. The saved grant contains SHA-256 of the token;
recovering a lost URL requires rotation. For a private PKI, pass `--ca` only when the clients already trust that CA.
Publishing requires a matching valid certificate. Use private local file or QR exports for IP-only deployments without
trusted HTTPS.

## Updates, recovery and removal

```sh
sudo singforge backup create --file /root/singforge-before.json
sudo singforge core update
sudo singforge core rollback --yes
sudo singforge self update --file ./singforge-linux-amd64 --sha256 TRUSTED_DIGEST --yes
sudo singforge backup restore --file /root/singforge-before.json --yes
```

Core and connector updates accept independently authenticated local binaries. Without a file the live updater consumes
upstream stable GitHub assets with SHA-256 metadata. Combined `update --all --yes` also requires a published SingForge
release. This checkout ships build tooling. A GitHub release has to exist before automatic CLI retrieval can work. Self
replacement targets `/usr/local/bin/singforge`. Keep the bootstrap binary and backup available out of band. Imported
provider credentials are included in state backups; the standalone Cloudflare API token, system binaries, DNS ownership
ledger and sysctl journal are host-local. A fresh restore of a custom-core deployment also needs its digest-addressed
executable. Restore clears subscription grants to prevent an older backup from reviving revoked bearer URLs. Issue fresh
URLs after restore. Backups are private plaintext with corruption detection; encrypt before storing off-host.

## Uninstall

Preview with `sudo singforge uninstall --full --dry-run`. The plan lists managed units, files, directories to remove when empty and recorded firewall or kernel settings to restore. Choose a removal mode:

| Command | Local effect |
|---|---|
| `uninstall --yes` | Detach services and boot links, remove the owned renewal hook and restore recorded tuning and ingress. Retain deployment credentials and executables. |
| `uninstall --purge --yes` | Also discard current deployment state and local API credentials. Keep the CLI and cached executables for restore or reinstall. |
| `uninstall --full --yes` | Also remove historical generations, DNS ownership metadata, cached and rollback binaries, worker copies and the installed CLI. Works after a previous purge. |

Run the installed CLI at `/usr/local/bin/singforge` for live self-removal. Workspace mode removes only workspace resources. Unknown files are included in the `preserved` result. Back up outside the deployment directory before removing credentials. File deletion does not erase snapshots or SSD backups.

Local removal works offline. To retire owned Cloudflare DNS records, use `cloudflare dns cleanup --dry-run` and `cloudflare dns cleanup --yes` before deleting the local ownership ledger. Retire provider-side tunnels separately. Imported certificate lineages, external credentials, system packages and shared system services remain under operator control.

## Troubleshooting

- Check `doctor`, `status`, `logs`, `ss -lntup` and cloud security groups. An open TCP socket confirms reachability
  only. Verify credentials and transport with a real client request.
- A port conflict during activation restores the previous generation. Use `protocol configure` to move the listener. UDP
  connectivity requires an actual QUIC request.
- For Tunnel failures, inspect `cloudflare tunnel status`, `journalctl -u singforge-tunnel` and the connector readiness
  endpoint at `127.0.0.1:19782/ready`. Verify egress to Cloudflare and the tunnel CNAME.
- An occupied TCP 443 can belong to an unrelated Nginx site. Choose another supported CDN HTTPS port. Subscription
  publication needs a free 443 in this version.
- `bbr status` inspects available algorithms. Preview `bbr enable --profile streaming --dry-run` before applying with
  `--yes`. The default profile changes TCP congestion control. FQ and bounded memory-aware buffer increases are opt-in.
  Existing sysctl ownership conflicts stop application. `bbr disable --yes` restores the recorded values. QUIC has its
  own congestion control.

## Optional IPv4 firewall integration

On hosts already using iptables-persistent, preview `sudo singforge firewall sync --dry-run` then apply
`sudo singforge firewall sync --yes`. This adds only the required IPv4 TCP/UDP allowances tagged `singforge-ingress`,
before a blanket INPUT rejection, and updates the existing persistent filter table. Re-run after changing protocols,
ports or publication. Uninstall removes the ledgered allowances. IPv6 firewall policy, nftables-native hosts and
provider security groups need separate administration.

## Certbot renewal for public subscriptions

Use a distro Certbot with `reconfigure` support (2.3+), the `dns-cloudflare` plugin and `certbot.timer`. This integration expects the active subscription hostname to match the Certbot certificate name under `/etc/letsencrypt/live/`. On Ubuntu install the plugin with `sudo apt-get install python3-certbot-dns-cloudflare`. Create a Cloudflare API token with Zone / DNS / Edit limited to the certificate's zone. Store its value under `dns_cloudflare_api_token` in a root-owned INI file (mode 0600) in a trusted directory. The connector token is a separate credential. [Cloudflare DNS plugin](https://certbot-dns-cloudflare.readthedocs.io/en/stable/)

On a fresh host, request the first certificate with the plugin. For an existing manual certificate, skip initial issuance and enable automation on the published hostname.

```sh
sudo certbot certonly --dns-cloudflare \
  --dns-cloudflare-credentials /etc/letsencrypt/singforge-dns.ini \
  --dns-cloudflare-propagation-seconds 60 -d sub.example.org
sudo singforge subscription publish --host sub.example.org \
  --cert /etc/letsencrypt/live/sub.example.org/fullchain.pem \
  --key /etc/letsencrypt/live/sub.example.org/privkey.pem
sudo singforge certificate auto-renew enable --credentials /etc/letsencrypt/singforge-dns.ini --yes
sudo singforge certificate auto-renew status
sudo singforge certificate auto-renew test
```

On migration, Certbot tests the modified settings with the staging CA before saving them. The system timer schedules due renewals. SingForge installs a guarded deploy hook in Certbot’s deploy directory and verifies the leaf served on local TCP 443 using the publication hostname as SNI. Ordinary renewals retain node credentials and subscription grants. Status reports the DNS authenticator, file permissions, timer state, hook and served certificate match. To replay a failed certificate import, run `sudo singforge certificate auto-renew sync`. To exercise Certbot's actual deploy-hook dispatch with the live certificate, run `sudo certbot renew --cert-name sub.example.org --dry-run --run-deploy-hooks`. A staging rehearsal does not extend the live certificate’s expiry. [Certbot renewal guide](https://eff-certbot.readthedocs.io/en/stable/using.html#renewing-certificates)

Unpublishing, manually republishing or restoring a backup clears the SingForge auto-deploy binding. Enable it again after a restore. Uninstall removes the unmodified SingForge hook. The Certbot lineage, its credentials and the shared system timer remain under Certbot administration.

### Automating other Cloudflare dashboard operations

SingForge already uses Cloudflare API v4 for zone discovery, DNS reconciliation and creation of locally managed named tunnels. Configure a management token with Zone Read and DNS Edit for the zone. Add Cloudflare Tunnel Write for the intended account only if that token must manage tunnels. Use `cloudflare dns sync --address IP --dry-run` to inspect DNS changes before applying them. Creating a local tunnel, enabling it and synchronizing DNS automates both WS routes for a fresh deployment; see the commands under Cloudflare above.

For remotely managed tunnels, Cloudflare also supports REST or Terraform for creation, connector-token retrieval, ingress configuration and the proxied DNS CNAME. The configuration update is a full PUT: read the existing config, preserve other ingress entries and origin settings and keep the catch-all last. `singforge cloudflare tunnel routes` supplies the desired hostname/path/upstream entries. The current renewal-only token deliberately has no tunnel management authority. [Cloudflare remote tunnel API walkthrough](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/)
