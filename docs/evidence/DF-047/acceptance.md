# DF-047 iOS 真实验收、运维回滚与 Android 回归

## 当前状态

`completed`

DF-047 已在专用 Apple Silicon macOS Host、真实 Xcode/CoreSimulator、Appium 3、Appium Device Farm、XCUITest 和 WDA 环境完成最终验收，并在独立 Linux KVM Host 完成 Android 第一版回归。当前只签收 iOS Simulator，不宣称真实 iPhone 已接入；iOS 远控对象仍是目标 Simulator，不是 macOS 桌面。

## 最终版本

- 本地开发分支：`codex/device-farm-v2`；
- Server：`device-farm-server-df047-r5-darwin-arm64`，SHA-256 `ea9403bc6f831a677a254ef222eacb90b367cd43a5b888f99ae8d722c5f1f4d1`；
- Host Agent：`device-host-agent-df047-r11-darwin-arm64`，SHA-256 `448bdd95a92091e5eafe36596eba53a9309aa501e83bd7a15c70c309cafce640`；
- Appium Device Farm 固定为 12.0.1。Hub、Node 和 Agent 均由 launchd 管理；Hub/Node 增加 10 秒探测、连续三次失败退出和自动拉起；Simulator 启动/重建需连续稳定 10 秒才完成；
- Host readiness 自动维护使用 90 秒恢复宽限。宽限内停止新预约，但不消耗 Device 隔离失败预算；恢复后自动清除故障起始时间。

## iOS Simulator 真实稳定性

正式连续长测从 1/50 启动，中途没有续算或跳过失败轮次。最终结果：

```text
completed_iterations=50
parallel_check=true
double_allocation=0
cross_device_session=0
core_simulator_residue=0
elapsed_seconds=6275.7
status=passed
```

两台不同 Simulator 的 Reservation、明确 UDID XCUITest Session 和 `/source` 并发通过。随后 50 次动态创建、Session、释放、重建、正式删除全部通过；20 轮和最终状态均抽样确认没有开放 Reservation 或非 `deleted` iOS Device。自然过期专项确认 Reaper 先关闭 Session/WDA，再将 Reservation 收敛为 `expired`，结果 `expiry_check=true`。Host drain 与 Pool disable 均拒绝新建，解除后重新可调度，结果 `drain_check=true`。

无 Reservation 的旁路 Appium Session 被收敛为 `quarantined/degraded`，原因码为 `IOS_PROVIDER_BUSY_WITHOUT_RESERVATION`，随后只通过正式 Device API 清理。首次专项暴露验收脚本错误地只接受 `unhealthy`；脚本已按设计允许 `degraded|unhealthy`，并增加原因码断言后从 1/1 完整重跑，结果 `drift_check=true`、`status=passed`。没有改数据库制造成功。

## 故障恢复、排空与回滚

- Hub inventory 存活但无响应：Host 先进入 maintenance，64 秒内由 watchdog/launchd 恢复并回到 online；
- Node inventory 存活但无响应：同样先阻止预约，69 秒内恢复；
- Agent 被强制终止后，launchd 将旧 PID 替换为新 PID，Host 保持 online；
- 三项均满足 120 秒内收敛或隔离的验收目标；自动维护期间没有误隔离当前 Device；
- 回滚前先 drain Host、禁用 iOS Pool并确认没有活动 Reservation/Session。最终 r5/r11 回滚到上一验证组合 r4/r10，真实单轮创建、Session、重建、删除 106.9 秒通过；随后按同样保护恢复 r5/r11，最终单轮 121.6 秒通过；
- `bootout` 后发现一个旧 Node 子进程仍占用 4724，已核对命令路径后只终止该受管进程，再由 launchd 正式启动；没有杀死 Hub、Server 或非本项目进程；
- Mac 自带 22 条非 `Alcor-DF-` Simulator 目录记录不属于本项目，没有删除。最终 Hub/Node 受管 inventory 均为 0，CoreSimulator Booted=0、受管实例=0。

## 备份与独立恢复

最终 PostgreSQL custom-format 备份为 `df047-final-20260819.dump`：

```text
sha256=1b05132f21cf0b1aa1d517e56b8a32624662dfe4bb3746346e091bd4a7d9b315
mode=0600
```

第一次使用容器管理员角色恢复时，Supabase 自带 `vault.secrets` 权限拒绝，未签收该备份。随后改用 Server 实际数据库角色重新导出；独立恢复库成功读取 Host=1、Device 历史=156、Reservation=170、Device Session=170。临时恢复库验证后删除，最终 dump 和校验文件保留且均为 0600；证据未保存数据库 URL、密码或 Vault Secret。

## Android、DaFit 与 Alcor 回归

- iOS 回滚前后 Android 长期 Emulator 保持同一容器 ID 前缀 `d10de42ab923…`、同一数据卷、同一启动时间，没有重建；
- 系统仍为 Android 15 / API 35，镜像仍为已验证的内部 Android 15 x86_64 镜像；
- `com.crrepa.band.dafit` 仍为 `2.9.19-6-ge155523fac-dirty`，首次安装和最后更新时间均未变化；
- Windows Harness 通过远端 ADB/Appium 运行真实 `STEPS_SMOKE_001`：`1 passed in 65.50s`，报告写入系统临时目录，没有写入或提交 DaFit 用户 `outputs/`；
- DaFit `python tools/run_full.py --collect-only` 仍收集 449 个执行实例；
- 新版 Alcor `go test ./internal/platform` 通过，未修改 Alcor 或 DaFit 的用户工作树。

## 本地门禁与最终状态

- `go test ./...`、`go vet ./...` 通过；
- PostgreSQL migration `up → down → up` 和全部设备域集成测试通过，包括磁盘/内存不足中文提示、累计运行超过四小时的滑动续约、Host 恢复宽限和删除并发保护；
- Console 7 个测试文件、37 项测试、生产构建和 `generate:check` 通过；既有单入口 bundle 大于 500 kB 提醒不阻塞本任务；
- 最终 Host 为 `online/draining=false`，iOS Pool 为 `active`，开放 Reservation=0，非 `deleted` iOS Device=0，数据库 ready=1，Agent online=1；
- Token、Session Grant、完整 UDID、Host 地址、硬件序列号和密码未写入 Git 证据。

DF-047 的 iOS Simulator P0/P1、故障恢复、备份、排空、版本回滚和 Android 回归均已通过，可以完成本任务。真实 iPhone、签名/Provisioning Profile 和 iOS 业务 Executor 仍是后续独立任务。
