# DF-015 实施与验收证据

## 当前结论

DF-015 已按 ADR-0008 的单台测试环境范围完成真实验收。Android 16/API 36 Emulator 的 Appium `/status` 健康检查通过，UiAutomator2 8.2.2 Session 创建成功，测试结束后 Session、容器、网络和卷全部清理。

最终镜像为 `alcor-device-farm/android-emulator:16.0-api36-r3`，本机镜像 ID 为 `sha256:8afadfa4c342194c360edaf8302fb082ca29896002fcd9a1c44e494fd550c400`。真实 Appium 验收耗时 149.01 秒；容器使用 4 核、5 GiB 上限，实际资源按需使用。

Android 16 使用 `skipDeviceInitialization`、`ignoreHiddenApiPolicyError` 和已校验 UiAutomator2 Server 预装链路，规避 API 36 的 Appium Settings/hidden-api 辅助故障；真实 UiAutomator2 instrumentation 和 Session 没有跳过。

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

该 SKIP 只表示代码可编译和本地契约通过，不能替代 ADR-0008 规定的真实 Emulator Appium 验收；该验收现已在 Linux KVM 服务器通过。

## Linux KVM 服务器验收

```sh
export DEVICE_FARM_GO=go
export DEVICE_FARM_DOCKER_IMAGE='<固定 tag 或 digest>'
export DEVICE_FARM_DOCKER_ADVERTISE_HOST='<Worker 可访问的 Host 内网地址>'
export DEVICE_FARM_DOCKER_BIND_ADDRESS='<设备内网 IP 或按网络策略使用 0.0.0.0>'
./scripts/verify-appium-endpoints.sh
```

必须真实满足：

1. 当前 Emulator 返回明确且独立的 ADB、Appium Endpoint；
2. Appium `/status` 返回 `value.ready=true`；
3. UiAutomator2 Session 可以建立并删除；
4. Android 版本为 16、API Level 为 36；
5. 任一 Appium 不健康时对应 Device 不进入 ready；
6. Session 和测试设备删除后无容器、网络、数据卷或端口残留。

## 最终 Linux KVM 证据

```text
PASS Android 16 / API 36
PASS Appium 3.5.2 / UiAutomator2 8.2.2
PASS TestDockerProviderLinuxKVMAppiumIntegration (149.01s)
PASS Appium /status healthy
PASS UiAutomator2 Session 创建和删除
PASS managed container/network/volume = 0
PASS /dev/kvm = root:kvm 660（前后未改变）
PASS 现有业务容器持续运行
```

DF-015 状态改为 `completed`。多设备并发 Session 和错误 UDID 隔离保留 P2 扩展验收，不阻塞当前单机交付。
