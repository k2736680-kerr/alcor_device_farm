# DF-039 第二版多平台宿主机与 iOS 接入设计验收

## 当前状态

`in_progress`

DF-039 已进入设计验收。本任务只开展 Appium Device Farm、Appium 3、XCUITest、WebDriverAgent、go-ios、macOS/Xcode、iOS 真机与 Simulator 的复用调查和架构设计；没有修改设备 API、数据库 migration、领域状态机、Host Agent 或 Provider 代码。

## 已形成的设计

- `docs/adr/0021_ios_host_integration_and_session_ownership.md`：唯一占用真相、宿主机组件和 Session UDID 约束；
- `docs/08_ios_device_farm_v2_design.md`：版本、模型、Host、健康、签名、安全、时序和回滚；
- `docs/09_ios_device_farm_v2_acceptance.md`：E4/E5/E6、G10～G16、P0/P1 和证据标准；
- `docs/05_step_by_step_implementation.md`：DF-040～DF-046 的实现顺序；
- `docs/evidence/DF-039/source_and_reuse_inventory.md`：官方来源与三仓库复用结论；
- `docs/evidence/DF-039/environment_inventory.md`：当前真实环境和实施前缺口。

## 设计验收检查

- [x] PostgreSQL Reservation/Scheduler/Pool/Lease/Reaper/审计仍是唯一设备占用真相；
- [x] Appium Device Farm 固定为宿主机侧 inventory、技术 busy 和 Session 路由，不启用跨 Host 自由分配；
- [x] iOS Session 强制 active Reservation、`appium:udid` 与单元素 `df:udids` 四方一致；
- [x] macOS Host、Simulator、真机、WDA 签名、健康、释放和故障收敛均有明确方案；
- [x] Appium Device Farm 12.x 人工串流移除已记录，iOS 首期明确不提供远控；
- [x] Alcor、DaFit、STF、Appium 的已有职责没有复制；
- [x] 后续生产实现拆成 DF-040～DF-046，且每项有独立验收入口；
- [x] 没有写入真实凭证或修改生产代码/migration；
- [ ] 保存本次设计提交并回填可达 commit；
- [ ] 实施证据门禁通过后将 DF-039 标记 completed。

## 必须保存的证据

- Android 第一版归档 commit、Tag 和恢复验证；
- Alcor、DaFit 与当前设备农场的复用搜索结果；
- Appium Device Farm 固定版本、官方能力、已知限制和许可证核对；
- macOS/Xcode、证书、Provisioning Profile、WDA 和测试设备环境盘点；
- 我方 Reservation 与宿主机 Session 路由之间的唯一占用时序；
- Android 回归范围、iOS 真机/Simulator 真实验收矩阵和回滚方案。

以上最后两项完成前，本任务保持 `in_progress`。
