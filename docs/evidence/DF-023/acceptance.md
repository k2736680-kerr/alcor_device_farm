# DF-023 实施与验收证据

## 当前结论

指标、Server 部署包、运维、备份、升级和回滚材料已完成本地实现、自动化验证和真实 Linux 环境演练。2026-08-06 在 `10.0.30.171` 完成隔离 fresh migration、Compose 部署、配置失败保护、Prometheus 告警、生产库备份、隔离恢复和旧版本 Server 回滚，DF-023 状态为 `completed`。

## 已完成交付

- `/metrics` 提供 Prometheus 文本格式，覆盖 Server、HTTP、数据库、Scheduler backlog、Device、Reservation、Agent、Host Command 和健康错误；
- `/readyz` 真实检查 PostgreSQL 和关键设备表，无数据库、不可连接或 migration 未应用时返回 503；
- 指标不使用 Device/Reservation/owner 高基数标签，不输出敏感 Endpoint 和 Token；
- root `Dockerfile` 和 `deploy/server/compose.yaml` 提供无 Docker Socket、只读文件系统、非 root、cap drop 的 Server 容器；
- systemd unit 使用独立无登录账户，不加入 docker/kvm 组，启动前强制检查数据库和 Service/Agent Token；
- 安装、fresh migration、显式增量 migration 和 PostgreSQL custom-format 备份脚本；
- Dashboard 指标清单、可导入 Prometheus 告警规则、故障注入、日常 runbook、升级步骤和数据库恢复回滚方案；
- Compose 默认只绑定 `127.0.0.1`，数据库和 STF 继续复用已有服务，不创建第二套平台能力。

## 本地验收结果

```text
PASS /healthz 存活检查与 /readyz 数据库就绪语义分离
PASS 无数据库时 /readyz=503，/metrics 仍可抓取且 database_ready=0
PASS 真实临时 PostgreSQL 下 Device/Scheduler/Agent/Reservation/Command/Error 指标正确
PASS HTTP 路由使用模板标签，不写入资源 ID
PASS OpenAPI、部署文件和 shell 安全静态契约
PASS 配置校验、go vet、全量 go test、三程序 build
PASS migration up/down/up 和数据库集成测试
```

## 真实环境验收

真实证据目录：

```text
/home/kerr/df023-acceptance-20260806/
```

主要证据：`fresh-deploy.log`、`alert-injection.log`、`alerts.log`、`alert-final-state.log`、`backup-restore-rollback.log`、`compose.rendered.sanitized.yaml`、`production-metrics-final.txt`、`rollback-metrics.txt` 和 `backups/`。

### 隔离部署和配置门禁

在独立 Docker 网络和独立 PostgreSQL 中按正式 Compose 安全参数部署 Server：

```text
CONFIG_REJECTED=missing_database
CONFIG_REJECTED=missing_service_token
CONFIG_REJECTED=duplicate_tokens
CONFIG_REJECTED=invalid_duration
MIGRATION_FRESH_AND_REPEAT_GUARD=PASS
FRESH_COMPOSE_DEPLOY=healthy|ready_200|metrics_database_ready_1|no_socket|non_root|readonly|cap_drop_all
DF023_PHASE1_PASS=true
```

fresh migration 成功，第二次 `--fresh` 因已有设备域表而拒绝。隔离 Server `/healthz`、`/readyz` 和 `/metrics` 正常，无 Docker Socket，用户为 `65532:65532`，根文件系统只读且 `CapDrop=ALL`。当前验收按 ADR-0008 使用单 Emulator 基线，不再沿用旧设计中的“两台 Emulator”要求。

### Prometheus 和故障告警

Docker Hub 代理不可用时，改用 Prometheus 官方 `2.54.1` Linux 发布包；官方 SHA-256 校验为 `31715ef65e8a898d0f97c8c08c03b6b9afe485ac84e1698bcfec90fc6e62924f`。`promtool` 对正式配置和 9 条规则均检查通过，隔离与生产两个 target 同时为 up。

```text
ALERT_FIRING=DeviceFarmDatabaseNotReady|127.0.0.1:18082|critical
ALERT_FIRING=DeviceFarmAgentHeartbeatStale|10.0.30.171:18080|critical
ALERT_FIRING=DeviceFarmHealthErrors|10.0.30.171:18080|warning
ALERT_FIRING=DeviceFarmReservationBacklog|10.0.30.171:18080|warning
ALERT_FINAL_STATE=database_recovered|agent_online|appium_healthy|device_ready|open_reservations_0|prometheus_stopped
```

- 停止隔离 PostgreSQL 后数据库未就绪告警 firing，恢复后 `/readyz` 回到 200；
- 停止真实 Host Agent 后心跳过期告警 firing，随后 Agent 与 Host 恢复在线；
- 停止真实 Appium 进程后健康错误计数增长且告警 firing，Appium 恢复为 HTTP 200，Device 恢复 `ready|healthy|0`；
- 创建 API 36/ABI 不匹配的 Reservation 后 backlog 告警 firing；清理 pending 预约的正确终态为 `failed/RESERVATION_CANCELED`，开放预约最终为 0。

### 备份、恢复和旧版本回滚

正式 `backup-device-farm.sh` 生成 PostgreSQL custom-format dump 和 0600 校验文件：

```text
BACKUP_CREATED=device-farm-20260806T190934Z.dump|sha256_03a96a03943afedfd9b98cd9e80cad5cb561063bf8df81c3eb57ede5adf33393|mode_600
RESTORE_CORE_COUNTS_MATCH=1|1|1|4|86|75|99
OLD_VERSION_ROLLBACK=version_recovery9|health_200|ready_200|metrics_ok|api_200|state_counts_match|no_down_migration
EVIDENCE_SECRET_SCAN=exact_runtime_secrets_0
DF023_PHASE3_PASS=true
```

备份恢复到隔离新库后，Image、Host、Pool、Device、Reservation、Session 和 Host Command 数量与生产备份点一致。上一版本镜像 `df021-stf-recovery9-20260806` 在隔离端口读取恢复库，健康、就绪、指标和设备 API 均通过；没有执行 down migration，也未覆盖生产数据库。

验收结束后已删除隔离 Server/PostgreSQL、Docker 网络、Prometheus 进程与数据目录、临时 env 和回滚容器。生产 Server 仍为 `df021-stf-recovery10-20260806`，Host `online`，当前 Device `ready|healthy|0`，开放 Reservation 为 0，受管容器/网络/卷为 `1/1/1`。真实数据库密码和 Service/Agent Token 未写入日志、证据文档或 Git 文件。
