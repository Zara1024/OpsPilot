# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **懂你的系统和基础设施、查得出根因、还能动手修复的 AI Agent，直接在飞书和钉钉里指挥�?*

*指标 · 日志 · 链路 · 拓扑影响�?· 根因关联 · 远程执行 · 告警自动排查 · 知识库与代码 RAG 检�?· 专家 Agent 与技能�?

[![Go Report Card](https://goreportcard.com/badge/github.com/Zara1024/OpsPilot)](https://goreportcard.com/report/github.com/Zara1024/OpsPilot)
[![Release](https://img.shields.io/github/v/release/Zara1024/OpsPilot?logo=github&label=release&color=2563eb)](https://github.com/Zara1024/OpsPilot/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/Zara1024/OpsPilot?logo=go&logoColor=white&color=00ADD8)](go.mod)
[![License](https://img.shields.io/badge/License-AGPLv3-blue.svg?logo=gnu)](https://www.gnu.org/licenses/agpl-3.0.html)
[![Stack](https://img.shields.io/badge/stack-Go%20%7C%20TypeScript%20%7C%20React-1e40af?logo=react&logoColor=white)](#features)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-22c55e.svg?logo=git&logoColor=white)](CONTRIBUTING.md)
[![Telegram](https://img.shields.io/badge/Telegram-Join-26A5E4?logo=telegram&logoColor=white)](https://t.me/opspilotai)
[![Slack](https://img.shields.io/badge/Slack-Join-4A154B?logo=slack&logoColor=white)](https://join.slack.com/t/opspilot-co/shared_invite/zt-400skx7hz-WU1nmF1XVYH4S3Q1NfWrbw)

<p>
  <a href="https://trendshift.io/repositories/48061?utm_source=repository-badge&amp;utm_medium=badge&amp;utm_campaign=badge-repository-48061" target="_blank" rel="noopener noreferrer">
    <img src="https://trendshift.io/api/badge/repositories/48061" alt="OpsPilot on Trendshift" width="250" height="55" />
  </a>
</p>

[English](./README.md) | 简体中�?| [日本語](./README_JA.md) | [한국어](./README_KO.md) | [Español](./README_ES.md) | [Français](./README_FR.md) | [Deutsch](./README_DE.md) | [Português](./README_PT.md) | [Русский](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Kubernetes 生命周期管理" width="100%" />
  <br />
  <strong>☸️ Kubernetes 生命周期管理现已发布</strong><br />
  <sub>通过 Edge 注册集群，查看工作负载与事件，管理升级，并将 Kubernetes 资源关联到拓扑�?/sub>
</p>

<div align="center">

[特性](#特�? �?[安装](#安装) �?[集成](#集成) �?[许可证](#许可�?

</div>

## 特�?
- 🤖 **Coordinator + Specialist 双层 Agent** �?coordinator 派活�?SRE / 网络 / DB �?agent
- 🚨 **告警触发自动调查** �?investigator �?RCA worker, 根因 + 证据回填到聊�?- 🔍 **根因 RCA** �?沿拓扑做爆炸半径分析, 跨指�?日志/链路相关到源码行
- 🔒 **零入站端�?* �?edge 主动外联, host 不开 22 / 80 / 443
- 💻 **浏览�?SSH** �?反向隧道开交互�?shell, 不用 key / 跳板�? 全程审计
- 🐳 **一行命令自托管** �?`install.sh` 起整套栈
- 📊 **可观测全栈内�?* �?Prometheus + Loki + Tempo + Grafana 自动�? Agent 自动�?query
- 🧠 **自带任意模型** �?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, 热路�?- 💬 **双向 IM 通道** �?Slack / Telegram / Larksuite / DingTalk / WeCom, 按通道语言
- 🛠�?**只读主机巡检工具** �?bash 沙箱 + 26+ 工具, 每次调用全审�?- ☸️ **Kubernetes 生命周期管理** �?注册集群、查看工作负载与事件、管理升级，并将 Kubernetes 资源映射到拓�?- 🌐 **网络设备管理** �?�?Edge 主机发现邻居，经 SNMP 校验和轮询接口状态，并建立主机到网络设备的拓扑关�?
## 安装

按服务器架构下载最�?release（`linux-amd64` �?`linux-arm64`），解压后运行安装脚本（Ubuntu 22.04+、Debian 12+、RHEL/Rocky 9）：

按服务器架构选择对应命令�?
**AMD64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-amd64.tar.xz
tar -xf opspilot-v1.1.0-linux-amd64.tar.xz && cd opspilot-v1.1.0-linux-amd64
sudo ./install.sh
```

**ARM64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-arm64.tar.xz
tar -xf opspilot-v1.1.0-linux-arm64.tar.xz && cd opspilot-v1.1.0-linux-arm64
sudo ./install.sh
```

**🇨🇳 中国大陆用户** �?GitHub 较慢时，选择对应架构�?CDN 镜像地址下载�?
```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-arm64.tar.xz
```

### 域名配置�?SSL 证书自动续签

OpsPilot 支持绑定任意自定义独立域名，并在 Nginx 80 端口原生集成�?ACME 验证挑战穿透路径，可实�?**零停机、无感自动续�?*�?
#### 1. 域名 DNS 解析
在您的域名服务商后台（如阿里云、腾讯云、Cloudflare、DNSPod 等）添加一�?**A 记录**，将您的域名（例�?`opspilot.example.com`）解析至部署服务器的公网 IP�?
#### 2. 修改配置文件 (`.env`)
在解压安装目录或 `/opt/opspilot/.env` 中配置您的真实域名：
```bash
# 对外公开的基础 URL（Edge 边缘客户端将使用此地址上报遥测与日志）
OPSPILOT_PUBLIC_URL=https://opspilot.example.com

# 管理员账号邮箱（可选）
OPSPILOT_ADMIN_EMAIL=admin@opspilot.example.com
```
> **提示**：`OPSPILOT_TUNNEL_ADDR` 保持留空即可，系统会自动识别 `OPSPILOT_PUBLIC_URL` 中的域名并自动拼�?`40012` 隧道端口�?
#### 3. 申请正式 SSL 证书与配置自动续�?使用 Certbot 申请 Let's Encrypt 免费证书并建立全自动续签链条�?
```bash
# 1. 安装 certbot 工具（Ubuntu / Debian�?sudo apt update && sudo apt install -y certbot

# 2. 申请域名 SSL 证书（利用预置的 Webroot 验证路径�?sudo mkdir -p /var/www/certbot
sudo certbot certonly --webroot -w /var/www/certbot -d opspilot.example.com --non-interactive --agree-tos --email 您的邮箱@example.com

# 3. 复制证书�?OpsPilot 证书挂载目录
sudo mkdir -p /opt/opspilot/certs
sudo cp -L /etc/letsencrypt/live/opspilot.example.com/fullchain.pem /opt/opspilot/certs/tls.crt
sudo cp -L /etc/letsencrypt/live/opspilot.example.com/privkey.pem /opt/opspilot/certs/tls.key
sudo chmod 644 /opt/opspilot/certs/tls.crt && sudo chmod 600 /opt/opspilot/certs/tls.key

# 4. 配置证书自动续签与热重载 Hook
sudo tee /etc/letsencrypt/renewal-hooks/deploy/opspilot-deploy.sh > /dev/null << 'EOF'
#!/bin/bash
CERT_DIR="${RENEWED_LINEAGE:-/etc/letsencrypt/live/opspilot.example.com}"
DEST_DIR="/opt/opspilot/certs"
if [ -d "$DEST_DIR" ]; then
    cp -L "$CERT_DIR/fullchain.pem" "$DEST_DIR/tls.crt"
    cp -L "$CERT_DIR/privkey.pem" "$DEST_DIR/tls.key"
    chmod 644 "$DEST_DIR/tls.crt" && chmod 600 "$DEST_DIR/tls.key"
    docker exec opspilot-nginx nginx -s reload 2>/dev/null || true
fi
EOF
sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/opspilot-deploy.sh
```

#### 4. 重启服务生效
```bash
cd /opt/opspilot
sudo docker compose restart nginx
```
> **架构说明**：OpsPilot �?Nginx 内部配置采用泛匹配模式（`server_name _;`），更换域名�?*无需手动修改 Nginx 配置文件**，仅需�?`.env` 中更�?`OPSPILOT_PUBLIC_URL` 并替�?`/opt/opspilot/certs/` 下的证书公私钥即可�?
## 产品导览

### 根因分析

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="根因分析" width="100%" />
</p>

从告警或运维问题出发，汇总拓扑、设备、指标、日志和变更上下文，形成带证据的根因分析与下一步建议�?
### 工作流编�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="工作流编�? width="100%" />
</p>

把触发器、Agent、工具、条件和通知节点串成可复用的自动化流程，并保持可编辑、可审查�?
### 技能目�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="技能目�? width="100%" />
</p>

展示 Agent 可调用的工具、描述和边界，让运维人员能清楚看到自动化能力面�?
### MCP 服务�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="MCP 服务�? width="100%" />
</p>

注册外部 MCP 服务器，�?Grafana、Kubernetes、PagerDuty、GitHub 或内部平台工具接入同一套治理清单�?
### 知识�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="知识�? width="100%" />
</p>

索引 Runbook、事故记录、架构笔记和代码仓库，让人和 Agent 使用同一份上下文�?
### 产物中心

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="产物中心" width="100%" />
</p>

集中保存 Agent 和工作流生成的页面与报告，默认私有，便于审阅和交接�?
### 监控

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="监控" width="100%" />
</p>

在同一个工作台里查看主机健康、日志、链路和告警状态，�?RCA 收集证据�?
### 拓扑�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="拓扑�? width="100%" />
</p>

可视化服务、集群、设备与故障域之间的依赖关系，用于判断影响面�?
### 审批与写入闸�?
<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="审批与写入闸�? width="100%" />
</p>

高风险动作先进入审批和策略边界，避免 Agent 直接修改生产系统�?
## 文档

完整产品文档�?[opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction)�?
| 领域 | 从这里开�?|
|---|---|
| **快速开�?* | [介绍](https://opspilot.cloud/docs/get-started/introduction) · [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) · [架构](https://opspilot.cloud/docs/get-started/architecture) · [概念](https://opspilot.cloud/docs/get-started/concepts) |
| **安装与运�?* | [服务端安装](https://opspilot.cloud/docs/install/server) · [边缘安装](https://opspilot.cloud/docs/install/edge) · [首次启动](https://opspilot.cloud/docs/install/first-boot) · [升级](https://opspilot.cloud/docs/install/upgrade) |
| **能力** | [告警](https://opspilot.cloud/docs/capabilities/alerts) · [RCA](https://opspilot.cloud/docs/capabilities/rca) · [监控](https://opspilot.cloud/docs/capabilities/monitoring) · [日志](https://opspilot.cloud/docs/capabilities/logs) · [链路](https://opspilot.cloud/docs/capabilities/traces) · [知识库](https://opspilot.cloud/docs/capabilities/knowledge) · [技能](https://opspilot.cloud/docs/capabilities/skills) |
| **Agent** | [概览](https://opspilot.cloud/docs/agents/overview) · [Coordinator](https://opspilot.cloud/docs/agents/coordinator) · [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) · [Specialists](https://opspilot.cloud/docs/agents/specialists) · [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **参�?* | [API](https://opspilot.cloud/docs/reference/api) · [CLI](https://opspilot.cloud/docs/reference/cli) · [告警规则](https://opspilot.cloud/docs/reference/alert-rules) · [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) · [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## 集成

即插即用，对接团队现有的可观测、IM 通道与模型栈�?
| | |
|---|---|
| **可观�?* | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **通道** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **模型** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## 许可�?
AGPLv3 �?�?[LICENSE](LICENSE)�?
OpsPilot 名称、Logo、域名和相关品牌资产不随 AGPLv3 授权，见
[TRADEMARK.md](TRADEMARK.md)�?
## 加入群组

<p align="center">
  <img src="docs/assets/community/wechat-group-qr.jpg" alt="OpsPilot 微信群二维码" width="200" />
</p>


扫码加入 OpsPilot 开发者社区，交流部署、AIOps 场景、工作流和插件扩展�?