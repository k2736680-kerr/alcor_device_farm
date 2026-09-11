# ADR-0034：控制台宿主机登记与 Agent 引导

状态：accepted

## 背景

设备农场已经有 Host API、Agent heartbeat 和安装脚本，但 Console 只能查看已登记 Host。以后增加多台 Android/iOS 服务器时，管理员需要一个统一登记入口，同时不能把 Agent Token 或远程执行权限交给浏览器。

## 决策

1. Console 增加“登记宿主机”表单，调用既有 `POST /api/v1/device-hosts`，使用现有幂等键、服务端校验和设备域审计边界。
2. 表单只收集名称、Host 类型、操作系统、架构、内网地址和可选静态能力/容量；实时 CPU、内存、磁盘和设备数量仍以 Agent heartbeat 为准。
3. 登记成功后只展示 Host ID、220 Agent API 地址、安装脚本和需要在受控 Secret 文件中填写的配置项名称，不显示或生成 Agent Token。
4. Agent 继续主动连接 220 的 18182，使用现有 Host ID/Agent Token 认证；Server 不通过 SSH、Docker Socket 或控制面命令管理远端 Host。
5. 失败回滚只删除本次登记的 Host 记录（在没有设备、预约和命令时按既有 Host 生命周期规则处理），不触碰其他 Host、设备、STF 或正式流量。

## 边界

- 不新增 API、表、状态、Token 类型或 Alcor 业务对象。
- 不在浏览器执行安装命令；安装仍由管理员在目标服务器按现有脚本完成。
- 该入口支持未来多台 Linux/macOS Host，不改变 171 现有 Agent 配置。
