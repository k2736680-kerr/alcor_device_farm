# 设备农场运维手册

## 日常检查

```sh
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/metrics
curl --fail http://127.0.0.1:8080/console/
```

每天关注：Server/数据库就绪、Console 入口、Agent 心跳、ready 设备数、pending Reservation 年龄、recycling/quarantined 设备和失败 Host Command。Console 入口必须返回 `Cache-Control: no-store`；浏览器显示的状态必须来自刷新后的 Server API。

iOS macOS Host 还要检查 `com.alcor.device-farm.appium-hub`、`com.alcor.device-farm.appium-node` 和 `com.alcor.device-farm.host-agent` 三个 launchd 服务；Hub、Node、Session Fence 只监听回环地址。Appium Device Farm Hub/Node 中由 `Alcor-DF-` 前缀标识的动态 inventory 必须与 CoreSimulator 中实际存在的受管 Simulator 一致。Hub 和 Node 都由 `run-service.sh` 检查 `/status` 与 iOS inventory，连续三次无响应时退出并由 launchd 拉起。Host 自动维护后的默认恢复宽限为 90 秒；宽限内停止新预约但不累计设备隔离次数。检查结果只记录数量、状态、版本和脱敏 ID 前缀，不复制完整 UDID、Session、WDA/MJPEG 地址或环境文件。

## 备份

升级、迁移、批量维护和 Token 轮换前执行：

```sh
export DEVICE_FARM_DATABASE_URL='由 Secret 管理器注入'
./scripts/backup-device-farm.sh /var/backups/alcor-device-farm
```

脚本生成 PostgreSQL custom-format dump 和 SHA-256 校验文件、权限设为 0600，并且不把数据库 URL 输出到终端。备份文件应复制到独立存储并按公司策略加密、保留和验证恢复。

至少每个发布周期在隔离数据库执行一次：

```sh
createdb alcor_device_farm_restore_test
pg_restore --exit-on-error --no-owner --dbname alcor_device_farm_restore_test <backup.dump>
```

禁止直接在生产数据库上试恢复。

## 常见故障

### `/healthz` 失败

检查 Server 进程、监听地址、systemd/Compose 状态、端口冲突和最近发布。进程反复退出时先回到已知可用二进制，不执行 down migration。

### `/readyz` 返回 503

检查 PostgreSQL 地址、证书、账户、连接上限和网络。Server 保持不就绪，Alcor Worker 不应继续创建新 Reservation。数据库恢复后 `/readyz` 自动恢复。

### Agent heartbeat 过期

检查 `alcor-device-host-agent`、Agent Token、Host ID、Server 网络和系统时间。Host offline 后新任务会停止分配；不要手工把数据库 Host 改回 online。

### Reservation 长时间 pending

依次检查 Pool active、Image ready、Host online、设备 ready+healthy、capabilities 匹配、`max_concurrency` 和 STF claim。容量确实不足时等待或调整固定目标参数，不复制 Scheduler。

### Device 长时间 recycling

检查对应 rebuild Host Command、Agent 日志、Docker 容器/网络/volume 清理和 Appium 健康。失败耗尽后设备应 quarantined；修复根因后使用既有 rebuild/unquarantine API，并填写原因。

### STF 不可用

Reservation claim/release 按既有补偿策略处理。不要直接修改 `device_reservations` 或把 STF 状态当作真相。恢复 STF 后按审计和 pending/active 状态复核。

### iOS Host 进入 maintenance

先查看心跳中的中文 readiness 原因，再分别检查 Hub、动态发现 Node、Xcode license、固定工具链版本和 Runtime/Device Type 目录。`appium driver doctor xcuitest` 只在 Host Agent 进程启动时作为门禁执行；本次进程已通过后不会在 WDA 编译或 Session 运行期间重复执行。不要通过手改 Host 状态掩盖失败，修复依赖或重启 Agent 后由健康心跳自动恢复 online。

### iOS Simulator 已删除但插件仍显示

先确认没有 active Reservation 和绑定 Session，再通过正式 Device DELETE API 重试清理。Agent 会使用 Appium Device Farm 官方本机注册接口精确注销 Hub/Node 中同一受管名称和 UDID；Node 正在由看门狗恢复时，删除命令会在受控上下文内幂等重试，单端最长等待 90 秒。禁止直接编辑插件数据库，也不能按模糊前缀删除固定 Simulator 或真机。仍不收敛时排空 Host，依次重启 Node、Hub、Agent并在 120 秒内复核 inventory。

### WDA 或 Appium Session 残留

先用 Server 查询 Reservation 与 `device_sessions` 真相，通过正式 release/Reaper/Fence DELETE 关闭会话。清理失败时设备应保持 quarantined，不能直接改成 ready。只有确认没有活动 Reservation/Session 后，才可在排空状态重启 Appium Node/Hub；恢复后确认无 WDA/xcodebuild 残留、动态 inventory 与 CoreSimulator 一致，再解除排空。

## 人工操作规则

- 禁止直接更新 Device/Reservation 状态绕过状态机；
- restart、rebuild、quarantine、unquarantine 和 force release 必须写明原因；
- 维护前先将 Host draining，等待在用 Reservation 结束；
- iOS 维护同时禁用对应 iOS Pool；确认 active Reservation 为零后才可重启 Hub/Node/Agent，恢复顺序和检查项按 [iOS Host 部署说明](../deploy/ios-host/README.md) 执行；
- Docker Socket 只由 Host Agent 账户访问，Server 和 Alcor 不加入 docker 组；
- 所有命令、日志和证据必须脱敏，使用 request ID 关联。

## 发布后检查

1. `device-farm-server --version` 与发布 commit 一致；
2. `--check-config` 成功，`/readyz` 为 200；
3. `/metrics` 的 build info、database ready 和 Agent 状态正确；
4. 当前单台设备最终为 ready+healthy；
5. 创建并释放一次测试 Reservation；
6. 释放后设备完成 recycling/rebuild 并回池；
7. 日志、审计和数据库普通字段无 canary Secret。
8. 对正式 HTTPS 入口执行 `DEVICE_FARM_CONSOLE_ORIGIN=https://... sh ./scripts/verify-console-deployment.sh`；
9. 使用受控 Console 账号登录，完成资源查看和一次人工预约释放，随后注销并确认会话不可复用；
10. 检查 `index.html` no-store、哈希资源 immutable，并确认浏览器 Cookie/存储/网络中没有 Service、Agent 或 STF Token。
11. iOS Host 验证 Hub/Node/Agent 均由 launchd 托管、120 秒内恢复，创建一台动态 Simulator 完成明确 UDID 的 Session/source/rebuild/delete 闭环，最终无 `Alcor-DF-*` inventory、CoreSimulator 或 WDA 残留；
12. Android Host、STF、长期 Emulator 和 DaFit 冒烟在 iOS 发布或回滚后仍可用。
