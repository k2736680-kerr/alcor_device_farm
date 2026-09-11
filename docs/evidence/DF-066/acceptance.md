# DF-066 控制面 iOS 隧道与 HTTPS Gateway 部署包

状态：通过（220 预发布 iOS profile 已真实启动、验证并完成 disabled 回滚演练；未切换 171 正式流量）。

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
| 220 正式预发布 iOS profile 启动 | 通过，Server/tunnel/Gateway 均 healthy，restart count 0 |
| 18181 HTTPS 未认证 `/`、`/simulators.json` | 通过，均返回 401 |
| 18180 `/readyz`、18182 `/readyz` | 通过，均返回 200 |
| disabled 回滚演练 | 通过，iOS 容器停止、18181 关闭，18180/18182 保持健康；随后恢复 iOS profile |
| 171 Server/Agent/STF/隧道 | 通过，171 端口和容器保持 running，Agent service active |

## 保护边界

本轮在明确的预发布验证范围内重建了 220 Server，并启动了 iOS tunnel/Gateway；未停止或修改 171 正式服务、Host Agent、STF、模拟器或 NPS，也没有导入生产数据库。完成 disabled 回滚演练后，220 已恢复 iOS profile 运行状态。

## 尚未签收

正式生产切换仍未执行：171 Agent URL、NPS 后端和生产数据库都保持原状。以后如要正式切换，仍需单独维护窗口、生产备份、Agent URL 切换和 NPS 变更确认。
