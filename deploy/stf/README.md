# STF 单机内网部署

该目录复用 DeviceFarmer/STF，不实现第二套看屏、触控、设备日志、文件管理、claim、release 或 remoteConnect。当前 MVP 面向一台 Emulator，采用官方支持的最小单机拓扑；增加设备不改变拓扑：

```text
authorized browser / reverse proxy
              |
      7100, 7110, 7400-7500
              |
    DeviceFarmer/STF local 3.7.9
          /               \
 internal ADB server      RethinkDB 2.4.2
          |
  adb connect Host:随机ADB端口
          |
 Docker Android Emulator
```

版本基线：

- DeviceFarmer/STF `v3.7.9`，发布于 2026-07-08，对应提交 `36d1a3e4336f2ecdf7885e3644fe34d0a4282c87`；
- 官方 Compose 使用独立 `devicefarmer/adb` 镜像；该仓库只有浮动 `latest`，本项目按 2026-05-05 发布内容固定为 digest `sha256:a699fafbc63d8a145f816257b1cd366ea3c5f0aff657e3bb135309bf7da45759`；
- RethinkDB `2.4.2`；
- Compose 默认镜像均使用固定 tag，验收脚本拒绝 `latest`；正式环境首次拉取后还应记录镜像 digest。

官方资料：

- <https://github.com/DeviceFarmer/stf/releases/tag/v3.7.9>
- <https://github.com/DeviceFarmer/stf/blob/v3.7.9/doc/DEPLOYMENT.md>
- <https://github.com/DeviceFarmer/stf/blob/v3.7.9/doc/API.md>

## 部署

仅在 Linux Docker Host 执行：

```sh
cd deploy/stf
cp .env.example .env
chmod 600 .env
# 填写内网地址、管理员邮箱和随机 STF_AUTH_SECRET
docker compose --env-file .env -f compose.yaml config
docker compose --env-file .env -f compose.yaml up -d
```

默认只绑定 `127.0.0.1`。推荐同机 Nginx/Caddy 提供 HTTPS、公司身份认证和访问控制。若明确允许内网直连，才把 `STF_BIND_ADDRESS` 改为设备网私有地址。禁止绑定公网地址。

## 端口

| 端口 | 用途 | 暴露规则 |
|---|---|---|
| 7100 | STF 页面、REST API、存储代理 | 仅反向代理或受控内网 |
| 7110 | STF WebSocket | 与 7100 同一安全边界 |
| 7400-7500 | STF 官方默认设备 worker/远控端口范围 | 仅授权浏览器和 Worker 可达 |
| 28015 | RethinkDB driver | 仅 Compose internal network，不发布 |
| 5038（Host）→5037（容器） | ADB endpoint registrar / 远程设备汇入点 | 只绑定 `127.0.0.1`，供同机 Host Agent 与本地同步脚本使用 |
| 8080 | RethinkDB 管理页面 | 不发布 |

STF 内部进程通信本身不适合不可信网络，因此整个部署必须位于设备内网。Docker Socket 不挂载给任何 STF 服务。

## 接入 Emulator

Docker Provider 为每台 Emulator 发布独立随机 ADB Host 端口。DF-017 验收时可将 Device Farm 返回的 `adb_endpoint` 手工连接到 STF 的 ADB server：

```sh
./scripts/stf-connect-emulators.sh 10.0.0.10:32771
```

这一步不创建 Emulator、不改 Pool membership、不改 Reservation，只让 STF 复用已存在的 ADB Endpoint。自动同步不以 STF 数据覆盖 Device Farm PostgreSQL 真相。

### 多宿主机：本机 ADB server 是唯一汇入点

全局只有这一套 STF。STF 的 ADB registrar **硬性要求回环地址**（`internal/adapters/stfadb/client.go`；见 ADR-0037 决策 4），因此**远程宿主机的 Host Agent 无法向这里的 STF 注册**。设备进入 STF 的唯一路径是：

> 由 STF 所在宿主机（本机）统一发起 `adb connect <远程宿主机设备 endpoint>`。

这一步由本机上的 systemd timer 自动完成，取代此前的纯手工连接：

```sh
# 查看状态与日志
systemctl status alcor-device-farm-stf-sync.timer
systemctl list-timers alcor-device-farm-stf-sync.timer
journalctl -u alcor-device-farm-stf-sync.service -n 50 --no-pager

# 手动触发一次（以 root 登录时无需 sudo）
systemctl start alcor-device-farm-stf-sync.service

# 干跑（不实际连接），用于排障
DEVICE_FARM_SERVICE_TOKEN='<控制面 Service Token>' \
  ./scripts/stf-sync-remote-emulators.sh --dry-run --verbose
```

| 文件 | 作用 |
|---|---|
| `scripts/install-stf-sync-timer.sh` | 安装器（需 root）：把同步脚本装到 `/opt/alcor-device-farm/`、生成 systemd 单元与 env 模板 |
| `scripts/stf-sync-remote-emulators.sh` | 同步脚本：从控制面拉取 ready 的 Android 设备并逐个 `adb connect` |
| `deploy/stf/alcor-device-farm-stf-sync.service` | oneshot 服务单元（安装器据此生成 `/etc/systemd/system/` 下的实际单元） |
| `deploy/stf/alcor-device-farm-stf-sync.timer` | 周期触发（开机后 3 分钟起，**每 20 秒**一次；间隔须明显小于 reconcile 的 STF 宽限期，见下） |
| `deploy/stf/stf-sync.env.example` | `/etc/alcor-device-farm/stf-sync.env` 的模板 |

脚本会**跳过本机设备**（本机 Host Agent 已通过回环 registrar 完成注册），只处理远程宿主机，因此新增宿主机不需要在本机做任何额外配置。设备清单来自控制面 `GET /api/v1/devices?platform=android&lifecycle_status=ready` 并自动翻页（服务端 `page_size` 上限 200）。

> **定时器间隔为什么必须是 20 秒。** `reconcile` 在判定设备是否通过 STF 可见时，先用一个宽限期保护刚创建/重建的设备（`DEVICE_FARM_RECONCILE_STF_VISIBILITY_GRACE`，默认 **30 秒**）。该宽限期的锚点是**最近一次成功 `create`/`rebuild` 命令的完成时刻**，而 `restart` **不会**重置这个锚点。
>
> 于是：若设备在宽限期内还没被同步进 STF，`reconcile` 即开始累计 `stf_not_visible` 失败并触发自愈重启；重启又不重置锚点 ⇒ **「反复重启但永远不可见」的死循环**。`10.0.30.55` 在 2026-09-14 出现的 `restart ×12` 就是这个机制——当时本题的定时器尚未上线，设备无论如何都不会可见。
>
> 所以间隔必须**明显小于**宽限期：20 秒留出约 10 秒余量。同理 `AccuracySec` 必须收紧到 `1s`；systemd 默认的 `1min` 会把 20 秒的间隔抖动到分钟级，等于重新引入超窗风险。

安装（在 STF 宿主机上以 root 执行，幂等；已存在的 `/etc/alcor-device-farm/stf-sync.env` 不会被覆盖）：

```sh
cd <本仓库在该宿主机的检出位置>
scripts/install-stf-sync-timer.sh
# 然后在 /etc/alcor-device-farm/stf-sync.env 填入 DEVICE_FARM_SERVICE_TOKEN
systemctl start alcor-device-farm-stf-sync.timer
```

安装器只把同步脚本复制到 `/opt/alcor-device-farm/scripts/`，因此该脚本**不能依赖默认的 compose 路径推导**（它默认按「脚本所在目录的上级/deploy/stf」定位，而 `/opt` 下没有 `deploy/`）。安装器会在生成的 env 中自动写入 `STF_COMPOSE_FILE` / `STF_ENV_FILE` 指向仓库检出位置；手工维护 env 时必须自行确认这两个路径，否则服务会以 `STF compose file not found` 反复失败。compose 顶层声明了 `name: alcor-device-farm-stf`，所以用 `-f` 指定绝对路径不影响项目名匹配。

服务每次执行都会在 journal 写一行摘要（`sync done: connected=N skipped_local=N failed=N`），`VERBOSE=1` 或在 env 文件中开启可打印逐设备明细。这是守护进程唯一的可观测面，排障先看它。

随后在 STF 页面创建专用 API Token。Token 只保存到 Device Farm Server 的秘密配置或验收进程环境，不写入 Compose `.env`，不发送给浏览器，不记录到日志。

DF-031 管理员 Web 远控还要求 Device Farm Server 持有与本目录 `STF_AUTH_SECRET` 相同的受限副本，并配置同一个 `STF_ADMIN_NAME/STF_ADMIN_EMAIL` 身份。Server 只用它签发最长 60 秒的 STF 登录 JWT；浏览器收到的 JWT 不能换取 API Token，STF 建立 Session 后会从地址栏移除 JWT。生产反向代理必须关闭包含查询参数的访问日志或对 `jwt` 参数脱敏。

STF 3.7.9 的 `local` 启动器会在 INFO 日志中打印子进程完整命令行，其中包含 `--auth-secret`。当前 Compose 因此对 `stf` 主容器使用 `logging.driver=none`，禁止 Docker 持久化该 stdout；`stf-adb`、RethinkDB 日志以及 STF 页面内的设备 Logcat/文件能力不受影响。生产环境如需采集 STF 进程日志，必须先接入经真实验收的 Secret 脱敏代理，不能直接恢复 `json-file`。

设备农场中的 Emulator 是由用户长期管理的设备，不是每次借用后恢复的临时测试机。Compose 必须保留 `--no-cleanup`：STF 默认 cleanup 会在 release 时卸载本次 claim 后新增的 APK，并可能清理账号和缓存；关闭它只停止 STF 的自动清理，不影响 claim、release、`using` 状态或 Device Farm 的预约回收。恢复出厂只能通过 Device Farm 明确的 rebuild/reimage 操作完成。

当 Console 使用 HSTS、STF 仍为 HTTP 时，两者不能共用同一个浏览器主机名：HSTS 不区分端口，会把 STF 的 `http://host:7100` 升级为不可用的 HTTPS。第一阶段应给 STF 配置独立的受控 DNS 名，并让 `STF_PUBLIC_IP` 与 Server 的 `DEVICE_FARM_STF_WEB_URL` 使用该名称；当前验收环境使用 `10-0-30-171.nip.io` 映射内网地址。正式内网 DNS 可用后应替换该临时解析名；另一条演进路径是为 STF App、WebSocket 和屏幕端口统一提供受信 TLS。

完整验收：

```sh
export STF_API_URL='http://127.0.0.1:7100'
export STF_API_TOKEN='<专用服务 Token>'
export DEVICE_FARM_ADB_ENDPOINTS='10.0.0.10:32771'
./scripts/verify-stf-deployment.sh
```

脚本会检查镜像未使用 `latest`、启动服务、连接当前一台 Emulator 的 ADB Endpoint，并通过官方 `/api/v1/devices` 确认该 serial 为 `present=true/ready=true`。如资源允许，可提供逗号分隔的多个 Endpoint 执行多设备扩展验收；当前 P0/P1 按 ADR-0008 的单台模拟器配置执行。脚本不会输出 API Token。

## 后续接 USB 真机

基础 Compose 不授予 USB 或 privileged 权限。需要真机时使用显式叠加文件：

```sh
docker compose --env-file .env -f compose.yaml -f compose.usb.yaml up -d
```

只有 `stf-adb` 获得 `/dev/bus/usb` 和 privileged；STF、RethinkDB、Device Farm Server、Alcor 和浏览器仍得不到 USB 或 Docker Socket。上层 Device、Pool、Reservation、STF Adapter 和 Appium 架构不变。

## 停止、备份和回滚

```sh
docker compose --env-file .env -f compose.yaml stop
docker compose --env-file .env -f compose.yaml down
```

普通 `down` 保留 `stf-rethinkdb-data` 和 `stf-adb-keys`。禁止在未备份时执行 `down -v`。升级前记录当前镜像 digest并备份 RethinkDB volume；回滚时恢复原固定镜像和匹配的数据备份。

STF 重启或回滚不得修改 `device_reservations`。预约真相只在 Device Farm PostgreSQL；STF 的 `using/owner` 只是远控工具状态，DF-018 负责 claim/release 补偿和收敛。
