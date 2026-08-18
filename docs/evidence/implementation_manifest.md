# DF-003～DF-046 与 ALCOR-001 实施和证据清单

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
| DF-031 | completed | `c9f09dc 加入管理员设备远程控制`、`8f458fb 修复设备远控连接超时与幂等关闭` | `docs/evidence/DF-031/acceptance.md` |
| DF-032 | completed | `d8f573d 按实际资源动态计算设备容量` | `docs/evidence/DF-032/acceptance.md` |
| DF-033 | completed | `79bff48 改为按设备池总目标动态扩缩容` | `docs/evidence/DF-033/acceptance.md` |
| DF-034 | completed | `48f2076 支持单台模拟器更换镜像和配置` | `docs/evidence/DF-034/acceptance.md` |
| DF-035 | completed | `5bbbf31 完成官方镜像按需构建和真实多规格验收` | `docs/evidence/DF-035/acceptance.md` |
| DF-036 | completed | `2c4ab80 完成旧镜像受控停用和可用镜像选择` | `docs/evidence/DF-036/acceptance.md` |
| DF-037 | completed | `6453034 完成Phone硬件模板和受控创建设备向导` | `docs/evidence/DF-037/acceptance.md` |
| DF-038 | completed | `56b7ae3 完成DF-038设备重建与真实验收` | `docs/evidence/DF-038/acceptance.md` |
| DF-039 | completed | `dd6a52c 形成DF-039 iOS设备农场设计` | `docs/evidence/DF-039/acceptance.md` |
| DF-040 | completed | `31fc0a2 完成DF-040平台中立设备域模型` | `docs/evidence/DF-040/acceptance.md` |
| DF-041 | completed | `ba25501 完成DF-041 macOS宿主机与iOS只读适配` | `docs/evidence/DF-041/acceptance.md` |
| DF-042 | completed | `aea220d 完成DF-042 iOS预约会话围栏`、`d485460 完成DF-042验收归档` | `docs/evidence/DF-042/acceptance.md` |
| DF-043 | completed | 本提交：`完成DF-043 iOS模拟器固定库存接入` | `docs/evidence/DF-043/acceptance.md` |
| DF-044 | completed | 本提交：`完成DF-044 iOS模拟器动态生命周期` | `docs/evidence/DF-044/acceptance.md` |
| DF-045 | completed | `完成DF-045 iOS设备域控制台` | `docs/evidence/DF-045/acceptance.md` |
| DF-046 | completed | 本提交：`完成DF-046 iOS模拟器远程控制` | `docs/evidence/DF-046/acceptance.md` |
| DF-047 | pending | — | `docs/evidence/DF-047/acceptance.md` |
| ALCOR-001 | completed | `9300fa2 完成Alcor设备农场统一入口`、`106e9dd 优化Alcor嵌入式设备页面` | `docs/evidence/ALCOR-001/readiness.md` |

## 当前结论

1. Android 第一版 DF-003～DF-038 和本地 ALCOR-001 已全部完成，冻结基线为 `106e9dd` 与 Tag `archive/android-baseline-2026-08-17`；
2. 第二版使用本地 `codex/device-farm-v2` 分支；DF-039～DF-046 已完成，下一步执行 DF-047 iOS 最终真实验收、运维回滚与 Android 回归；
3. 所有后续状态变化继续保存验收证据并使用简洁中文 Git 提交。
