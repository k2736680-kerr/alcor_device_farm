# ALCOR-001 新版 Alcor 接入与统一入口验收

验收时间：2026-08-13

验收代码：Device Farm `master`、Alcor `test`

## 结论

ALCOR-001 已完成本地代码开发和测试环境真实联调。正式分支合并及正式环境发布不属于本次本地完成条件。

Alcor 已具备钉钉会话、App Build、Android Worker、Device Farm Adapter、RunAttempt、Artifact 和 DaFit 执行链路。设备农场继续只保存 Device、Pool、Reservation、Host 和设备域审计；没有在任一仓库复制 DaFit Runner、STF 远控或设备状态。

## 统一入口

- Alcor 一级导航以“设备农场”替换独立“应用版本”入口；
- 页面内提供“设备管理”和“应用版本（APK）”二级切换；
- 设备管理通过 Alcor 同源代理嵌入既有 Device Farm Console，APK 仍由 Alcor 管理；
- 浏览器只携带 Alcor 签名会话，不持有 Device Farm Service Token、独立 Console Cookie、STF API Token或签名 Secret；
- Alcor 代理丢弃浏览器 Cookie、Authorization 和操作者 Header，注入服务端凭据和已验证用户 ID；
- Device Farm Service Bearer 远控接口复用唯一的 `remotecontrol.Service`、Reservation、STF JWT 和 Reaper。

## 验收结果

| 检查项 | 结果 | 证据 |
|---|---|---|
| Alcor 用例明细默认展示全部结果 | PASS | Alcor 提交 `d99c817`；首次列表请求和页面状态均为 `all` |
| 一级导航与 APK 二级入口 | PASS | Edge 真实页面仅显示一级“设备农场”，同页可切换“设备管理 / 应用版本（APK）”并看到既有 APK |
| 单点登录 | PASS | 只写入 `alcor_session` 后，`/api/v2/device-farm/session`、Console HTML 和设备列表均返回 200；iframe 无设备农场登录页 |
| 真实设备数据 | PASS | 统一入口通过代理读取测试环境 15 条设备记录，其中当前 Emulator 为 `ready/healthy` |
| 真实远控 | PASS | 对当前 `ready/healthy` Emulator 通过 Alcor 代理启动远控，状态收敛为 `connected` 并返回 STF 原生短时入口，随后挂断返回 200/ended |
| 操作者审计 | PASS | 对应 release 审计为 `actor_type=service`、`actor_id=local-manual-user`，身份来自已验证 Alcor 会话 |
| 身份与凭据隔离 | PASS | Go 安全测试确认浏览器 Authorization、Cookie 和伪造 Actor 不转发；上游只收到服务器 Token 和会话用户 ID |
| DF-038 基线保持 | PASS | 候选镜像从当前 Device Farm `master` 完整归档构建；切换 Server 后既有 Emulator 未重启，持续 `ready/healthy` |
| 可回滚性 | PASS | 原镜像 `df038-rebuildfix-final-20260812` 和原容器配置均保留，未迁移数据库、未重建 Emulator |

## 自动验证

- Device Farm：`go test ./...` 全部通过；
- Device Farm Console：7 个测试文件、30 个测试全部通过，生产构建通过；
- OpenAPI 1.9.0：契约路径、冻结哈希和 Orval 生成通过；
- Alcor Server：`go test ./...` 全部通过；
- Alcor Console：生产构建和 lint 通过；lint 仅保留既有 React Hooks 警告；
- Edge 无头浏览器：一级入口、无二次登录、嵌入导航和 APK 切换通过；
- Linux 测试服务：`/readyz` 返回 ready，既有 Android Emulator 容器启动时间未改变。

## 部署边界

本次只更新本地 Alcor 测试程序和现有 Device Farm 测试服务用于验收，没有推送 Git 远端、没有合并 Alcor 主分支、没有发布正式环境。正式发布时只需部署这两个已提交版本并配置同一 Device Farm Endpoint 与 Service Token 引用，无数据库 migration。
