# ADR-0033：控制面 iOS 隧道与 HTTPS Gateway 部署包

状态：accepted（仅下一阶段预发布，未切换正式流量）

## 背景

220 控制面需要在未来承接 iOS Baguette 远控入口。Server 的 iOS Gateway 监听 8081，但 Baguette 只绑定 Mac 回环地址，必须通过受控 SSH 隧道访问。当前预部署 Compose 只有 Server 端口映射，依赖手工临时容器，无法重复部署、健康检查和回滚。

## 决策

1. 新增固定版本的非 root SSH tunnel sidecar，使用只读 Secret 目录，通过本地转发把 220 Server 网络命名空间内的 `127.0.0.1:4811`、`127.0.0.1:4842` 连接到 Mac/Baguette 回环端口。
2. 隧道使用 `network_mode: service:device-farm-server`，因此 Server 可通过 `127.0.0.1` 访问隧道；隧道不挂载 Docker Socket，不使用 privileged，丢弃全部 Linux capabilities。
3. Server 的 8081 只使用 Compose `expose`，不直接发布宿主机端口。独立 Nginx Gateway 绑定 220 的 18181，以 TLS 终止后代理到 `device-farm-server:8081`。
4. 证书、私钥、SSH 私钥和 known_hosts 只通过未纳入 Git 的 Secret 文件挂载；示例配置只包含占位符。
5. DF-066 的构建、220 预发布和真实 TLS/隧道验证不等于正式切换。不得修改 171、NPS、Agent URL 或正式数据库；iOS 开关关闭时，Gateway 和隧道可以停用并回滚到 DF-065 的 disabled 状态。

## 取舍与边界

- 复用现有 Baguette、Server iOS Gateway、Nginx 和 SSH 受控通道，不实现画面、触控、WebDriver、STF 或 Appium。
- 18181 是 HTTPS iOS Gateway；18182 继续只给 Host Agent 使用，不接 NPS。
- 隧道账号 UID/GID 固定且非 root，避免 root 无法读取 0600 私钥以及 Alpine 不识别动态 UID 的问题。
