# 10.0.30.55 宿主机重装后 Host Agent 重新接入（2026-10-12）

对应操作：55 宿主机系统重装后，把 Host Agent 重新装回并让它重新被控制面识别。

本轮范围（用户确认）：**只恢复 55，并且只做到"控制面心跳 online"为止**；
不建模拟器、不改池容量、不碰 171、不碰 STF、不碰 220 控制面。

## 一、一句话结论

55 重装系统后，`/opt/alcor-device-farm`、`/etc/alcor-device-farm` 和 Host Agent 的
systemd 单元**全部丢失**，因此控制面里 55 一直停在 `offline`。
控制面侧的 **Host 记录和设备池记录都还在**，所以不需要重新登记 Host——
按原 Host ID 重装 Agent 后，55 在 **约 10 秒内恢复 `online`**：

> 装二进制 → 建 `device-farm` 用户 → 写 `host-agent.env`（复用旧 Host ID）→ 起服务 → 心跳 online

**零建机、零池变更、对 171 与 iOS 宿主零影响。**

## 二、重装后的问题定位

### 2.1 55 侧：设备农场痕迹被完全清掉

| 项目 | 重装后实测 |
|---|---|
| 操作系统 | Ubuntu 24.04.3 LTS（原为 22.04） |
| `/dev/kvm` | 存在，`crw-rw----+ root kvm`，可读可写 |
| `/dev/dri/renderD128` | 存在，`root render` |
| CPU / 内存 | 12 核 / 15 GB |
| 根分区 | 464 GB，可用 431 GB |
| Docker | 29.1.3，`overlayfs`，`hadoop` 在 `docker` 组 |
| `/opt/alcor-device-farm` | **不存在** |
| `/etc/alcor-device-farm` | **不存在** |
| `alcor-device-host-agent.service` | **不存在**（unit 文件都没了） |
| 模拟器镜像 | **不存在**（只剩 `worker-05`、`node-exporter` 等无关镜像） |
| 到控制面 `220:18182` | 可达 |

### 2.2 控制面侧：Host 与 Pool 记录都还在

| 对象 | ID | 状态 |
|---|---|---|
| Host `安卓宿主机 - 10.0.30.55` | `a2a4ee1d-1fec-4a25-a954-0fd2f2bc3205` | 重装前 `offline`，最后心跳 `2026-10-12 01:43:46Z` |
| Pool `android-10.0.30.55` | `c8e22592-7356-49ac-b7f2-43bf64525445` | `active`，`total_target=0 / min_ready=0` |
| Pool 默认镜像 | `109cf3d6-ef57-485a-b924-6ecf8369200b` | `127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9` |

### 2.3 关键契约：Agent 不能自注册

`internal/hostcommand/service.go` 的 `Heartbeat()` 对失败主机只做：

```sql
UPDATE device_hosts SET ... WHERE id=$1
```

匹配不到行时返回 `ErrNotFound`。**因此 Agent 无法自己创建 Host 记录**，
必须先在控制面存在 Host（本仓库既有 `POST /api/v1/device-hosts`，见 ADR-0034）。
本轮因为旧记录仍在，直接复用其 ID，**没有新增任何 Host 表记录**。

## 三、实际执行动作

| # | 动作 | 位置 | 结果 |
|---|---|---|---|
| 1 | 安装部署机 SSH 公钥到 55 `hadoop` 的 `authorized_keys` | 55 `~/.ssh/authorized_keys` | 密钥登录验证通过 |
| 2 | 从仓库 `test` 分支 HEAD `04573be794ea` 交叉编译 `device-host-agent`（CGO 关闭、linux/amd64、go1.26.5） | 部署机 → 55 `/opt/alcor-device-farm/bin/` | sha256 `e49df2c8…0824`，`--version` 与 HEAD 一致 |
| 3 | 创建系统用户 `device-farm`（`nologin`，home `/var/lib/device-farm`）并加入 `docker`、`kvm` 组 | 55 | `uid=995`，组已生效 |
| 4 | 写 `/etc/alcor-device-farm/host-agent.env`（复用旧 Host ID 与现有 Agent Token） | 55 | 权限 `0600 root:root` |
| 5 | 安装 `alcor-device-host-agent.service`，`daemon-reload`，`enable --now` | 55 | `enabled` + `active`，`NRestarts=0` |

Agent 配置要点（相对 171 模板只改地址类字段）：

```
DEVICE_FARM_AGENT_SERVER_URL=http://10.0.80.220:18182
DEVICE_FARM_AGENT_HOST_ID=a2a4ee1d-1fec-4a25-a954-0fd2f2bc3205
DEVICE_FARM_DOCKER_ADVERTISE_HOST=10.0.30.55
DEVICE_FARM_DOCKER_BIND_ADDRESS=10.0.30.55
DEVICE_FARM_SECURITY_AGENT_TOKEN=<沿用 220 server.env 中的现有 Agent Token>
```

> Token 只落在 55 的 `0600` 文件，未写入仓库、未回显到记录。

## 四、验证证据

### 4.1 心跳恢复（`device_hosts`）

```
安卓宿主机 - 10.0.33.68   |online| age 2.1s
安卓宿主机 - 10.0.30.171  |online| age 1.8s
安卓宿主机 - 10.0.30.55   |online| age 4.0s     ← 本次恢复
```

### 4.2 capabilities 已重建

```json
{"kvm": true, "gpu_render": true, "gpu_render_device": "/dev/dri/renderD128",
 "provider_inventory_complete": true, "host_readiness_auto_maintenance": false}
```

### 4.3 权限与开机自启

- `is-enabled: enabled`；`multi-user.target.wants` 软链已建立；
- 单元 `Requires=docker.service`、`After=network-online.target`；
- 以 `device-farm` 身份实测：`/dev/kvm` 可读、`docker info` 可达。

### 4.4 零副作用

- 55 上的池 `total_target/min_ready` 仍为 `0/0`，本轮**未改容量**；
- `device_host_commands` 中该 Host 最新一条仍是 `2026-09-28` 的 `delete`，
  **本轮没有下发任何 `create`/`rebuild`**；
- 55 上未出现设备容器（只有既有的 `worker-05`、`node-exporter`）；
- 171 与 33.68 心跳、STF、iOS 链路未受影响。

## 五、更正：农场现在是「按需」模型（初稿误判，已订正）

初稿把 `total_target=0 / min_ready=0` 当成「池被暂停」，**这是错的**。
农场已经改成**按需创建**：`total_target` 是上限，`min_ready=0` 表示空闲时不保留设备；
只有出现 pending Reservation 才创建，任务结束即删除释放资源（ADR-0038、AT-EMU-016）。
⇒ **`0/0` 是正确状态**。171 池同样是 `0/0`，两台宿主配置一致，不需要改容量。

## 六、后续闭环（已完成 → 见 `docs/host_onboarding_standard_process.md`）

1. **镜像**：已用 `docker save`（171）→ 传输 → `docker load`（55）放入
   `127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9`，
   55 本地 digest 为 `sha256:4bfbcf34…`，与 55 池默认镜像记录 `109cf3d6` 期望值一致。
   > 注意：同一 tag 在 171 上是 digest `e61f195a…`，`save`/`load` 后变为 `4bfbcf34…`
   > （内容相同、digest 不同）。`device_images` 以 digest 为身份，故逐台宿主各自成立。
2. **选池验证**：55 池的 B 分支判据成立，`安卓宿主机 - 10.0.30.55` = 可建机。
3. **STF 汇入无需额外配置**：171 的 `alcor-device-farm-stf-sync.timer` 仍 `active`。
4. **55 上的非设备农场负载**（`worker-05`、`node-exporter`）与本项目无关，未触碰。

## 六、复核命令

```sh
# 55 侧
systemctl status --no-pager alcor-device-host-agent.service
journalctl -u alcor-device-host-agent.service -n 20 --no-pager
ls -l /opt/alcor-device-farm/bin/ /etc/alcor-device-farm/

# 控制面侧
docker exec alcor-device-farm-control-plane-postgres-1 psql -U device_farm_server -d device_farm -c \
  "select name,status,last_heartbeat_at,now()-last_heartbeat_at as age from device_hosts order by name"
```