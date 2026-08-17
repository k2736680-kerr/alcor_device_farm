# Android 第一版归档记录

## 冻结基线

- Commit：`106e9dd7c83b8034fbf97baf7bde3979a9afcb10`；
- Tag：`archive/android-baseline-2026-08-17`；
- 后续本地开发分支：`codex/device-farm-v2`；
- 归档时工作区：干净；
- 远端操作：未推送、未修改远端分支。

## 本地归档产物

归档产物保存在 Git 工作区外的 `E:/AutoTestTools/Archives/alcor_device_farm/android-baseline-20260817-106e9dd/`，不纳入仓库：

| 产物 | 字节数 | SHA-256 |
|---|---:|---|
| `alcor_device_farm-android-baseline-20260817.bundle` | 4,124,612 | `F195F40F1C52EDAAF9413EF423B8B6AECF2D0F13791F45FE3AFD47A102523564` |
| `alcor_device_farm-android-baseline-20260817-source.zip` | 4,024,922 | `D52C5F04ED3856B855029BF337C5BF35B0848070DCDE524ECE29B6B213A6B34C` |

Git bundle 已执行完整历史验证并通过。源码 ZIP 从冻结 Tag 直接生成。

## 归档前门禁

- `go test ./...`：通过；
- Console `pnpm test`：7 个测试文件、31 个测试通过；
- Console `pnpm build`：通过；
- Git 引用对象检查和 `git diff --check`：通过；
- 工作区在测试和构建后保持干净。

本记录只证明代码、文档和 Git 历史归档。生产/测试宿主机上的 PostgreSQL、Registry、Docker 数据卷、STF/RethinkDB 和部署 Secret 必须按运维备份流程另行生成恢复点，不能用源码归档替代运行态备份。
