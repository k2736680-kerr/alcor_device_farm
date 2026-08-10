# ADR-0013：管理员通过短租约进入 STF 原生 Web 远控

## 状态

已实施，DF-031 真实验收通过。

## 背景

ADR-0010 正确禁止把 STF `remoteConnect` 返回的 ADB TCP 地址展示为浏览器入口，但管理员仍需要在 Device Farm Console 的设备页面直接操控真实手机。当前阶段只有一名管理员和一台设备，优先使用新浏览器标签页，不做 iframe、多用户共享或 Alcor Eval Console 集成。

DeviceFarmer/STF 3.7.9 已提供完整的原生看屏、点击、滑动、输入、Home、返回、日志和文件能力。STF App 支持 HS256 JWT 登录，登录后建立自身 Session 并从地址移除 JWT；指定设备路由为 `/#!/control/{serial}`。因此本项目只需要编排设备占用和短时 Web 授权，不能复制 STF 的远控实现。

## 决策

- 在 Device 页面只对 `ready/healthy` Device 向管理员显示“远程连接”；服务端仍重新校验设备、Host、Pool 和权限状态。
- 一次远控直接复用一条 `owner_type=manual` 的短租约 Reservation，并记录只供 Scheduler 使用的精确 Device 选择条件；不新增第二套远控占用真相或业务数据库。
- Scheduler 必须把该内部选择条件解释为 Device ID 约束，并继续执行 PostgreSQL 锁、Pool 并发检查和 STF claim；普通 Reservation 不能注入该内部条件。
- 连接建立后，Server 使用与 STF `--auth-secret` 相同的受限 Secret，为固定 STF 管理员身份签发极短有效的 HS256 Web 登录 JWT。响应只返回短时入口，不返回 STF API Token 或签名 Secret。
- 浏览器先同步打开空白标签页，待 Reservation active 后跳转到 `STF_WEB_URL/?jwt=...#!/control/{serial}`；STF 随即建立自己的 Session 并通过重定向移除 JWT。
- Console 保留标签页句柄并轮询 `closed`。点击“挂断”或检测到标签页关闭时调用同一个结束 API；结束 API 释放 Reservation 和 STF claim，使 Device 进入既有 `recycling → rebuild → ready/healthy` 链路。
- 连接期间 Console 定时发送心跳。浏览器崩溃、Console 被关闭或网络中断时，短租约停止续期，由现有 Reaper 兜底回收；STF 已自行释放设备时，心跳检查 STF inventory 并主动结束对应 Reservation。
- 第一阶段仅允许 `admin`，同一 Device 同时只允许一条 pending/active 远控 Reservation。多用户共享、排队、观察者模式和 Alcor 身份透传另行设计。
- 远控响应设置 `Cache-Control: no-store`；JWT、Secret、STF API Token 不得写入日志、审计、数据库普通字段、截图或测试报告。生产必须使用 HTTPS/受控内网，并配置不向 STF 页面发送 Console 来源信息的 Referrer Policy。
- STF 3.7.9 `local` 会在 INFO 日志打印包含 `--auth-secret` 的子进程命令行；当前部署必须对 STF 主容器禁用 Docker stdout 持久化。若未来恢复进程日志采集，必须先通过 Secret 脱敏验收；STF 页面内的设备 Logcat 不受此限制。
- Console 启用 HSTS 而 STF 仍为 HTTP 时，STF 必须使用不同于 Console 的受控主机名，因为 HSTS 按主机名生效且不区分端口。当前验收环境使用解析到同一内网 IP 的独立名称；正式环境优先替换为内网 DNS，后续可演进为 STF 全链路 HTTPS/WSS。

## 覆盖关系

本 ADR 在满足 Reservation 绑定、短时有效、可审计、可撤销的条件后，覆盖 ADR-0010 中“Console 暂不提供 STF Web 入口”的结论；继续保留 ADR-0010 对 ADB TCP `remoteConnect`、Token 隔离和禁止伪造浏览器入口的全部限制。

## 后果与限制

- 第一阶段关闭标签页后的主动回收依赖 Device 页面仍在运行；Console 同时崩溃时由短租约和 Reaper 在限定时间内兜底，不承诺浏览器进程退出瞬间完成网络请求。
- Reservation 最大租期仍受 Pool `max_lease_seconds` 约束；达到上限后管理员需要重新连接。
- STF Web 登录身份必须与 STF API Token 的设备所有者一致，否则原生控制页无法访问已 claim 的设备。
- 新功能只编排 Device、Pool、Reservation、STF claim/release 和设备域审计，不创建 Alcor Run/Result，也不实现 Appium 或 DaFit 业务步骤。
