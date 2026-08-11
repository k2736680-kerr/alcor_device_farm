# 官方 Android System Image 按需准备

本目录是 DF-035 唯一的镜像准备入口。管理员在 Console 中选择 Build Agent 已同步的 Android SDK 稳定频道条目和运行规格；Server 只下发固定的包名、官方 revision 与既有验证命令，不接受 URL、Shell 片段、Registry 凭证或 Docker 参数。

## 文件职责

- `build-agent.sh`：Host Agent 配置的唯一入口，只接受 `catalog` 或 `prepare <package> <revision>`。
- `sdk_catalog.py`：只解析固定 `sdkmanager --list --channel=0` 的 `Available Packages`，候选限制为 API 33～36、`google_apis/google_play`、`x86_64`。
- `prepare.sh`：再次校验包名和 revision，调用内部构建、推送，并返回不可变 digest 与实际共享镜像层大小。
- `build.sh`：只接受 `prepare.sh` 设置的受控 selector，使用固定上游 commit 和一份参数化 Dockerfile。
- `Dockerfile`：安装选定 System Image，核对实际安装 revision，并用 `avdmanager` 创建/删除构建检查 AVD；复用既有 Appium、UiAutomator2 与启动补丁。

没有批量登记四个 `device_images` 的兼容入口。目录同步不会构建镜像，也不会创建 Emulator；构建成功后仍必须经过真实 digest、KVM 启动、ADB、STF 注册与 Appium 健康验证，Server 才创建状态为 `ready` 的 `device_images` 记录。

## Build Agent 配置

Linux KVM Host 必须提供固定版本的 Android command-line tools，并把受信任脚本配置给现有 Host Agent：

```sh
DEVICE_FARM_IMAGE_PREPARE_SCRIPT=/opt/alcor-device-farm/deploy/docker-emulator/images/build-agent.sh
DEVICE_FARM_IMAGE_PREPARE_TIMEOUT=2h
DEVICE_FARM_SDKMANAGER_BINARY=/opt/android-sdk/cmdline-tools/19.0/bin/sdkmanager
DEVICE_FARM_SDKMANAGER_VERSION=19.0
DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY=registry.internal/alcor/android-emulator
```

`DEVICE_FARM_SDKMANAGER_BINARY` 必须是 Build Agent 本地的受控绝对路径；Server 和 Console 均不能覆盖它。Registry 登录信息只存在于 Build Agent 部署环境中。正式同步由 Console 的“同步官方目录”触发；正式构建由“准备镜像”触发，不直接运行本目录脚本。

可选配置：

- `DEVICE_FARM_ANDROID_SOURCE_CACHE`：固定上游 commit 的只读 Git 缓存，HEAD 不匹配即拒绝使用。
- `DEVICE_FARM_ANDROID_BASE_IMAGE`：明确标签或 digest，禁止 `latest`。
- `DEVICE_FARM_ANDROID_SKIP_BASE_BUILD=1`：只在已核对本地基础镜像时跳过基础镜像重建。

## 缓存、资源与回滚

镜像标签包含官方 revision，发布过的标签不得覆盖或改指向另一个 digest。Server 以 `docker_digest + runtime_profile` 复用已验证缓存；同 digest 的共享镜像层在同一 Host 只计一次，`data_disk_mb` 仍按每台 Device 单独预留。

官方目录 revision 更新只显示“官方已更新”，不会替换 Pool 默认镜像或重装既有 Device。回滚时在 Console 把 Pool 默认 Image 切回上一条已验证记录；需要恢复已重装设备时，通过“编辑配置”选择旧 Image，确认清空数据后走同一可恢复 reimage 链路。失败的新构建/验证只保留准备状态与错误码，不产生可用 Image。
