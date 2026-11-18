# SingForge

SingForge 是单台 VPS 的 sing-box 部署和管理工具。Go CLI 使用 Cobra 解析命令、校验必填参数和生成帮助。systemd 独立运行代理服务。直连模式只需 sing-box，CDN 模式增加专用 Nginx，具名隧道使用 cloudflared。

## 编译、安装与连接

使用 Go 1.26.11 或以上版本。

```sh
make check
make release
```

将适合 VPS 架构的产物与仓库中的引导脚本复制到服务器。以下示例使用 AMD64，ARM64 对应文件为 `singforge-linux-arm64`。摘要取自可信构建结果。

```sh
sudo sh scripts/install.sh ./singforge-linux-amd64 <可信的SHA256>
sudo singforge install --mode direct --preset recommended --host 203.0.113.10
sudo singforge doctor
sudo singforge endpoint list
sudo singforge subscription show --format mihomo --output client.yaml
```

将示例地址替换为 VPS 公网地址。云平台使用 NAT 时必须显式提供公网地址。推荐预设部署 Reality、Hysteria2 和 Shadowsocks。按需在主机防火墙和云安全组放行 TCP 443、UDP 8443、TCP/UDP 8388。客户端文件包含节点凭据，应通过可信渠道传输。

## CLI 管理

直接运行 `sudo singforge` 打开纵向菜单。用 ↑/↓ 或 j/k 选择、Enter 打开、Esc 返回、q 退出、L 切换中英文。SSH 使用交互终端，例如 `ssh -t HOST sudo singforge`。设置 `NO_COLOR=1` 可关闭颜色；管道输出保留文本帮助。

```sh
SINGFORGE_LANG=zh singforge --help
singforge cloudflare tunnel --help
sudo singforge protocol enable --id tuic
sudo singforge protocol configure --id ss --port 8892
sudo singforge subscription show --format uri --filter direct-ss --qr
sudo singforge backup create --file /root/singforge.backup
```

需要公开订阅时，准备独立域名和受信任的 HTTPS 证书。签发链接后可以单独轮换或撤销令牌。修改节点地址和端口会保留协议凭据。普通状态命令可用于检查部署，`subscription show` 会主动显示敏感配置。

同一主机可以同时使用直连、CDN 和具名隧道。CDN 与 Tunnel 均可将同一域名上的不同 WebSocket 路径转发给不同入站。DNS-only 域名仍然直连 VPS。Reality、Hysteria2、TUIC 和裸 Shadowsocks 使用直连入口。

[部署、域名与恢复](docs/operations.md) · [客户端兼容矩阵](docs/protocols.md) · [DNS 隐私和分流](docs/privacy-routing.md) · [实测范围](docs/validation.md) · [架构图](docs/architecture.md)

[Reality TCP 443、Hysteria2 UDP 443、本机 HTTPS 握手目标与 Tunnel 的组合指南](docs/topologies.zh-CN.md)
