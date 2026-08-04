# DF-016 实施与验收证据

## 结论

DF-016 已完成。镜像验证、按 Image 运行引用、固定目标自动补池、单设备容量限制、释放重建、隔离补回、失败退避和真实 Host Agent 链路均已在 Linux KVM 服务器通过验收。

当前验收配置固定为：

```text
min_ready=1
max_instances=1
max_concurrency=1
DEVICE_FARM_AGENT_CONCURRENCY=1
DEVICE_FARM_DOCKER_CPUS=4
DEVICE_FARM_DOCKER_MEMORY=5g
```

数量和资源均来自配置或数据库，没有写死在 Provider、Scheduler、Reservation、Device 或 migration 中。后续增加模拟器只改容量参数；接入 USB 真机仍复用现有上层架构。

## 验收环境

- 服务器：`10.0.30.171`，Ubuntu 22.04、x86_64、Linux KVM；
- Host：12 核 CPU、15 GiB 内存，验收前约 12 GiB 可用；
- Docker：28.1.1，`/dev/kvm` 为 `root:kvm 660`；
- PostgreSQL：16，使用独立临时验收数据库和 `127.0.0.1:55432`；
- Device Farm Server：`127.0.0.1:18080`；
- 镜像：`alcor-device-farm/android-emulator:16.0-api36-r3`；
- 摘要：`sha256:8afadfa4c342194c360edaf8302fb082ca29896002fcd9a1c44e494fd550c400`；
- Android：16 / API 36 / x86_64；
- 设备模板：`Pixel 9`；
- Appium：3.5.2，UiAutomator2 8.2.2。

单台模拟器稳定运行时实测约 `3.954 GiB / 5 GiB`。`5g` 和 `4` 核都是容器上限，不是启动时预占。

## 真实运行链路

| 验收项 | 结果 |
|---|---|
| Agent 心跳和容量 | Host `online`，`device_slots=1` |
| Image validation | 1 次成功；摘要、ADB、boot、Appium 全部通过，Image 进入 `ready` |
| 验证资源清理 | 临时 validation 容器、网络、卷全部自动删除 |
| 固定目标补池 | 设置 `1/1/1` 后，Controller 自动登记 Device、Pool membership 和 create Host Command |
| 正式设备健康 | Android 16 设备进入 `ready/healthy`，ADB 和 Appium Endpoint 完整 |
| Appium Session | 使用 `emulator-5554` 创建并删除真实 UiAutomator2 Session 成功 |
| 单并发限制 | 第一条 Reservation 进入 `active`；第二条保持 `pending`；运行容器始终为 1 |
| 释放和数据清理 | 两条 Reservation 依次释放，产生 2 次 rebuild，旧容器、网络、卷均被替换 |
| 隔离补回 | 原 Device 进入 `quarantined/unhealthy`，Controller 自动登记并创建新的 `ready/healthy` Device |
| 错误摘要 | 错误 digest 的 Image 进入 `failed/IMAGE_DIGEST_MISMATCH` |
| 命令风暴保护 | 错误摘要只产生 1 条 validation Command、1 次 attempt，且没有创建第二个容器 |
| 最终资源数 | `managed_containers=1`、`managed_networks=1`、`managed_volumes=1` |

最终验收数据库中的 Host Command 汇总：

```text
create         succeeded attempts=1 count=2
rebuild        succeeded attempts=1 count=2
validate_image succeeded attempts=1 count=1
validate_image failed    attempts=1 error=IMAGE_DIGEST_MISMATCH count=1
```

两条测试 Reservation 最终均为 `released`。隔离前旧设备为 `quarantined/unhealthy`，替代设备为 `ready/healthy`。

## 真实链路发现并修复的问题

### 1. Agent 容量与单机配置不一致

systemd 原来把 `--concurrency 2` 写死，Host 会错误上报两个槽位。现改为 `DEVICE_FARM_AGENT_CONCURRENCY`，样例和当前环境默认 `1`；扩容只改环境配置。

### 2. Host Command 租约短于 Android 启动时间

原租约 60 秒，但 Android 16 validation 可能超过 60 秒，导致成功结果被拒绝为 `STALE_COMMAND_LEASE`。现使用 300 秒租约和 270 秒执行超时，并在 Agent 启动时校验“执行超时必须严格短于租约”。

### 3. Agent 退出时在途验证可能残留资源

原执行上下文基于 `context.Background()`，Agent 收到退出信号后不能及时取消在途命令。现改为继承 Agent 运行上下文；退出会触发 validation/create 的受控清理，再等待 worker 结束。

### 4. Android 16 镜像不支持旧设备模板

镜像内 `avdmanager list device` 不包含 `Samsung Galaxy S10`，会造成 AVD 创建失败并表现为持续 `ADB_OFFLINE`。默认模板已改为镜像真实支持的 `Pixel 9`，并在部署文档明确设备模板必须来自镜像列表。

## 自动化回归

Linux 上使用 Go 1.24.6 完成：

```text
PASS gofmt -l .（无输出）
PASS go test ./...
PASS go vet ./...
PASS ./internal/repository
PASS ./internal/scheduler
PASS ./internal/reaper
PASS ./internal/reconcile
PASS ./internal/hostcommand
PASS ./internal/metrics
PASS ./internal/api
PASS ./internal/warmpool
PASS migration constraints
PASS migration up -> down -> up，最终 12 张表
```

并发 Controller、并发 Scheduler/Reaper、KVM/ADB/Appium 故障、创建失败隔离、指数退避和不形成命令风暴继续由上述 PostgreSQL、Agent、Provider 和 Controller 自动化测试覆盖；真实服务器同时验证了 KVM、ADB、boot、Appium 成功链路以及错误 digest 的失败链路。

## 业务服务保护和清理

验收过程中未停止、未重启服务器已有业务容器：

```text
vega-face-search_nginx_1 restart_count=0
vega-face-search_app1_1  restart_count=0
```

验收结束后删除独立 PostgreSQL 容器及卷、Device Farm 临时进程、模拟器容器及专属网络/卷、临时源码/工具链/日志和 PostgreSQL 临时镜像；只保留正式 Android 16 r3 镜像。密钥只存在于服务器临时目录和本地 Git 忽略的 `.env.server`，未写入 Git。
