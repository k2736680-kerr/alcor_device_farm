# ADR-0002：复用优先，设备农场不复制执行与报告能力

## 状态

已确定。

## 决策

- 远程控制、设备Inventory和远程ADB复用STF；
- Android自动化协议复用Appium 2和UiAutomator2；
- DaFit业务自动化复用`dafit_auto_platform`的Runner、页面、动作、断言、证据和报告；
- 新版 Alcor 的 Case、Dataset、Target、Config、Run、RunAttempt、Result 和 Artifact 等业务对象等待其接口，不在设备农场预建；
- 本项目只新增现有系统缺失的设备Host、Provider、Pool、Reservation、Scheduler、Reconciler、Reaper和Adapter。

## 目录约束

- 使用`adapters/stf`，不得出现第二套STF服务或远控前端；
- 使用`adapters/appium`，其中不得出现DaFit页面、Locator、业务步骤或报告；
- 不创建`console`目录；未来设备管理页面进入新版 Eval Console；
- `harness/dafit`只能做联调参数传递与产物收集，不保存DaFit业务用例。

## 例外流程

确实无法复用时，必须先在`docs/01_existing_capability_reuse_matrix.md`记录差异，并新增ADR说明为什么Adapter不足，再开始编码。
