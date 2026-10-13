# ADR-0039：宿主机一键接入脚本与标准流程

## 状态

已批准，作为 DF-071 的基线。

## 背景

设备农场已经支持多台 Linux/Docker/KVM 宿主机（171、55）和一台 macOS 宿主机，但新增或
重装一台宿主机的接入动作此前只存在于人工操作和经验记录里：

- ADR-0034 提供了 Console 登记入口，ADR-0035 提供了安装引导，但**引导是给人看的步骤**，
  实际执行仍是逐条敲命令；
- 2026-10-12 的 55 重装事件证明这条路径容易出错：系统重装清空了
  `/opt/alcor-device-farm`、`/etc/alcor-device-farm`、systemd 单元和本地镜像，
  但控制面的 Host 与 Pool 记录仍在，需要**复用原 Host ID** 而不是重新登记；
- 镜像这一环最容易踩坑：`device_images` 以 **digest** 为身份，而 `docker save`/`load`
  产生的本地 digest 与另一台机器上同一 tag 的 repo digest **可能不同**；
  `create` 不做 pull，宿主缺该 digest 必然 `IMAGE_DIGEST_MISMATCH`。

因此需要把「接入一台宿主机」固化成**可重复、可审查、幂等**的动作。

## 决策

1. 新增 `scripts/onboard-device-host.sh`，把已确认的接入步骤固化为一个脚本：
   前置检查 → 系统用户与目录 → 写 `host-agent.env` → 安装 systemd 单元 → 自检。
2. **脚本不登记 Host、不动镜像、不动池容量。** 三者都必须由管理员显式决定：
   - 登记走既有 `POST /api/v1/device-hosts`（ADR-0034），Token 不进脚本；
   - 镜像按 ADR-0015/DF-035 的既有链路或标准流程第 5 节手工搬运；
   - 池容量按 ADR-0038 的按需模型由管理员通过既有 `PUT /api/v1/device-pools` 调整。
3. 脚本**幂等**：已存在的 `host-agent.env` 默认不覆盖（需 `--force-env`），
   系统用户与目录重复创建安全。
4. 脚本提供 `--check-only` 只读模式，可在改动任何系统状态前验证前置条件
   （Linux/x86_64、`/dev/kvm` 读写、docker 组与 daemon、磁盘下限、控制面 `healthz`）。
5. 脚本提供 `--unit-file`，使其**不依赖仓库检出布局**即可在目标机执行；
   缺少单元模板时必须在改动系统**之前**失败，避免留下「建了用户、写了配置却没装服务」
   的半成品状态。
6. 新增 `docs/host_onboarding_standard_process.md` 作为流程与运维基线，记录机制原因、
   验证 SQL、真实踩坑和脚本设计约束。

## 边界

- 不新增 API、表、状态或 Alcor 业务对象。
- 不新建远程执行通道：Server 仍不通过 SSH 或 Docker Socket 管理远端 Host
  （ADR-0031 决策 6）。脚本由管理员在目标宿主机本地执行。
- 不替代官方镜像准备链路（ADR-0015），也不修改 Pool/Device/Reservation 语义。
- 不处理 iOS/macOS 宿主机；macOS 仍按 `deploy/ios-host/` 的既有方式接入。

## 后果

- 新增或重装 Linux 宿主机变成一条命令，减少漏步骤和配置漂移；
- 镜像与池容量仍是**显式人工决策**，不会因为脚本重跑而意外改变调度行为；
- 55 的重装流程有了可复现依据，后续每台服务器走同一路径。

## 验收

- `--check-only` 在真实宿主上通过且不改动系统；
- 在清空 `/opt/alcor-device-farm`、`/etc/alcor-device-farm`、systemd 单元与
  `device-farm` 用户后，脚本一次执行即可让该宿主在控制面恢复 `online`；
- 重复执行不覆盖已有配置、不改变已生效的 Host ID；
- 缺少单元模板时在改动系统前失败，且不留半成品。