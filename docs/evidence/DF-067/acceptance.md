# DF-067 控制台宿主机登记与 Agent 引导

状态：通过（本地自动化验收通过；仅完成代码和预发布准备，未切换 171 正式服）。

## 面向实际运维的功能

- “主机详情”用于查看一台服务器的 Agent 在线状态、自动化就绪状态、CPU/内存/磁盘容量、最后心跳和内部地址；普通查看者只能查看，管理员可修改显示名称。
- “暂停接收任务”用于服务器升级、重启或下线前的安全维护：不再接受新设备创建和新预约，已有预约继续完成，不强制杀任务。
- “恢复接收任务”用于维护结束后的复岗：服务器和 Agent 稳定后重新参加设备创建和预约调度。
- “登记宿主机”用于新增 Linux/macOS 服务器：录入名称、系统、设备能力、架构和内网地址，复用既有 `POST /api/v1/device-hosts`；实时容量仍由 Agent heartbeat 上报。
- 登记成功后只显示 Host ID、220 Agent API 地址和 Secret 使用提示；Agent Token 不返回浏览器，不写入页面或 Git。

## 自动化验证

| 检查 | 结果 |
|---|---|
| Console 全量 Vitest | 通过，10 个测试文件、60 个测试 |
| Host 页面登记/权限/维护说明测试 | 通过，7 个测试 |
| Console TypeScript/Vite 构建 | 通过 |
| Orval `generate:check` | 通过，未修改生成客户端 |
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| `git diff --check` | 通过 |

## 边界与安全

- 没有新增 Host 表、API、状态、Token 类型或第二套 Agent 注册协议。
- Agent 仍主动连接 220 的 18182；Server 不通过 SSH 或 Docker Socket 管理远端 Host。
- 未修改 171 正式 Server/Agent/STF/模拟器、NPS 或生产数据库；未执行正式流量切换。
