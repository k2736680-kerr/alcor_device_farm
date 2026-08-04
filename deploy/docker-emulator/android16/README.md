# Android 16 模拟器镜像

该目录保存 Android 16 / API 36 / x86_64 模拟器的可重复构建入口。正式交付不依赖仓库 `tmp/` 目录。

## 固定版本

- 上游：`budtmo/docker-android`
- 上游提交：`e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`
- Appium 基础版本：`3.5.2-p0`
- Android：`16.0` / API `36`
- Emulator：由构建时 Android SDK 仓库解析，验收镜像为 `37.1.11`
- UiAutomator2：`8.2.2`
- 默认镜像标签：`alcor-device-farm/android-emulator:16.0-api36-r3`

## 构建

Linux Docker Host 需要能够访问 GitHub、Docker Hub 和 Android SDK 仓库：

```sh
./deploy/docker-emulator/android16/build.sh
```

构建脚本只使用临时目录，结束后自动删除克隆源码。可以通过环境变量替换内部镜像标签，但不得使用 `latest`：

```sh
DEVICE_FARM_ANDROID_BASE_IMAGE='registry.example/alcor/docker-android-base:v3.5.2-p0' \
DEVICE_FARM_ANDROID_OUTPUT_IMAGE='registry.example/alcor/android-emulator:16.0-api36-r3' \
./deploy/docker-emulator/android16/build.sh
```

构建完成后应把镜像推送到内部 Registry，并在 `device_images.docker_image` 保存明确标签或 digest、在 `docker_digest` 保存不可变摘要。

## 项目补丁

`upstream.patch` 和 `uiautomator2-preinstall.patch` 只处理设备农场需要的差异：

- 增加 Android 16/API 36；
- 延长 Emulator 启动等待；
- 不修改宿主机 `/dev/kvm` 的属主和权限；
- Android package/settings 服务可用后再启动 Appium；
- Appium 启动前按顺序预装并校验固定版本的 UiAutomator2 Server APK，避免 API 36 上并发安装压垮 package 服务；
- 完全关闭上游匿名使用统计并移除 Google Form ID。

容器运行时只映射 `/dev/kvm`，不使用 `--privileged`。Provider 继续负责 CPU、内存、PID、端口、网络和数据卷限制。

## 当前单机验收配置

当前测试服务器只启动一台模拟器：

```text
min_ready=1
max_instances=1
max_concurrency=1
DEVICE_FARM_DOCKER_MEMORY=5g
DEVICE_FARM_DOCKER_CPUS=4
```

`5g` 和 `4` 分别是容器内存、CPU 上限，不是启动时一次性预占；Android 16 实测 4 GiB/2 核会在 APK 安装期间出现系统服务不稳定，当前单机配置仍为服务器保留充足余量。架构和接口仍支持后续把数量调大以及接入 USB 真机。

Android 16 验收创建真实 UiAutomator2 Session，但设置 `appium:skipDeviceInitialization=true` 和 `appium:ignoreHiddenApiPolicyError=true`，跳过设备农场不使用且在 API 36 上不稳定的 Appium Settings 辅助初始化，并忽略 Android settings 服务重启时的 hidden-api 策略恢复错误。UiAutomator2 Server 的安装、instrumentation 启动和 Session 删除仍会真实执行。DaFit 已通过 `APPIUM_SKIP_DEVICE_INITIALIZATION=1` 提供同一可选能力；hidden-api 开关在 DF-019 的薄适配中配置，不进入设备农场业务逻辑。

## 第三方许可

上游许可原文保存在 `UPSTREAM_LICENSE.md`。本项目补丁关闭上游数据采集；部署时仍需由公司按内部开源合规流程复核许可证。
