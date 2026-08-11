# DF-038 验收证据

## 本地门禁

- Go SDK：已安装到项目忽略目录 `.tmp/toolchains/go1.26.5/`，`go version go1.26.5 windows/amd64` 通过。
- `go test ./...`：通过（使用上述项目内 Go SDK）。
- `console: pnpm build`：通过（重新生成 OpenAPI Client、TypeScript 检查和 Vite 生产构建）。
- `console: pnpm test`：通过，7 个测试文件、30 个测试。
- migration up/down/up：已在部署主机 `10.0.30.171` 的临时 PostgreSQL 16 容器执行，完整 up/down/up 和约束检查通过；生产库已备份并成功应用 `000012`、`000013`。

## 已执行的真实环境验证

- 部署主机 `10.0.30.171` 已确认具备 Docker、`/dev/kvm`、STF 3.7.9、Appium 3.5.2、PostgreSQL 16 和运行中的 Host Agent；DF-038 Server 候选已通过 `/readyz`。
- API 36 缓存目录创建请求已复用已有 `device_image_preparations`，没有重复下发 `prepare_android_image`，并进入 `creating_emulator`；新增 Emulator 容器和 Appium 端口已实际创建。
- 本次第二 Emulator 在 ADB 就绪前退出，Host Agent 返回 `DEVICE_BOOT_TIMEOUT`。因此完整 KVM/ADB/STF/Appium 闭环及基础设备扩容验收仍未通过，不能标记 DF-038 完成。

## 真实环境待保存证据

- 释放预约前后设备内 APK、应用数据和文件校验；
- 显式 rebuild/reimage 后数据清空校验；
- 基础设备变更后扩容命令的 Image、Phone Profile 和 runtime profile；
- 已缓存和未缓存 `catalog_id` 的 provisioning job、页面刷新/同一幂等键重试不重复创建的数据库与命令时间线；
- 删除空闲设备后 Pool 目标降低且 Controller 未补建的数据库/命令时间线；
- Linux KVM 上第二实例的 ADB 启动失败根因、成功创建后的 STF、Appium 健康检查截图和脱敏日志。
