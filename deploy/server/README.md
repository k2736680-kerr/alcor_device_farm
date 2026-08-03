# Device Farm Server 部署

该部署只包含设备农场 Server，不复制 PostgreSQL、STF 或 Alcor。PostgreSQL 使用现有数据库服务，STF 继续使用 `deploy/stf`，Host Agent 继续使用 `deploy/docker-emulator`。

## 方式一：systemd

前置条件：Linux、Go 1.24+、PostgreSQL 客户端、可访问的 PostgreSQL 数据库。

```sh
sudo ./scripts/install-device-farm-server.sh
sudoedit /etc/alcor-device-farm/server.env
```

至少填写：

- `DEVICE_FARM_DATABASE_URL`；
- `DEVICE_FARM_SECURITY_SERVICE_TOKEN`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`；
- 启用 STF 时填写 `DEVICE_FARM_STF_BASE_URL` 和 `DEVICE_FARM_STF_API_TOKEN`。

首次空库执行：

```sh
set -a
. /etc/alcor-device-farm/server.env
set +a
DEVICE_FARM_MIGRATION_DIR=/opt/alcor-device-farm/migrations \
  ./scripts/apply-device-farm-migrations.sh --fresh
```

检查并启动：

```sh
sudo systemctl enable --now alcor-device-farm-server.service
sudo systemctl status alcor-device-farm-server.service
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/metrics
```

systemd 会在启动前执行 `check-server-deployment.sh`；缺数据库 URL、缺 Token、Token 冲突或 YAML 非法时服务不会启动。正式环境应由 Secret 管理器或 systemd credential 注入环境变量。

## 方式二：Docker Compose

```sh
cd deploy/server
cp server.env.example server.env
chmod 0600 server.env
```

编辑 `server.env`，再为 Compose build 提供版本信息：

```sh
export DEVICE_FARM_VERSION=0.1.0
export DEVICE_FARM_COMMIT=$(git rev-parse --short=12 HEAD)
export DEVICE_FARM_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker compose --env-file server.env config
docker compose --env-file server.env build --pull
docker compose --env-file server.env up -d
docker compose --env-file server.env ps
```

Compose 不挂载 Docker Socket、不创建第二套 PostgreSQL，并默认只把 8080 绑定到 `127.0.0.1`。需要跨主机访问时通过内网反向代理或修改 `DEVICE_FARM_BIND_ADDRESS`，不得直接暴露公网。

## 升级

1. 先执行数据库备份并记录当前 Server 镜像 digest、二进制版本和配置哈希；
2. 阅读发布说明，只执行本次新增的 `*.up.sql`，不要重放旧迁移；
3. 运行新二进制 `--check-config`；
4. 先停止一个旧实例，再启动一个新实例；MVP 单实例期间保持短维护窗口；
5. 验证 `/readyz`、`/metrics`、Host Agent heartbeat、pending Reservation 和两台设备状态；
6. 观察至少一个 Scheduler/Reaper 周期后再完成升级。

备份、故障处理和回滚分别见：

- [运维手册](../../docs/operations_runbook.md)
- [可观测性和告警](../../docs/observability.md)
- [回滚方案](../../docs/rollback.md)
