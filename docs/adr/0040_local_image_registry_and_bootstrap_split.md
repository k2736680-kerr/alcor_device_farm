# ADR-0040：宿主机自带本机镜像 registry，并拆分 Bootstrap 与 Agent 接入

## 状态

已批准（2026-10-13）。

## 背景

df017 这一批服务器要接入设备农场，但**新机器的系统是全新的**（无 Docker、无 SDK、
无镜像、无农场痕迹）。推进过程中查出三个具体障碍：

1. **`device_images.docker_image` 全部指向 `127.0.0.1:5001/...`，但该 registry
   在任何宿主机上都不存在**：55 无监听；171 上 `127.0.0.1:5001` 连接被拒，
   其 `:5000` 是无关的 `vega-face-search` nginx（`/v2/` 返回 404）。
2. **官方镜像准备链路必须 push**：`deploy/docker-emulator/images/prepare.sh`
   依赖 `DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY` 并执行 `docker push`，
   再用推送后的 `RepoDigests` 作为 `docker_digest`。没有 registry 就没有官方构建产物。
3. **`create` 永不 pull**：镜像引用经宿主本地解析，宿主缺该 digest 必然
   `IMAGE_DIGEST_MISMATCH`。

同时实测澄清了一个此前的误解：**`docker save` 的 tar 不是字节确定的**
（同一镜像两次 save，sha256 不同），因此 tar 哈希不能作为镜像身份，
只能用 load 后的 image ID / repo digest。

## 决策

1. **每台宿主机自带一个只监听回环的本机 registry**，统一前缀
   `127.0.0.1:5001/alcor/android-emulator`，数据存 docker volume，
   `restart unless-stopped`。安装入口 `scripts/install-local-image-registry.sh`。
   - 好处：`device_images.docker_image` 在各宿主间保持一致；`create` 的本地解析成立；
     官方 `prepare.sh` 链路有一个真实可推送的目标。
   - 安全：只绑定 `127.0.0.1`，无认证也可接受，因为不可从网络访问。
   - **边界**：一个宿主无法从另一个宿主的 registry 拉镜像（已实测 55 → `10.0.30.54:5001`
     连接被拒）。因此**每台宿主必须各自独立取得镜像**，不能靠网络共享。

2. **接入一台宿主机拆成三层，互不混合**：

   | 层 | 脚本 | 职责 | 风险面 |
   |---|---|---|---|
   | 0 Bootstrap | `scripts/bootstrap-device-host.sh` | Docker、KVM 组与 `/dev/kvm` 权限、udev 规则、Android cmdline-tools | 机器级配置，需 root 与网络 |
   | 1 Agent 接入 | `scripts/onboard-device-host.sh` | `device-farm` 用户、`host-agent.env`、systemd 单元、自检 | 只动农场自身 |
   | 2 镜像分发 | `scripts/install-local-image-registry.sh` + `scripts/distribute-emulator-image.sh` | 本机 registry、镜像搬运与 digest 核对 | 影响调度正确性 |

   拆分的理由：三者的前置条件、回滚方式和排障路径都不同。Bootstrap 改的是
   内核模块、系统组和 docker 源；Agent 接入只涉及农场自己的文件与单元。
   混在一起会让「装失败」难以定位，也无法单独重跑。

3. **镜像分发脚本以 load 后的 digest 为权威校验**，不以 tar 的 sha256 为身份依据；
   tar 的 sha256 只用于检测单次传输是否损坏。

4. **`Bootstrap` 默认交互确认**，`--yes` 用于自动化；`--check-only` 为只读勘察，
   **不需要 root**。

## 边界

- 不新增 API、表、状态或 Alcor 业务对象。
- 不替换官方镜像准备链路（ADR-0015 / DF-035），只是为它提供本机推送目标。
- 不新建跨机镜像分发服务；registry 不对外暴露。
- 不改 Pool/Device/Reservation 语义，不改池容量。
- 不自动改 `device_images` / `device_host_image_states`；镜像登记仍走既有链路。

## 后果

- 全新服务器可以从零走完 Bootstrap → Agent 接入 → 镜像导入，且每步可独立重跑；
- 镜像 digest 在同一份内容上跨机一致（已实测，见
  `docs/evidence/DF-071/acceptance.md` §5.3），但**逐宿主的可用性记录依然必要**，
  因为「这台机器有没有这个镜像」是逐机器事实；
- tar 哈希不再被误当作镜像身份。

## 验收

- 本机 registry 在 54、55 上安装成功，`/v2/` 返回 200，push/pull 冒烟通过；
- 同一镜像在 54 与 55 上 push 后 digest 一致（`4bfbcf34…`）；
- 55 无法访问 54 的 registry，确认回环绑定的边界成立；
- `bootstrap --check-only` 无需 root 即可给出机器现状；
- 三个层级的脚本 `sh -n` 语法检查全部通过。