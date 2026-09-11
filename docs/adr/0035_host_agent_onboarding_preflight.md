# ADR-0035：宿主机 Agent 接入安装引导与预检

状态：accepted

## 背景

登记 Host 只创建设备域记录，Agent 仍需管理员登录目标 Linux/macOS 主机本地安装、写入 Secret 并启动。若控制台只显示 Host ID，新增服务器容易漏填地址、Provider 或 readiness 检查。

## 决策

1. Console 在登记成功结果中按平台展示脱敏的本机操作步骤：Linux 复用 `scripts/install-device-host-agent.sh` 和既有 systemd；macOS 复用 `deploy/ios-host/host-agent.env.example` 的配置项。
2. 引导只包含 220 Agent API 地址、Host ID、配置文件路径、Provider 和本地 readiness 检查命令；Agent Token 只以“从受控 Secret 注入”的占位说明出现。
3. 所有安装、Secret 注入、服务启动和网络检查均由管理员在目标主机执行；Server 不 SSH、Docker Socket 或远程命令代替管理员操作。
4. Host 是否在线、自动化是否就绪和容量是否可用继续以既有 heartbeat/Host API 为准，预检不新增状态或 API。

## 边界

- 不生成、回显或存储 Agent Token。
- 不新增 Host 表、注册协议、安装服务或远程执行通道。
- 不修改 171 正式服、NPS 或生产数据库。
