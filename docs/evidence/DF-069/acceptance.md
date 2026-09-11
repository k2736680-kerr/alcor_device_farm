# DF-069 控制台本地 E2E 启动与发布前回归

状态：通过（本机隔离 PostgreSQL + Mock STF 的 Console E0 E2E 已完成；真实 Linux/KVM 设备用例按环境开关跳过）。

## 已完成

- 修复 Windows Playwright `webServer` 默认可执行文件路径传递，避免把带引号路径识别成命令名。
- 本地 E2E 脚本临时关闭 Corepack project-spec 强制版本检查，并在结束时恢复环境变量；兼容当前 pnpm 11.x 工具链。
- 本地 E2E 脚本会先按文件名顺序应用全部 migration，再导入固定 fixture，避免新建测试库因缺少 `device_console_sessions` 等表而提前失败。
- 保留 loopback 数据库校验，只允许 `device_farm_*` 测试库；不会连接 220、171 或生产数据库。

## 验证

| 检查 | 结果 |
|---|---|
| Console Vitest 全量 | 通过，10 个测试文件、60 个测试 |
| Console TypeScript/Vite 构建 | 通过 |
| Orval `generate:check` | 通过 |
| Go 全量测试与 `go vet` | 通过 |
| Playwright 本地 E0 E2E | 通过，4 个用例；4 个真实设备用例按环境开关跳过 |
| E2E 清理检查 | 通过，开放预约、活跃会话和 reserved/busy 设备均为 0 |

## 待真实环境执行

需要本机启动隔离 PostgreSQL（默认 `127.0.0.1:55432/device_farm_df004`）后运行：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/run-console-local-e2e.ps1
```

该步骤属于发布前回归，不执行正式切换。
