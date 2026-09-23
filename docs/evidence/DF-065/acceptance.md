# DF-065 控制面瞬时切换准备与 Agent 端点验收

## 结论

通过。2026-09-11 已在 `10.0.80.220` 补齐 Host Agent 私网 API `18182`、切换配置模板和只读 readiness 检查。220 继续使用 HTTPS 控制台；本阶段没有切换生产数据库、171 Agent、STF、iOS 链路或 NPS。

## 端点与隔离

- `10.0.80.220:18180`：HTTPS 控制台和未来 NPS 后端，健康检查通过。
- `10.0.80.220:18181`：iOS Gateway 预留入口，已绑定 220 私网地址。
- `10.0.80.220:18182`：Host Agent HTTP API，只给 171 和未来 Host Agent 使用，不加入 NPS。
- PostgreSQL 只暴露 Compose 内部 `5432/tcp`，没有发布宿主机端口。
- PostgreSQL、Device Farm Server、HTTPS Gateway 三个容器均为 `healthy`。

## Readiness 验收

| 检查 | 结果 |
|---|---|
| 本地 `go test ./...` | 通过 |
| 本地 `go vet ./...` | 通过 |
| 220 `docker compose config --quiet` | 通过 |
| 220 `/healthz`、`/readyz`、`/metrics`、`/console/` | HTTPS 通过 |
| 未认证 `/api/v1/device-hosts` | 返回 401，拒绝访问 |
| 220 Agent API `18182` | TCP 可达 |
| 171 旧 Server `18080` | TCP 可达 |
| 220 端口监听 | 18180、18181、18182 均只绑定 `10.0.80.220` |
| 生产数据库备份 | 未提供，明确保留为正式切换步骤 |
| 脚本副作用 | 未停止服务，未修改 Agent 或 NPS |

Readiness 脚本在 220 的 Alpine `/bin/sh` 环境完成实测。验证期间修正了未认证 401 被 `curl --fail` 提前中止，以及 shell 字符集取反不兼容的问题；修正后完整检查通过。

## 尚未执行的正式切换

- 没有从 171 生成或导入生产设备域 PostgreSQL custom dump。
- 没有复制 Host Agent 认证 Token、STF API Token、STF Web Secret、Console 密码、数据库口令或其他生产 Secret。
- 没有把 171 Agent 的 `DEVICE_FARM_AGENT_SERVER_URL` 改为 `http://10.0.80.220:18182`。
- 没有建立 220 到 Mac 的 iOS Baguette SSH 隧道。
- 没有停止 171 Server，也没有修改 NPS 后端。

正式维护窗口只需完成生产备份和导入、对齐 Host Agent 认证 Token、注入 STF/iOS Secret、建立 Baguette 隧道、切换 Agent 地址并验证心跳/设备，最后切换 NPS。任一步失败时先把 Agent 指回 171，并保留旧 Server 和原生产数据用于回滚。
