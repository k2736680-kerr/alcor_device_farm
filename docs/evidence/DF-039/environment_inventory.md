# DF-039 iOS 环境盘点

## 1. 当前可访问开发环境

2026-08-17 在当前工作机实际检查：

| 项目 | 结果 |
|---|---|
| 主机 | `KERR` |
| 操作系统 | Windows 11 专业版 10.0.26100，64 位 |
| 内存 | 31.8 GiB |
| Xcode/xcodebuild/xcrun | 不存在；Windows 不能运行 iOS Simulator |
| 本地 Appium | 存在，仅证明开发机可运行 Appium CLI，不作为 iOS 验收 |
| 已配置 macOS Host | 当前仓库和部署配置中没有 |
| 已配置 iOS Simulator | 没有 |
| 已配置 iOS 真机/UDID | 没有 |
| Apple Developer Team/证书/Profile | 未提供，也未搜索本机秘密 |

结论：当前机器可以完成文档、Mock、契约和 Windows 构建工作，不能完成 E4/E5 真实 iOS 验收。不能将本机 Appium、网络资料或 Mock 结果表述为 iOS 已接入。

## 2. DF-040～DF-042 可开工条件

- DF-040 平台中立模型可在当前开发环境实现和测试；
- DF-041 开始前必须准备一台可由团队管理的 Apple Silicon macOS Host，并记录硬件、macOS、Xcode 和网络；
- DF-042 的 Session Fence 可先做 Mock 插件契约，但 completed 必须在 E4 真实 Node 上验证。

## 3. E4/E5 待准备清单

| 类别 | 必须确认的值 | 安全要求 |
|---|---|---|
| macOS Host | 资产标识、Apple Silicon 型号/内存/磁盘、macOS 版本、固定地址 | 不在文档记录登录密码 |
| Xcode | 精确版本/build、license 状态、目标 Simulator Runtime | 与目标 iOS 兼容 |
| Appium 栈 | Node/Appium/Device Farm/XCUITest/WDA/go-ios 精确版本和 lockfile | 禁止 `latest` |
| Simulator | 至少两个 allowlist UDID、机型、Runtime | 证据展示时脱敏 |
| iPhone | 至少一个资产标识、机型、iOS、配对/信任/Developer Mode 状态 | 完整 UDID 只进受控数据库，公开证据脱敏 |
| WDA 签名 | Team/bundle 的非敏感摘要、Profile 到期日、轮换负责人 | 私钥、账号、Profile 内容只在 Keychain/Secret |
| 网络 | Server↔Agent、Session Fence↔Appium Node 的受控端口 | 浏览器和普通用户不可直连 Node/Dashboard |

这些缺口不阻止 DF-039 设计完成，但会阻止 DF-041～DF-046 对应真实环境任务被标记 completed。
