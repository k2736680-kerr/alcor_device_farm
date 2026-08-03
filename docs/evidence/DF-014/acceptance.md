# DF-014 实施与验收证据

## 当前结论

Docker Emulator Provider 的代码、Host Agent 接入、配置说明、Linux KVM 双设备集成测试和验收脚本已经完成。本机没有 Docker CLI/Engine；WSL Ubuntu 虚拟磁盘路径损坏，无法启动，也没有可验证的 `/dev/kvm`。因此 DF-014 当前状态为 `blocked`，不能标记 `completed`，也不能用 Fake Backend 或 Mock Provider 代替真实 Linux KVM 验收。

待提供其他 Linux KVM 服务器后，不需要调整现有架构，只需部署当前 Host Agent、注入固定 Emulator 镜像和 Host 地址，然后执行本文件中的真实验收命令。

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
- Host Agent 支持 `DEVICE_FARM_AGENT_PROVIDER=mock|docker`，心跳明确上报实际 Provider 类型；
- Agent create 命令透传 capabilities，并保留 Provider 的 retryable 分类；
- Agent 的 create/start/stop/restart/rebuild/inspect completion 返回稳定的 Provider Snapshot、连接和健康字段，为后续自动补齐 Controller 更新同一 Device 提供依据；
- 运行中但尚未通过 ADB/boot/Appium 的设备在 heartbeat 中上报为 `booting/unknown`，不会因为容器刚 running 就被错误标记为 ready；
- Agent 启动时先完成首次 heartbeat 再领取命令，避免短进程或退出竞态导致 Host 尚未上线就执行设备操作；首次心跳失败会记录并由周期心跳重试，不创建第二套状态真相；
- 新增 [Docker Emulator Provider 说明](../../docker_emulator_provider.md)；
- 新增 `scripts/verify-docker-emulator.sh` 和真实 Linux KVM 集成测试。
- 已核对 `budtmo/docker-android` 上游运行契约并把独立数据卷修正为 `/home/androidusr`；Host Agent 可配置容器内 ADB serial、ADB 端口、数据目录和 Emulator 设备型号；
- 新增 systemd unit、脱敏环境样例和 `scripts/install-device-host-agent.sh`，服务器可直接安装但不会在密钥和镜像未配置时自动启动。

## 本地自动化验证

```powershell
go test -count=1 -v ./internal/providers/docker ./internal/agent ./cmd/device-host-agent
```

已通过：

```text
PASS TestCLIBackendCreatesContainerWithKVMResourceLimitsAndRandomADBPort
PASS TestCLIBackendInspectDecodesPublishedPortAndLabels
PASS TestDockerProviderLifecycleUsesUniquePortsAndCleansResources
PASS TestDockerProviderDoesNotSilentlyRunWithoutKVMOrFixedImage
PASS TestDockerResourceNamesAreStableAndBounded
PASS TestAgentCreateCompletionReturnsProviderSnapshot
PASS TestProviderHeartbeatStatusDoesNotMarkBootingDeviceReady
PASS TestAgentCreatePassesCapabilitiesAndPreservesProviderRetryability
PASS internal/providers/docker
PASS internal/agent
cmd/device-host-agent [no test files]
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

1. 同一 Host 创建并启动两台 Android Emulator；
2. 两台设备的 serial 和 ADB Host 端口不同；
3. 两台设备均达到 container running、ADB online、`sys.boot_completed=1`；
4. `discover` 返回两台正确 Host/Device/Image 元数据；
5. 删除后对应容器、受管网络和独立数据卷全部为空；
6. 无 KVM、Docker 不可用、浮动镜像、ADB 失败、boot timeout 或清理残留时命令必须失败。

## 阻塞解除条件

在真实 Linux KVM 服务器执行 `scripts/verify-docker-emulator.sh` 全部通过，并把脱敏输出保存到本目录后：

- 将 DF-014 从 `blocked` 改为 `completed`；
- 记录实际 Docker Engine、Emulator 镜像 tag/digest、CPU/内存和启动时间；
- 确认没有容器、网络、数据卷或完整设备序列号残留；
- 单独提交真实验收结果；
- 然后才进入 DF-015 Appium Endpoint 和健康 Adapter。
