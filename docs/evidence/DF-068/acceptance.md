# DF-068 宿主机接入安装引导与预检

状态：通过（本地自动化验收通过；未执行远程安装，未切换 171 正式服）。

## 功能验收

- Linux 登记成功后显示现有安装脚本、配置文件路径、220 Agent API、Host ID 和 systemd 重启命令。
- macOS 登记成功后显示现有 iOS Host 配置模板和本机启动提示。
- 配置示例只出现 `DEVICE_FARM_SECURITY_AGENT_TOKEN=从受控Secret注入`，不回显真实 Token。
- 明确说明安装、Secret 注入和启动必须在目标服务器本机完成，控制面不 SSH 或远程执行。
- 页面继续以 Host heartbeat 的在线/自动化就绪状态作为最终接入结果。

## 自动化验证

| 检查 | 结果 |
|---|---|
| Host 页面测试 | 通过，7 个测试 |
| TypeScript/Vite 构建 | 通过 |
| Orval `generate:check` | 通过 |
| `go test ./...` | 通过（DF-067 同轮验证） |
| `go vet ./...` | 通过（DF-067 同轮验证） |
| `git diff --check` | 通过 |

## 边界

- 复用 `scripts/install-device-host-agent.sh`、systemd 和 `deploy/ios-host/host-agent.env.example`，没有新增安装服务、API、数据库表、状态或注册协议。
- 未修改 171、NPS、生产数据库或正式 Agent 配置；没有执行正式流量切换。
