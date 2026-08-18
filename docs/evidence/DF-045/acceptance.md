# DF-045 Device Farm Console iOS 设备域页面验收

## 当前状态

`completed`

验收入口：平台筛选、iOS 设备/Host/Pool/预约/健康/签名摘要、权限、安全和刷新一致性；明确不提供 iOS 人工远控，Android STF 页面必须回归。

必须保存 viewer/operator/admin 权限、跨平台错误、浏览器网络/存储/构建 Secret 扫描和真实 E4/E5 页面证据。Console 不得显示 Appium Node Endpoint、Dashboard、WDA 地址、Session Grant 或完整证书信息，也不得复用 Android STF 按钮制造伪远控。

## 已完成的自动化与构建证据

- Devices 页面新增 3 条 iOS 回归：iOS Simulator 行仅展示设备域信息且没有 Android STF/配置入口；viewer 只读；管理员从 Mac 的受控 Runtime/机型目录完成三步创建请求。创建向导切换步骤后持续保留 Host、Runtime 和机型，避免提交时丢失已选值。
- `go test ./...`：通过，包含 OpenAPI 平台筛选契约和设备列表 `platform=android|ios` 过滤测试。
- `pnpm --dir console test`：7 个测试文件、36 个测试通过。
- `pnpm --dir console build`：通过；OpenAPI client、TypeScript 和 Vite 生产构建完成。
- 已扫描生产 JS bundle：未发现本机 Appium Hub/Node 地址、Session Grant、明文 token、secret 或 password 值。Console 仅保留“不会向浏览器暴露 Appium、WDA 或会话授权”的中文边界说明，不显示这些内部值。

## 真实 E4 浏览器与服务证据

- 2026-08-18 在 E4 Mac 以独立回环端口启动候选 Server，连接真实 PostgreSQL、运行中的 iOS Host Agent 与 CoreSimulator；候选 `/readyz`、`/console/` 均通过。受控服务身份请求 `GET /api/v1/devices?platform=ios` 返回 3 条真实 iOS Simulator 记录。
- 临时 admin/operator/viewer 三类 Console 会话均创建成功，`/console/api/v1/me` 返回角色与配置一致；admin 会话按 `platform=ios` 能读取这 3 条记录。
- Safari 已验证可打开登录页；随后用 Mac 上独立临时配置的 Chrome 完成实际管理员登录，并进入 `/console/devices?platform=ios`。页面显示 iOS 筛选、iOS Simulator 创建入口和中文设备域提示；在默认可用视图没有可用设备时，页面准确显示空态，不伪造远控入口。创建、删除、重建均未执行。
- 浏览器页面和生产构建均未暴露 Appium Node Endpoint、WDA、Dashboard、Session Grant、token、secret 或 password。iOS 行不复用 Android STF 远程连接；Android STF 原有操作覆盖继续由 Devices/App 回归用例验证。
- 验证完成后已终止候选 Server 与临时浏览器，删除临时用户、Cookie、候选二进制、环境文件和浏览器配置；确认候选端口无监听，原 DF-044 服务 `/readyz` 仍返回就绪。
