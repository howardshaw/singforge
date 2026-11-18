# Validation and reproducibility

Validation was performed on 2026-11-19. These results cover the tested clients, operating systems and workflows. A production rollout should also exercise its own domain, certificates, security group and Cloudflare account.

| Boundary | Observed result |
|---|---|
| Go quality | Formatting, module integrity, go vet, Staticcheck v0.8.2 and race-enabled tests passed. govulncheck v1.10.0 reported no reachable known vulnerabilities in the Go CLI at scan time. |
| Linux artifacts | Cross-built linux/amd64 and linux/arm64 executables with CGO disabled. SHA-256 verification passed. Both architectures were executed on Linux. |
| Public VPS | Ubuntu 24.04 amd64 under sudo. Verified official release fetch, systemd installation, independent service identity, protocol port changes, digest-verified self-update and boot enablement. A remote sing-box client using each of Vision Reality, Hysteria2 and Shadowsocks made an HTTPS request and verified the VPS egress IP. |
| Live Named Tunnel | A dashboard-managed connector authenticated with its systemd credential file. Two published application routes shared one hostname. A separate Linux client fetched external HTTPS data through every generated hostname endpoint (three direct, two tunnel) and verified the VPS egress address. Both tunnel routes also passed from Mihomo on macOS. |
| Terminal console | Tested real PTYs locally and over SSH under sudo. Arrow keys opened a real result view, Esc returned, L switched language and q exited. |
| Existing deployment migration | Backed up the legacy configuration and verified the backup. Retired its dedicated services and removed its identified firewall rules. SSH remained available. Repeated the three authenticated Internet requests after cutover. |
| Debian | Debian 12 ARM64 cloud image booted under full-system QEMU. Installed the recommended preset, checked service health, disabled and re-enabled Hysteria2, detached and purged the deployment. On another install the core and scheduled rule timer started after a guest reboot verified by a changed boot ID. |
| Ubuntu VM | Ubuntu 24.04 ARM64 cloud image with real systemd. Tested dedicated Nginx, TLS subscriptions, reject/authorize/revoke behavior, steady core PID across revocation, backup, uninstall and restore. A bind conflict exposed an early health-check flaw during development. Rechecking the service PID's owned sockets detected the failure and restored the previous generation. |
| Kernel | The disposable Ubuntu guest switched from cubic to BBR and restored cubic. Memory-dependent tuning calculations have deterministic unit tests. |
| Client traffic | Official sing-box 1.14.4 and Mihomo 1.19.34 each carried authenticated traffic for SS AEAD, SS2022, Trojan, TUIC, Hysteria2 and AnyTLS. Native Snell v6 also carried traffic with sing-box. The fixture forces global routing so the loopback test target cannot bypass the proxy. |
| Shared TCP/UDP port | An Ubuntu VM ran the generated sing-box server with TCP Reality and UDP Hysteria2 simultaneously on port 443. Each client authenticated and fetched a response. An ordinary certificate-verifying HTTPS client reached a separate loopback TLS 1.3 Nginx site via the Reality handshake target. |
| CDN origin and Tunnel | Validated Nginx configuration and passed real TLS/WebSocket proxy traffic over VLESS, Trojan and VMess paths. The official cloudflared 2026.11.1 binary accepted the generated single-tunnel ingress and matched the configured paths to their separate upstream ports. Mock API tests exercise DNS ownership, conflicts, creation and tunnel API behavior. |
| Subscriptions | Real HTTPS origin returned exported data to a valid grant and 404 after revocation. Tests cover expiration, grace, rate limit, method/path/host boundaries and caching. QR PNG generation and private output permissions were checked. |
| DNS | With a local TLS DoH canary, the strict native client resolved via the proxy. Stopping the data plane caused a fresh query to fail without reaching the canary directly. Native and Mihomo configs with TUN, DoT and IPv6 resolver selection passed real parser checks. |
| Rules and state | Downloaded and parsed pinned source data with verified digests. Tested rule priority, CIDR and suffix semantics, malicious input, workspace isolation, atomic changes, lock contention, backup integrity and journal recovery. |

## Repeat locally

```sh
make check
make release
go install golang.org/x/vuln/cmd/govulncheck@v1.10.0
make scan
make integration CORE=/absolute/path/to/sing-box \
  MIHOMO=/absolute/path/to/mihomo CLOUDFLARED=/absolute/path/to/cloudflared
```

Integration requires Python 3, curl, OpenSSL and Nginx. It uses a temporary workspace, test-only private certificates, loopback sockets and ephemeral ports. To reproduce the exact shared port inside a disposable Linux guest, run the topology test with `--port 443` and permission to bind that port.

For service lifecycle testing, build the CLI, copy it and [systemd.sh](../tests/systemd.sh) to a fresh disposable Ubuntu or Debian VM with Nginx installed, and run the following **inside the guest**.

```sh
sudo env SINGFORGE_DISPOSABLE_VM=1 sh tests/systemd.sh /absolute/path/to/singforge
```

This test installs and removes services and reserved host files. It intentionally occupies a port and exercises a simulated update that passes version/config checks but fails at runtime. Ordinary Docker containers are unnecessary. For the recorded ARM64 VM runs, Docker hosted QEMU, with systemd, sysctls and service accounts residing inside the guest kernels.

## Remaining verification boundaries

- The public VPS acceptance exercised direct access and an actual remotely managed Named Tunnel. Ordinary orange-cloud CDN Full (strict) was tested at the HTTPS origin boundary. Provider API provisioning and DNS conflict handling used mocks.
- On one macOS host the standalone sing-box process received EOF during direct encrypted bootstrap for endpoint names. The unchanged hostname exports worked in Linux. Both remote routes also worked on the Mac when testing TLS against a verified edge IP. The macOS Mihomo test found a stale OS hosts entry. Strict Mihomo exports now disable system-hosts overrides.
- Tests include IPv6 endpoint and resolver generation. Public IPv6-only and NAT64 Internet paths, browser-specific DNS bypasses and client GUI imports require separate client/network tests.
- Traffic evidence for the recommended Vision variant does not extend to every advanced protocol. Reality gRPC, ShadowTLS and platform-specific Naive outbound have narrower validation than the protocols enumerated in the traffic rows.
- Ubuntu 22.04 is a supported installation target. The recorded Ubuntu guest and VPS both used 24.04.
- The build produces release files and tagged CI workflows. No public release, hosted CI run or external security audit is claimed.
