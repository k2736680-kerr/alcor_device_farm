# DF-019 实施与验收证据

## 当前结论

DaFit 已在原有唯一 `tools/run_full.py` 执行链路上加入 Farm 模式，只接收外部明确的 UDID、Appium Endpoint 和本次报告目录，没有复制 Appium Session、页面、动作、断言、Runner 或报告。当前没有 Linux Device Farm、真实 Emulator 和远程 Appium 可做一次真实运行，因此 DF-019 状态为 `blocked`，不能标记 `completed`。

## 代码位置与提交

- 仓库：`E:/AutoTestTools/Projects/dafit_auto_platform`；
- 该仓库只有主分支 `main`，没有 `master`，因此修改直接提交到现有主分支，没有创建其他功能分支；
- commit：`2ae74ca 加入设备农场运行模式`。

## 已完成交付

- `DAFIT_RUN_MODE=local|farm`，默认 local；
- Farm 模式必须显式注入 `ANDROID_UDID`、`APPIUM_SERVER` 和绝对 `DAFIT_REPORT_DIR`；
- 缺少任一参数时在清理报告和连接设备前直接返回失败；
- Farm 指定 UDID 必须已存在于 `adb devices` 且 shell ready，禁止自动选择第一台真机或模拟器；
- Farm 只探测指定 Appium `/status`，不可用时失败，不在 DaFit 内启动远程 Appium；
- local 模式继续覆盖外部错误 App 包名/Activity，并保持真机优先、无真机时本地模拟器的原行为；
- Farm 参数优先于 `_scratch/local.yaml` 的 UDID/Appium 本机配置；
- pytest、runner、scenario、Appium Session、页面动作、断言、证据和 HTML/JSON 报告继续使用原实现；
- `DAFIT_REPORT_DIR` 只允许绝对的本次目录，拒绝磁盘根、用户目录、项目根和项目任意祖先目录，避免错误清理；
- DaFit 不申请或释放 Reservation，生命周期仍由后续 Device Farm Harness 的 `finally` 和 Reaper 管理。

## 本地验收结果

执行：

```powershell
python -m compileall -q core tools tests
python tools/validate_architecture.py
python tools/validate_feature_config.py
python -m pytest tests/unit -q
python tools/run_full.py --collect-only
```

通过：

```text
PASS architecture validation
PASS home/sleep/sport/steps/weight feature validation
PASS 308 unit tests
PASS Farm 缺少明确参数时在报告清理前失败
PASS Farm 指定设备不存在时不回退到其他 adb device
PASS Farm Appium 不可用时不自动启动服务
PASS Farm 只清理本次报告目录并拒绝项目祖先目录
PASS local 配置行为保持不变
PASS full_regression collect-only = 158 items（改造前后相同）
```

设备农场早期方案中的“原 26 个用例”是 DaFit 语言矩阵扩展前的旧基线。当前 DaFit 主分支已经按 46 语言动态展开为 158 个执行实例，因此 DF-019 使用修改前实测 158、修改后仍为 158 作为不回归证据，不把项目退回旧的 26 项结构。

## 真实 Farm 验收

1. 在 Linux Device Farm 取得一个 active Reservation；
2. 确认 Host/网络已经让明确 UDID 出现在 DaFit 运行环境的 `adb devices`；
3. 注入该 Reservation 对应的 Appium Endpoint 和独立报告目录；
4. 执行一个不改业务架构的现有冒烟 case；
5. 确认只连接分配的 UDID，不受同机其他设备影响；
6. 确认 DaFit 没有创建本地 Emulator 或启动远程 Appium；
7. 确认 `report.html`、`report.json`、截图、XML 和 run.log 全部进入本次目录；
8. 不设置 UDID、设置错误 UDID、设置不可用 Appium 分别验证快速失败；
9. 执行本地模式，确认真机优先和模拟器回退行为不变；
10. 保存脱敏命令、ADB、Appium、报告路径和执行结果证据。

## 阻塞解除条件

DF-015 的真实 Appium Endpoint 验收完成，并在 Device Farm 分配的 Emulator 上通过上述冒烟和本地回归后，将 DF-019 改为 `completed`。单元测试和 collect-only 不能替代真实设备运行。
