# DF-043 iOS Simulator 固定库存接入验收

## 当前状态

`pending`

验收入口：E4 两台 allowlist Simulator 的发现、健康、Pool、预约、并发 XCUITest、故障恢复和 50 次稳定性。未通过真实 Simulator 不得 completed。

必须保存 Simulator Runtime/机型/脱敏 UDID、simctl/Appium inventory 对齐、两台并发 Session、shutdown/boot timeout/Agent 重启和 50 次循环后的 Device/Reservation/插件 busy/端口清理结果。首期不得以任务名义自动下载 Runtime、克隆或删除任意 Simulator。
