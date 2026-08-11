# DF-038 验收证据

## 本地门禁

- `console: pnpm build`：通过（生成 OpenAPI Client、TypeScript 检查和 Vite 生产构建）。
- `console: pnpm test`：通过，7 个测试文件、30 个测试。
- Go 测试：当前 Windows 开发环境未安装 Go SDK，待具备 Go/Linux KVM 环境时执行本任务的后端和真实设备验收。

## 真实环境待保存证据

- 释放预约前后设备内 APK、应用数据和文件校验；
- 显式 rebuild/reimage 后数据清空校验；
- 基础设备变更后扩容命令的 Image、Phone Profile 和 runtime profile；
- 删除空闲设备后 Pool 目标降低且 Controller 未补建的数据库/命令时间线；
- Linux KVM 上创建、STF、Appium 健康检查截图和脱敏日志。
