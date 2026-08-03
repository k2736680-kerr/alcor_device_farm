# DF-015 实施与验收证据

## 当前结论

Appium Endpoint 分配、健康 Adapter、Host Agent 配置、单元测试和 Linux KVM 双设备真实验收入口已经完成。本机没有 Docker、可用 WSL 和 Linux `/dev/kvm`，不能真实启动两台 Emulator 或创建 UiAutomator2 Session，因此 DF-015 当前状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- 新增 `internal/adapters/appium`，只实现 Appium `/status` 健康探针，不实现业务 WebDriver；
- 健康探针支持 Appium 根路径和受控 base path，拒绝用户信息、查询参数、非 HTTP(S) 和重定向；
- Docker Provider 为每个容器同时随机发布独立 ADB 和 Appium Host 端口；
- `ConnectionInfo` 返回明确的 serial、ADB Endpoint 和 Appium Endpoint；
- 连接信息额外区分 `appium_udid`：容器外 ADB 使用 Host Endpoint，容器内 Appium 使用本地 `emulator-5554`，避免把两个不同网络空间的标识混用；
- Host Agent 按上游 `budtmo/docker-android` 契约设置 `APPIUM=true`，继续关闭镜像内 `WEB_VNC`；
- 只有容器、ADB、boot completed 和 Appium `value.ready=true` 全部通过才满足 `Health.Ready()`；
- Appium 未健康时设备不会被 Agent 心跳错误上报为 ready；
- 新增 `scripts/verify-appium-endpoints.sh` 和真实双 Session 集成测试；
- 新增 [Appium Adapter 说明](../../appium_adapter.md)，部署环境样例包含端口和健康超时参数；
- 已再次核对 DaFit：现有 ADB 和 Appium Session 共用 `ANDROID_UDID`。DF-019 只需让 `core/driver/appium_session.py` 优先读取可选 `APPIUM_UDID`，原本地模式继续回退 `ANDROID_UDID`；不复制或重写 WebDriver 实现。

## 本地自动化验证

```powershell
go test -count=1 -v ./internal/adapters/appium ./internal/providers/docker ./cmd/device-host-agent
./scripts/dev.ps1 -Task check
```

当前通过：

```text
PASS internal/adapters/appium
PASS TestCLIBackendCreatesContainerWithKVMResourceLimitsAndRandomADBPort
PASS TestCLIBackendInspectDecodesPublishedPortAndLabels
PASS TestDockerProviderLifecycleUsesUniquePortsAndCleansResources
PASS TestDockerProviderRequiresIndependentHealthyAppiumEndpoints
PASS internal/providers/docker
PASS cmd/device-host-agent
```

真实测试在本机按设计显示 SKIP：

```text
set DEVICE_FARM_APPIUM_INTEGRATION=1 on a Linux KVM host
```

该 SKIP 只表示代码可编译和本地契约通过，不能替代两台真实 Emulator 的 Appium 验收。

## Linux KVM 服务器验收

```sh
export DEVICE_FARM_GO=go
export DEVICE_FARM_DOCKER_IMAGE='<固定 tag 或 digest>'
export DEVICE_FARM_DOCKER_ADVERTISE_HOST='<Worker 可访问的 Host 内网地址>'
export DEVICE_FARM_DOCKER_BIND_ADDRESS='<设备内网 IP 或按网络策略使用 0.0.0.0>'
./scripts/verify-appium-endpoints.sh
```

必须真实满足：

1. 两台 Emulator 的 ADB 和 Appium Host 端口均互不冲突；
2. 两个 Appium `/status` 均返回 `value.ready=true`；
3. 两个 UiAutomator2 Session 可以同时建立；
4. 每个 Session 只控制所属容器设备，错误 UDID 不会连到另一台；
5. 任一 Appium 不健康时对应 Device 不进入 ready；
6. Session 和测试设备删除后无容器、网络、数据卷或端口残留。

## 阻塞解除条件

在真实 Linux KVM 服务器执行 `scripts/verify-appium-endpoints.sh` 全部通过并保存脱敏输出后：

- 将 DF-015 从 `blocked` 改为 `completed`；
- 记录固定 Emulator 镜像 digest、Appium 和 UiAutomator2 版本；
- 确认两套 Endpoint 从 DaFit/Worker 所在网络可达，但不对公网开放；
- 单独提交真实验收结果。
