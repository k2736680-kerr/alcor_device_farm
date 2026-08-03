# 设备农场开发环境清单

盘点日期：2026-08-03  
对应任务：DF-000

## 1. 结论

当前 Windows 机器可以立即用于文档、Go 控制面、Mock Provider、OpenAPI、状态机和 DaFit 薄适配开发。DF-001 已提供便携式 Go 工具链；当前仍缺少 Docker 和 PostgreSQL 客户端，开始 DF-004 前需要提供 PostgreSQL 测试实例。

真实 Docker Android Emulator 不能在本机标记验收完成。DF-014 及之后的 Emulator 验收必须准备能够访问 `/dev/kvm` 的 Linux Host；STF/Appium/DaFit 完整链路在该 Host 或同一可达内网环境验证。

## 2. 本机资源

| 项目 | 当前值 | 判断 |
|---|---|---|
| 操作系统 | Windows 11 专业版，64 位 | 可做 E0 Mock 开发 |
| CPU | Intel Core i5-12400，12 逻辑核 | 足够本地编译和 Mock 测试 |
| 内存 | 31.8 GB | 足够本地开发 |
| E 盘剩余 | 78.3 GB | 足够代码和普通制品；不作为多镜像生产 Host |
| Hypervisor | Windows 报告已存在 | 不能等价证明 Linux `/dev/kvm` 可用 |
| 相关端口 | 5432、8080、7100、4723~4730 当前未监听 | 可作为开发默认值，启动服务时仍需再次检查 |

## 3. 工具和依赖

| 工具 | 当前状态 | 版本/说明 | 后续处理 |
|---|---|---|---|
| Git | 可用 | 2.53.0.windows.1 | 直接使用 |
| Go | 便携工具链可用，不在系统 PATH | 官方 Go 1.26.5，SHA-256 已校验；项目语言级别 1.24 | 通过 `DEVICE_FARM_GO` 由开发脚本使用 |
| Docker CLI/Engine | 未安装 | 无服务、常见安装路径不存在 | Windows 不作为真实 Emulator 验收环境；Linux Host 单独准备 |
| PostgreSQL/psql | 未安装 | 无服务 | DF-004 前提供测试 PostgreSQL |
| Python | 可用 | 3.12.10 | DaFit 使用 |
| uv | 可用 | 0.11.28 | DaFit 使用 |
| Node.js | 可用 | 24.14.0 | Appium 使用 |
| npm | 可用 | 11.9.0 | Appium 使用 |
| Appium | 可用 | 3.3.1 | 本地 DaFit 可用；设备 Host 版本在 DF-015 做兼容性固定 |
| UiAutomator2 Driver | 可用 | 8.1.2 | 本地 DaFit 可用 |
| ADB | 可用 | 1.0.41 / platform-tools 37.0.0 | 设备检查可用 |
| Java | 可用 | Temurin 17.0.19 | Android 工具可用 |

Appium 设备方案原文写的是 Appium 2，而当前已验证环境安装 Appium 3.3.1。当前不降级或复制安装；DF-015 必须用 DaFit 冒烟和并发 Session 实测后固定 Host 镜像版本，并把最终版本写入部署清单。

## 4. WSL 与 Linux KVM

- WSL2 注册了 Ubuntu，但当前启动失败，错误指向发行版虚拟磁盘路径不可用；
- 因 WSL 无法启动，未能验证其中的 Go、Docker、PostgreSQL 或 `/dev/kvm`；
- 即使修复 WSL，也不能在未验证 `/dev/kvm` 和嵌套虚拟化前把它当作 DF-014 验收 Host；
- 真实 Host 最少必须验证：Linux、Docker Engine、`/dev/kvm`、x86_64、Agent 到 Server 网络、Emulator 镜像拉取和两实例资源。

## 5. Android 与 DaFit 现状

- ADB 当前发现一台已授权三星真机；证据中不保存完整序列号；
- 用户当前选择 Docker Emulator 为 MVP，因此该真机不用于替代模拟器验收；
- `dafit_auto_platform` 已支持通过环境变量读取 `ANDROID_UDID` 和 `APPIUM_SERVER`；
- DaFit Appium Session 已显式设置 UDID，不需要重写 Driver；
- 当前运行环境仍会自动选择第一台真机，并会在 Appium 未运行时自行启动 Appium；Farm 模式必须禁止这两种行为；
- 当前未发现统一外部 `REPORT_DIR` 参数，DF-019 需要增加；
- `python tools/run_full.py --collect-only` 成功，仍收集 26 个用例。

## 6. 仓库基线

| 仓库 | 分支/commit | 工作区状态 | 处理规则 |
|---|---|---|---|
| `alcor_device_farm` | `master`，尚无首个 commit | 当前方案和目录均为未跟踪文件 | 开发只在该目录进行，首个 commit 由用户决定何时创建 |
| `dafit_auto_platform` | `main` / `ea59e78f3d3a7026a48ddb5a237f6d6d27915956` | 干净 | DF-019 前不修改；届时创建独立 feature 分支 |
| `Alcor` | `master` / `1755f5c58e0b20deff2e9c331457c0933fcdc9de` | 有未跟踪 `alcor_console/`、`docs/` | 旧版事实和用户文件，不修改；等待新版实际分支 |

## 7. 开发和验收能力矩阵

| 工作 | 当前是否可做 | 条件 |
|---|---|---|
| 方案、OpenAPI、目录和配置 | 是 | 无 |
| Go Server/Agent 编译测试 | 是 | DF-001 已验证 build/vet/test |
| Mock Provider/状态机 | 补齐 Go 后可做 | 不依赖 Docker |
| PostgreSQL migration/并发测试 | 暂不可 | 缺测试 PostgreSQL；DF-004 前补齐 |
| DaFit collect-only 和静态核对 | 是 | 已验证 26 用例 |
| 本地真机 DaFit | 技术上可做 | 非当前 MVP 验收范围，未经用户要求不执行 |
| Docker Emulator | 不可 | 缺 Linux KVM Host 和 Docker |
| STF 完整环境 | 不可 | 等 DF-017 部署环境 |
| 两设备并发 Appium/DaFit | 不可 | 等 Linux KVM、两台 Emulator、STF/Appium |
| 新版 Alcor 联调 | 不可 | 等新版实际分支和 OpenAPI |

## 8. 依赖准备顺序

1. DF-001：已提供 Go 工具链并完成纯 Go 工程骨架；
2. DF-004 前：提供 PostgreSQL 测试实例；
3. DF-014 前：提供 Linux KVM Host、Docker Engine 和可用网络；
4. DF-015~DF-018：在 Host 环境固定 Appium、UiAutomator2、STF 和 Emulator 镜像版本；
5. DF-019：只对 DaFit 增加 Farm 模式薄适配；
6. ALCOR-001：等待新版 Alcor OpenAPI。

## 9. 敏感信息处理

本清单未保存完整设备序列号、用户名密码、数据库 DSN、Appium/STF Token、Docker Registry 凭据或 Alcor Secret。后续证据继续使用相同规则。
