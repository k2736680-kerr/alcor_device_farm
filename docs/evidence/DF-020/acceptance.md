# DF-020 实施与验收证据

## 当前结论

DF-020 已在真实 Linux KVM、Docker Android 16 Emulator、STF 3.7.9、独立 Appium 3.5.2 和 DaFit 主线入口上完成验收，状态为 `completed`。

Harness 已覆盖申请、等待、连接信息注入、DaFit 执行、独立报告目录、finally release、等待超时、运行超时、可处理终止和进程强制终止。强制终止后，Reservation 由 Server Reaper 通过正式 STF Adapter 和 Repository 状态机回收，没有直接修改数据库。

## 真实环境

- Device Farm Server：`http://10.0.30.171:18080`；
- Linux/KVM Host：`10.0.30.171`，`kerr` 属于 `kvm`、`docker` 组；
- Emulator：Android 16 / API 36 / x86_64，独立 ADB 与 Appium 动态端口；
- STF：DeviceFarmer/STF 3.7.9；
- Appium：3.5.2；
- DaFit：`D:/AutoTestTools/Projects/dafit_auto_platform` 主线 `tools/run_full.py`，未复制 Runner、页面、动作、断言或报告实现；
- 逻辑设备池：`8b97a9e7-ab6c-41ff-bc1e-bd922b829ab3`；
- 验收日期：2026-08-06。

## 场景结果

| 场景 | Reservation | 结果 |
|---|---|---|
| 无人值守成功冒烟 | `46d6f634-14a1-4518-a10b-ff07676ba022` | DaFit `STEPS_SMOKE_001` 为 1 passed；HTML/JSON 报告生成；finally 自动释放 |
| 故意失败 | `d6f5948c-58b4-43d3-b5cc-2138606b648d` | 保留首次启动状态后 DaFit 返回失败；HTML/JSON 报告保留；finally 自动释放 |
| 等待 active 超时 | `d27ac5f9-6f68-4a92-bc22-22bc2d93cef4` | pending 被正式取消，最终 `failed/RESERVATION_CANCELED` |
| 运行前置失败 | `f6b7798f-0a96-430c-9d7e-cd1f94de659f` | App 未安装时 Harness 明确失败，Reservation 仍自动释放 |
| 5 秒 DaFit 运行超时 | `d994f4ca-337d-4f75-9523-9f1da02d94f1` | 约 6 秒终止子进程；未生成报告时明确报错；Reservation 自动释放 |
| 可处理的任务中断 | `208aed72-73c9-4d4a-9363-8183012138bd` | 终止信号进入 Harness signal/finally 路径，最终 released |
| Harness 与 Python 被强制杀死 | `4b8f045e-3ba1-47d9-95c7-85e7604fb7b4` | 进程无法执行 finally；租约过期后由 Reaper 回收为 expired，并触发设备 rebuild |

成功报告目录：

```text
D:\dafit-farm-runs\df020-success-20260806-02
```

故意失败报告目录：

```text
D:\dafit-farm-runs\df020-failure-20260806
```

## 强制终止与 Reaper 兜底

强制杀死 Harness 和 Python 后，STF 已经显示设备空闲：

```text
present=true
ready=true
using=false
owner=null
GET /api/v1/user/devices -> devices=[]
```

STF 3.7.9 对重复 release 返回：

```text
HTTP 403
{"success":false,"description":"You cannot release this device. Not owned by you"}
```

旧 Adapter 只把 DELETE 404 视为幂等成功，导致已释放设备的 Reservation 保持 active。修复后只在以下条件同时满足时接受该 403：

1. release 的 HTTP 状态确实为 403；
2. 随后通过官方 inventory 查询到同一 serial；
3. inventory 明确返回 `using=false`。

如果 inventory 失败、设备不存在于结果中或仍为 `using=true`，403 继续作为 `STF_RELEASE_FAILED` 返回，不能笼统吞掉拒绝响应。对应测试同时覆盖“已空闲成功”和“仍占用保持失败”。

部署 `alcor-device-farm:df020-releasefix-20260806` 后，Server 启动不到 1 秒记录：

```text
expired device reservation reaped
reservation_id=4b8f045e-3ba1-47d9-95c7-85e7604fb7b4
device_id=67434725-32ff-4870-8c55-ad46fd5d9486
```

只读查询确认最终 Reservation 为：

```text
4b8f045e-3ba1-47d9-95c7-85e7604fb7b4 | expired
```

## 回归门禁

使用固定 Go 1.24.6 工具链和隔离源码上下文执行：

```text
gofmt -l internal/adapters/stf/client.go internal/adapters/stf/client_test.go
go test ./internal/adapters/stf -count=1
go test ./internal/... -p=1 -count=1
```

结果：gofmt 无输出，定向 STF Adapter 测试通过，`internal/...` 全部通过。构建上下文基于提交 `e37d13f`，仅叠加本任务的 STF Adapter、测试和文档修改，没有混入工作区中未完成的 DF-028 Console 改动。

## 最终清理与后续项

Reaper 触发 rebuild 后，设备获得新连接：

```text
ADB     10.0.30.171:32799
Appium http://10.0.30.171:32798/status -> HTTP 200, ready=true
STF     present=true, ready=true, using=false
```

本轮再次复现“rebuild 完成时 STF 尚未重新连接，设备先变为 ready/unhealthy，随后被隔离”的时序问题。验收使用现有 `stf-connect-emulators.sh` 和正式 `DELETE /api/v1/devices/{id}/quarantines` 恢复，最终设备为 `ready/healthy`。该自动重连和健康判定竞态属于 DF-021 的故障恢复范围，不在 DF-020 中用数据库修改或放宽健康条件规避。

DF-020 的验收目标已满足：成功、故意失败、超时和中断路径均无永久 active Reservation；强制终止由 Reaper 兜底回收；DaFit 报告属于对应运行目录；清理使用正式 Adapter/API 链路。
