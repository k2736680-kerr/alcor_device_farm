# DF-014 实施与验收证据

## 当前结论

DF-014 已按 ADR-0008 的单台测试环境范围完成真实 Linux KVM 验收。2026-08-04 在 Ubuntu 22.04 x86_64 服务器使用 `alcor-device-farm/android-emulator:16.0-api36-r3` 创建一台 Android 16/API 36 Emulator，ADB online、`sys.boot_completed=1`、discover 和受管资源清理全部通过。

真实 Docker 生命周期测试耗时 46.57 秒。测试结束后受管容器、网络和卷均为空；`/dev/kvm` 前后保持 `root:kvm 660`；服务器现有 `vega-face-search` 容器和设备农场专用 PostgreSQL 测试容器持续运行。多设备端口隔离保留自动化契约和 P2 扩展验收，不再阻塞当前 DF-014。

## 最终 Linux KVM 证据

```text
PASS Android 16 / API 36 / x86_64
PASS DEVICE_FARM_DOCKER_INTEGRATION_COUNT=1
PASS DEVICE_FARM_DOCKER_CPUS=4（上限）
PASS DEVICE_FARM_DOCKER_MEMORY=5g（上限）
PASS TestDockerProviderLinuxKVMIntegration (46.57s)
PASS ADB online、boot_completed=1
PASS 删除后 managed container/network/volume = 0
PASS /dev/kvm = root:kvm 660（前后未改变）
PASS 现有业务容器未停止、未重启
```

## 候选服务器脱敏预检证据

```text
PASS SSH 密码认证和已知主机指纹校验
PASS Ubuntu 22.04 x86_64、Docker Engine 28.1.1
PASS 12 逻辑核、15 GiB 内存、约 47 GiB 根分区可用
BLOCKED /dev/kvm 不存在
BLOCKED CPU 未暴露 vmx/svm
BLOCKED 内核记录 VMX disabled by BIOS
BLOCKED Docker Hub Registry 443 连接超时
SAFE 未安装软件、未重启、未创建或删除任何 Docker 资源
```

## 已完成交付

- 新增 Docker CLI Backend，不通过 shell 拼接命令，Server 不接触远程 Docker Socket；
- 新增 Docker Emulator Provider：create/start/stop/restart/rebuild/delete/discover；
- Provider 启动强制检查 Linux、可读写 KVM 字符设备和 Docker Engine，失败时明确返回 `KVM_UNAVAILABLE` 或 `DOCKER_UNAVAILABLE`；
- 镜像必须使用固定 tag 或 digest，显式拒绝 `latest`；
- 每个设备使用确定性容器名、专属 bridge network 和独立 data volume；
- 容器、网络和数据卷统一使用受管标签，删除只清理匹配的设备农场资源；
- Docker 自动分配空闲 Host ADB 端口，`serial`/`adb_endpoint` 使用 `<advertise_host>:<port>`；
- 每台容器配置 CPU、内存和 PID 限制，并只挂载指定 `/dev/kvm`；
- 健康检查真实执行容器内 `adb get-state` 和 `adb shell getprop sys.boot_completed`；
- Appium 健康保持未通过，不在 DF-014 伪造 ready，独立 Endpoint 在 DF-015 实现；
- Create/Delete 幂等，Rebuild 保留 Device/Image/capabilities 并提升 generation；
- Host Agent 必须显式配置 `DEVICE_FARM_AGENT_PROVIDER=mock|docker` 或 `--provider`，心跳明确上报规范化后的实际 Provider 类型；配置缺失或未知时拒绝启动，不会降级到 Mock；
- Agent create 命令透传 capabilities，并保留 Provider 的 retryable 分类；
- Agent 的 create/start/stop/restart/rebuild/inspect completion 返回稳定的 Provider Snapshot、连接和健康字段，为后续自动补齐 Controller 更新同一 Device 提供依据；
- 管理 API 的 restart/rebuild 不再调用 Server 内 Mock Provider，而是原子写 Device 状态、审计和持久化 Host Command，由 Host Agent 在本机执行真实 Provider；
- Agent restart 会重新等待 ADB、boot completed 和 Appium 全部健康后才回报成功；管理命令成功时依据完整快照回 ready，最终失败或结果不完整时自动 quarantined 并写健康事件；
- Host Agent 心跳现在会把已登记 Device 的 serial、ADB/Appium Endpoint、生命周期、健康状态和 `last_seen_at` 原子回写；只匹配同一 `host_id + provider_ref`，不会根据 Agent 自报创建 Device，也不会串绑其他 Host；
- 心跳状态更新复用 Device 状态机，进入 ready 前先更新 healthy；reserved/busy/recycling、quarantined/deleted 不会被 Agent 自动覆盖或恢复；
- serial、ADB Endpoint 或 Appium Endpoint 唯一冲突会回滚 Host 和 Device 的整笔心跳，并返回稳定的 `DEVICE_IDENTITY_CONFLICT`；
- 运行中但尚未通过 ADB/boot/Appium 的设备在 heartbeat 中上报为 `booting/unknown`，不会因为容器刚 running 就被错误标记为 ready；
- Agent 启动时先完成首次 heartbeat 再领取命令，避免短进程或退出竞态导致 Host 尚未上线就执行设备操作；首次心跳失败会记录并由周期心跳重试，不创建第二套状态真相；
- 新增 [Docker Emulator Provider 说明](../../docker_emulator_provider.md)；
- 新增 `scripts/verify-docker-emulator.sh` 和真实 Linux KVM 集成测试。
- 已核对 `budtmo/docker-android` 上游运行契约并把独立数据卷修正为 `/home/androidusr`；Host Agent 可配置容器内 ADB serial、ADB 端口、数据目录和 Emulator 设备型号；
- 新增 systemd unit、脱敏环境样例和 `scripts/install-device-host-agent.sh`，服务器可直接安装但不会在密钥和镜像未配置时自动启动。

## 本地自动化验证

```powershell
go test -count=1 -v ./internal/providers/docker ./internal/agent ./internal/hostcommand ./cmd/device-host-agent
./scripts/verify-migrations.ps1 -RunRepositoryTests
```

已通过：

```text
PASS TestCLIBackendCreatesContainerWithKVMResourceLimitsAndRandomADBPort
PASS TestCLIBackendInspectDecodesPublishedPortAndLabels
PASS TestDockerProviderLifecycleUsesUniquePortsAndCleansResources
PASS TestDockerProviderDoesNotSilentlyRunWithoutKVMOrFixedImage
PASS TestDockerResourceNamesAreStableAndBounded
PASS TestBuildProviderRequiresExplicitProvider
PASS TestBuildProviderAcceptsExplicitMock
PASS TestBuildProviderRejectsUnknownProvider
PASS TestAgentCreateCompletionReturnsProviderSnapshot
PASS TestManagementAPICompleteMockFlow（restart/rebuild Host Command 幂等、成功恢复和失败隔离）
PASS TestProviderHeartbeatStatusDoesNotMarkBootingDeviceReady
PASS TestAgentCreatePassesCapabilitiesAndPreservesProviderRetryability
PASS TestHeartbeatBringsOfflineHostOnline
PASS TestHeartbeatDoesNotCreateOrCrossBindDiscoveredDevice
PASS TestHeartbeatPreservesReservationAndTerminalLifecycleTruth
PASS TestHeartbeatIdentityConflictsRollbackEntireTransaction
PASS internal/providers/docker
PASS internal/agent
PASS internal/hostcommand
PASS cmd/device-host-agent
```

真实测试 `TestDockerProviderLinuxKVMIntegration` 在本机按设计显示 SKIP：

```text
set DEVICE_FARM_DOCKER_INTEGRATION=1 on a Linux KVM host
```

该 SKIP 是未完成真实验收的明确证据，不计为 DF-014 通过。

## Linux KVM 服务器验收步骤

服务器需预装 Git、Go 1.24+、Docker Engine，并确保运行 Agent 的用户可读写 `/dev/kvm`。

```sh
export DEVICE_FARM_GO=go
export DEVICE_FARM_DOCKER_IMAGE='<固定 tag 或 sha256 digest>'
export DEVICE_FARM_DOCKER_ADVERTISE_HOST='<服务器设备内网地址>'
export DEVICE_FARM_DOCKER_BIND_ADDRESS='<127.0.0.1、设备内网 IP 或按网络策略使用 0.0.0.0>'
./scripts/verify-docker-emulator.sh
```

必须真实满足：

1. 当前 Host 创建并启动一台 Android 16 Emulator；
2. 设备连接信息包含明确 serial 和 ADB Host 端口；
3. 设备达到 container running、ADB online、`sys.boot_completed=1`；
4. `discover` 返回正确 Host/Device/Image 元数据；
5. 删除后对应容器、受管网络和独立数据卷全部为空；
6. 无 KVM、Docker 不可用、浮动镜像、ADB 失败、boot timeout 或清理残留时命令必须失败。

## 完成说明

阻塞条件已经解除，DF-014 状态改为 `completed`。后续增加模拟器数量时只调整配置并执行 P2 多设备扩展验收，不修改 Provider 架构。
