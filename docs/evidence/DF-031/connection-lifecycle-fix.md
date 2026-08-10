# DF-031 连接检查与关闭收敛修复

日期：2026-08-10

## 根因

- Console 请求封装没有超时，远控状态为 `connecting` 时每秒轮询但没有总截止时间，因此网络半开或 STF claim 未完成会一直显示加载状态。
- 远控状态原来属于 Device 页面组件；离开页面后心跳随组件卸载而停止，短租约随后过期并释放 STF claim。
- 浏览器跨域跳转到 STF 后，部分环境会暂时把仍存活的窗口句柄表现为 `closed`；旧逻辑会立即调用结束接口，导致刚连接就进入 release/rebuild。
- 结束接口虽然服务层已按“没有当前预约”等同已结束处理，但 Console 没有把设备/远控 404 当作幂等成功，也没有统一刷新设备列表、详情和预约查询。

后端 `Start` 只创建精确 Device 的短 Reservation；Scheduler 异步执行 STF claim，不在 HTTP 请求中等待 `adb connect`、Emulator 启动或 Host Agent。STF Adapter 自身已有有界 HTTP 超时与重试，因此未发现 adb/emulator/agent 阻塞该请求的路径。

ID 映射保持一致：Reservation 的精确目标和最终 `device_id` 都使用 Device 主键，Pool 通过 membership 关联 Device，STF 使用 Device `serial`，Appium/ADB 使用连接端点字段；没有把 pool ID、reservation ID、serial 或 endpoint 当作 `deviceId` 的实现。

## 修复

- 所有 Console API 请求增加 15 秒客户端超时和稳定的 `REQUEST_TIMEOUT` 错误。
- 远控四个 Console API 增加 10 秒服务端 context deadline，超时返回可重试的 `REMOTE_CONTROL_TIMEOUT` / HTTP 504。
- `connecting` 增加 30 秒总截止时间；超时后自动幂等结束、关闭窗口并刷新设备与预约状态。
- 启动请求失败或响应丢失时执行补偿结束，防止服务端已提交 Reservation 而浏览器未收到响应。
- 取消/挂断遇到设备或远控 404 时按“已经结束”收敛，不显示异常错误。
- 远控会话提升到 Console 全局 Provider，跨路由继续心跳，刷新后可从 `sessionStorage` 恢复服务端真相。
- 关闭或切换 STF 标签页不再触发释放；只有管理员明确点击“取消连接/挂断”才立即结束，Console 整体失联仍由短租约兜底。
- 远控心跳只负责续租，不再把 STF inventory 单次 `using=false` 或短暂掉线当成管理员挂断。
- 打开 `about:blank` 占位页后，即使 Console 成为后台标签页，也继续轮询远控状态；STF URL 就绪后自动替换占位页，不再依赖管理员切回 Console 触发窗口焦点查询。

## 验证

- `go test ./...`：通过。
- `pnpm test`：7 个测试文件、26 项测试通过。
- `pnpm build`：OpenAPI 生成、TypeScript 编译和 Vite 生产构建通过。
- 回归覆盖：请求超时、服务端 deadline、永久 connecting 自动取消、启动响应丢失补偿、404 幂等挂断、后台标签页持续查询、跨路由心跳、刷新恢复、关闭 STF 标签页保持会话、显式挂断释放。

## 自动断开现场根因与规则收敛

- Reservation `c522d79c-a797-477d-bf53-d3888e7e9e53` 在 `2026-08-10 05:54:05Z`、`3832e9f8-b9e9-4266-aa44-45d8ce658326` 在 `05:59:07Z` 都由心跳请求结束；同一时间没有 Console `DELETE /remote-control` 请求。
- 两条设备域审计均记录 `release_device_reservation / STF 已结束远控`，证明旧后端把 STF inventory 的 `using=false` 直接等同于管理员挂断。`05:59:07Z` 的状态变化与本地诊断执行 ADB root/unroot 的连接抖动重合，该诊断操作不应在活动会话中执行。
- 收敛后的占用真相以 Device Farm Reservation 和管理员显式挂断为准：心跳续租、标签页可关闭重开；只有显式挂断立即释放，Console 失联才由租约到期回收。
- 本地 `go test ./...`、`go vet ./...`、Console 7 个测试文件/26 项测试及 `pnpm build` 全部通过；其中新增回归证明关闭 STF 标签页不会发送 DELETE，既有回归继续证明显式挂断会释放并关闭标签页。
- 正式 Server 已切换为 `alcor-device-farm:df031-20260810-explicit-hangup`（镜像 ID `sha256:69cb379c3c01a442924faf62af785ce1c47e095014f014606cac1862c126ac04`），正式 HTTP/HTTPS 健康与就绪检查通过，Console 资源为 `assets/index-tIiz2RT-.js`。
- 部署前版本保留在停止容器 `alcor-device-farm-server-df017-rollback-before-explicit-hangup-20260810`，本次部署未重启 STF、Emulator 或 ADB。

## 后台轮询现场回归

- 现场 Reservation `de0acab1-004c-4721-bc51-79b7e7c82209` 于 `04:24:51` 激活；首次 GET 后直到管理员于 `04:25:24` 切回 Console 才再次 GET，证明设备已就绪但前端后台轮询暂停，空等约 33 秒。
- 正式容器已部署 `alcor-device-farm:df031-20260810-fullfix-bg-poll`，镜像 ID `sha256:c97fa4faf0bd28f7424b2768c972615b371bb85de94f59be98840d696b0b8ed8`。
- 正式 HTTPS `/healthz`、`/readyz` 均返回 200，Console 资源为 `assets/index-hcSkV5ht.js`；部署前生命周期修复镜像保留在停止容器 `alcor-device-farm-server-df017-rollback-lifecycle-only-20260810`。

本轮同时确认 Android 16 冷启动存在独立的系统级 ANR：SystemUI、Google Play 服务、Phone 与输入法在同一启动窗口发生超时，Launcher 首次绘制约 12.6 秒。该问题属于 Emulator Image 启动稳定门禁，不以增加 Console 等待时间冒充修复，后续需单独修改并真实验收镜像。
