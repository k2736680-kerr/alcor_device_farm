# DF-059 正式控制台密码修复验收

## 结论

通过。正式 `admin` 用户的 Argon2id 哈希已按管理员当前明确指定的密码重新生成并写入 Secret volume；用户名、显示名和 `admin` 角色不变。正式 HTTPS 登录、当前会话、30 天安全 Cookie和注销全链路真实通过。

## 根因

DF-058 正确部署了 30 天会话，但错误地把“此前某次登录成功”和“本次 Secret volume 未变化”当成当前指定密码仍然有效，没有使用指定密码重新走正式 HTTPS 登录。Secret 中实际仍是旧密码对应的哈希，因此登录返回 `INVALID_CREDENTIALS`；连续尝试随后触发 15 分钟进程内 `LOGIN_RATE_LIMITED`。

## 修复

- 本地一次性工具从标准输入读取密码，以随机 16 字节 salt 和项目已接受的 Argon2id 参数生成 PHC 哈希；工具和临时 YAML 随后删除。
- 正式 Secret volume 只替换 `password_hash`，文件保持 `0400`、owner `65532:65532`，未写入明文。
- 重启 Server 重新加载用户并清除进程内登录失败窗口；按既有约束先删除、后重建 iOS 隧道，避免隧道绑定旧网络命名空间。
- 所有本地和远端临时文件均已删除，没有保留密码或部署备份。

## 真实验收

正式 HTTPS 入口返回：

| 检查 | 结果 |
|---|---|
| `POST /console/api/v1/sessions` | HTTP 201 |
| `GET /console/api/v1/sessions/current` | HTTP 200 |
| Session Cookie | `HttpOnly=true`、`Secure=true`、剩余 30 天 |
| `DELETE /console/api/v1/sessions/current` | HTTP 200 |

直连 Server 的 HTTP 端口登录同样返回 201，但生产 Secure Cookie 不会在 HTTP 连接回传；完整会话验收因此以正式 HTTPS 入口为准。

## 最终状态

- Server：`running/healthy`。
- iOS SSH 隧道：`running`，restart count 0；Server 网络空间 4811/4842 可达。
- Console Session、Audit Event、Reservation、Device Session：全部为 0。
- Secret 文件：`0400`、owner `65532:65532`。
- Git 工作区未留下哈希工具或临时 Secret，只保留用户原有未跟踪文档。

证据不包含 Console 明文密码、密码哈希、Cookie、Token、数据库口令或 SSH 私钥。
