# 设备农场 MVP 验收方案

> 本文件记录 Android 第一版 MVP 的验收基线。DF-028、DF-029～DF-038 和本地 ALCOR-001 的后续完成情况分别以各任务证据和实施清单为准；第二版 iOS 验收环境与用例必须由 DF-039 设计后再追加，不能用本文件的 Android Mock 或 Linux KVM 结果替代。

第二版 iOS 的 E4/E5/E6 环境、G10～G16、P0/P1 用例和证据要求已经独立定义在 [第二版 iOS 设备农场验收方案](09_ios_device_farm_v2_acceptance.md)。Android 本文件继续作为强制回归基线，不能被新方案覆盖或降级。

## 1. 验收原则

- `P0`：核心正确性和安全项，必须全部通过；
- `P1`：MVP 可运维和稳定性项，正式交付前必须通过；
- `P2`：增强项，可以记录为后续改进，但不得影响 P0/P1；
- 只有真实 Linux KVM、Docker Emulator、Appium 和 STF 环境通过后，才能宣称“真实设备农场 MVP 完成”；Mock 通过只代表控制面完成；
- 验收不能通过手工修改数据库制造成功状态。

## 2. 验收环境

### E0：本地 Mock 环境

- Windows 或 Linux 开发机；
- Device Farm Server；
- PostgreSQL；
- Mock Provider；
- 不要求 Docker Emulator、STF 和 Appium。

用途：接口、状态机、并发、幂等、Scheduler、Reaper、Reconciler 和故障测试。

### E1：Linux KVM 设备环境

- 支持 `/dev/kvm` 的 Linux Host；
- Docker Engine；
- Host Agent；
- 至少一个通过验证的 Android x86_64 镜像；
- 资源足以稳定运行一台 Android 16 Emulator，容器内存上限 5 GiB。

用途：真实创建、启动、ADB、boot、清理、重建和固定目标自动补齐。

### E2：完整联调环境

- E1 全部组件；
- STF + RethinkDB；
- Appium 2 + UiAutomator2；
- `dafit_auto_platform` Farm 适配分支；
- Device Farm Harness。

用途：远控、Appium 并发、DaFit 冒烟和全链路故障恢复。

### E3：设备控制后台环境

- E0 的 Device Farm Server、PostgreSQL 和 Mock Provider，用于页面功能和故障自动化；
- E2 的真实单台 Android 16 Emulator、STF、RethinkDB 和 Appium，用于最终 Web 验收；
- Device Farm Console 生产构建；
- HTTPS 或受控内网反向代理、浏览器安全会话和可审计操作者身份；
- Chromium 系浏览器以及浏览器端到端自动化入口。

用途：资源管理、人工预约、远控、设备操作、权限、安全、部署和回滚验收。

## 3. 阶段验收门

| Gate | 对应任务 | 通过条件 |
|---|---|---|
| G0 文档基线 | DF-000 | 环境、依赖、端口和阻塞项清楚，无密钥泄露 |
| G1 Mock 控制面 | DF-001~DF-008 | E0 可管理全部核心资源，状态机和 migration 通过 |
| G2 预约正确性 | DF-009~DF-011 | E0 并发预约无双占，租约和状态可收敛 |
| G3 Docker 设备 | DF-012~DF-016 | E1 一台 Android 16 Emulator 可创建、健康、补池和重建，第二个预约不突破容量 |
| G4 STF/Appium | DF-017~DF-018 | E2 可远控、claim/release，Appium 端口隔离 |
| G5 DaFit 闭环 | DF-019~DF-021 | E2 冒烟成功/失败均可释放和清理 |
| G6 可交付 | DF-022~DF-025 | 安全、运维、回滚、全量验收和 Adapter 契约齐全 |
| G7 Web 可用 | DF-026~DF-028 | E3 可通过浏览器完成设备管理、预约、远控和释放，且无内部 Token 泄露 |
| G8 容量闭环 | DF-029 | 后台单点设置目标即可自动扩缩容；缩容不强删占用设备且真实清理资源 |
| G9 隔离设备清理 | DF-030 | 管理员可安全删除无预约的隔离/停止设备；Agent 清理资源，失败保持隔离 |

未通过前一 Gate，不进入下一阶段的真实环境部署。DF-026/DF-027 的 E0 本地实现可与 G4～G6 的真实验收并行；G7 只有 DF-028 真实 Web 验收通过后才完成。

## 4. 功能验收用例

### 4.1 API、认证和契约

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-API-001 | P0 | 无 Token 调用北向 API | 401，统一错误结构，不返回内部堆栈 |
| AT-API-002 | P0 | Agent Token 调用北向管理 API | 403，Agent 权限不能越界 |
| AT-API-003 | P0 | 请求携带 request/run/attempt/trace ID | 响应和日志可按关联 ID 查到同一链路 |
| AT-API-004 | P0 | 相同 Idempotency-Key 重复创建预约 | 返回同一 reservation，不重复占设备 |
| AT-API-005 | P0 | 非法 UUID/ULID、租期或能力 | 400 和稳定错误码，数据库无脏记录 |
| AT-API-006 | P1 | OpenAPI 示例和实际响应比对 | Schema 一致，无未记录字段 |

### 4.2 数据库和状态机

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-DB-001 | P0 | 空库执行 up/down/up | 全部成功且结构一致 |
| AT-DB-002 | P0 | 非法 Device 状态转换 | 领域层拒绝，状态不变并有错误事件 |
| AT-DB-003 | P0 | quarantined Device 参与调度 | 不得被选中 |
| AT-DB-004 | P0 | 同一 Device 插入两个 active reservation | 数据库唯一约束拒绝 |
| AT-DB-005 | P0 | Repository 事务中途失败 | 全部回滚，无半条 Session/Reservation |
| AT-DB-006 | P1 | 数据库短暂断开后恢复 | 服务恢复处理，未确认事务不被当作成功 |

### 4.3 Scheduler、Reservation 和租约

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-SCH-001 | P0 | 100 个并发预约竞争 2 台 ready 设备 | 任意时刻最多 2 个 active，无双占 |
| AT-SCH-002 | P0 | 要求 API/ABI/分辨率能力 | 只分配完全满足的设备 |
| AT-SCH-003 | P0 | 无满足能力设备 | pending 或明确容量失败，不错配设备 |
| AT-SCH-004 | P0 | 运行方持续续租且逻辑运行时间超过 4 小时 | expires_at 按数据库当前时间滑动延长；总运行时间不锁死，任意时刻的未来窗口不超过 Pool `max_lease_seconds` |
| AT-SCH-005 | P0 | 过期后续租 | 被拒绝，不复活旧预约 |
| AT-SCH-006 | P0 | 同时调用两次 release | 幂等成功，只执行一次底层释放 |
| AT-SCH-007 | P0 | 两个 Scheduler 实例同时领取 | 一个 pending reservation 只被处理一次 |
| AT-SCH-008 | P0 | 两个 Reaper 实例同时回收 | 只产生一次终态和清理命令 |
| AT-SCH-009 | P1 | ready warm 设备正常申请 | 10 秒内获得 active 或返回明确可重试错误 |
| AT-SCH-010 | P1 | 预约过期 | grace period 后 60 秒内关闭并进入清理 |

### 4.4 Host Agent 和 Docker Emulator

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-AGT-001 | P0 | Agent 注册和连续心跳 | Host online，容量和能力正确 |
| AT-AGT-002 | P0 | Agent 停止心跳 | 超时后 Host offline，不再接收新分配 |
| AT-AGT-003 | P0 | 命令重复领取/完成 | Provider 操作只执行一次，旧完成不能覆盖新结果 |
| AT-AGT-004 | P0 | Agent 执行中重启 | 命令最终可恢复、重领或明确失败，无永久 executing |
| AT-EMU-001 | P0 | 创建一台 Android 16 Emulator | Android 16/API 36、ADB online、boot completed，连接信息完整 |
| AT-EMU-002 | P0 | 启动健康检查 | ADB online、boot completed、Appium healthy 后才 ready |
| AT-EMU-003 | P0 | 无 `/dev/kvm` | 明确返回 KVM_UNAVAILABLE，不标记 ready |
| AT-EMU-004 | P0 | rebuild | 新实例不保留上一次 App 和测试文件 |
| AT-EMU-005 | P0 | 创建两个不同 Device Image | Host Command、Agent 校验和 Docker 容器分别使用各自 `docker_image`，rebuild 不串换版本 |
| AT-EMU-006 | P1 | 删除设备 | 容器、网络、端口、卷和数据库引用按策略清理 |
| AT-EMU-007 | P0 | Pool `total_target=1/min_ready=1` 且为空 | 使用 Pool 默认 Image 自动创建并加入一台；两个 Controller 并发不超建；第二个预约不突破总目标/最大并发 |
| AT-EMU-008 | P2 | 资源允许时创建两台 Emulator | serial、ADB/Appium 端口、容器名互不冲突 |
| AT-EMU-009 | P0 | 控制台把 Pool 总目标和最小预热从 1 调到 2 | 无需修改/重启 Agent；资源足够时 Controller 自动补齐到 2，资源不足时保留目标并报告容量不足，不伪造 Host 容量 |
| AT-EMU-010 | P0 | 目标从 3 调到 1，三台均空闲 | 删除最旧两台并保留最新；Device 标记 deleted；容器、网络和卷无残留 |
| AT-EMU-011 | P0 | 最旧设备存在 active Reservation 时缩容 | 不强删、不影响预约；存在其他空闲设备时删除最旧的可删空闲设备，全部占用时等待释放 |
| AT-EMU-012 | P0 | 两个 Controller 并发缩容 | 每台超额设备只有一个有效 delete Command，无重复删除或扩缩容振荡 |
| AT-EMU-013 | P0 | delete 连续失败 | Host Command 按上限重试；设备最终 quarantined/unhealthy、不可调度且有健康事件和审计 |
| AT-EMU-014 | P0 | 管理员删除隔离设备 | 仅 quarantined/stopped 且无活动预约可提交；相同幂等键单命令；成功后资源清理、membership 禁用、Device=deleted、Endpoint 为空 |
| AT-EMU-015 | P0 | Pool 同时登记 Android 13～16 并切换默认 Image | 镜像数量不相加；已有设备不重装，后续自动补建设备使用新默认 Image |
| AT-EMU-016 | P0 | `total_target=3/min_ready=0/max_concurrency=2` | 空闲时不维持常驻预热设备；出现默认 Image 可满足的 pending Reservation 时按需创建；总设备数不超过 3、同时占用不超过 2，三个值可在 Console 独立调整并受关系校验 |
| AT-EMU-017 | P0 | 空闲或隔离 Emulator 执行受控 reimage | 旧容器和数据卷被删除重建；Device ID/Pool membership 不变；目标配置通过 ADB、STF、Appium 后才生效并恢复 ready/healthy |
| AT-EMU-018 | P0 | reimage 目标配置启动失败 | 当前 Image/规格不提前切换；只尝试恢复旧配置一次；恢复成功记录失败状态，恢复也失败则隔离并保留审计和健康事件 |

### 4.5 STF 和 Appium

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-STF-001 | P0 | STF inventory 同步 | serial 与 Device 唯一对应，STF 不覆盖业务状态 |
| AT-STF-002 | P0 | claim 成功 | Reservation 才可进入 active |
| AT-STF-003 | P0 | claim 失败 | 预约失败/重试并补偿，不返回连接信息 |
| AT-STF-004 | P0 | release 暂时失败 | 后台重试并审计，不能静默关闭底层占用 |
| AT-STF-005 | P0 | 请求远控入口 | 只返回短时入口，不返回管理 Token |
| AT-STF-006 | P0 | A 用户访问 B 预约远控 | 403，不能越权 |
| AT-STF-007 | P0 | 管理员从 Device 页面打开 STF Web 远控 | 无需再次登录，直接进入指定 serial 的 STF 原生控制页；真实看屏、点击、滑动、输入、Home、返回有效 |
| AT-STF-008 | P0 | 检查 STF Web 短时授权 | JWT 极短有效并在 STF 重定向后从地址移除；浏览器、响应、日志和数据库无 STF API Token 或签名 Secret |
| AT-STF-009 | P0 | 挂断、关闭远控标签页、Console 崩溃或网络中断 | 主动路径立即释放；异常路径由短租约和 Reaper 回收；STF claim、Reservation 和设备最终收敛到空闲/ready/healthy |
| AT-STF-010 | P0 | STF 原生页面主动释放设备 | 下一次心跳检测 using=false，结束远控 Reservation 并进入统一清理链路 |
| AT-APP-001 | P0 | 单台设备创建 Appium Session | `/status` healthy，UiAutomator2 Session 创建和删除成功 |
| AT-APP-002 | P0 | Appium unhealthy | Device 不进入 ready 或被隔离 |
| AT-APP-003 | P2 | 资源允许时并发两台 Appium Session | 两个 Session 同时成功且 UDID 不串设备 |

### 4.6 DaFit 闭环和数据隔离

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-DFT-001 | P0 | DaFit 当前 collect-only | 以验收时用户工作树为事实基线；DF-047 当前收集 449 个执行实例，设备农场改造不得让该数量减少或写回历史 158 基线 |
| AT-DFT-002 | P0 | Farm 模式未传 UDID | 立即失败，不自动选择第一台设备 |
| AT-DFT-003 | P0 | 冒烟用例成功 | 报告生成、预约释放、设备进入 recycling/ready |
| AT-DFT-004 | P0 | 冒烟用例断言失败 | 失败报告保留，预约仍释放 |
| AT-DFT-005 | P0 | Harness 被强制终止 | Reaper 最终回收设备 |
| AT-DFT-006 | P0 | 下一预约检查上一任务数据 | App 数据、缓存和指定测试文件不可见 |
| AT-DFT-007 | P2 | 资源允许且存在两台真实设备时执行两个 DaFit 冒烟并发 | 各自使用独立设备和报告目录，不串数据；当前 ADR-0008 单 Emulator 基线继续强制验证第二个预约不突破容量 |

### 4.7 故障恢复和安全

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-REL-001 | P0 | Server 在 active reservation 时重启 | 重启后两分钟内恢复正确状态，不丢预约 |
| AT-REL-002 | P0 | Agent 离线 | 受影响设备停止分配并进入恢复/隔离 |
| AT-REL-003 | P0 | Emulator boot timeout | 命令失败，设备不 ready，按策略重建/隔离 |
| AT-REL-004 | P0 | PostgreSQL、STF、Appium 分别超时 | 错误分类准确，无永久悬挂资源 |
| AT-SEC-001 | P0 | 扫描响应、日志、审计和数据库普通字段 | 无 Token、密码、Cookie、STF 管理密钥 |
| AT-SEC-002 | P0 | 强制释放/隔离/重建无 reason | 请求被拒绝 |
| AT-SEC-003 | P0 | 过期或错误 Agent Token | 认证失败，不能上报伪造设备 |
| AT-SEC-004 | P0 | 从 Alcor/浏览器访问 Docker Socket | 网络和配置层均不可访问 |
| AT-REL-005 | P1 | 连续 8 小时或至少 50 次申请/执行/释放 | 零双占、零永久悬挂，资源无持续增长 |

### 4.8 新版 Alcor 契约准备

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-ALC-001 | P0 | Mock RunAttempt 创建预约 | 使用 UUID/ULID Owner，不依赖旧 Eval Task |
| AT-ALC-002 | P0 | capacity unavailable | Adapter 可识别 retryable 并稍后重试 |
| AT-ALC-003 | P0 | Provider/STF/Appium 故障 | 可稳定映射为基础设施失败信息 |
| AT-ALC-004 | P0 | Attempt 取消/超时 | release 接口幂等，设备最终回收 |
| AT-ALC-005 | P1 | OpenAPI 生成客户端对 Mock Server 运行 | 申请、查询、续租、释放全部通过 |
| AT-ALC-006 | P0 | 钉钉用户从 Alcor 一级导航打开设备农场 | 同源加载既有 Device Farm Console，无第二次登录，不产生第二套 Device/Reservation 数据 |
| AT-ALC-007 | P0 | 在 Alcor 内预约、操作或远控设备 | Alcor 服务端使用 Service Token，设备域审计记录受控钉钉操作者；浏览器无 Service/STF Token、内部 Endpoint 和独立 Console Cookie |

### 4.9 Device Farm Console

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-WEB-001 | P0 | 未认证浏览器访问控制台和设备 API | 不返回设备数据，跳转登录或返回 401/403，静态资源不含内部 Token |
| AT-WEB-002 | P0 | 检查浏览器网络、Cookie、LocalStorage、SessionStorage 和构建产物 | 不存在 Service Token、Agent Token、STF 管理 Token、数据库 URL 或内部凭证 |
| AT-WEB-003 | P0 | 打开总览、Image、Host、Pool、Device、Reservation 页面 | 数据与 Server API/PostgreSQL 真相一致，状态和 request ID 可追踪 |
| AT-WEB-004 | P0 | 执行 restart/rebuild/quarantine/drain/release 等危险操作 | 必须二次确认并填写 reason；服务端非法状态拒绝被页面正确展示 |
| AT-WEB-005 | P0 | 创建一条人工预约并轮询 | 单台 ready 设备进入 active，连接信息属于当前预约；第二条预约不突破容量 |
| AT-WEB-006 | P0 | 检查 STF Web 边界 | Console 不展示 `remoteConnect` TCP 地址、不复制 STF 远控；只向管理员返回 Reservation 绑定的短时原生 Web 入口，不暴露管理 Token 或签名 Secret |
| AT-WEB-007 | P0 | 续租并释放预约 | expires_at 正确更新；释放后设备进入清理并最终回 ready，页面状态随 Server 收敛 |
| AT-WEB-008 | P0 | A 操作者访问或操作 B 的受控资源 | 按设备域权限返回 403，不能越权远控或释放 |
| AT-WEB-009 | P0 | 构造 CSRF、过期会话和伪造 actor 请求 | 请求被拒绝，审计中不接受浏览器伪造身份 |
| AT-WEB-010 | P0 | STF、Server 或网络故障时执行操作 | 页面显示稳定错误码、request ID 和重试提示，不出现虚假成功状态 |
| AT-WEB-011 | P1 | Server/STF/Console 重启并刷新页面 | 120 秒内恢复真实状态，不依赖浏览器缓存维持业务状态 |
| AT-WEB-012 | P1 | 新环境部署和版本回滚 | 控制台可访问、静态资源版本一致；回滚后 API 和设备状态不受损 |
| AT-WEB-013 | P0 | 在 Pool 页面只修改目标设备数 | Server 同步并发和 Host 容量，页面显示扩容/缩容收敛状态，不要求用户登录服务器 |
| AT-WEB-014 | P0 | 降低目标设备数 | 必须二次确认并填写 reason；历史 Device/Reservation 不被首页误计为当前运行容量 |
| AT-WEB-015 | P0 | Device 行点击远程连接 | 浏览器预开新标签避免弹窗拦截；连接中、已连接、挂断和错误状态清晰，目标 serial 正确 |
| AT-WEB-016 | P0 | 点击挂断或关闭远控标签页 | 调用同一结束语义，Reservation/STF claim 释放，设备重建后恢复可用；重复结束幂等 |
| AT-WEB-017 | P0 | viewer/operator 或非 ready/healthy Device 发起远控 | 页面不提供入口，直接请求也返回 403/409，不创建预约 |
| AT-WEB-018 | P0 | 查看 Host 动态容量并切换 4 GB/8 GB 设备规格 | 页面按实际 CPU、内存、磁盘显示可新增台数；结果不同且阻断项给出具体缺口，不显示固定“一台上限” |
| AT-WEB-019 | P0 | Pool 目标提高到实际资源无法全部满足 | 保存真实目标，Controller 只创建可容纳数量并显示待扩容原因；不抬高或伪造 Host `device_slots` |
| AT-WEB-020 | P0 | 编辑空闲 Emulator 的 Image 和运行规格 | 二次确认清空数据；异步状态可刷新恢复；成功后 Device ID 不变且 Image/规格/Endpoint 更新 |
| AT-WEB-021 | P0 | 编辑有活动预约或资源不足的 Emulator | 服务端返回 409 和可解释原因，不删除旧 Provider 资源，不提前改写当前 Image/规格 |
| AT-WEB-022 | P0 | 独立 Console 登录后关闭并重开浏览器，且空闲超过旧 30 分钟窗口 | 30 天内复用 HttpOnly/SameSite Cookie 继续登录；浏览器不保存明文密码，主动退出、到期或服务端吊销后立即要求重新认证 |
| AT-WEB-023 | P0 | 使用正式管理员指定密码从 HTTPS 入口登录 | 登录返回 201、当前会话返回 200、Cookie 为 Secure/HttpOnly 且有效期 30 天、注销返回 200；不得以 Secret 文件存在或历史登录成功替代当前密码的真实验证 |

### 4.10 动态容量、镜像和重装

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-CAP-001 | P0 | Host 心跳 | 上报实际 CPU、总/可用内存、Docker 数据盘总/可用空间、KVM/GPU 能力和采集时间 |
| AT-CAP-002 | P0 | 同一 Host 估算 4 GB 与 8 GB 规格 | 分别按完整规格计算可新增数量，结果受 CPU、内存、磁盘中最小值约束 |
| AT-CAP-003 | P0 | 两个 Controller 同时扩容 | PostgreSQL 锁和在途命令预留保证资源不超分、不重复创建 |
| AT-CAP-004 | P0 | 心跳后磁盘或内存突然下降 | Agent 创建前实时预检拒绝，命令返回稳定资源不足码和具体缺口，不启动半配置 Emulator |
| AT-CAP-005 | P1 | 相同 digest 创建第二台 | 共享镜像层不重复计费；数据卷和可写层预留按设备重复计费 |
| AT-IMG-001 | P0 | 拉取 Android 13～16 | 固定 tag/digest 均可验证并完成 ADB、STF、Appium 冒烟，Android 16 为默认 |
| AT-IMG-002 | P0 | 未选择其他版本时提高 Pool 目标 | 新设备全部使用默认 Android 16，不因目录中有四个 Image 而每版常驻一台 |
| AT-RIM-001 | P0 | 空闲 Device 从 Android 16 重装为其他版本 | 同一 Device ID/Pool membership，旧数据清空，成功后原子更新 Image、规格和 Endpoint |
| AT-RIM-002 | P0 | 重装目标启动或健康失败 | 尝试恢复旧配置一次；恢复成功保持旧 Image，恢复失败隔离，所有结果可审计 |

## 5. 非功能指标

| 指标 | MVP 标准 |
|---|---|
| 双占 | 0 次 |
| 普通查询 API | 内网 E0、20 RPS 下 p95 ≤ 300 ms |
| 已有 warm 设备预约 | 正常依赖下 10 秒内 active |
| 过期回收 | grace period 后 60 秒内开始并完成可执行清理 |
| 状态收敛 | Server/Agent 重启或依赖恢复后 120 秒内 |
| 稳定性 | 8 小时或 50 次完整循环无永久悬挂和持续资源泄漏 |
| 数据隔离 | 上一任务测试数据检出率为 0 |
| 密钥泄露 | 响应、日志、报告和普通数据库字段中为 0 |
| migration | up/down/up 全通过 |
| 真机扩展 | Provider 接口编译级契约测试通过，不改上层模型 |
| 控制台首屏 | 正常内网下 p95 ≤ 3 秒，资源列表查询仍满足 API p95 标准 |
| 浏览器密钥泄露 | Service/Agent/STF Token、数据库 URL 和内部凭证为 0 |
| Web 状态一致性 | 刷新后不得依赖前端缓存产生与 Server 不一致的资源状态 |

若验收机器资源不足导致性能指标不可比，必须记录机器规格和实测基线；不得删除正确性、安全性和双占标准。

## 6. 验收证据

每项测试至少保存：

- 测试编号、环境、时间、版本 commit；
- 执行命令或自动化入口；
- 关键请求/响应，必须脱敏；
- 数据库状态或事件时间线；
- 容器/设备/预约清理结果；
- 通过/失败结论和问题编号。

证据目录：

```text
docs/evidence/
├─ DF-xxx/
└─ mvp-acceptance/
   ├─ environment.md
   ├─ results.md
   ├─ logs-sanitized/
   └─ artifacts/
```

## 7. 最终签收条件

设备农场 MVP 只有满足以下条件才能签收：

1. 所有 P0、P1 用例通过；
2. DF-000~DF-028 全部 completed；
3. Linux KVM、一台 Android 16 Emulator、STF、Appium 和 DaFit 冒烟真实通过；
4. 无双占、无永久悬挂、无跨任务数据残留、无密钥泄露；
5. OpenAPI、migration、部署、监控、故障处理和回滚文档齐全；
6. 新版 Alcor 团队可使用 Mock 契约包开发 Device Farm Adapter；
7. ALCOR-001 可以等待新版 Alcor 完成，不影响设备农场 MVP 独立签收。
8. 用户可以通过 Device Farm Console 完成设备查看、人工预约、续租/释放和受控设备操作，不需要使用命令行或直接访问内部服务；Console 不展示伪 STF Web 入口。
# DF-035 官方目录与按需准备补充

验收应在真实 Linux KVM Build Agent 上证明：官方稳定目录只能由 Server 同步；Console 没有外部下载或任意命令入口；一个选定的 System Image 依次经历下载/构建、验证、内部 Registry digest 锁定和可用登记；构建失败不会产生 `device_images`；第二次相同请求命中缓存；官方目录变更可见但不自动替换已验证成品。CPU、内存、数据盘、分辨率、DPI、图形模式与品牌硬件预设仅改变 runtime profile，不改变官方 System Image 选择。

# DF-036 镜像生命周期补充

验收应证明：停用仍被 Pool 默认值或活动 Device 引用的 Image 必须返回 409；仅有 `deleted` 历史 Device 引用时可停用并保留历史查询和审计，默认可用列表不返回它。管理员可从 Image 页面选择任意 `ready` Image 作为指定 Pool 默认值；服务端原子启用 Pool Image 关系并切换默认值，拒绝非 `ready` Image，且不修改已有 Device 的 Image。

# DF-037 Phone 创建向导补充

验收应证明：Phone 模板列表至少可搜索、显示多条 SDK Profile 和屏幕参数，且不出现 Tablet、Wear、TV、Automotive、Desktop、XR；系统镜像选择使用 Server 的官方目录，旧的 API 33～36 固定正则不再限制目录。选择已验证镜像并提交完整 runtime profile 后，Server 在事务中创建 provisioning Device、Pool membership 和 `create` Host Command，并增加 Pool `total_target`；容量不足、镜像未就绪、非法硬件模板均不能留下半条记录。Agent 实际收到的 Docker 环境必须使用选定 Phone Profile；真实 Linux KVM 环境需证明最终 ADB、STF、Appium 全部通过后设备才 ready/healthy。

# DF-038 长期设备和基础设备扩容补充

验收应证明：释放预约只关闭预约/STF 会话并把 healthy Device 返回 `ready`，不发 rebuild、不删除数据卷，已安装 APK、应用数据、帐号和文件仍在；显式 rebuild/reimage 仍清空数据。创建向导可选择目录中的任一 Phone Android 版本，提交 `catalog_id` 后由持久化 provisioning job 完成已缓存直接创建或未缓存受控准备、验证与自动继续；刷新和同一幂等键重试只能看到同一 job，不能重复下载、创建或增加 Pool 目标，失败必须显示明确阶段。每个 Pool 可在控制台选择基础设备；扩容使用该设备的 Image、Phone 模板和有效 runtime profile，但新实例使用全新数据卷，绝不复制用户数据；非 healthy Phone、非本 Pool 设备及未先切换替代基础设备时均不得设置/删除。空闲 ready Device 可直接删除，删除命令和 Pool `total_target`/`min_ready`/`max_concurrency` 的下调必须原子生效，控制器不得自动补回；reserved、busy、recycling 或带活动预约的 Device 必须拒绝删除。真实 Linux KVM 验收需保存释放前后 APK/数据校验、基础设备扩容命令 payload、删除后 Pool 目标及无补建证据。

# DF-039～DF-046 第二版 iOS 补充

DF-039 只签收设计和真实环境缺口盘点，不用本机 Windows Appium 或 Mock 冒充 iOS 可运行。DF-040～DF-046 必须逐项使用 `docs/09_ios_device_farm_v2_acceptance.md`；只有 E4 Simulator、E5 真机、E6 发布回归对应 Gate 通过后，才能分别宣称控制面、Simulator、真机和第二版整体完成。

DF-053 额外要求：Pool 目标为 2 且一台受管 iOS Simulator 隔离、停止、不健康或从成功的完整 inventory 持续消失时，该设备不得继续满足可服务目标；无活动占用时只排队一个幂等删除命令，删除成功后保持 Pool 目标并自动补建。删除失败不得盲目创建第三台或形成命令/事件风暴，inventory 请求失败不得被解释成全部设备消失。详细用例见 `docs/09_ios_device_farm_v2_acceptance.md` 的 AT-IOS-SIM-016～019。

# DF-056 长期设备非破坏恢复与正式数据清理补充

ADR-0029 取代 DF-053 的自动删除补建验收口径。验收必须证明：系统产生的 Android Agent/STF 隔离可由真实心跳/可见性恢复原 Device；持续故障最多自动 restart 一次，且 restart 前后 Device ID、Provider ref、Pool membership、已安装 App/账号/缓存/文件不变。人工隔离不得自动解除；任何健康隔离都不得产生 delete/rebuild/reimage 或替代 create。iOS inventory 消失或故障只形成容量缺口和告警，不自动删除 CoreSimulator。清理正式库前必须生成可恢复备份并记录保留 ID；清理后 Reservation、Session、健康事件、设备审计、Host Command、provisioning job、幂等/Console Session 和 deleted Device 为零，当前 Host、Pool、Image 与三台 Device 保留并逐台完成真实 Appium Session 和 `/source`。

# DF-057 全仓不可达代码与旧策略清理补充

DF-057 必须同时使用生产入口、测试入口、部署/运维引用和静态调用图判断删除，不得把独立 CLI、迁移、E2E fixture、OpenAPI 生成代码或历史证据误判为死代码。验收保存删除项与保留项清单，并证明 Go deadcode 零结果、Staticcheck 无有效问题、TypeScript 未使用检查零结果、生成代码无漂移和全量测试/构建通过。删除 DF-053 旧完成分支后必须回归管理员显式缩容成功/失败、iOS 故障不自动删除、Android 原机恢复和三台真实设备会话；正式库最终继续为零历史记录。

# DF-058 独立控制台安全保持登录补充

DF-058 必须证明新会话的数据库绝对期限、Cookie `Expires/Max-Age` 和空闲期限均为 30 天，旧的 30 分钟空闲窗口不再导致频繁重登；浏览器重开后仍能读取当前会话，注销后原 Cookie 无法认证。检查 localStorage、sessionStorage、构建产物、日志和 Git diff 均不得出现明文密码。正式部署必须同步重建依附 Server 网络命名空间的 iOS 隧道，并在清理回滚资源前完成 Android 与两台 iOS 的真实 `/source` 回归。

# DF-059 正式管理员密码修复补充

密码修复验收必须使用正式 HTTPS 入口和管理员当前指定的密码走完整登录链路，不能只校验 Argon2id 格式、配置加载或旧 Session。连续错误登录触发进程内限流后，受控重启 Server 清除限流时必须同步重建 iOS 隧道；成功验证后删除临时哈希工具、临时 Secret 文件、Console Session 和审计记录，保持设备域正式历史为零。
