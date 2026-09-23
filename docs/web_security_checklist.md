# Device Farm Console Web 安全清单（DF-028）

本清单逐项核对 Device Farm Console（浏览器侧）的安全控制，对应 DF-028「控制台部署、安全和真实 Web 验收」。服务端身份/权限/审计/脱敏见 [security_and_audit.md](security_and_audit.md)，部署与回滚见 [deploy/server/README.md](../deploy/server/README.md) 和 [rollback.md](rollback.md)。

## 1. HTTPS 与受控内网访问

| 要求 | 实现 | 验证 |
|---|---|---|
| 生产默认只绑定回环地址 | `config.Default().Server.Address = "127.0.0.1:8080"`（`internal/config/config.go`）；Compose 只发布 `127.0.0.1:8080` | 新机器按 `deploy/server/README.md` 部署后，外部地址不可直连 8080 |
| HTTPS 反向代理或受控内网 | `deploy/server/nginx-console.conf.example` 提供同机 TLS 1.2/1.3、HSTS、HTTP→HTTPS 和同源代理基线；CSP `connect-src 'self'` 保证反代保持同源 | 反代后 `https://host/console/` 正常登录与调用，HTTP 跳转 HTTPS，HSTS 存在 |
| `development_insecure` 仅限回环 | 配置校验：开启时 `server.address` 必须为回环地址，否则启动失败（`internal/config/config.go:329`） | `--check-config` 拒绝非回环 + development_insecure 组合 |
| X-Forwarded-For 防伪造 | `clientAddress()` 仅在直连对端为回环时信任 XFF 首条；任意私网/公网直连均忽略（`internal/api/console.go`） | 登录限流来源地址不被同网段请求伪造 |

## 2. 响应安全头（所有 /console/* 响应）

实现位置：`internal/consoleui/handler.go::setSecurityHeaders`。

| 头 | 值 |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'` |
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `no-referrer` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` |

单测：`internal/consoleui/handler_test.go::TestHandlerServesEntryAndAssetsWithCachePolicy/security headers`。

## 3. Cookie 与 CSRF

实现位置：`internal/consoleauth/service.go`、`internal/auth/auth.go`。

| 要求 | 实现 | 验证 |
|---|---|---|
| 会话 Cookie HttpOnly | `http.Cookie{HttpOnly: true}`（consoleauth:227） | 浏览器 DevTools 检查 |
| SameSite=Strict | 会话与 CSRF Cookie 均 `SameSiteStrictMode` | 跨站请求不带 Cookie |
| CSRF 双提交 | 登录返回 `device_farm_csrf` Cookie + JSON 中的 CSRF Token；写请求需 `X-CSRF-Token` 头等于 Cookie 值，且服务端用会话绑定的 `csrf_hash` 校验（consoleauth:159） | 无头或错误头返回 401/`CSRF_VALIDATION_FAILED` |
| 登录限流 | `console.login_window` / `login_max_failures`，按 userID + 来源地址限流 | 连续错误密码触发拒绝 |
| 会话过期 | `console.session_max_age` 与 `session_idle_timeout` 默认均为 30 天；Cookie 仅保存随机会话令牌，不保存明文密码，过期后 401 | 30 天内浏览器重开仍可复用；过期会话调用 API 返回 401 |

## 4. 防缓存与静态资源版本

| 要求 | 实现 | 验证 |
|---|---|---|
| 入口防缓存 | `index.html` 与所有 SPA 回退路由 `Cache-Control: no-store`（`internal/consoleui/handler.go::setCacheControl`） | 单测断言 no-store；浏览器刷新立即生效 |
| 静态资源版本 | Vite 产物带内容哈希（`assets/index-<hash>.js/.css`），响应 `Cache-Control: public, max-age=31536000, immutable` | 新构建产物文件名变化，旧缓存不会命中 |
| API 防缓存 | 所有统一 JSON API 响应由 `internal/httpx::writeJSON` 设置 `Cache-Control: no-store` + `Pragma: no-cache` | 浏览器不缓存列表、预约、审计或错误响应 |

## 5. 认证、越权与直接内部访问

| 场景 | 预期 | 验证方式 |
|---|---|---|
| 未认证访问 /console/ API | 401 统一错误结构，无内部堆栈 | E2E / 集成测试 |
| 越权（跨 owner 预约、改 owner） | 403；owner 由会话绑定不可修改（DF-027 已验） | 集成测试 + E2E |
| 直接访问内部端口（STF 7100/7110、RethinkDB 28015/8080、ADB 5037） | RethinkDB/ADB 不发布；STF 和 Server 只绑定回环并由受控代理提供入口 | 从浏览器网络执行机探测，除批准入口外均不可达 |
| 未匹配路径 | `/` 返回统一 404 | curl 验证 |

## 6. Token 不落浏览器

| 要求 | 实现 | 验证 |
|---|---|---|
| STF 管理 Token 仅存 Server 进程环境 | Adapter 内部使用；按 ADR-0010，Console 不调用或展示 `remoteConnect` TCP 地址 | 构建产物/源码/E2E 页面扫描 0 命中，预约页面无 STF 远控按钮 |
| 页面不展示任何 API Token | Console 只展示 request ID、资源信息与脱敏数据 | Token 扫描脚本 |

## 7. 健康检查与回滚

- `/healthz` 存活；`/readyz` 真实检查 PostgreSQL 与关键表，不可用时 503（`internal/server/server.go`）；
- `/metrics` 无敏感标签、不输出 Token/Endpoint（DF-023）；
- Docker 与 systemd 源码安装都会先执行 pnpm 生产构建，再把 Console 嵌入 Server；不依赖 Git 忽略的本地 `dist/`；
- Compose 只读挂载 `deploy/server/secrets/`，systemd 使用 `0750 root:device-farm-server` 配置目录；
- Console 随 Server 二进制升级/回滚，入口 no-store 保证无缓存残留（`deploy/server/README.md` Console 章节）。

## 验收记录

- 代码单测：`internal/consoleui`（安全头 + 缓存策略）、`internal/consoleauth`（CSRF/会话）、`internal/api`（认证/越权/限流）；
- 真实 Web E2E：见 `docs/evidence/DF-028/acceptance.md`；
- 生产入口静态/安全自检：`scripts/verify-console-deployment.sh`；
- Token 扫描：见 `docs/evidence/DF-028/acceptance.md`。
