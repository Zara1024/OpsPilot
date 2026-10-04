# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **Un agente de IA de ops que entiende tu infraestructura, encuentra la causa ra铆z y la soluciona 鈥?directamente desde Slack o Telegram.**

*M茅tricas 路 registros 路 trazas 路 radio de impacto de topolog铆a 路 correlaci贸n de causa ra铆z 路 ejecuci贸n remota 路 investigaci贸n autom谩tica por alertas 路 b煤squeda RAG en conocimiento y c贸digo 路 agentes especialistas y skills.*

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

[English](./README.md) | [绠�浣撲腑鏂嘳(./README_ZH.md) | [鏃ユ湰瑾瀅(./README_JA.md) | [頃滉淡鞏碷(./README_KO.md) | Espa帽ol | [Fran莽ais](./README_FR.md) | [Deutsch](./README_DE.md) | [Portugu锚s](./README_PT.md) | [袪褍褋褋泻懈泄](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Ciclo de vida de Kubernetes" width="100%" />
  <br />
  <strong>鈽革笍 La gesti贸n del ciclo de vida de Kubernetes ya est谩 disponible</strong><br />
  <sub>Registra cl煤steres mediante Edge, inspecciona cargas y eventos, gestiona actualizaciones y conecta recursos de Kubernetes con la topolog铆a.</sub>
</p>

<div align="center">

[Caracter铆sticas](#caracter铆sticas) 鈥?[Instalaci贸n](#instalaci贸n) 鈥?[Integraciones](#integraciones) 鈥?[Licencia](#licencia)

</div>

## Caracter铆sticas

- 馃 **Agentes Coordinator + Specialist** 鈥?el coordinator delega a sub-agentes SRE / red / DB / activos
- 馃毃 **Auto-investigaci贸n en alerta** 鈥?el investigator lanza un RCA worker y escribe la causa al chat
- 馃攳 **RCA de causa ra铆z** 鈥?recorre la topolog铆a, correlaciona m茅tricas/logs/trazas, llega a una l铆nea de c贸digo
- 馃敀 **Cero puertos entrantes** 鈥?el edge sale al exterior; ning煤n puerto 22 / 80 / 443 en hosts
- 馃捇 **SSH en el navegador** 鈥?shell por t煤nel inverso, sin claves, sin jumpbox, todo auditado
- 馃惓 **Self-host en un comando** 鈥?`install.sh` levanta toda la stack
- 馃搳 **Observabilidad integrada** 鈥?Prometheus + Loki + Tempo + Grafana listos, el agente escribe las queries
- 馃 **Trae tu propio modelo** 鈥?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, enrutamiento en caliente
- 馃挰 **Canales IM bidireccionales** 鈥?Slack / Telegram / Larksuite / DingTalk / WeCom, idioma por canal
- 馃洜锔?**Herramientas de host solo-lectura** 鈥?sandbox bash + 26+ herramientas, cada llamada auditada
- 鈽革笍 **Ciclo de vida de Kubernetes** 鈥?registra cl煤steres, inspecciona cargas y eventos, gestiona actualizaciones y refleja recursos en la topolog铆a
- 馃寪 **Gesti贸n de dispositivos de red** 鈥?descubre vecinos desde hosts Edge, verifica con SNMP, sondea interfaces y mapea enlaces host-dispositivo

## Instalaci贸n

Descarga la 煤ltima release para la arquitectura de tu servidor (`linux-amd64` o `linux-arm64`), descompr铆mela y ejecuta el instalador (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9):

Elige el comando para la arquitectura de tu servidor:

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

**馃嚚馃嚦 China continental** 鈥?si GitHub va lento, usa la URL del mirror CDN que coincida con tu arquitectura:

```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-arm64.tar.xz
```

## Recorrido del producto

### An谩lisis de causa ra铆z

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="An谩lisis de causa ra铆z" width="100%" />
</p>

Empieza desde una alerta o pregunta operativa y re煤ne topolog铆a, dispositivos, m茅tricas, logs y cambios para generar un an谩lisis con evidencias.

### Constructor de workflows

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Constructor de workflows" width="100%" />
</p>

Conecta disparadores, agentes, herramientas, condiciones y notificaciones en automatizaciones reutilizables.

### Cat谩logo de skills

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Cat谩logo de skills" width="100%" />
</p>

Muestra las herramientas que los agentes pueden llamar, con descripciones y l铆mites claros.

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

Guarda p谩ginas e informes generados en un centro privado por defecto para revisi贸n y traspaso.

### Monitorizaci贸n

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Monitorizaci贸n" width="100%" />
</p>

Inspecciona salud de flota, logs, trazas y alertas en el mismo espacio de trabajo.

### Mapa de topolog铆a

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Mapa de topolog铆a" width="100%" />
</p>

Visualiza dependencias y radio de impacto para seguir la propagaci贸n de incidentes.

### Aprobaci贸n y puerta de escritura

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Aprobaci贸n y puerta de escritura" width="100%" />
</p>

Mant茅n las acciones riesgosas detr谩s de aprobaciones y pol铆ticas visibles.

## Documentaci贸n

La documentaci贸n completa est谩 disponible en [opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction).

| 脕rea | Empieza aqu铆 |
|---|---|
| **Primeros pasos** | [Introduction](https://opspilot.cloud/docs/get-started/introduction) 路 [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) 路 [Architecture](https://opspilot.cloud/docs/get-started/architecture) 路 [Concepts](https://opspilot.cloud/docs/get-started/concepts) |
| **Instalaci贸n y operaci贸n** | [Server install](https://opspilot.cloud/docs/install/server) 路 [Edge install](https://opspilot.cloud/docs/install/edge) 路 [First boot](https://opspilot.cloud/docs/install/first-boot) 路 [Upgrade](https://opspilot.cloud/docs/install/upgrade) |
| **Capacidades** | [Alerts](https://opspilot.cloud/docs/capabilities/alerts) 路 [RCA](https://opspilot.cloud/docs/capabilities/rca) 路 [Monitoring](https://opspilot.cloud/docs/capabilities/monitoring) 路 [Logs](https://opspilot.cloud/docs/capabilities/logs) 路 [Traces](https://opspilot.cloud/docs/capabilities/traces) 路 [Knowledge](https://opspilot.cloud/docs/capabilities/knowledge) 路 [Skills](https://opspilot.cloud/docs/capabilities/skills) |
| **Agentes** | [Overview](https://opspilot.cloud/docs/agents/overview) 路 [Coordinator](https://opspilot.cloud/docs/agents/coordinator) 路 [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) 路 [Specialists](https://opspilot.cloud/docs/agents/specialists) 路 [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **Referencia** | [API](https://opspilot.cloud/docs/reference/api) 路 [CLI](https://opspilot.cloud/docs/reference/cli) 路 [Alert rules](https://opspilot.cloud/docs/reference/alert-rules) 路 [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) 路 [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## Integraciones

Se integra con los stacks de observabilidad, canales y modelos que tu equipo ya usa.

| | |
|---|---|
| **Observabilidad** | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Canales** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Modelos** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## Licencia

AGPLv3 鈥?ver [LICENSE](LICENSE).
