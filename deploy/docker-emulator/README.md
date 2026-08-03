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

编辑配置：

```sh
sudoedit /etc/alcor-device-farm/host-agent.env
```

至少填写：

- `DEVICE_FARM_AGENT_SERVER_URL`；
- `DEVICE_FARM_AGENT_HOST_ID`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`；
- `DEVICE_FARM_DOCKER_IMAGE`；
- `DEVICE_FARM_DOCKER_ADVERTISE_HOST`。

再执行真实双设备验收：

```sh
set -a
. /etc/alcor-device-farm/host-agent.env
set +a
./scripts/verify-docker-emulator.sh
```

验收通过后启动：

```sh
sudo systemctl enable --now alcor-device-host-agent.service
sudo systemctl status alcor-device-host-agent.service
```

## 镜像基线说明

当前 Provider 对齐 `budtmo/docker-android` 的公开运行契约：容器内 Emulator serial 为 `emulator-5554`，Host ADB 使用容器端口 `5555`，持久化目录为 `/home/androidusr`，设备型号通过 `EMULATOR_DEVICE` 指定。参考上游基线提交为 `e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`。

部署时必须填写固定 tag 或 digest，不能使用 `latest`。DF-016 会在真实 Host 上记录最终镜像 digest 并完成 Image validation；在此之前不要把未验证镜像加入正式设备池。

DF-014 明确关闭镜像内 `APPIUM` 和 `WEB_VNC`。Appium Endpoint 在 DF-015 管理，远控在 DF-017/DF-018 复用 STF，避免提前形成第二套入口。
