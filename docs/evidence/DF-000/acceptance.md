# DF-000 验收记录

日期：2026-08-03  
任务：环境与依赖盘点

## 验收结论

通过。当前可执行范围、缺失依赖和 Linux KVM 外部环境要求已经明确；未修改 DaFit 或 Alcor，未保存真实密钥。

## 验收项

| 验收项 | 结果 | 证据 |
|---|---|---|
| 项目、DaFit、Alcor 路径和分支确认 | 通过 | `docs/environment_inventory.md` 第 6 节 |
| 本机工具、资源和端口盘点 | 通过 | `docs/environment_inventory.md` 第 2、3 节 |
| Windows/Mock 与 Linux KVM 验收边界明确 | 通过 | `docs/environment_inventory.md` 第 4、7 节 |
| DaFit 当前入口可用 | 通过 | collect-only 收集 26 个用例 |
| DaFit Farm 参数差距明确 | 通过 | 已支持 UDID/Appium；缺 Farm 禁止自动选择/启动和 REPORT_DIR |
| 敏感信息未写入文档 | 通过 | 完整设备序列号和 Secret 均未保存 |

## 已确认阻塞项

- DF-001 开始前需要 Go 工具链；该项已在 DF-001 使用官方便携 Go 1.26.5 解决；
- DF-004 开始前需要 PostgreSQL 测试实例；
- DF-014 真实验收前需要 Linux KVM Host 和 Docker Engine；
- ALCOR-001 等待新版 Alcor 实际 OpenAPI。

这些阻塞不影响 DF-000 完成，也不影响在补齐 Go 后推进 DF-001~DF-003、DF-005 和 Mock 相关开发。

## 仓库保护

- `dafit_auto_platform` 保持 `main` 和干净工作区；
- `Alcor` 未跟踪的 `alcor_console/`、`docs/` 未修改；
- `alcor_device_farm` 仍在 `master`。
