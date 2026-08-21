# ADR-0027：远控页面临时安装必须固定到预约设备

## 状态

已接受，DF-052 已完成。

## 背景

人工调试允许在 STF 或 Baguette 原生单设备页临时安装测试包。Android STF 已提供 APK 安装，Baguette 0.1.92 已提供 `POST /simulators/:udid/files`，并在成功返回前执行 `xcrun simctl install <udid>`。设备农场不应复制这些实现。

现有编排仍有两个多设备风险：Android 页面使用通用 `serial` 而不是 STF 实际登记的 `stf_serial`；iOS Gateway 的所有标签页共用一个 Cookie 名，后打开的设备会覆盖前一个标签页的浏览器会话。即使上游安装命令支持明确目标，入口上下文不唯一仍会造成错误、拒绝或误导性结果。

## 决策

1. 不新增 APK/IPA 存储、解析、安装 API、Host Command 或 App Build 模型；继续完全复用 STF/Baguette 原生安装。
2. Android Scheduler claim、remoteConnect、release 和 STF Web 单设备入口统一使用 `COALESCE(stf_serial, serial)`；不得自动选择第一台 ADB 设备。
3. iOS Gateway 的 HttpOnly 会话 Cookie 名按 UDID 派生，使同一浏览器中的多个 Simulator 标签页可以并存而不覆盖。
4. Gateway 优先从 `/simulators/:udid/...` 请求路径确定目标；静态资源和 `/simulators.json` 只能使用同源 `Referer` 对应的目标会话。只有一个有效会话时允许兼容无目标的只读请求；多个会话且无明确目标时拒绝。
5. App 上传只允许当前预约 UDID 的原生 `/files` 路径。错误会话、其他 UDID、预约失效、来源不一致或上游失败均不得返回成功。
6. Baguette 的 `simctl install <udid>` 成功响应和 STF 原生安装结果继续作为临时安装完成依据；浏览器上传到 Host、放入下载目录或仅收到文件不算安装成功。

## 后果

- 两台或更多设备同时打开时，每个页面的安装目标仍与 Reservation、Device 和技术序列号一致；
- 不产生第二套安装协议、业务 App/Build 管理或 IPA Artifact；
- 旧的单设备 Baguette 会话在部署后需要从设备页面重新打开；
- 正式 Alcor 不需要修改，Android/iOS 自动化 Session 也不受影响。
