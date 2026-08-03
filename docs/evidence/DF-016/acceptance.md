# DF-016 实施与验收证据

## 当前结论

镜像 digest 验证、`validate_image` Host Command、固定目标 Controller、池镜像参数接口、并发锁、容量限制、失败隔离和退避已实现。本机没有 Docker、Linux KVM 和真实 Emulator，因此“两台 Emulator 自动创建并 ready、删除或隔离后真实补回”尚未执行，DF-016 当前状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- `POST /api/v1/device-images/{id}/validations` 将 Image 置为 `validating`；
- Controller 选择 online、非 draining 且有空闲槽位的 Docker/Hybrid Host，创建 `validate_image` Host Command；
- Agent 校验 `DEVICE_FARM_DOCKER_IMAGE` 的本机真实 digest，启动临时 Emulator，并检查 ADB、boot 和 Appium；
- 每条正式 create Host Command 都携带已验证 digest，Agent 创建前再次核对，防止验证后镜像配置漂移；
- 临时验证实例在成功或失败后清理；只有完整验证结果才能令 Image 进入 `ready`，失败进入 `failed` 并保存稳定错误码；
- 新增 Pool Image GET/PUT/DELETE 接口，设置 `min_ready/max_instances/enabled` 后由后台异步消费；
- Controller 原子登记 provisioning Device、Pool membership 和 create Host Command，Server 不访问 Docker；
- 两个 Controller 并发运行通过 PostgreSQL 行锁不超建；
- 默认 `2/2` 时 reserved/busy 设备仍占上限，不因任务压力创建第三台；
- 运行中把 Pool 并发、Host 槽位和 Image 目标从 `2` 调到 `N` 时只补创建缺少的 Emulator，不需要修改代码或数据库结构；
- 降低目标或禁用配置不自动删除设备；
- create 最终失败后隔离设备、写健康事件并指数退避；
- Controller 只处理 Docker Emulator Image，不自动创建 USB 真机；
- Server 启动时按 `warm_pool.interval` 运行 Controller。

## 本地验证结果

执行：

```powershell
./scripts/dev.ps1 -Task check
./scripts/verify-migrations.ps1 -RunRepositoryTests
```

通过项：

```text
PASS TestConcurrentControllersCreateConfiguredTargetWithoutOverbuilding
PASS TestControllerAdjustsToLargerConfiguredTargetWithoutCodeChanges
PASS TestControllerRespectsHostCapacityImageStatusAndSafeScaleDown
PASS TestFailedCreateIsQuarantinedAndBackoffPreventsCommandStorm
PASS TestImageValidationCommandGatesWarmPoolCreation
PASS TestFailedImageValidationDoesNotCreateEmulator
PASS TestManagementAPICompleteMockFlow
PASS TestEveryManagementRouteIsProtected
PASS TestManagementAPIRejectsInvalidParameters
PASS migration up/down/up
PASS repository/scheduler/reaper/reconcile/hostcommand/api suites
```

## Linux KVM 服务器必须补做

1. 固定 `DEVICE_FARM_DOCKER_IMAGE`，登记其真实 `sha256` digest；
2. 发起 Image validation，确认 Agent 创建临时 Emulator，ADB、boot 和 Appium 全部成功后 Image 进入 `ready`，且临时容器、网络和卷已清理；
3. 创建 `max_concurrency=2` 的 Pool，并设置 `min_ready=2/max_instances=2/enabled=true`；
4. 不手工创建设备，等待 Controller 自动创建、加入池并令两台设备进入 `ready/healthy`；
5. 同时创建两个 Appium Session，确认 Endpoint 和 UDID 不串设备；
6. 创建第三个并发预约，确认保持 pending/capacity unavailable，且不会创建第三台；
7. 隔离或受控删除一台，确认 Controller 自动补回一台；
8. 注入错误 digest、KVM、ADB、Appium 故障，确认 Image/Device 状态、错误码、隔离和退避符合设计且无命令风暴；
9. 将目标降低为 `1/1`，确认不会自动删除正在使用的设备；
10. 保存脱敏日志、数据库查询和容器清单作为最终证据。

## 阻塞解除条件

上述 Linux KVM 真实验收全部通过后，将 DF-016 改为 `completed` 并单独提交真实验收证据。任何 Mock 或仅 PostgreSQL 的结果都不能替代该步骤。
