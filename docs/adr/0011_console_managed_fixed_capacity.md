# ADR-0011：控制台统一管理固定设备容量并自动安全缩容

## 状态

已确定。

## 背景

ADR-0006 实现了按 `device_pool_images.min_ready/max_instances` 自动向上补齐 Emulator，但明确不自动缩容；ADR-0008 的当前部署还要求分别修改 Host `device_slots`、Pool `max_concurrency` 和 Image 目标。实际使用中，管理员在控制台设置目标数量后仍需登录 Linux Host 修改 Agent 配置，降低目标也不会删除多余实例，无法形成可独立使用的控制闭环。

设备农场已经具备 PostgreSQL 行锁、Host Command、Agent `delete`、Docker 容器/网络/卷幂等清理、设备状态机和审计能力，不需要新增第二套执行或基础设施控制协议。

## 决策

- 控制台以每个 Pool Image 的“目标设备数”作为管理员唯一需要设置的固定容量；保存时 Server 将 `min_ready=max_instances=target_instances`，并按所有启用 Image 目标之和同步 Pool `max_concurrency`；
- Server 同时把可承载 Docker Emulator 的 Host `capacity.device_slots` 提升到满足目标所需的容量高水位。Agent 的 `DEVICE_FARM_AGENT_CONCURRENCY` 只限制同时执行的 Provider Command 数量，不再要求管理员为设备目标逐次修改或重启 Agent；
- Host 心跳继续上报真实 `used_capacity` 和非槽位能力，但不得把 Server 已提升的 `capacity.device_slots` 覆盖回命令并发数；
- Controller 在目标增加时继续复用现有 PostgreSQL 行锁和 create Host Command 自动补齐；
- Controller 在目标降低时计算超额实例，按 `created_at ASC` 选择最旧的可删除 Emulator，保留最新实例；优先范围只包括没有 active Reservation、没有在途 create/rebuild/delete Command、且不被其他 Pool 使用的设备；
- 缩容不会中断 `reserved/busy/recycling` 设备。无法立即删除时保持“等待缩容”，设备释放并恢复到可删除状态后继续收敛；
- 选中的 ready 设备先退出 Pool membership 并进入不可调度的 stopped 状态，再创建持久化 delete Host Command；Agent 幂等删除容器、网络和数据卷；成功后 Device 标记为 `deleted`，数据库历史和审计记录不做物理删除；
- delete 失败沿用 Host Command 的三次重试。最终失败时 Device 进入 `quarantined/unhealthy`，保持不可调度并写健康事件，不通过创建替代设备掩盖仍占用的 Host 资源；
- 调整目标和每台自动删除都写设备域审计。降低目标必须填写原因；控制台必须二次确认并说明占用中的设备不会被强制删除；
- 多 Controller 通过锁定 `device_pool_images`、Device 和 membership，保证同一超额设备只生成一条有效删除命令，不出现重复删除或扩缩容振荡；
- 当前真实环境默认目标仍为 1。ADR-0008 的单 Emulator 验收结论不因本 ADR 自动扩大；设置 2 台或更多前仍应核对 Host 资源，真实多设备能力按 DF-029 验收。

## 覆盖关系

本 ADR 覆盖 ADR-0006 中“Controller 只自动向上补齐”和“降低目标必须人工删除”的决策，也覆盖 ADR-0008 中每次扩容都需人工同步 Host `device_slots`、Pool `max_concurrency` 和 Image 目标的操作方式。ADR-0008 的当前默认数量、单机资源基线和真实验收规模继续有效。

## 后果

- 管理员只在 Device Farm Console 设置一次目标设备数，不再登录服务器修改 Agent 环境文件；
- 扩容和缩容都通过既有 Server → PostgreSQL Host Command → Agent → Docker Provider 边界完成，Server 和浏览器仍不访问 Docker Socket；
- 数据库保留 `deleted` 设备历史，控制台容量摘要必须区分当前实例与历史记录；
- 自动缩容属于设备域能力，不引入 Alcor Run、Result、Artifact 或 DaFit 执行逻辑；
- DF-029 必须覆盖扩容、最旧空闲设备删除、占用保护、并发 Controller、删除失败和真实 Docker 清理证据。
