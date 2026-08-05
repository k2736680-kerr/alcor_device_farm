# ADR-0002：复用优先，设备农场不复制执行与报告能力

## 状态

已确定；“不创建控制台”的目录约束已由 ADR-0009 修订，其他复用边界继续有效。

## 决策

- 远程控制、设备Inventory和远程ADB复用STF；
- Android自动化协议复用Appium 2和UiAutomator2；
- DaFit业务自动化复用`dafit_auto_platform`的Runner、页面、动作、断言、证据和报告；
- 新版 Alcor 的 Case、Dataset、Target、Config、Run、RunAttempt、Result 和 Artifact 等业务对象等待其接口，不在设备农场预建；
- 本项目只新增现有系统缺失的设备Host、Provider、Pool、Reservation、Scheduler、Reconciler、Reaper、Adapter和设备域控制后台。

## 目录约束

- 使用`adapters/stf`，不得出现第二套STF服务或远控前端；
- 使用`adapters/appium`，其中不得出现DaFit页面、Locator、业务步骤或报告；
- 允许创建`console`目录实现设备域控制后台，但不得包含 Alcor 的 Case、Dataset、Target、Config、Run、Result、Artifact、评分、门禁或业务报告页面；
- 控制台只能通过设备农场 Server API 操作资源，STF 看屏、日志和远控继续复用 STF，不得在控制台重写；
- `harness/dafit`只能做联调参数传递与产物收集，不保存DaFit业务用例。

## 例外流程

确实无法复用时，必须先在`docs/01_existing_capability_reuse_matrix.md`记录差异，并新增ADR说明为什么Adapter不足，再开始编码。
