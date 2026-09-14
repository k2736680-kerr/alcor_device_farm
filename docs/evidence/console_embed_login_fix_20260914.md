# Alcor 嵌入控制台误弹登录页 —— 修复与上线记录

日期：2026-09-14
提交：`70d1224`（构建版本 `predeploy-20260914-console-embed-fix`，镜像 `615ee2d35663`）
环境：生产控制面 `10.0.80.220`

## 1. 故障现象

220 上 Alcor 的「Alcor / 设备池」页面，内嵌区域显示**设备农场控制台登录页**（"设备农场控制台登录" + 用户账号/密码输入框），而不是设备池列表。

## 2. 根因

线上 console 是 `9043f1a`「删 fetcher 里的 Alcor 代理翻译，控制台永远直连 /api/v1」的构建。在 Alcor iframe 内它直连错误路径：

| 请求路径 | 8880 返回 | 后果 |
|---|---|---|
| `/console/api/v1/me`（旧版直连路径） | **200 `text/html`**（Alcor SPA 的 HTML） | ❌ `response.json()` 解析失败 |
| `/api/v1/device-pools`（旧版直连路径） | **404 `text/plain`** | ❌ 路由不存在 |
| `/api/v2/device-farm/proxy/api/v1/device-pools`（正确代理路径） | **200 `application/json`** | ✅ 真实数据 |
| `/api/v2/device-farm/session`（正确会话端点） | **200 JSON，role=admin** | ✅ 路由存在 |

`console/src/App.tsx` 的 `if (isError || !session) return <LoginPage />` 因而必然触发 → 渲染登录页。

**线上 JS 指纹证据**：`index-CYQywJFo.js` 中 `device-farm/proxy` 命中 **0 次**，确认不含代理翻译逻辑。

**同时证伪了 `9043f1a` 的历史注释**：该提交称 "Alcor 后端并未提供该代理路由（POST 会被静默吞掉）"。实测 `POST /api/v2/device-farm/proxy/api/v1/device-reservations` 返回标准 401 JSON，**路由存在**，注释不可信。

## 3. 修复内容（提交 `70d1224`，3 文件）

- `console/src/api/fetcher.ts`
  - 嵌入判定拆为 `hasAlcorProxyMarker()`（pathname 含 `/api/v2/device-farm/console/`）与 `isInsideFrame()`（`window.self !== window.top`），取或；
  - `embeddedURL()` 按 Alcor 受控代理前缀翻译：`/api/v1/*` → `/api/v2/device-farm/proxy/api/v1/*`，`/console/api/v1/*` → `/api/v2/device-farm/console/api/v1/*`；
  - 新增 `fetchAlcorEmbeddedSession()`，嵌入态改用 Alcor 会话端点。
- `console/src/App.tsx`：嵌入态以 Alcor 会话为唯一判据，不再依赖设备农场自己的 console cookie；Alcor role 映射到 `ConsoleRole`。
- `console/src/api/fetcher.test.ts`：删除与新行为相反的旧用例，新增 6 个用例（65 个测试全过）。

## 4. 上线过程

线上此前运行 `7311f84`。`7311f84..70d1224` 之间共 **9 个提交**（含 4 处后端 Go：`auth.go` operator 权限、`reconcile.go` 隔离幂等防刷库、`management/service.go` runtime-profile 幂等、`cmd/device-host-agent/main.go` Appium 额外参数）。经确认全部属同一条主线既有工作，**整体上线到 HEAD**。

步骤：
1. `git archive HEAD` 出干净快照（3.9 MB），上传至 `/data/stacks/alcor-device-farm/staging/`；
2. 备份 `source/` → `source.before-70d1224` 与 `source.before-70d1224.tar.gz`；备份 `server.env` → `staging/server.env.before-20260914-console-embed-fix`；
3. 更新 `server.env` 版本标识为 `predeploy-20260914-console-embed-fix` / `70d1224`；
4. `docker compose build device-farm-server` 成功（258 行日志，`device-farm-server Built`）；
5. 替换容器并拉起 `server` + `ios-tunnel` + `ios-gateway`。

## 5. ⚠️ 过程中的操作失误（记录备查）

替换容器时出现**约 4 分钟的服务中断**，原因是我自己的操作错误，不是修复本身的问题：

- 首次 `docker compose up -d` 未成功替换，Docker 把新容器命名为 `d200a32d3a2e_...`（带 ID 前缀的游离名）；
- 我用 `docker rm -f d200a32d3a2e` 清理，**短 ID 同时命中并删除了正常运行的旧容器**，导致 server 彻底消失；
- 恢复方式：`docker rm -f` 精确清理残留 → `docker compose up -d --force-recreate` → 容器 `Created` 后未自动 start，用 `docker start` 逐个拉起。

**教训：清理容器必须用完整容器名，绝不用短 ID 前缀匹配。** 短 ID 会命中 `d200a32d3a2e_<原名>` 这种派生命名。

最终状态：5 容器全 healthy，服务恢复，容器名规范无前缀。

## 6. 验收证据

**服务健康**

| 检查 | 结果 |
|---|---|
| `10.0.80.220:18182/readyz` | 200 |
| `10.0.80.220:18182/healthz` | 200 |
| `https://10.0.80.220:18180/healthz` | 200 |
| `https://10.0.80.220:18181/healthz` | 401（设计如此：未认证 Gateway 请求拒绝）|
| 5 容器状态 | 全 `healthy` |

**嵌入态端到端（自签 Alcor 会话，走 8880 真实路径）**

| 端点 | 结果 |
|---|---|
| `GET /api/v2/device-farm/session` | **200** `{"expires_at":"2099-12-31T23:59:59Z","user":{...,"role":"admin"}}` |
| `GET /api/v2/device-farm/proxy/api/v1/device-pools` | **200** JSON，真实池数据（`total_target:1, min_ready:1`）|
| `GET /api/v2/device-farm/proxy/api/v1/devices` | **200** JSON，`Android 15-1` / ready |
| `GET /api/v2/device-farm/console/` 入口 | **`index-CUScZKN5.js`**（修复版，旧 `index-CYQywJFo.js` 已替换）|

**业务未受影响**

| 检查 | 结果 |
|---|---|
| `安卓宿主机 - 10.0.30.171` 心跳 | online，`hb_age_s=1` |
| `iOS 宿主机 - 10.0.33.68` 心跳 | online，`hb_age_s=1` |
| 设备 | `Android 15-1` ready/healthy；`iPhone17-1/2` ready/healthy |
| 近 2 小时预约 | released 4 / expired 1 / failed 1（链路正常）|

## 7. 回滚方式

```sh
cd /data/stacks/alcor-device-farm
# 恢复旧源码
rm -rf source && tar xzf staging/source.before-70d1224.tar.gz
# 恢复旧配置
cp staging/server.env.before-20260914-console-embed-fix server.env
# 重建并拉起
docker compose --profile ios --env-file postgres.env --env-file server.env build device-farm-server
docker compose --profile ios --env-file postgres.env --env-file server.env up -d --force-recreate device-farm-server
```

旧镜像 `alcor-device-farm:predeploy-20260914-idempotent` 仍在本地，可直接回退镜像。

## 8. 遗留事项

1. **浏览器缓存**：新 console 入口是 `index-CUScZKN5.js`。`index.html` 已设 `Cache-Control: no-store`，正常刷新即可拿到新版本；若仍见登录页需强刷。
2. **写操作尚未实测**：本次验证覆盖 GET 路径。`9043f1a` 曾警告 "POST 会被静默吞掉"，建议在控制台实际执行一次写操作（如改设备名）确认。
3. 分支 `codex/device-farm-v2` 领先 origin **24 个提交未 push**。
4. `docs/evidence/stf_release_diagnosis_20260914.md` 未提交。
