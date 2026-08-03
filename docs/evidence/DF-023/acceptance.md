# DF-023 实施与验收证据

## 当前结论

指标、Server 部署包、运维、备份、升级和回滚材料已完成本地实现与静态/自动化验证。当前机器没有 Linux、Docker、systemd、真实 PostgreSQL 远程服务、Prometheus/告警系统和 Linux KVM Host，无法完成新服务器真实部署、告警触发和恢复演练，因此 DF-023 状态为 `blocked`，不能标记 `completed`。

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

1. 在全新 Linux Server 按 `deploy/server/README.md` 完成 systemd 或 Compose 部署；
2. 验证缺数据库 URL、缺 Token、Token 重复和非法配置时服务不会启动；
3. 应用 fresh migration，启动 Server/Agent/STF，两台 Emulator 自动达到 ready；
4. Prometheus 抓取 `/metrics`，导入最小 Dashboard；
5. 分别停止 PostgreSQL、Agent、Appium，并创建不匹配能力的 Reservation，确认关键告警触发；
6. 完成一次数据库备份、恢复到新库、Server 旧版本回滚和状态核对；
7. 保存脱敏的部署记录、指标截图、告警事件、备份校验和回滚结果。

## 阻塞解除条件

在真实 Linux 部署环境完成从零部署、故障告警、备份恢复和版本回滚演练后，将 DF-023 改为 `completed`。本地 HTTP/数据库测试不能替代 systemd、Docker、Prometheus 和真实网络验收。
