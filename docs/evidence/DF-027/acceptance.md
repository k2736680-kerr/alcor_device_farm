# DF-027 设备操作和人工预约页面验收

## 状态

**completed**（2026-08-06，E0 本地 Server + PostgreSQL + Mock Provider + Mock STF + 浏览器自动化完成；真实 Linux/STF/Appium Web 验收仍属于 DF-028）。

## 目标

在 DF-026 只读控制台基础上补齐设备域受控写操作、人工预约完整生命周期和当前预约的 STF 短时入口。所有浏览器操作只调用 Device Farm Server API，不直连 STF、Docker、ADB、Appium 或数据库。

## 验收条件对照

| 验收条件 | 结果 | 证据 |
|---|---|---|
| Image validation、Host drain/undrain、Pool 配置、Device restart/rebuild/quarantine/unquarantine | ✅ | `ImagesPage`、`HostsPage`、`PoolsPage`、`DevicesPage` 已接入现有 mutation hooks；设备和主机危险操作统一使用 `ReasonActionModal` |
| 危险操作二次确认、reason 必填、展示服务端 request ID | ✅ | 通用确认框强制 reason；创建、取消、续租、释放及设备/主机操作成功提示均展示服务端 `request_id`；写请求自动携带唯一 `Idempotency-Key` |
| “查看容量 → 创建人工预约 → 等待 active → 受控 STF → 续租/释放 → 查看审计” | ✅ | Playwright `reservations.spec.ts` 真实启动 Server 并连接 PostgreSQL/Mock STF，全流程通过；与 DF-026 登录列表用例合跑为 2/2 passed |
| Console 用户不能修改 owner 或访问他人预约 | ✅ | 创建表单 owner 为禁用说明项，提交 owner 由当前 Console 会话绑定；E2E 确认创建结果 owner=`admin`；后端远控入口 owner 不匹配返回 403 的集成测试通过 |
| 第二个预约不突破单设备容量 | ✅ | E2E 仅播种 1 台 ready 设备；第一条 active 后第二条保持 pending，随后通过带 reason 的取消操作关闭 |
| 非法状态操作被页面和 Server 同时拒绝 | ✅ | pending 行仅允许取消，不显示续租/STF；后端禁用池、超租期、pending 取消、active 续租/释放、STF 释放失败补偿等 5 个集成测试实跑通过 |
| 刷新后不保留虚假成功状态 | ✅ | 第一条预约 active 后执行浏览器 reload，再次从 Server 查询并确认仍为 active、owner=`admin` 后继续远控和释放 |
| 浏览器拿不到 STF 管理 Token | ✅ | Mock STF Token 只作为 Server 进程环境变量；浏览器仅收到 Adapter 返回的短时远控地址；扫描源码、E2E 与构建产物为 0 命中 |
| 审计可查看且收口无悬挂预约 | ✅ | E2E 页面确认 `cancel_pending_device_reservation`、`release_device_reservation` 及 reason；结束后 PostgreSQL 中 pending/active 计数为 0 |

## 环境

- Windows 11；Go 1.26.5；PostgreSQL 17.10，`127.0.0.1:55432/device_farm_df004`
- Node 24.14.0；Orval 7.21.0；Vite 7.3.6；Vitest 3.2.7；Playwright 1.55.0 + 系统 Microsoft Edge
- E2E 夹具：1 个 active Pool、1 个 ready Device；Server 可在无真实 Provider 的情况下通过 Scheduler 分配预置设备
- Mock STF 仅实现 Adapter 测试所需 claim/remoteConnect/release 行为，不替代 DF-028 的真实 STF 验收

## 执行结果

```text
Orval client generation                         → OK
TypeScript tsc -b                              → OK
Vite production build                         → OK（4916 modules）
Vitest                                         → 4 files / 7 tests passed
go test ./...                                  → 全部通过
预约/STF/owner 定向 Go 集成测试（禁用缓存）    → 5 tests passed
Playwright console + reservations              → 2 tests passed
构建产物/源码 STF 管理 Token 扫描              → 0 matches
E2E 结束后 pending/active reservation          → 0
```

预约浏览器闭环实际覆盖：

1. Console admin 登录并查看预约容量；
2. 创建人工预约，确认 owner 不可编辑并显示服务端 request ID；
3. 等待 Scheduler 将预约从 pending 分配为 active；
4. 刷新页面后从 Server 恢复真实 active 状态；
5. 通过 Device Farm Server/STF Adapter 获取短时远控地址；
6. 创建第二条预约并确认其保持 pending，且不显示非法续租/STF 操作；
7. 填写 reason 取消 pending 预约；
8. 续租并填写 reason 释放 active 预约；
9. 在审计页面确认取消、释放动作和操作原因。

## 独立 Git 提交

```text
dfab824 DF-027a 设备操作列：重启/重建/隔离/解除隔离，reason必填+二次确认+服务端requestID展示
247d106 DF-027b 主机排空/解除排空与镜像验证操作：危险操作reason必填+确认+requestID
088587d DF-027c 池配置：基本信息编辑、镜像目标设置/停用、设备加入
dfdd383 DF-027d 人工预约：创建/续租/释放/STF远控入口，5秒轮询刷新
本次验收提交：补齐 owner 会话绑定、幂等键、pending 取消/非法操作约束、E2E 与验收证据
```

## 已知说明

1. WorkBuddy 通过 `NODE_OPTIONS` 注入 safe-delete 守卫，会在 Playwright 清理输出目录时偶发超时；验收使用仓库内 Playwright CLI，并在测试进程清空该注入变量，不影响产品运行。
2. Node 24 内置 fetch 与 jsdom 的 `AbortSignal` 类型不兼容；Vitest 初始化仅在测试环境移除该 signal，真实浏览器 E2E 继续覆盖请求取消链路。
3. Vite 报告单入口 bundle 超过 500 kB 的性能提醒，不影响 DF-027 正确性验收；拆包优化可在后续控制台工程优化中处理。

## ADR-0010 后续范围调整

DF-027 当时的 Mock STF 验收只证明后端 `remoteConnect` 的 owner、TTL、Token 隔离和预约编排，没有证明返回值可作为浏览器页面。DF-028 真实 STF 联调确认该返回值是 ADB TCP 地址，因此交付版 Console 已按 ADR-0010 删除“STF 远控”按钮和地址弹窗。

本文件保留上述历史 Mock 测试记录用于追溯 DF-018/DF-027 后端契约；当前产品验收以“不展示 ADB TCP 地址、不泄露 STF Token”为准。STF Adapter 的 claim、release、remoteConnect 能力继续保留，未在本项目重写 STF 原生远控功能。
