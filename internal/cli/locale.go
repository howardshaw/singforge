package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func chinese() bool {
	locale := os.Getenv("SINGFORGE_LANG")
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	return strings.HasPrefix(locale, "zh")
}

var zhHelp = map[string]string{
	"edge": "管理 Cloudflare 优选入口", "edge set": "配置优选域名、IP 和默认回退入口", "edge status": "查看默认入口和优选地址",
	"certificate auto-renew": "管理订阅证书自动续签", "certificate auto-renew enable": "通过 Cloudflare DNS-01 配置自动续签", "certificate auto-renew status": "检查续签配置和正在使用的证书", "certificate auto-renew test": "使用测试 CA 演练续签", "certificate auto-renew sync": "同步证书并校验 HTTPS 服务", "certificate": "管理证书与客户端信任", "certificate import": "导入公开受信任的证书", "certificate renew": "更新本机证书与客户端指纹", "firewall": "管理主机入站规则", "firewall sync": "同步所需的 IPv4 防火墙规则", "reality": "配置 Reality 握手", "reality configure": "设置 TLS 域名和握手目标", "cloudflared": "管理隧道连接器", "cloudflared update": "校验并更新连接器", "cloudflared rollback": "恢复之前的连接器", "update": "统一更新已启用的组件", "version": "显示 SingForge 与内核版本", "rules schedule": "启用定时规则更新", "rules unschedule": "关闭定时规则更新", "cloudflare dns cleanup": "清理本工具创建的 DNS 记录", "cloudflare tunnel routes": "查看域名、路径和回环上游",
	"singforge": "部署与管理单机 sing-box 服务器", "install": "校验配置、安装内核并启动服务", "status": "查看部署状态", "doctor": "验证配置和服务健康状态", "start": "启动托管服务", "stop": "停止托管服务", "restart": "重启托管服务", "reload": "重新加载配置", "logs": "查看近期系统日志",
	"protocol": "管理入站协议", "protocol list": "查看协议能力", "protocol configure": "修改监听端口", "protocol enable": "启用协议，保留已有凭据", "protocol disable": "停用协议，保留已有凭据", "endpoint": "管理公开连接入口", "endpoint add": "添加或修改入口", "endpoint remove": "移除入口", "endpoint test": "检查 TCP 可达性", "endpoint list": "查看入口列表",
	"subscription": "管理订阅和客户端导出", "mihomo-policy": "分流 DNS 策略：AI 国际站使用境外加密 DNS，AI 国内站按策略选择", "subscription list": "查看订阅授权概况", "subscription show": "显示包含凭据的客户端配置或二维码", "subscription publish": "通过 HTTPS 发布订阅", "subscription unpublish": "关闭发布并撤销授权", "subscription rotate": "生成新的订阅链接", "subscription revoke": "撤销所有订阅令牌",
	"domain": "管理域名", "domain configure": "配置 DNS-only 直连域名", "domain status": "查看域名状态", "domain sync": "同步 DNS 记录", "cloudflare": "管理 Cloudflare 接入", "cloudflare enable": "启用 CDN 或具名隧道", "cloudflare disable": "停用接入方式", "cloudflare configure": "验证 API 令牌并发现区域", "cloudflare status": "查看接入域名", "cloudflare dns": "管理 DNS 记录", "cloudflare dns sync": "检查冲突并同步记录", "cloudflare dns list": "查看 DNS 记录", "cloudflare tunnel": "管理具名隧道", "cloudflare tunnel list": "列出已有隧道", "cloudflare tunnel create": "创建具名隧道", "cloudflare tunnel status": "查看连接状态",
	"dns": "管理客户端 DNS", "dns show": "查看解析策略", "dns set": "配置隐私策略和加密解析器", "dns test": "检查本地客户端 DNS 监听器", "rules": "管理分流规则", "rules list": "查看内置规则和缓存版本", "rules set": "选择分流配置", "rules test": "解释域名或 IP 的匹配结果", "rules update": "下载并校验规则", "rules validate": "校验本地规则", "rules override": "添加优先匹配规则", "rules rollback": "恢复上一份规则缓存",
	"core": "管理内核版本", "core update": "校验并启用新的内核", "core rollback": "切换至上一版本", "self": "更新命令行程序", "self update": "校验并替换命令行程序", "self rollback": "恢复命令行程序", "backup": "备份与恢复", "backup create": "创建私密备份", "backup restore": "校验并恢复备份", "bbr": "维护 Linux TCP 拥塞控制", "bbr status": "检查内核设置", "bbr enable": "应用可回滚的网络参数", "bbr disable": "恢复原始参数", "uninstall": "卸载托管服务，可选择清理私密状态",
}

func localize(root *cobra.Command) {
	if !chinese() {
		return
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		key := strings.TrimPrefix(c.CommandPath(), "singforge ")
		if v := zhHelp[key]; v != "" {
			c.Short = v
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
}
