# MVP 验收环境

验收日期：2026-08-03（E0 初始）、2026-08-06（E1/E2 最终）

Device Farm commit：执行本地门禁时记录在 `artifacts/local-gate.txt`；DF-024 代码提交后以该提交及后续 DF-025 提交补充复核。

## E0 本地环境

- 操作系统：Windows 11；
- Go：1.26.5，本项目 `go.mod` 兼容线为 Go 1.24+；
- PostgreSQL：17.10 Windows 免安装实例，由验收脚本在项目 `tmp` 下临时创建并清理；
- Provider：Mock；
- Python：3.12.10；
- DaFit 当前分支：`main`，collect-only 当前基线 158；
- 本机 Docker、Linux KVM、systemd：不可用；
- WSL2：登记有 `Ubuntu`，但启动时因注册的 `ext4.vhdx` 路径不存在而失败，不能作为 Linux 验收环境；
- 内网候选服务器：Ubuntu 22.04、Docker Engine 28.1.1 可用，但 BIOS 关闭 VMX、没有 `/dev/kvm`，Docker Hub Registry 443 连接超时；只完成脱敏只读预检，未改动现有业务服务；
- STF、远程 Appium、两台 Docker Emulator：仍不可用。

## 2026-08-03 E1/E2 初始阻塞

本机不能完成以下真实验收：

- `/dev/kvm`、Docker Engine 和两台 Emulator；
- Host Agent systemd/容器部署；
- ADB boot、Appium 双 Session、STF inventory/claim/release/remoteConnect；
- DaFit 真实成功、失败、并发、中断和跨任务数据隔离；
- Prometheus 告警、8 小时或 50 次完整循环；
- 备份恢复到新 PostgreSQL 和版本回滚。

这些项目必须在 Linux KVM 服务器继续执行，Mock 证据不改变其 `BLOCKED` 结论。

## 2026-08-04 环境更新

- 内网服务器已启用 VMX 并提供可读写 `/dev/kvm`；
- DF-014～DF-016 已完成 Android 16/API 36、ADB、boot、Appium、自动补池、预约、重建和清理真实验收；
- 当前正式镜像 `alcor-device-farm/android-emulator:16.0-api36-r3` 已保留；
- Docker daemon 的失效代理仍导致直接 `docker pull` 不可用，但不再影响已保留镜像运行；
- DF-017～DF-024 和 DF-028 的 STF、DaFit、故障、运维及 Web 真实验收仍需继续执行。

## 2026-08-06 E1/E2 最终环境

- 服务器：Ubuntu 22.04，Linux `5.15.0-186-generic`，12 vCPU，约 15 GiB 内存；
- Docker Engine：28.1.1；`/dev/kvm` 为 `660 root:kvm`，Host Agent 用户具有 kvm/docker 组权限；
- Server：`alcor-device-farm:df021-stf-recovery10-20260806`，Image ID `sha256:4f8605935c3d0be2f8d6e2fa149efb9d797fef74d86097748bc8c3bc5e23b91a`；
- Host Agent SHA-256：`bf457d76eddc39358d0664dd0702d4ec7a38826a8679e24417a63ac6a3b27cff`；
- Emulator：Android 16/API 36，镜像 `alcor-device-farm/android-emulator:16.0-api36-r3`，单台验收配置符合 ADR-0008；
- Appium：3.5.2；STF：3.7.9；RethinkDB：2.4.2；PostgreSQL：16-alpine；
- DaFit：`main@ca6430c`，collect-only 158 个执行实例；
- 真实证据：DF-019～DF-023 验收文档及服务器 `/home/kerr/df021-acceptance-20260806/`～`df024-acceptance-20260806/`；
- Docker daemon 失效代理仍阻止直接 Docker Hub pull；已保留正式镜像，Prometheus 2.54.1 使用官方 GitHub 发布包并校验 SHA-256，因此不阻塞验收。

最终环境已覆盖 Emulator、ADB、Appium、STF、DaFit、50 次稳定性、数据隔离、Token 轮换、告警、备份恢复、旧版本回滚和数据库断连恢复。Device Farm Console 的 E3 浏览器验收继续由 DF-028 执行。
