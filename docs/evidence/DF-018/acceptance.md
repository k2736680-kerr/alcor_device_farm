# DF-018 实施与验收证据

## 当前结论

DF-018 已于 2026-08-06 在 Linux KVM、Docker Android 16 Emulator、STF 3.7.9、RethinkDB 2.4.2 和真实 Appium Endpoint 环境完成验收，状态改为 `completed`。

本次真实验收按 ADR-0008 使用单台 Emulator。多 Scheduler 并发、双占约束和多设备端口隔离继续由 PostgreSQL/Adapter 自动测试覆盖；当前真实环境不再要求同时运行两台 Emulator。

## 实施范围

- Adapter 固定使用 STF 3.7.9 官方 inventory、claim、release、remoteConnect API；
- 支持 Base Path、5 秒超时、默认 3 次尝试、递增短退避和 HTTP/网络错误分类；
- release 404 作为幂等成功，响应正文不进入错误，Token 不进入错误、JSON 或配置日志；
- remoteConnect URL 拒绝凭证、query、fragment 和控制字符；
- Scheduler 先把 Device 置为 reserved，事务外 claim 成功后才激活 Reservation；
- claim 可重试失败保持 pending 并进行 5 秒调度退避，不可重试失败进入 failed；两种失败都按 reserved → recycling → ready 补偿 Device；
- claim 成功后若 Session ID 或数据库激活失败，调用 STF release 并补偿数据库；
- 主动释放和过期回收先执行 STF release，失败时 Reservation 保持 active 并写 `stf_release_failed` 审计；
- remote session 必须匹配 Reservation owner，TTL 为 30～3600 秒，同一幂等请求不重复 remoteConnect；
- remote session 复用 Device Session metadata，过期后 Reaper 先 remoteDisconnect 再清除入口；
- Reconciler 按 serial 核对 STF 可见性，只写健康事件，不覆盖 PostgreSQL 生命周期真相。

## 真实环境

| 资源 | 验收值 |
|---|---|
| Server/Console | `10.0.30.171` |
| Pool | `8b97a9e7-ab6c-41ff-bc1e-bd922b829ab3` |
| Host | `f40f1a34-7a5d-4ca6-8c5b-e8e94d6346ac` |
| Image | `b6492bc3-4e37-457e-a5f8-f4a30f46959f` |
| Device | `67434725-32ff-4870-8c55-ad46fd5d9486` |
| ADB serial | `10.0.30.171:32785` |
| Appium Endpoint | `http://10.0.30.171:32784` |
| Server image | `alcor-device-farm:df017-20260806-warmfix6` |

最终 STF inventory 中当前 serial 为 `present=true / ready=true / using=false`。历史 Emulator serial 仍可作为 `present=false` 记录存在于 STF，但没有对应运行容器，也不参与调度。

## 正常 claim、远控和 release

正常链路 Reservation 为 `6f84e1cb-9ca8-46c1-a574-524dccb7cfcc`：

1. Reservation 在 STF claim 完成前保持 pending，claim 成功后才进入 active；
2. active 后 STF 对应设备为 `using=true`；
3. remoteConnect 返回 `10.0.30.171:7405`，相同 Idempotency-Key 返回同一 remote session ID，没有重复创建入口；
4. 使用错误 owner 请求同一 Reservation 返回 403；
5. Server 重启后数据库中的 remote session ID、地址和到期时间保持不变；
6. TTL 到期后 connection metadata 自动清除，`7405` 端口失效；
7. 注入 STF release 故障时，三次 Adapter 尝试后 API 返回稳定错误，Reservation 保持 active；
8. 审计记录 `stf_release_failed`，request ID 为 `req_13064e3b5078839ee044a664`；
9. 恢复 STF 后使用同一幂等 release 完成释放，审计记录 `release_device_reservation`，最终状态为 released。

这证明 release 故障不会提前忘记 PostgreSQL 中的占用真相，恢复后可以继续完成同一释放操作。

## claim 故障分类和补偿

使用只实现 STF 官方 inventory/claim 形状的临时故障代理，在真实 Server 和真实 PostgreSQL 上分别注入 400、503、超时和连接中断。代理不替代正常链路验收，只用于精确观察 Adapter 请求次数和 Scheduler 退避。

| 故障 | Reservation | 观察结果 |
|---|---|---|
| HTTP 400 | `92822cdf-e770-4843-9508-8ef623719a81` | claim 只请求 1 次；Reservation 进入 failed，`device_id` 清空，`failure_code=STF_CLAIM_FAILED`；Device 回到 ready/healthy |
| HTTP 503 | `e4f79050-e5ab-481c-bf1d-1e8a460f4b22` | 每轮恰好 3 次 Adapter 请求；轮次间约 5 秒退避；Reservation 保持 pending，Device 每轮补偿回 ready/healthy；验收后通过正式 Console release API 取消，最终 `RESERVATION_CANCELED` |
| 5 秒超时 | `f126c197-2be2-42cd-9464-2630e9ddb62e` | `11:28:03Z` 计数为 3，`11:28:27Z` 计数为 6；两轮之间没有请求风暴；第二次检查时 Reservation 为 pending、`device_id` 已清空、`failure_code=STF_CLAIM_FAILED`，Device 为 ready/healthy |
| 连接中断 | `f01ee413-cf00-4d11-9195-5ce6d9f5f741` | `11:29:57Z` 计数为 6，`11:30:16Z` 计数为 18；每轮仍为 3 次短重试并带约 5 秒调度退避；Reservation 保持 pending，Device 为 ready/healthy |

timeout/drop 验收结束后将代理切换为 400，使下一轮按不可重试错误自然进入 failed；没有直接修改数据库。最终两条 Reservation 都为 failed、`device_id` 为空、`failure_code=STF_CLAIM_FAILED`，Device 为 ready/healthy。

## STF 重启和 PostgreSQL 真相

- 故障代理切换期间真实 STF 容器被停止，但 Server 不把 STF 状态写成 Reservation 真相；
- 恢复时先停止代理，再启动真实 STF 和 Server，并重新确认当前 Emulator 的 ADB 连接；
- Server、STF、Console Proxy 和 PostgreSQL 最终全部为 healthy；
- 数据库最终没有 pending 或 active Reservation；
- 当前只运行 1 个 Emulator 容器，故障代理进程数为 0；
- 当前 Device 为 `ready/healthy`，连续失败数为 0。

## 敏感数据检查

对以下六类语料执行配置中实际 Secret 的精确匹配扫描：Server 日志、STF 日志、Console Proxy 日志、PostgreSQL data-only 导出、Device Farm API 响应和 STF inventory 响应。

```text
SERVICE_TOKEN_MATCH_CORPORA=0
AGENT_TOKEN_MATCH_CORPORA=0
STF_TOKEN_MATCH_CORPORA=0
CONSOLE_PASSWORD_MATCH_CORPORA=0
COOKIE_OR_BEARER_HEADER_PATTERN=0
```

浏览器和 API 响应只获得与当前 Reservation 绑定的短时 remoteConnect 地址，不包含 STF 管理 Token。

## 自动回归

执行：

```powershell
& 'E:\AutoTestTools\Tools\go1.26.5\go\bin\go.exe' test ./internal/... -p=1 -count=1
```

结果：`internal/adapters/stf`、`internal/scheduler`、`internal/reservation`、`internal/reaper`、`internal/reconcile`、`internal/api`、`internal/contract` 以及其余 `internal/...` 包全部通过。

## 验收结论

DF-018 的真实 claim、release、remoteConnect、owner 隔离、幂等、Server 重启恢复、TTL 清理、400/503/timeout/drop 错误分类、补偿、退避和敏感数据检查全部通过。Mock HTTP Server 只保留为自动回归，本结论以 Linux KVM 和真实 STF 正常链路为准。
