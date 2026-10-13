# 10.0.30.54 接入验收记录（2026-10-13）

## 1. 结论

10.0.30.54 已完成接入，**含真实建机验证**。三台 Android 宿主（171 / 54 / 55）
现均在控制面 `online`，各自有独立池，均可在按需模式下建机。

| 项 | 结果 |
|---|---|
| Host 记录 | `53e25d6f-4da0-49c2-bf11-ac8aad195e50`，名称「安卓宿主机 - 10.0.30.54」，`online` |
| 池 | `149c3632-5eba-42b0-85de-51d6e21c3139`，`android-10.0.30.54`，`0/0`、`max_concurrency=1` |
| 默认镜像 | `109cf3d6-ef57-485a-b924-6ecf8369200b`（`android-api35-host55-4bfbcf348b7d`，复用 55 的记录） |
| Agent | `alcor-device-host-agent.service` active + enabled，`NRestarts=0` |
| 真机建机 | ✅ 设备 `eafe6fce-c867-4300-afcd-e0c0c4aef73f` 在 54 上 `busy/healthy` |
| 回收 | ✅ 释放后 54 设备归零，`delete succeeded` |

## 2. 接入过程中发现并修复的真实缺陷

### 2.1 `ProtectHome=true` 遮住 linuxbrew 安装的 docker（已修复）

**症状**：Agent 每 5 秒重启一次，`NRestarts` 涨到 27。日志反复出现：

```
宿主机代理 Provider 配置错误：provider discover: Docker Engine is unavailable:
docker command failed: : exec: "docker": executable file not found in $PATH
```

**根因**：54 的 docker 由 linuxbrew 提供，是符号链接：

```
/usr/local/bin/docker -> /home/linuxbrew/.linuxbrew/bin/docker
                      -> ../Cellar/docker/29.7.2/bin/docker
```

单元模板写死 `ProtectHome=true`，让整个 `/home` 对服务不可见，**符号链接目标随之失效**。
`/usr/local/bin` 虽然在 systemd 默认 `PATH` 里，但链接指不到东西。

**同机对照实测**（只改这一行）：

| 设置 | 服务内 `command -v docker` | `docker version` |
|---|---|---|
| `ProtectHome=true` | 找不到 ❌ | — |
| `ProtectHome=read-only` | 解析成功 ✅ | `29.7.2` ✅ |
| `ProtectHome=false` | 解析成功 ✅ | `29.7.2` ✅ |

**为什么 171 没暴露**：171 的 docker 在 `/usr/bin/docker`（真实文件），不经过 `/home`。

**修复**：`scripts/onboard-device-host.sh` 先 `readlink -f "$(command -v docker)"`，
落点在 `/home` 下时自动把单元改成 `ProtectHome=read-only`（保留其余隔离，只放开 `/home` 读），
并在安装前的检查阶段就提示该情况。选择 read-only 而非 false，是为了尽量少牺牲隔离。

### 2.2 管理 API 端口与 Agent API 端口不同（文档已补充）

排查 2.1 时先撞上 `400 Bad Request`，一度误判为鉴权问题。实测：

| 端口 | 服务 | 路径 | 结果 |
|---|---|---|---|
| 18182 | Agent 内部 API | `/internal/v1/*` | 未授权 **401**（正常）；非该前缀 → 400 |
| 18180 | HTTPS 网关 → `device-farm-server:8080` | `/api/v1/*` | 200 ✅ |

即**管理 API 走 18180 网关**，18182 只服务 `/internal/v1`。
另：`GET /api/v1/device-hosts` 需要分页参数，且 shell 里 URL 含 `&` 必须整体加引号，
否则会被 shell 截断成 400。两者都已写入标准流程第 8 节。

## 3. 三宿主并发建机实测

**场景**：3 个池各 `max_concurrency=1`，并发投递 3 个不指定池的 Android 预约。

**结果**：

| 预约 | 分配到的池 | 结果 |
|---|---|---|
| final-verify-1 | `8b97a9e7`（171） | `active`，设备在 171，`busy/healthy` |
| final-verify-2 | `c8e22592`（55） | `active`，设备在 **54**，`busy/healthy` ⭐ |
| final-verify-3 | `8b97a9e7`（171） | `pending` → 释放时 `failed` |

**关键观察 1：设备可以建在「非该池同名」的宿主上。** 预约 2 被路由到 55 的池，
但设备实际建在 **54**——因为选池 SQL 的宿主条件是
`JOIN device_hosts h ON h.status='online'`，**未与池绑定**，任何 online 的 docker 宿主都算供给。
这与 54 复用 55 的镜像记录（同一 digest）是一致的，因此能成功。

**关键观察 2：调度器会「试错」并留下负向记录。** 日志里 54 上出现一次
`create → failed (IMAGE_DIGEST_MISMATCH)`，紧接着 `create → succeeded`：

```
03:40:53.834733 | create | failed    | IMAGE_DIGEST_MISMATCH | 10.0.30.54
03:40:53.845351 | create | succeeded                       | 10.0.30.54
```

成因：调度器在 54 上先后尝试了两个镜像。`abc5e576`（171 的 `e61f195a…`）在 54 上
确实不存在 → 负向记录（正确）；`109cf3d6`（`4bfbcf34…`）存在 → 成功。
**这不是故障，而是"镜像不在本机"的正常探测路径。**

**关键观察 3：`max_concurrency` 是每池上限，超出的预约静默排队。** 预约 3 与预约 1
同池，而该池已满 1 个 active 租约，因此一直 `pending`，**不报错、不超时**。
⇒ 多任务并行时必须把 `max_concurrency` 提到与实际并发匹配，详见 DF-071 §6.3。

## 4. 镜像记录决策（全局视角）

**决策：54 复用 55 的 `device_images` 记录 `109cf3d6`，不新建。**

理由：

1. `device_images` 的唯一键是 `(docker_digest, md5(resource_config::text))`，
   而 54 与 55 的镜像 digest（`4bfbcf34…`）与 `resource_config` **完全相同**；
2. 新建记录会制造「同一镜像两条记录」，让池配置与负向记录分散，违背单一事实来源；
3. 复用的直接收益已验证：54 与 55 **可以互相供给对方的池**（本次预约 2 即示例），
   提高了按需模式的可用性。

**54 上刻意保留的负向记录 `abc5e576 → IMAGE_DIGEST_MISMATCH`**：这是正确的，
因为 54 确实没有 `e61f195a…`。它会让调度器跳过「用 171 的镜像在 54 建机」这一无效组合，
属于期望行为。若将来把 `e61f195a…` 导入 54，新的成功观测会自动覆盖它。

## 5. 最终状态

| 宿主 | 状态 | 心跳时延 |
|---|---|---|
| iOS 宿主机 - 10.0.33.68 | online | 2.0s |
| 安卓宿主机 - 10.0.30.171 | online | 2.2s |
| 安卓宿主机 - 10.0.30.54 | online | 0.3s |
| 安卓宿主机 - 10.0.30.55 | online | 2.6s |

| 池 | total_target | min_ready | max_concurrency | 状态 |
|---|---|---|---|---|
| android-10.0.30.171 | 0 | 0 | 1 | active |
| android-10.0.30.54 | 0 | 0 | 1 | active |
| android-10.0.30.55 | 0 | 0 | 1 | active |

Android 活跃设备：**0**（全部已回收，符合按需模式的静息态）。