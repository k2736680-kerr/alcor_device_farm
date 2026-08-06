# DF-003～DF-028 实施与证据清单

本清单用于证明每个步骤都有明确提交、状态和验收入口。`completed` 表示任务定义的全部完成条件已满足；`blocked` 表示代码和本地门禁已交付，但方案明确要求的真实 Linux/设备环境尚未验收，不能以 Mock 替代；`pending` 表示已建立正式任务和验收模板，但尚未开始实现。

| 任务 | 当前状态 | 主要提交 | 验收证据 |
|---|---|---|---|
| DF-003 | completed | `aede06f 固定设备接口并加入双身份认证` | `docs/evidence/DF-003/acceptance.md` |
| DF-004 | completed | `696c11d 建立设备数据库迁移和约束` | `docs/evidence/DF-004/acceptance.md` |
| DF-005 | completed | `d4dc074 加入设备领域模型和状态机` | `docs/evidence/DF-005/acceptance.md` |
| DF-006 | completed | `be3b820 加入数据库事务和并发领取` | `docs/evidence/DF-006/acceptance.md` |
| DF-007 | completed | `a8def5e 加入模拟设备和故障注入` | `docs/evidence/DF-007/acceptance.md` |
| DF-008 | completed | `54fa240 完成设备资源管理接口` | `docs/evidence/DF-008/acceptance.md` |
| DF-009 | completed | `7366c50 加入设备预约和并发调度` | `docs/evidence/DF-009/acceptance.md` |
| DF-010 | completed | `a9bc50d 加入预约续租释放和过期回收` | `docs/evidence/DF-010/acceptance.md` |
| DF-011 | completed | `29d27c3 加入设备健康上报和自动隔离` | `docs/evidence/DF-011/acceptance.md` |
| DF-012 | completed | `fb92644 加入主机心跳和命令租约协议` | `docs/evidence/DF-012/acceptance.md` |
| DF-013 | completed | `a5ad7bc 加入主机代理运行和优雅退出`、`2701fcb`、`ce58864` | `docs/evidence/DF-013/acceptance.md` |
| DF-014 | completed | `0573568 加入Docker模拟器Provider和验收入口`、`63707ed`、`2c1468a 支持单台Android16模拟器验收` | `docs/evidence/DF-014/acceptance.md` |
| DF-015 | completed | `03215e1 加入独立Appium端点和健康检查`、`2c1468a 支持单台Android16模拟器验收` | `docs/evidence/DF-015/acceptance.md` |
| DF-016 | completed | `24c2c56 加入参数化模拟器自动补齐控制器`、`2c1468a 支持单台Android16模拟器验收`、`c3fd757 完成单台模拟器自动补池` | `docs/evidence/DF-016/acceptance.md` |
| DF-017 | completed | `c617b34 加入固定版本STF内网部署`、`完成DF-017真实STF验收和故障收敛` | `docs/evidence/DF-017/acceptance.md` |
| DF-018 | completed | `78cc97f 接入STF预约和远程连接`、`完成DF-018真实STF适配验收` | `docs/evidence/DF-018/acceptance.md` |
| DF-019 | completed | `84b3331 记录DaFit设备农场适配`、`14e2928`、`完成DF-019真实DaFit Farm验收` | `docs/evidence/DF-019/acceptance.md` |
| DF-020 | blocked | `65abd2d 加入DaFit端到端运行工具` | `docs/evidence/DF-020/acceptance.md` |
| DF-021 | blocked | `a3d2bf5 补齐设备故障恢复和重建清理` | `docs/evidence/DF-021/acceptance.md` |
| DF-022 | blocked | `f1acdf5 加固设备权限审计和敏感数据` | `docs/evidence/DF-022/acceptance.md` |
| DF-023 | blocked | `ef28201 补齐设备指标部署和回滚手册` | `docs/evidence/DF-023/acceptance.md` |
| DF-024 | blocked | `392c93a 整理设备农场全量验收证据` | `docs/evidence/DF-024/acceptance.md` |
| DF-025 | completed | `df975e1 补齐新版Alcor设备接入契约` | `docs/evidence/DF-025/acceptance.md` |
| DF-026 | in_progress | — | `docs/evidence/DF-026/acceptance.md` |
| DF-027 | pending | — | `docs/evidence/DF-027/acceptance.md` |
| DF-028 | pending | — | `docs/evidence/DF-028/acceptance.md` |

## 当前剩余条件

1. DF-026/DF-027 在 E0 本地环境完成 Console 实现和自动化验收；
2. Linux KVM/Docker/真实 Android 环境继续完成 DF-020～DF-024，并由 DF-028 完成真实 Web 验收；
3. 新版 Alcor 发布真实 Run/RunAttempt OpenAPI 和 Device Farm 扩展输入后完成 ALCOR-001；
4. 所有状态变化继续保存验收证据并使用简洁中文 Git 提交。
