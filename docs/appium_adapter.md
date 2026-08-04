# Appium Endpoint 和健康 Adapter

## 1. 目标与边界

设备农场复用 Android Emulator 镜像已有的 Appium 2.x 和 UiAutomator2，不实现 WebDriver、页面动作、断言、截图或业务 Session。DaFit 和未来 Alcor Android Executor 继续创建真正的 WebDriver Session；设备农场只提供明确的设备 UDID、独立 Appium Endpoint 和健康状态。

## 2. Endpoint 模型

每个 Docker Emulator 容器内只运行一台 `emulator-5554`，并启用镜像已有的 Appium Server：

```text
Emulator 容器 A: 5555/ADB + 4723/Appium → Host 随机端口 A
Emulator 容器 B: 5555/ADB + 4723/Appium → Host 随机端口 B
```

Docker Provider 为 ADB 和 Appium 分别请求随机 Host 端口，返回：

```json
{
  "serial": "device-host.internal:31000",
  "adb_endpoint": "device-host.internal:31000",
  "appium_endpoint": "http://device-host.internal:32000",
  "appium_udid": "emulator-5554"
}
```

每个 Appium 进程只看到本容器的 Android Emulator，因此两台设备不会共享 Appium 进程或默认选择同一台设备。容器外 ADB 使用 Host Endpoint，容器内 Appium 使用本地 `emulator-5554`，两者必须明确区分：DF-019 的 DaFit 薄适配分别注入 `ANDROID_ADB_SERIAL=<adb_endpoint>`、`ANDROID_UDID=<appium_udid>` 和 `APPIUM_SERVER=<appium_endpoint>`，不能自行挑选设备。本地模式下 `ANDROID_ADB_SERIAL` 缺省时仍回退 `ANDROID_UDID`，保证原有本地入口不变。

## 3. 健康规则

设备进入 ready 必须依次满足：

1. Docker 容器 running；
2. 容器内 ADB `get-state=device`；
3. `sys.boot_completed=1`；
4. 对该设备自己的 Appium Endpoint 请求 `GET /status`；
5. HTTP 成功且响应包含 `value.ready=true`。

Appium 超时、拒绝连接、重定向、非 2xx、非法 JSON 或缺少 `value.ready` 都视为 `APPIUM_UNHEALTHY`。未通过时 Agent 上报 booting/unknown，Server 不会把设备放入可调度 ready 池。

## 4. 配置

```text
DEVICE_FARM_DOCKER_APPIUM_PORT=4723
DEVICE_FARM_APPIUM_HEALTH_TIMEOUT=5s
```

`DEVICE_FARM_DOCKER_BIND_ADDRESS` 同时控制 ADB 和 Appium Host 端口绑定。默认 `127.0.0.1`；如果 Server、STF 或 DaFit Worker 位于其他主机，只能改为设备内网地址或受防火墙保护的 `0.0.0.0`，禁止公网暴露。

## 5. 验收

本地无 Docker 环境可执行：

```powershell
go test -count=1 -v ./internal/adapters/appium ./internal/providers/docker ./cmd/device-host-agent
```

Linux KVM Host 执行：

```sh
export DEVICE_FARM_DOCKER_IMAGE='<固定 tag 或 digest>'
export DEVICE_FARM_DOCKER_ADVERTISE_HOST='<Worker 可访问的 Host 内网地址>'
./scripts/verify-appium-endpoints.sh
```

当前真实验收创建一台 Android 16 Emulator，等待 Appium 健康，使用 UiAutomator2 创建并删除真实 Session。设置 `DEVICE_FARM_DOCKER_INTEGRATION_COUNT=2` 时继续验证两套 Endpoint 隔离和错误 UDID；该扩展验收不影响当前单机 P0。测试结束必须删除 Session、容器、网络和数据卷。

Android 16/API 36 的 Appium Settings 辅助初始化和 hidden-api 策略恢复在当前镜像中不稳定，验收使用 `appium:skipDeviceInitialization=true`、`appium:ignoreHiddenApiPolicyError=true`，但不会跳过 UiAutomator2 Server 安装和 Session。DaFit 已有 `APPIUM_SKIP_DEVICE_INITIALIZATION=1` 开关，hidden-api 选项在 DF-019 薄适配中配置；正式用例是否启用由执行器决定，设备农场不执行页面动作，也不在 Agent 中硬编码业务 capability。

## 6. 避免事项

- 不在 Server 启动 Appium，也不把 Docker Socket交给 Server；
- 不让多台 Emulator 共用一个未经设备绑定的 Appium 进程；
- 不把容器 running 或 ADB online 当成 Appium ready；
- 不在 Adapter 中实现 DaFit 已有的 WebDriver Session 和测试动作；
- 不把容器外 ADB Endpoint 当作容器内 Appium UDID；
- 不返回 Appium 管理凭证或带用户信息、查询 Token 的 Endpoint；
- 不使用镜像浮动 `latest`，Appium/UiAutomator2 最终版本随 DF-016 镜像验证固定。
