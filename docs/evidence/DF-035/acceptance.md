# DF-035 官方目录、按需准备与真实多规格验收

## 结论

**completed（2026-08-11）**。Server、Console 与受控 Build Agent 已形成 Android SDK 官方稳定 System Image 目录、异步按需构建、内部 Registry 推送、真实启动验证和不可变摘要登记的唯一链路。验证成功前不会产生候选 `device_images`；相同 digest 与 runtime profile 会直接复用已验证成品。Android 16 是 Console 默认候选，但目录同步不会自动创建 Android 13～16 四台设备。

真实 Linux/KVM 主机完成了官方 API 36 镜像构建、摘要锁定、缓存命中、单设备重装、5/8 GiB 两档内存、GPU host 失败与 auto/software 安全回退、目标失败后恢复旧配置，以及 ADB、STF、Appium UiAutomator2 冒烟。最终 Host 只有用户设定的一台运行设备，Pool 默认镜像为本次验证的 Android 16 成品。

## 实现与边界

- OpenAPI 提升到 `1.6.0`，新增官方目录查询、目录同步、镜像准备、准备状态与 Host Command 续租契约；Console 生成客户端随契约更新。
- migration `000009_android_system_image_catalog` 增加官方目录和准备记录；`000010_verified_image_preparation_gate` 兼容生产环境早期预览表结构，并补齐 build/validation 命令、digest、镜像大小和“验证后才可缓存”约束。
- Build Agent 只接受 Server 下发的固定官方包名与 revision，使用固定 `sdkmanager` 19.0、`avdmanager`、固定上游提交和仓库内补丁；不接受 URL、Shell、Registry 凭证或任意 Docker 参数。
- 构建完成只排队 `validate_image`。只有 digest 核对、KVM 启动、ADB/boot、Appium 和 STF 注册全部成功后，Server 才在事务内创建 `ready` Image 并把准备状态改为 `cached`。
- 多小时构建使用 Host Command 租约续期和独立两小时准备超时；Agent 重启后由持久化命令恢复，不依赖浏览器标签页。
- Console 展示“可下载、准备中、验证中、已缓存、失败、官方已更新”，并区分共享镜像层 `image_disk_mb` 与每设备数据盘；viewer 角色没有同步、准备和验证入口。
- Docker Provider 对显式 `host` 映射受控的 `/dev/dri/card0` 与 `/dev/dri/renderD128`；`software` 和安全的 `auto` 使用 SwiftShader 且不映射 GPU。仅发现 render node 不能证明 Headless EGL 可启动，因此 `auto` 不会把探测结果误当成已验证 host GPU。
- 重装目标耗尽启动 deadline 后，回滚获得独立的有限 deadline，Host Command 租约持续续期；回归测试覆盖目标超时后仍可恢复旧配置。

本任务只实现 Device、Image、Host、Pool、受控命令和设备域审计。没有引入 Case、Dataset、Target、Run、评分、Artifact 业务索引或第二套评估前端；没有复制 DaFit 的 Appium Runner、页面对象、动作、断言、证据和报告，也没有复制 STF 的 claim、release、remoteConnect、看屏、日志或文件管理。

## 复用核对

开工前按项目规则搜索了 `E:/AutoTestTools/Projects/Alcor` 与 `E:/AutoTestTools/Projects/dafit_auto_platform`。旧 Alcor 只提供历史边界理解，DaFit 继续作为业务执行和报告能力；两者都没有可直接复用的官方 System Image 目录、受控构建 Agent 或验证后登记能力。本任务复用现有 Docker Provider、KVM、STF Adapter、Appium 健康探针、Host Command、动态容量和 DF-034 重装编排，没有形成第二套兼容实现。

## 自动化门禁

本地最终门禁：

```text
go test ./...
PASS（全部 package）

verify-migrations.ps1 -RunRepositoryTests
000001～000010 up: passed
constraint checks: passed
down migration: passed
up-down-up migration: passed
repository/scheduler/reaper/reconcile/hostcommand/metrics/API/warmpool: passed

python -m unittest test_sdk_catalog.py
Ran 2 tests - OK

Console
Test Files  7 passed (7)
Tests       28 passed (28)
tsc -b     passed
vite build passed

git diff --check
bash -n build-agent/migration/STF scripts
passed
```

API 集成测试明确断言：构建成功但验证前 Image 数量不变；缺失 `digest_verified`、`ready` 或 `stf_registered` 的验证结果不能登记；失败不创建 Image；相同 digest/profile 的第二次准备不增加 validation 命令；官方 revision 变化只显示“官方已更新”，不会替换现有成品。Provider/Agent 测试覆盖 host/auto/software 解析、GPU 设备映射、5/8 GiB 容量、长命令租约续期和目标超时后的独立回滚窗口。

## 生产迁移与部署

真实主机 `10.0.30.171`：12 CPU、15.6 GiB 内存、Linux KVM、Docker、`/dev/dri/card0`、`/dev/dri/renderD128`、STF 3.7.9、Appium 3.5.2。迁移前备份：

```text
/home/kerr/alcor-device-farm-runtime-20260806/backups/df035-final-before-000010-20260811.dump
sha256: 86a10f2f00dfaefdef6470cd78c17ff2c0220a7e27574d7b85dc6a1681ea0d76
```

`000010` 已在生产 PostgreSQL 成功应用。最终 Server 容器为 `alcor-device-farm:df035-final-20260811`，旧预览容器保留为 `alcor-device-farm-server-df035-preview-r2-rollback`。由于部署时外部 Go Proxy 超时，Server runtime image 使用本地同源码交叉构建并校验的静态二进制组装；Server 二进制 SHA-256 为 `baad46c0a05b7b6802dfbdc846b59e47b552b4172f8a31aaab322b2bc7ebd87c`。

最终 Host Agent SHA-256：

```text
1bd047504a8502d0fde076ab833fa32daa67fd1428ddad7e3acbfb4837576959
```

固定上游 `budtmo/docker-android` 提交为 `e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`；共享补丁检查通过。内部 Registry 为 `127.0.0.1:5001`，不向 Console 暴露凭证或 Docker Socket。

## 官方目录与真实构建

2026-08-11 02:59 UTC 的真实同步只读取 `sdkmanager --list` 的稳定 `Available Packages`。该 SDK 快照返回以下候选：

```text
API 36 google_apis x86_64 revision 7
API 35 google_apis x86_64 revision 9
API 34 google_apis x86_64 revision 14
API 33 google_apis x86_64 revision 17
```

实现和数据库约束同时支持 `google_apis` 与 `google_play`，但不会为当前官方输出中未出现的 `google_play` 条目伪造目录记录。API 按 API level 降序返回，所以 Android 16 是默认候选。

第一次 API 36 准备：

```text
preparation: 908b3c09-ee3f-4421-ad9f-2c1c55215685
prepare command: 8c1e6d8b-2b92-4abd-a6fb-a2fcdc5483e4
build: 02:59:46Z -> 03:05:34Z, succeeded, attempts=1
validation command: 859b50a4-878a-4539-8850-ac33c5dc7545
validation: 03:05:34Z -> 03:07:01Z, succeeded, attempts=1
```

构建期间租约多次延长，`attempts` 始终为 1；没有因五分钟 lease 重复构建。`device_images` 在构建和 validating 阶段保持 1 条，真实验证成功后才增加到 2 条。成品：

```text
Image ID: 60651f99-400b-4bb7-a915-55bdbd44502c
Registry: 127.0.0.1:5001/alcor/android-emulator:16.0-api36-google_apis-x86_64-sdk7
Digest: sha256:894537cec0eec1039a8fa40a82a5a3bd8366b91c75df8b19fa1d246401331ffa
Shared image layer: 7516 MiB
Status: ready
```

第二次相同 digest/profile 的准备 `63767a7f-a642-46dc-85a5-4b3a27d57ca2` 在 3 秒内变为 `cached`，复用同一 Image ID，`validation_command_id` 为空，validation 命令总数没有增加。早期故意触发的 SDK 版本解析/下载失败记录均为 `failed`，没有产生 Image。

## 多规格、GPU 与回滚真实验收

真实 Host 心跳容量在验收时为 12 CPU、15592 MiB 总内存、约 8192 MiB 可用内存、单设备已用 4 CPU/5120 MiB。镜像已缓存，所以容量计算只收取一次 7516 MiB 共享层，并按设备收取 4096 MiB 数据盘。

1. 显式 `host/5120 MiB` 映射 DRM 节点后，Headless Emulator 在 command deadline 内没有 ADB。这证明“节点存在”不等于 host EGL 可运行；失败目标没有成为当前配置。
2. `software/8192 MiB`、Guest 6144 MiB 真实启动成功。Docker `Memory=8589934592`，只映射 `/dev/kvm`，启动参数包含 `-gpu swiftshader_indirect -memory 6144`。
3. `auto/5120 MiB`、Guest 4096 MiB 真实启动成功。Docker `Memory=5368709120`，只映射 `/dev/kvm`，安全回退参数为 `-gpu swiftshader_indirect -memory 4096`。
4. 最终版 Agent 再次故意提交 `host`：目标从 `04:10:12Z` 运行到 deadline，恢复容器于 `04:14:46Z` 以 auto/SwiftShader 创建，ADB、Appium、STF 通过后在 `04:16:45Z` 收敛为 `failed|ready|healthy|auto`。错误为 `REIMAGE_TARGET_FAILED: target failed; previous image was restored`，不是 `REIMAGE_ROLLBACK_FAILED`。随后一次成功的 auto 重装清除了失败提示。

## 最终状态与接入冒烟

最终巡检：

```text
Device: f1d2a677-e05f-4926-80dc-c113cd5b9598
Image: 60651f99-400b-4bb7-a915-55bdbd44502c
lifecycle_status=ready
health_status=healthy
reimage_status=idle
runtime=4 CPU / 5120 MiB / auto -> SwiftShader

Pool default_image_id=60651f99-400b-4bb7-a915-55bdbd44502c
total_target=1, min_ready=1, max_concurrency=1
managed running containers=1
incomplete Host Commands=0
```

ADB 在 STF ADB 容器中显示 `10-0-30-171.nip.io:32795 device`；STF API 返回 `present=true, ready=true`；Appium `/status` 返回 `ready=true, version=3.5.2`，并成功创建、删除一次真实 UiAutomator2 Session。最终容器使用锁定 Registry tag/digest、4 CPU、5 GiB 内存和单独的数据卷。失败或被替换的 Device 行保留为设备域审计历史，但没有对应运行容器，不计入用户设定的设备数量。

## 已知限制与回滚

- 该测试 Host 的 DRM 节点可见，但显式 host GPU 无法完成 Headless Emulator 启动；这是本次实测的 Host 能力限制。`auto` 已安全回退 SwiftShader，管理员仍可在未来驱动/EGL 环境修复后显式重测 host。
- 当前官方稳定输出只有四个 `google_apis` 候选；`google_play` 会在 SDK 官方输出出现时由同一解析和验证链路同步，不会手工造数。
- 历史失败准备、失败重装和 quarantined Device 记录按审计要求保留；最终运行资源和在途命令均已收敛。

回滚顺序：先停止最终 Agent，恢复保留的上一版 Agent 二进制；把 Server 切回 `alcor-device-farm-server-df035-preview-r2-rollback`；如需回退 schema，先用上述 dump 恢复生产数据库，再移除最终容器。Registry 成品按 digest 不变，可保留供审计；Pool 默认镜像在回退前应先切回旧 ready Image。不得只执行 migration down 而继续运行依赖 `000009/000010` 的 Server。
