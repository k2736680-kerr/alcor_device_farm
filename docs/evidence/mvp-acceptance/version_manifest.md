# 验收版本清单

> 历史快照说明：本清单记录 DF-024 验收时的版本，不是 Android 第一版最终 Head。最终冻结基线为 `106e9dd7c83b8034fbf97baf7bde3979a9afcb10`，Tag `archive/android-baseline-2026-08-17`；完整归档校验见 `docs/evidence/android_v1_archive.md`。

## Device Farm

- 分支：`master`；
- DF-022：`f1acdf5 加固设备权限审计和敏感数据`；
- DF-023：`ef28201 补齐设备指标部署和回滚手册`；
- DF-024：`392c93a 整理设备农场全量验收证据`；
- DF-022 真实验收：`56f5c2a 完成DF-022真实权限审计和秘密验收`；
- DF-023 真实验收：`70870c8 完成DF-023真实部署告警和回滚验收`；
- DF-024 真实验收：`完成DF-024设备域MVP全量验收`；
- DF-025：以本步骤提交为准，冻结契约 `1.0.0`；
- 构建产物：`device-farm-server`、`device-host-agent`、`dafit-farm-harness`、`device-farm-adapter-mock`；
- 接口契约：`openapi/device-farm-v1.yaml`。

## DaFit

- 仓库：`E:/AutoTestTools/Projects/dafit_auto_platform`；
- 分支：`main`；
- Farm 接入 commits：`2ae74ca`、`d211eb2`；
- 2026-08-06 当前 HEAD：`ca6430c`；
- collect-only：158 个执行实例。

## 外部组件基线

- PostgreSQL：生产 `16-alpine`，E0 临时门禁为 17.10；
- DeviceFarmer/STF：3.7.9；
- RethinkDB：2.4.2；
- Docker Android 镜像：`alcor-device-farm/android-emulator:16.0-api36-r3`；
- Appium：3.5.2；
- Server：`alcor-device-farm:df021-stf-recovery10-20260806`；
- 回滚验证镜像：`alcor-device-farm:df021-stf-recovery9-20260806`；
- Host Agent SHA-256：`bf457d76eddc39358d0664dd0702d4ec7a38826a8679e24417a63ac6a3b27cff`；
- Prometheus：2.54.1，官方发布包 SHA-256 `31715ef65e8a898d0f97c8c08c03b6b9afe485ac84e1698bcfec90fc6e62924f`。
