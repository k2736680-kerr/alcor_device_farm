# DF-038 验收证据

## 本地门禁

- Go SDK：已安装到项目忽略目录 `.tmp/toolchains/go1.26.5/`，`go version go1.26.5 windows/amd64` 通过。
- `go test ./...`：通过（使用上述项目内 Go SDK）。
- `console: pnpm build`：通过（重新生成 OpenAPI Client、TypeScript 检查和 Vite 生产构建）。
- `console: pnpm test`：通过，7 个测试文件、30 个测试。
- migration up/down/up：本机未发现 `initdb`、`pg_ctl` 或 `psql`，`scripts/verify-migrations.ps1` 无法在当前 Windows 环境启动临时 PostgreSQL；待具备 PostgreSQL 后执行。

## 真实环境待保存证据

- 释放预约前后设备内 APK、应用数据和文件校验；
- 显式 rebuild/reimage 后数据清空校验；
- 基础设备变更后扩容命令的 Image、Phone Profile 和 runtime profile；
- 已缓存和未缓存 `catalog_id` 的 provisioning job、页面刷新/同一幂等键重试不重复创建的数据库与命令时间线；
- 删除空闲设备后 Pool 目标降低且 Controller 未补建的数据库/命令时间线；
- Linux KVM 上创建、STF、Appium 健康检查截图和脱敏日志。
