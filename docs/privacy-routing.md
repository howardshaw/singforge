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

Strict config has no ISP fallback. The full strict Mihomo profile disables system-hosts overrides so stale OS entries cannot redirect proxy hostnames. Balanced mode may use system hosts. Balanced mode sends only local, lan and home.arpa suffixes to the system resolver, which trades local DNS privacy for local naming. Mihomo additionally returns real IPs for the explicitly listed connectivity-check hosts. Fake-IP filtering affects the answer presented to the application; DNS upstream selection is a separate policy. In TUN capture mode sing-box uses a typed Fake-IP server for intercepted A/AAAA, local-domain rules first, and 53 interception. Both client TUN configurations need host permissions. Full Mihomo exports enable TUN by default unless `--capture system` was explicitly selected. Native sing-box enables TUN only with `--capture tun`. The mixed proxy listener stays on loopback. A browser or program ignoring the proxy may still use system DNS; applications with independent DoH can bypass the client's DNS rules, particularly in system-proxy mode. IPv6 outside a captured default route is another bypass.

To investigate leaks, use a domain you control and a fresh random label. Observe authoritative queries and capture physical-interface traffic while testing with and without the proxy. Watch UDP/TCP 53, 853, resolver HTTPS connections and both IP families. Compare browser DoH on/off, test proxy loss and check that sensitive names fail closed. `dns test --host NAME --port 1053` queries a running local client DNS listener on the machine running the command. Its answer alone cannot identify every application or upstream path.

## Rule semantics

The built-in smart profile works offline. Users can select AI, Streaming and Communication separately in the full client. Overrides precede service rules. Service rules precede broad geographic rules. The smart fallback is Main. Global defaults to Main. GFW-only requires a downloaded GFW list, sends its matches through Main and otherwise uses DIRECT. Custom uses the operator's ordered overrides and a Main fallback. For Mihomo, explicitly DIRECT traffic can resolve via the direct resolver list. Direct resolution follows nameserver-policy, including explicit AI protections.

Mihomo uses its native `GEOSITE,CN` and `GEOIP,CN,no-resolve` rules after explicit service rules. They replace the generated lists of more than 120,000 domain and CIDR entries. Mihomo refreshes its GeoSite and GeoIP data from the documented MetaCubeX sources every 24 hours. Sing-box downloads cached CN SRS rule sets from the format-specific MetaCubeX `sing` branch on the same interval. The subscription carries the rule-set URLs and small configurations rather than geographic payloads. Neither database membership grants permission to send a domain to AliDNS/DNSPod. AI and service rules always match before the geographic rules. Geographic downloads are HTTPS authenticated third-party data. Availability and data correctness remain dependent on that upstream.

Maintained sources and revisions appear in [the upstream review](research.md). Client-side policy sets derived from external rule data retain that data's licensing obligations.

## Mihomo DNS and failover

```sh
sudo singforge dns set --mihomo-policy split --mihomo-ipv6 off
```

Full Mihomo subscriptions use separate lists for encrypted bootstrap, proxy hostnames and application queries. `default-nameserver` uses authenticated IP endpoints operated by Cloudflare, Google and Quad9. The configured resolver is listed first; Mihomo can query multiple upstreams concurrently. Proxy-server lookups use these endpoints through DIRECT to avoid a DNS/proxy dependency loop. Application queries use the Main group, and known AI domain policies explicitly use the AI group. No DNS fallback list includes domestic or ISP resolvers.

`--mihomo-policy strict` leaves domestic public DNS unused. With `split` plus smart routing, only explicit domestic service hosts and the AI-CN provider suffixes are eligible for AliDNS/DNSPod encrypted resolution over DIRECT. International AI service rules match first. A broad `.cn`, `aliyun.com` or `geosite:cn` DNS policy could send AI queries to domestic resolvers. Unknown domains follow the foreign default, and a general DIRECT traffic rule never automatically permits domestic DNS. For DNS performed specifically by the DIRECT outbound the foreign direct resolvers are used unless an explicit policy matches. The VPS also uses encrypted foreign DNS for destinations resolved server-side.

`Auto` is an ordered fallback group. Main selects Auto first on a fresh import. Priority is direct Reality Vision, direct Hysteria2, tunnel VLESS-WS, tunnel Trojan-WS, then remaining available nodes. Client-saved selections and Clash Verge merge profiles may override these settings. The connectivity-check URL measures node reachability; an AI service's regional or account availability is independent. Smart routing sends Apple TV to Streaming and common connectivity, push, time and update endpoints direct. Other Apple hosts follow normal precedence and the final policy.

Both Mihomo IPv6 switches default off for an ordinary IPv4 deployment. IPv6 resolver selection or an exclusively literal IPv6 node set selects IPv6 automatically unless overridden. Use `--mihomo-ipv6 on` to allow IPv6 with IPv4 resolvers on a tested dual-stack client. DNS family controls resolver reachability independently. Disabling AAAA in a proxy config cannot disable the OS IPv6 stack. TUN requires a capable client with service privileges; inspect the effective config after subscription refresh. Browser-specific DoH, excluded apps/interfaces, OS encrypted DNS and traffic before the client starts remain separate boundaries. Provider-only and URI exports cannot carry these DNS/TUN guarantees.

To reproduce DNS path checks, run `MIHOMO_BIN=/path/to/mihomo go test -count=1 -run TestMihomoLiveDNSPaths ./internal/render`. The test substitutes local TLS DoH recorders and a CONNECT proxy, queries fresh names, interrupts the proxy and checks which recorder received each question. Set `MIHOMO_TEST_TUN=1` only inside a disposable Linux network namespace with NET_ADMIN and /dev/net/tun. In that mode it sends UDP/TCP DNS packets to a test-network destination on port 53 to exercise actual TUN interception.

### Smart rule precedence and geographic data

User overrides are first. The smart preset then matches private addresses (including CGNAT 100.64.0.0/10), connectivity checks, basic ad blocking, international AI, domestic AI, streaming, communications and developer services. Domestic exact hosts and the .cn suffix precede the final geographic datasets. The terminal policy is Main. The Gemini API and Apple captive portal use exact matches; service families such as YouTube CDN and Discord use suffix matches. AI-CN currently covers DeepSeek, Moonshot/Kimi, Qwen/Tongyi, ChatGLM/BigModel and Doubao domains. AI-CN is a separate selectable proxy group. Split mode sends its listed domains directly and resolves them with AliDNS/DNSPod over DoH/DoT. Strict mode still sends this group through the proxy and foreign encrypted resolvers.

`rules update` continues to refresh the smaller service lists used for AI, communication and streaming routing. It no longer downloads CN GeoSite or CIDR lists into SingForge state. Mihomo loads those datasets in its native format. Sing-box loads the upstream SRS files and caches them in its normal client cache. Geographic IP matches use `no-resolve`, so they don't trigger a DNS lookup to obtain an IP solely for country classification. Geographic datasets only choose traffic routing. They never authorize domestic DNS.

### Node hostname bootstrap

If direct connections to the default encrypted DNS providers time out before a node can connect, use `singforge dns set --mihomo-node-dns alidns`. Full Mihomo exports then send only their exact proxy server hostnames to AliDNS over encrypted DIRECT connections. AliDNS sees the client address and node hostname. Application DNS and traffic rules retain their own policies. An IP-only export needs no override. Use `--mihomo-node-dns default` to return to the normal international bootstrap resolvers.
