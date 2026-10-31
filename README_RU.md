# <img src="web/public/opspilot-logo.svg" alt="" width="40" align="absmiddle" style="vertical-align: middle;" /> OpsPilot

> **ИИ-агент для эксплуатации, который понимает вашу инфраструктуру, находит первопричину и устраняет её — прямо из Slack или Telegram.**

*Метрики · логи · трейсы · радиус влияния топологии · корреляция первопричин · удалённое выполнение · автоматическое расследование по алертам · RAG-поиск по знаниям и коду · специализированные агенты и навыки.*

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

[English](./README_EN.md) | [简体中文](./README.md) | [日本語](./README_JA.md) | [한국어](./README_KO.md) | [Español](./README_ES.md) | [Français](./README_FR.md) | [Deutsch](./README_DE.md) | [Português](./README_PT.md) | Русский

---

<p align="center">
  <img src="docs/assets/demo.gif" alt="OpsPilot demo" width="100%" />
</p>

<p align="center">
  <img src="docs/assets/kubernetes-release-banner.png" alt="Управление жизненным циклом Kubernetes" width="100%" />
  <br />
  <strong>☸️ Управление жизненным циклом Kubernetes уже доступно</strong><br />
  <sub>Подключайте кластеры через Edge, проверяйте нагрузки и события, управляйте обновлениями и связывайте ресурсы Kubernetes с топологией.</sub>
</p>

<div align="center">

[Возможности](#возможности) • [Установка](#установка) • [Интеграции](#интеграции) • [Лицензия](#лицензия)

</div>

## Возможности

- 🤖 **Агенты Coordinator + Specialist** — coordinator делегирует суб-агентам SRE / сеть / БД / активы
- 🚨 **Авто-исследование по алерту** — investigator запускает RCA-worker и пишет причину в чат
- 🔍 **Корневая RCA** — обходит топологию, коррелирует метрики/логи/трейсы, до строки исходного кода
- 🔒 **Ноль входящих портов** — edge выходит наружу; нет порта 22 / 80 / 443 на хосте
- 💻 **SSH в браузере** — оболочка через обратный туннель, без ключей, без jumpbox, в аудите
- 🐳 **Self-host одной командой** — `install.sh` поднимает весь стек
- 📊 **Встроенная observability** — Prometheus + Loki + Tempo + Grafana готовы, агент пишет запросы
- 🧠 **Принесите свою модель** — Anthropic / OpenAI / GLM / DeepSeek / Gemini / Kimi, горячая маршрутизация
- 💬 **Двусторонние IM-каналы** — Slack / Telegram / Larksuite / DingTalk / WeCom, локаль на канал
- 🛠️ **Read-only host-инструменты** — sandbox bash + 26+ инструментов, каждый вызов в аудите
- ☸️ **Жизненный цикл Kubernetes** — регистрируйте кластеры, проверяйте нагрузки и события, управляйте обновлениями и отражайте ресурсы в топологии
- 🌐 **Управление сетевыми устройствами** — обнаруживайте соседей с Edge-хостов, проверяйте через SNMP, опрашивайте интерфейсы и отображайте связи хост-устройство

## Установка

Скачайте последний релиз для архитектуры вашего сервера (`linux-amd64` или `linux-arm64`), распакуйте и запустите скрипт установки (Ubuntu 22.04+, Debian 12+, RHEL/Rocky 9):

Выберите команду для архитектуры вашего сервера:

**AMD64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.1.4/opspilot-v1.1.4-linux-amd64.tar.xz
tar -xf opspilot-v1.1.4-linux-amd64.tar.xz && cd opspilot-v1.1.4-linux-amd64
sudo ./install.sh
```

**ARM64**
```bash
wget https://github.com/Zara1024/OpsPilot/releases/download/v1.1.4/opspilot-v1.1.4-linux-arm64.tar.xz
tar -xf opspilot-v1.1.4-linux-arm64.tar.xz && cd opspilot-v1.1.4-linux-arm64
sudo ./install.sh
```

**🇨🇳 Материковый Китай** — если GitHub медленный, используйте URL CDN-зеркала для вашей архитектуры:

```bash
# AMD64
wget https://opspilot.cloud/dl/opspilot-v0.18.2-linux-amd64.tar.xz

# ARM64
wget https://opspilot.cloud/dl/opspilot-v0.18.2-linux-arm64.tar.xz
```

## Обзор продукта

### Анализ первопричины

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-agent-write-gate.png" alt="Анализ первопричины" width="100%" />
</p>

Начните с алерта или вопроса оператора и соберите топологию, устройства, метрики, логи и изменения для анализа с доказательствами.

### Конструктор workflow

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-workflow-editor.png" alt="Конструктор workflow" width="100%" />
</p>

Связывайте триггеры, агентов, инструменты, условия и уведомления в повторяемые автоматизации.

### Каталог skills

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-skills-catalog.png" alt="Каталог skills" width="100%" />
</p>

Показывайте инструменты, которые могут вызывать агенты, с описаниями и границами.

### MCP-серверы

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-mcp-servers.png" alt="MCP-серверы" width="100%" />
</p>

Регистрируйте внешние MCP-серверы и подключайте их инструменты к единому управляемому инвентарю.

### База знаний

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-knowledge-vault.png" alt="База знаний" width="100%" />
</p>

Индексируйте runbook-и, историю инцидентов, архитектурные заметки и репозитории как поисковый контекст.

### Центр артефактов

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-artifacts-pages.png" alt="Центр артефактов" width="100%" />
</p>

Храните созданные страницы и отчеты в одном центре, приватном по умолчанию.

### Мониторинг

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-monitor.png" alt="Мониторинг" width="100%" />
</p>

Проверяйте состояние флота, логи, трейсы и алерты в одном рабочем пространстве.

### Карта топологии

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-topology-map.png" alt="Карта топологии" width="100%" />
</p>

Визуализируйте зависимости и blast radius, чтобы отслеживать влияние инцидента.

### Согласование и write gate

<p align="center">
  <img src="docs/assets/readme-tour/user-20260707-rca-session.png" alt="Согласование и write gate" width="100%" />
</p>

Держите рискованные действия за согласованиями и видимыми политиками.

## Интеграции

Подключается к стекам observability, каналов и моделей, которые ваша команда уже использует.

| | |
|---|---|
| **Observability** | <img src="docs/assets/integrations/prometheus.svg" alt="Prometheus" title="Prometheus" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/grafana.svg" alt="Grafana" title="Grafana" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/loki.svg" alt="Loki" title="Loki" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/tempo.svg" alt="Tempo" title="Tempo" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/opentelemetry.svg" alt="OpenTelemetry" title="OpenTelemetry" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/qdrant.svg" alt="Qdrant" title="Qdrant" width="28" height="28" /> |
| **Каналы** | <img src="docs/assets/integrations/slack.svg" alt="Slack" title="Slack" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/telegram.svg" alt="Telegram" title="Telegram" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/larksuite.svg" alt="Larksuite" title="Larksuite" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/dingtalk.svg" alt="DingTalk" title="DingTalk" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/wecom.svg" alt="WeCom" title="WeCom" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/webhook.svg" alt="Webhook" title="Webhook" width="28" height="28" /> |
| **Модели** | <img src="docs/assets/integrations/anthropic.svg" alt="Anthropic" title="Anthropic" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/openai.svg" alt="OpenAI" title="OpenAI" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/gemini.svg" alt="Gemini" title="Gemini" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/deepseek.svg" alt="DeepSeek" title="DeepSeek" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/zhipu.svg" alt="Zhipu" title="Zhipu" width="28" height="28" />&nbsp;&nbsp;&nbsp;<img src="docs/assets/integrations/kimi.svg" alt="Kimi" title="Kimi" width="28" height="28" /> |

## Лицензия

AGPLv3 — см. [LICENSE](LICENSE).
