# DaFit Integration Harness

本目录只负责设备农场与`dafit_auto_platform`的端到端联调：

- 申请设备；
- 传入明确的UDID和Appium Endpoint；
- 调用DaFit现有运行入口；
- 收集现有HTML/JSON报告路径；
- 无论成功失败都释放设备。

禁止复制DaFit的页面、组件、场景、断言、数据和报告代码。

## 运行方式

Harness 使用 Device Farm 服务 Token，但不会把该 Token、数据库地址或 STF Token传给 DaFit 子进程。Token 只通过环境变量注入，禁止放到命令行参数：

```powershell
$env:DEVICE_FARM_SECURITY_SERVICE_TOKEN='通过部署 Secret 注入'

.\bin\dafit-farm-harness.exe `
  -server-url http://127.0.0.1:8080 `
  -pool-id pool_000000000000001 `
  -dafit-dir E:\AutoTestTools\Projects\dafit_auto_platform `
  -report-dir E:\AutoTestTools\Runs\dafit-attempt-001 `
  -python D:\python\python.exe `
  -adb E:\Android\platform-tools\adb.exe `
  -case STEPS_SMOKE_001
```

不传 `owner-id` 时自动生成测试 UUID/ULID。Harness 固定使用 `owner_type=test_run`，不会创建 Alcor Run、RunAttempt 或业务结果。

## 执行链路

1. 创建 pending Reservation；
2. 轮询至 active；
3. 查询该 Reservation 的 Device；
4. 从同一 Device 读取宿主机 `adb_endpoint`、容器内 `capabilities.appiumUdid` 和 `appium_endpoint`；
5. 对网络 ADB Endpoint 执行 `adb connect`；
6. 注入 `DAFIT_RUN_MODE=farm`、`ANDROID_ADB_SERIAL`、`ANDROID_UDID`、`APPIUM_SERVER` 和独立 `DAFIT_REPORT_DIR`；
7. 调用 DaFit 原有 `tools/run_full.py --case ...`；
8. 检查原有 `report.html` 和 `report.json`；
9. 在成功、失败、超时、取消或进程信号路径使用独立清理上下文释放 Reservation。

等待容量期间被取消时，Harness 会调用同一个 release 接口。尚未分配设备的 pending Reservation 转为 `failed/RESERVATION_CANCELED`；若 Scheduler 正在执行 STF claim，Harness 短时重试，直到 claim 完成后释放或 Reservation 已进入终态。

## 结果

Harness 在标准输出写一行 JSON，只包含 Reservation、Device 和报告路径；DaFit 的业务结果仍以原有报告为准。命令非零退出不等于预约未释放，释放失败会合并进最终错误并返回非零退出码。
