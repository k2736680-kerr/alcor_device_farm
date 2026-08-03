# DF-022 实施与验收证据

## 当前结论

权限、审计和敏感数据加固已完成本地实现与自动化测试。当前机器没有真实 Linux 部署、Host Agent、Docker Socket 网络隔离环境和集中日志系统，无法完成真实部署秘密扫描与网络边界验收，因此 DF-022 状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- Service/Agent current + previous Token 轮换；移除 previous 后旧 Token 返回 401；
- previous 不能脱离 current 使用，Service/Agent 四个非空 Token 不得重复；
- Service Token 和 Agent Token 的 API 路径权限继续严格隔离；
- Device restart/rebuild/quarantine/unquarantine 保存 actor、action、request ID 和 reason；
- Device 状态更新与管理审计在同一 PostgreSQL 事务中提交；
- `X-Device-Farm-Actor-Id` 支持 Alcor Adapter 透传操作者，缺省时回落为 Service 身份；
- 管理操作和 Reservation release 的敏感 reason 返回 400；
- Image/Host/Reservation 配置字段携带敏感 key 或值时返回 400；
- 日志普通字符串、健康事件嵌套 payload、Host Command payload/result、Reservation 审计和 Device 审计均在写出前脱敏；
- Server 仍不接触 Docker Socket，真实 Provider 权限只保留在 Host Agent。

## 本地验收结果

```text
PASS current/previous Token 轮换与旧 Token 失效
PASS previous 无 current 和跨身份 Token 重复时配置校验失败
PASS Service/Agent 路径权限隔离
PASS 管理 Device 操作缺少 reason 时返回 400
PASS 敏感 Device/Reservation reason 返回 400
PASS 敏感资源配置、能力和容量字段返回 400
PASS Device 状态和审计原子写入，Alcor actor ID 可追踪
PASS 日志 message/error 中 Token 和密码被脱敏
PASS Agent health reason/payload 入库后秘密检出数为 0
PASS Host Command result 入库和 API 返回前秘密检出数为 0
PASS migration up/down/up 与全量 Go 门禁
```

## 真实环境验收

1. 在 Linux 服务器通过 Secret 管理系统配置新 current + 旧 previous，滚动重启 Server；
2. 验证新旧 Agent Token 均可心跳，随后清空 previous 并重启，旧 Token 必须返回 401；
3. 分别使用 Service Token、Agent Token 跨域调用，确认 403；
4. 从 Alcor Adapter 发起隔离/重建，确认 actor ID、request ID 和 reason 可关联；
5. 注入仅用于验收的 canary Token，扫描 Server/Agent/STF 日志、数据库导出、API 响应和验收证据，精确检出数必须为 0；
6. 从 Alcor 网络和浏览器侧验证 Docker Socket 不可达，只有 Host Agent 运行账户具有最小访问权限。

## 阻塞解除条件

完成真实部署 Token 轮换、canary 秘密零检出和 Docker Socket 网络/账户权限验证后，将 DF-022 改为 `completed`。本地 Mock 与单元测试不能替代真实权限边界验收。
