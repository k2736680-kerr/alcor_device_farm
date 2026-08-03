# DF-017 实施与验收证据

## 当前结论

DeviceFarmer/STF 与 RethinkDB 的固定版本 Compose、配置样例、日志轮转、健康检查、Emulator ADB 连接脚本、USB 真机叠加配置和真实验收脚本已完成。本机没有可用 Docker/Linux 环境，不能真实启动 STF 或让两台 Emulator 显示为 ready，因此 DF-017 当前状态为 `blocked`，不能标记 `completed`。

## 上游基线

已直接核对 DeviceFarmer/STF 官方仓库：

- 最新稳定版本：`v3.7.9`；
- 发布时间：2026-07-08；
- Git commit：`36d1a3e4336f2ecdf7885e3644fe34d0a4282c87`；
- 官方发布流程将 `v3.7.9` 构建为 `devicefarmer/stf:3.7.9`；
- 官方 `docker-compose.yaml` 使用 `stf local --adb-host ... --public-ip ... --provider-min-port ... --provider-max-port ...`；
- 官方部署文档明确 STF 为多进程系统、内部通信不加密，不应放置在不可信网络；
- 官方 REST API 使用 `Authorization: Bearer <token>`，提供 inventory、claim/release 和 remoteConnect，后续 DF-018 只封装这些已有接口。

## 已完成交付

- `deploy/stf/compose.yaml`：`STF local 3.7.9 + STF image 内置 ADB server + RethinkDB 2.4.2`；
- 固定 tag，验收脚本拒绝 `latest`，正式部署要求记录拉取后的 digest；
- 默认 `STF_BIND_ADDRESS=127.0.0.1`，只发布 7100、7110 和 STF 官方默认 7400-7500；
- RethinkDB 28015、管理页面 8080 和 ADB 5037 均不发布到 Host；
- Compose internal network 隔离 RethinkDB/ADB，任何服务都不挂载 Docker Socket；
- 三个服务均有健康检查和受限 JSON 日志轮转；
- `.env.example` 不包含真实 Token/密码，真实 `.env` 被 Git 忽略；
- STF API Token 不进入 Compose，不发给浏览器，只由后续 Server secret 或验收进程使用；
- `scripts/stf-connect-emulators.sh` 只把已有随机 ADB Endpoint 接入 STF，不创建或占用设备；
- `scripts/verify-stf-deployment.sh` 通过官方 inventory API 检查至少两个指定 serial 的 `present/ready`；
- `compose.usb.yaml` 只在未来真机部署时给 `stf-adb` 增加 `/dev/bus/usb` 和 privileged，不扩大 STF/RethinkDB/Server 权限；
- RethinkDB volume 与 Device Farm PostgreSQL 完全分离，STF 重启不能修改 Reservation 真相。

## 本地验证

```powershell
./scripts/dev.ps1 -Task check
```

自动契约测试覆盖：

```text
PASS TestSTFComposePinsImagesAndKeepsInfrastructurePrivate
PASS TestUSBOverlayLimitsPrivilegeToADBService
PASS OpenAPI/migration/Go full suite
```

本机 WSL 磁盘不可用且没有 Docker，因此 shell 脚本只能保存为 Linux 验收入口，不能在本机宣称运行通过。

## Linux 真实验收

1. 在设备内网 Linux Host 复制 `.env.example` 为 `.env`，设置随机 `STF_AUTH_SECRET`、内网 `STF_PUBLIC_IP` 和管理员邮箱；
2. 执行 `docker compose config`，确认没有 `latest`、公网绑定、Docker Socket 或真实 Token 输出；
3. 启动 Compose，确认 RethinkDB、stf-adb、stf 均 healthy；
4. 从 Device Farm 获取两台自动创建 Emulator 的 ADB Endpoint；
5. 使用 `stf-connect-emulators.sh` 连接两个 Endpoint；
6. 创建专用 STF API Token，只放入验收进程环境；
7. 执行 `verify-stf-deployment.sh`，确认两个 serial 均 `present=true/ready=true`；
8. 在授权浏览器验证两台设备看屏、输入和日志基础能力；
9. 记录 active Reservation 的 ID、device_id 和状态，重启 STF，再次查询 Device Farm，确认三项未变化；
10. 检查浏览器网络、页面源码、日志和 Compose 配置，确认没有 Device Farm 管理 Token 或 STF API Token；
11. 保存脱敏日志、inventory 响应、容器健康和 Reservation 前后对比证据。

## 阻塞解除条件

Linux 环境完成上述真实 STF、两台 Emulator 和安全验收后，将 DF-017 改为 `completed` 并单独提交证据。仅 Compose 解析或契约测试通过不能替代真实 STF 验收。
