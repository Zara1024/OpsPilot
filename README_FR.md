# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **Un agent IA d'ops qui comprend votre infrastructure, trouve la cause racine et la corrige �?directement depuis Slack ou Telegram.**

*Métriques · logs · traces · rayon d'impact de topologie · corrélation de cause racine · exécution à distance · investigation automatique déclenchée par alerte · recherche RAG dans les connaissances et le code · agents spécialisés et compétences.*

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

[English](./README.md) | [简体中文](./README_ZH.md) | [日本語](./README_JA.md) | [한국어](./README_KO.md) | [Español](./README_ES.md) | Français | [Deutsch](./README_DE.md) | [Português](./README_PT.md) | [Русский](./README_RU.md)

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Cycle de vie Kubernetes" width="100%" />
  <br />
  <strong>☸️ La gestion du cycle de vie Kubernetes est disponible</strong><br />
  <sub>Enregistrez des clusters via Edge, inspectez charges et événements, gérez les mises à niveau et reliez les ressources Kubernetes à la topologie.</sub>
</p>

<div align="center">

[Fonctionnalités](#fonctionnalités) �?[Installation](#installation) �?[Intégrations](#intégrations) �?[Licence](#licence)

</div>

## Fonctionnalités

- 🤖 **Agents Coordinator + Specialist** �?le coordinator délègue aux sous-agents SRE / réseau / DB / actifs
- 🚨 **Auto-investigation sur alerte** �?l’investigator lance un RCA worker et écrit la cause au chat
- 🔍 **RCA cause racine** �?parcourt la topologie, corrèle métriques/logs/traces, identifie une ligne de code
- 🔒 **Zéro port entrant** �?l’edge sort vers l’extérieur ; aucun port 22 / 80 / 443 sur l’hôte
- 💻 **SSH dans le navigateur** �?shell par tunnel inverse, pas de clé, pas de jumpbox, tout audité
- 🐳 **Auto-hébergeable en une commande** �?`install.sh` lance toute la stack
- 📊 **Observabilité intégrée** �?Prometheus + Loki + Tempo + Grafana prêts, l’agent écrit les requêtes
- 🧠 **Apportez votre modèle** �?Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, routage à chaud
- 💬 **Canaux IM bidirectionnels** �?Slack / Telegram / Larksuite / DingTalk / WeCom, langue par canal
- 🛠�?**Outils host en lecture seule** �?sandbox bash + 26+ outils, chaque appel audité
- ☸️ **Cycle de vie Kubernetes** �?enregistrez les clusters, inspectez charges et événements, gérez les mises à niveau et reflétez les ressources dans la topologie
- 🌐 **Gestion des équipements réseau** �?découvrez les voisins depuis les hôtes Edge, validez-les par SNMP, sondez les interfaces et cartographiez les liens hôte-équipement

## Installation

Téléchargez la dernière release adaptée à l’architecture de votre serveur (`linux-amd64` ou `linux-arm64`), décompressez-la et exécutez le script d’installation (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9) :

Choisissez la commande adaptée à l’architecture de votre serveur :

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

**🇨🇳 Chine continentale** �?si GitHub est lent, utilisez l’URL du miroir CDN correspondant à votre architecture :

```bash
# AMD64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-amd64.tar.xz

# ARM64
wget https://mirror.ghproxy.com/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.5/opspilot-v1.0.5-linux-arm64.tar.xz
```

## Tour du produit

### Analyse de cause racine

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="Analyse de cause racine" width="100%" />
</p>

Partez d’une alerte ou d’une question d’exploitation, puis rassemblez topologie, hôtes, métriques, logs et changements pour produire une analyse étayée.

### Constructeur de workflows

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Constructeur de workflows" width="100%" />
</p>

Assemblez déclencheurs, agents, outils, conditions et notifications dans des automatisations réutilisables.

### Catalogue de skills

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Catalogue de skills" width="100%" />
</p>

Affichez les outils que les agents peuvent appeler, avec descriptions et limites claires.

### Serveurs MCP

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="Serveurs MCP" width="100%" />
</p>

Enregistrez des serveurs MCP externes et exposez leurs outils dans le même inventaire gouverné.

### Base de connaissances

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="Base de connaissances" width="100%" />
</p>

Indexez runbooks, historiques d’incidents, notes d’architecture et dépôts comme contexte consultable.

### Centre des artefacts

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="Centre des artefacts" width="100%" />
</p>

Conservez pages et rapports générés dans un centre privé par défaut pour la revue et le transfert.

### Surveillance

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Surveillance" width="100%" />
</p>

Inspectez santé de flotte, logs, traces et alertes dans le même espace de travail.

### Carte de topologie

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Carte de topologie" width="100%" />
</p>

Visualisez dépendances et rayon d’impact pour suivre la propagation des incidents.

### Approbation et garde-fou d’écriture

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Approbation et garde-fou d’écriture" width="100%" />
</p>

Gardez les actions risquées derrière des approbations et des politiques visibles.

## Documentation

La documentation complète est disponible sur [opspilot.cloud](https://opspilot.cloud/docs/get-started/introduction).

| Domaine | Commencer ici |
|---|---|
| **Bien démarrer** | [Introduction](https://opspilot.cloud/docs/get-started/introduction) · [Quickstart](https://opspilot.cloud/docs/get-started/quickstart) · [Architecture](https://opspilot.cloud/docs/get-started/architecture) · [Concepts](https://opspilot.cloud/docs/get-started/concepts) |
| **Installation et exploitation** | [Server install](https://opspilot.cloud/docs/install/server) · [Edge install](https://opspilot.cloud/docs/install/edge) · [First boot](https://opspilot.cloud/docs/install/first-boot) · [Upgrade](https://opspilot.cloud/docs/install/upgrade) |
| **Capacités** | [Alerts](https://opspilot.cloud/docs/capabilities/alerts) · [RCA](https://opspilot.cloud/docs/capabilities/rca) · [Monitoring](https://opspilot.cloud/docs/capabilities/monitoring) · [Logs](https://opspilot.cloud/docs/capabilities/logs) · [Traces](https://opspilot.cloud/docs/capabilities/traces) · [Knowledge](https://opspilot.cloud/docs/capabilities/knowledge) · [Skills](https://opspilot.cloud/docs/capabilities/skills) |
| **Agents** | [Overview](https://opspilot.cloud/docs/agents/overview) · [Coordinator](https://opspilot.cloud/docs/agents/coordinator) · [Incident investigator](https://opspilot.cloud/docs/agents/incident-investigator) · [Specialists](https://opspilot.cloud/docs/agents/specialists) · [Reviewer](https://opspilot.cloud/docs/agents/reviewer) |
| **Référence** | [API](https://opspilot.cloud/docs/reference/api) · [CLI](https://opspilot.cloud/docs/reference/cli) · [Alert rules](https://opspilot.cloud/docs/reference/alert-rules) · [Skill manifest](https://opspilot.cloud/docs/reference/skill-manifest) · [Data plane](https://opspilot.cloud/docs/reference/data-plane) |

## Intégrations

S’intègre aux stacks d’observabilité, de canaux et de modèles déjà en place.

| | |
|---|---|
| **Observabilité** | <img src="https://api.iconify.design/logos:prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:qdrant-icon.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Canaux** | <img src="https://api.iconify.design/logos:slack-icon.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.simpleicons.org/wechat" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://api.iconify.design/logos:webhooks.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Modèles** | <img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/claude-color.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/gemini-color.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/deepseek-color.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@latest/icons/kimi-color.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## Licence

AGPLv3 �?voir [LICENSE](LICENSE).
