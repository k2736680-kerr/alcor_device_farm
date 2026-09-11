# DF-066 控制面 iOS 隧道与 HTTPS Gateway 部署包

状态：进行中（部署代码和 220 预发布静态验证通过，尚未启用 iOS profile 或切换正式流量）。

## 本轮完成

- 已登记 ADR-0033，并更新复用矩阵、架构对齐表、Alcor 对齐说明和逐步实施清单。
- `compose.yaml` 已移除 Server 直接发布 18181 的映射；Server 8081 只使用 Compose `expose`。
- 已新增固定 `alpine:3.22.1`、非 root 的 SSH tunnel 镜像，隧道使用 Server 网络命名空间并只转发 4811/4842。
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

## 保护边界

本轮只上传了 220 预部署目录的 Compose、tunnel/gateway 配置和验证脚本，并构建了 tunnel 镜像；没有执行 `docker compose up`、没有重启 220 现有 Server/Gateway/PostgreSQL，没有修改 171 正式服务、Host Agent、STF、模拟器或 NPS，也没有导入生产数据库。220 当前运行容器仍保留旧配置，待维护窗口明确启用 DF-066 时再按 profile 重建。

## 尚未签收

正式签收还需要在明确的下一阶段窗口中补齐 SSH 远端账号参数，使用 `--profile ios` 启动 sidecar/Gateway，真实验证 Mac/Baguette `simulators.json`、18181 TLS 握手和未认证请求 401；验证后保持 iOS 开关和正式流量策略不变，另行决定是否发布。
