# ADR-0022：iOS Session 使用一次性 Grant 和宿主机 Fence

## 状态

已接受。

## 背景

ADR-0021 已确定 PostgreSQL Reservation 是唯一设备占用真相，Appium Device Farm 只能路由我方已经选定的明确 UDID。DF-041 又把每台 macOS Host 的 Appium Node 固定为 loopback，因此可信 Executor 不能直接连接 Node，也不能依赖插件的 tags、Host filter 或自由选机。

现有 `device_sessions` 已在 Reservation 激活时保存 Device、Host、Endpoint 和 UDID 快照，并承载 Android 第一版的技术占用历史。为 iOS 再建一套业务 Session 表会产生重复真相；把一次性明文 Grant、Apple Secret 或服务 Token 放入连接快照也不符合安全边界。

## 决策

1. 新增服务身份专用的 `POST /api/v1/device-reservations/{id}/session-grants`。它只接受 active iOS Reservation，校验调用方提交的 owner 与 Reservation 一致，返回 15～120 秒有效的一次性随机 Grant、Host Fence Endpoint 和脱敏绑定摘要。Console Principal 和浏览器不能调用。
2. Grant 使用至少 256 bit CSPRNG 随机值；API 只返回一次明文，PostgreSQL 只在既有 `device_sessions` 保存 SHA-256、过期时间和消费时间。重新签发会原子失效尚未消费的旧 Grant；重放已消费或过期 Grant必须拒绝。
3. macOS `device-host-agent` 同进程启动 Session Fence。Fence 使用独立监听地址，对 Executor 只开放 `POST /session` 和已绑定 Appium Session ID 下的 WebDriver 路径；Dashboard、插件 API、任意转发地址和跨 Session 路径全部拒绝。Appium Node 继续只监听 loopback。
4. Fence 在创建 Session 前，以现有 Agent Token 调用 `/internal/v1/ios-session-fence/grants/consumptions`。Server 在一个事务内锁定 Grant、Reservation、Device Session 和 Device，校验 Host、Device、Endpoint、UDID、租约和插件 busy 快照，并原子标记 Grant 已消费。
5. Session 创建请求只检查和限制路由能力，不解释业务能力或执行 WebDriver 命令。Appium Device Farm 12.0.1 的 `df:udids` 是逗号分隔字符串，因此只接受不含逗号且等于连接快照 UDID 的单值字符串；`appium:udid` 必须与它相同。`df:tags`、`tags`、`filterByHost`、`df:filterByHost`、多 UDID、JSONWP `desiredCapabilities` 和多个 `firstMatch` 候选一律拒绝。其余 W3C capabilities 原样转发。
6. Appium 成功响应中的 Session ID 由 Fence 回报 Server，并写入同一 `device_sessions` 的技术绑定字段。后续请求每次校验同一 Grant、active Reservation、Host 和精确 Session ID；Fence 从不把 Grant 或 Agent Token转发给 Appium。
7. Executor 删除 Session 后清除技术绑定，但 Reservation 仍由调用方显式释放。显式释放和 Reaper 发现仍有 Appium Session 时，必须先通过 Host Fence 的 Agent 认证清理入口删除 Session，再关闭 Device Session 和 Reservation；清理失败时 Reservation 不伪装为 released，Device 进入隔离并留下审计。
8. 现有 Android 语义保持不变：Scheduler 激活 Reservation 时即将 Device Session 置为 active、Device 置为 busy。iOS 沿用这层独占，Appium Session ID 是 active Device Session 内部的子绑定；不为等待 XCUITest 创建引入第二套 Reservation 状态或复制 Session 表。
9. Reconciler 比对 active Reservation、技术 Session 绑定和 Agent 上报的 `providerBusy`。`providerBusy` 是技术占用事实，不是 Router 健康失败：已绑定 Session 的设备保持 `busy/healthy`；无 Reservation 的 busy、绑定 Session 的非 busy、无绑定 Session 的异常 busy 或 Host/UDID 漂移才会阻止新 Grant 并把 Device 标记 degraded/quarantined。Session 正常结束后的 30 秒内允许 Agent 心跳刷新插件 busy，超过宽限仍 busy 才按漂移隔离；插件 busy 不能自行创建、续租或释放 Reservation。
10. Host 心跳只上报 Fence 的受控 Endpoint 和健康摘要，不上报 Grant、Agent Token、Apple Secret 或 WDA 内部地址。Fence Endpoint 不进入 Console 响应；只有 Session Grant 响应可返回给可信服务调用方。
11. 固定版 XCUITest doctor 的必需检查必须全部通过。doctor 探测可选 Remote XPC Registry 时可能命中已结束 Session 留存的 WDA 端口，因此 Agent 将该命令限制为 15 秒；仅当 HOME、Xcode 和 Xcode Command Line Tools 三项必需结果已明确通过时，允许可选探测超时而继续心跳，缺少任一必需结果仍按 Host 不可用处理。

## API 与持久化

- 北向：`POST /api/v1/device-reservations/{id}/session-grants`；
- Agent 内部：Grant consume、Session bind/authorize/close/fail；
- Host Fence：`POST /session`、精确 `/session/{id}/...` 和 Agent 认证的内部清理路径；
- `device_sessions` 原位增加 Grant 哈希/时间和 Appium Session ID/开始结束时间；不新增 Alcor Run、Case、Result、Artifact 或业务 Session 表。

## 后果

- Appium Device Farm 只能收到我方已经预约的单一 UDID，无法再次自由选机；
- Appium 保持 Host loopback，浏览器和普通 API 都拿不到 Node、Dashboard 或 Grant；
- Agent/Server/Appium 任一故障都能从 PostgreSQL 技术绑定和 Reaper 恢复或隔离；
- iOS Executor 仍由 Alcor 或独立执行器实现，本仓库只提供受控 WebDriver 透明通道。
