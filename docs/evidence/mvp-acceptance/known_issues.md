# 已知问题和风险

| 编号 | 问题 | 影响 | 处理计划 |
|---|---|---|---|
| KI-001 | 内网候选服务器已有 Ubuntu 22.04 和 Docker 28.1.1，但 BIOS 关闭 VMX、没有 `/dev/kvm`，Docker Hub Registry 443 连接超时；本机 WSL2 仍因 `ext4.vhdx` 路径不存在而无法启动 | G3～G5 不能签收，当前不能启动真实 Docker Emulator | 由服务器管理员开启 Intel VT-x/VMX 并安排受控重启，确认 `/dev/kvm` 可读写；开放 Docker Hub 或提供内网镜像仓库/代理后，再执行 DF-014～DF-021 和 AT-EMU/APP/STF/DFT |
| KI-002 | 当前没有 Prometheus/Alertmanager 和真实 systemd | 部署、告警和回滚只能做静态/本地验证 | 按 DF-023 手册完成从零部署、故障告警和恢复演练 |
| KI-003 | PostgreSQL 短暂断连后的未确认事务未做真实网络故障注入 | AT-DB-006 尚未签收 | 在 Linux 测试库使用网络阻断验证客户端重试和幂等 |
| KI-004 | 稳定性未运行 8 小时或 50 次真实循环 | 资源泄漏和长期悬挂风险未关闭 | 在 E2 运行循环并保存 Docker/DB/指标前后快照 |
| KI-005 | Alcor `origin/feature/refactoring@868de61` 已有 RunAttempt/Worker，但 Swagger 仍未发布实际 Run/RunAttempt，且没有 Device Farm 配置/Adapter | ALCOR-001 不能安全编码联调 | 使用 `check-alcor-integration-readiness.ps1` 持续检查；真实 OpenAPI 和 Android 扩展输入到位后按 DF-025 冻结边界接入 |
| KI-007 | Alcor 当前 Worker 使用 `X-Alcor-Run-Id` / `X-Alcor-Attempt-Id`，与确定方案的 `X-Eval-*` 不一致 | 日志、Trace 和设备预约关联字段可能分裂 | Alcor 合入 Device Farm 前统一为 `X-Eval-Run-Id`、`X-Eval-Attempt-Id` 和 `traceparent`，或由双方正式 ADR 修改契约 |
| KI-006 | 原验收计划的 DaFit “26 个用例”已过时 | 会错误要求回退当前语言矩阵 | 已按现主线改为 158 个执行实例不回归基线 |

没有风险接受可以替代 P0 真实设备、安全和数据隔离验收。以上问题关闭前，MVP 不签字为完成。
