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
- 跨域窗口只在稳定观察窗口后才启用关闭检测，避免 STF 跳转期间误释放。

## 验证

- `go test ./...`：通过。
- `pnpm test`：7 个测试文件、25 项测试通过。
- `pnpm build`：OpenAPI 生成、TypeScript 编译和 Vite 生产构建通过。
- 回归覆盖：请求超时、服务端 deadline、永久 connecting 自动取消、启动响应丢失补偿、404 幂等挂断、跨路由心跳、刷新恢复、跨域窗口误判与真实关闭释放。
