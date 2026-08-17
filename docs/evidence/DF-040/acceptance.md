# DF-040 平台中立设备域模型与契约验收

## 当前状态

`pending`

验收入口：migration up/down/up、Android 回填与回归、iOS Pool/Device 平台约束、共享 Appium Endpoint、唯一 UDID 和 Mock 并发预约。未修改代码前保持 pending。

必须保存 migration 前后数据校验、OpenAPI 变更、领域/Repository/Scheduler 测试、100 并发预约结果和 Android 全量回归。共享 Endpoint 只能放宽 Endpoint 索引，不能放宽 serial、Provider identity 或 active Reservation 唯一性。全部标准通过并回填独立提交前不得改变状态。
