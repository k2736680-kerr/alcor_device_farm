# ADR-0014：按实际资源和设备规格动态计算容量

## 状态

已批准，按 DF-032～DF-035 分步实施。

## 背景

ADR-0011 为了让首台测试机可以从 Console 扩缩容，把 Pool Image 的目标数同步成 Host `device_slots` 高水位。这能避免 Agent 配置写死一台，但它仍只按“台数”判断：一台 4 GB 和一台 8 GB 模拟器被视为相同，Host 心跳也只上报已发现容器数。与此同时，Image 的 `resource_config` 虽已保存 CPU 和内存，Docker Provider 创建容器时仍使用 Agent 全局参数，后台配置没有真正生效。

测试环境当前只运行一台不代表代码只能创建一台。迁移到资源更大的 Host 后，允许创建多少台必须由设备规格和 Host 实际可分配 CPU、内存、磁盘共同决定；资源不足时要告诉管理员缺少什么，而不是依赖固定数字或让管理员登录 Host 修改环境变量。

Android 13、14、15、16 是可选择的镜像版本，不是四个必须常驻的独立设备池。默认使用 Android 16；需要其他版本时，管理员应能把一台没有预约的空闲 Emulator 受控重装为目标镜像和规格。

## 决策

### 1. 容量不是固定台数

- Host Agent 自动探测并在心跳中上报逻辑 CPU、总内存、当前可用内存、Docker 数据盘总量和可用量；`device_slots` 只保留为可选的运维安全上限，不能由 Pool 目标数反向抬高。
- Server 对每个待创建或重装的 Emulator 使用其有效运行规格做预检。有效规格来自 Image 默认值和 Device 覆盖值，字段至少包括容器 CPU、容器内存、Guest CPU、Guest 内存、数据盘预留、分辨率、DPI 和图形加速方式。
- CPU 和内存按已存在 Device 以及 pending/leased 创建类 Host Command 的规格预留，避免两个 Controller 同时看到同一份剩余资源。Host 实时可用内存同时作为保护线，防止数据库账面可用但操作系统已经承压。
- 磁盘分为共享镜像层和单设备独占数据。相同 digest 的镜像层只在 Host 尚未缓存时计一次；每台设备只重复计算数据卷和可写层预留。Agent 在真正创建前再执行一次本机预检，避免心跳到执行之间资源变化。
- Host 保留系统余量，默认至少预留 1 个逻辑 CPU、2048 MB 内存、4096 MB 磁盘；这些是调度保护线，不是单设备固定规格。可由 Host 容量策略调整，但普通用户不需要编辑 Agent 环境变量。
- Console 必须显示“按当前规格最多还能创建几台”和阻断原因，例如“内存还差 3072 MB”或“磁盘还差 6 GB”。估算是调度提示，最终创建仍以 Host Agent 执行时的实时检查为准。

### 2. 运行规格由设备域管理

- `device_images.resource_config` 继续作为 Image 默认运行规格的存储，不新增重复配置表；OpenAPI 和服务端把它收敛成明确字段并统一校验。
- Device 可以保存覆盖规格；未覆盖字段继承 Image。创建、重建和重装 Host Command 必须携带解析后的完整有效规格，Docker Provider 不再把 Agent 全局 CPU/内存作为每台设备的最终值。
- Docker 容器 CPU/内存限制与 Android Guest CPU/内存分别配置，避免把 4 GB 容器上限误当成 Guest 可全部使用的内存。Guest 必须给 Appium、STF agent 和容器侧进程留下余量。
- 图形加速支持 `host`、`software`、`auto`。只有 Agent 确认存在可用 render node 且镜像支持时才能选择 `host`；否则明确回退 software 并在 Console 展示，不静默假装已启用 GPU。

### 3. 镜像目录与单设备重装

- 镜像仓库预发布 Android 13/API 33、Android 14/API 34、Android 15/API 35、Android 16/API 36 的不可变 tag/digest；Image 记录下载地址、摘要和默认规格，Android 16 标记为 Pool 默认镜像。
- Pool 保存总目标、最小预热、最大并发和默认 Image。不得把每个 Android 版本的 `max_instances` 相加当成 Pool 必须常驻的总量。
- 管理员只可编辑没有 pending/active Reservation、没有其他操作命令且处于 `ready/stopped/quarantined` 的 Emulator。重装异步执行：删除旧 Provider 资源和数据卷，按目标 Image/规格创建，验证 ADB、启动、STF、Appium 后继续使用同一个 Device ID。
- 重装成功前数据库中的当前 Image/规格保持旧值；成功后原子切换。失败时尝试恢复旧 Image/规格一次，恢复失败则隔离，并保留完整审计和错误原因。
- 自动补建设备使用 Pool 默认 Android 16；Android 13～15 仅在管理员明确选择或未来 Reservation 能力明确要求时创建，不为每个版本维持一台常驻设备。

## 覆盖关系

本 ADR 覆盖 ADR-0011 中“Server 把目标数写入 Host `device_slots` 高水位”和“Pool Image 固定容量相加”的决定；保留其通过 Console 调整容量、自动安全缩容、占用设备不强删和全部操作审计的要求。

本 ADR 扩展 ADR-0007 的每镜像运行选择：选择的不只是 Docker 引用，还包括经过校验的有效运行规格。它不改变 STF、Appium、DaFit 和新版 Alcor 的既有边界。

## 分步实施

- DF-032：运行规格模型、Host 实际容量心跳、Server/Agent 双重预检和可解释容量结果。
- DF-033：Pool 总目标、最小预热、最大并发和默认 Image，移除按 Image 数量相加语义。
- DF-034：Console 容量提示、Device 编辑规格和异步重装/失败恢复。
- DF-035：发布并登记 Android 13～16 镜像，默认 Android 16，完成多规格真实容量验收。

## 后果与限制

- CPU 是可压缩资源，动态计算采用可配置超配系数，但测试环境初始为 1.0；内存和磁盘默认不超配。
- Docker 镜像层大小、数据卷增长和 Android 首次启动存在波动，容量提示必须展示预留和估算口径，不能承诺精确到 MB。
- 在线修改运行规格需要重装并清空该 Device 的模拟器数据；Console 必须二次确认，不能伪装成无损热更新。
- 当前单 Host 实施也必须使用通用资源模型；多 Host 调度只是在同一预检上选择合适 Host，不需要重写规格和容量语义。
