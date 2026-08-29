# DF-056 正式数据清理与长期设备非破坏自愈验收

## 结论

通过。Android 与 iOS 长期虚拟设备不再因健康隔离被自动删除、重建或补建替代设备；系统优先恢复原 Device，持续 Android 故障且空闲时最多排队一次非破坏 restart。正式 PostgreSQL 测试历史已在可恢复备份后清空，当前三台设备身份、Provider 资源和 Pool membership 保持不变。

## 根因与修复

- 原 Android 在 Provider、ADB、Appium 和 STF 仍正常时，因连续 `agent_reported_unhealthy` 被永久隔离；Warm Pool 又把隔离设备排除在登记容量外，最终可能补建并缩容删除原机。
- ADR-0028/DF-053 曾明确允许 iOS 故障 Simulator 自动删除补建，与 DF-038 的长期设备、保留 App/账号/缓存/文件目标冲突。
- ADR-0029 取代上述策略：所有非 `deleted` 设备占用登记容量；系统隔离继续重探原 ID；健康恢复回到原预约状态或 `ready/healthy`；持续 Android 故障最多执行一次原机 restart；失败后保留隔离，不自动执行 delete/rebuild/reimage/create replacement。
- Server 或 Agent 短暂重启造成 Host heartbeat 变旧时，以 Host 最近心跳作为恢复宽限起点；宽限内不改设备健康、不累计失败、不写 `host_unavailable` 事件。健康 Agent heartbeat 会清除非隔离设备的旧失败计数。

## 备份与数据清理

清理前停止 Server 写入并生成 PostgreSQL custom-format 备份：

`D:\AutoTestTools\Data\Backups\alcor_device_farm\alcor-device-farm-df056-pre-clean-20260829.dump`

SHA-256：`1db450c42d8b2de2c889de80a7cda0a2090c44329ac54f7c6b4d4ef44e339f4c`

清理前主要计数：Device 40、Reservation 209、Session 196、Health Event 4379、Audit Event 1320、Host Command 226、Idempotency 223、Provisioning Job 7、Image Preparation 10、Pool Membership 37。

最终正式库计数：Device 3；Reservation、Session、Health Event、Audit Event、Host Command、Idempotency、Provisioning Job、Image Preparation、Console Session 均为 0。两个 Host、两个 Pool、当前 Image 配置和三条启用 Pool membership 保留。

保留设备：

| 平台 | Device ID | Provider ref | 最终状态 |
|---|---|---|---|
| Android | `ef26de28-b0d8-4afa-894d-7a8b1d3716ad` | `emulator-ef26de28-b0d8-4afa-894d-7a8b1d3716ad` | `ready/healthy`，失败次数 0 |
| iOS | `32155337-3478-4b5e-9ea7-c856e6f3c749` | `45779828-41FC-4DEC-A38F-8877F9321426` | `ready/healthy`，失败次数 0 |
| iOS | `eb3aad90-8f06-45ea-a5ea-2465185a5b96` | `A66EF5FD-C95A-49F3-978D-5E32ADC710BB` | `ready/healthy`，失败次数 0 |

## 自动化与真实设备验收

- `go test -p 1 ./...`：通过，使用真实 PostgreSQL 集成测试库串行执行数据库包。
- `go vet ./...`：通过。
- Console：9 个测试文件、50 项测试通过；生产构建通过。
- 新增回归覆盖：系统隔离原 ID 自动恢复、人工隔离不自动解除、持续故障只排一次 restart、无 delete/rebuild/create、iOS 故障不删除不补建、健康 heartbeat 清零旧失败、Host 恢复宽限内零状态变更和零健康事件。
- 生产 Server 镜像部署并通过 `/readyz`；重启后等待完整 heartbeat/reconcile 周期，Health Event 仍为 0，三台失败次数均为 0。
- Android 使用正式 Reservation 建立 UiAutomator2 Session，`/source` 成功返回 22,419 字节，Session 删除和 Reservation 释放成功。
- 两台 iOS 同时预约到不同 UDID，分别通过一次性 Session Grant、Session Fence 和 XCUITest 建立 Session；两次 `/source` 均成功返回 41,127 字节，Fence 关闭和 Reservation 释放成功。
- Server 容器替换后重新绑定既有 iOS Fence/Baguette SSH 隧道到新容器网络命名空间；Fence 和 Baguette inventory 最终可达，未修改或重建 Simulator。

## 登录数据边界

Console 用户配置保存在独立只读 Secret volume，只保存 Argon2id 哈希，不属于 PostgreSQL 测试数据清理范围。本次清库和部署未覆盖该文件。旧密码无法从哈希恢复；若当前输入与哈希不匹配，需由管理员提供新密码后单独重置，明文不得进入仓库、证据、环境文件或日志。

所有 Token、Cookie、Session Grant、数据库口令、SSH 私钥和 Console 密码均未写入本证据。
