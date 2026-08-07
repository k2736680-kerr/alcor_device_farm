# ADR-0012：管理员受控删除隔离或已停止设备

## 状态

已确定。

## 背景

ADR-0011 已实现通过固定目标缩容删除最旧空闲设备，但控制台中的隔离设备只有恢复和重建入口。对于 Provider 资源无法恢复、删除失败后残留或已经确认不再需要的隔离设备，管理员无法从设备页面完成收尾，只能人工操作数据库或服务器。这既破坏控制台闭环，也容易绕过 Host Command、审计和资源清理边界。

现有系统已经具备持久化 delete Host Command、Agent 幂等执行、Docker Provider 容器/网络/卷清理、Device 状态机和审计，不应新增第二套删除实现。

## 决策

- 新增 `DELETE /api/v1/devices/{id}`，请求体继续使用 `OperationReason`，并强制 `Idempotency-Key` 和操作者身份；
- 只允许管理员删除 `quarantined` 或 `stopped` Device；ready、reserved、busy、recycling、provisioning 和 booting 均拒绝；
- 删除前在同一 PostgreSQL 事务内锁定 Device，确认没有 pending/active Reservation 和同设备 pending/leased delete Command，禁用全部 Pool membership、写设备审计并创建一条 management delete Host Command；
- Server 和浏览器不访问 Docker Socket。Agent 继续调用既有 Provider `Delete`，幂等清理容器、网络、端口和卷；
- Agent 返回 `deleted=true` 后 Device 转为 `deleted`，清空 STF/ADB/Appium Endpoint，并保留数据库历史；
- 命令失败按既有 Host Command 上限重试，最终失败时 Device 保持或回到 `quarantined/unhealthy`，写健康事件，不伪造删除成功；
- Console 只在 quarantined/stopped 行显示“删除”，必须填写原因并二次确认；提示固定目标未降低时 Controller 可能自动补建；
- 人工删除不改变 Pool Image 目标、Pool 并发或 Host 容量配置。减少设备总量仍必须修改目标设备数。

## 后果

- 隔离设备可以在控制台内安全收尾，不再需要数据库或服务器人工修改；
- 人工删除和自动缩容共用 Agent/Provider 删除能力，但具有不同的 operation source、审计动作和选择规则；
- Device 只做逻辑删除并保留历史，符合设备域审计要求；
- 目标容量不变时删除设备可能触发自动补建，这是预期行为，不把单设备删除误解为缩容。
