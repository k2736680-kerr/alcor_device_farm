# Device Farm Server 部署

该部署只包含设备农场 Server，不复制 PostgreSQL、STF 或 Alcor。PostgreSQL 使用现有数据库服务，STF 继续使用 `deploy/stf`，Host Agent 继续使用 `deploy/docker-emulator`。

## 方式一：systemd

前置条件：Linux、Go 1.24+、Node.js 24、pnpm 11、PostgreSQL 客户端、可访问的 PostgreSQL 数据库。安装脚本会先从 `console/` 生成带内容哈希的生产静态资源，再编译嵌入资源的 Server；全新源码环境不依赖未纳入 Git 的本地 `dist/`。

```sh
sudo ./scripts/install-device-farm-server.sh
sudoedit /etc/alcor-device-farm/server.env
```

至少填写：

- `DEVICE_FARM_DATABASE_URL`；
- `DEVICE_FARM_SECURITY_SERVICE_TOKEN`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`；
- 启用 STF 时填写 `DEVICE_FARM_STF_BASE_URL` 和 `DEVICE_FARM_STF_API_TOKEN`；启用管理员 Web 远控时再填写 `DEVICE_FARM_STF_WEB_URL`、`DEVICE_FARM_STF_WEB_AUTH_SECRET`、`DEVICE_FARM_STF_WEB_USER_NAME`、`DEVICE_FARM_STF_WEB_USER_EMAIL`。

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
5. 验证 `/readyz`、`/metrics`、Host Agent heartbeat、pending Reservation 和当前单台设备状态；
6. 观察至少一个 Scheduler/Reaper 周期后再完成升级。

备份、故障处理和回滚分别见：

- [运维手册](../../docs/operations_runbook.md)
- [可观测性和告警](../../docs/observability.md)
- [回滚方案](../../docs/rollback.md)

## Console(Web 控制台)

Console 的静态资源通过 `//go:embed` 内嵌在 Server 二进制中，**部署 Server 即部署 Console**，不需要独立的静态文件服务器或 CDN。

### 启用

在 `server.env` 中开启并指向 Console 用户文件。systemd 部署使用：

```sh
DEVICE_FARM_CONSOLE_ENABLED=true
DEVICE_FARM_CONSOLE_USERS_FILE=/etc/alcor-device-farm/console-users.yaml
```

文件使用 `0640 root:device-farm-server`，父目录使用 `0750 root:device-farm-server`。Compose 部署把 `deploy/server/secrets/` 只读挂载到容器；在该目录创建未纳入 Git 的 `console-users.yaml`，并设置：

```sh
DEVICE_FARM_CONSOLE_USERS_FILE=/run/secrets/device-farm/console-users.yaml
```

`console-users.yaml` 只包含 Console 登录用户（Argon2id 哈希密码与角色），例如 `tmp/console-users.yaml` 的本地形态；生产环境由部署机密机制生成。`DEVICE_FARM_CONSOLE_DEVELOPMENT_INSECURE` 仅限开发环境且 Server 必须绑定回环地址，生产环境必须保持 false。

### STF 原生 Web 远控

管理员远控不复用 Console 密码，也不把 STF API Token 发给浏览器。Server 使用与 STF `--auth-secret` 相同的受限 Secret 签发 30 秒短时 JWT，STF 建立自身 Session 后立即通过重定向从地址移除 JWT。配置要求：

- `DEVICE_FARM_STF_WEB_URL` 是管理员浏览器可访问的 STF HTTPS 地址；7100、7110 和设备画面端口必须位于同一受控内网边界；
- `DEVICE_FARM_STF_WEB_AUTH_SECRET` 必须与 `deploy/stf/.env` 的 `STF_AUTH_SECRET` 完全一致，不能复用 Service/Agent Token；
- `DEVICE_FARM_STF_WEB_USER_NAME/EMAIL` 必须与生成 `DEVICE_FARM_STF_API_TOKEN` 的 STF 用户一致，否则页面无法控制 Server 已 claim 的设备；
- `DEVICE_FARM_CONSOLE_REMOTE_LEASE` 默认 60 秒，心跳默认 15 秒。关闭页面的主动释放失败时，Reservation Reaper 在租约和 grace period 后兜底；
- 远控响应使用 `no-store` 和 `Referrer-Policy: no-referrer`。反向代理不得记录包含 `jwt` 查询参数的完整 URL；STF App 会在首次成功请求后移除该参数。

### iOS Simulator Baguette 原生远控

iOS 人工远控复用固定版本 Baguette 的原生 Web UI、画面流和 Host HID，不创建人工 Appium/XCUITest Session，也不控制 macOS 桌面。Server 在独立端口提供受控 Gateway；每个 HTTP/WebSocket 请求都绑定 active Reservation 和唯一目标 UDID。

- `DEVICE_FARM_IOS_REMOTE_CONTROL_BAGUETTE_URL` 必须是 Server 网络空间中的回环地址；部署时通过 SSH 隧道转发到 Mac `127.0.0.1:8421`；
- `DEVICE_FARM_IOS_REMOTE_CONTROL_GATEWAY_ADDRESS` 是独立 Gateway 监听地址，不能与主 API 端口相同；`PUBLIC_URL` 是浏览器可访问的该 Gateway 根地址；
- `DEVICE_FARM_IOS_REMOTE_CONTROL_GATEWAY_SECRET` 使用至少 32 字节的独立随机值，只进入权限为 `0600` 的 Server Secret；不得复用 Console 密码或 Service/Agent Token；
- 返回浏览器的入口只含 Device/Reservation 绑定的短时签名票据，不含 Mac/Baguette 回环地址、Fence、Appium、WDA 或 Session Grant；
- Gateway 只代理目标 `/simulators/{udid}` 和原生静态资源；设备墙、其他 UDID、boot/shutdown、插件和 bakery 路由全部拒绝；
- Console 的 15 秒心跳使用 60 秒滑动租约，持续操作没有固定一小时上限；浏览器停止心跳后由 Reaper 释放。

### 访问与健康检查

- 入口：`http://<server>:8080/console/`，SPA 路由由服务端回退到应用壳；
- 存活与就绪：沿用 `/healthz`、`/readyz`（真实检查 PostgreSQL 与关键表）；
- Console 入口可用性：`curl --fail http://127.0.0.1:8080/console/`，返回 200 即静态资源可服务。

部署完成后执行自动自检：

```sh
export DEVICE_FARM_CONSOLE_ORIGIN=https://farm.example.internal
sh ./scripts/verify-console-deployment.sh
```

回环开发环境可显式设置 `DEVICE_FARM_CONSOLE_ALLOW_HTTP=1`。脚本验证健康/就绪、未认证拒绝、CSP/HSTS、防缓存、哈希资源长缓存和构建产物凭证扫描；它不接收或输出 Console 密码、Service Token 或 STF Token。

仅在封闭验收环境使用自签名证书时，可临时设置 `DEVICE_FARM_CONSOLE_INSECURE_TLS=1` 跳过证书链校验；正式环境不得设置，必须使用受信任证书。

### 内置 Web 安全（随二进制生效，无需额外配置）

- CSP：`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'`；
- `X-Frame-Options: DENY`、`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`、`Permissions-Policy` 全禁；
- 会话 Cookie `HttpOnly + SameSite=Strict`，写操作要求 `X-CSRF-Token` 与服务端哈希匹配；
- 缓存策略：`index.html` 与 SPA 回退路由 `Cache-Control: no-store`，内容哈希资源（`assets/index-<hash>.*`）`public, max-age=31536000, immutable`。

### HTTPS / 受控内网

生产环境必须置于同机 HTTPS 反向代理之后或受控内网，Server 默认只绑定 `127.0.0.1`。可从 `nginx-console.conf.example` 起步，配置正式域名、证书和访问控制；示例同时设置 TLS 1.2/1.3、HSTS、HTTP→HTTPS 跳转和同源代理。CSP `connect-src 'self'` 要求反向代理保持同源路径（例如 `https://farm.example.com/console/`），不得跨域改写或拆分静态资源。

Server 只信任来自回环对端的 `X-Forwarded-For`；反向代理跨主机部署时不会信任该头，登录限流将按代理地址聚合。不要为了显示客户端地址而把 Server 直接发布到私网并信任任意 XFF。

### 升级与回滚

Console 随 Server 二进制整体升级/回滚，无独立版本。升级后验证 `GET /console/` 返回 200 并完成一次浏览器登录；入口 no-store + 内容哈希资源保证用户刷新后立即获得新版本，不会命中旧缓存。
