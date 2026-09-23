# DF-043 iOS Simulator 固定库存接入验收

## 当前状态

`completed`

验收日期：2026-08-17

验收环境：授权 macOS E4 宿主机、Appium 3.6.0、Appium Device Farm 12.0.1、XCUITest 12.4.0、两台 iOS 26.3 Simulator。两台设备均来自固定 allowlist，UDID 不同；证据仅保留脱敏身份，不记录完整 UDID。

## 验收结果

- `simctl list` 与 Appium Device Farm inventory 均发现两台 allowlist Simulator，Runtime、机型和健康状态一致；非 allowlist 设备不会进入 Server 库存。
- 两台 Simulator 加入同一 iOS Pool 后，两条并发 Reservation 分配到不同设备，并发建立两个 XCUITest Session；每个 Session 均完成一次 `/source` 最小操作，并通过 Session Fence 删除和释放。
- 连续完成 50/50 个有效 XCUITest Session：50 次 `/source`、50 次 Fence 删除、50 次对应释放，失败数为 0；结束后无活动 Reservation、无活动 Device Session、无未结束 Appium 技术绑定、无 provider busy、无隔离设备。
- 受控生命周期命令只允许 `xcrun simctl boot`、`shutdown`、`bootstatus` 和只读 `list`；北向 stop/start Host Command 在真实 Mac 上通过，未知设备、真机和任意命令均被拒绝。
- WDA 冷启动实测最长约 37 秒。Grant 消费后采用 300 秒绑定宽限，与 270 秒 Fence 命令超时及 30 秒收敛余量对齐，冷启动期间设备未被漂移检查误隔离。
- 上一个 Session 删除后的迟到心跳在 30 秒清理收敛窗口内返回可重试的 `IOS_PROVIDER_BUSY_CONVERGING`；设备保持 `busy/healthy`，不会误隔离，真正持续 busy 仍按漂移故障处理。
- shutdown、boot、bootstatus 正常；boot timeout 返回稳定中文故障；Appium inventory 重复 UDID 返回中文错误并拒绝接入。
- 停止 Agent 后 Host 收敛为 offline；重新启动后恢复为 online，`host_readiness.ready=true`，两台设备恢复为 `ready/healthy`。
- 最终数据库脱敏核对：pending/active Reservation 为 0，active Device Session 为 0，未结束 Appium 绑定为 0，ready/healthy iOS Device 为 2，provider busy 为 0，隔离设备为 0，在线且 readiness 通过的 Host 为 1。

## 自动化门禁

- `go test ./...`：通过。
- `go vet ./...`：通过。
- PostgreSQL migration、repository、scheduler、reaper、reconcile、Host Command、API 和 Appium Adapter 集成测试：通过。
- `pnpm generate:check`：通过，OpenAPI 2.3.0 生成客户端与冻结契约一致。
- `pnpm test`：7 个测试文件、32 个测试全部通过。
- `pnpm build`：通过。

## 安全与边界

- Server 只下发固定 allowlist Host Command，不直接连接 Mac，也不接收任意 shell 命令。
- 首期未自动下载 Runtime，未自动克隆或删除 Simulator。
- 未保存 Token、Grant、完整 UDID、Session ID、Apple Secret、密码或内部 Appium Endpoint。
