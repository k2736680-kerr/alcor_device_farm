# ADR-0023：预约使用有限滑动续约窗口

状态：已接受

## 背景

设备池的 `max_lease_seconds=3600` 原先被实现为从 `starts_at` 起计算的预约总寿命上限。即使 Alcor Worker 在 DaFit 执行期间持续调用续约接口，预约运行到一小时后也无法继续延长，Reaper 随后会回收仍在执行自动化任务的设备。当前 DaFit 单次运行已经超过四小时，后续运行时间还会继续增加。

直接取消租约上限会让 Worker 崩溃或网络永久中断后的设备无法自动回收，也不符合既有 Reaper 安全边界。Alcor Worker 已有运行期周期续约，DaFit Harness 只需复用同一预约接口，不能复制 DaFit Runner。

## 决策

1. `device_pools.max_lease_seconds` 表示预约在任意数据库当前时刻最多可以持有的未来安全窗口，不再表示从 `starts_at` 起计算的总运行寿命。
2. active 且未过期的预约可以使用不同幂等键持续续约。新的 `expires_at` 为“当前到期时间加本次秒数”和“数据库当前时间加设备池最大窗口”两者中的较早值。
3. 单次 `additional_seconds` 不得超过设备池最大窗口；续约不能复活已过期或非 active 预约。
4. Alcor Worker 或 Harness 仅在真实执行存活期间周期续约。正常成功、失败、取消或超时后立即停止续约并幂等 release。
5. Worker/Harness 崩溃、进程被终止或网络持续中断时不会再产生续约；Reaper 在有限窗口和 grace period 后继续负责最终回收。
6. Console 远控心跳使用相同滑动边界，不再受首次 `starts_at` 的一小时总寿命限制。
7. 稳定错误码继续使用英文标识；面向用户的 API、Console 和 Harness 提示使用中文。

## 后果

- 单次自动化任务可以运行四小时以上，理论总运行时长不设固定上限，只要执行方持续健康续约。
- 任意时刻的失联占用仍被 `max_lease_seconds + grace period` 限定，不会形成永久预约。
- 不新增 Reservation 状态、表或第二套运行模型；设备农场只管理租约，DaFit 和新版 Alcor 继续管理各自执行生命周期。
- ADR-0013 中“达到从开始时间计算的最大租期后必须重新连接”的限制由本 ADR 替代；远控仍必须有心跳且失联后可回收。
