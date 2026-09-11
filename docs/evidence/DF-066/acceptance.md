# DF-066 控制面 iOS 隧道与 HTTPS Gateway 部署包

状态：进行中（部署代码和 220 预发布静态验证通过，已发现并修正 Baguette 远端端口映射，尚未启用 iOS profile 或切换正式流量）。

## 本轮完成

- 已登记 ADR-0033，并更新复用矩阵、架构对齐表、Alcor 对齐说明和逐步实施清单。
- `compose.yaml` 已移除 Server 直接发布 18181 的映射；Server 8081 只使用 Compose `expose`。
- 已新增固定 `alpine:3.22.1`、非 root 的 SSH tunnel 镜像，隧道使用 Server 网络命名空间并只转发 4811/4842。
- 已按正式 171 隧道配置修正端口：220 本地 4842 转发到 Mac Baguette 的 8421。
- 已新增独立 Nginx TLS Gateway，profile 名为 `ios`，默认关闭；证书和 SSH Secret 仍只从远端未纳入 Git 的目录挂载。
- 已新增只读的 `verify-control-plane-ios-predeployment.sh`，不停止服务、不修改 Agent、NPS、数据库或 171。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| 控制面契约测试 | 通过 |
| `git diff --check` | 通过 |
| 220 `docker compose --profile ios ... config --quiet` | 通过 |
| 220 tunnel 镜像构建 | 通过，镜像 `alcor-device-farm-ios-tunnel:1.0.0` |
| tunnel 容器身份 | `uid=10001(ios-tunnel) gid=10001(ios-tunnel)` |
| 220 iOS Nginx `nginx -t`（非 root、真实证书挂载、Compose 网络） | 通过 |
| 171/220 隧道密钥 SHA-256 对齐 | 通过 |
| Mac `127.0.0.1:8421/simulators.json`（经 220 临时 tunnel） | 通过，HTTP 200，3942 字节 |
| 220 隔离 TLS Gateway 握手（iOS 功能关闭） | 通过 TLS，HTTP 502 符合关闭状态；未开启 Server 8081，因此未期待 401 |

## 保护边界

本轮只上传了 220 预部署目录的 Compose、tunnel/gateway 配置和验证脚本，并构建了 tunnel 镜像；没有执行 `docker compose up`、没有重启 220 现有 Server/Gateway/PostgreSQL，没有修改 171 正式服务、Host Agent、STF、模拟器或 NPS，也没有导入生产数据库。220 当前运行容器仍保留旧配置，待维护窗口明确启用 DF-066 时再按 profile 重建。

## 尚未签收

正式签收还需要在明确的下一阶段窗口中使用 `--profile ios` 启动 sidecar/Gateway，并同时启用 220 Server 的 iOS 配置，真实验证 18181 未认证请求 401。当前已补齐 220 的 Mac SSH 主机参数但没有启动服务；验证后保持 iOS 开关和正式流量策略不变，另行决定是否发布。
