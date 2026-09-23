# ADR-0020：冻结 Android 第一版并使用独立本地分支开发第二版

## 状态

已接受。

## 背景

DF-000～DF-038 和本地 ALCOR-001 已完成，当前 `master@106e9dd` 是通过真实 Linux KVM、Android Emulator、STF、Appium、DaFit、Console 和 Alcor 统一入口验证的稳定基线。下一阶段准备扩展多平台宿主机和 iOS。如果继续直接在 `master` 上试验，稳定归档基线与第二版开发状态会混在同一分支，增加恢复和审计难度。

## 决策

- Android 第一版冻结在 commit `106e9dd7c83b8034fbf97baf7bde3979a9afcb10` 和 Tag `archive/android-baseline-2026-08-17`；
- 本地 `master` 保留为第一版基线，不再承载第二版日常开发；
- 第二版从上述 Tag 创建并固定使用本地分支 `codex/device-farm-v2`；当前不设置 upstream、不推送远端，远端分支与合并策略以后单独决定；
- 第二版仍沿用连续 DF 编号、单任务验收证据和简洁中文提交；第一个任务为 DF-039，只完成多平台宿主机与 iOS 接入设计；
- 第二版不创建另一套仓库、数据库、设备池或 Console。现有 Device、Host、Pool、Reservation、Scheduler、Reaper、审计和 Alcor Adapter 继续作为内核；
- Appium Device Farm 只作为 DF-039 中评估的宿主机侧候选组件。未经专项 ADR 和验收，不得让它取代 PostgreSQL 预约真相或独立分配设备；
- 紧急修复第一版时从归档 Tag 单独创建修复分支，验证后再决定是否移植到第二版，不直接把第二版未完成能力带回第一版。

## 后果

- 第一版可以通过 Tag、Git bundle 和源码归档稳定恢复；
- 第二版允许跨多个本地提交持续开发，又不会改变第一版 `master` 的语义；
- 现有只允许 `master` 的证据门禁需要同时允许 `codex/device-farm-v2`，并继续拒绝未登记分支；
- iOS 功能必须先通过 DF-039 完成复用、职责、数据模型、真实宿主机和验收设计，之后才能拆分实现任务。
