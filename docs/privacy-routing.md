# DNS and routing

Both client renderers share endpoint, resolver and policy selection. The sing-box renderer emits typed DNS servers and route actions. Mihomo gets its native nameserver, resolver-policy, proxy groups and rule fields. Exporting a proxy provider serializes nodes only; DNS and groups belong in the containing Mihomo config.

```sh
sudo singforge dns set --profile strict --resolver quad9 --transport https --family ipv6 --capture tun
sudo singforge dns show
sudo singforge rules update
sudo singforge rules set --profile smart
sudo singforge rules override --kind DOMAIN-SUFFIX --value example.org --group AI
sudo singforge rules test --host sub.example.org
```

Supported public resolvers are Google, Cloudflare and Quad9. The rendered encrypted resolver uses a literal IP to bootstrap without an OS lookup. Choose IPv4 or IPv6 according to the **client's** available network. sing-box sets the TLS server name explicitly. Mihomo uses the resolver's IP HTTPS or TLS endpoint. On the server, domain destinations resolve through typed Cloudflare DoH. Set `--server-family ipv6` on an IPv6-only host with a DNS hostname. Client resolver family is configured separately. The main remote resolver is explicitly routed through the Main proxy selector. Resolving an upstream proxy hostname has a separate encrypted direct bootstrap path; this reveals the proxy hostname and the client's IP to that resolver. An IP endpoint avoids this lookup.

Strict config has no ISP fallback. The full strict Mihomo profile disables system-hosts overrides so stale OS entries cannot redirect proxy hostnames. Balanced mode may use system hosts. Balanced mode sends only local, lan and home.arpa suffixes to the system resolver, which trades local DNS privacy for local naming. In Mihomo, Fake-IP exclusions are limited to these suffixes. In TUN capture mode sing-box uses a typed Fake-IP server for intercepted A/AAAA, local-domain rules first, and 53 interception. Both client TUN configurations need host permissions. The default system-proxy export listens on loopback. A browser or program ignoring the proxy may still use system DNS; applications with independent DoH can bypass the client's DNS rules, particularly in system-proxy mode. IPv6 outside a captured default route is another bypass.

To investigate leaks, use a domain you control and a fresh random label. Observe authoritative queries and capture physical-interface traffic while testing with and without the proxy. Watch UDP/TCP 53, 853, resolver HTTPS connections and both IP families. Compare browser DoH on/off, test proxy loss and check that sensitive names fail closed. `dns test --host NAME --port 1053` queries a running local client DNS listener on the machine running the command. Its answer alone cannot identify every application or upstream path.

## Rule semantics

The built-in smart profile works offline. Users can select AI, Streaming and Communication separately in the full client. Overrides precede service rules. Service rules precede broad geographic rules. The smart fallback is Main. Global defaults to Main. GFW-only requires a downloaded GFW list, sends its matches through Main and otherwise uses DIRECT. Custom uses the operator's ordered overrides and a Main fallback. DNS remains encrypted through Main in strict mode even when a traffic policy selects DIRECT.

`rules update` downloads the sources selected by a built-in manifest of immutable commits and SHA-256 digests. Available source adapters consume domain/CIDR payloads and selected classical Clash rules. The classical importer omits process names and ASNs explicitly; these depend on platform process metadata or autonomous-system databases. The implementation translates supported matches independently into Mihomo rule strings and sing-box match fields. It never interprets MRS as SRS. Native caches live in the private state, so export can operate offline. `rules rollback --yes` swaps the saved revisions. Use `rules schedule --yes` for the weekly systemd timer and `rules unschedule --yes` to detach it. A newer reviewed manifest arrives with a CLI update.

Maintained sources and revisions appear in [the upstream review](research.md). Client-side policy sets derived from external rule data retain that data's licensing obligations.
