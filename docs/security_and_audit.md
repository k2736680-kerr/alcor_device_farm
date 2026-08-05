# 安全与审计

## 身份和最小权限

- `/api/v1/device-*` 仅接受 Service Token，供新版 Alcor Worker、Alcor Adapter、DaFit Harness 和受控运维服务使用；
- `/internal/v1` 仅接受 Agent Token，Host Agent 不能调用北向管理接口；
- Device Farm Console 必须通过同源短时会话或受信任反向代理访问北向 API；浏览器不得接收或保存 Service Token，控制台身份必须映射为可审计的设备域操作者；
- Server 不访问 Docker Socket；Docker Socket 只允许 Linux 设备宿主机上的 Host Agent 访问；
- STF API Token、数据库 URL 和全部 Service/Agent Token 只通过部署 Secret 或环境变量注入，不进入 API 响应。

## Token 轮换

Service 和 Agent 分别具有 current、previous 两个配置槽：

| 身份 | current | previous |
|---|---|---|
| Service | `DEVICE_FARM_SECURITY_SERVICE_TOKEN` | `DEVICE_FARM_SECURITY_SERVICE_PREVIOUS_TOKEN` |
| Agent | `DEVICE_FARM_SECURITY_AGENT_TOKEN` | `DEVICE_FARM_SECURITY_AGENT_PREVIOUS_TOKEN` |

轮换顺序：

1. 生成新的随机 Token，将新值放入 current，将旧值临时放入 previous；
2. 滚动重启 Server；此时新旧 Token 都可用；
3. 切换 Alcor Adapter、DaFit Harness 或 Host Agent 到新 Token，并确认心跳和 API 正常；
4. 清空 previous 后再次滚动重启；旧 Token 会立即返回 401；
5. 在 Secret 管理系统中吊销和删除旧值。

previous 不能脱离对应 current 单独配置。四个非空 Token 必须互不相同，配置冲突时 Server 的 `--check-config` 和启动都会失败。

## 强制原因和操作者

以下操作必须提供 3～500 字符的 `reason`：

- Reservation 主动释放和强制释放；
- Device restart、rebuild、quarantine、unquarantine；
- 已有 Host drain/undrain 操作继续沿用原因校验。

原因中出现 `Bearer`、`token=...`、`password=...`、Cookie、credential、API key 或带用户名密码的数据库 DSN 时，请求返回 400，防止秘密进入审计链。

Alcor Adapter 调用设备管理操作时应设置 `X-Device-Farm-Actor-Id`，值为 Alcor 当前操作者的稳定标识。未设置时使用认证 Service 身份。该字段最多 128 个字符，不得包含控制字符或敏感值。

## 审计记录

设备状态更新和对应审计插入在同一 PostgreSQL 事务中完成。核心字段为：

- `actor_type`、`actor_id`：调用身份和操作者；
- `action`：如 `restart_device`、`rebuild_device`、`quarantine_device`；
- `resource_type`、`resource_id`：设备或预约；
- `request_id`：与响应和日志一致的关联 ID；
- `reason`：脱敏后的操作原因；
- `created_at`：数据库生成的事件时间。

Device Farm Console 可以提供设备域审计只读页面，只查询 `device_audit_events` 的脱敏技术审计字段；不得建立 Alcor 业务审计表或展示 Run/Result 业务审计。数据库运维只读查询示例：

```sql
SELECT created_at, actor_type, actor_id, action, resource_type, resource_id, request_id, reason
FROM device_audit_events
WHERE resource_type = 'device' AND resource_id = $1
ORDER BY created_at DESC, id DESC;
```

后续接入 Alcor 时，由 Adapter 使用现有 Device Farm API 发起操作，并把 Alcor 操作者 ID 透传到请求头；Alcor 自身仍保存平台业务审计，设备农场保存设备域技术审计。

控制台必须额外满足：

- 使用 Secure、HttpOnly、SameSite Cookie 或受信任代理提供等价会话保护；
- 所有写请求具备 CSRF 防护，退出和过期后会话立即不可继续写操作；
- 浏览器提交的 actor header 不得直接作为可信身份，操作者由服务端会话或受信任代理确定；
- 静态构建产物、Source Map、运行时配置、浏览器存储和网络响应中不得包含内部 Token；
- 内容安全策略禁止任意外部脚本，远控只允许受控 STF 来源和短时入口。

## 脱敏和禁止项

- 日志按敏感字段名整体替换，并扫描普通 `message`、`error` 字符串中的秘密形态；
- Agent 健康事件的 reason 和嵌套 payload 入库前递归脱敏；
- Host Agent 心跳容量、Host Command payload/result 入库前递归脱敏，错误码只允许稳定的大写枚举格式；
- Image/Host/Reservation 的资源配置、能力和容量字段出现敏感 key 或敏感值时直接拒绝，避免秘密参与调度匹配；
- Reservation 和 Device 审计在 Repository 层再次脱敏，避免上层遗漏；
- 禁止在日志、审计、健康事件、Host Command 普通字段、测试证据和 Git 文件中保存真实 Token、密码、Cookie 或 STF 管理密钥；
- 调试时只能记录秘密类型、Secret 版本号或哈希前缀，不记录原值。

## 复核

```sql
SELECT count(*)
FROM device_health_events
WHERE payload::text ~* '(bearer[[:space:]]+|token[[:space:]]*[:=]|password[[:space:]]*[:=])';

SELECT count(*)
FROM device_audit_events
WHERE coalesce(reason, '') ~* '(bearer[[:space:]]+|token[[:space:]]*[:=]|password[[:space:]]*[:=])';
```

查询结果必须为 0。真实 Secret 值还应由部署侧秘密扫描工具对日志目录、数据库导出和验收证据做精确匹配。
