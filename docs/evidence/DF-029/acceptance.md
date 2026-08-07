# DF-029 控制台统一容量和自动安全缩容

## 当前结论

**completed**。后台单一目标设备数、Server 容量同步、Host 心跳解耦、Warm Pool 自动安全缩容、Console 交互和 PostgreSQL 并发/失败测试已经实现并通过本地自动化验证；真实 Linux KVM + Docker Emulator 已完成两轮 `1 → 2 → 1`，覆盖自动扩容、删除最旧实例、占用保护、释放后继续缩容，以及容器/网络/卷真实清理。2026-08-07 用户确认当前 16 GiB 宿主机以两台为验收和运行上限，真实 `3 → 1` 不作为本服务器的完成阻塞项；多设备删除语义由 PostgreSQL 17.10 集成测试覆盖。

## 已完成自动化验证

2026-08-07 使用 PostgreSQL 17.10 真实数据库执行：

```text
PASS TestControllerAdjustsToLargerConfiguredTargetWithoutCodeChanges
PASS TestControllerScaleDownDeletesOldestIdleDevicesAndKeepsNewest
PASS TestControllerScaleDownWaitsForOldestActiveDevice
PASS TestConcurrentControllersQueueOneDeletePerExcessDevice
PASS TestFailedScaleDownDeleteQuarantinesWithoutCreatingReplacement
PASS TestAgentHeartbeatClaimAndCompletionAPI
PASS TestManagementAPICompleteMockFlow
```

验证内容：

- 一个目标数同时写入 `min_ready=max_instances`；不相等的请求被拒绝；
- Pool `max_concurrency` 自动同步，Docker/Hybrid Host `device_slots` 高水位自动提升；
- Agent heartbeat 上报较小 `device_slots` 时不会覆盖 Server 管理值；
- 目标降低必须填写原因并写 `device_pool_image` 审计；
- `3 → 1` 只为最旧两台空闲设备生成 delete Host Command，最新设备保持 ready；
- 最旧设备 active 时不删除更新设备，释放后继续缩容；
- 两个 Controller 并发不会重复或超额创建删除命令；
- delete 最终失败后设备进入 `quarantined/unhealthy`，不会创建替代设备掩盖泄漏资源。

Console Vitest：

```text
PASS expansion maps one target to min_ready/max_instances without requiring a reason
PASS shrink requires a reason and destructive confirmation
```

OpenAPI 保持冻结版本 `1.2.0`，只增加向后兼容的可选 `reason` 字段和固定目标相等约束说明；冻结哈希更新为 `30342dcc3cf078283237e2a76cd2a27c6cef5aae75408aa6db136dcdc90693c8`。

本地全量门禁：

```text
PASS go test -count=1 -p=1 ./...（连接 PostgreSQL 17.10）
PASS go vet ./...
PASS go build ./...
PASS Vitest: 5 files / 9 tests
PASS Orval + TypeScript + Vite production build
WARN Vite 单入口 bundle > 500 kB（既有非阻塞项）
```

## 真实部署与验收

2026-08-07 部署到 Linux KVM 验收主机 `10.0.30.171`：

```text
Host: 12 CPU / 15.6 GiB RAM
Server container: alcor-device-farm-server-df017
Server image: alcor-device-farm:df029-capacity-rc1-20260807
Image ID: sha256:59230a015c1d4504cf454a793bbc5c0fd427fbac14142139ad4ce24ac5ef131f
Rollback container: alcor-device-farm-server-df028-rollback-20260807（stopped）
Console: https://10.0.30.171:18443/console/
```

验收期间没有修改 Agent 配置，也没有重启 Agent。

### 自动扩容和删除最旧设备

第一次把目标从 1 改为 2 后：

- `device_pool_images.min_ready/max_instances` 自动变为 `2/2`；
- Pool `max_concurrency` 自动变为 2；
- Host `device_slots` 高水位自动提升为 2；
- 第二台 Emulator 一次创建成功并进入 `ready/healthy`。

目标从 2 改回 1 后：

```text
deleted oldest: 67434725-32ff-4870-8c55-ad46fd5d9486
kept newest:    0a06b2fe-99fe-4c95-bcb6-87f1cc4ef6c9
delete command: succeeded / attempts=1
```

最旧设备被标记 `deleted`，ADB/Appium/STF Endpoint 清空；对应 Emulator 容器、专属网络和数据卷均为 0，最新设备的容器、网络和数据卷各保留 1。

### 占用保护和释放后继续缩容

第二次扩到 2 后，对最旧设备建立真实 Reservation：

```text
oldest busy device: 0a06b2fe-99fe-4c95-bcb6-87f1cc4ef6c9
newest ready device: f1dd3343-cd7f-4fcc-bda1-692c6c91ee4c
reservation: a9aec8ad-7542-4fa1-8d1d-09e6158e3d07
```

在 Reservation 保持 `active` 时把目标从 2 降到 1，并等待超过一个 Controller 周期：

```text
target=1:1
oldest=busy
newest=ready
protected_delete_commands=0
```

两个 Emulator 容器均保持运行，证明系统没有强删最旧占用设备，也没有转而删除最新空闲设备。随后通过正式 release API 释放 Reservation，系统按既有数据清理流程先对旧设备执行 rebuild，再自动继续缩容：

```text
reservation=released
rebuild old device=succeeded / attempts=1
delete old device=succeeded / attempts=1 / reconciled=true
old device=deleted / endpoints=false:false:false
new device=ready/healthy
active reservations=0
```

最终 Docker 资源：

```text
old device container/network/volume: 0/0/0
new device container/network/volume: 1/1/1
```

最终控制面和宿主机状态：

```text
target min_ready/max_instances=1/1
pool max_concurrency=1
host status=online, device_slots high-water=2, used device_slots=1
/readyz=200
/console/=200
memory total=15 GiB, used=5.5 GiB, available=9.4 GiB
swap total=4.0 GiB, used=1.9 GiB
```

Host `device_slots=2` 是 Server 管理的容量高水位，不代表同时运行两台；当前实际运行 Emulator 和 `used device_slots` 均为 1。控制台显示的 91 条 Reservation 是历史记录，活动 Reservation 为 0，不计入当前运行容量。

### 控制台显示口径与中文化复验

2026-08-07 复查控制台时发现，前端虽然请求了设备状态过滤条件，但 Server 的设备列表接口没有实际应用 `pool_id`、`lifecycle_status` 和 `health_status`，导致已隔离、已删除的历史记录也被计入仪表盘主数字，页面显示 6 台并容易被误解为 6 台可用设备。

修复后：

- Server 设备列表接口真实支持设备池、生命周期和健康状态过滤，非法状态返回 HTTP 400，PostgreSQL 的数据查询与总数查询使用相同过滤条件；
- 仪表盘主数字改为“当前可用设备”，同时独立显示使用中、隔离设备和已删除历史；
- 设备页默认只显示可用设备，并提供“可用设备 / 使用中 / 隔离设备 / 已删除历史 / 全部记录”分类；
- 设备、镜像、宿主机、设备池、预约、审计和健康事件中的常见状态、类型、角色和操作均使用中文显示，未知后端值仍保留原值以便排障；
- 分页、登录页、管理员角色和控制台品牌文案完成中文化。

真实 PostgreSQL 临时隔离数据库验证：

```text
PASS TestManagementAPICompleteMockFlow
临时测试数据库已在测试后删除，未对生产数据库执行 TRUNCATE 或测试写入。
```

本地门禁：

```text
PASS go test -count=1 ./...
PASS go vet ./...
PASS go build ./...
PASS Vitest: 7 files / 11 tests
PASS Orval + TypeScript + Vite production build
WARN Vite 单入口 bundle > 500 kB（既有非阻塞项）
```

真实环境部署结果：

```text
Server image: alcor-device-farm:df029-display-fix-20260807
Image ID: sha256:4f0c509a243eafc75c7a4df6a9a599ea6d1633937755171fbe5aa7774ee0b04f
Rollback container: alcor-device-farm-server-df029-capacity-rollback-20260807（stopped）
/readyz=200
/console/=200
可用设备=1
使用中=0
隔离设备=3
已删除历史=2
全部记录=6
```

因此页面中的“6”只会出现在“全部记录”分类中，表示设备数据记录总数；仪表盘主数字和默认设备列表均为当前真正可用的 1 台。

### 首页单屏与任务自动刷新复验

2026-08-07 根据实际使用反馈继续收敛首页信息：删除次数、累计预约等历史信息不再出现在运行首页，历史记录仍保留在设备分类和审计页面。首页只展示当前可用、使用中、宿主机、设备池、基础设施状态、进行中的设备任务和需要处理的隔离告警。

任务刷新规则：

- 目标设备数保存成功后立即失效设备查询缓存；
- 存在 `provisioning/booting` 时合并显示为“创建中”；
- 存在 `recycling/stopped` 时合并显示为“清理中”；
- 创建或清理进行中时每 5 秒自动刷新，任务结束后自动恢复为每 30 秒刷新；
- 首页提供“刷新状态”按钮和最近更新时间，不再要求用户按 F5；
- 针对反馈截图的 1920×919 分辨率压缩首页留白、卡片和状态行，桌面布局高度控制在内容区域单屏范围内；窄屏仍保留响应式滚动。

验证结果：

```text
PASS Vitest: 7 files / 12 tests
PASS Orval + TypeScript + Vite production build
Server image: alcor-device-farm:df029-dashboard-live-20260807
Image ID: sha256:5d6c553ee1a1c05b2df3049edce2e16efaddf2109673cbcc96f6c4cbad25450e
/readyz=200
/console/=200
当前真实设备：ready=2, creating=0, cleaning=0
```

2026-08-07 修复首页隔离告警的分类跳转：链接改为 `/devices?view=quarantined`，设备页从地址参数恢复并选中“隔离设备”分类，直接展示隔离设备列表；无效或缺失参数仍安全回退到“可用设备”。Vitest 增加直接分类跳转覆盖后为 7 files / 13 tests 全部通过，部署镜像为 `alcor-device-farm:df029-dashboard-link-20260807`，`/readyz` 和 `/console/` 均返回 200。

2026-08-07 经用户确认清理 3 条无运行资源的隔离设备记录。操作前逐项验证对应设备没有 pending/active Reservation，Docker 容器、网络和数据卷均不存在；随后在单个 PostgreSQL 事务中禁用设备池关联、将生命周期从 `quarantined` 转为 `deleted`、清空 STF/ADB/Appium Endpoint，并为每台设备写入 `delete_quarantined_device` 系统审计。操作后 `quarantined=0 / ready=2 / deleted=5`，3 条审计均已保存，当前两台 Emulator 和 `/readyz=200` 不受影响。

### 当前容量范围与后续验证

两台 Emulator 同时运行时单实例约占 4.2 GiB 和 3.4 GiB，可用内存一度约 5.7 GiB，并已出现 Swap 压力。为避免宿主机失稳，本次没有执行真实 `3 → 1`：

- `3 → 1` 删除两个最旧空闲设备、保留最新设备的算法已由 PostgreSQL 17.10 集成测试覆盖；
- 真实 Docker/KVM 环境已用 `2 → 1` 覆盖实际 delete Command、容器、网络、卷和 Endpoint 清理；
- 用户确认当前服务器暂按最多两台运行并接受本次验收结果，DF-029 据此完成；更换高内存正式服务器后，再根据真实内存设置目标上限并补充 `3 → 1` 容量压力验证，该验证不阻塞当前自动扩缩容能力交付。
