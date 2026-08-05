# DF-026 Device Farm Console 工程和只读页面验收

## 状态

in_progress。已确认使用 E0 本地 PostgreSQL、Mock Provider 和浏览器自动化完成本任务，不等待 Linux/STF/Appium 真实验收。

## 目标

建立 `console` React + TypeScript 工程、浏览器安全访问方式、统一 API Client 和错误处理，并完成总览、Image、Host、Pool、Device、Reservation 的列表与详情只读页面。

## 必须保存的证据

- 前端依赖安装、构建和组件测试结果；
- Mock Server 页面测试和关键页面截图；
- 未认证访问、会话过期和权限拒绝结果；
- 浏览器网络、存储和构建产物的 Token 扫描结果；
- 页面资源状态与 Device Farm API 一致性检查；
- 临时服务和测试资源清理结果。

## 完成条件

满足 `docs/05_step_by_step_implementation.md` 中 DF-026 的全部验收条件后，更新本文件为 completed，并记录版本、环境、命令、结果、已知问题和独立中文 Git commit。
