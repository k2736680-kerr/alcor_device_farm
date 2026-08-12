# DF-038 验收证据：长期设备、基础设备扩容和 Android Studio 式创建流程

## 结论

DF-038 已于 2026-08-12 在 `10.0.30.171` 的真实 PostgreSQL 16、Linux KVM、Docker Emulator、STF 3.7.9、Appium 3.5.2 和 Host Agent 环境完成验收。设备使用后的数据默认保留；只有显式 rebuild/reimage 恢复干净状态。Phone 创建、已缓存与未缓存镜像编排、基础设备扩容和空闲设备直接删除均通过真实链路。

本任务只建设设备域。开工前已搜索 `E:/AutoTestTools/Projects/Alcor` 和 `E:/AutoTestTools/Projects/dafit_auto_platform`，没有可直接复用的 Phone 创建编排或镜像 Build Agent；实现继续复用 Android SDK、Host Command、Host Agent、Docker Provider、Reservation、STF Adapter 和 Appium 健康链路，没有复制 DaFit Runner、STF 远控或 Alcor 业务对象。

## 数据库、构建与部署门禁

- Go SDK 安装在仓库忽略目录 `.tmp/toolchains/go1.26.5/`，版本为 `go1.26.5 windows/amd64`。
- migration up/down/up 已在部署主机的临时 PostgreSQL 16 容器完成；约束与 Repository 集成测试通过。生产库已先备份，再应用 `000012`、`000013`。
- Console 测试和生产构建通过；OpenAPI 生成客户端、TypeScript 和 Vite 构建通过。
- 正式 Server 镜像为 `alcor-device-farm:df038-rebuildfix-final-20260812`，镜像 ID `sha256:cc242fce268d7b96fed41a59d3e779c97181cf26b05e9efd38906181fddaf0c9`，`/readyz` 通过。
- 部署主机确认具有 `/dev/kvm`、Docker、PostgreSQL 16、STF 3.7.9、Appium 3.5.2 和运行中的 Host Agent。验收结束时未完成 Host Command 为 0。

## Android Studio 式 Phone 创建

Device 页面提供四步 Phone 创建流程：选择多条可搜索的 Pixel 模板、Android SDK 目录版本、活动 Pool，以及 CPU、内存、数据盘、分辨率、DPI、VM Heap 和 GPU 等高级配置。TV、Wear、Tablet、Automotive、Desktop 和 XR 未进入本期入口。日常导航不再要求管理员先操作镜像页；镜像仍由底层目录、准备、验证和 digest 缓存链路管理。

已缓存 API 36 请求复用 preparation `63767a7f-a642-46dc-85a5-4b3a27d57ca2` 和 Image `60651f99-400b-4bb7-a915-55bdbd44502c`。同一 Idempotency-Key 两次请求只生成一个 job、一个 Device 和一个 create Host Command。真实 Pixel 8 设备 `3469b5c2-a9aa-496e-be82-3b3acc567935` 最终通过 ADB、STF 和 Appium 门禁，且没有基础设备 marker；验收后通过公开删除 API 清理，Pool 恢复为 1。

未缓存 API 35 / `google_apis` / revision 9 选择 Pixel 7，首次真实执行暴露并保存了两个失败阶段：

1. `prepare_system_image / AGENT_COMMAND_FAILED`：Docker 镜像实际已构建，但 `prepare.sh` 使用 `15.0.0-api35-...`，`build.sh` 使用 `15.0-api35-...`，推送阶段找不到源标签。修复后两侧统一为 `${android_version}-api${api_level}-...`，并新增契约回归测试。
2. `prepare_system_image / INSUFFICIENT_HOST_RESOURCES`：镜像推送成功后，Host 只剩约 7.7 GiB，无法同时满足 4 GiB 磁盘安全余量和验证设备数据盘。确认没有活动构建后，只清理超过 24 小时且未被引用的 BuildKit 缓存，释放 1.052 GiB；没有删除容器、设备卷、Registry 或 API 35/API 36 成品镜像。

修复后的最终 job `4d9025ca-2f92-4424-874d-87a62ad37766` 使用同一 Idempotency-Key 连续提交两次，均返回同一 job。数据库唯一性结果为：1 job、1 preparation、1 Device、1 create Host Command、1 Pool membership，只增加一次 Pool 目标。最终结果：

| 项目 | 实测值 |
| --- | --- |
| preparation | `817753fa-caf2-402a-958f-7aede331cdb4`，`cached` |
| Image | `abc5e576-a8e8-4e4a-94e5-86ca5bcec866` |
| Registry | `127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9` |
| digest | `sha256:c56869fac38daef9dfedfc3f8858da5fbd7f2301c484bca9563120907cf30c8e` |
| Device | `fe3ff3ed-e66b-44d2-8360-02c1df7275dc`，Pixel 7 / API 35 |
| runtime profile | 4 container CPU、5120 MiB container memory、4 guest CPU、4096 MiB guest memory、4096 MiB data disk、1080×2400、420 dpi、512 MiB heap、auto graphics |
| ADB | `device`，`sys.boot_completed=1` |
| STF | `present=true, ready=true, using=false` |
| Appium | `/status ready=true, version=3.5.2`；真实 UiAutomator2 session 创建和删除成功 |
| 数据隔离 | 独立卷 `alcor-df-emulator-fe3ff3ed-e66b-44d2-8360-0ae33ed1-data`；基础设备 `/sdcard/df038/persistent.txt` 未复制 |
| 最终 job / Device | `ready` / `ready, healthy` |

验收后通过公开删除 API 删除该临时设备。Device 行保留为 `deleted` 审计历史，容器和独立数据卷已删除，Pool 从 2 恢复为 1，等待后没有自动补建。

## 基础设备与扩容

Pool `8b97a9e7-ab6c-41ff-bc1e-bd922b829ab3` 通过公开 API 将 Pixel 9 / API 36 设备 `f05429f0-39c2-47be-a7da-8c73a6f34207` 设置为基础设备。候选设备为本 Pool 的 Phone Emulator，状态为 `ready/healthy`；最终 Pool 配置为 `total_target=1`、`min_ready=1`、`max_concurrency=1`。

扩容设备 `3ff8ddaa-5798-43e4-a2c0-0c3fcf21a92e` 与基础设备的 Image、`hardware_profile_id=pixel_9` 和完整 effective runtime profile 一致，但 Provider ref、容器和数据卷均独立，且不包含基础设备 marker。真实 ADB、`sys.boot_completed=1`、STF `present/ready`、Appium `ready=true` 和 UiAutomator2 session 均通过。

基础设备仍可正常预约使用。直接删除基础设备返回 409；必须先选择替代基础设备。非本 Pool、非 Emulator、非 healthy/ready 候选由服务端状态和归属门禁拒绝。

## 预约释放保留数据

在基础设备写入 `/sdcard/df038/persistent.txt`，内容为 `retained-base-20260812`，随后创建预约 `cf2330ae-af5d-44ba-9fdb-1648812cc967` 并通过正式 release API 释放。释放前后：

- 容器 ID、StartedAt 和数据卷不变；
- marker 文件内容仍存在；
- Appium UiAutomator2 辅助包仍存在；
- Device 回到 `ready/healthy`；
- 没有产生 rebuild/reimage Host Command。

该结果证明预约 release 只释放 Reservation/STF 占用，不再恢复出厂；APK、账号、缓存和文件由使用者自行管理。

## 显式恢复出厂与直接删除

对扩容设备调用公开 rebuild API，健康 `ready` 设备的请求返回 202。rebuild 使用设备当前 `effective_runtime_profile`，保持 4 CPU、5120 MiB 等有效配置；Host Agent 完成删除和干净重建后设备重新收敛为 `ready/healthy`，原 marker 文件被清除，Appium 辅助包由健康门禁重新安装。rebuild 同时拒绝带活动预约或活动命令的设备。

随后通过公开 DELETE API 删除该非基础设备：Pool 目标 2→1，容器和独立数据卷删除，等待 35 秒未自动补建，未完成 Host Command 为 0。删除操作在设备域保留 `deleted` 行和审计记录，不物理删库。

## 最终状态与回滚

- 基础设备 `f05429f0-39c2-47be-a7da-8c73a6f34207` 仍在运行并为 `ready/healthy`。
- Pool 最终为 `total_target=1`、`min_ready=1`、`max_concurrency=1`，基础设备 ID 未改变。
- API 35 和 API 36 已验证镜像保留在内部缓存；临时验收设备均已通过公开 API 清理。
- Server 可切换回部署前保留镜像；远端 `prepare.sh.before-df038-tagfix` 可作为单文件回滚副本。数据库需要回退时应先停止 Server，再使用应用 migration 前的 PostgreSQL 备份恢复。
