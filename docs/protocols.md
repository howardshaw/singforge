# Protocol compatibility

The installer pins official sing-box 1.14.4. Core updates accept stable 1.14.x candidates after version, configuration and readiness checks. The native client target is the same series. Mihomo validation uses v1.19.34.

| Protocol ID | Public access | Native client | Mihomo / provider | URI / V2Ray subscription |
|---|---|---|---|---|
| reality | Direct, TCP Vision | Yes | Yes | Yes |
| reality-grpc | Direct, gRPC Reality | Yes | Yes | Yes |
| hysteria2 | Direct, UDP TLS | Yes | Yes | Publicly trusted certificate |
| tuic | Direct, UDP TLS, v5 | Yes | Yes | Publicly trusted certificate |
| ss | Direct, TCP/UDP AES-256-GCM | Yes | Yes | SIP002 |
| ss2022 | Direct, TCP/UDP BLAKE3 AES-256-GCM | Yes | Yes | 2022-aware clients |
| trojan | Direct, TLS | Yes | Yes | Publicly trusted certificate |
| anytls | Direct, TLS | Yes | Yes | Publicly trusted certificate |
| snell | Direct, native v6 | Yes | Omitted | Omitted |
| shadowtls | Direct, v3 with an inner Shadowsocks connection | Yes | Omitted | Omitted |
| naive | Direct, HTTP/2 TLS | Public trust and Naive-capable build | Omitted | Omitted |
| vless-ws, trojan-ws, vmess-ws | CDN or named tunnel, edge HTTPS | Yes | Yes | Yes |

The Shadowsocks export includes only Shadowsocks nodes. Use `--filter direct-ss` for older AEAD-only clients. `--filter` also accepts a mode or a specific endpoint ID. Native JSON and Mihomo carry trust material for the default direct certificate. A public URI omits a node if the client would require a trust bypass. The CLI reports omissions on stderr. The HTTP publisher returns their count in `X-SingForge-Omitted`.

Reality requires a reachable TLS 1.3 handshake target suited to the deployment. Select it at install using `--handshake`. Naive outbound has OS/build-specific Chromium dependencies; official Linux pure-Go clients need the matching libcronet.so. Review the current [Naive specification](https://sing-box.sagernet.org/configuration/outbound/naive/). Schema acceptance and a successful client import do not prove end-to-end traffic. The exercised pairs are listed in [validation](validation.md).
