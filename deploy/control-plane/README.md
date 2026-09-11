# 220 控制面预部署

本目录用于把 Device Farm 控制面预部署到 `10.0.80.220`，不切换现有评估后台，也不修改 `10.0.30.171` 的正式 Server、STF 或模拟器。

## 目录与隔离

正式预部署目录固定为：

```text
/data/stacks/alcor-device-farm/
├── compose.yaml
├── server.env
├── postgres.env
├── postgres-data/
├── backups/
├── gateway/nginx.conf
├── ios-gateway/nginx.conf
├── ios-tunnel/Dockerfile
├── secrets/console-users.yaml
├── secrets/tls.crt
├── secrets/tls.key
├── secrets/ios-gateway-public-tls/{fullchain.pem,private.key}
├── secrets/ios-tunnel/{id_ed25519,known_hosts}
└── source/
```

它与现有 `/data/stacks/mongodb` 同级，但拥有独立 Compose 项目、网络、PostgreSQL 数据目录、数据库账号、备份目录和容器资源限制。不会复用现有 PostgreSQL 5432、`alcor` 数据库或现有 Docker 卷。

## 预部署端口

| 地址 | 用途 | 预部署策略 |
|---|---|---|
| `https://10.0.80.220:18180` | Device Farm Server/Console | 独立 TLS 网关；预部署证书可先自签，正式切换前替换为内网证书 |
| `https://10.0.80.220:18181` | iOS Gateway | 独立 TLS Gateway；默认 `ios` profile 不启动，不改变现有 171 服务 |
| `10.0.80.220:18182` | Host Agent 内网 API | 只给 171/未来 Host Agent 使用，不接 NPS、不提供浏览器访问 |
| Compose 内部 `postgres:5432` | Device Farm PostgreSQL | 不发布到宿主机，不允许外部访问 |
| `10.0.30.171:7100` | 现有 STF | 预部署 Server 只读连接，正式切换前不迁移 |

不要使用 220 上已有评估后台的 `18080`，也不要把 RethinkDB `28015` 或 ADB `5037/5038` 暴露到办公网。

## 部署流程

在部署机准备源码快照后，将 `source/`、`compose.yaml`、`server.env`、`postgres.env` 和 `secrets/console-users.yaml` 放到上述目录。真实 Token、数据库密码和 Console Argon2id 哈希只放在远端未纳入 Git 的文件中。

```sh
cd /data/stacks/alcor-device-farm
chmod 600 server.env postgres.env secrets/console-users.yaml
chown 70:70 postgres-data
chown 65532:65532 backups secrets/console-users.yaml
chmod 700 postgres-data backups
chmod 755 . secrets
chmod 400 secrets/console-users.yaml
chmod 400 secrets/tls.key
chmod 444 secrets/tls.crt
chown 101:101 secrets/tls.key secrets/tls.crt
docker compose --env-file postgres.env --env-file server.env config
docker compose --env-file postgres.env --env-file server.env build
docker compose --env-file postgres.env --env-file server.env up -d postgres
```

首次初始化独立数据库后，在 Server 容器中执行内置 migration，再启动 Server：

```sh
docker compose --env-file postgres.env --env-file server.env run --rm \
  --entrypoint /usr/local/bin/apply-device-farm-migrations.sh device-farm-server --fresh
docker compose --env-file postgres.env --env-file server.env up -d device-farm-server device-farm-gateway
```

上述默认命令不会启动 iOS 隧道与 18181 Gateway。下一阶段预发布验证前，先把 Mac SSH 私钥、`known_hosts` 和 iOS 域名证书放入对应 Secret 目录，按隧道非 root UID/GID 设置只读权限，再显式启用 profile：

```sh
chmod 700 secrets/ios-tunnel secrets/ios-gateway-public-tls
chmod 400 secrets/ios-tunnel/id_ed25519 secrets/ios-tunnel/known_hosts
chmod 400 secrets/ios-gateway-public-tls/private.key
chmod 444 secrets/ios-gateway-public-tls/fullchain.pem
chown -R 10001:10001 secrets/ios-tunnel
chown -R 101:101 secrets/ios-gateway-public-tls
docker compose --profile ios --env-file postgres.env --env-file server.env config
docker compose --profile ios --env-file postgres.env --env-file server.env build device-farm-ios-tunnel
docker compose --profile ios --env-file postgres.env --env-file server.env up -d \
  device-farm-server device-farm-ios-tunnel device-farm-ios-gateway
```

Server 的 8081 不再直接映射宿主机；18181 只由独立 Nginx TLS Gateway 暴露。Tunnel 与 Server 共用网络命名空间，使 Server 通过 `127.0.0.1:4811` 访问 Mac Session Fence，并通过 `127.0.0.1:4842` 转发到 Mac 的 Baguette `127.0.0.1:8421`。部署代码可以提前准备，但是否启用 iOS profile、切 Agent 和改 NPS 都是以后单独确认的动作。

发布镜像会内置 migration runner、备份脚本和设备域 migration；执行前仍必须先做数据库备份和 `--check-config`。预部署配置中的 STF 默认关闭，待取得现有 171 的 API Token 后再单独启用，避免把未知凭证写入部署包。

## 预部署验收

必须通过：

- `docker compose config` 无明文生产凭证输出；
- `postgres` 健康，数据库只存在设备域表；
- Server `/healthz`、`/readyz`、`/metrics` 通过；
- `GET /console/` 可访问，未认证请求被拒绝；
- 220 能读取 171 的 STF API，但不访问 RethinkDB；
- 现有评估后台的 18080、5432、8008、8880、8888 服务保持不变；
- Server/Console/PostgreSQL 任一预部署容器重启后状态可恢复；
- 启用 iOS profile 时 tunnel 为非 root、无 Docker Socket，`127.0.0.1:4842/simulators.json` 可达；
- 18181 完成 TLS 握手，未认证 iOS Gateway 请求返回 401；
- 预部署失败时只删除本项目容器、网络和目录，不执行全局 Docker 清理。

## 正式切换原则

预部署和正式切换分开。切换前不改 171 Agent 的 Server URL，不停止 171 Server，不切换评估后台入口。正式切换时只需要：停止旧设备农场 Server、保留 171 Host Agent/Emulator/STF、切换 Agent 指向 220:18182、验证心跳和设备状态，再把 NPS/控制台入口指向 220:18180。切换脚本必须先做备份、健康检查和可回滚检查。18182 不应加入公网或 NPS 转发。

维护窗口前复制模板并运行只读检查；`cutover.env` 已被 Git 忽略，不得把真实路径、校验值或 Secret 写回示例文件：

```sh
cp deploy/control-plane/cutover.env.example deploy/control-plane/cutover.env
chmod 600 deploy/control-plane/cutover.env
set -a
. deploy/control-plane/cutover.env
set +a
scripts/verify-control-plane-cutover-readiness.sh
```

检查通过只代表入口和回滚基础可用。正式切换前仍需生成并校验生产备份、对齐 Host Agent 认证 Token、确认 STF/iOS Secret 与 Baguette 隧道、完成 Agent 心跳和设备验证，之后才可修改 NPS。仓库部署代码和 220 预发布均不等于发布到 171。
