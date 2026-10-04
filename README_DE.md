# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **Ein Ops-KI-Agent, der deine Infrastruktur versteht, die Ursache findet und sie behebt 鈥?direkt aus Slack oder Telegram.**

*Metriken 路 Logs 路 Traces 路 Topologie-Auswirkungsbereich 路 Ursachenkorrelation 路 Remote-Ausf眉hrung 路 alarmgesteuerte Auto-Untersuchung 路 RAG-Suche 眉ber Wissen und Code 路 Spezialisten-Agenten und Skills.*

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

[English](./README.md) | [绠�浣撲腑鏂嘳(./README_ZH.md) | [鏃ユ湰瑾瀅(./README_JA.md) | [頃滉淡鞏碷(./README_KO.md) | [Espa帽ol](./README_ES.md) | [Fran莽ais](./README_FR.md) | Deutsch | [Portugu锚s](./README_PT.md) | [袪褍褋褋泻懈泄](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Kubernetes-Lebenszyklus" width="100%" />
  <br />
  <strong>鈽革笍 Kubernetes-Lebenszyklusverwaltung ist jetzt verf眉gbar</strong><br />
  <sub>Registrieren Sie Cluster 眉ber Edge, pr眉fen Sie Workloads und Ereignisse, verwalten Sie Upgrades und verbinden Sie Kubernetes-Ressourcen mit der Topologie.</sub>
</p>

<div align="center">

[Funktionen](#funktionen) 鈥?[Installation](#installation) 鈥?[Integrationen](#integrationen) 鈥?[Lizenz](#lizenz)

</div>

## Funktionen

- 馃 **Coordinator + Specialist Agenten** 鈥?der Coordinator delegiert an SRE / Netzwerk / DB / Asset Sub-Agenten
- 馃毃 **Auto-Investigation bei Alarm** 鈥?der Investigator startet einen RCA-Worker, schreibt die Ursache in den Chat
- 馃攳 **Grundursachen-RCA** 鈥?durchl盲uft die Topologie, korreliert Metriken/Logs/Traces, identifiziert eine Quellcode-Zeile
- 馃敀 **Null eingehende Ports** 鈥?der Edge w盲hlt nach au脽en; kein Port 22 / 80 / 443 auf Hosts
- 馃捇 **SSH im Browser** 鈥?Shell 眉ber R眉ckw盲rtstunnel, keine Schl眉ssel, kein Jumpbox, alles auditiert
- 馃惓 **Selbst-Hosting in einem Befehl** 鈥?`install.sh` startet die gesamte Stack
- 馃搳 **Eingebaute Observability** 鈥?Prometheus + Loki + Tempo + Grafana bereit, der Agent schreibt die Queries
- 馃 **Eigenes Modell mitbringen** 鈥?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, Hot-Routing
- 馃挰 **Zweiwege-IM-Kan盲le** 鈥?Slack / Telegram / Larksuite / DingTalk / WeCom, Sprache pro Kanal
- 馃洜锔?**Schreibgesch眉tzte Host-Tools** 鈥?bash Sandbox + 26+ Tools, jeder Aufruf auditiert
- 鈽革笍 **Kubernetes-Lebenszyklus** 鈥?Cluster registrieren, Workloads und Events pr眉fen, Upgrades verwalten und Kubernetes-Ressourcen in die Topologie spiegeln
- 馃寪 **Netzwerkger盲teverwaltung** 鈥?Nachbarn 眉ber Edge-Hosts erkennen, per SNMP pr眉fen, Schnittstellen abfragen und Host-Ger盲te-Verbindungen abbilden

## Installation

Laden Sie das aktuelle Release f眉r Ihre Serverarchitektur (`linux-amd64` oder `linux-arm64`) herunter, entpacken Sie es und f眉hren Sie das Installationsskript aus (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9):

W盲hlen Sie den Befehl f眉r Ihre Serverarchitektur:

**AMD64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-amd64.tar.xz
tar -xf opspilot-v1.0.5-linux-amd64.tar.xz && cd opspilot-v1.0.5-linux-amd64
sudo ./install.sh
```

**ARM64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-arm64.tar.xz
tar -xf opspilot-v1.0.5-linux-arm64.tar.xz && cd opspilot-v1.0.5-linux-arm64
sudo ./install.sh
```

**馃嚚馃嚦 Festlandchina** 鈥?Wenn GitHub langsam ist, verwenden Sie die passende CDN-Mirror-URL f眉r Ihre Architektur:

```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-arm64.tar.xz
```

## Produkttour

### Root-Cause-Analyse

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="Root-Cause-Analyse" width="100%" />
</p>

Starte mit einem Alert oder einer Betriebsfrage und sammle Topologie, Ger盲te, Metriken, Logs und 脛nderungen f眉r eine belegbare Analyse.

### Workflow Builder

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Workflow Builder" width="100%" />
</p>

Verbinde Trigger, Agents, Tools, Bedingungen und Benachrichtigungen zu wiederverwendbaren Automationen.

### Skill-Katalog

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Skill-Katalog" width="100%" />
</p>

Zeige die Tools, die Agents aufrufen k枚nnen, inklusive Beschreibung und Grenzen.

### MCP-Server

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="MCP-Server" width="100%" />
</p>

Registriere externe MCP-Server und binde ihre Tools in dasselbe gesteuerte Inventar ein.

### Knowledge Vault

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="Knowledge Vault" width="100%" />
</p>

Indexiere Runbooks, Incident-Historie, Architekturhinweise und Repositories als durchsuchbaren Kontext.

### Artefakt-Zentrale

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="Artefakt-Zentrale" width="100%" />
</p>

Speichere erzeugte Seiten und Berichte zentral, standardm盲脽ig privat und gut pr眉fbar.

### Monitoring

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Monitoring" width="100%" />
</p>

Pr眉fe Flottenzustand, Logs, Traces und Alerts im selben Workspace.

### Topologie-Karte

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Topologie-Karte" width="100%" />
</p>

Visualisiere Abh盲ngigkeiten und Blast Radius, um Incident-Auswirkungen nachzuvollziehen.

### Freigabe- und Schreib-Gate

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Freigabe- und Schreib-Gate" width="100%" />
</p>

Halte riskante Aktionen hinter Freigaben und sichtbaren Policy-Grenzen.

## Dokumentation

Die vollst盲ndige Produktdokumentation ist auf [opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction) verf眉gbar.

| Bereich | Einstieg |
|---|---|
| **Erste Schritte** | [Introduction](https://opspilot.cloud/docs/get-started/introduction) 路 [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) 路 [Architecture](https://opspilot.cloud/docs/get-started/architecture) 路 [Concepts](https://opspilot.cloud/docs/get-started/concepts) |
| **Installation und Betrieb** | [Server install](https://opspilot.cloud/docs/install/server) 路 [Edge install](https://opspilot.cloud/docs/install/edge) 路 [First boot](https://opspilot.cloud/docs/install/first-boot) 路 [Upgrade](https://opspilot.cloud/docs/install/upgrade) |
| **Funktionen** | [Alerts](https://opspilot.cloud/docs/capabilities/alerts) 路 [RCA](https://opspilot.cloud/docs/capabilities/rca) 路 [Monitoring](https://opspilot.cloud/docs/capabilities/monitoring) 路 [Logs](https://opspilot.cloud/docs/capabilities/logs) 路 [Traces](https://opspilot.cloud/docs/capabilities/traces) 路 [Knowledge](https://opspilot.cloud/docs/capabilities/knowledge) 路 [Skills](https://opspilot.cloud/docs/capabilities/skills) |
| **Agents** | [Overview](https://opspilot.cloud/docs/agents/overview) 路 [Coordinator](https://opspilot.cloud/docs/agents/coordinator) 路 [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) 路 [Specialists](https://opspilot.cloud/docs/agents/specialists) 路 [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **Referenz** | [API](https://opspilot.cloud/docs/reference/api) 路 [CLI](https://opspilot.cloud/docs/reference/cli) 路 [Alert rules](https://opspilot.cloud/docs/reference/alert-rules) 路 [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) 路 [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## Integrationen

Drop-in f眉r die Observability-, Channel- und Modell-Stacks, die Ihr Team bereits nutzt.

| | |
|---|---|
| **Observability** | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Kan盲le** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Modelle** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## Lizenz

AGPLv3 鈥?siehe [LICENSE](LICENSE).
