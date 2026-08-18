# DF-046 iOS Simulator 受控远程控制验收

## 当前状态

`completed`

## 实现边界

- 远控对象是 Reservation 绑定的单台 iOS Simulator，不是 macOS 桌面；不需要 Remote Management、Screen Sharing、VNC、noVNC 或 websockify。
- 复用 Appium Device Farm 12.0.1、Appium 3.6.0、XCUITest 12.4.0、WebDriverAgent 16.2.0 的明确 UDID Session、MJPEG 和动作能力；项目只实现同源鉴权代理和动作白名单。
- Android 继续复用 STF；本任务不新增 iOS 业务 Runner、Case、Result 或报告能力。

## 自动验证

- `go test ./...`：通过。
- Console `pnpm test`：通过。
- Console `pnpm build`：通过，OpenAPI client 重新生成。
- 隔离 PostgreSQL 17.10 执行全部 migration `up → down → up`、约束检查及 Repository/Scheduler/Reaper/Reconciler/API/Warm Pool 数据库集成测试：通过。
- Reconciler 数据库集成用例覆盖 Reservation 到期与 Session Reaper 并发：最终 Reservation 为 expired、Device 为 ready/healthy，未产生 `IOS_BOUND_SESSION_NOT_BUSY` 健康事件。
- Session Fence 单元测试覆盖进程重启后从绑定 Appium Session capabilities 恢复 MJPEG 端口；Gateway 测试覆盖入口票据过期后已加载会话继续使用。

## 真实 macOS E4 验收

环境使用专用 Apple Silicon Mac、项目固定 Node 22.23.2、Appium Hub `4723` 仅路由、动态 Node `4724` 唯一发现 Simulator；所有地址、完整 UDID、Token、Grant 和凭据均未写入本证据。

| 场景 | 结果 |
|---|---|
| 目标画面 | PNG 1206×2622；MJPEG 返回 multipart 流，2 秒接收约 1.7 MB；画面不含 Mac 桌面、菜单栏或其他窗口 |
| 人工操作 | Home、滑动、点击、文字输入均返回 200；输入 `Alcor` 后截图哈希变化 |
| 目标隔离 | 浏览器只得到同源 URL；任意 Appium/WDA 风格路径返回 404 |
| 双占 | 第二操作者启动远控返回 409 `REMOTE_CONTROL_CONFLICT` |
| 长会话入口 | 首次 `control` 为 200；30 秒入口票据过期后旧 `control` 为 401，但已加载 JS 和动作仍为 200，依赖活动 Console 会话和 Reservation |
| Fence 重启 | 保持 Simulator 与 Appium Session 不变，仅重启 Host Agent/Fence；PNG 恢复为 1206 宽，MJPEG 2 秒约 1.7 MB |
| 主动结束 | 返回 ended，设备恢复 ready/healthy |
| 超时回收竞态 | 临时使用 60 秒远控租约和 5 秒回收宽限，停止心跳后自然回收；最终 ready/healthy，未再次隔离；随后恢复正式 10 分钟滑动窗口和 30 秒宽限 |

脱敏目标画面见 [target-simulator.png](target-simulator.png)。截图只包含 Simulator，并显示远控输入的 `Alcor`。

## 最终状态与后续

- 真实 Host 上 Server 与 Host Agent/Fence 健康，目标 Simulator 为 ready/healthy，开放远控 Reservation 为零。
- DF-046 仅声明 iOS Simulator 受控远控完成；DF-047 继续执行 50 次生命周期、完整故障/回滚、Android 真实回归和发布签收，不宣称真实 iPhone 已接入。
