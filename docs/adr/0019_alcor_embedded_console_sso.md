# ADR-0019：Alcor 以同源受控代理嵌入设备农场控制台

## 状态

已确定。

## 背景

设备农场已经交付可独立运行的 Console，新版 Alcor `test` 分支也已经具备钉钉登录、App Build、Android Worker 和 Device Farm Adapter。产品希望在 Alcor 一级导航直接管理 APK、设备、预约和远控，不再重复登录独立设备农场后台。

直接复制 Device Farm Console 到 Alcor 会形成两套页面和操作语义；让浏览器直接访问设备农场或持有 Service Token 又会破坏既有安全边界。

## 决策

- Alcor 一级导航使用“设备农场”，默认嵌入现有 Device Farm Console；“应用版本（APK）”作为该页面的二级区域，数据仍属于 Alcor。
- Alcor API 校验既有钉钉会话，并通过同源受控代理转发设备请求；浏览器不持有 Device Farm Service Token、STF Token 或独立 Console 密码。
- Alcor 服务端只从已验证会话取得用户 ID，通过 `X-Device-Farm-Actor-Id` 传给设备农场；浏览器提交的同名 Header 不可信且必须被覆盖。
- 设备农场保留独立 Console 和独立 PostgreSQL；嵌入模式复用同一前端源码、OpenAPI 客户端、Device/Reservation 状态机和设备域审计。
- 管理员 Web 远控增加等价的 Service 认证北向入口，内部继续复用现有 `remotecontrol.Service`、Reservation、STF JWT 和 Reaper，不复制远控实现。
- Alcor 代理只允许固定设备域路径和方法，剥离浏览器 Cookie、Authorization 及跳到任意上游的能力；写请求必须带同源前端专用 Header。

## 后果

- 用户只登录一次 Alcor 即可完成 APK 管理、设备查看、预约和远控。
- 独立 Device Farm Console 仍可用于故障隔离和回滚，但日常入口统一到 Alcor。
- 当前 Alcor 尚无完整平台 RBAC，统一入口沿用其现有“已认证用户可管理”的权限语义；后续平台 RBAC 落地时只收紧 Alcor 代理授权，不修改设备 API 和数据库。
