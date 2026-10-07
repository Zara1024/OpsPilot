# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **An ops AI Agent that understands your infrastructure, finds the root cause, and fixes it �?right from Slack or Telegram.**

*Metrics · logs · traces · topology blast-radius · root-cause correlation · remote execution · alert-driven auto-investigation · RAG knowledge & code search · specialist agents & skills.*

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

English | [简体中文](./README_ZH.md) | [日本語](./README_JA.md) | [한국어](./README_KO.md) | [Español](./README_ES.md) | [Français](./README_FR.md) | [Deutsch](./README_DE.md) | [Português](./README_PT.md) | [Русский](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Kubernetes lifecycle management" width="100%" />
  <br />
  <strong>☸️ Kubernetes lifecycle management is now available</strong><br />
  <sub>Enroll clusters through Edge, inspect workloads and events, manage upgrades, and connect Kubernetes resources to topology.</sub>
</p>

<div align="center">

[Features](#features) �?[Install](#install) �?[Integrations](#integrations) �?[License](#license)

</div>

## Features

- 🤖 **Coordinator + Specialist agents** �?coordinator dispatches to SRE / network / DB sub-agents
- 🚨 **Auto-investigate on alert** �?investigator spawns an RCA worker, writes the cause back to chat
- 🔍 **Root-cause RCA** �?walks topology, correlates m/l/t, pins the "why" to a source-code line
- 🔒 **Zero inbound ports** �?edge dials out; no port 22 / 80 / 443 on hosts
- 💻 **Browser SSH** �?reverse-tunnel shell into any host; no keys, no jumpbox, all audited
- 🐳 **Self-host in one command** �?`install.sh` brings up the full stack
- 📊 **Built-in observability** �?Prometheus + Loki + Tempo + Grafana wired; the agent writes the queries
- 🧠 **Bring your own model** �?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, hot routing
- 💬 **Two-way IM channels** �?Slack / Telegram / Larksuite / DingTalk / WeCom, per-channel locale
- 🛠�?**Read-only host tools** �?bash sandbox + 26+ inspection tools; every call audited
- ☸️ **Kubernetes lifecycle** �?enroll clusters, inspect workloads and events, manage upgrades, and mirror Kubernetes resources into topology
- 🌐 **Network device management** �?discover neighbors from Edge hosts, verify with SNMP, poll interfaces, and map host-to-network-device links

## Install

Download the latest release for your server architecture (`linux-amd64` or `linux-arm64`), extract it, and run the installer (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9):

Choose the command for your server architecture:

**AMD64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7/opspilot-v1.0.7-linux-amd64.tar.xz
tar -xf opspilot-v1.0.7-linux-amd64.tar.xz && cd opspilot-v1.0.7-linux-amd64
sudo ./install.sh
```

**ARM64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7/opspilot-v1.0.7-linux-arm64.tar.xz
tar -xf opspilot-v1.0.7-linux-arm64.tar.xz && cd opspilot-v1.0.7-linux-arm64
sudo ./install.sh
```

**🇨🇳 Mainland China** �?if GitHub is slow, use the matching CDN mirror URL instead:

```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7/opspilot-v1.0.7-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7/opspilot-v1.0.7-linux-arm64.tar.xz
```

### Custom Domain & SSL Automatic Renewal

OpsPilot supports binding any custom domain name with native Nginx ACME challenge passthrough for **zero-downtime, automated SSL renewal**.

#### 1. DNS Resolution
Add an **A Record** pointing your domain (e.g. `opspilot.example.com`) to the target server's public IP address.

#### 2. Configure Environment (`.env`)
Edit `/opt/opspilot/.env` with your domain:
```bash
# Public canonical base URL (Edge agents use this to report metrics and logs)
OPSPILOT_PUBLIC_URL=https://opspilot.example.com

# Admin notification email (optional)
OPSPILOT_ADMIN_EMAIL=admin@opspilot.example.com
```

#### 3. Issue SSL Certificate & Configure Renewal
Issue a free Let's Encrypt certificate with Certbot webroot mode:

```bash
# 1. Install certbot (Ubuntu / Debian)
sudo apt update && sudo apt install -y certbot

# 2. Request certificate via pre-wired webroot challenge path
sudo mkdir -p /var/www/certbot
sudo certbot certonly --webroot -w /var/www/certbot -d opspilot.example.com --non-interactive --agree-tos --email admin@example.com

# 3. Copy certificates to OpsPilot mount directory
sudo mkdir -p /opt/opspilot/certs
sudo cp -L /etc/letsencrypt/live/opspilot.example.com/fullchain.pem /opt/opspilot/certs/tls.crt
sudo cp -L /etc/letsencrypt/live/opspilot.example.com/privkey.pem /opt/opspilot/certs/tls.key
sudo chmod 644 /opt/opspilot/certs/tls.crt && sudo chmod 600 /opt/opspilot/certs/tls.key

# 4. Set up auto-deploy reload hook
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

#### 4. Restart Nginx
```bash
cd /opt/opspilot
sudo docker compose restart nginx
```
> **Architecture Note**: Nginx routes use wildcard catch-all (`server_name _;`). Changing your domain requires **no changes to nginx.conf**, only updating `OPSPILOT_PUBLIC_URL` in `.env` and refreshing certificates in `/opt/opspilot/certs/`.

## Product Tour

### Root Cause Analysis

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="Root cause analysis report" width="100%" />
</p>

Start from an operator question or alert, collect topology and device context, and produce an evidence-backed analysis with concrete next steps.

### Workflow Builder

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Workflow builder" width="100%" />
</p>

Wire triggers, agents, tools, conditions, and notifications into repeatable automations that stay editable and reviewable.

### Skills Catalog

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Skills catalog" width="100%" />
</p>

Show the tools agents can call, with clear descriptions and a visible inventory for operators.

### MCP Servers

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="MCP servers" width="100%" />
</p>

Register external MCP servers and expose their tools to chat agents and workflows with clear runtime and trust boundaries.

### Knowledge Vault

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="Knowledge vault" width="100%" />
</p>

Index runbooks, notes, incident history, and repositories so both humans and agents can search the same operational context.

### Artifacts Center

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="Artifacts center" width="100%" />
</p>

Keep generated pages and reports in one place, with private-by-default sharing and a clean handoff for review.

### Monitoring

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Monitoring" width="100%" />
</p>

Inspect fleet health, logs, traces, and alert state from the same workspace where the agent gathers evidence.

### Topology Map

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Topology map" width="100%" />
</p>

Visualize dependencies and blast radius so incidents can be traced through the affected service graph.

### Approval and Write Gate

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Approval and write gate" width="100%" />
</p>

Keep risky actions behind approvals, with a visible policy boundary before anything changes production systems.

## Documentation

The full product documentation is available at [opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction).

| Area | Start here |
|---|---|
| **Get started** | [Introduction](https://opspilot.cloud/docs/get-started/introduction) · [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) · [Architecture](https://opspilot.cloud/docs/get-started/architecture) · [Concepts](https://opspilot.cloud/docs/get-started/concepts) |
| **Install & operate** | [Server install](https://opspilot.cloud/docs/install/server) · [Edge install](https://opspilot.cloud/docs/install/edge) · [First boot](https://opspilot.cloud/docs/install/first-boot) · [Upgrade](https://opspilot.cloud/docs/install/upgrade) |
| **Capabilities** | [Alerts](https://opspilot.cloud/docs/capabilities/alerts) · [RCA](https://opspilot.cloud/docs/capabilities/rca) · [Monitoring](https://opspilot.cloud/docs/capabilities/monitoring) · [Logs](https://opspilot.cloud/docs/capabilities/logs) · [Traces](https://opspilot.cloud/docs/capabilities/traces) · [Knowledge](https://opspilot.cloud/docs/capabilities/knowledge) · [Skills](https://opspilot.cloud/docs/capabilities/skills) |
| **Agents** | [Overview](https://opspilot.cloud/docs/agents/overview) · [Coordinator](https://opspilot.cloud/docs/agents/coordinator) · [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) · [Specialists](https://opspilot.cloud/docs/agents/specialists) · [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **Reference** | [API](https://opspilot.cloud/docs/reference/api) · [CLI](https://opspilot.cloud/docs/reference/cli) · [Alert rules](https://opspilot.cloud/docs/reference/alert-rules) · [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) · [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## Integrations

Drop-in for the observability, channel, and model stacks your team already uses.

| | |
|---|---|
| **Observability** | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Channels** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Models** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## License

AGPLv3 �?see [LICENSE](LICENSE).

OpsPilot brand assets are not licensed under AGPLv3. See
[TRADEMARK.md](TRADEMARK.md).
