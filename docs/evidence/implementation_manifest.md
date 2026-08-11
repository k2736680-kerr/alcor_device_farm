# DF-003～DF-037 实施与证据清单

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
| DF-020 | completed | `65abd2d 加入DaFit端到端运行工具`、`完成DF-020真实Harness和Reaper验收` | `docs/evidence/DF-020/acceptance.md` |
| DF-021 | completed | `a3d2bf5 补齐设备故障恢复和重建清理`、`完成DF-021真实故障恢复验收` | `docs/evidence/DF-021/acceptance.md` |
| DF-022 | completed | `f1acdf5 加固设备权限审计和敏感数据`、`完成DF-022真实权限审计和秘密验收` | `docs/evidence/DF-022/acceptance.md` |
| DF-023 | completed | `ef28201 补齐设备指标部署和回滚手册`、`完成DF-023真实部署告警和回滚验收` | `docs/evidence/DF-023/acceptance.md` |
| DF-024 | completed | `392c93a 整理设备农场全量验收证据`、`完成DF-024设备域MVP全量验收` | `docs/evidence/DF-024/acceptance.md` |
| DF-025 | completed | `df975e1 补齐新版Alcor设备接入契约` | `docs/evidence/DF-025/acceptance.md` |
| DF-026 | completed | `4b1d7d1 DF-026 本地E0验收完成` | `docs/evidence/DF-026/acceptance.md` |
| DF-027 | completed | `190bbc6 完成DF-027设备操作、人工预约与STF远控页面验收` | `docs/evidence/DF-027/acceptance.md` |
| DF-028 | completed | `92fa8a8 实现DF-028控制台部署和真实Web验收链路` | `docs/evidence/DF-028/acceptance.md` |
| DF-029 | completed | `ccf2940 完成后台设备容量自动扩缩容` | `docs/evidence/DF-029/acceptance.md` |
| DF-030 | completed | `1629edb 完成隔离设备安全删除` | `docs/evidence/DF-030/acceptance.md` |
| DF-031 | completed | `本次提交：加入管理员设备远程控制` | `docs/evidence/DF-031/acceptance.md` |
| DF-032 | completed | `d8f573d 按实际资源动态计算设备容量` | `docs/evidence/DF-032/acceptance.md` |
| DF-033 | completed | `本次提交：改为按设备池总目标动态扩缩容` | `docs/evidence/DF-033/acceptance.md` |
| DF-034 | completed | `本次提交：支持单台模拟器更换镜像和配置` | `docs/evidence/DF-034/acceptance.md` |
| DF-035 | completed | `本次提交：完成官方镜像按需构建和真实多规格验收` | `docs/evidence/DF-035/acceptance.md` |
| DF-036 | completed | `本次提交：完成旧镜像受控停用和可用镜像选择` | `docs/evidence/DF-036/acceptance.md` |
| DF-037 | completed | `本次提交：完成 Phone 硬件模板和受控模拟器创建向导` | `docs/evidence/DF-037/acceptance.md` |

## 当前剩余条件

1. 新版 Alcor 发布真实 Run/RunAttempt OpenAPI 和 Device Farm 扩展输入后完成 ALCOR-001；
2. KI-005/KI-007 所列跨仓库接口字段由新版 Alcor 正式契约确认后再联调；
3. 所有后续状态变化继续保存验收证据并使用简洁中文 Git 提交。
