# OpsPilot 控制面高可用（HA）部署与运维手册（Runbook）

> 适用范围：`OpsPilot Manager >= 1.2` · 组件：控制面 Helm Chart（`deploy/kubernetes/opspilot`）、分布式选主控制器（`internal/pkg/coord`）、Docker Compose 单机/升级运维。

---

## 1. 控制面多副本架构与边界

OpsPilot 控制面采用**“无状态接入 + 数据库租约单活（Fail-Closed）+ 实例级执行路由”**的多副本架构，无需额外引入 Etcd / ZooKeeper / Redis 即可安全运行 `2+` 副本（依赖外部托管 **MySQL 8.0+**）：

| 能力域 | 多副本运行策略 | 关键机制说明 |
|---|---|---|
| HTTP API / Metrics / Web UI | 全副本并行服务 | 通过 `/readyz` 对数据库连通性与优雅摘流（`draining`）做真实检查 |
| On-Call 值班路由 & 规则缓存 | 全副本并行 | `oncall` 使用数据库条件租约防重；`alertRules` 在各副本保持内存规则缓存新鲜 |
| 全局定时/事件单活循环 | 分布式租约互斥单活（`cluster_leases`） | `alert-evaluator`、`alert-retry`、`flow-scheduler`、`report-scheduler`、`imbridge-stream`、`network-poll`、`cluster-reconcilers` 由 `coord.Elector` 持租约单活，含单调递增 `fencing_token` |
| `FlowRun` 长流程执行 | `execution_owner + lease_until` | 触发副本后台续租；`cluster-reconcilers` 仅扫除租约已过期的僵尸执行 |
| 交互式 AI Tool 审批（`agent_tool`） | 跨副本交接回原属副本执行 | 批准请求可落在任意副本；非 `ExecutionOwner` 副本落库 `approved` 并等待拥有内存闭包的原属副本写回结果；同副本受 `IsLocallyExecuting` 互斥保护 |
| 持久化工作区与附件（`Persistence`） | 可选 **RWX 共享存储** | `/var/lib/opspilot` 包含会话附件、溢出工具结果、Skill、工作区与抓包文件；多副本共享读写时应启用 `manager.persistence.enabled=true` 且采用 `ReadWriteMany` PVC（如 NFS/EFS/CFS） |
| 边端隧道（`Frontier`） | 独立 Deployment（单活优先，RollingUpdate `maxUnavailable=0`） | 开启 `edgeid_alloc_when_no_idservice_on: false`，确保控制面重连恢复前不放行未鉴权临时会话 |

---

## 2. Kubernetes Helm 生产部署与外置托管集成

### 2.1 前置条件

1. 外部高可用 **MySQL 8.0+** 实例与库表账号（多副本严禁使用 `sqlite`，Chart 模板会强制拦截）。
2. 如多副本需要跨 Pod 共享本地 Skill、工作区、附件及抓包文件，提前准备支持 `ReadWriteMany` 的 StorageClass 或预建 PVC。
3. 生产环境推荐提前创建好外部 Kubernetes `Secret` 存放数据库 DSN 与应用密钥，避免明文密码进入 Helm Release values。

### 2.2 准备外部 Secret 示例

```bash
kubectl create namespace opspilot-system

kubectl -n opspilot-system create secret generic opspilot-db-secret \
  --from-literal=db-dsn='opspilot:STRONG_PASSWORD@tcp(mysql-ha.infra.svc:3306)/opspilot?parseTime=true&charset=utf8mb4&loc=UTC'

kubectl -n opspilot-system create secret generic opspilot-app-secret \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=secret-key="$(openssl rand -hex 32)" \
  --from-literal=packet-parser-token-secret="$(openssl rand -hex 32)"
```

### 2.3 安装与对接外置可观测后端

```bash
helm upgrade --install opspilot deploy/kubernetes/opspilot \
  --namespace opspilot-system \
  --set manager.replicaCount=2 \
  --set web.replicaCount=2 \
  --set database.existingSecret=opspilot-db-secret \
  --set secrets.existingSecret=opspilot-app-secret \
  --set externalServices.publicURL="https://ops.example.com" \
  --set externalServices.prometheus.enabled=true \
  --set externalServices.prometheus.url="http://prometheus.monitoring.svc:9090" \
  --set externalServices.prometheus.queryURL="http://prometheus.monitoring.svc:9090" \
  --set externalServices.loki.url="http://loki.monitoring.svc:3100" \
  --set externalServices.tempo.url="http://tempo.monitoring.svc:3200" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=ops.example.com' \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.prometheusRule.enabled=true
```

---

## 3. 优雅升级、防误杀机制与回滚方案

### 3.1 零中断滚动更新时序

1. Pod 终止时，Kubernetes 先向 EndpointSlice 摘除实例并执行 `preStop: sleep 5`，确保新入站 HTTP/审批流量停止路由。
2. 收到 `SIGTERM` 后，`rootCtx` 立即取消：
   - `/readyz` 在内核退出循环尚未结束前即返回 `503 draining`（即使 DB 仍连通）。
   - `coord.Elector` 在收到上下文取消后等待各组内工作协程退出并主动向 `cluster_leases` 调用 `Release`，备选副本在 `LeaseRetryInterval`（默认 `2s`）内接管，无需等待租约自然过期。
3. 新启动副本在 `bootstrapRuntime` 时调用 `HasOtherActiveHolder`：只要集群中仍有其他未过期租约持有者，启动阶段跳过激进式清理，避免误杀同行副本上正在运行的 Worker；超期僵尸 Worker 与执行租约由 `cluster-reconcilers` 按 `OPSPILOT_STALE_WORKER_TIMEOUT`（默认 `30m`）周期安全回收。

### 3.2 版本回滚步骤

```bash
# 查看 Helm 发布历史
helm -n opspilot-system history opspilot

# 一键回滚到上一个已验证版本（代码向后兼容 cluster_leases / flow_runs 扩展列）
helm -n opspilot-system rollback opspilot <REVISION>

# 核对回滚后 Pod 就绪状态与 Leader 分布
kubectl -n opspilot-system rollout status deploy/opspilot-manager
```

Docker Compose 单机回滚同样只需在安装目录把 `.env` 中的 `OPSPILOT_VERSION` 改回上一版本后执行 `docker compose up -d`。

---

## 4. 自观测与 HA 告警排障指南

### OpsPilotClusterLeaseNoLeader

- **触发条件**：`sum by (lease) (opspilot_cluster_lease_held) == 0` 持续 2 分钟。
- **影响**：对应后台单活控制器（如 `alert-evaluator`、`flow-scheduler`、`imbridge-stream`）在全集群处于无主停摆状态。
- **排查步骤**：
  1. 检查所有 `manager` Pod 的 `/readyz` 与数据库连通性：`kubectl logs deploy/opspilot-manager --tail=200 | grep -E "coord|lease|mysql"`。
  2. 直连 MySQL 查看租约表：`SELECT lease_key, holder_id, fencing_token, expires_at FROM cluster_leases;`。
  3. 验证数据库服务器与节点时钟偏差（`SELECT UTC_TIMESTAMP();`），若存在明显时钟漂移先同步 NTP。
  4. 确认是否存在连接池打满导致续租超时（同步排查 `#opspilotdbpoolsaturated`）。

### OpsPilotClusterLeaseSplitBrain

- **触发条件**：`sum by (lease) (opspilot_cluster_lease_held) > 1` 持续 1 分钟。
- **影响**：多于 1 个副本自认为持有同一个分布式租约，可能导致重复通知或重复触发工作流。
- **排查步骤**：
  1. 立即检查各副本指标与日志确认持有者实例 ID：`kubectl get pods -l app.kubernetes.io/component=manager -o wide`。
  2. 核对每个 Pod 的 `OPSPILOT_INSTANCE_ID` 环境变量是否唯一（Helm 默认注入唯一 Pod 名 `metadata.name`；禁止将同一静态 ID 复制到多个副本）。
  3. 确认 MySQL 存储层未发生主从异步切换回退或时钟回拨，检查 `cluster_leases.fencing_token` 是否单调递增。
  4. 若确认存在异常驻留老 Pod，手动 `kubectl delete pod <pod>` 令其触发 fail-closed 退出并交出租约。

### OpsPilotClusterLeaseFlapping

- **触发条件**：同一租约在 15 分钟内发生 `>= 4` 次重新获取（`opspilot_cluster_lease_transitions_total{event="acquired"}`）并持续 5 分钟。
- **排查步骤**：
  1. 检查 Pod 是否发生频繁 `OOMKilled` 或 CPU Throttling，适当提升 `manager.resources.limits`。
  2. 检查 `OPSPILOT_LEASE_TTL`（默认 `15s`）与 `OPSPILOT_LEASE_RENEW_INTERVAL`（默认 `5s`），在跨可用区高延迟 DB 场景可适当调大为 `30s / 10s`。
  3. 排查 MySQL 慢查询锁表是否阻塞租约行更新。

### OpsPilotManagerDown

- **触发条件**：Prometheus 抓取 `up{job="opspilot-manager"} == 0` 持续 2 分钟。
- **排查步骤**：检查容器存活状态、`kubectl describe pod` 事件、`/var/lib/opspilot` 磁盘/PVC 挂载状态及端口 `9100` 连通性。

### OpsPilotOrphanWorkerSessions

- **触发条件**：运行态内部 Worker 会话数 `> 20` 持续 5 分钟。
- **排查步骤**：
  1. 查看当前活跃会话：查询数据库或调用 `/api/v1/aiops/sessions?kind=investigation`。
  2. 确认 `cluster-reconcilers` 租约持有正常，且 `OPSPILOT_STALE_WORKER_TIMEOUT`（默认 `30m`）配置未被误设过长；如遇 goroutine 卡死，采集 `/metrics` 及堆栈日志定位阻塞工具调用。

### OpsPilotLLMErrorRateHigh

- **触发条件**：最近 5 分钟 LLM 调用错误率超过 `10%` 并持续 10 分钟。
- **排查步骤**：检查 `/admin/llm` 模型配置、API Key 余额与限流响应，检索 Manager 日志 `llm chat completion failed` 区分上游 429/5xx 与网络超时。

### OpsPilotDBPoolSaturated

- **触发条件**：`opspilot_db_pool_in_use / opspilot_db_pool_open_connections > 0.9` 持续 5 分钟。
- **排查步骤**：在多副本下核对总连接数（`副本数 × OPSPILOT_DB_MAX_OPEN_CONNS`）不超过 MySQL `max_connections` 的 80%，按需调整 `database.maxOpenConns` 并检查慢 SQL。

### OpsPilotAlertEvaluatorStalled

- **触发条件**：全集群告警评估心跳指标 `opspilot_alert_eval_ticks_total` 速率为 0 持续 10 分钟。
- **排查步骤**：先确认 `alert-evaluator` 租约是否有主（`#opspilotclusterleasenoleader`），再检索持有该租约 Pod 日志中 `alert-pipeline` 与 Prometheus/Loki 连接错误。

### OpsPilotHTTP5xxRateHigh

- **触发条件**：HTTP 5xx 请求占比超过 `5%` 持续 5 分钟。
- **排查步骤**：按路由聚合定位高错误接口：`topk(5, sum by (route) (rate(opspilot_http_requests_total{status="5xx"}[5m])))`，联动检查 `/readyz`、MySQL 及外置可观测后端健康度。

---

## 5. 上线前故障演练验收清单

1. **杀主切换演练**：在 2 副本运行时 `kubectl delete pod <alert-evaluator-holder>`（或 Compose 强杀主进程），确认另一副本在 `< LeaseTTL` 内接管 `cluster_leases` 且仅存在单个 `opspilot_cluster_lease_held == 1`。
2. **滚动升级不断流**：在触发长耗时 `FlowRun` 执行与滚动发布 `helm upgrade` 同步进行时，验证活跃工作流继续完成而不会被启动流程提前置为 `failed`。
3. **跨副本审批交接**：由副本 A 发起需审批工具调用，强行将 `/api/v1/approvals/{id}/approve` 路由至副本 B，确认副本 A 唯一执行工具并在超时窗口内把最终结果写回批准响应。
4. **就绪摘流验证**：向某副本发送 `SIGTERM` 时立即探测其 `/readyz`，确认返回 HTTP `503` 与 `draining`。
