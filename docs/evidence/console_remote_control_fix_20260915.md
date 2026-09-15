# 控制台远程连接 / 宿主就绪 / 55 池模板修复（2026-09-15）

## 三个根因与修复

### 1. 嵌入态远程连接报「无法连接设备农场服务」（NETWORK_ERROR）
- **根因**：`console/src/api/generated/device-farm.ts` 中 remote-control 四个端点（start/get/end/heartbeat）生成为 `/console/api/v1/devices/{id}/remote-control*`。嵌入态 `fetcher.ts embeddedURL()` 把它翻到 `/api/v2/device-farm/console/api/v1/*`，而 Alcor 侧该前缀**只注册 GET**（`internal/router/router.go:263`，静态资源处理器 `ProxyDeviceFarmConsole`）→ 非 GET 必返 `404 page not found`（text/plain）→ `response.json()` 抛异常 → fetcher 兜底为 NETWORK_ERROR。这是**嵌入态通病**，55/171 都受影响。
- **修复**：RemoteControlProvider 改用 Integrated 家族端点（`/api/v1/devices/{id}/remote-control*`），嵌入态经 `ProxyDeviceFarm`（全方法 + method/path 白名单，`platform/device_farm_gateway.go` 中 POST/DELETE devices/[^/]+/remote-control 在白名单内）。
- **线上取证（修复前）**：`POST …/console/api/v1/devices/{id}/remote-control` → 404 text/plain；`POST …/proxy/api/v1/…` → 401（路由存在）。

### 2. 55 宿主机「自动化未就绪」
- **根因**：`HostsPage.tsx` 用 `capabilities.kvm && capabilities.docker` 判定，而 `internal/hostcapacity/system.go:49` 的 Snapshot 只产出 `{kvm, gpu_render}`，**从不产出 docker**；171 的 `docker`/`api_levels` 是历史 jsonb `||` 合并残留（`hostcommand/service.go:265` 只增不减）→ 该判定对任何新宿主**永假**。
- **修复**：改用 `runsDockerEmulators(host_os/host_type)` 推导（非 macOS 且 docker 模拟器宿主即视为具备 docker 能力）。

### 3. 55 池「扩容模板：未设置，自动扩容已暂停」
- **根因**：55 池 `base_device_id` 从建池起就为空（审计 09-14 10:27「建池时遗漏字段」；UpdatePool 增量保留 BaseDeviceID，非人为抹除）。
- **修复**：`PUT /api/v1/device-pools/c8e22592-7356-49ac-b7f2-43bf64525445/base-device` `{"device_id":"858fd7cc-33b3-402c-b77f-3af9eb189cc8","reason":"…"}` → 200，审计留痕。

## 部署

- 快照 = `git archive HEAD` + 仅覆盖本次 7 个 console 文件（不带上工作区他人在途 Go 改动，快照 diff 校验通过）。
- 220：备份 `source.before-*` → 覆盖 → server.env 版本三元组更新 → build（新镜像内嵌 bundle 指纹 `index-BeT-9QmJ.js`，与本地构建一致）→ `up -d --force-recreate` 三服务，5 容器 healthy，无游离容器。

## 端到端验收（自签 Alcor 会话，走 8880 嵌入态入口）

secret 取自 `/opt/apps/alcor/alcor_server/config.yaml` `secrets.ALCOR_SESSION_SECRET`；cookie 配方 `auth.go`。

| 步骤 | 结果 |
|---|---|
| GET `/api/v2/device-farm/session` | 200 json |
| GET proxy 池详情（复核 base_device_id） | 200 |
| **POST proxy 远控**（带 Idempotency-Key） | **202，`status=connecting, transport=stf, reservation_id=5a6b1418…`** |
| **DELETE proxy 远控** | **200，`status=ended`**（干净收尾，无残留会话） |
| 对照：POST console 前缀 | 404 text/plain（仍坏，符合预期） |

> 缺 Idempotency-Key 时返回 400 业务校验 —— 恰好证明请求已通过鉴权与路由到达 handler。

## 测试与构建

- vitest 全套通过；`tsc -b` 无错；orval 重生成与工作区 diff 为空（客户端与规格同步）；vite build 成功。
- 测试更新：`fetcher.test.ts`（嵌入态路径翻译注释修正）、`DevicesPage.test.tsx` / `App.test.tsx` / `test/handlers.ts`（远控 mock 改 Integrated 端点）、`HostsPage.test.tsx`（无 docker 键的 capabilities 也判就绪）。

## 待办

- 代码改动 7 个 console 文件在工作区，**待授权后提交**。
