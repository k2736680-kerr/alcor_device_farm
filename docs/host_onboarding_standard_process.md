# 宿主机接入设备农场标准流程（54 / 55 / 后续每台新服务器）

适用：Linux + Docker + KVM 的 Android 模拟器宿主机，接入 220 控制面。
本文既是操作记录，也是「一键安装脚本」的设计依据。

对应决策：ADR-0031（控制面与独立数据库）、ADR-0034（Console 登记 Host 与 Agent 引导）、
ADR-0035（Agent 接入安装引导与预检）、ADR-0037（多宿主 STF 可见性）、ADR-0038（按需池）。

---

## 0. 先理解机制（决定每一步为什么必须做）

### 0.1 农场是「按需」的，不是常驻暖池

- `device_pools.total_target` 是**上限**，不是常驻台数；`min_ready=0` 表示**空闲时不留任何设备**。
- 只有出现 pending Reservation 时，`warmpool.Controller` 才创建模拟器；任务结束目标归零，
  设备被删除、资源释放。
- **所以 `total_target=0 / min_ready=0` 是正确状态，不是「被暂停」。**
  看到某台宿主「Agent 在跑但没有容器」时，先确认池目标，不要怀疑 Agent。

### 0.2 Agent 不能自注册

`internal/hostcommand/service.go` 的心跳是 `UPDATE device_hosts ... WHERE id=$1`，
匹配不到就 `ErrNotFound`。**必须先在控制面存在 Host 记录**，Agent 才可能被认。
新增服务器时用 `POST /api/v1/device-hosts` 登记（可用 Console 表单），拿到 Host ID 再装 Agent。

### 0.3 镜像按宿主本地 digest 建记录；create 永不 pull

- `docker save` / `load` 得到的 **image ID 与 repo digest 是该宿主本地的**，
  与「另一台机器上同一个 tag」可能不同（内容可以相同，digest 却不同）。
- `device_images` 的唯一键是 **`(docker_digest, md5(resource_config))`** —— **digest 就是身份**。
  ⇒ **每台宿主本地解出来的 digest，对应它自己的一条 `device_images` 记录。**
- `internal/providers/docker/provider.go:VerifyImageDigest` 接受
  `metadata.ID` **或** 任一 `RepoDigests` 命中期望值。
- `create` **不做 `--pull`**：镜像引用经**宿主本地**解析。宿主没有该 digest 时，
  `create` 必然 `IMAGE_DIGEST_MISMATCH`。
- 负向记录（`device_host_image_states.available=false`）**不随时间失效**：
  「这台宿主没有这个镜像」不会自愈。恢复途径只有两条：
  ① 之后有一次成功的 `create` 覆盖它；② 运维对该镜像重新执行 `validate_image`。
  只有**关机/重装导致的失效**属于可能需要清理的情况。

### 0.4 设备进 STF 只有一条路径

STF 的 ADB registrar **硬性要求回环地址**，远程宿主的 Agent 无法注册进 STF。
唯一路径是：**由 STF 所在机（171）执行 `adb connect <远端设备高位端口>`**。
这件事由 171 上的 `alcor-device-farm-stf-sync.timer` 每 20 秒自动完成
（间隔必须显著小于 reconcile 的 STF 宽限期 30 秒，否则会陷入「反复重启但永不可见」）。
⇒ **新增宿主不需要在 171 上做任何配置**，同步脚本按控制面设备清单自动处理远程宿主。

---

## 1. 前置条件检查（目标宿主上执行）

| 检查项 | 命令 | 期望 |
|---|---|---|
| Linux | `uname -s` | `Linux` |
| 架构 | `uname -m` | `x86_64`（与镜像 abi 匹配） |
| KVM | `test -r /dev/kvm && test -w /dev/kvm && echo ok` | `ok` |
| KVM 组 | `getent group kvm` | 存在 |
| Docker | `docker version --format '{{.Server.Version}}'` | 有版本号 |
| Docker 组 | `getent group docker` | 存在 |
| GPU（可选） | `ls /dev/dri/renderD128` | 存在即可启用 `gpu_render` |
| 到控制面 | `test "$(curl -s -o /dev/null -w '%{http_code}' http://10.0.80.220:18182/healthz)" = 200` | `200` |
| 磁盘 | `df -h /var/lib/docker` | 至少能容纳镜像（~15 GB/镜像）+ 数据盘 |
| 内存 | `free -g` | 每台模拟器约需 5 GiB 容器限额 + Guest |

> 55 实测参考：Ubuntu 24.04.3、12 核、15 GB、Docker 29.1.3、
> `/dev/kvm` 与 `/dev/dri/renderD128` 均存在、到 220:18182 可达。

### 1.1 SSH 引导入口

部署工作站需要在目标机上有登录入口。首次用密码，随后安装公钥：

```sh
# 在目标机的部署账号下
mkdir -p ~/.ssh && chmod 700 ~/.ssh
touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
grep -qF '<部署机公钥>' ~/.ssh/authorized_keys || echo '<部署机公钥>' >> ~/.ssh/authorized_keys
```

**注意**：系统重装会清空 `authorized_keys`，这一步必须重做。

---

## 2. 在控制面登记 Host（仅新增服务器需要）

已有 Host 记录（重装/恢复场景）**跳过此步，直接复用原 Host ID**。

Console「登记宿主机」或 API：

```sh
TOKEN=$(grep -m1 '^DEVICE_FARM_SECURITY_SERVICE_TOKEN=' /data/stacks/alcor-device-farm/server.env | cut -d= -f2-)
curl -sS -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: onboard-<hostname>" \
  -d '{"name":"安卓宿主机 - <IP>","host_type":"docker_emulator","host_os":"linux",
       "host_arch":"amd64","address":"<IP>"}' \
  https://10.0.80.220:18180/api/v1/device-hosts
```

记录返回的 **Host ID**（形如 `a2a4ee1d-1fec-4a25-a954-0fd2f2bc3205`）。

> 实时 CPU/内存/磁盘/设备数**不在这里填**，一律以 Agent heartbeat 为准（ADR-0034 决策 2）。

---

## 3. 安装 Host Agent（目标宿主上，root）

### 3.1 取得二进制

两种方式：

1. **在部署机交叉编译**（推荐，产物与仓库 HEAD 一致）：
   ```powershell
   $env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
   & $env:DEVICE_FARM_GO build -trimpath ^
     -ldflags "-s -w -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.version=dev ^
       -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.commit=<commit> ^
       -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.buildDate=<date>" ^
     -o device-host-agent ./cmd/device-host-agent
   ```
2. 从已接入的宿主复制现役二进制（如 171 的 `/opt/alcor-device-farm/bin/device-host-agent`）。

> ⚠️ 官方安装器 `scripts/install-device-host-agent.sh` 会在**目标机本地**用 `go build`，
> 因此目标机需要 Go 工具链。新增宿主若不便装 Go，应改为「部署机编译 + 上传」。

### 3.2 建立系统用户与目录

```sh
# device-farm 系统用户（不可登录），加入 docker 与 kvm 组
if ! id device-farm >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/device-farm --shell /usr/sbin/nologin device-farm
  mkdir -p /var/lib/device-farm
  chown device-farm:device-farm /var/lib/device-farm && chmod 700 /var/lib/device-farm
fi
usermod --append --groups docker,kvm device-farm

install -d -m 0755 /opt/alcor-device-farm/bin
install -d -m 0700 -o root -g root /etc/alcor-device-farm
install -m 0755 device-host-agent /opt/alcor-device-farm/bin/device-host-agent
chown root:root /opt/alcor-device-farm/bin/device-host-agent
```

### 3.3 写 `/etc/alcor-device-farm/host-agent.env`

以 `deploy/docker-emulator/host-agent.env.example` 为模板，**必须改的字段**：

```
DEVICE_FARM_AGENT_SERVER_URL=http://10.0.80.220:18182
DEVICE_FARM_AGENT_HOST_ID=<本机 Host ID>
DEVICE_FARM_SECURITY_AGENT_TOKEN=<220 server.env 中的 Agent Token>
DEVICE_FARM_DOCKER_ADVERTISE_HOST=<本机 IP>
DEVICE_FARM_DOCKER_BIND_ADDRESS=<本机 IP>
DEVICE_FARM_DOCKER_KVM_DEVICE=/dev/kvm
# 有 /dev/dri/renderD128 时：
# DEVICE_FARM_DOCKER_RENDER_DEVICE=/dev/dri/renderD128
```

权限：

```sh
chmod 600 /etc/alcor-device-farm/host-agent.env && chown root:root /etc/alcor-device-farm/host-agent.env
```

> Token 只落在本机 `0600` 文件；不写入仓库、不回显到记录。
> `/var/lib/device-farm` 作为 `HOME`，用于 Agent 的本地状态。

### 3.4 安装 systemd 单元

`deploy/docker-emulator/alcor-device-host-agent.service` → `/etc/systemd/system/`：

```sh
install -m 0644 alcor-device-host-agent.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now alcor-device-host-agent.service
```

> 单元名是 **`alcor-device-host-agent.service`**，**没有 `farm`** ——
> 拼错会得出「它没装 systemd 单元、是裸进程」的错误结论。

---

## 4. 验证 Agent 上线

```sh
# 目标宿主
systemctl is-active alcor-device-host-agent.service      # active
systemctl show -p NRestarts alcor-device-host-agent.service  # 0
journalctl -u alcor-device-host-agent.service -n 20 --no-pager
id device-farm                                            # 含 docker、kvm

# 控制面：心跳必须新鲜（age 秒级）
docker exec alcor-device-farm-control-plane-postgres-1 psql -U device_farm_server -d device_farm -c \
  "select name,status,last_heartbeat_at,now()-last_heartbeat_at as age from device_hosts order by name"
```

期望：该宿主 `status=online`、`age` 秒级。
第一次心跳后 `capabilities` 应含 `kvm`、`docker`/`gpu_render`、`provider_inventory_complete`。

---

## 5. 准备模拟器镜像（关键且最容易出错的一步）

### 5.1 为什么必须逐台准备

`create` **不 pull**，镜像经宿主本地解析；而 `device_images` 以 **digest 为身份**。
每台宿主的本地 digest 只对**它自己**有效。⇒ **每台宿主都要有自己的镜像记录 + 本地镜像。**

### 5.2 搬运镜像

```sh
# 在源宿主（有镜像的机器）
docker save -o /tmp/emu.tar <image-ref>
sha256sum /tmp/emu.tar

# 传到目标宿主后
docker load -i /tmp/emu.tar
docker image inspect <image-ref> --format 'ID={{.Id}} RepoDigests={{.RepoDigests}}'
```

> 实测（55）：源 171 上该 tag 的 `ID/digest = e61f195a…`；
> 经 `save`→`load` 到 55 后变成 `ID/digest = 4bfbcf34…`（`Created`、层数、env 完全相同，
> 属**同一内容的不同 digest**）。**所以不能假定「同一个 tag 就等于同一个 digest」。**
>
> 目前两台机器都**没有**可用的 `127.0.0.1:5001` registry（171 的 `:5000` 是无关的
> `vega-face-search` nginx）。因此：
> - **方案 A（本次采用）**：`docker save/load`，并让目标宿主拥有**与其本地 digest 匹配**
>   的 `device_images` 记录与池绑定。
> - **方案 B（后续可选）**：把镜像 tag 改为无 registry 前缀的本地名，并同步更新
>   `device_images.docker_image`。
> - **方案 C（长期）**：在每台宿主常驻本机 registry，使 digest 可复现。

### 5.3 登记 / 复用镜像记录

**若目标宿主本地 digest 已有对应 `device_images` 记录** → 直接复用，并把该池的
`default_image_id` 指过去、在 `device_pool_images` 建 enabled 绑定。

**若没有对应记录** → 走仓库既有链路创建（官方目录同步 + 按需构建 + `validate_image`
校验 + 不可变 digest 登记，见 ADR-0015 / DF-035），**不要手工 INSERT `device_images`**。

> ⚠️ 手工往 `device_host_image_states` 写 `available=true` 只是**声明**，
> 真正生效靠 `create`/`validate_image` 的成功结果。**首选是让镜像真正到位**，
> 而不是改记录把问题盖住。

### 5.4 池配置

```sh
# 按需池：total_target 是上限，min_ready=0
# PUT 是全量替换，必须带齐所有字段
curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"android-<IP>","platform":"android","default_lease_seconds":1800,
       "max_lease_seconds":7200,"max_concurrency":1,"total_target":0,"min_ready":0,
       "default_image_id":"<本机 digest 对应的 image id>",
       "reason":"接入新宿主 <IP>"}' \
  https://10.0.80.220:18180/api/v1/device-pools/<pool-id>
```

---

## 6. 验证「可被按需选中」

选池 SQL 的 B 分支（`internal/reservation/pool_selector.go:204-306`）要求：

1. 池 `status='active'`、平台匹配；
2. `default_image_id` 非空且该镜像 `status='ready'`；
3. `device_pool_images` 中该镜像 **enabled** 绑定到本池；
4. **存在**至少一台宿主：`status='online'`、`NOT draining`、`host_type IN ('docker_emulator','hybrid')`、
   **且没有被证明缺该镜像**（不存在 `available=false` 记录）；
5. 该池**未达 `max_concurrency`**（只统计未过期的 active 租约）。

逐条自查：

```sql
-- 对每个 Android 池，列出「能建机」的宿主
SELECT p.name AS pool, i.name AS image, h.name AS host,
  CASE WHEN h.status='online' AND NOT h.draining
    AND h.host_type IN ('docker_emulator','hybrid')
    AND COALESCE((SELECT s.available FROM device_host_image_states s
                  WHERE s.host_id=h.id AND s.image_id=p.default_image_id), true)
  THEN '可建机' ELSE '跳过' END AS verdict
FROM device_pools p
JOIN device_images i ON i.id=p.default_image_id
CROSS JOIN device_hosts h
WHERE p.platform='android' AND h.host_type IN ('docker_emulator','hybrid')
ORDER BY p.name, verdict DESC, h.name;
```

期望：**该池自己的宿主 verdict = 可建机**。若被 `available=false` 钉住，
先确认镜像是否真的缺失；**只有确认镜像已到位**，才考虑清除该陈旧记录。

校验镜像 digest 与记录的对应关系（逐宿主）：

```sh
docker image inspect <image-ref> --format 'ID={{.Id}} RepoDigests={{.RepoDigests}}'
# 期望：ID 或某个 RepoDigest 的 @sha256: 部分 == device_images.docker_digest
```

---

## 7. 端到端验收（按需建机 → 释放 → 归零）

真正证明「和 171 一致」的观测点：

| 阶段 | 观测 |
|---|---|
| 投递任务 | pending Reservation 出现 |
| 按需创建 | `device_host_commands` 新增 `create`（`leased`→`succeeded`），宿主上出现容器 |
| 设备就绪 | `devices.lifecycle_status='ready'`、`health_status='healthy'` |
| STF 可见 | 171 sync timer 日志 `connected=N`；`docker exec alcor-device-farm-stf-stf-adb-1 adb devices -l` 中该 serial 为 `device`；控制面 `devices.health_reason='STF visibility recovered'` 且 `consecutive_failures=0` |
| 任务结束 | Reservation `released` |
| 资源释放 | `delete` 命令 succeeded；设备 `deleted`；**池回到 0**；宿主容器消失 |

> 验收必须证明：**空闲时不留常驻设备**（AT-EMU-016），且不突破
> Pool `total_target`/`max_concurrency`、镜像和宿主容量上限。

> **STF 判定的权威口径**：不要用 STF 的 `/api/v1/devices`（需用户令牌，会 401）。
> 用**两个**可靠信号：① 171 的 `stf-adb` 容器内 `adb devices -l` 显示 `device` 状态；
> ② 控制面 `devices.health_reason='STF visibility recovered'` 且 `consecutive_failures=0`。
> 新宿主机**无需任何 STF 侧改动**——171 的 timer 会自动发现新设备的 `adb_endpoint` 并接入
> （ADR-0037 决策 3）。详见 `docs/evidence/stf_usability_20261013.md`。

---

## 8. 常见坑（都是真实踩过的）

1. **「Agent 在跑但没有容器」→ 先查池目标**。`total_target=0` 时调度器静默不下发命令，
   Agent 侧表现完全正常。排查入口：`device_audit_events` 搜 `update_device_pool_capacity`。
2. **系统重装会清空一切**：`/opt/alcor-device-farm`、`/etc/alcor-device-farm`、
   systemd 单元、`authorized_keys`、本地镜像全部丢失，但**控制面的 Host 与 Pool 记录还在**。
   ⇒ 复用原 Host ID 重装即可，**不要重复登记**。
3. **单元名没有 `farm`**：是 `alcor-device-host-agent.service`。
4. **同一个 tag ≠ 同一个 digest**。跨机搬运后必须重新核对 digest，并按宿主建立/复用镜像记录。
5. **负向记录不会自愈**：`available=false` 永久有效，会让调度器跳过该宿主。
6. **STF 同步间隔必须 < reconcile 宽限期（30s）**，当前是 20s + `AccuracySec=1s`。
7. **`PUT /device-pools` 是全量替换**，漏字段会被清掉。
8. **`ProtectHome=true` 会把 linuxbrew 装的 docker 一起遮住**（2026-10-13 在 54 上实测）。
   症状：Agent 每 5 秒重启一次，日志反复出现
   `provider discover: Docker Engine is unavailable: docker command failed: exec: "docker": executable file not found in $PATH`。
   根因：`/usr/local/bin/docker` 是指向 `/home/linuxbrew/.linuxbrew/bin/docker` 的符号链接，
   而 `ProtectHome` 让 `/home` 不可见，符号链接目标随之失效。
   对照实测（同一台机器，只改这一行）：

   | 设置 | `command -v docker` |
   |---|---|
   | `ProtectHome=false` | `/usr/local/bin/docker` ✅ |
   | `ProtectHome=read-only` | 解析成功，`docker version` 返回版本 ✅ |
   | `ProtectHome=true` | **找不到** ❌ |

   ⇒ `onboard-device-host.sh` 会先 `readlink -f $(command -v docker)`，
   落点在 `/home` 下时自动把单元改成 `ProtectHome=read-only`。
9. **管理 API（`/api/v1/*`）不在 18182 上**。`18182` 是 Agent 内部 API，只服务
   `/internal/v1/*`（未授权返回 **401**）。管理 API 经 **18180 的 HTTPS 网关**转发到
   `device-farm-server:8080`。用错端口会得到 nginx 的 `400 Bad Request`，极易误判为鉴权问题。
10. **`GET /api/v1/device-hosts` 需要分页参数**（其余 list 端点多数有默认值）。
    在 shell 里 `curl` 带 `&` 的 URL 必须整体加引号，否则会被截断。

---

## 9. 一键脚本（已实现：`scripts/onboard-device-host.sh`）

对应 ADR-0039。脚本把第 1～4 节中**可安全自动化**的部分固化下来：

```sh
# 在目标宿主机本地执行（需 root）。脚本不必位于仓库检出内。
sudo scripts/onboard-device-host.sh \
  --server-url http://10.0.80.220:18182 \
  --host-id <已登记的 Host ID> \
  --agent-token-file /etc/alcor-device-farm/agent-token \
  --agent-binary /path/to/device-host-agent \
  --unit-file /path/to/alcor-device-host-agent.service \
  --advertise-host 10.0.30.55 \
  --render-device /dev/dri/renderD128 \
  --image '127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9'
```

只读预检（不改动系统）：

```sh
sudo scripts/onboard-device-host.sh --check-only --server-url http://10.0.80.220:18182
```

### 9.1 脚本做什么

```
0 参数校验 → 1 前置检查 → 3 系统用户与目录 → 4 写 host-agent.env
→ 5 安装 systemd 单元 → 6 自检（含心跳等待）
```

- 前置检查：Linux/x86_64、`/dev/kvm` 读写、`kvm`/`docker` 组、docker daemon、
  Docker 数据盘 ≥ 40 GiB、控制面 `/healthz` = 200；
- 幂等：已存在的 `host-agent.env` 默认保留（`--force-env` 才覆盖）；
- **fail-fast**：缺少单元模板时在**改动系统之前**失败，不留半成品；
- `--no-start` 可只安装不启动。

### 9.2 脚本刻意不做的事

| 不做 | 原因 | 由谁做 |
|---|---|---|
| 登记 Host | 需要控制面 API 与 Token，属 ADR-0034 既有入口 | 管理员（第 2 节） |
| 搬运镜像 | 影响调度正确性；digest 逐台不同 | 管理员（第 5 节） |
| 改池容量 | 改变调度行为 | 管理员（第 5.4 节） |
| 远程执行 | Server 不 SSH/Docker Socket 管远端（ADR-0031 决策 6） | 管理员在目标机本地执行 |

### 9.3 后续演进

- 把「三件套」打包（脚本 + 二进制 + 单元 + env 模板）成一个可分发的目录，
  减少手工传参；
- 增加可选的镜像对账子命令，直接输出「本机 digest ↔ `device_images` 记录」对照表；
- 增加与 Alcor/控制面的登记联动（仍需管理员确认后才执行）。

### 9.4 真实测试结论

**2026-10-13 · 10.0.30.55**（故障恢复演练）：清空 `/opt/alcor-device-farm`、
`/etc/alcor-device-farm`、systemd 单元和 `device-farm` 用户后，脚本**一次执行**即恢复
`online`（心跳 0.6 秒）；`--check-only`、fail-fast、幂等三项均真实验证通过。
详见 `docs/evidence/DF-071/acceptance.md`。

**2026-10-13 · 10.0.30.54**（全新机器首次接入，含真实建机）：

| 项 | 结果 |
|---|---|
| 脚本安装 Agent | ✅ `active` + `enabled`，`NRestarts=0` |
| 发现并修复的缺陷 | ✅ `ProtectHome` 遮住 linuxbrew docker（见第 8 节第 8 条） |
| 控制面登记 | ✅ Host `53e25d6f`、池 `149c3632`（`android-10.0.30.54`，`0/0`） |
| 真机建机 | ✅ 设备 `eafe6fce` 在 54 上 `busy/healthy` |
| 回收 | ✅ 释放后归零，`delete succeeded` |

**结论**：脚本对「全新机器」可用。唯一需要人工判断的是**镜像来源**——
脚本不搬运镜像，也不新建 `device_images` 记录（见 9.2）。
详细接入过程、缺陷复现与同机对照实验见 `docs/evidence/host54_onboard_20261013.md`。

### 9.5 接入一台新机器的最小步骤

1. `bootstrap-device-host.sh`（全新机器；已有 docker/KVM 可跳过）
2. 控制面 `POST /api/v1/device-hosts` 登记，拿到 Host ID
3. 目标机本地跑 `install-local-image-registry.sh`
4. 把镜像弄到本机（`distribute-emulator-image.sh` 或直接 build），
   **以 `docker load` 之后的 digest 为准**
5. 该 digest 已有 `device_images` 记录 → **复用**；否则新建
6. `onboard-device-host.sh` 装 Agent
7. 确认 `status=online`；建池（`total_target=0 / min_ready=0`）；跑选池自查 SQL
8. 投一个预约做真机验收

第 4 步的 digest 是唯一身份，第 5 步的决策决定池能否选中该宿主。

#### 9.5.1 建池时并发上限怎么设（2026-10-13 定）

**单机开几台由每台宿主的实时剩余内存决定**，不由池的计数决定。因此建池时：

| 层级 | 字段 | 建议值 | 理由 |
|---|---|---|---|
| 池 | `total_target` | `0` | 按需：空闲清空 |
| 池 | `min_ready` | `0` | 不保温 |
| 池 | `max_concurrency` | 足够大（如 8） | **安全网**，不让它成为瓶颈 |
| 镜像 | `max_instances` | 同上 | **安全网** |

真正的约束在 `internal/capacity/capacity.go`：按该机
`min(账面可用, 实时可用)` 减预留（CPU 1 核 / 内存 2048MB / 磁盘 4096MB）后
除以单台规格。**因此大内存机器自动多开、小内存机器自动少开，无需逐台配置。**

> ⚠️ **设置顺序有讲究**：`SetPoolImage` 强制 `min_ready == max_instances`，
> 且写入会**回写池级 `total_target`/`min_ready`**。必须
> **先** `PUT /device-pools/{id}/images/{image_id}`（设 `max_instances`），
> **后** `PUT /device-pools/{id}`（设 `total_target=0 / min_ready=0 / max_concurrency=N`）。
> 顺序颠倒会把池级 `min_ready` 抬成 `max_instances`，导致池常驻热备、**破坏按需清空**。
>
> 实测：15GB 级宿主在 `container_memory_mb=5120` + 预留 2048MB 下，
> 空闲 available ≈ 11.5~13.4GB → 可开 2 台；已跑 1 台后降到 ≈ 6.8GB → 第 2 台被
> 宿主以 `INSUFFICIENT_HOST_RESOURCES` 拒绝并自动回收。
> 详见 `docs/evidence/concurrency_policy_20261013.md`。

### 9.6 一条命令接入（`scripts/install-device-host.sh`）

上面的 8 步已串成一个入口，**它把第 1～4、6、7 步自动化**：

```sh
# 先预演，看它打算做什么
sh install-device-host.sh --host 10.0.30.56 --host-id <UUID> --pool-image-id <UUID> --dry-run

# 真正执行（目标机全新、只有密码）
sh install-device-host.sh \
  --host 10.0.30.56 \
  --host-id <控制面 Host UUID> \
  --pool-image-id <device_images 记录 UUID> \
  --token-file <服务Token文件> \
  --ssh-user hadoop --ssh-password '<目标机密码>' \
  --image-source 10.0.30.54 --image-source-user hadoop --image-source-password '<源机密码>'
```

**为什么要打成整包分发**：该脚本要求五个同级脚本同行。逐个手拷容易漏，
所以用 `scripts/package-host-toolkit.sh` 一次产出整齐的一套（含单元模板，
可用 `--with-agent` 内置 Agent 二进制）：

```sh
sh package-host-toolkit.sh --with-agent /path/to/device-host-agent
# 产出 dist/alcor-host-toolkit/ 与 dist/alcor-host-toolkit.tar.gz
```

**它刻意不做的事**（与 9.2 一致，逐条保留人工判断）：

| 不做 | 原因 |
|---|---|
| 新建 `device_images` 记录 | 镜像身份决策会影响调度正确性；复用与否需人工判断 |
| 删除镜像或负向记录 | 删除会破坏可追溯性与既有调度结论 |
| 改已有池容量 | 会改变整体调度行为 |
| 控制面删除 Host | 破坏审计链 |

**两个防「半成品」的设计**（都来自真实踩坑）：

1. 脚本常被拷到 `/tmp` 执行，此时由 `$0` 推导的仓库根是错的（会去找 `/deploy/...`）。
   现在改为**显式探测** Agent 二进制与单元模板，找不到就直接报错；
2. 五个同级脚本任一缺失，**在改动任何系统之前**即退出——
   绝不允许出现「用户建好了、systemd 单元没装上」的状态。

---

## 10. 本次（10.0.30.55）实际操作记录

| # | 动作 | 结果 |
|---|---|---|
| 1 | 部署机公钥装入 55 `hadoop` 的 `authorized_keys` | 密钥登录通过 |
| 2 | 本地交叉编译 `device-host-agent`（`test` HEAD `04573be794ea`，linux/amd64，go1.26.5） | sha256 `e49df2c8…0824` |
| 3 | 建 `device-farm` 系统用户并加入 `docker`、`kvm` | `uid=995` |
| 4 | 上传二进制到 `/opt/alcor-device-farm/bin/` | `--version` 与 HEAD 一致 |
| 5 | 写 `/etc/alcor-device-farm/host-agent.env`（**复用原 Host ID `a2a4ee1d-…3205`** + 现有 Agent Token） | `0600 root:root` |
| 6 | 安装并 `enable --now` `alcor-device-host-agent.service` | `active`、`NRestarts=0` |
| 7 | 镜像 `save`（171）→ 传输 → `load`（55），sha256 全程校验 | 55 本地 `ID/digest = 4bfbcf34…` |
| 8 | 验证 digest 与 55 池默认镜像记录 `109cf3d6` 期望值**一致** | ✅ 匹配 |
| 9 | 执行选池自查 SQL | 55 池 `b_branch_selectable = t`，**55 宿主 = 可建机** |
| 10 | 池容量**保持 `0/0`**（与 171 一致，按需） | 未改容量 |

**结论**：55 已与 171 完全一致 —— 在线、按需建机、空闲自动清空。本轮未发起真实任务，
故未创建任何模拟器。

**遗留**：55 池默认镜像记录名仍是 `android-api35-host55-4bfbcf348b7d`（描述其本地 digest），
命名可以接受；后续若统一改为本机 registry 方案，需同步更新 `device_images.docker_image`。