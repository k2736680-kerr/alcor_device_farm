# DF-018 实施与验收证据

## 当前结论

STF Adapter、配置注入、inventory 可见性、两阶段 claim、失败补偿、release 顺序、短时 remoteConnect、owner 绑定、过期 remoteDisconnect、OpenAPI 和自动测试已实现。DF-017 的真实 STF 与两台 Emulator 验收仍被本机无 Linux/Docker 环境阻塞，因此 DF-018 当前状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- Adapter 固定使用 STF 3.7.9 官方 inventory、claim、release、remoteConnect API；
- 支持 Base Path、超时、1~5 次尝试、递增短退避、HTTP/网络错误分类；
- release 404 作为幂等成功，响应正文不进入错误，Token 不进入错误、JSON或配置日志；
- remoteConnect URL 拒绝凭证、query、fragment 和控制字符；
- Scheduler 先把 Device 置为 reserved，事务外 claim 成功后才激活 Reservation；
- claim 可重试失败保持 pending 并 5 秒退避，不可重试失败进入 failed；两种失败都按 reserved→recycling→ready 补偿 Device；
- claim 成功后若 Session ID 或数据库激活失败，调用 STF release 并补偿数据库；
- 主动释放和过期回收先 STF release，失败时 Reservation 保持 active 并写 `stf_release_failed` 审计；
- remote session 必须匹配 Reservation owner，TTL 30~3600 秒，同一幂等请求不重复 remoteConnect；
- remote session 复用 Device Session metadata，过期后 Reaper 先 remoteDisconnect 再清除入口；
- Reconciler 按 serial 核对 STF 可见性，只写健康事件，不覆盖 PostgreSQL 生命周期真相；
- Server 仅在 `stf.enabled=true` 时注入 Adapter，本地无 STF 模式保持既有 Mock 开发入口。

## 本地验证结果

执行：

```powershell
$env:DEVICE_FARM_GO='E:\AutoTestTools\Tools\go1.26.5\go\bin\go.exe'
$env:DEVICE_FARM_POSTGRES_BIN='E:\AutoTestTools\Tools\PostgreSQL-17.10\pgsql\bin'
./scripts/dev.ps1 -Task check
./scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键通过项：

```text
PASS TestClientUsesOfficialInventoryClaimReleaseAndRemoteConnectAPIs
PASS TestClientRetriesTransientFailureAndDoesNotLeakToken
PASS TestClientRejectsUnsafeConfigurationAndTreatsDeleteNotFoundAsReleased
PASS TestClientRejectsRemoteConnectURLContainingCredentialsOrQuery
PASS TestSTFClaimRunsBeforeReservationActivation
PASS TestRetryableSTFClaimFailureRestoresDeviceAndKeepsReservationPending
PASS TestTerminalSTFClaimFailureRestoresDeviceAndFailsReservation
PASS TestSTFClaimIsReleasedWhenSessionIDGenerationFails
PASS TestReservationReleaseKeepsDatabaseActiveUntilSTFReleaseSucceeds
PASS TestRemoteSessionIsOwnerBoundIdempotentAndDisconnectedAfterExpiry
PASS TestOneHundredConcurrentReservationsUseTwoDevicesWithoutDoubleAllocation
PASS TestConcurrentSchedulersRespectPoolMaximumBelowDeviceCount
PASS migration up/down/up
PASS go vet/test/build full suite
```

## Linux STF 真实验收

1. 完成 DF-017，启动固定版本 STF 3.7.9、RethinkDB 2.4.2 和两台 Docker Emulator；
2. 通过 Secret 注入专用 STF API Token，确认 Server 日志和配置输出不含 Token；
3. 创建两个 Reservation，确认每个在 STF claim 成功前都保持 pending，成功后才 active；
4. 让 STF claim 返回 4xx、5xx、超时和网络中断，分别核对 terminal/retryable 补偿、5 秒退避和无请求风暴；
5. 同时调度两台设备，确认最多两个 active、serial 一一对应、Appium UDID 不串设备；
6. 注入 release 临时失败，确认三次短重试后 Reservation 仍 active、有审计；恢复 STF 后确认 Reaper 或同一幂等请求完成释放；
7. 创建 30 秒 remote session，确认只返回 remoteConnect ADB 地址和 expires_at，不含 STF Token；
8. 使用错误 owner 请求另一 Reservation，确认 403；
9. 等待 TTL 到期，确认 remoteDisconnect 已执行、旧地址失效、metadata 被清除；
10. 重启 Server 后保留数据库中的未过期入口和到期信息，Reaper 能继续清理；
11. 将某一 serial 从 STF inventory 移除，确认只产生健康事件/隔离，不由 STF 覆盖 Reservation 状态；
12. 保存脱敏 STF 请求日志、数据库状态、审计、API 响应和 remoteDisconnect 证据。

## 阻塞解除条件

上述真实 STF、两台 Emulator、网络故障和 Token 安全验收通过后，将 DF-018 改为 `completed` 并单独提交真实证据。Mock HTTP Server 和 PostgreSQL 自动测试不能替代真实 STF 验收。
