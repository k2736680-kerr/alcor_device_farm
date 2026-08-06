# DF-022 实施与验收证据

## 当前结论

权限、审计和敏感数据加固已完成本地实现、自动化测试和真实 Linux 环境验收。2026-08-06 在 `10.0.30.171` 完成 Service/Agent Token 轮换、跨身份权限、真实 Host Agent 心跳、设备操作审计、秘密零检出和 Docker Socket 边界验证，DF-022 状态为 `completed`。

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

环境：Ubuntu 22.04、Docker、PostgreSQL、真实 Host Agent、单台 Android Emulator、STF 与 Appium。验收日志位于：

```text
/home/kerr/df022-acceptance-20260806/acceptance.log
/home/kerr/df022-acceptance-20260806/final-state.log
```

关键结果：

```text
TOKEN_OVERLAP_PHASE=service_old_new_200|agent_old_heartbeat_online|cross_identity_403
AGENT_SWITCHED_TO_NEW_TOKEN=true
TOKEN_REVOCATION_PHASE=old_service_401|old_agent_401|new_credentials_active
AUDIT_CORRELATION=actor_request_reason_atomic|device_ready_healthy
SECRET_SCAN=exact_runtime_tokens_and_canary_0|health_regex_0|audit_regex_0
DOCKER_BOUNDARY=server_socket_absent|server_uid_65532|readonly|cap_drop_all|host_socket_660_root_docker|agent_user_in_docker_group
DF022_REAL_ACCEPTANCE_PASS=true
```

- 新旧 Service Token 在重叠期均返回 200；旧 Agent Token 继续产生真实心跳，切换新 Agent Token 后 Host 保持 `online`；
- Agent Token 调北向接口、Service Token 调内部接口均返回 403；清空 previous 后旧 Service/Agent Token 均返回 401；
- 正式 restart API 保存唯一的 actor、request ID、action 和 reason 审计记录，设备操作与审计同事务提交；
- 两次受控 Server 替换期间，设备因短暂心跳窗口被协调器隔离；restart Host Command 实际成功后，通过正式 unquarantine API 留存恢复审计，最终恢复 `ready|healthy|0`；
- 向敏感 reason 注入随机 canary 后请求返回 400；Server 日志、Agent 日志、数据库 data-only dump 和 API 响应对旧/新 Token 与 canary 的精确命中合计为 0；
- Server 容器没有 Docker Socket，运行用户为 `65532:65532`，根文件系统只读并 `CapDrop=ALL`；宿主 Socket 为 `660 root:docker`，只有加入 docker 组的 Host Agent 账户可访问；
- 最终 previous Token 均为空、Agent 与 Server current Token 一致，开放 Reservation 为 0，受管容器/网络/卷为 `1/1/1`，候选和回滚容器残留为 0。

最终 Host 为 `online`，Device 为 `ready|healthy|0`。真实 Token、密码和 canary 值未写入本文档或 Git 文件。
