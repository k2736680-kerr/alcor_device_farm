# DF-027 设备操作、人工预约和 STF 远控页面验收

## 状态

pending。依赖 DF-018 和 DF-026，尚未开始实现或测试。

## 目标

在 Device Farm Console 中完成设备域写操作、人工 Reservation、续租、释放、审计和当前预约绑定的 STF 短时远控入口，且不重写 STF 或直接访问内部基础设施。

## 必须保存的证据

- Image、Host、Pool、Device 危险操作的确认、reason 和审计结果；
- 人工预约从 pending 到 active、续租、释放和设备回池的浏览器端到端报告；
- 单台设备第二条预约保持 pending/capacity unavailable 的页面与数据库证据；
- STF 短时入口可用、越权被拒绝且浏览器无管理 Token 的证据；
- Server/STF 故障时错误码、request ID 和无虚假成功状态的证据；
- 临时预约、容器、网络、卷和会话清理结果。

## 完成条件

满足 `docs/05_step_by_step_implementation.md` 中 DF-027 的全部验收条件后，更新本文件为 completed，并记录独立中文 Git commit。
