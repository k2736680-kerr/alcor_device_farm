# DF-037 宿主机容量不足提示修复验收

日期：2026-08-17

## 结论

通过。设备目录式创建在宿主机内存、磁盘、CPU 或设备名额不足时进入可重试的 `waiting_capacity` 状态，保存结构化缺口，并在 Device Farm Console 中显示中文提示。容量不足期间不创建设备、Pool membership 或 `create` Host Command；宿主机容量恢复后，原任务自动继续既有创建链路并清除等待原因。

## 自动化证据

- PostgreSQL 17.10 临时数据库完成全部 migration `up → down → up`；
- `TestCatalogProvisioningReportsCapacityShortfallAndResumesAfterRecovery/内存不足`：通过；
- `TestCatalogProvisioningReportsCapacityShortfallAndResumesAfterRecovery/磁盘不足`：通过；
- Repository、Scheduler、Reaper、Reconciler、Host Command、Metrics、API、Warm Pool 和 iOS Session Fence 的真实 PostgreSQL 集成门禁全部通过；
- `DevicesPage.test.tsx` 共 11 项通过，包含内存还缺 2048 MB、磁盘还缺 8192 MB 的中文页面提示；
- TypeScript 类型检查通过，OpenAPI 2.2.0 生成客户端与冻结摘要同步。

## 安全与边界

- 容量等待仍属于 Device、Host 和 provisioning job 的设备域技术状态；
- 未新增 Alcor Run、Result、Artifact 或业务执行能力；
- 未把 Mock 结果作为真实基础设施验收，容量状态恢复由真实 PostgreSQL 持久化测试覆盖；
- 验收未使用生产数据库，临时 PostgreSQL 在测试结束后已停止并清理。
