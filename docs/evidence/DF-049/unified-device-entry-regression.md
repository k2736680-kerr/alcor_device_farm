# DF-049 统一设备新增入口回归修正

日期：2026-08-20

## 修正范围

- 删除独立 Android 镜像页面及导航，旧 `/images` 路由跳转到 `/devices`；
- Android 系统目录改为新增 Android 模拟器向导中的可搜索下拉列表，默认选择 Android 16 / Google APIs / x86_64 候选；
- iOS 新增向导默认选择当前 Mac 目录中的首个可用运行时和首个兼容 iPhone 模板；
- Host 心跳发现首台 `ready/healthy` iOS Simulator 时，为尚未设置模板的活动 iOS Pool 自动补齐扩容模板，不覆盖已有模板；
- 全局字体和页面背景统一，原生表单控件继承同一字体。

## 自动化证据

- `pnpm test`：9 个测试文件、45 个测试全部通过；
- `pnpm build`：Orval 生成、TypeScript 编译和 Vite 生产构建通过；
- `go test ./...`：全部 Go 包通过；
- `go test ./internal/hostcommand -run 'TestIOSHeartbeatSelectsFirstHealthySimulatorAsDefaultPoolTemplate|TestIOSHeartbeatReadinessControlsHostAndPersistsOnlyRegisteredInventory' -count=1`：iOS 默认扩容模板和心跳回归通过；
- 本机浏览器 E2E 脚本未启动：终端未配置 `DEVICE_FARM_POSTGRES_BIN`，脚本在重置测试库前安全退出；没有以该环境缺项替代自动化通过结论。

## 结论

通过本地代码、组件、集成和生产构建回归。真实部署后，已有健康 iOS Simulator 会在下一次 Agent 心跳时补齐默认扩容模板。
