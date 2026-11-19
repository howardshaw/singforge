# Reality、Hysteria2 与 Tunnel 的三种组合

| 组合                            | VPS 公网入口       | 用途                                                |
|---------------------------------|--------------------|-----------------------------------------------------|
| Reality + Hysteria2             | TCP 443 和 UDP 443 | 分别提供 TCP 和 QUIC 路径                           |
| Reality + Tunnel WS             | TCP 443            | UDP 不通时也可以选择经 Cloudflare 的 WebSocket 入口 |
| Reality + Hysteria2 + Tunnel WS | TCP 443 和 UDP 443 | 保留两种直连方式，再增加按需启用的备用路径          |

同一个端口号可以同时分配给 TCP 和 UDP。两个 Hysteria2/TUIC UDP 入站仍不能抢占同一个 UDP 套接字。Reality 与普通公网 Nginx
同时绑定相同地址的 TCP 443 也会冲突。

## 部署两种直连协议

在新的 VPS 上，先把示例 IP 替换为真实公网地址。

```sh
sudo singforge install --mode direct --preset custom \
  --protocols reality,hysteria2 --host 203.0.113.10
sudo singforge protocol configure --id hysteria2 --port 443
sudo singforge doctor
```

主机防火墙和云安全组都要允许 TCP 443、UDP 443。Hysteria2 使用自己的 TLS 信任配置。SingForge 默认在原生 sing-box 和 Mihomo
配置中提供信任材料。需要公开信任链的客户端可以导入匹配的证书。UDP 443 被运营商限制时，客户端可以选择 Reality 或 WS 入口。

## 可选的本机 HTTPS 网站

先在回环地址启动 TLS 1.3 网站，例如 `127.0.0.1:9443`。准备与网站域名匹配且客户端信任的证书，维护其续签。然后设置 Reality 的
SNI 和实际握手目标。

```sh
sudo singforge reality configure \
  --handshake www.example.org \
  --handshake-target 127.0.0.1:9443
```

普通 HTTPS 访问可以由 Reality 转交给该网站。合规的 Reality 客户端则进入代理。把握手目标指回 Reality 自己监听的 TCP 443
会形成循环。网站的 QUIC 监听也要避开 Hysteria2 使用的 UDP 443。此命令连接已有网站，网站进程和证书续签由运营者管理。

## 可选的 Cloudflare Tunnel

```sh
sudo singforge cloudflare enable --mode tunnel \
  --host edge.example.org --credentials /root/named-tunnel.json
sudo singforge endpoint list
```

将 `edge.example.org` 配置为指向 `<tunnel-uuid>.cfargotunnel.com` 的代理 CNAME；已经配置 API Token 和 Zone 时可以使用
`cloudflare dns sync`。直连域名独立配置为 DNS-only。Tunnel 原生 ingress 用不同路径转发至回环上的 VLESS-WS 和
Trojan-WS。这一步不会启用普通 CDN 模式，也无需开放公网 HTTP 端口。

第二种组合可以从 `--preset custom --protocols reality` 开始，再开启 Tunnel。第三种组合可以在两种直连协议运行后追加
Tunnel。入口共用同一台 VPS，客户端可以手选，自动选路取决于客户端的策略组。

本机订阅导出可以与以上组合共存。当前原生 `subscription publish` 使用独立 Nginx 的 TCP 443，与同地址的 Reality TCP 443
有端口冲突。需要公网发布时应先规划独立监听资源，或调整 Reality 端口。
