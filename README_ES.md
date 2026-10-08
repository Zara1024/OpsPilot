# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **Un agente de IA de ops que entiende tu infraestructura, encuentra la causa raíz y la soluciona �?directamente desde Slack o Telegram.**

*Métricas · registros · trazas · radio de impacto de topología · correlación de causa raíz · ejecución remota · investigación automática por alertas · búsqueda RAG en conocimiento y código · agentes especialistas y skills.*

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

[English](./README.md) | [简体中文](./README_ZH.md) | [日本語](./README_JA.md) | [한국어](./README_KO.md) | Español | [Français](./README_FR.md) | [Deutsch](./README_DE.md) | [Português](./README_PT.md) | [Русский](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Ciclo de vida de Kubernetes" width="100%" />
  <br />
  <strong>☸️ La gestión del ciclo de vida de Kubernetes ya está disponible</strong><br />
  <sub>Registra clústeres mediante Edge, inspecciona cargas y eventos, gestiona actualizaciones y conecta recursos de Kubernetes con la topología.</sub>
</p>

<div align="center">

[Características](#características) �?[Instalación](#instalación) �?[Integraciones](#integraciones) �?[Licencia](#licencia)

</div>

## Características

- 🤖 **Agentes Coordinator + Specialist** �?el coordinator delega a sub-agentes SRE / red / DB / activos
- 🚨 **Auto-investigación en alerta** �?el investigator lanza un RCA worker y escribe la causa al chat
- 🔍 **RCA de causa raíz** �?recorre la topología, correlaciona métricas/logs/trazas, llega a una línea de código
- 🔒 **Cero puertos entrantes** �?el edge sale al exterior; ningún puerto 22 / 80 / 443 en hosts
- 💻 **SSH en el navegador** �?shell por túnel inverso, sin claves, sin jumpbox, todo auditado
- 🐳 **Self-host en un comando** �?`install.sh` levanta toda la stack
- 📊 **Observabilidad integrada** �?Prometheus + Loki + Tempo + Grafana listos, el agente escribe las queries
- 🧠 **Trae tu propio modelo** �?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, enrutamiento en caliente
- 💬 **Canales IM bidireccionales** �?Slack / Telegram / Larksuite / DingTalk / WeCom, idioma por canal
- 🛠�?**Herramientas de host solo-lectura** �?sandbox bash + 26+ herramientas, cada llamada auditada
- ☸️ **Ciclo de vida de Kubernetes** �?registra clústeres, inspecciona cargas y eventos, gestiona actualizaciones y refleja recursos en la topología
- 🌐 **Gestión de dispositivos de red** �?descubre vecinos desde hosts Edge, verifica con SNMP, sondea interfaces y mapea enlaces host-dispositivo

## Instalación

Descarga la última release para la arquitectura de tu servidor (`linux-amd64` o `linux-arm64`), descomprímela y ejecuta el instalador (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9):

Elige el comando para la arquitectura de tu servidor:

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

**🇨🇳 China continental** �?si GitHub va lento, usa la URL del mirror CDN que coincida con tu arquitectura:

```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.1.0/opspilot-v1.1.0-linux-arm64.tar.xz
```

## Recorrido del producto

### Análisis de causa raíz

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="Análisis de causa raíz" width="100%" />
</p>

Empieza desde una alerta o pregunta operativa y reúne topología, dispositivos, métricas, logs y cambios para generar un análisis con evidencias.

### Constructor de workflows

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Constructor de workflows" width="100%" />
</p>

Conecta disparadores, agentes, herramientas, condiciones y notificaciones en automatizaciones reutilizables.

### Catálogo de skills

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Catálogo de skills" width="100%" />
</p>

Muestra las herramientas que los agentes pueden llamar, con descripciones y límites claros.

### Servidores MCP

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="Servidores MCP" width="100%" />
</p>

Registra servidores MCP externos y conecta sus herramientas al mismo inventario gobernado.

### Base de conocimiento

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="Base de conocimiento" width="100%" />
</p>

Indexa runbooks, historial de incidentes, notas de arquitectura y repositorios como contexto buscable.

### Centro de artefactos

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="Centro de artefactos" width="100%" />
</p>

Guarda páginas e informes generados en un centro privado por defecto para revisión y traspaso.

### Monitorización

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Monitorización" width="100%" />
</p>

Inspecciona salud de flota, logs, trazas y alertas en el mismo espacio de trabajo.

### Mapa de topología

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Mapa de topología" width="100%" />
</p>

Visualiza dependencias y radio de impacto para seguir la propagación de incidentes.

### Aprobación y puerta de escritura

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Aprobación y puerta de escritura" width="100%" />
</p>

Mantén las acciones riesgosas detrás de aprobaciones y políticas visibles.

## Documentación

La documentación completa está disponible en [opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction).

| Área | Empieza aquí |
|---|---|
| **Primeros pasos** | [Introduction](https://opspilot.cloud/docs/get-started/introduction) · [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) · [Architecture](https://opspilot.cloud/docs/get-started/architecture) · [Concepts](https://opspilot.cloud/docs/get-started/concepts) |
| **Instalación y operación** | [Server install](https://opspilot.cloud/docs/install/server) · [Edge install](https://opspilot.cloud/docs/install/edge) · [First boot](https://opspilot.cloud/docs/install/first-boot) · [Upgrade](https://opspilot.cloud/docs/install/upgrade) |
| **Capacidades** | [Alerts](https://opspilot.cloud/docs/capabilities/alerts) · [RCA](https://opspilot.cloud/docs/capabilities/rca) · [Monitoring](https://opspilot.cloud/docs/capabilities/monitoring) · [Logs](https://opspilot.cloud/docs/capabilities/logs) · [Traces](https://opspilot.cloud/docs/capabilities/traces) · [Knowledge](https://opspilot.cloud/docs/capabilities/knowledge) · [Skills](https://opspilot.cloud/docs/capabilities/skills) |
| **Agentes** | [Overview](https://opspilot.cloud/docs/agents/overview) · [Coordinator](https://opspilot.cloud/docs/agents/coordinator) · [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) · [Specialists](https://opspilot.cloud/docs/agents/specialists) · [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **Referencia** | [API](https://opspilot.cloud/docs/reference/api) · [CLI](https://opspilot.cloud/docs/reference/cli) · [Alert rules](https://opspilot.cloud/docs/reference/alert-rules) · [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) · [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## Integraciones

Se integra con los stacks de observabilidad, canales y modelos que tu equipo ya usa.

| | |
|---|---|
| **Observabilidad** | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Canales** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Modelos** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## Licencia

AGPLv3 �?ver [LICENSE](LICENSE).
