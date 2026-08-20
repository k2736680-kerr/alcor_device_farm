# ADR-0026：iOS Simulator 远控复用 Baguette 原生 Web UI

## 状态

已接受，DF-050 实施中。

## 背景

DF-046 曾按 ADR-0025 使用 Appium/XCUITest/WDA MJPEG，并在本仓库实现 HTML、CSS、JavaScript、坐标转换和动作代理。真实浏览器验收显示，请求虽然返回成功，但动作耗时持续堆积到数秒，画面和操作不能满足人工远控要求。

Mac 上安装的 Appium Device Farm 12.0.1 官方 README 明确说明：12.0.0 起已经删除人工控制和实时串流。上游给出的原因正是 WDA 截图流慢、与自动化 Session 争用同一 WDA，以及由此产生的延迟、掉帧、Session 停滞和设备状态不稳定。因此降级到 Device Farm 11.x 或继续维护 ADR-0025 的自写页面都不是可接受方案。

Baguette 0.1.92 是 Apache-2.0 的原生 iOS Simulator 管理和远控项目，提供现成 Web UI、H.264/MJPEG 画面流、宿主机 HID 点击/滑动/键盘/系统按键、单设备页和设备墙。它不通过 WDA 发送人工输入，不占用自动化 Appium Session。真实 Mac 环境为 Apple Silicon、macOS 26.5.1、Xcode 26.3，符合其运行条件；真实目标 Simulator 的页面、截图和 HID 点击已通过冒烟验证，单次点击约 0.26 秒。

## 决策

1. Android 继续采用“PostgreSQL Reservation/Release + STF Adapter + STF 原生页面”；iOS 采用完全对称的“PostgreSQL Reservation/Release + Baguette Adapter + Baguette 原生页面”。
2. Appium Device Farm、XCUITest、WDA 和 Session Fence 继续只服务设备发现、明确 UDID 的自动化 Session 与异常清理，不再参与人工远控画面或动作。
3. 删除本仓库自写的 iOS 远控 HTML、CSS、JavaScript、MJPEG 转发、截图降级、坐标转换和 Appium 动作白名单；不得通过隐藏路由或 Feature Flag 保留第二套实现。
4. Baguette 固定版本并作为 macOS Host 后台服务，只监听回环地址。它不接管 Pool、Reservation、Lease、Reaper、Device 状态或审计。
5. Device Farm Server 通过 Baguette Adapter 检查目标 UDID 是否为 booted Simulator，并为 active manual Reservation 返回短时入口。浏览器使用 Baguette 原生 UI，不复制或改写其远控组件。
6. 独立 Baguette Gateway 使用单独监听地址和 Origin，反向代理 Mac 回环服务。首次入口使用短时签名票据；随后每个 HTTP/WebSocket 请求都校验票据绑定的操作者、Device、UDID、Reservation 和有效租约。
7. Gateway 只允许当前预约 UDID 的 Simulator 路径，并过滤设备清单；拒绝设备墙、其他 UDID、boot/shutdown、任意插件命令和未登记管理路径。浏览器不得获得 Mac 地址、Baguette 回环地址、Appium、WDA、Fence、Agent Token 或 Session Grant。
8. Console 的开始、查询、心跳和结束 API 保持不变；iOS `transport` 改为 `baguette`。心跳只滑动续约 Reservation，不创建或保活 Appium/WDA Session。
9. Console 结束、标签页关闭、心跳停止或 Reaper 到期继续释放同一 Reservation。Baguette 不保存额外占用真相，因此无需伪造 Appium busy 或清理人工 WDA Session。
10. Baguette 不满足固定版本、健康、目标 UDID 或 Gateway 安全检查时，iOS 远控入口不可用并显示中文错误；不得自动回退 ADR-0025。

## API 与部署边界

- `ios_remote_control` 配置保存固定 Baguette upstream、独立 Gateway 监听地址、浏览器 public URL、票据签名 Secret 和短时 TTL；Secret 不进入浏览器或日志。
- Mac 只开放现有受控 SSH 反向/本地通道；Baguette 继续绑定 `127.0.0.1`，浏览器不能直连 Mac。
- Gateway 与主 Device Farm API 可由同一进程运行，但使用不同监听地址，避免 Baguette 的根路径静态资源污染 Console/API Origin。
- 原生 Web UI 内显示当前 Reservation 的完整 UDID 是目标设备技术标识；它只存在于签名 Gateway 会话中，不能用于访问其他设备或内部 Endpoint。

## 验收与回滚

- 真实浏览器连续验证画面、点击、拖动、文字、Home、应用切换和 WebSocket 重连；不能以 HTTP 200 代替操作结果。
- 第二个操作者、其他 UDID、设备墙、生命周期按钮和票据重放必须被拒绝；Reservation 结束后已打开页面立即失效。
- 扫描浏览器、响应、日志和数据库普通字段，不得出现 Mac/Fence/Appium/WDA 内部地址、Agent Token、Session Grant 或 Gateway Secret。
- Android STF、iOS 自动化 Session Fence、Appium Device Farm inventory 和预约释放必须全部回归。
- 回滚只允许禁用 Baguette Gateway 并返回“iOS 远控暂不可用”；ADR-0025 的自写实现不保留、不恢复。
