# ADR-0030：Android CPU 和内存无损改配

## 状态

已批准，按 DF-063 实施。

## 背景

ADR-0014 将 Android Emulator 的镜像、CPU、内存、数据盘和图形模式统一放入受控 `reimage`，因此管理员即使只调整 CPU 或内存，也必须删除数据卷并清空 APK、账号、缓存和文件。长期设备在 ADR-0018、ADR-0029 生效后已经要求日常释放和故障恢复保留数据；只改计算资源仍强制清空，与长期设备的使用方式不一致。

现有 Docker Provider 已有 `RestartWithProfile`：替换容器并保留确定性网络和数据卷，同时把新的容器 CPU/内存限制和 Android Emulator CPU/内存启动参数写入新容器。Alcor、DaFit、STF 和 Appium 都没有同职责的设备规格编排入口，因此本任务复用该 Provider、Host Command、容量预检、健康门禁和审计，不新增第二套执行或远控能力。

## 决策

- 新增 `POST /api/v1/devices/{id}/runtime-profile-updates`，只允许管理员提交容器 CPU、容器内存、Android CPU、Android 内存和操作原因；请求不能携带 Image、数据盘、分辨率、DPI、VM Heap、图形模式或任意 Docker/Emulator 参数。
- 只允许没有 pending/active Reservation、没有其他在途 Host Command，且处于 `ready/stopped/quarantined` 的 Docker Android Emulator 执行。
- Server 使用当前完整有效规格覆盖四个允许字段，校验 Guest 不超过容器资源，并按 Host 最新容量执行与 reimage 相同口径的替换预检。
- Server 复用持久化 `restart` Host Command，并以 `operation_kind=runtime_profile_update` 区分普通重启。Agent 执行前再次检查实时容量，调用现有 `RestartWithProfile` 替换容器并保留数据卷。
- 只有容器、ADB、Android boot、Appium 和 STF 全部健康后，Server 才原子提交 `runtime_profile_override`。目标启动失败时使用同一数据卷恢复旧规格一次；恢复成功则保留旧规格并记录失败，恢复也失败则隔离。
- Device 保存待应用规格、处理状态和最近错误，Console 刷新后仍能显示真实异步状态。该状态与破坏性 `reimage_status` 分开，不能把无损重启伪装成重装。
- Console 将原入口拆成“调整 CPU/内存”和“更换镜像/重建数据”。前者明确提示会短暂中断但保留数据；后者继续二次确认并明确清空 APK 和全部设备数据。
- Image、数据盘大小和图形模式继续只能走原有 `reimage`。混合修改不能自动降级为无损操作。

## 覆盖关系

本 ADR 只覆盖 ADR-0014“在线修改任何运行规格都需要重装并清空数据”的结论。ADR-0014 的动态容量、完整运行规格和破坏性 reimage 设计继续有效；ADR-0018、ADR-0029 的长期设备、数据保留和显式破坏性操作边界保持不变。

## 验收

- 自动化证明 API 只接受四个资源字段，拒绝忙碌设备、在途命令、非法规格和容量不足，并且幂等请求只产生一个命令。
- Agent/Provider 测试证明目标规格生效、数据卷未删除；目标失败可恢复旧规格，双重失败进入隔离。
- Console 测试证明纯 CPU/内存调整走无损接口，镜像或数据盘修改仍走带清空确认的 reimage。
- 在 `10.0.30.171` 的 Linux KVM Emulator 中写入 APK/应用数据/文件标记，执行真实改配后验证标记仍存在；同时核对 Docker CPU/内存限制、Emulator Guest 参数、ADB、STF 和 Appium。
