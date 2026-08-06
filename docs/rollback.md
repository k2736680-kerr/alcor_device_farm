# 发布与回滚方案

## 回滚原则

- 应用二进制/镜像和数据库迁移分开处理；
- 优先回滚 Server/Agent 版本，不自动执行 down migration；
- `000001_device_domain.down.sql` 会删除设备域表，只允许空环境演练或已经切换到可验证恢复点时使用；
- 有数据环境的数据库回滚采用“恢复到新数据库并切换连接”，不在原库覆盖恢复；
- STF 回滚不能修改 Device Farm Reservation 真相。

## 发布前恢复点

记录：

- Server 和 Agent 的 version、commit、镜像 digest或二进制校验和；
- 当前配置文件哈希和 Secret 版本号，不记录 Secret 原值；
- 本次新增 migration 文件列表；
- PostgreSQL custom-format 备份路径和恢复验证结果；
- STF/RethinkDB 镜像 digest 与必要备份。

## 仅应用回滚

适用于新版本尚未写入不兼容数据：

1. 停止新 Reservation 入口或将相关 Host draining；
2. 停止新 Server，恢复上一版本二进制/镜像和配置；
3. 启动并检查 `/readyz`、`/console/`、指标、Agent 心跳和 Reservation；
4. 不执行 down migration。

Console 与 Server 使用同一二进制版本。回滚后必须强制刷新浏览器并确认入口 HTML 为 `no-store`、引用的内容哈希资源属于目标旧版本；不得单独回滚或复制静态目录形成与 Server 不一致的 Console。

## 数据库恢复回滚

适用于 migration 或新版本数据写入导致旧版本不兼容：

1. 停止 Server 和 Host Agent，阻止继续写入；
2. 保留故障数据库作为只读调查副本；
3. 创建新的空 PostgreSQL 数据库；
4. 使用发布前 dump 执行 `pg_restore --exit-on-error --no-owner`；
5. 在隔离端口启动上一版本 Server，验证 `/readyz`、表约束、Reservation 和审计；
6. 切换 `DEVICE_FARM_DATABASE_URL` 到恢复库并启动上一版本；
7. 保留故障库到复盘结束，不直接删除。

恢复点之后创建或完成的 Reservation、审计和健康事件不会存在于旧备份中。回滚前必须记录受影响时间窗，并由 Alcor/DaFit 侧确认相关运行是否需要重试。

## 回滚验收

- Server version/commit 为目标旧版本；
- `/healthz`、`/readyz`、`/metrics` 正常；
- `/console/` 可登录，`verify-console-deployment.sh` 通过，静态资源版本与目标 Server commit 一致；
- active Reservation、Device、Session 和 STF claim 可核对；
- Scheduler/Reaper/Reconciler 在两个周期内收敛；
- 无双占、无永久 recycling、无 Token 泄露；
- 回滚过程、操作者、原因、恢复点和影响范围进入变更审计。
