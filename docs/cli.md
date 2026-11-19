# CLI reference

Generated from Cobra. Append `--help` to any command for flag types and defaults. Global options are `--workspace`, `--core`, `--json`, `--dry-run` and `--yes`. Shell completions are available through `singforge completion`.

| Command | Purpose | Local options |
|---|---|---|
| `singforge doctor` | Check the active server configuration and service health |  |
| `singforge logs` | Read recent managed service journal entries |  |
| `singforge reload` | Reload or restart managed services |  |
| `singforge restart` | Restart managed services |  |
| `singforge start` | Start managed services |  |
| `singforge status` | Show deployment status without credentials |  |
| `singforge stop` | Stop managed services |  |
| `singforge certificate auto-renew enable` | Configure Cloudflare DNS-01 and deploy renewed subscription certificates | `--credentials` |
| `singforge certificate auto-renew status` | Inspect the Certbot timer, authenticator, hook and served leaf |  |
| `singforge certificate auto-renew sync` | Import the current Certbot certificate and verify HTTPS |  |
| `singforge certificate auto-renew test` | Exercise a staging renewal and verify the live HTTPS origin |  |
| `singforge certificate import` | Import a publicly trusted direct TLS certificate and key | `--cert`, `--host`, `--key` |
| `singforge certificate renew` | Generate a new direct certificate and new client pins |  |
| `singforge cloudflare configure` | Discover an existing zone using a scoped token | `--domain`, `--token-file` |
| `singforge cloudflare disable` | Remove an access mode | `--mode` |
| `singforge cloudflare dns cleanup` | Delete records created in the selected zone |  |
| `singforge cloudflare dns list` | List records in the selected zone |  |
| `singforge cloudflare dns sync` | Check conflicts and synchronize managed DNS | `--address`, `--replace-id` |
| `singforge cloudflare enable` | Configure HTTP CDN or named tunnel access | `--cert`, `--connector-token-file`, `--credentials`, `--host`, `--key`, `--mode`, `--port` |
| `singforge cloudflare status` | Show configured edge hosts |  |
| `singforge cloudflare tunnel create` | Provision a locally managed named tunnel | `--name` |
| `singforge cloudflare tunnel list` | Discover tunnels in the configured account |  |
| `singforge cloudflare tunnel routes` | Show the required public hostname, path patterns and origins |  |
| `singforge cloudflare tunnel status` | Inspect tunnel connection status |  |
| `singforge dns set` | Change client DNS profile or encrypted resolver | `--capture`, `--family`, `--mihomo-ipv`, `--mihomo-node-dns`, `--mihomo-policy`, `--profile`, `--resolver`, `--server-family`, `--transport` |
| `singforge dns show` | Inspect client DNS settings |  |
| `singforge dns test` | Query a running local client DNS listener | `--host`, `--port` |
| `singforge domain configure` | Set a DNS-only direct hostname | `--domain`, `--hostname`, `--mode` |
| `singforge domain status` | Inspect configured hostnames |  |
| `singforge domain sync` | Reconcile configured DNS records | `--address`, `--replace-id` |
| `singforge endpoint add` | Add or update a public endpoint | `--host`, `--id`, `--mode`, `--name`, `--port` |
| `singforge endpoint list` | List public endpoints |  |
| `singforge endpoint remove` | Remove a public endpoint | `--id` |
| `singforge endpoint test` | Test TCP reachability | `--id` |
| `singforge firewall sync` | Reconcile optional IPv4 iptables-persistent ingress |  |
| `singforge protocol configure` | Change the listening port | `--id`, `--port` |
| `singforge protocol disable` | Disable an inbound without deleting credentials | `--id` |
| `singforge protocol enable` | Enable a protocol and its endpoints | `--id`, `--port` |
| `singforge protocol list` | List protocol capabilities |  |
| `singforge reality configure` | Set the Reality server name and TLS handshake destination | `--handshake`, `--handshake-target` |
| `singforge rules list` | List built-in routing rules |  |
| `singforge rules override` | Prepend a domain or CIDR routing override | `--group`, `--kind`, `--value` |
| `singforge rules rollback` | Swap cached rules with the previous revision |  |
| `singforge rules schedule` | Enable the weekly systemd rule refresh timer |  |
| `singforge rules set` | Select a routing preset | `--profile` |
| `singforge rules test` | Explain routing for a hostname | `--host` |
| `singforge rules unschedule` | Disable the rule refresh timer |  |
| `singforge rules update` | Download and verify pinned routing data |  |
| `singforge rules validate` | Validate cached rules and overrides |  |
| `singforge subscription issue` | Issue another private HTTPS URL, preserving previous grants | `--format`, `--ttl` |
| `singforge subscription list` | Inspect publication and grant counts |  |
| `singforge subscription nodes` | List enabled endpoints with a compatible import URI |  |
| `singforge subscription publish` | Publish subscriptions over HTTPS | `--ca`, `--cert`, `--host`, `--key` |
| `singforge subscription revoke` | Invalidate all subscription URLs |  |
| `singforge subscription rotate` | Issue a bearer URL, invalidating previous grants after grace | `--format`, `--grace`, `--ttl` |
| `singforge subscription show` | Reveal client credentials or export a private file | `--filter`, `--format`, `--output`, `--qr`, `--qr` |
| `singforge subscription unpublish` | Stop publication and revoke grants |  |
| `singforge backup create` | Save an integrity-checked private backup | `--file` |
| `singforge backup restore` | Validate and activate a backup | `--file` |
| `singforge bbr disable` | Restore recorded kernel values |  |
| `singforge bbr enable` | Plan or apply reversible TCP tuning | `--profile` |
| `singforge bbr status` | Inspect kernel congestion control and queue settings |  |
| `singforge cloudflared rollback` | Restore the previous connector |  |
| `singforge cloudflared update` | Stage a verified connector and test tunnel readiness | `--file`, `--sha` |
| `singforge core rollback` | Reapply the previous core with current credentials |  |
| `singforge core update` | Stage a digest-verified core, validate and activate | `--file`, `--sha` |
| `singforge install` | Deploy a validated generation and start managed services | `--cdn`, `--cert`, `--config`, `--connector-token-file`, `--credentials`, `--handshake`, `--handshake-target`, `--host`, `--key`, `--listen`, `--mode`, `--port`, `--preset`, `--protocols`, `--server-family`, `--tunnel` |
| `singforge self rollback` | Restore the preceding CLI executable |  |
| `singforge self update` | Replace the CLI from a digest-verified executable | `--file`, `--sha` |
| `singforge uninstall` | Detach managed services and optionally purge state | `--purge` |
| `singforge update` | Update all enabled components from stable upstream releases | `--all` |
| `singforge version` | Print compatible core version |  |
