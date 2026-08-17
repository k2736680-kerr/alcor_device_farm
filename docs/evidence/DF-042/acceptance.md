# DF-042 Reservation 绑定的 iOS Session Fence 验收

## 当前状态

`pending`

验收入口：无预约、错/多 UDID、Grant 重放、跨 Host、插件 busy 漂移、Session 过期回收和单设备唯一 Session。Mock 不能替代 E4 真实 Appium Device Farm Node。

必须保存 Reservation、脱敏 Grant、Device Session、Appium Session ID、UDID 和插件 busy 的完整时序，以及 Reaper、Agent/Appium 离线和漂移隔离结果。Fence 只能验证、限制和透明路由上游协议，不得新增页面动作、断言或业务 Session 执行。
