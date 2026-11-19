# SingForge

SingForge 是单台 VPS 的 sing-box 部署和管理工具。Go CLI 使用 Cobra 解析命令、校验必填参数和生成帮助。systemd
独立运行代理服务。直连模式只需 sing-box，CDN 模式增加专用 Nginx，具名隧道使用 cloudflared。

## 安装前准备

准备一台 Debian 12+ 或 Ubuntu 22.04+ 的 VPS，确认使用 systemd、有 SSH 和 sudo/root 权限，并记下公网 IP 和 CPU 架构（`uname -m`）。云平台使用 NAT 时，安装参数 `--host` 填公网地址。按你打算使用的入口准备以下资源：

| 用途 | 域名和 DNS | VPS 和凭据 |
|---|---|---|
| 只用 IP 直连，私下导入客户端 | 公网 IPv4 或 IPv6 即可 | 放行所选协议的 TCP/UDP 端口。可以跳过域名购买和 Cloudflare 注册 |
| 直连域名，例如 `fast.example.org` | A/AAAA 指向 VPS 公网地址。Cloudflare 上选 **DNS only** | 手动添加 DNS 记录即可；需要 CLI 自动同步时再创建管理 Token |
| Cloudflare CDN，例如 `cdn.example.org` | Cloudflare 账户、已激活的 Zone、开启代理的 A/AAAA | Nginx、与域名匹配的源站证书和私钥、Cloudflare Full (strict)，以及可访问的 HTTPS 源站端口 |
| Named Tunnel，例如 `edge.example.org` | 已激活的 Cloudflare Zone，代理 CNAME 指向 `<隧道 UUID>.cfargotunnel.com` | VPS 能出站连接 Cloudflare。本地管理用隧道凭据 JSON；控制台管理用 connector token |
| 公开 HTTPS 订阅，例如 `sub.example.org` | 独立子域名指向 VPS，证书受客户端信任 | Nginx 和空闲的公网 TCP 443。若 Reality 已占用同地址 TCP 443，先规划新端口。Cloudflare DNS-01 自动续签另需专用 Token |

**购买与托管域名。** 已有域名可直接使用。希望降低成本时，可以在 Spaceship 搜索 **6 至 9 位纯数字**的 `.xyz`，这一类属于注册局的 1.111B Class。付款前对比具体域名的首年价、续费价和 ICANN 等费用；普通 `.xyz` 的首年促销与数字域名的长期价格可能不同。[Spaceship 价格](https://www.spaceship.com/domains/gtld/xyz/) · [注册局数字域名说明](https://gen.xyz/1111b)

使用 Cloudflare 托管时，注册账户、添加根域名并核对导入的现有记录（含邮件 MX/TXT）。在 Spaceship 或原注册商处将权威名称服务器改为 Cloudflare 分配的 NS，待 Zone 显示 **Active** 后，再按上表创建所需子域名。[Cloudflare 接入步骤](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/)

**按用途准备凭据。** SingForge 自动管理 DNS 使用限定目标 Zone 的 Zone Read 和 DNS Edit Token；通过 API 创建隧道还需目标账户的 Tunnel Write 权限。Certbot 使用独立的 Zone DNS Edit Token，写入 root 专用的 INI 文件（0600）。connector token 供 cloudflared 运行一条隧道。具体命令和凭据格式见[运维指南](docs/operations.md#cloudflare)与[自动续签说明](docs/operations.md#certbot-renewal-for-public-subscriptions)。

### 将示例换成自己的域名

复制[混合部署示例](configs/hybrid.yaml)，将 `endpoints.direct`、`endpoints.cdn`、`endpoints.tunnel` 填为自己创建的完整主机名，并替换证书和凭据路径。已有部署可用 `singforge domain configure --domain example.org --hostname fast --mode dns-only` 更新直连主机名。

直连境外 DNS 导致节点启动超时时，可在服务器执行 `sudo singforge dns set --mihomo-node-dns alidns`，然后刷新完整 Mihomo 订阅。SingForge 会根据**这次导出的节点**自动生成 `proxy-server-nameserver-policy`：例如三个节点共用 `fast.example.org`、另两个共用 `edge.example.org`，配置中就只有这两个精确域名。IP 节点跳过，重复域名合并。这样也能覆盖使用不同根域名的节点。通配整个 Zone 会让其他同后缀的节点也命中 AliDNS，因此默认使用精确匹配，无需手写 YAML 或添加泛域名 DNS 记录。应用域名继续使用各自的 DNS 策略。

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

### Cloudflare 优选入口（Clash Verge / Mihomo）

在服务器执行 `sudo singforge edge set --mode all`，或在「订阅与二维码 → Cloudflare 优选入口」选择「发布全部优选域名和 IP 节点」。刷新原有的完整 Mihomo 订阅后，打开 Clash Verge 的 **代理 → Main**，点击延迟测试，直接点选想用的节点。每个协议的默认入口和优选地址相邻显示，例如 `tunnel-vless-ws`、`tunnel-vless-ws / cf.090227.xyz`、`tunnel-vless-ws / cloudflare.182682.xyz`。Trojan 同样单独列出。

选 **Auto** 可让当前客户端每 120 秒按 HTTPS 探测延迟自动选择，25 ms 切换容差用于减少抖动。Auto 包含直连与所有可用配置的优选节点，**Preferred Edge** 只比较 Cloudflare 节点。手动点选具体节点会固定该选择。健康探测衡量到探测站点的延迟，实际下载速度和特定网站体验还取决于线路及目的地。

```sh
sudo singforge edge status
sudo singforge edge set --mode all
sudo singforge edge set --mode all --domains cf.090227.xyz,cloudflare.182682.xyz --ips 188.114.96.1
sudo singforge edge set --mode default
```

完整订阅和 Mihomo proxy-provider 都包含这些独立节点。`all` 发布当前版本内置的社区域名和 Cloudflare IP 变体。用 `edge status` 对照内置候选和已发布地址。使用默认列表的部署升级后可重新执行 `edge set --mode all` 更新订阅。`ips` 与 `domains` 模式可单独选用。每个变体沿用原入口的 TLS SNI、WebSocket Host、路径、凭据和证书校验。IPv6 候选需要开启客户端 IPv6。[Mihomo 自动选择](https://wiki.metacubex.one/config/proxy-groups/url-test/)

### Mihomo 国内外 DNS 分流

`sudo singforge dns set --mihomo-policy split --mihomo-ipv6 off` 会重新生成完整的 Clash Verge Rev / Mihomo 订阅。smart 模式下仅列入允许名单的普通国内主机使用 AliDNS / DNSPod。中国域名和 IP 分流由 Mihomo 内置的 GEOSITE/GEOIP 数据集匹配，并每 24 小时更新，不会展开成十万条订阅规则。AI 分为国际站 `AI` 与国内站 `AI-CN` 代理组；仅 `split` 模式会将明确列出的国内 AI 域名交给国内加密 DNS，`strict` 模式仍全部使用境外解析器，其他未分类域名的客户端 DNS 默认走 Main。若希望所有公网域名都不交给国内解析器，可选择 `--mihomo-policy strict`。更新订阅后检查 Clash Verge Rev 的实际运行配置，开启 TUN 服务权限，并在 Main 中选择自动组或具体节点。[配置与测试说明](docs/privacy-routing.md)
