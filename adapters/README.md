# Adapters

本目录只封装外部系统：

- `stf`：调用DeviceFarmer/STF现有API；
- `appium`：管理Appium Endpoint、端口和健康状态。
- `alcor`：新版 Alcor Worker 使用的 RunAttempt 预约示例客户端、错误映射和独立 Mock Server。

禁止在这里实现远程画面、业务WebDriver步骤、DaFit页面、Locator、断言或报告。

Alcor 接入包只编排现有设备预约、查询、续租、释放和设备查询 API，不保存 Case、Run、RunAttempt 或结果，不读取 Alcor 数据库。使用说明见 [新版 Alcor Adapter 契约包](../docs/alcor_adapter_contract.md)。
