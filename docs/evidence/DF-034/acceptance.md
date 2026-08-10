# DF-034 设备规格编辑和受控重装验收

## 结论

**completed（2026-08-10）**。Device 已能保存当前、有效和待应用运行规格；管理员可在 Console 对没有活动预约、没有在途命令的 `ready/stopped/quarantined` Docker Emulator 编辑 Image、CPU、内存、数据盘和图形模式。Server 在提交瞬间按 Host 实际剩余 CPU、内存和磁盘复核容量，Agent 清理旧容器和设备数据卷后创建目标实例。只有 ADB、Android 启动、Appium 和 STF 全部通过，当前 Image/规格才会切换。

目标创建失败时只恢复旧 Image/规格一次。恢复成功时保留旧的当前配置并把本次重装记为失败；恢复也失败时设备进入 `quarantined/unhealthy`。使用中的设备、存在任意在途命令的设备和资源不足的规格均在删除旧 Provider 资源前被拒绝。

## 实现范围

- migration `000008_device_reimage_configuration` 增加当前覆盖、待应用 Image/规格、重装状态和错误信息，并用数据库约束防止非法 pending 组合；
- OpenAPI 提升到 `1.5.0`，新增 `POST /api/v1/devices/{id}/reimages`、`DeviceReimageInput` 和 Device 配置状态字段；
- Management API 复用持久化 `rebuild` Host Command，以 `operation_kind=reimage` 区分受控重装；幂等键、审计原因、活动预约、任意在途命令、Image 状态和动态容量都由服务端校验；
- Agent 在执行前再次按实时资源预检，删除旧容器/卷并创建目标；目标失败只恢复旧配置一次；
- Host Command 只有在目标健康结果完整时才提交新 Image/规格，回滚或最终失败不会提前改写当前配置；
- Console 增加“编辑配置”表单、处理中/失败状态、清空 APK 和设备数据的明确警告及二次确认；占用中的设备不显示入口，刷新后从 Server 恢复异步状态。

## 自动化门禁

在与生产隔离的 PostgreSQL 16 容器中从空库执行 `000001`～`000008` migration 和约束脚本，结果：

```text
MIGRATIONS_CONSTRAINTS_OK
```

Go 全量测试和静态检查通过：

```text
go test -p 1 -count=1 ./...
GO_TEST_OK

go vet ./...
GO_VET_OK
```

Console 单元测试和生产构建通过：

```text
Test Files  7 passed (7)
Tests       27 passed (27)
pnpm build
✓ built
```

API 集成测试覆盖成功后才切换当前配置、目标失败恢复旧配置、最终失败隔离、幂等重放、活动预约/任意在途命令/容量不足拒绝。Console 测试覆盖空闲设备入口、占用设备无入口、表单初值、警告和二次确认。

## Linux KVM 真实验收

目标主机：`10.0.30.171`，12 CPU、15 GiB 内存、Linux KVM、Docker Emulator、STF 3.7.9、Appium 3.5.2。生产 migration 前已保存可恢复备份：

```text
/home/kerr/alcor-device-farm-runtime-20260806/backups/df034-before-migration-20260810.dump
```

真实验收使用已有 Android 16/API 36 设备 `834af4a4-be48-4144-9587-8b9c33afb8f0`，没有活动预约和在途命令。先故意保留其当时的 `quarantined/unhealthy` 状态，验证受控重装可恢复隔离设备；目标仍为同一 ready Image，运行规格为 4 CPU / 5120 MiB。

提交后立即观察到：

```text
lifecycle_status=provisioning
health_status=unknown
reimage_status=pending
pending_image_id=b6492bc3-4e37-457e-a5f8-f4a30f46959f
current image_id=b6492bc3-4e37-457e-a5f8-f4a30f46959f
active commands=1
```

重装前后证据：

```text
旧容器 ID: 1db6dbe76d3a78515b7faaa928f5eda77cd4f86b3b6eadebc18c496757ad2342
旧容器启动: 2026-08-10T05:59:22.468686907Z
旧 ADB/Appium: 10-0-30-171.nip.io:33030 / http://10-0-30-171.nip.io:33029

新容器 ID: ff66d39c415468df616e714aa52628d1a706f34196020d449219e0ae703644c6
新容器启动: 2026-08-10T10:35:36.894804069Z
新 ADB/Appium: 10-0-30-171.nip.io:32769 / http://10-0-30-171.nip.io:32768
新数据卷创建: 2026-08-10T10:35:30Z
```

数据卷名称按 Device 保持稳定，但 Docker volume 的创建时间已更新，证明旧卷被删除后重新创建，不会保留原 APK 和设备数据。最终状态：

```text
Device ID 不变
Pool membership 不变
lifecycle_status=ready
health_status=healthy
reimage_status=idle
consecutive_failures=0
active reservations=0
active commands=0
STF ADB: 10-0-30-171.nip.io:32769 device
Appium /status: ready=true, version=3.5.2
```

真实过程没有依赖浏览器刷新或切换标签页；Server、Host Agent、Docker、STF 和 Appium 在后台完成并持久化状态收敛。

## 边界

本步骤不发布 Android 13～16 的四套正式镜像目录。统一镜像地址、不可变摘要、Android 16 默认项和跨版本真实重装验收属于 DF-035。本步骤也没有在设备农场实现 Appium 业务步骤或 STF 看屏协议，只复用现有 Adapter 和 Provider。
