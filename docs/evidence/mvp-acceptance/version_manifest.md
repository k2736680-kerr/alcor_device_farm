# 验收版本清单

## Device Farm

- 分支：`master`；
- DF-022：`f1acdf5 加固设备权限审计和敏感数据`；
- DF-023：`ef28201 补齐设备指标部署和回滚手册`；
- DF-024：以本步骤提交为准；
- 构建产物：`device-farm-server`、`device-host-agent`、`dafit-farm-harness`；
- 接口契约：`openapi/device-farm-v1.yaml`。

## DaFit

- 仓库：`E:/AutoTestTools/Projects/dafit_auto_platform`；
- 分支：`main`；
- Farm 接入 commits：`2ae74ca`、`d211eb2`；
- 2026-08-03 当前 HEAD：`40a0af5`；
- collect-only：158 个执行实例。

## 外部组件基线

- PostgreSQL：17.x；
- DeviceFarmer/STF：3.7.9；
- RethinkDB：2.4.2；
- Docker Android 镜像：部署时必须使用已验证固定 tag/digest；
- Appium/UiAutomator2：真实 Host 版本待 E2 验收后冻结。
