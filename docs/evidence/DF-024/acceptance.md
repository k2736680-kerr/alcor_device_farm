# DF-024 实施与验收证据

## 当前结论

DF-024 已完成 E0 本地全量门禁和 E1/E2 真实 Linux KVM、Android 16 Emulator、STF、Appium、DaFit、故障恢复、安全、稳定性、告警与回滚验收。设备域 G0～G6 的 P0/P1 全部通过，DF-024 状态为 `completed`；浏览器控制台 G7 继续由 DF-028 独立验收。

## 权威证据

- 环境：[MVP 验收环境](../mvp-acceptance/environment.md)
- 用例结果：[MVP 验收报告](../mvp-acceptance/results.md)
- 已知问题：[已知问题和风险](../mvp-acceptance/known_issues.md)
- 版本清单：[验收版本清单](../mvp-acceptance/version_manifest.md)
- 自动门禁日志：[local-gate.txt](../mvp-acceptance/artifacts/local-gate.txt)
- 真实最终门禁：`/home/kerr/df024-acceptance-20260806/final-gate.log`
- 数据库断连恢复：`/home/kerr/df024-acceptance-20260806/database-disconnect-recovery.log`
- 实施提交：`392c93a 整理设备农场全量验收证据`

## 已验证结果

```text
PASS Device Farm 全量 Go 门禁
PASS PostgreSQL migration up/down/up
PASS Repository/Scheduler/Reaper/API/Agent/健康/指标集成测试
PASS 100 次、20 RPS 查询性能基线，p95 远低于 300ms
PASS DaFit collect-only 收集 158 个执行实例
PASS 验收日志为有效 UTF-8，无 UTF-16 NUL 混入
PASS 实施证据门禁动态读取计划状态，并校验计划/清单一致、证据已纳入 Git、主要提交存在且可从 master HEAD 追溯
PASS 真实 Android 16/API 36、ADB boot completed、Appium ready、STF serial 唯一可见
PASS DaFit 成功/故意失败/超时/中断/Reaper 与跨任务数据隔离
PASS 连续 50 次真实申请/释放/重建，开放预约 0，受管资源稳定为 1/1/1
PASS Token 轮换、旧 Token 401、跨身份 403、秘密零检出和 Docker Socket 隔离
PASS Prometheus 四类真实告警、生产备份、隔离恢复和旧版本 Server 回滚
PASS 数据库断开期间请求 HTTP 500 而非伪成功，恢复后同一幂等请求仅创建 1 条预约
```

真实最终门禁：

```text
ENVIRONMENT=linux_5.15.0-186-generic|cpu_12|memory_16349499392|docker_28.1.1|kvm_rw
SERVER_ENDPOINTS=health_200|ready_200|metrics_database_ready_1
AUTH_BOUNDARY=unauth_401|agent_north_403|service_internal_403
CONTROL_STATE=host_online|device_ready_healthy_0|open_reservations_0|open_commands_0|resources_1_1_1
EMULATOR_APPIUM=android_16|api_36|boot_completed_1|appium_ready
STF_INVENTORY=serial_unique_present_ready
CROSS_TASK_EVIDENCE=df021_50_cycles|df022_security|df023_deploy_alert_rollback|df024_db_recovery
SECURITY_RUNTIME=no_server_socket|uid_65532|readonly|cap_drop_all|socket_660_root_docker
TEMPORARY_RESIDUE=containers_0|prometheus_0
DF024_FINAL_GATE_PASS=true
```

## 验收边界

- 当前真实资源基线按 ADR-0008 固定为单台 Emulator；多设备端口隔离由自动化契约覆盖，真实两设备 DaFit/Appium 并发改为 P2 扩展项；
- 第二个并发预约不得突破 `max_instances=1` 仍是 P0，并已由真实容量场景和 PostgreSQL 并发门禁覆盖；
- Device Farm Console 的浏览器登录、受控 STF 看屏、Web 安全、重启和回滚属于 G7/DF-028，不阻塞 DF-024 的设备域 G0～G6 结论；
- 新版 Alcor 真实 Run/RunAttempt OpenAPI 尚未发布，ALCOR-001 保持外部等待，不影响独立 Device Farm MVP。

DF-024 验收结束时，生产 Server/Agent/STF/Appium 均运行，Host `online`，当前 Device `ready|healthy|0`，开放 Reservation 和未完成 Host Command 均为 0，受管容器/网络/卷为 `1/1/1`，无 DF-022～DF-024 临时容器或 Prometheus 进程残留。
