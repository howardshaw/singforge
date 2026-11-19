# SingForge

在单台 Linux VPS 上部署和管理 sing-box。命令行和数字菜单都支持直连 Reality、Hysteria2、Shadowsocks；可选 Cloudflare Named Tunnel 备用入口和 HTTPS 订阅。

[English](README.md) · [下载 Release](https://github.com/howardshaw/singforge/releases) · [命令索引](docs/cli.md) · [运维指南](docs/operations.md)

## 在 VPS 安装正式版

准备 Debian 12+ 或 Ubuntu 22.04+、systemd、sudo/root 和 amd64 或 arm64。下面的命令在**新 VPS** 上运行，直接下载 GitHub Actions 构建的 CLI。安装核心还需能通过 HTTPS 访问上游发布站点。

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
sudo install -m 600 singforge.env.example /root/singforge/.env  # 仅新机首次执行
sudoedit /root/singforge/.env
```

安装器自动选择架构，并用同一 GitHub Release 的 SHA-256 清单核对二进制。已有部署可用 `sudo singforge self update --yes` 更新 CLI。

## 填写 .env

**只有 VPS IP** 时，只填 `PUBLIC_IP`，云服务器经 NAT 时填公网映射地址。`DOMAIN` 留空，两个功能开关保留 false，直接进入「部署与连接」。

**需要直连、Tunnel 和 HTTPS 订阅**时，使用已激活的 Cloudflare 域名。新购域名先在注册商设置 Cloudflare 分配的两个 nameserver，等 Zone 显示 Active。每台服务器使用独立 `PREFIX`，可以共用一个根域名。[域名接入](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/)

1. 在 Cloudflare「My Profile → API Tokens → Create Token」创建部署令牌。为指定 Zone 授予 **Zone Read、DNS Edit**；开启 Tunnel 时，再给所属账户添加 **Cloudflare Tunnel Write** 或 **Cloudflare One Connector: cloudflared Edit**。
2. 再创建一枚证书续签令牌，只授予同一 Zone 的 **DNS Edit**。在 VPS 安装依赖并填写以下文件。[创建 Token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/) · [Certbot DNS 插件](https://certbot-dns-cloudflare.readthedocs.io/en/stable/)

```sh
sudo apt-get update
sudo apt-get install -y nginx certbot python3-certbot-dns-cloudflare
sudo touch /root/singforge/cloudflare.token /root/singforge/certbot.ini
sudo chmod 600 /root/singforge/cloudflare.token /root/singforge/certbot.ini
sudoedit /root/singforge/cloudflare.token   # 只填部署令牌本身，一行
sudoedit /root/singforge/certbot.ini        # dns_cloudflare_api_token = 续签令牌
```

编辑私有 `.env` 中对应的行；Token 通过文件路径引用。配置按字面值解析，未知字段会报错，不执行 shell 代码。

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

替换示例 IP、根域名和邮箱。阅读并接受 [Let's Encrypt 订户协议](https://letsencrypt.org/repository/) 后再设置 `ACME_AGREE_TOS=true`。上述示例生成三个入口：

| 域名 | DNS | 用途 |
|---|---|---|
| `vps2-fast.example.org` | A 或 AAAA，DNS only | 协议直连 |
| `vps2-edge.example.org` | 指向独立 Tunnel 的 CNAME，Proxied | VLESS-WS、Trojan-WS |
| `vps2-sub.example.org` | A 或 AAAA，DNS only | HTTPS 订阅地址 |

## 部署与连接

在云安全组和主机防火墙放行入站 TCP 8881（Reality）、UDP 8882（Hysteria2）、TCP/UDP 8892（Shadowsocks）。启用订阅时放行 TCP 443，保留 SSH 端口。三个直连端口可以在模板中修改。Tunnel 使用出站 TCP/UDP 7844。[Cloudflare 防火墙要求](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/)

```sh
sudo singforge provision --env-file /root/singforge/.env --dry-run
sudo singforge provision --env-file /root/singforge/.env
sudo singforge doctor
sudo singforge
```

预览显示本地部署计划。执行后按所选功能检查 DNS 冲突、创建并启动 Tunnel、通过 DNS-01 签发证书并配置自动续签。中断后可使用同一文件重试，已有节点凭据和订阅授权会保留。

离线导出使用 `sudo singforge subscription show --format mihomo --output client.yaml`。启用发布后，执行 `sudo singforge subscription issue --format mihomo` 并私下保存显示的 URL。在 Clash Verge 导入后可在 **Main** 中逐个测速、选节点。主菜单支持方向键或数字加 Enter，输入 0 可选中退出。

Smart 路由下，国内 AI 域名匹配 `AI-CN`，首次导入默认选择 `DIRECT`（客户端本地直连）。已保存分组选择的客户端可在「代理 → AI-CN」手动切换到 DIRECT。国际 AI 保持代理，严格模式的 DNS 仍通过加密代理路径解析。

## 开发与发版

本地检查使用 `make check`。在 GitHub 的 **Actions → Publish tagged release → Run workflow** 中选择 main 并填写新版本号，即可由 CI 打标签和发布。也可以推送形如 `v0.1.1` 的语义版本标签即触发发布。GitHub 在 Ubuntu 上验证代码和 systemd 生命周期，构建两个 Linux 架构，并发布二进制、安装器、env 模板、SHA256SUMS 和许可证。[发布工作流](.github/workflows/release.yml) · [测试说明](docs/validation.md) · [安全说明](docs/security.md)
