# 可观测性和告警

## 端点

- `/healthz`：进程存活检查，只要 HTTP 进程可服务就返回 200；
- `/readyz`：就绪检查，PostgreSQL 未配置、不可连接、超时或关键 migration 未应用时返回 503；
- `/metrics`：Prometheus 文本格式，部署在受控内网，不要求把 Service Token 交给监控系统。

`/metrics` 不输出 Token、数据库 URL、STF 管理密钥、设备 Endpoint 或 Reservation owner ID。

## MVP 指标

| 指标 | 含义 | 主要用途 |
|---|---|---|
| `device_farm_up` | Server 进程存活 | 服务不可用告警 |
| `device_farm_build_info` | version、commit、build date、Go/OS/arch | 发布追踪 |
| `device_farm_process_uptime_seconds` | 进程运行时长 | 重启风暴识别 |
| `device_farm_http_requests_total` | method、路由模板、status 维度请求数 | 流量和 4xx/5xx |
| `device_farm_http_request_duration_seconds` | 路由耗时 sum/count | 平均延迟和慢请求 |
| `device_farm_database_ready` | PostgreSQL 可用性 | 就绪和数据库告警 |
| `device_farm_database_connections` | acquired/idle/total | 连接池压力 |
| `device_farm_devices` | lifecycle + health 设备数 | ready、recycling、quarantined 观察 |
| `device_farm_reservations` | 各预约状态数量 | pending/active/terminal 观察 |
| `device_farm_scheduler_oldest_pending_seconds` | 最老 pending 等待时间 | 容量不足或 Scheduler 故障 |
| `device_farm_agents` | Host/Agent 状态数量 | Agent 离线告警 |
| `device_farm_agent_heartbeat_max_age_seconds` | 最老 Host heartbeat 年龄 | 心跳中断告警 |
| `device_farm_host_commands` | Host Command 状态数量 | pending/leased/failed/timed_out |
| `device_farm_health_events_total` | 按 severity 累计健康事件 | Appium/STF/Agent 错误增长 |
| `device_farm_metric_collection_errors_total` | 指标查询失败次数 | 指标本身失真告警 |

MVP 只有两台模拟器，不按 Device ID、Reservation ID、owner ID 建指标标签，避免高基数和业务标识泄露。排障明细使用 request ID、审计表和健康事件查询。

## Dashboard 最小面板

1. Server up、版本、uptime、HTTP 5xx 比例和平均延迟；
2. PostgreSQL ready 和连接池；
3. ready/healthy、recycling、quarantined 设备数；
4. pending/active Reservation 和最老 pending 年龄；
5. Agent online/offline 和 heartbeat 最大年龄；
6. Host Command pending/leased/failed/timed_out；
7. error health event 增量。

## 告警建议

可直接导入的 Prometheus 规则位于 `deploy/monitoring/prometheus-rules.yaml`。

| 告警 | 建议条件 | 级别 | 操作 |
|---|---|---|---|
| ServerDown | `device_farm_up == 0` 持续 1 分钟 | P0 | 检查进程、端口、发布和节点 |
| DatabaseNotReady | `device_farm_database_ready == 0` 持续 1 分钟 | P0 | 停止发布，检查 PostgreSQL 和网络 |
| AgentHeartbeatStale | heartbeat 最大年龄大于 `2 × reconcile.host_timeout` | P0 | 检查 Agent、Host、Token 和网络 |
| NoReadyDevice | ready+healthy 为 0 持续 5 分钟 | P0 | 检查 KVM、镜像、Appium 和重建命令 |
| ReservationBacklog | 最老 pending 大于 60 秒 | P1 | 检查容量、Scheduler、Pool 和能力匹配 |
| RecyclingStuck | recycling 设备大于 0 持续 10 分钟 | P1 | 检查 rebuild command 和 Docker 清理 |
| CommandFailures | failed/timed_out command 增长 | P1 | 按 error_code 和 Host 日志处理 |
| HTTP5xx | 5 分钟 5xx 比例大于 5% | P1 | 按路由和 request ID 排查 |
| MetricCollectionError | collection error 增长 | P1 | 指标可能不完整，检查 DB 查询 |

阈值是两台设备 MVP 的起始值。增加模拟器或接入真机后只调整阈值，不修改指标和架构。

## 故障注入验收

- 停止 PostgreSQL：`/readyz` 返回 503，DatabaseNotReady 可触发；
- 停止 Host Agent 超过阈值：Host offline/heartbeat stale 可触发；
- 创建无法匹配能力的 Reservation：oldest pending 上升并触发 backlog；
- 让 Appium 健康检查失败：设备最终 quarantined，health error 和 command failure 增长；
- 使用 Mock 故障只验证指标表达式和流程，真实签收仍需 Linux KVM、Docker、Agent 和监控系统。
