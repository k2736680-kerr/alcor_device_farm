# Docker Emulator Provider

## 1. 作用和边界

Docker Emulator Provider 只运行在 Device Host Agent 本机，负责 Android Emulator 容器、专属网络、独立数据卷、KVM 挂载、资源限制和 ADB 连接信息。Server 不访问远程 Docker Socket，Provider 不执行 DaFit 用例，也不实现 Appium、STF 或 Alcor 业务对象。

当前 Server 管理面继续使用 Mock Provider 做纯控制面开发。真实 Docker 生命周期由 Host Agent 选择 `DEVICE_FARM_AGENT_PROVIDER=docker` 后执行；后续 DF-016 Pool Controller 负责把已验证 Image 转成 Host Command，不把 Docker 操作搬回 Server。

## 2. Host 和镜像契约

真实运行必须同时满足：

- Linux Host；
- `/dev/kvm` 是 Agent 用户可读写的字符设备；
- Docker Engine 和 Docker CLI 可用；
- Android Emulator 镜像使用固定 tag 或 `sha256` digest，禁止 `latest`；
- 镜像内存在 `adb`，单个容器只运行一台 Emulator；
- 默认容器内 ADB serial 为 `emulator-5554`，ADB TCP 端口为 `5555`；
- 默认独立数据卷挂载到 `budtmo/docker-android` 的持久化目录 `/home/androidusr`。若所选镜像的数据目录不同，部署时必须显式调整并完成清理验证；
- 镜像的 ABI 必须与待测 APK 兼容，MVP 推荐 `x86_64`。

Provider 初始化会先检查 Linux、KVM 读写权限和 Docker Engine。任何一项失败都会返回明确错误，不会自动切换 Mock Provider，也不会把 Windows Docker Desktop 当成通过。

## 3. 资源和命名规则

每个 `provider_ref` 确定性生成一组资源：

```text
alcor-df-<可读短名>-<provider_ref 哈希>
alcor-df-<可读短名>-<provider_ref 哈希>-net
alcor-df-<可读短名>-<provider_ref 哈希>-data
```

容器、网络和数据卷统一带以下受管标签：

- `io.alcor.device-farm.managed=true`；
- `io.alcor.device-farm.provider-ref`；
- `io.alcor.device-farm.host-id`。

容器额外保存 Device ID、Image ID、运行镜像、generation 和 capabilities。删除只按“受管标签 + provider_ref”清理，禁止使用模糊名称或清理非设备农场资源。

## 4. 端口和资源限制

- Docker 为每台 Emulator 随机分配空闲 Host ADB 端口，避免多设备硬编码端口冲突；
- `serial` 和 `adb_endpoint` 都使用 `<advertise_host>:<随机端口>`，不会自动选择第一台设备；
- 默认只绑定 `127.0.0.1`。STF/Appium 位于其他主机时，必须把 bind address 配置为设备内网地址或 `0.0.0.0`，并用防火墙限制来源；
- 每台容器默认限制为 2 CPU、4 GiB 内存和 512 PID；
- 每台设备使用独立 bridge network 和独立 data volume；
- 容器内 Appium 端口 `4723` 也由 Docker 随机映射到独立 Host 端口，Endpoint 使用 `http://<advertise_host>:<随机端口>`；
- Appium Adapter 只请求每台设备自己的 `/status`，`value.ready=true` 后才把 `AppiumHealthy` 标记为真；
- 每个 Appium Server 与 Emulator 位于同一容器，内部只连接本容器的 `emulator-5554`，避免两台设备共用一个 Appium 进程导致 UDID 串线。

## 5. 生命周期和幂等

- `create`：清理同 `provider_ref` 的历史残留，创建网络、数据卷和 stopped 容器；若相同设备容器已经存在，返回原结果；
- `start/stop/restart`：只操作确定性容器名，操作后重新 inspect；
- `rebuild`：读取受管标签，删除旧容器、网络和数据卷，以相同 Device/Image 元数据重新创建并启动，generation 加一；
- `delete`：重复调用仍成功，清理容器、专属网络和数据卷；
- `discover`：只返回当前 Host ID 且带受管标签的容器；单台设备仍在启动不会阻断其他设备心跳；
- Host Agent 命令 completion 继续使用 DF-012 lease token 和 attempt，Provider 本身不创建第二套任务队列。

Agent 心跳发现结果按以下规则回写 Server：

- 只更新已经登记且同时匹配 `host_id + provider_ref` 的 Device；未知设备和其他 Host 的同名引用不会自动注册或串绑；
- `serial`、ADB/Appium Endpoint、容器内 `appium_udid` 和 `last_seen_at` 使用 Server 数据库事务更新；`appium_udid` 放入 Device capabilities 和 Session 连接快照，不新增第二套 Device；唯一标识冲突时整笔心跳回滚并返回 `DEVICE_IDENTITY_CONFLICT`；
- `provisioning → booting → ready`、`stopped → booting` 等变化必须通过 Device 状态机，进入 `ready` 前先确认健康为 `healthy`；
- `reserved/busy/recycling` 的预约生命周期不被 Agent 覆盖，`quarantined/deleted` 也不会因心跳自动恢复；
- 自动补齐所需的新 Device 必须由 DF-016 Controller 先登记，再创建 Host Command，不能把 Agent 自报设备当作创建入口；create 命令会启动设备并等待 ADB、boot 和 Appium 全部健康；
- Reservation 释放后的 rebuild 命令使用“幂等 Delete + 全新 Create”，旧数据卷删除失败时命令失败并重试，不允许挂载旧卷冒充清理完成。

## 6. 配置

```text
DEVICE_FARM_AGENT_PROVIDER=docker
DEVICE_FARM_DOCKER_BINARY=docker
DEVICE_FARM_DOCKER_IMAGE=<固定 tag 或 digest>
DEVICE_FARM_DOCKER_ADVERTISE_HOST=<ADB 客户端可访问的 Host 地址>
DEVICE_FARM_DOCKER_BIND_ADDRESS=127.0.0.1
DEVICE_FARM_DOCKER_KVM_DEVICE=/dev/kvm
DEVICE_FARM_DOCKER_ADB_PORT=5555
DEVICE_FARM_DOCKER_APPIUM_PORT=4723
DEVICE_FARM_DOCKER_ADB_SERIAL=emulator-5554
DEVICE_FARM_APPIUM_HEALTH_TIMEOUT=5s
DEVICE_FARM_DOCKER_DATA_MOUNT_PATH=/home/androidusr
DEVICE_FARM_DOCKER_EMULATOR_DEVICE=Samsung Galaxy S10
DEVICE_FARM_DOCKER_CPUS=2
DEVICE_FARM_DOCKER_MEMORY=4g
DEVICE_FARM_DOCKER_PIDS_LIMIT=512
```

Provider 当前按 `budtmo/docker-android` 的公开契约配置：Host ADB 连接容器端口 `5555`，Appium 连接容器端口 `4723`，容器内 serial 为 `emulator-5554`，持久化目录为 `/home/androidusr`，设备型号通过 `EMULATOR_DEVICE` 设置。镜像通过 `APPIUM=true` 启用其已有 Appium 2.x，不在本项目重写 Appium Server 或 WebDriver。参考上游基线提交为 `e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`。DF-016 已要求 validation 和每次正式 create 都核对本机镜像 ID/RepoDigest 与已登记 digest。

Linux Host 的 systemd 配置和安装步骤见 [部署包](../deploy/docker-emulator/README.md)。

## 7. Linux KVM 验收

在仓库根目录执行：

```sh
export DEVICE_FARM_DOCKER_IMAGE='<固定 tag 或 digest>'
export DEVICE_FARM_DOCKER_ADVERTISE_HOST='<Host 内网地址>'
./scripts/verify-docker-emulator.sh
```

脚本会真实创建并启动两台 Emulator，等待 ADB online 和 Android boot completed，验证 serial/端口不冲突，然后删除全部容器、网络和数据卷。任何 KVM、Docker、镜像、ADB、启动或清理错误都会直接失败。

## 8. 当前环境限制

当前 Windows 开发机没有 Docker CLI/Engine，WSL Ubuntu 的虚拟磁盘路径损坏，无法提供 `/dev/kvm`。因此纯 Go 生命周期和 Docker 命令契约可在本机验证，但 DF-014 最终状态必须等待 Linux KVM Host 执行上述真实验收，不能用 Fake Backend 或 Mock Provider 代替。
