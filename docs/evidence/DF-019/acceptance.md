# DF-019 实施与验收证据

## 当前结论

DF-019 已于 2026-08-06 在 Device Farm 分配的真实 Android 16 Emulator 和远程 Appium Endpoint 上完成 DaFit Farm 模式验收，状态改为 `completed`。

DaFit 继续复用唯一的 `tools/run_full.py → runner → scenarios → pages/components/core → reporting` 链路。本次没有新增或复制 Appium Session、页面对象、元素动作、断言、Runner、证据或 HTML/JSON 报告实现。

## 代码位置与既有提交

- 仓库：`D:/AutoTestTools/Projects/dafit_auto_platform`；
- 当前分支：`main`；
- Farm 薄适配既有提交：`2ae74ca 加入设备农场运行模式`、`d211eb2 区分Appium设备和ADB连接`；
- 本次真实验收不需要修改 DaFit 代码，DaFit 工作树最终保持 clean。

上述既有提交在本次验收前已经进入 DaFit `main`，因此没有重写历史或把后续 DaFit 修改回退到旧 feature 分支；真实验收证据和 DF 状态在本仓库单独提交。

## Farm 薄适配范围

- `DAFIT_RUN_MODE=local|farm`，默认 local；
- Farm 模式必须显式注入 Appium 使用的 `ANDROID_UDID`、宿主机 ADB 使用的 `ANDROID_ADB_SERIAL`、`APPIUM_SERVER` 和绝对 `DAFIT_REPORT_DIR`；
- Docker 内 Appium UDID 与宿主机随机 ADB Endpoint 可以不同；本地模式下两者默认相同；
- 缺少任一参数时在清理报告和连接设备前直接返回失败；
- Farm 指定 ADB serial 必须已存在于 `adb devices` 且 shell ready，禁止自动选择第一台真机或模拟器；
- Farm 只探测指定 Appium `/status`，不可用时失败，不在 DaFit 内启动远程 Appium；
- local 模式继续保持真机优先、无真机时本地模拟器的原行为；
- Farm 参数优先于 `_scratch/local.yaml` 的 UDID/Appium 本机配置；
- `DAFIT_REPORT_DIR` 只允许绝对的本次目录，拒绝磁盘根、用户目录、项目根和项目祖先目录；
- DaFit 不申请或释放 Reservation，生命周期由 Device Farm Harness/调用方的 finally 和 Reaper 管理。

## 真实环境

| 项目 | 验收值 |
|---|---|
| Reservation | `7246811b-08ba-4b24-a73c-ba1a5ae5380d` |
| Device | `67434725-32ff-4870-8c55-ad46fd5d9486` |
| Appium UDID | `emulator-5554` |
| Host ADB serial | `10.0.30.171:32785` |
| Appium Endpoint | `http://10.0.30.171:32784` |
| Da Fit package | `com.crrepa.band.dafit` |
| Da Fit version | `2.9.18-41-g0aff0abfdc-dirty` |
| APK SHA-256 | `196A0402148C8992FA56F238EE2C96A0CAE0CF5FDCE21983D057DF0F7B4D929B` |

Reservation 从 pending 经真实 STF claim 后进入 active，绑定的 Device 与上表一致。Windows Harness 环境通过 ADB TCP 只连接 `10.0.30.171:32785`，Appium `/status` 返回 `ready=true`、版本 `3.5.2`。

## 真实 DaFit 冒烟

使用现有正式入口和现有配置用例：

```powershell
$env:DAFIT_RUN_MODE='farm'
$env:ANDROID_UDID='emulator-5554'
$env:ANDROID_ADB_SERIAL='10.0.30.171:32785'
$env:APPIUM_SERVER='http://10.0.30.171:32784'
$env:DAFIT_REPORT_DIR='D:\dafit-farm-runs\df019-20260806-02'
python tools/run_full.py --case STEPS_SMOKE_001
```

第一次运行保存在独立目录 `df019-20260806-01`，Appium 已成功建立到指定设备的会话，但新安装 App 仍停留在隐私政策和首次引导，随后 Android 出现一次 `System UI isn't responding` 对话框，因此原用例按真实结果失败。该失败报告没有被删除或覆盖。

完成隐私确认、跳过首次资料、权限提示和设备绑定引导后，不修改 DaFit 用例或执行代码，第二次运行通过：

```text
collected 1 item
STEPS_SMOKE_001 PASSED
1 passed in 72.76s
Farm 指定设备：Appium UDID=emulator-5554，ADB Serial=10.0.30.171:32785
```

`report.json` 的结构化结果：

```text
case_stats: total=1 passed=1 failed=0 skipped=0
assertion_stats: PASS=35 FAIL=0 SKIP=0 UNKNOWN=0
group_stats: PASS=3 FAIL=0 SKIP=0 UNKNOWN=0
business_points: total=3 passed=3 failed=0
```

成功目录包含 `report.html`、`report.json`、`run.log`、2 张截图和 2 份 XML。失败和成功使用两个独立目录，证明 Farm 报告不会写回项目默认报告目录，也不会覆盖另一运行的证据。

## 参数错误和禁止回退

三项真实命令均在执行 UI 用例前失败：

```text
缺少 ANDROID_UDID：Farm 模式缺少显式参数：ANDROID_UDID
错误 ADB serial：Farm 指定设备 10.0.30.171:39999 未出现在 adb devices，禁止自动选择其他设备
不可用 Appium：Farm 指定 Appium 不可用：http://10.0.30.171:39998
```

缺少 UDID 时没有创建报告目录；错误 ADB serial 时没有回退到当前可用的 `32785`；Appium 不可用时没有在 DaFit 内启动本地或远程 Appium。

## 本地回归

执行并通过：

```text
python -m compileall -q core tools tests
python tools/validate_architecture.py
python tools/validate_feature_config.py
python -m pytest tests/unit -q
python tools/run_full.py --collect-only
```

结果：

```text
architecture validation PASS
home/sleep/sport/steps/weight feature validation PASS
318 unit tests passed
full_regression collect-only = 158 items
```

单元测试首次使用系统默认 pytest 临时目录时遇到 Windows 目录权限错误；将 `TEMP/TMP` 指向本次受控临时目录后，同一 318 项测试全部通过，证明不是代码失败。

设备农场早期方案中的“26 个用例”是 DaFit 语言矩阵扩展前的旧基线。当前正式入口仍收集 158 个执行实例，与 Farm 改造后的既有基线一致。

## 释放和清理

- Reservation 通过正式 release API 进入 released；
- release 后 Device 按既有 rebuild 模式进入 recycling，约 178 秒后重新回到 ready/healthy；
- rebuild 后 ADB serial 变为 `10.0.30.171:32787`，Appium Endpoint 变为 `http://10.0.30.171:32786`；
- 新 Emulator 中已不存在 `com.crrepa.band.dafit`，说明本次安装和 App 数据没有泄漏到下一次预约；
- 新 Appium `/status` 仍返回 `ready=true`。

## 验收结论

DF-019 的外部 UDID、外部 ADB serial、远程 Appium、独立报告目录、错误参数快速失败、禁止设备回退、158 条收集基线、本地单元回归和真实 DaFit 冒烟全部通过。DaFit 只做运行时薄适配，没有发展为第二套 Device Farm 客户端或复制执行框架。
