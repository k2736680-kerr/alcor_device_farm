# 现有能力复用矩阵

本文件是编码前的强制检查表。目标是只建设缺失的设备域能力，不产生第二套 Appium、STF、评估任务、测试执行或报告实现。

2026-08-05 前端复用盘点：当前仓库没有前端工程；本地旧版 Alcor `master` 没有已跟踪的前端源码，本地 `alcor_console` 只有未跟踪构建产物和依赖目录；DaFit 没有设备管理 Web 后台。因此没有可直接复用的控制后台源码，但 React + TypeScript 技术方向和现有设备 OpenAPI 可以复用。

## 1. 直接复用，不允许重写

| 能力 | 现有来源 | 本项目使用方式 | 禁止事项 |
|---|---|---|---|
| 浏览器远程看屏、操作和临时 APK 安装 | DeviceFarmer/STF | `adapters/stf` 调用 STF 页面和 API；设备农场只把预约设备的明确 `stf_serial` 交给原生单设备页 | 不开发第二套远控页面、画面流、触控或 APK 安装协议，不允许回退到第一台设备 |
| STF设备Inventory | DeviceFarmer/STF | 读取并映射 serial、present、ready、using | 不复制STF设备库作为业务真相 |
| STF claim/release/remoteConnect | DeviceFarmer/STF REST API | Adapter封装并增加超时、重试和错误分类 | 不重新实现相同设备控制协议 |
| Android UI自动化协议 | Appium 2 + UiAutomator2 | 使用现有服务和Driver | 不自研WebDriver协议或UiAutomator2 Server |
| iOS UI 自动化协议 | Appium 3 + XCUITest Driver + WebDriverAgent | 使用固定上游版本和明确 UDID；设备农场只管理宿主机连接与健康 | 不自研 WebDriver、XCTest、WDA、页面动作或断言 |
| iOS 宿主机设备发现与自动化 Session 路由 | Appium Device Farm 12.0.1、Appium XCUITest Driver、WebDriverAgent | Host Adapter 复用 inventory、技术 busy、明确 UDID 路由和自动化 Session Fence | 不使用插件 Dashboard 建第二套 Pool/预约；不把已由上游删除的人工串流重新包装为产品能力 |
| iOS Simulator 浏览器远控和临时 App 安装 | Baguette 0.1.92 原生 Web UI、画面流、Host HID 与明确 UDID 的 `/files` 安装 | `adapters/baguette` 只做固定版本健康、目标 UDID、每设备独立签名会话和 Reservation 鉴权；页面、输入和 `simctl install <udid>` 完全复用上游 | 不自研或保留第二套 HTML/画面流/触控/IPA 安装协议；不让 Baguette 接管 Pool、预约、租约或审计，不使用 `booted` 模糊目标 |
| Android Emulator容器基础 | Google Android Emulator Container Scripts与Android SDK | 固定上游版本并制作内部不可变镜像 | 不从零编写Emulator实现 |
| 数据库事务与唯一约束 | PostgreSQL | 预约、租约、状态和命令使用PostgreSQL | 不用内存锁代替数据库并发控制 |

## 2. 复用 DaFit 已有实现，不迁入本项目

| 能力 | 已有代码 | 本项目职责 |
|---|---|---|
| ADB命令封装 | `dafit_auto_platform/core/adb` | 不复制；Harness只传入明确UDID，设备Agent仅做基础设施级发现和健康命令 |
| Appium Session | `core/driver/appium_session.py` | 不创建业务WebDriver Session；只提供可访问的Appium Endpoint |
| App生命周期 | `core/driver/app_lifecycle.py` | 不重写；由DaFit Runner处理DaFit应用生命周期 |
| 元素定位和等待 | `core/element` | 不实现 |
| 点击、滑动、返回和前台恢复 | `core/actions` | 不实现 |
| 通用断言 | `core/assertions` | 不实现 |
| 截图、XML和证据策略 | `core/evidence` | 不实现；只保管Harness返回的制品路径 |
| Case/Run结果 | `core/results` | 联调阶段读取现有`report.json`，不建立第二套业务结果模型 |
| 用例目录、计划和执行 | `runner` | 通过现有入口执行，不复制Planner/Executor |
| HTML/JSON报告 | `reporting` | 直接保留为联调Artifact，不开发新报告生成器 |
| 正式完整入口 | `tools/run_full.py` | Farm适配分支仍调用同一入口，或增加同层薄入口 |

DaFit项目后续只增加Farm运行适配，不改变上述职责：外部指定UDID、Appium Endpoint和报告目录，禁止自动挑选其他设备。

## 3. 对接新版 Alcor，不提前建设

本地 `Alcor` master 是旧版代码事实；附件中的新版方案是正在开发的目标。旧表和旧接口只作为迁移来源，不再作为设备农场新接口的依赖。

| 能力 | 新版 Alcor 目标来源 | 当前处理 |
|---|---|---|
| 用例和数据集 | `cases`、`datasets`、`dataset_snapshots`、ClickHouse `dataset_case_rows` | 本项目不建表、不建 API |
| 运行与重试 | `runs`、`run_attempts`、`POST /api/v1/runs/:id/attempts` | Alcor 自动执行使用 `owner_type=run_attempt` 和 UUID/ULID `owner_id`；另允许受控 `manual/test_run` 预约 |
| 运行结果与指标 | PostgreSQL `run_results`、ClickHouse `run_case_results/run_target_metrics` | 本项目不计算、不保存业务结果和用例级指标 |
| 基础设施运行指标 | Alcor 继续使用自身可观测平台；设备农场只暴露 Prometheus `/metrics` | 仅包含 Server、数据库、设备、Agent、Reservation 和 Host Command 状态，不复制 Run/Result 业务指标 |
| 业务制品 | PostgreSQL `artifacts` 索引 + Supabase Storage | 正式接入由 Worker 上传；设备农场不保存业务报告 |
| 业务执行队列 | 独立 Worker + PostgreSQL 租约 | 设备农场不建立第二套 Run 队列；只管理设备 Reservation 租约 |
| Device Farm 接入 | Worker 的 `Device Farm Adapter（后续）` | 当前固化北向设备契约和 Mock；等待新版 RunAttempt API 后联调 |
| 用户、权限、审计 | 钉钉登录、`users`、`audit_logs`、Eval Console | 不预建 Alcor 用户模型；Device Farm Console 只实现设备域浏览器访问保护和设备技术审计，未来可接公司身份或由 Alcor 透传操作者 |
| Target、Config、Secret | `targets`、`configs/config_versions`、受限 YAML | 本项目不接收业务密钥，不复制 Target/Config 管理 |
| 统一响应和关联标识 | `/api/v1` 的 `request_id/data/error`，`X-Eval-Run-Id`、`X-Eval-Attempt-Id`、`traceparent` | 北向 API 兼容统一响应并透传关联标识 |
| 旧版历史对象 | `test_items`、`eval_tasks`、`eval_results`、本地 `/tasks` | 仅由新版迁移 CLI 处理；设备农场禁止依赖 |

## 4. 允许新建的设备域能力

这些能力在Alcor和DaFit当前代码中都不存在，是本项目的有效开发范围：

- Device Host与Host Agent协议；
-统一Device模型和状态机；
-Docker Emulator Provider；
-USB设备基础设施Provider；
-Device Image、每 Image 运行镜像选择与不可变摘要验证；
-Device Pool与容量；
-固定目标容量的控制台单点配置、自动扩容和安全缩容；
-隔离或已停止设备的受控人工删除；删除继续复用既有 Host Command、Host Agent 和 Provider `Delete`，不新增 Server 直连 Docker 的通道；
-Reservation、Lease、续租和释放；
-Scheduler和数据库并发锁；
-Reconciler和Reaper；
-设备健康事件、隔离、恢复和重建；
-STF Adapter，包括官方 REST API 封装和 Host Agent 将动态 ADB Endpoint 注册到同机 STF ADB server；
-Appium Endpoint/端口/健康管理Adapter；
-平台中立的 Host/Pool/Device/连接与健康模型；Android 旧数据原位回填，禁止复制一套 iOS 表；
-macOS Host Agent 运行适配、固定版本工具链盘点和 iOS inventory/health Adapter；
-Reservation 绑定的 iOS Session Fence：只校验 active Reservation、固定 Endpoint 和单一 UDID，并透明转发上游 Appium 协议，不实现 WebDriver 命令；
-iOS Simulator 的动态创建、启动、停止、擦除重建、删除、设备域登记、Pool、预约、故障收敛和审计；实际虚拟化完全复用 Xcode CoreSimulator，编排按 ADR-0024 实施；
-iOS Pool 的固定目标自动扩缩容；扩容复用已选扩容模板 Simulator 的 Host、Runtime 与 iPhone Device Type，通过既有 CoreSimulator 创建链路生成全新设备，缩容复用既有 Host Command、Agent 和 Provider `Delete` 安全删除空闲 Simulator；
-受管 iOS Simulator 的自动故障淘汰与目标补建；复用既有隔离判定、Host Command、Agent、CoreSimulator `delete/create`、Pool 目标和审计，不新建虚拟化、Session 或业务执行实现；删除失败时保留隔离并停止盲目补建；
-面向未来Alcor的设备北向API；
-Device Farm Console，只展示和操作设备域资源；
-浏览器安全访问、页面权限和设备域操作审计衔接；
-管理员 STF Web 远控编排：精确设备短租约、短时 JWT 入口、心跳和关闭回收；只链接 STF 原生页面，不实现画面或触控；
-管理员 Baguette Web 远控编排：精确 iOS Simulator 短租约、短时签名入口、独立 Gateway、心跳和关闭回收；只代理目标 UDID 的 Baguette 原生页面，不实现画面或触控；
-多设备远控安装目标绑定：Android 原生 STF 页面固定使用预约 Device 的 `stf_serial`，iOS Baguette Gateway 为每个 UDID 使用独立会话并只代理同一 UDID 的上传路径；安装实现仍由 STF/Baguette 上游提供；
-Alcor 统一设备入口：只允许 Alcor 服务端持有 Device Farm Service Token，钉钉用户经同源代理访问既有 Console；代理透传受控操作者 ID，浏览器不得获得 Service Token、STF Token 或设备内部端口；
-Host 资源探测、设备运行规格校验和动态容量预检；复用 Docker/KVM/Android Emulator 的限制参数，不另建虚拟化层；
-官方 Android System Image 目录同步、按需镜像准备、不可变 digest 验证和内部缓存；继续复用 Android SDK `sdkmanager`/`avdmanager`、既有 Image、Host Command 和 Docker Provider；
-已验证 Device Image 的受控停用、默认隐藏和设备池默认镜像选择；复用既有 Image 状态机、Pool Image 关系、设备域审计和 Console，不物理删除历史 Device/Image，也不新增镜像仓库实现；
-Phone 硬件模板目录和受控创建向导；硬件模板只描述 Android SDK `avdmanager` 可识别的 Phone Profile，创建仍复用既有 Host Command、Host Agent、Docker Emulator Provider、容量预检和 ADB/STF/Appium 健康链路；
-长期设备保留、Pool 基础设备和直接删除；复用现有 Reservation/STF release、Device 状态机、Warm Pool、Host Command 与 Docker Provider，不新增业务设备快照、Appium 执行或 Docker 直连；
-仅用于端到端证明的DaFit Harness。

## 5. 名称相近但职责不同的能力

| 本项目能力 | 看起来相似的现有能力 | 不属于重复实现的原因 |
|---|---|---|
| Agent ADB发现与健康检查 | DaFit `core/adb` | Agent需要跨设备Inventory和宿主机健康；DaFit ADB只操作已经选定的单台业务设备。Agent只实现必要的`devices/getprop/shell-ready`白名单，不实现App业务动作 |
| Appium Adapter健康检查 | DaFit Appium Session | Adapter只确认服务可用、端口隔离和Endpoint，不执行页面步骤；WebDriver仍由DaFit或未来Alcor Executor创建 |
| PostgreSQL Reservation | STF claim | Reservation是跨进程业务占用真相和租约；STF claim是远控工具的技术占用。顺序固定为数据库预约成功后调用STF claim |
| Device Session | 新版 Alcor RunAttempt | Device Session只描述一次设备占用与技术连接；RunAttempt负责整个业务执行和结果。两者通过外部 UUID/ULID 关联，不互相替代 |
| Device Farm Console | 新版 Alcor Eval Console | 前者只控制设备资源并可独立运行；后者负责完整评估业务。Alcor 通过同源受控代理嵌入现有 Console，复用钉钉会话并以 Service Token 调用同一设备 API；不复制页面、设备状态或 Alcor 业务对象 |
| STF 原生 Web 远控 | STF 原生 Web 页面 | 按 ADR-0013 由 Console 编排短租约和短时 Web 登录后打开 STF 原生单设备页；不展示 `remoteConnect` TCP 地址，也不实现画面流、触控、日志或文件协议 |
| 动态容量预检 | Docker/cgroup 与 Host 操作系统资源 | Docker 和操作系统只提供事实；设备农场根据已登记设备、在途命令和每台有效规格做调度预留，不复制容器运行时 |
| Emulator 运行规格和重装 | Android Emulator/Docker 参数 | Console 只保存、校验并编排 CPU、内存、分辨率、GPU 与镜像选择；实际创建、删除和启动仍由既有 Host Agent/Provider 完成 |

## 6. 开发审查规则

### 第二版 iOS 能力的批准边界

DF-039 已按 ADR-0021 完成职责和验收设计。后续只能按 DF-040～DF-047 的顺序实现上面列出的设备域能力，不得把“允许新建”解释为可以直接建设 iOS 业务执行器：

- PostgreSQL Reservation、Scheduler、Pool、Lease、Reaper 和审计继续是唯一设备占用真相；
- Appium Device Farm 不得再次自由选择我方已经预约的设备，Session 必须同时使用与 active Reservation 相同的单值字符串 `df:udids=<reserved_udid>` 和 `appium:udid=<reserved_udid>`，并经过 Session Fence；
- Android 继续复用 STF 原生远控；iOS 按 ADR-0026 复用 Baguette 原生 Web UI 和 Host HID，Appium Device Farm Dashboard 仍不是远控页面；
- iOS 自动化继续复用 Appium XCUITest/WebDriverAgent，不在本仓库重写 WebDriver、WDA 或业务用例执行器；
- iOS App、Build、Case、Run、结果和 Artifact 仍属于 Alcor/对应执行器，不进入设备域；
- 首期只允许专用 macOS Host；Windows/Linux iOS 真机、tvOS、无线设备、跨 Host Appium Hub 和 Runtime 自动下载必须另行验收；
- iOS Appium Node Endpoint 只能由受信 Session Fence/Worker 网络访问，浏览器不得访问插件 Dashboard、Endpoint 或 Session Grant。

每个新增模块必须在代码评审中回答：

1. 新版 Alcor 方案是否已经定义同一业务能力？
2. DaFit是否已经有同一执行能力？
3. STF/Appium/Android SDK是否已经提供？
4. 能否通过Adapter调用而不是复制？
5. 若必须新增，它是否属于第4节允许的设备域？
6. 是否误用了旧版 `test_items/eval_tasks/eval_results` 或本地报告路径？
7. 若属于控制台功能，是否只操作设备域资源，且浏览器没有接触 Service/STF Token 或内部基础设施端口？

自动缩容继续复用现有 Host Command、Host Agent 和 Provider `Delete`，不得另建直接访问 Docker Socket 的 Server 删除通道。Device 数据只标记 `deleted` 并保留审计，不通过物理删库伪装缩容完成。

DF-030 的人工删除同样复用上述删除链路，只允许 `quarantined/stopped` 且没有活动预约的设备进入删除命令；可用和使用中的设备必须通过目标容量或预约释放流程处理。

DF-031 复用 STF 3.7.9 原生 Web UI、JWT 登录、claim/release、现有 Reservation/Reaper 和 rebuild 链路。新增代码只负责管理员精确选中 Device、签发短时 Web 入口、心跳和结束编排；不得把 STF `remoteConnect` TCP 地址或管理 Token 交给浏览器。

DF-032～DF-035 复用 Docker 的资源限制、镜像缓存、KVM、Android Emulator 启动参数以及 Android SDK 官方稳定频道的 `sdkmanager`/`avdmanager`；Server 不访问 Docker Socket 或 Google，Console 不执行宿主机命令、不接受任意下载地址或命令。目录同步、构建、推送和验证由受控 Build Agent 的异步 Host Command 执行；只有完成验证并锁定 digest 的成品才进入 `device_images`。CPU、内存、数据盘、分辨率、DPI 和图形模式继续是 `runtime_profile`，品牌只可作为硬件预设，绝不能表述为官方 System Image 属性。

DF-036 复用现有 `disabled` Image 状态、Pool 默认镜像、Pool Image 关系和设备域审计。停用只退出可选范围并保留历史外键；选择默认镜像只影响后续自动补建，不隐式重装已有设备，不直接删除 Registry/Docker 内容。

DF-037 复用 Android SDK/`avdmanager` 的 Phone Profile 命名、既有官方 System Image 目录和 Image 准备任务。新增的向导与受控创建接口只保存设备域的 Pool、硬件 Profile、系统镜像和 runtime profile；Server 在事务中登记 `create` Host Command，浏览器、Server 均不直连 Docker、SDK 或 Google。TV、Wear、Automotive、Desktop、XR 等 Profile 不进入首期接口或 Console。

DF-038 复用 Reservation 的 STF release、既有 System Image 准备、Device/Pool PostgreSQL 锁、Host Command、Host Agent 和 Docker Provider。新增 `device_provisioning_jobs` 仅持久化编排状态，绝不下载镜像或直接操作 Docker；目录项未缓存时复用既有准备/验证链路，完成后再调用既有创建链路。release 后不再排队 recycle rebuild，直接回到 ready 并保留数据卷；显式 rebuild/reimage 继续使用既有清空链路。基础设备只复制已登记的 Image、Phone Profile 和 runtime profile 来创建干净新实例，绝不复制 App 数据。直接删除仍由既有 delete Host Command 清理容器/网络/卷，并在同一事务收缩所属 Pool 目标。

DF-039 的自动化复用结论固定为 Appium 3.6.0、Appium Device Farm 12.0.1、XCUITest Driver 12.4.0 和 go-ios 1.3.2 的宿主机 Adapter 方案。插件内部 busy 只是技术互斥，出现与 PostgreSQL Reservation 不一致时必须隔离收敛；共享 Appium Endpoint 不代表共享 UDID。人工远控由 ADR-0026 改为固定版本 Baguette 原生 Web UI；不复用会暴露整台宿主机的 VNC，也不恢复已被 Device Farm 12.x 删除的 WDA 串流页面。

ADR-0024 进一步确认：动态虚拟 iPhone 必须复用 Xcode CoreSimulator。允许新建的是目录校验、Host Command 编排、幂等身份和状态收敛，不是自研 iOS 虚拟机。Runtime/Device Type 必须来自 Host 上报与部署 allowlist 的交集；Server、Console 和调用方均不能提交任意 `simctl` 参数。

无法回答或没有更新本矩阵时，不进入编码。
