# DF-028 控制台部署、安全和真实 Web 验收

## 状态

pending。依赖 DF-017～DF-024 和 DF-027，尚未开始真实 Linux Web 验收。

## 目标

把 Device Farm Console 纳入正式部署、升级、回滚和安全基线，并在一台 Android 16 Emulator、STF、RethinkDB、Appium 和真实 Device Farm Server 上完成浏览器全链路验收。

## 必须保存的证据

- 新环境部署、健康检查、HTTPS/受控内网访问和静态资源版本；
- 未认证、越权、CSRF、过期会话、伪造 actor 和内部端口隔离测试；
- Web 完成资源查看、预约、远控、续租/释放和受控设备操作的报告与截图；
- Server、STF 和 Console 重启后的状态收敛；
- 升级与回滚演练结果；
- Token 精确扫描和全部临时资源清理结果。

## 完成条件

满足 `docs/05_step_by_step_implementation.md`、`docs/07_acceptance_test_plan.md` 的 DF-028 和 AT-WEB 全部 P0/P1 条件后，更新本文件为 completed，并记录独立中文 Git commit。
