# Linux KVM Host Agent 部署

## 前置条件

- x86_64 Linux；
- Docker Engine 和 Docker CLI；
- `/dev/kvm` 存在；
- 当前仓库可使用 Go 1.24+ 构建；
- Host 已通过设备农场北向 API 创建并取得 Host ID；
- Agent Token 通过部署密钥系统提供。

## 安装

在 Linux Host 的仓库根目录执行：

```sh
sudo ./scripts/install-device-host-agent.sh
```

脚本会：

1. 校验 Linux、Docker 和 `/dev/kvm`；
2. 创建无登录权限的 `device-farm` 系统用户，并加入 `docker`、`kvm` 组；
3. 构建并安装 `device-host-agent`；
4. 安装 systemd unit；
5. 首次安装时生成权限为 `0600` 的环境配置样例；
6. 只执行 `daemon-reload`，不会在 Token、Host ID、镜像未配置时自动启动服务。

正式发布时可设置 `DEVICE_FARM_VERSION`、`DEVICE_FARM_COMMIT` 和 `DEVICE_FARM_BUILD_DATE`，安装脚本会把版本信息写入 Agent 二进制，便于回滚核对。

编辑配置：

```sh
sudoedit /etc/alcor-device-farm/host-agent.env
```

至少填写：

- `DEVICE_FARM_AGENT_PROVIDER=docker`；
- `DEVICE_FARM_AGENT_SERVER_URL`；
- `DEVICE_FARM_AGENT_HOST_ID`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`；
- `DEVICE_FARM_AGENT_CONCURRENCY=1`（只限制 Agent 同时执行的 Provider Command 数量；设备目标扩缩容在 Console 修改，不需要改此值或重启 Agent）；
- `DEVICE_FARM_AGENT_DEVICE_SLOT_LIMIT=0`（默认不写死设备台数；非零值只作为额外安全上限）；
- `DEVICE_FARM_DOCKER_DATA_ROOT=/var/lib/docker`（Agent 从该文件系统读取实际剩余磁盘）；
- `DEVICE_FARM_DOCKER_IMAGE`；
- `DEVICE_FARM_DOCKER_ADVERTISE_HOST`。

Provider 没有默认值。漏配或填写未知值时 Agent 会直接退出，绝不会改用 Mock 冒充真实设备；正式 Linux Host 必须保持部署样例中的 `docker`。

Host Command 使用 300 秒租约，Agent 的单命令执行超时为 270 秒且必须短于租约，避免 Android 启动尚未结束时命令被重复领取。部署代码会在启动时拒绝超时大于或等于租约的不安全配置。

当前测试服务器使用一台模拟器验收：

```sh
set -a
. /etc/alcor-device-farm/host-agent.env
set +a
export DEVICE_FARM_DOCKER_INTEGRATION_COUNT=1
./scripts/verify-docker-emulator.sh
```

资源允许时可以把数量改为 `2` 执行多设备端口隔离扩展验收；生产是否还能创建由 Image 的运行规格以及 Host 实时 CPU、可用内存、Docker 数据盘剩余空间共同计算，不由该测试变量或 Agent 命令并发决定。资源不足时 Controller 保持缺口并返回明确的限制项和欠缺量，不会超卖后再让 Emulator 卡死。

验收通过后启动：

```sh
sudo systemctl enable --now alcor-device-host-agent.service
sudo systemctl status alcor-device-host-agent.service
```

## 镜像基线说明

当前 Provider 对齐 `budtmo/docker-android` 的公开运行契约：容器内 Emulator serial 为 `emulator-5554`，Host ADB 使用容器端口 `5555`，内置 Appium 使用容器端口 `4723`，持久化目录为 `/home/androidusr`，设备型号通过 `EMULATOR_DEVICE` 指定。参考上游基线提交为 `e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`。

部署时必须填写固定 tag 或 digest，不能使用 `latest`。DF-016 会在真实 Host 上记录最终镜像 digest 并完成 Image validation；在此之前不要把未验证镜像加入正式设备池。

Host Agent 会开启镜像内置 Appium、关闭 `WEB_VNC` 和上游匿名使用统计，并为每个 Emulator 随机发布独立 Host Appium 端口。Appium 必须通过 `/status` 健康检查后设备才能 ready；远控仍在 DF-017/DF-018 复用 STF，不使用镜像自带 VNC 形成第二套入口。
