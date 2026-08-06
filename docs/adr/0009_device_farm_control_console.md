# ADR-0009：设备农场独立交付设备控制后台

## 状态

已确定；其中 Console 展示 STF 短时入口的决策已被 ADR-0010 覆盖。

## 背景

现有实施计划把设备管理页面延期到新版 Alcor Eval Console，因此即使设备 API、STF、Appium 和 DaFit 链路全部完成，本项目仍只能通过 API、脚本和 STF 原生页面使用。需求方已明确：设备农场在接入新版 Alcor 前必须先成为可独立操作和验收的产品，必须提供 Web 设备控制后台。

复用检查结果如下：

- 当前仓库没有前端工程；
- 本地旧版 Alcor `master` 没有已跟踪的前端源码，本地 `alcor_console` 只有未跟踪的构建产物和依赖目录，不能作为可维护源码复用；
- DaFit 没有设备管理 Web 后台；
- STF 已提供远程看屏、日志、文件和控制能力，必须继续复用，不能在本项目重写远控协议或画面组件。

## 决策

- 在本仓库新增 Device Farm Console，作为设备农场 MVP 的正式交付物和最终签收条件；
- 控制台只管理设备域：总览、Device Image、Device Host、Device Pool、Device、Reservation 和设备域审计；STF Web 入口按 ADR-0010 处理；
- 控制台不得实现 Alcor 的 Protocol Template、Case、Dataset、Target、Config、Run、RunAttempt、Result、Artifact、评分、门禁、业务报告或 CI 发布入口；
- 控制台只调用现有 `/api/v1/device-*` 设备接口，不直接访问 PostgreSQL、Docker Socket、RethinkDB、ADB 或 Appium 内部端口；
- 远程看屏、日志、文件和触控继续使用 STF 原生能力。当前控制台不展示 `remoteConnect` TCP 地址；未来只有具备 Reservation 级短时 Web 授权时才可重新加入入口；
- 浏览器不得持有 Device Farm Service Token。DF-026 在同一个 Go Server 中实现 Console Gateway：浏览器会话直接转换为 `console` Principal 并复用现有 handler/service，不使用 Service Token 回调自身 API；
- 本地用户来自部署机受限的 `console-users.yaml`，只保存用户 ID、显示名、`viewer/operator/admin` 角色和 Argon2id 密码哈希；PostgreSQL 只新增可撤销短时会话，不建立 Alcor 用户表；
- 会话 Cookie 使用 Secure、HttpOnly、SameSite=Strict；写请求必须通过与会话绑定的 CSRF Token 校验。非 HTTPS 只允许显式开发模式且 Server 绑定 loopback；
- 控制台写操作必须遵循服务端状态机、权限、幂等、确认和 `reason` 规则，页面不能自行把操作显示为成功；
- 前端工程固定使用 pnpm 11、Vite、React、TypeScript、Ant Design、React Router、TanStack Query 和 Orval；测试使用 Vitest、React Testing Library、MSW 和 Playwright；
- Orval 必须从 `openapi/device-farm-v1.yaml` 生成强类型 fetch client 和 Query hooks；设备契约版本提升为 `1.2.0`，先补齐精确成功响应和分页结构，禁止生成代码退化为通用 `Record<string, unknown>`；
- 以后接入新版 Alcor 时，Eval Console 可以链接、嵌入或复用 Device Farm Console 的设备域模块，也可以通过同一 API 提供统一入口；该接入不得成为当前控制后台可用性的前置条件。

## 任务安排

- DF-026：控制台工程、浏览器安全访问和只读资源页面；只依赖已完成的 DF-003、DF-004、DF-008 和 DF-025，本地 E0 验收通过即可 completed；
- DF-027：设备操作和人工预约流程；历史 Mock STF 远控契约验证保留为后端证据，交付页面按 ADR-0010 删除伪 Web 入口；
- DF-028：部署、安全、真实单设备 Web 端到端验收和最终签收。

ALCOR-001 必须依赖 DF-028；设备农场在控制后台验收完成前不得宣称 MVP 可独立交付。

## 后果

- 项目从“仅提供设备 API”调整为“提供设备 API 与可独立使用的设备控制后台”；
- 新增前端构建、浏览器认证、页面测试、静态部署和 Web 安全验收工作；
- STF、Appium、DaFit 和 Alcor 业务能力的复用边界保持不变；
- 设备农场仍不是第二套评估平台，未来 Alcor 集成仍通过 Device Farm Adapter 和稳定北向 API 完成。
