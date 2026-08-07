# DF-030 隔离设备受控人工删除

## 验收范围

本任务为 Device Farm Console 增加隔离或已停止设备的安全删除闭环。删除仍复用既有 Host Command、Host Agent 和 Docker Emulator Provider，不让浏览器或 Server 访问 Docker Socket，也不物理删除 Device 历史记录。

## 实现结果

- 新增 `DELETE /api/v1/devices/{id}`，强制操作原因、操作者和 `Idempotency-Key`；
- 仅 `quarantined/stopped` 可以提交，其他生命周期返回 409；
- Store 在同一 PostgreSQL 事务中检查 pending/active Reservation、禁用 Pool membership、创建 management delete Command 并写设备审计；
- Agent 成功返回 `deleted=true` 后，Server 将 Device 转为 `deleted` 并清空 STF、ADB、Appium Endpoint；
- 可重试删除失败达到三次后，Device 保持或回到 `quarantined/unhealthy` 并写健康事件；
- Console 在隔离或已停止设备行显示“删除”，先填写原因，再进行危险操作二次确认；设备列表每 5 秒刷新；
- 固定目标数量不随单设备删除改变，目标未降低时允许 Warm Pool 自动补建。

## 自动化验证

2026-08-07 本地验证：

```text
PASS gofmt -l .（无输出）
PASS go test -count=1 -p=1 ./...
PASS go vet ./...
PASS go build ./...
PASS Vitest：7 files / 14 tests
PASS Orval + TypeScript + Vite production build
WARN Vite 单入口 bundle > 500 kB（既有非阻塞项）
```

使用 PostgreSQL 17.10 创建独立临时数据库，只执行 migration 和 `TestManagementAPICompleteMockFlow`；测试结束后停止 PostgreSQL 并删除临时数据目录，未连接生产数据库：

```text
PASS TestManagementAPICompleteMockFlow
```

该事务级用例覆盖：

- ready 设备删除返回 409；
- pending/active Reservation 阻止删除；
- 同一幂等键重放只产生一条 delete Command；
- 首条 delete Command 仍处于 pending/leased 时，即使更换幂等键也拒绝生成第二条命令；
- 提交删除后 Pool membership 立即禁用；
- `deleted=true` 后状态转为 deleted、Endpoint 清空并保留一条 `delete_device` 审计；
- 可重试 Provider 故障连续三次后收敛为 quarantined/unhealthy。

Console 用例覆盖删除按钮、原因必填、二次确认、DELETE 请求和成功提示。

## Linux 部署验证

2026-08-07 部署到 Linux KVM 验收主机 `10.0.30.171`：

```text
Server container: alcor-device-farm-server-df017
Server image: alcor-device-farm:df030-manual-delete-v2-20260807
Image ID: sha256:c1b97084e383d90c957c05eb1d346b23e579ecfbd8c57fc6f6a3955a5c4b242e
Rollback container: alcor-device-farm-server-df030-v1-rollback-20260807（stopped）
/readyz=200
/console/=200
```

新镜像先用同一生产配置和只读 secrets volume 启动 canary，`/readyz` 通过后才替换正式容器。正式入口中的 Console bundle 已确认包含“确认删除这台设备”交互。

使用当前 ready 设备执行保护性 DELETE 验证，返回：

```text
HTTP 409
error_code=INVALID_STATE_TRANSITION
```

验收时生产环境没有 quarantined/stopped 设备，因此没有为测试制造假设备、没有隔离当前可用 Emulator，也没有改变设备池目标。成功删除的 Agent/Docker Provider 真实资源清理路径与 DF-029 已验收的自动缩容 delete Command 完全复用；本任务新增的人工删除事务编排和成功/失败收敛由上述 PostgreSQL 集成测试覆盖。

部署后保持用户此前设置的固定目标 `min_ready/max_instances=1/1`，当前实际 `ready=1`；部署未触发额外扩缩容。

## 结论

DF-030 验收通过。管理员现在可以直接在“隔离设备”列表中填写原因并二次确认删除；正常或仍被预约的设备不会被误删，失败也不会伪造为成功。
