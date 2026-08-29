# DF-058 独立控制台安全保持登录验收

## 结论

通过。独立 Device Farm Console 继续复用既有 PostgreSQL Session、HttpOnly/SameSite Cookie、CSRF 双提交校验、Argon2id 配置用户和注销吊销链路；默认绝对会话期限与空闲期限由 8 小时/30 分钟统一调整为 30 天。账号、角色和密码哈希 Secret 未改动，浏览器和仓库均不保存明文密码；没有新增 API、表、状态或第二套认证实现。

## 实现与安全边界

- `console.session_max_age` 默认值为 `720h`，新登录的数据库 `expires_at` 和持久 Cookie `Expires/Max-Age` 继续由同一值生成。
- `console.session_idle_timeout` 默认值为 `720h`，不再因旧的 30 分钟空闲窗口频繁退出。
- 登录页明确显示“本浏览器将安全保持登录 30 天，不保存明文密码”。
- 主动注销仍吊销数据库 Session 并删除 Session/CSRF Cookie；会话到期、服务端吊销和配置用户变化仍会拒绝认证。
- 正式 Console 用户文件继续位于独立只读 Secret volume，只包含 Argon2id 哈希。本任务未修改用户名、角色、密码或 Secret volume。
- 全仓检索确认登录页没有使用 localStorage/sessionStorage 保存账号或密码；已有 sessionStorage 仅保存短时远控页面状态，与登录凭证无关。

## 自动化验收

- `go test -p 1 ./...`：通过，使用真实 PostgreSQL 集成测试库串行执行数据库包。
- `go vet ./...`：通过。
- Console Vitest：9 个测试文件、50 项测试通过；登录页测试覆盖 30 天安全提示。
- Console `pnpm build`：通过；Orval 生成后无漂移，TypeScript 未使用检查通过。
- `deadcode -test ./...`：零结果。
- Staticcheck `all,-ST*`：通过。
- `git diff --check`：通过。

## 正式部署与浏览器验证

- 正式镜像：`alcor-device-farm:df058-remember-login-20260829`。
- Server：`running/healthy`；`/readyz` 和 `/console/` 均成功。
- Server 启动配置记录 `console_session_max_age=2592000000000000ns`、`console_session_idle_timeout=2592000000000000ns`，均等于 30 天。
- 正式浏览器加载到新登录页并显示 30 天保持登录提示；静态页面没有密码、Token 或内部凭证。
- Server 容器替换后同步重建 iOS SSH 隧道；隧道 `running`、restart count 为 0，Server 网络空间的 `127.0.0.1:4811` 和 `127.0.0.1:4842` 均可达。

## 三台长期设备真实回归

第一轮 iOS 冒烟从 Linux Host 访问仅存在于 Server 网络空间的 Fence 回环地址，被预期拒绝；该轮 Reservation/Session 已由 `finally` 清理。随后把同一无破坏脚本放入 Server 网络空间重跑，三台均通过：

| 平台 | Device ID | Provider ref | `/source` | 最终状态 |
|---|---|---|---:|---|
| Android | `ef26de28-b0d8-4afa-894d-7a8b1d3716ad` | `emulator-ef26de28-b0d8-4afa-894d-7a8b1d3716ad` | 22,419 bytes | `ready/healthy`，失败次数 0 |
| iOS | `32155337-3478-4b5e-9ea7-c856e6f3c749` | `45779828-41FC-4DEC-A38F-8877F9321426` | 41,127 chars | `ready/healthy`，失败次数 0 |
| iOS | `eb3aad90-8f06-45ea-a5ea-2465185a5b96` | `A66EF5FD-C95A-49F3-978D-5E32ADC710BB` | 41,127 chars | `ready/healthy`，失败次数 0 |

Appium Session 和 Reservation 均已释放；未执行 delete、rebuild、reimage、替代 create 或数据卷清理。

## 正式历史与部署备份清理

冒烟产生的 6 条 Reservation、6 条 Device Session、15 条审计和 6 条幂等记录已清理。最终正式库以下计数均为 0：Reservation、Session、Health Event、Audit Event、Host Command、Provisioning Job、Image Preparation、Idempotency、Console Session。

部署完成并验证后，按用户授权删除：

- 本机 DF-056 清理前 PostgreSQL dump 及其空目录；
- Server 上 DF-047 前备份、2026-08-18 备份及校验文件；
- 4 个退出的 Server 回滚容器；
- DF-053、DF-055、DF-056、DF-056-r2 四个旧 Server 镜像标签/不再引用的镜像层；
- DF-038 迁移临时目录、DF-047～DF-058 部署临时目录、旧 SSO 构建目录和旧 Server inspect 备份。

上述远端临时目录合计约 125 MB；Docker 镜像占用报告由 33.06 GB 降至 33.03 GB，另删除本机约 0.4 MB dump。共享镜像层按 Docker 实际引用保留，没有执行影响其他项目的全局 image/volume/build-cache prune。

明确保留：当前 DF-058 Server/镜像、iOS 隧道与 SSH 目录、PostgreSQL/数据卷、Android Emulator/数据卷、运行中的 Host Agent、本地 Registry、Nginx 当前配置和三台长期虚拟机。另有一个仍运行的 DF-038 测试 PostgreSQL 容器不属于本次备份/回滚范围，未越权删除。

全过程未把 Console 密码、Token、Cookie、Session Grant、数据库口令或 SSH 私钥写入代码、证据或 Git。
