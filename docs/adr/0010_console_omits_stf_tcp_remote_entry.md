# ADR-0010：控制台不展示 STF remoteConnect TCP 地址

## 状态

已确定。

## 背景

DF-027 曾把 STF REST API `remoteConnect` 返回的 `remote_connect_url` 展示为“STF 远控入口”。真实 STF 联调确认该值是供 ADB 客户端使用的 TCP 地址，不是 STF 原生浏览器 Devices 页面，也不包含可安全交给浏览器的短时 Web 授权。

把 TCP 地址放入弹窗会误导用户，且无法满足“打开 STF 原生设备页面”的需求。当前部署的 STF 也没有与 Device Farm Console 会话绑定的短时 Web 登录或单设备授权能力。

## 决策

- Device Farm Console 删除“STF 远控”按钮、地址弹窗和相关登录页文案；
- Console 不调用 `/api/v1/device-reservations/{id}/remote-sessions`，不把 `remoteConnect` TCP 地址冒充浏览器 URL；
- STF Adapter 的 inventory、claim、release、remoteConnect 和 remoteDisconnect 后端能力继续保留，供设备编排和未来可信服务端 Adapter 使用；
- STF 原生 Web 页面如需开放，必须作为独立受控服务配置 HTTPS 与真实认证，不能复用 Device Farm Console Cookie，也不能向浏览器暴露 STF 管理 Token；
- 以后只有在 STF 或受控网关提供“与当前 Reservation 绑定、短时有效、可审计、可撤销”的 Web 授权契约后，才能通过新的 ADR 和 OpenAPI 字段重新加入 Console 入口；
- DF-028 的 Web 验收改为确认 Console 不展示伪远控入口，同时继续真实验证 STF claim/release、设备可见性、重启收敛和 Token 隔离。

## 覆盖关系

本 ADR 只覆盖 ADR-0009、DF-027/DF-028 和 Web 验收中“Console 展示 STF 短时入口”的部分，不覆盖 DF-018 已完成的 STF Adapter 技术契约，也不移除后端 remote session API。

## 后果

- 用户不会再看到无法在浏览器打开的 ADB TCP 地址；
- Console 继续完整支持资源查看、人工预约、续租、释放、设备操作和审计；
- STF 仍是独立远控产品，设备农场不重写其画面、触控、日志或文件能力；
- 后续接入原生 STF Web 页面需要先补齐认证和 Reservation 级授权，而不是仅拼接页面 URL。
