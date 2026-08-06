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
| 5037 | ADB server | 仅 Compose internal network，不发布 |
| 8080 | RethinkDB 管理页面 | 不发布 |

STF 内部进程通信本身不适合不可信网络，因此整个部署必须位于设备内网。Docker Socket 不挂载给任何 STF 服务。

## 接入 Emulator

Docker Provider 为每台 Emulator 发布独立随机 ADB Host 端口。DF-017 验收时将 Device Farm 返回的 `adb_endpoint` 连接到 STF 的 ADB server：

```sh
./scripts/stf-connect-emulators.sh 10.0.0.10:32771
```

这一步不创建 Emulator、不改 Pool membership、不改 Reservation，只让 STF 复用已存在的 ADB Endpoint。最终运行时的自动同步属于 DF-018 Adapter 编排，不以 STF 数据覆盖 Device Farm PostgreSQL 真相。

随后在 STF 页面创建专用 API Token。Token 只保存到 Device Farm Server 的秘密配置或验收进程环境，不写入 Compose `.env`，不发送给浏览器，不记录到日志。

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
