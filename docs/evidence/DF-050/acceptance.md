# DF-050 使用 Baguette 替换并清理自写 iOS 远控验收证据

验收日期：2026-08-20

## 结论

通过。Android 人工远控继续挂载 STF 原生 Web；iOS Simulator 人工远控已替换为 Baguette 0.1.92 原生 Web。设备预约、滑动续租、释放和超时回收仍由 PostgreSQL 设备农场负责。Appium Device Farm、XCUITest、WDA 和 Session Fence 只保留自动化 Session 与 inventory 职责，不再创建人工 Appium Session。

用户已在 `http://127.0.0.1:18888/` 的同一设备农场后台实际使用 iOS 原生远控，并确认操作体验符合预期。

## 真实部署

- Mac Host：Baguette 0.1.92 由 `com.alcor.device-farm.baguette` LaunchAgent 托管，状态为 `running`，进程从未异常退出；`127.0.0.1:8421/simulators.json` 返回 HTTP 200。
- Device Farm Server：运行状态为 `running/healthy`，主 API `readyz` 返回 HTTP 200。
- 安全通道：Server 到 Mac 的 Baguette/Fence SSH 隧道状态为 `running`，Server 网络空间访问 Baguette inventory 成功。
- 独立 Gateway：无预约 Cookie 访问返回 HTTP 401；Mac 回环地址、Fence、Appium、WDA、Session Grant、Token 和签名 Secret 均未返回浏览器。

## Baguette 原生远控验收

- 为一台 `ready/healthy` iOS Simulator 创建人工预约后，开始接口返回 `connected`，transport 为 `baguette`。
- 签名入口成功打开 Baguette 原生 Simulator 页面；inventory 只返回预约目标，截图和 WebSocket 真实画面帧成功。
- 目标画面、点击、滑动、文字输入、Home、应用切换和重新打开由 Baguette 原生 Web/Host HID 提供，不经过项目自写画面或坐标代理。
- `/farm` 返回 HTTP 403；其他 UDID、boot/shutdown、插件和 bakery 路由被 Gateway 拒绝。
- 明确挂断后预约状态为 `ended`，旧 Cookie 立即返回 HTTP 401，设备恢复 `ready/healthy`。
- 停止浏览器心跳后，短租约自然到期并由 Reaper 回收；旧 WebSocket/Cookie 无法继续访问。持续操作时每次心跳滑动续租，不存在固定一小时强制锁死上限。

## 自动化与 Android 回归

- iOS 自动化最小真实回归：创建 `test_run` Reservation、签发一次性 Session Grant、通过 Fence 建立指定 UDID 的 XCUITest Session、读取 `/source`、删除 Session 并释放预约全部成功；页面结构长度 42550，结束后设备为 `ready/healthy`，无 busy 残留。
- Android 真实回归：人工远控返回 transport `stf`，建立和释放均成功，Android 链路未切换到 Baguette。
- Appium Device Farm 12.0.1 inventory、XCUITest/WDA 自动化职责继续保留，人工远控不调用这些路径。

## 旧实现零残留

以下旧实现已从可执行代码、路由、配置和测试中删除，并对 Device Farm 与 Alcor 可执行目录执行全文零命中扫描：

- 自写 iOS HTML、CSS、JavaScript 页面；
- WDA MJPEG、截图、坐标、文字和动作代理；
- `mjpegServerPort` 注入与人工 Appium Session goroutine；
- `IssueManual`、`RemoteBinding`、`prepareMJPEGCapabilities`、`remoteSessions`；
- `/internal/v1/ios-remote`、`/console/remote/ios`；
- Alcor `ProxyDeviceFarmIOSRemote` 及旧同源代理路由；
- 旧 VNC 整机级 iOS 人工预约互斥锁；
- 旧实现专属测试与可恢复旧方案的配置键。

历史 ADR 中保留“旧方案已否决/已删除”的事实说明；已应用 migration 的文件名作为数据库历史不可改写，但不包含可执行旧远控能力。

## 最终门禁

- `git diff --check`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- Console `pnpm test`：9 个测试文件、44 项测试全部通过。
- Console `pnpm build`：OpenAPI 客户端生成、TypeScript 检查和生产构建通过。
- Alcor `go test ./internal/platform ./internal/router`：通过。
- OpenAPI 冻结哈希按 LF 规范化算法校验通过；远控 transport 只允许 `stf` 和 `baguette`。
- 用户提示和本次新增 OpenAPI 说明均为中文；平台通用状态不再误写成仅等待 STF。
