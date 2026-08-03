# DaFit Integration Harness

本目录只负责设备农场与`dafit_auto_platform`的端到端联调：

- 申请设备；
- 传入明确的UDID和Appium Endpoint；
- 调用DaFit现有运行入口；
- 收集现有HTML/JSON报告路径；
- 无论成功失败都释放设备。

禁止复制DaFit的页面、组件、场景、断言、数据和报告代码。
