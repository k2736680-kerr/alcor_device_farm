# ADR-0025：iOS Simulator 复用 Appium、XCUITest、WDA 与 MJPEG 受控远控

## 状态

已接受，DF-046 已完成。

## 背景

需求方要求在 Device Farm Console 中直接看屏和操作指定 iOS Simulator，体验应接近 Android STF，但不能暴露整台 macOS Host。Appium Device Farm 12.0.1 的 Dashboard 没有可直接复用的 Simulator 人工远控页面，源码中的 `/tap` 也未启用；它不能被描述为现成的跨平台 STF 替代。

真实 macOS 验证确认：按预约 UDID 建立 Appium/XCUITest Session 后，WebDriverAgent 提供目标 Simulator 的截图、触控命令和 MJPEG 画面。macOS Screen Sharing/VNC 即使能在浏览器中连接，展示和控制的仍是整个宿主机桌面，无法可靠隔离单台 Simulator，还会暴露其他窗口，因此不能作为设备农场用户入口。

## 决策

1. iOS 远控仅支持专用 macOS Host 上的受管 CoreSimulator；首期不支持真实 iPhone、任意 macOS 桌面或 Appium Dashboard。
2. 远控继续先创建指向明确 Device 的 `manual` Reservation。Reservation active 后，Server 通过现有 Session Grant 和 Host Session Fence 创建一条同时固定 `df:udids`、`appium:udid` 的人工 Appium/XCUITest Session；Appium Device Farm 只做宿主机发现、技术 busy 和路由，PostgreSQL 仍是唯一占用真相。
3. 画面复用 WDA MJPEG；截图降级、点击、滑动、文本输入和 Home 复用 Appium/XCUITest/WDA。项目只实现同源鉴权代理、坐标转换和动作白名单，不实现视频编码、WebDriver、XCTest、WDA、页面动作库或业务 Runner。
4. Browser 只获得同源、一次性、短时入口。Server 每次打开画面或提交动作都校验 Console 管理员、入口签名、操作者、Device、Reservation、租约和绑定的 Appium Session；Browser 永远拿不到 Host/Fence/Appium/WDA/MJPEG 地址、Session Grant、Agent Token 或任意 WebDriver 命令能力。
5. Host Session Fence 只接受 Server 使用 Agent Token 调用固定的画面、健康和动作接口；它自行分配回环 MJPEG 端口，并只代理已绑定 Session。接口不接受 URL、端口、UDID、shell、bundle ID、脚本名或原始 WebDriver 路径。
6. Console 显式结束、心跳丢失、Reservation 过期、Host/Session 故障或浏览器断连时，复用现有 iOS Session cleanup、Reservation release 和 Reaper 关闭 Appium/WDA Session 并释放设备。远控心跳同时执行轻量 Session 健康命令，避免活动中的人工 Session 被 Appium 默认空闲超时误杀。
7. Android 保持 STF 原生 Web 远控，不迁移 STF 协议，也不让 iOS 实现影响 Android 的 claim、release 和页面。
8. 同源签名 URL 只在首次加载 `control` 页面时校验短时有效期；页面加载后所有资源和动作仍逐次校验 Console 会话、操作者和 active Reservation，因此长时间活动会话不会被入口票据强制中断。
9. Session Fence 重启后只允许从已绑定 Appium Session capabilities 恢复 MJPEG 端口；不接受 Browser、Server 或调用参数指定端口。Reservation 已到期或接近回收窗口时，Reconciler 让 Reaper/cleanup 优先收敛，不把插件先释放 busy 的短暂窗口误判为漂移隔离。

## API 与部署边界

- 现有远控开始、查询、心跳、结束 API 维持资源和角色语义；OpenAPI 使用 `transport=stf|appium`，同源入口不包含内部连接信息。
- iOS Host 不再需要 Remote Management、Screen Sharing、VNC、noVNC、websockify 或 `remote_open/remote_close/remote_health` Host Command。
- Session Fence 的 Appium 与 MJPEG 上游只使用回环地址；非回环 Fence Endpoint 必须使用 HTTPS。
- 仅暴露 `stream`、`frame`、`health` 和 `actions` 白名单；动作只允许规范化坐标的 `tap`、`swipe`、长度受限的 `text` 和 `home`。

## 验收与回滚

- 管理员只能看到和操作当前预约的目标 Simulator；画面不得包含 macOS 菜单栏、桌面、其他 Simulator 或宿主机窗口。
- 真实验证截图/MJPEG、点击、滑动、文本和 Home；同时验证第二个操作者、自动化 Session、drain、隔离、过期和断连不能双占或遗留 Session。
- 浏览器、日志、审计、数据库和生产构建不含 Host/Fence/Appium/WDA/MJPEG 地址、UDID、Session Grant 或 Agent Token；任意原始 Appium/WDA 请求被拒绝。
- 禁用 iOS 远控配置即可回滚到 DF-045；Reservation、自动化 Session Fence 和 Android STF 不受影响。
