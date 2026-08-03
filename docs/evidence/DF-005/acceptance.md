# DF-005 验收证据

## 交付内容

- `internal/domain`：Image、Host、Pool、Device、Reservation、Session、Host Command 聚合与类型化状态；
- 通用状态机：校验 ID、目标状态、原因和时间，统一产生 `TransitionEvent`；
- Device 双状态：`lifecycle_status` 与 `health_status` 分开维护；
- `docs/domain_state_machines.md`：Device 状态图和其他资源合法转换表；
- 表驱动测试：对 8 组状态机的所有状态组合进行穷举验证。

## 核心保证

- 领域对象不暴露状态字段和任意字符串 setter；
- 只有定义过的转换会成功，成功后状态改变并产生 `accepted=true` 事件；
- 非法转换返回 `ErrInvalidTransition`，状态保持不变，并产生 `accepted=false / INVALID_STATE_TRANSITION` 拒绝事件；
- 空原因、空时间、未知状态和非法 ID 会被拒绝，不污染领域事件；
- Device 只有同时满足 `ready + healthy` 才返回可调度；
- 健康变化不会偷偷改变生命周期；
- quarantined Device 不能直接进入 ready，只能先进入 provisioning 执行批准的 rebuild。

## 验收测试

执行：

```powershell
go test -v ./internal/domain
```

覆盖：

- Image 5×5；
- Host 4×4；
- Pool 2×2；
- Reservation 6×6；
- Session 5×5；
- Command 6×6；
- Device lifecycle 9×9；
- Device health 4×4。

共 239 组状态对全部验证：合法转换成功，所有未列入白名单的转换失败。额外测试覆盖 readiness 健康门槛、quarantined 防绕过、无效参数不变性、事件只读副本和事件清理。

执行完整工程检查：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/dev.ps1 -Task check
```

结果：`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。

## 验收结论

DF-005 的领域状态机满足方案与数据库状态约束，非法状态不能绕过领域对象写入，可以进入 DF-006 Repository、事务和幂等基础开发。
