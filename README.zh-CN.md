# SingForge

SingForge 是单台 VPS 的 sing-box 部署和管理工具。Go CLI 使用 Cobra 解析命令、校验必填参数和生成帮助。systemd
独立运行代理服务。直连模式只需 sing-box，CDN 模式增加专用 Nginx，具名隧道使用 cloudflared。

## 编译、安装与连接

使用 Go 1.26.11 或以上版本。

```sh
make check
make release
```

将适合 VPS 架构的产物与仓库中的引导脚本复制到服务器。以下示例使用 AMD64，ARM64 对应文件为 `singforge-linux-arm64`
。摘要取自可信构建结果。

```sh
sudo sh scripts/install.sh ./singforge-linux-amd64 <可信的SHA256>
sudo singforge install --mode direct --preset recommended --host 203.0.113.10
sudo singforge doctor
sudo singforge endpoint list
sudo singforge subscription show --format mihomo --output client.yaml
```

将示例地址替换为 VPS 公网地址。云平台使用 NAT 时必须显式提供公网地址。推荐预设部署 Reality、Hysteria2 和
Shadowsocks。按需在主机防火墙和云安全组放行 TCP 443、UDP 8443、TCP/UDP 8388。客户端文件包含节点凭据，应通过可信渠道传输。

## CLI 管理

直接运行 `sudo singforge` 打开纵向菜单。用 ↑/↓ 或 j/k 选择、Enter 打开、Esc 返回、q 退出、L 切换中英文。SSH 使用交互终端，例如
`ssh -t HOST sudo singforge`。设置 `NO_COLOR=1` 可关闭颜色；管道输出保留文本帮助。

```sh
SINGFORGE_LANG=zh singforge --help
singforge cloudflare tunnel --help
sudo singforge protocol enable --id tuic
sudo singforge protocol configure --id ss --port 8892
sudo singforge subscription show --format uri --filter direct-ss --qr
sudo singforge backup create --file /root/singforge.backup
```

需要公开订阅时，准备独立域名和受信任的 HTTPS 证书。签发链接后可以单独轮换或撤销令牌。修改节点地址和端口会保留协议凭据。普通状态命令可用于检查部署，
`subscription show` 会主动显示敏感配置。

同一主机可以同时使用直连、CDN 和具名隧道。CDN 与 Tunnel 均可将同一域名上的不同 WebSocket 路径转发给不同入站。DNS-only
域名仍然直连 VPS。Reality、Hysteria2、TUIC 和裸 Shadowsocks 使用直连入口。

[部署、域名与恢复](docs/operations.md) · [客户端兼容矩阵](docs/protocols.md) · [DNS 隐私和分流](docs/privacy-routing.md) · [实测范围](docs/validation.md) · [架构图](docs/architecture.md)

[Reality TCP 443、Hysteria2 UDP 443、本机 HTTPS 握手目标与 Tunnel 的组合指南](docs/topologies.zh-CN.md)

### 订阅、二维码和组件更新

在“订阅与二维码”里选择 Clash Verge Rev、Shadowsocks 或 v2rayN 签发独立 HTTPS 地址。签发新链接会保留已有授权。首次发布需要订阅域名以及服务器上的证书链与私钥路径。显示地址后按 `r` 扫码；二维码页面按 `p` 可保存私密 PNG。仅需单个节点时选择“生成节点二维码（离线）”，无需公开订阅。

“组件更新”支持 sing-box、cloudflared、SingForge 和整体更新，选中后会显示命令并要求确认。“运行诊断”会列出每个 systemd 单元、运行状态、PID、启动设置、重启次数和已验证的监听端口；启用 Tunnel 时也会检查连接器的 ready 状态。

### 订阅 HTTPS 证书自动续签

在“订阅与二维码”里选择“配置证书自动续签”。现有手动签发的订阅证书可迁移到 Certbot 的 Cloudflare DNS-01 插件。使用仅限目标 Zone 的 DNS 编辑令牌，将 Certbot INI 存在服务器上，仅 root 可读。菜单填写的是文件路径。

```sh
sudo apt-get install python3-certbot-dns-cloudflare
sudo singforge certificate auto-renew enable --credentials /etc/letsencrypt/singforge-dns.ini --yes
sudo singforge certificate auto-renew status
sudo singforge certificate auto-renew test
```

系统的 `certbot.timer` 定期检查到期时间。续签时插件创建验证 TXT 并清理本次记录；部署 hook 会校验新证书、更新 SingForge 配置并通过本机 TLS 握手核对 Nginx 实际返回的证书。此流程适用于已发布的订阅域名和同名 Certbot 证书。节点使用的自签证书仍有独立的生命周期。

[凭据、首次签发及 Cloudflare API 自动化](docs/operations.md#certbot-renewal-for-public-subscriptions)

### Mihomo 国内外 DNS 分流

`sudo singforge dns set --mihomo-policy split --mihomo-ipv6 off` 会重新生成完整的 Clash Verge Rev / Mihomo 订阅。smart 模式下仅列入允许名单的普通国内主机使用 AliDNS / DNSPod。中国域名和 IP 分流由 Mihomo 内置的 GEOSITE/GEOIP 数据集匹配，并每 24 小时更新，不会展开成十万条订阅规则。AI 分为国际站 `AI` 与国内站 `AI-CN` 代理组；仅 `split` 模式会将明确列出的国内 AI 域名交给国内加密 DNS，`strict` 模式仍全部使用境外解析器，其他未分类域名的客户端 DNS 默认走 Main。若希望所有公网域名都不交给国内解析器，可选择 `--mihomo-policy strict`。更新订阅后检查 Clash Verge Rev 的实际运行配置，开启 TUN 服务权限，并将 Main 设为 Auto 以启用按优先级故障切换。[配置与测试说明](docs/privacy-routing.md)
