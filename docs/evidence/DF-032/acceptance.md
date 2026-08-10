# DF-032 动态 Host 容量和设备运行规格验收证据

验收日期：2026-08-10

## 结论

DF-032 通过。设备数量不再由测试环境的一台或 Agent 命令并发写死，而是按待创建设备的有效运行规格、Host 实时 CPU/内存/Docker 数据盘和已有/在途资源占用动态计算。资源不足时不登记一个注定启动失败的新 Device，并返回稳定错误码 `INSUFFICIENT_HOST_RESOURCES`、限制资源和具体欠缺量。

本步骤只完成动态资源底座。Pool 总目标/默认镜像、单设备编辑重装以及 Android 13～16 镜像目录分别按 DF-033、DF-034、DF-035 实施，不能把后续页面入口伪装成本步骤已完成。

## 实现检查

- 新增 `runtimeprofile` 强类型规格：容器 CPU/内存、Guest CPU/内存、数据盘、镜像盘、分辨率、DPI、VM heap 和 GPU 模式统一解析、补默认值和校验；旧 `cpu`、`memory_mb` 字段保留兼容读取。
- Linux Host Agent 从 `runtime.NumCPU`、`/proc/meminfo` 的 `MemAvailable` 和 Docker 数据根目录文件系统采集实际容量；可用 render node 只作为能力上报，不在未验证设备映射时宣称已启用 Host GPU。
- Server 在 Warm Pool 行锁内累计现有 Device 和 pending/leased 创建、验证命令的规格，扣除系统保留量后再选择 Host；旧 Agent 仍沿用 slot 逻辑，支持 Server/Agent 滚动升级。
- Agent 在真正执行 create 前使用最新本机容量和已发现 Provider 再预检一次，覆盖心跳到执行之间资源下降的窗口。
- 相同镜像 digest 已缓存时不重复扣共享镜像层；数据盘逐台扣减。
- Docker Provider 把有效规格落实为容器 CPU/内存限制和 Emulator cores、memory、GPU、skin、DPI/data partition 参数，并把规格写入受管标签供 discover/rebuild 恢复。
- Pool 目标不再反向提高 Host `device_slots`。`DEVICE_FARM_AGENT_DEVICE_SLOT_LIMIT=0` 表示不设置固定台数上限，非零值仅作为运维安全上限；`DEVICE_FARM_AGENT_CONCURRENCY` 只控制命令并发。
- Console Host 页面展示总量和实际可用 CPU、内存、磁盘，并明确提示容量按所选规格和实际剩余资源计算。

## 自动化验收

在 Windows 工作区完成 Console 验收：

- `pnpm test --run`：7 个测试文件、26 项测试全部通过；包含远控生命周期回归测试。
- `pnpm build`：OpenAPI 客户端重新生成，TypeScript 编译和 Vite 生产构建通过；仅保留已有的大 bundle 警告。

Go 验收在 Linux Docker `golang:1.24.6-alpine3.22` 中执行，使用隔离 PostgreSQL 16，避免 Windows 环境缺少 Go/KVM 造成假通过：

- `go test -p 1 ./...`：全部通过；串行执行防止多个集成测试包同时清空同一个隔离测试库。
- `go vet ./...`：全部通过。
- `gofmt -l cmd internal`：无输出。
- 容量单元测试覆盖 4 GiB 规格可容纳 3 台、8 GiB 规格只可容纳 1 台的同 Host 对比，CPU/内存/磁盘限制项与欠缺量，以及镜像层只扣一次、数据盘逐台扣减。
- Warm Pool PostgreSQL 集成测试验证待创建设备携带完整 `runtime_profile`，两个不同规格得到不同创建数量，Controller 不靠固定台数扩容。
- Host heartbeat 集成测试验证 `dynamic_v1` 心跳替换实测容量和实际使用量；旧心跳继续兼容原 slot 语义。

## 真实测试服务器验收

目标 Host：`10.0.30.171`。

Agent 实测心跳（采样会随系统负载变化）：

| 项目 | 实测值 |
|---|---:|
| 逻辑 CPU | 12 核 |
| 总内存 | 15592 MB |
| 当时可用内存 | 约 8103 MB |
| Docker 数据盘总量 | 100220 MB |
| 当时可用磁盘 | 约 25837 MB |
| 已有设备占用 | 4 CPU、5120 MB 内存、4096 MB 数据盘、1 台 |
| KVM / render node | 可用 / 检测到 `/dev/dri/renderD128` |

按默认保留 1 CPU、2048 MB 内存和 4096 MB 磁盘计算，当时再增加一台同类 4 GiB Guest（容器预留 5120 MB）可以通过；增加一台 8 GiB Guest 不能通过，限制项为可用内存。这个结果来自实时资源和规格，不是“一台上限”：迁移到内存、磁盘更大的 Host 后同一套代码会自然允许更多台。

已部署候选 Server 镜像 `alcor-device-farm:df032-20260810-dynamic-capacity-r3` 并保持健康；Host Agent 已切换到 DF-032 候选二进制。Console 生产资源包含 `dynamic_v1` 和动态容量提示。现有 Android 16 Emulator 容器的 `StartedAt` 保持 `2026-08-10T05:59:22.468686907Z`，本步骤没有为验证容量而删除或重建设备；最终状态收敛为 `ready/healthy`，STF/Appium 既有链路保留。

## 滚动升级故障与修正

首次先升级 Agent、后升级 Server 时，新 Agent 的严格 `runtime_profile` 心跳被旧 Server 拒绝，Host 短暂离线并使设备进入“宿主机不可用”隔离。已增加有边界的自动恢复：只有隔离原因精确为临时 Host 不可用、Provider 已重新健康且无在途操作时，心跳才沿合法状态机恢复；有活动预约时恢复到 busy。人工隔离、STF 不可见和 Appium 故障均不会被自动解除。对应 PostgreSQL 集成测试已通过。

正式滚动顺序固定为先升级 Server 再升级 Agent；这项恢复用于处理短暂断连和旧部署遗留状态，不替代正确的发布顺序。

## 已知边界

- 当前 Host 页面展示实测资源和动态规则；对任意待选规格直接显示“还能建几台”的交互入口在 DF-033/DF-034 完成。
- `host` GPU 模式必须在镜像支持和容器 render node 映射完成真实验收后才能开放；当前检测能力不等于已经启用硬件加速。
- Android 13～16 的不可变镜像下载、摘要登记和真实启动验收属于 DF-035；本步骤没有用未验证镜像占满测试机磁盘。
