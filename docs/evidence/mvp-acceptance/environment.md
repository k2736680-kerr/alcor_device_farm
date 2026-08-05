# MVP 验收环境

验收日期：2026-08-03

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

## E1/E2 阻塞

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
