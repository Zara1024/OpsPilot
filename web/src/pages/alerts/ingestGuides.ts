import type { IngestProvider } from '@/api/alerts';

export type ProviderMeta = {
  key: IngestProvider;
  labelZh: string;
  labelEn: string;
  hintZh: string;
  hintEn: string;
};

export const INGEST_PROVIDERS: ProviderMeta[] = [
  {
    key: 'alertmanager',
    labelZh: 'Prometheus Alertmanager',
    labelEn: 'Prometheus Alertmanager',
    hintZh: '支持标准 Webhook 批量告警、fingerprint 去重与自动恢复关单',
    hintEn: 'Standard batch alerts, fingerprint deduplication, and auto-resolve.',
  },
  {
    key: 'grafana',
    labelZh: 'Grafana Alerting',
    labelEn: 'Grafana Alerting',
    hintZh: '兼容 Unified Alerting Webhook 与旧版 Legacy 告警格式',
    hintEn: 'Supports Unified Alerting and legacy Grafana webhook payloads.',
  },
  {
    key: 'aliyun_cms',
    labelZh: '阿里云云监控 CMS',
    labelEn: 'Aliyun CloudMonitor',
    hintZh: '同时支持经典表单 Form URL-Encoded 回调与 EventBridge JSON',
    hintEn: 'Supports classic form-urlencoded callbacks and JSON payloads.',
  },
  {
    key: 'tencent_cm',
    labelZh: '腾讯云云监控',
    labelEn: 'Tencent Cloud Monitor',
    hintZh: '解析 alarmStatus (1 触发 / 0 恢复) 与策略维度标签',
    hintEn: 'Parses alarmStatus (1 firing / 0 ok) and policy dimensions.',
  },
  {
    key: 'huawei_ces',
    labelZh: '华为云云监控 CES / SMN',
    labelEn: 'Huawei Cloud CES / SMN',
    hintZh: '支持 SMN 订阅确认提示与 Notification 消息解包，以及直连 CES JSON',
    hintEn: 'Handles SMN subscription confirmation, Notification unwrap, and direct CES JSON.',
  },
  {
    key: 'zabbix',
    labelZh: 'Zabbix 5/6/7 Webhook',
    labelEn: 'Zabbix 5/6/7 Webhook',
    hintZh: '支持 0~5 级别映射、Trigger+Host 精确恢复关联与宏脚本',
    hintEn: 'Maps severity 0-5, correlates PROBLEM/OK by trigger and host.',
  },
  {
    key: 'generic_webhook',
    labelZh: '通用 JSON Webhook',
    labelEn: 'Generic JSON Webhook',
    hintZh: '兼容标准 JSON 与 PagerDuty Events v2 格式，适合自定义脚本接入',
    hintEn: 'Compatible with simple JSON or PagerDuty Events v2 trigger/resolve.',
  },
];

export function resolveFullEndpointUrl(rawPathOrUrl: string): string {
  if (!rawPathOrUrl) return '';
  if (/^https?:\/\//i.test(rawPathOrUrl)) {
    return rawPathOrUrl;
  }
  const origin = typeof window !== 'undefined' && window.location?.origin ? window.location.origin : '';
  const path = rawPathOrUrl.startsWith('/') ? rawPathOrUrl : `/${rawPathOrUrl}`;
  return `${origin}${path}`;
}

export function buildProviderSnippet(
  provider: IngestProvider,
  webhookUrl: string,
  lang: 'zh' | 'en',
): { title: string; code: string; note?: string } {
  const fullUrl = resolveFullEndpointUrl(webhookUrl);
  const isZh = lang === 'zh';

  switch (provider) {
    case 'alertmanager':
      return {
        title: isZh ? 'alertmanager.yml 接收器配置' : 'alertmanager.yml Receiver Config',
        code: `receivers:
  - name: opspilot
    webhook_configs:
      - url: '${fullUrl}'
        send_resolved: true`,
        note: isZh
          ? '请确保开启 send_resolved: true，Alertmanager 恢复时 OpsPilot 会自动关闭告警并收敛 On-Call 升级。'
          : 'Keep send_resolved: true so recovery events automatically resolve incidents.',
      };
    case 'grafana':
      return {
        title: isZh ? 'Grafana Contact Point (Webhook)' : 'Grafana Contact Point (Webhook)',
        code: `URL: ${fullUrl}
HTTP Method: POST
Disable resolve message: false`,
        note: isZh
          ? '在 Grafana Alerting -> Contact points 中新建 Webhook，将上述地址填入 URL 即可。'
          : 'Create a Webhook Contact Point in Grafana Alerting and paste this URL.',
      };
    case 'aliyun_cms':
      return {
        title: isZh ? '阿里云报警联系组 / 回调 URL' : 'Aliyun CMS Alert Callback URL',
        code: `${fullUrl}`,
        note: isZh
          ? '支持阿里云云监控原生 application/x-www-form-urlencoded HTTP 回调及 JSON 回调，无需额外网关转换。'
          : 'Directly accepts Aliyun CMS form-urlencoded HTTP callbacks and JSON payloads.',
      };
    case 'tencent_cm':
      return {
        title: isZh ? '腾讯云告警策略 Webhook 回调地址' : 'Tencent Cloud Monitor Webhook URL',
        code: `${fullUrl}`,
        note: isZh
          ? '在腾讯云可观测平台通知模板中配置“接口回调”，填入上述地址并确保使用 POST JSON。'
          : 'Configure this URL as the HTTP callback endpoint in Tencent Cloud Monitor notification templates.',
      };
    case 'huawei_ces':
      return {
        title: isZh ? '华为云 SMN 主题 HTTPS 订阅终端' : 'Huawei Cloud SMN Subscription Endpoint',
        code: `${fullUrl}`,
        note: isZh
          ? '首次创建 HTTPS 订阅时，华为云 SMN 会发送 SubscriptionConfirmation 请求；平台会安全捕获 confirm URL，在接入源列表点“查看载荷”或“配置向导”复制后完成订阅确认即可。'
          : 'On first subscription, Huawei SMN sends a SubscriptionConfirmation payload. Inspect it in OpsPilot and confirm in a trusted browser or curl.',
      };
    case 'zabbix':
      return {
        title: isZh ? 'Zabbix Media Type (Webhook JavaScript)' : 'Zabbix Media Type (Webhook JavaScript)',
        code: `var req = new HttpRequest();
req.addHeader('Content-Type: application/json');
var payload = {
  event_id: value.EVENT_ID || '{EVENT.ID}',
  trigger_id: value.TRIGGER_ID || '{TRIGGER.ID}',
  status: value.EVENT_STATUS || '{EVENT.STATUS}',
  severity: value.EVENT_NSEVERITY || '{EVENT.NSEVERITY}',
  event_name: value.EVENT_NAME || '{EVENT.NAME}',
  host: value.HOST_NAME || '{HOST.NAME}',
  host_ip: value.HOST_IP || '{HOST.IP}',
  item_value: value.ITEM_LASTVALUE || '{ITEM.LASTVALUE}',
  message: value.ALERT_MESSAGE || '{ALERT.MESSAGE}',
  tags: value.EVENT_TAGSJSON || '{EVENT.TAGSJSON}'
};
var resp = req.post('${fullUrl}', JSON.stringify(payload));
if (req.getStatus() < 200 || req.getStatus() >= 300) {
  throw 'OpsPilot webhook error: ' + resp;
}
return 'ok';`,
        note: isZh
          ? '在 Zabbix Management -> Media types 新建 Webhook，将常用内置宏绑定为参数（触发与恢复动作均需通知），Trigger+Host 将精准关联恢复关单。'
          : 'Paste into a Zabbix Webhook Media Type. Recovery notifications with the same Trigger ID + Host automatically resolve the incident.',
      };
    default:
      return {
        title: isZh ? '通用 JSON Webhook（curl 触发与恢复示例）' : 'Generic JSON Webhook (curl fire & resolve)',
        code: `# ${isZh ? '触发告警' : 'Trigger alert'}
curl -X POST '${fullUrl}' \\
  -H 'Content-Type: application/json' \\
  -d '{
    "status": "firing",
    "dedup_key": "checkout-db-01-conn",
    "severity": "critical",
    "rule_key": "db_conn_high",
    "title": "Checkout 主库连接池打满 (>95%)",
    "target": "mysql-checkout-01",
    "labels": { "service": "checkout", "env": "prod" }
  }'

# ${isZh ? '恢复告警' : 'Resolve alert'}
curl -X POST '${fullUrl}' \\
  -H 'Content-Type: application/json' \\
  -d '{ "status": "resolved", "dedup_key": "checkout-db-01-conn" }'`,
      };
  }
}

export function buildHeartbeatSnippet(pingUrl: string, lang: 'zh' | 'en'): string {
  const fullUrl = resolveFullEndpointUrl(pingUrl);
  if (lang === 'zh') {
    return `# 1. 定时脚本 Crontab 示例（执行成功后再 Ping，失败则不 Ping 自动触发失联告警）
* * * * * /usr/local/bin/backup.sh && curl -fsS -m 5 '${fullUrl}' >/dev/null

# 2. 携带状态说明（GET 或 POST 均可）
curl -fsS -X POST '${fullUrl}' \\
  -H 'Content-Type: application/json' \\
  -d '{"message":"watchdog alive"}'`;
  }
  return `# 1. Cron job example (ping only after the job succeeds)
* * * * * /usr/local/bin/backup.sh && curl -fsS -m 5 '${fullUrl}' >/dev/null

# 2. POST with optional message
curl -fsS -X POST '${fullUrl}' \\
  -H 'Content-Type: application/json' \\
  -d '{"message":"watchdog alive"}'`;
}
