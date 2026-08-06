# 已知问题和风险

| 编号 | 问题 | 影响 | 处理计划 |
|---|---|---|---|
| KI-001 | `/dev/kvm` 和 VMX 阻塞已于 2026-08-04 解除，DF-014～DF-028 已使用一台 Android 16 Emulator 完成真实验收；Docker daemon 仍配置失效代理，不能直接访问 Docker Hub | 不再阻塞设备域 Gate；以后拉取新镜像仍需使用已保留正式镜像、宿主机网络下载工具或受控镜像代理 | 保留正式 Emulator、STF 和 DF-028 Server 镜像；不重启 Docker；需要新镜像时继续使用临时 crane/国内代理并在完成后清理 |
| KI-002 | 已关闭：DF-023 已完成隔离 Compose 部署、Prometheus 真实告警、备份恢复和旧版本回滚 | 不再阻塞 G6 | 证据见 `docs/evidence/DF-023/acceptance.md` |
| KI-003 | 已关闭：DF-024 停止真实 PostgreSQL 后请求返回 500，恢复后相同幂等键只创建一条 Reservation | AT-DB-006 已签收 | 证据见 `/home/kerr/df024-acceptance-20260806/database-disconnect-recovery.log` |
| KI-004 | 已关闭：DF-021 已完成连续 50 次真实申请/释放/重建 | 资源泄漏和长期悬挂风险已关闭 | 50 次后开放预约 0，受管容器/网络/卷稳定为 `1/1/1` |
| KI-005 | Alcor `origin/feature/refactoring@868de61` 已有 RunAttempt/Worker，但 Swagger 仍未发布实际 Run/RunAttempt，且没有 Device Farm 配置/Adapter | ALCOR-001 不能安全编码联调 | 使用 `check-alcor-integration-readiness.ps1` 持续检查；真实 OpenAPI 和 Android 扩展输入到位后按 DF-025 冻结边界接入 |
| KI-007 | Alcor 当前 Worker 使用 `X-Alcor-Run-Id` / `X-Alcor-Attempt-Id`，与确定方案的 `X-Eval-*` 不一致 | 日志、Trace 和设备预约关联字段可能分裂 | Alcor 合入 Device Farm 前统一为 `X-Eval-Run-Id`、`X-Eval-Attempt-Id` 和 `traceparent`，或由双方正式 ADR 修改契约 |
| KI-006 | 原验收计划的 DaFit “26 个用例”已过时 | 会错误要求回退当前语言矩阵 | 已按现主线改为 158 个执行实例不回归基线 |
| KI-008 | 已关闭：真实 STF 联调确认 `remoteConnect` 返回 ADB TCP 地址；Console 已按 ADR-0010 删除伪 Web 入口 | 不再误导用户，也不向浏览器暴露 STF 管理能力；DF-018 后端 Adapter 契约不受影响 | 如未来 STF 或受控网关提供 Reservation 级短时 Web 授权，必须先新增 ADR 和 OpenAPI 字段再接入 |
| KI-009 | Vite 提示 Console 单入口 bundle 大于 500 kB | P2 首屏加载优化，不影响功能、安全、部署、回滚和真实设备验收 | 后续按页面拆包和按需加载优化，不阻塞 DF-028 或 G7 |

设备域 G0～G7 的真实设备、安全、数据隔离、稳定性、回滚和浏览器交付风险均已关闭。KI-001 是不影响已保留镜像运行的运维限制，KI-009 是非阻塞性能优化；KI-005/KI-007 继续等待新版 Alcor 外部契约。
