# DF-041 macOS Host Agent 与 Appium Device Farm Adapter 验收

## 当前状态

`completed`

代码、真实 E4 验收和脱敏证据均已完成，主要实现提交为 `ba25501 完成DF-041 macOS宿主机与iOS只读适配`。本状态不提前宣称 DF-042 Session Fence、DF-043 Simulator 生命周期或 DF-044 真机能力完成。

## 实现结果

- 现有 `device-host-agent` 可编译并原生运行在 macOS arm64，新增 `appium_device_farm_ios` Provider 分支；Android `mock/docker`、STF 注册和动态容量路径保持原样；
- 新增 `internal/adapters/appiumdevicefarm`，固定适配 12.0.1 的本机接口，只执行 GET 读取 `/status`、`/device-farm/api/status` 和 `/device-farm/api/device/ios`；Create/Start/Stop/Restart/Rebuild/Delete 全部返回稳定的只读库存不支持错误；
- Adapter 只接受 loopback Node Endpoint，不启用跨 Host Hub、Dashboard、Team Allocation 或插件数据库同步；
- iOS inventory 保存平台、Simulator/physical 类型、系统版本、机型、busy、block、Node 连接和组件健康；Appium Endpoint 可共享，但 serial/UDID 与 Provider identity 不放宽；
- Host Agent 上报 `host_os/host_arch`、macOS/Xcode build、可用 iOS Runtime、Node/Appium/插件/XCUITest/WDA/go-ios 固定版本、Appium doctor、Node health 和脱敏 readiness；
- readiness 或 inventory 失败时，Server 把 Host 置为 `maintenance` 并停止新预约；由 readiness 导致的维护态恢复后回到 `online`，人工维护态不会被健康心跳误解除；
- Host 心跳增加平台、设备类型、Provider、能力和组件健康；iOS 不执行 STF ADB 注册，不套用 Android Emulator runtime profile 和资源占用；
- 未知 Simulator 继续可观察但 `allowlisted=false`、non-ready，不自动创建 PostgreSQL Device；只有 allowlist 内的固定设备计入有效槽位；
- 内部 OpenAPI 的 `DiscoveredDevice` 已增加平台中立字段并更新冻结 hash；部署、验证和回滚步骤见 `deploy/ios-host/README.md`。

## E4 脱敏环境

验收日期：2026-08-17。

- 专用 Apple Silicon Mac mini：Apple M4 10 核、24 GB；硬件序列号未保存；
- macOS 26.5.1，Build 25F80；架构 arm64；
- Xcode 26.3，Build 17C529；license/first-launch 检查通过；
- 可用 iOS Runtime：18.6、26.3；另有 tvOS/watchOS/visionOS，但不进入首期；
- allowlist 内启动 1 台 iOS 26.3 iPhone Simulator；完整 UDID 已脱敏，只保留在 macOS 本机验收环境；
- 当前没有连接物理 iPhone；这不阻止 DF-041，真机、配对、Developer Mode 和 WDA 签名属于 DF-044；
- 工具链安装在 macOS 用户目录下的 DF-041 独立目录，没有替换原有 Node 26 或其他全局工具。

固定版本实测：

| 组件 | 实测版本 | 结果 |
|---|---:|---|
| Node.js | 22.23.2 | 通过 |
| Appium | 3.6.0 | 通过 |
| Appium Device Farm | 12.0.1 | 通过；CLI installed JSON 为版本真相 |
| XCUITest Driver | 12.4.0 | 通过 |
| WebDriverAgent | 16.2.0 | 通过 |
| go-ios | 1.3.2 | 通过 |

Appium XCUITest doctor：0 个 required fix；1 个可选 `applesimutils` 缺失，只影响可选 execute method，不阻止本任务 inventory、Node health 和固定 Simulator readiness。

## 真实验收记录

### 1. 本机 Adapter 和工具链

在 E4 Mac 上运行版本化 Go 集成入口：

```text
go test -count=1 -v ./internal/adapters/appiumdevicefarm ./internal/ioshost
```

结果：

- 只读 Adapter 发现 allowlist 内 1 台 `Booted`、idle 的 iOS 26.3 Simulator；
- Appium `/status` ready，插件 `/device-farm/api/status` ok；插件状态接口按上游实现返回 `version=unknown`，固定 12.0.1 由 CLI installed JSON 独立验证；
- macOS/Xcode/Runtime/Node/Appium/插件/XCUITest/WDA/go-ios/readiness 全部通过；
- macOS arm64 `device-host-agent --version` 可运行，Appium 只监听 `127.0.0.1:4723`。

### 2. inventory 加入、移除与上游回退行为

- 关闭 allowlist Simulator 后，真实集成测试按预期失败关闭，该设备不再 ready；
- 再次启动 Simulator 并重启本机 Node 后，Adapter 和 readiness 测试恢复通过；
- 实测确认 Device Farm 12.0.1 在没有任何 Booted Simulator 时会回退列出全部本机 Simulator，而不是返回空列表；本实现没有把该上游选项当作安全边界：其余条目均为 unknown/non-ready，Server 未自动建 Device，有效槽位仍为 1；
- 该行为和防护已写入 macOS 部署文档，后续 DF-043 仍必须显式登记固定库存。

### 3. Node 故障和恢复

- 停止本项目独立 Appium Node 后，`ioshost` readiness 真实测试失败，未伪造 healthy；
- Node 重启并通过两个状态接口约 2 秒；Host Agent 下一次心跳恢复 `online`；
- 真实 Server 心跳链路中，Node 停止后 Host 收敛为 `maintenance`，Node 恢复后回到 `online`；该流程没有修改 Reservation 或插件 busy 伪造释放；
- 人工设置的 `maintenance` 在 readiness=true 心跳后仍保持，证明自动恢复不会覆盖运维意图。

### 4. 真实 Host Agent → Server 心跳

使用一次性 PostgreSQL 17.10、仅监听 loopback 的临时 Device Farm Server 和 SSH 反向隧道运行真实 macOS Agent。验收后 Agent、隧道、Server 和数据库均已停止。

数据库脱敏核对结果：

```text
Host: online / macos / arm64 / readiness=true / used_slots=1
Device: ready / healthy / platformVersion=26.3 / providerBusy=false / router=passed
unknown_devices_created=0
Node failure: Host -> maintenance
Node recovery: Host -> online, Device -> ready/healthy
```

Agent 连续心跳和 Command Claim 都经 Agent Token 认证成功。测试 Token 只作为进程环境变量使用，未写入仓库、配置、证据或普通日志。

### 5. PostgreSQL、契约和 Android 回归

Windows 11、Go 工具链和一次性 PostgreSQL 17.10：

1. `scripts/verify-migrations.ps1 -RunRepositoryTests`
   - migration `up → down → up`、旧 Android 回填和全部约束通过；
   - Repository、Scheduler、Reaper、Reconcile、Host Command、Metrics、API、Warm Pool 串行数据库集成测试全部通过；
   - 新增 Host readiness、iOS registered/unknown inventory、平台身份冲突和人工 maintenance 保留测试通过；
   - Android 第一版数据库路径无回归。
2. `go test ./...` 通过；
3. `go vet ./...` 通过；
4. `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/device-host-agent` 通过，并在 E4 Mac 实际运行；
5. OpenAPI frozen hash、Agent、Adapter、readiness 和全部既有契约测试通过。

## Secret 与边界核对

- 仓库扫描未发现 Mac 地址、SSH 密码、完整 Simulator UDID、硬件序列号、Apple Account、签名私钥、Provisioning Profile、Agent Token 或 Appium Credential；
- Adapter 请求不携带 Authorization Header；Node 只监听 loopback，避免触发上游 12.0.1 auth middleware 打印 token 的风险；
- Appium/Agent 原始本机日志留在权限受控的 DF-041 目录，不复制进仓库；本证据不包含完整 UDID、Host 地址或硬件序列号；
- 未新增 App、IPA、Build、Case、Run、RunAttempt、Result、Artifact、评分或报告；
- 未实现 iOS Session、`df:udids`/`appium:udid` Fence、WDA 签名、真机生命周期或人工远控；这些继续按 DF-042～DF-046 顺序开发；
- Appium Device Farm busy 仍只是宿主机技术状态，PostgreSQL Reservation 仍是唯一业务占用真相；Android 继续使用原有 Docker Emulator、独立 Appium Endpoint 和 STF。
