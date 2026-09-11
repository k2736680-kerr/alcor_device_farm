# ADR-0031：220 控制面预部署与独立数据库实例

## 状态

已批准，作为 DF-064 的部署基线；本 ADR 只定义预部署和切换边界，不代表已经完成正式切换。

## 决策

1. `10.0.80.220` 作为 Device Farm 控制面中心，承担 Device Farm Server、Console 和独立设备域 PostgreSQL。
2. 设备域 PostgreSQL 使用独立 Docker Compose 服务、独立数据目录、独立数据库角色、独立备份目录和独立资源限制；不复用 220 现有 PostgreSQL 实例、`alcor` 数据库、Schema、账号或数据目录。
3. 预部署目录使用 `/data/stacks/alcor-device-farm`，与已有 `/data/stacks/mongodb` 同级；不修改现有评估后台目录和容器。
4. 预部署 Server 使用 `10.0.80.220:18180`，避开现有评估后台的 `18080`；PostgreSQL 只在 Compose 网络内监听。
5. 预部署阶段继续使用 171 上现有 STF，220 只通过 STF Adapter 访问 7100；不暴露 RethinkDB 或 ADB 端口，不迁移 171 的正式 STF、模拟器和 Host Agent。
6. 新增服务器通过 Host Agent 主动连接 220；Server 不通过 SSH 或 Docker Socket 管理远程主机。正式切换前不改变 171 Agent 的 Server URL。
7. 预部署必须支持独立备份、健康验证、容器重启、版本回滚和最小化切换。不得以第二个 Server 连接同一生产设备数据库的方式做双活。

## 原因

- 220 当前已有评估后台和共享 PostgreSQL，独立实例能避免资源、权限和迁移互相影响；
- 220 没有 KVM，应只运行控制面，不运行 Emulator；
- 同级 `/data/stacks` 目录符合现有运维习惯，也便于单项目备份和回滚；
- 预部署先验证新控制面，正式切换才改变 171 Agent 指向，能把维护窗口压缩到服务切换和心跳收敛时间。

## 不在本 ADR 范围

- 不新增 Alcor Case、Dataset、Run、Result、Artifact 或业务数据库；
- 不迁移现有评估后台；
- 不在本阶段集中迁移 STF/RethinkDB；
- 不把 220 变成模拟器宿主机。
