# DF-064 220 控制面预部署与独立数据库隔离验收

## 结论

通过。`10.0.80.220` 已在 `/data/stacks/alcor-device-farm` 完成独立预部署；Device Farm PostgreSQL、Server 和 HTTPS Gateway 均为 healthy。`10.0.30.171` 的正式 Server、Agent、STF、模拟器及 220 现有评估后台均未切换或修改。

## 部署与隔离

- Compose 项目名：`alcor-device-farm-control-plane`。
- PostgreSQL 使用独立容器、角色、数据库和 bind mount；宿主机未发布 5432。
- Server 资源上限 512 MiB/1 CPU，PostgreSQL 768 MiB/1.5 CPU，Gateway 64 MiB/0.25 CPU。
- HTTPS Gateway 独占 `10.0.80.220:18180`；Server 业务端口只在 Compose 网络内暴露，iOS Gateway 预留 18181。
- 预部署 TLS 证书和 Console Argon2id 用户文件只在远端 Secret 目录；数据库密码、Service/Agent Token、密码哈希和会话值未进入 Git 或证据。
- STF Adapter 在未取得正式 API Token 前保持关闭；220 到 171 的 `7100` 网络探测返回 HTTP 302，未访问 RethinkDB 或 ADB。

## 验收结果

| 检查 | 结果 |
|---|---|
| 本地 `go test ./...` | 通过 |
| 本地 `go vet ./...` | 通过 |
| 220 `docker compose config --quiet` | 通过 |
| 220 镜像构建（Go + Console 生产构建） | 通过 |
| 独立 PostgreSQL migration | 000001～000019 全部通过 |
| 独立数据库 public 表 | 16 张，均为 Device Farm 设备域表 |
| PostgreSQL 宿主机端口 | 未发布 |
| `/healthz`、`/readyz`、`/metrics`、`/console/` | HTTPS 200 |
| 未认证设备 API | HTTPS 401 |
| Console 登录、当前会话、注销 | 201 / 200 / 200，Session Cookie 为 Secure |
| 数据库备份 | custom dump 与 SHA-256 文件生成成功，权限 0600 |
| PostgreSQL/Server/Gateway 重启恢复 | 三者最终均为 healthy，HTTPS ready 200 |
| 220 现有评估后台 18080 | 重启验收后仍返回 HTTP 200 |
| 171 STF 7100 | 重启验收后仍返回 HTTP 302 |

空载观测时，本项目三个容器内存约为 PostgreSQL 37 MiB、Server 7.5 MiB、Gateway 19 MiB，均低于配置上限。

## 首次启动修正

- bind mount 初始 owner 不符合 PostgreSQL 容器 UID，修正为 UID 70 后健康；部署文档已写明。
- Console Secret 初始父目录不可遍历，修正目录和文件 owner/mode 后 Server 健康；部署文档已写明。
- Alpine BusyBox `sha256sum` 不支持 `--version`，备份脚本改用可移植的 `command -v` 检查后真实备份通过。
- Console Secure Cookie 不能在明文 HTTP 保持会话，因此增加独立、无特权、只读文件系统的 Nginx TLS Gateway；HTTPS 登录全链路通过。

## 未执行的正式切换

- 没有修改 171 Agent 的 Server URL 或 Agent Token。
- 没有停止 171 正式 Server、STF、Appium 或 Emulator。
- 没有迁移生产设备域数据，也没有让两个 Server 连接同一生产数据库。
- 没有把 220 预部署入口加入现有评估后台导航。

正式切换应另开维护窗口，先备份生产设备域数据库，再导入 220 独立数据库、切换 Agent 指向、验证 Host 心跳和设备状态；失败时将 Agent 指回旧 Server。预部署阶段已把切换窗口压缩为数据导入与 Agent 配置切换，不需要现场构建镜像或初始化服务。
