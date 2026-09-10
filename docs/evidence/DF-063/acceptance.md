# DF-063 Android CPU 和内存无损改配验收

## 结论

通过。2026-09-10 已在 `10.0.30.171` 的真实 Linux KVM、Docker Emulator、STF 和 Appium 环境完成验收。纯 CPU/内存调整会保留原数据卷替换容器，目标规格仅在 ADB、Android boot、Appium 和 STF 健康后生效；Image、数据盘和图形模式继续使用明确清空数据的 reimage 流程。

16 GB 主机最终采用每台 Container `4 CPU / 4608 MB`、Android Guest `4 CPU / 3584 MB`、数据盘 `6144 MB`。Android Pool 的 `total_target/min_ready/max_concurrency` 均为 2，两台设备均为 `ready/healthy`。

## 实现范围

- 新增 `POST /api/v1/devices/{id}/runtime-profile-updates`，只接受 Container/Guest CPU、内存和原因，不接受 Image、数据盘、图形、显示或任意 Docker/Emulator 参数。
- Server 复用动态容量预检、PostgreSQL Host Command、幂等和设备域审计；有效规格在命令成功后原子提交。
- Agent 复用 Docker Provider `RestartWithProfile`，先执行 Android `sync`，再正常停止并替换容器；网络和数据卷保持不变。
- 目标失败时使用同一数据卷恢复旧规格一次；恢复成功保留旧有效规格，恢复失败隔离设备。
- Reconciler 把 runtime profile update 识别为在途操作，不在改配期间排队 self-healing restart。
- Console 将“调整 CPU/内存”与“更换镜像/重建数据”分开，分别提示“重启并保留数据”和“清空数据”。
- migration `000019` 增加改配状态和错误字段；OpenAPI 版本更新为 `2.6.0`；设计依据为 ADR-0030。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `go test -p 1 -count=1 ./...` | 通过 |
| `go vet ./...` | 通过 |
| `pnpm test` | 10 个文件、57 个测试通过 |
| `pnpm build` | OpenAPI 客户端生成、TypeScript 和 Vite 生产构建通过 |
| OpenAPI 与 migration 契约测试 | 随 Go 全量测试通过 |
| PostgreSQL migration | 临时库全量 up、`000019` down/up 通过 |
| `python -m unittest deploy/docker-emulator/images/test_non_destructive_start.py` | 通过 |
| 固定上游补丁检查 | `e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2` 上 `git apply --check` 通过 |

Console 测试与生产构建首次并行执行时，Orval 清理生成目录的瞬间使 `App.test.tsx` 导入失败；构建完成后单独重跑全部 57 个测试通过。这是两条验证命令争用同一生成目录，不是产品代码失败。

## 无损改配证据

- Device ID：`ebb4d6c8-a5c5-49f7-8a29-a1a773e88d1c`；Provider ref 和 Pool membership 保持不变。
- 改配前容器 ID：`d872ce0cfc567d2bfc4f906ec743dc017cb1b79c977c098f3367d40953c7df41`。
- 改配后容器 ID：`ae7f574563f589eab100d0b178c0958d894c580831627bda4ab1a89d7f2768c8`。
- 数据卷始终为 `alcor-df-emulator-ebb4d6c8-a5c5-49f7-8a29-0a467cc3-data`，创建时间保持 `2026-09-10T13:11:10Z`。
- Android 文件 `/data/local/tmp/df063-v4-marker` 保持 56 字节，SHA-256 保持 `425c3e713d5bae19b031bc8639c20c6a23e311a54647ba1824cbf45969a11ff4`。
- 已安装包 `io.appium.settings` 保留。
- Docker 限额为 `NanoCPUs=4000000000`、Memory=`4831838208` 字节；QEMU 参数包含 `-cores 4 -memory 3584`。
- 最终 `runtime_profile_update_status=idle`，无 pending profile；同一验收窗口只有一条 `operation_source=management / operation_kind=runtime_profile_update` 命令，无 self-healing 抢占。
- ADB=`device`、`sys.boot_completed=1`、Appium 3.5.2 ready；UiAutomator2 Session 创建成功，`/source` 返回 7,922 字节后正常删除。
- STF RethinkDB 中 serial `10.0.30.171:32831` 为 `present=true / ready=true`。

## 双机与主机容量

- Image ID：`62d976be-c73a-4ca1-bbec-19133dd79591`；镜像为 `127.0.0.1:5001/alcor/android-emulator-df063-v4:15.0-api35-google_apis-x86_64-sdk9`，digest `sha256:f9dde728c544723767f9278785d5a66b81ff727f22520b2ccb7f264c03a864bc`。
- 两台 Android Device 都使用 v4、Container `4 CPU / 4608 MB`、Guest `4 CPU / 3584 MB`，均为 `ready/healthy`。
- 两个容器的 Docker `OOMKilled=false`、RestartCount=0；ADB 和 Android boot 均通过。
- 两台同时建立 UiAutomator2 Session，`/source` 分别返回 7,934 和 22,387 字节，随后均正常删除。
- STF serial `10.0.30.171:32831` 和 `10.0.30.171:32833` 均为 `present=true / ready=true`。
- Host 动态账本：总内存 `15594 MB`，两台合计分配 `9216 MB`，验收时可用约 `3434 MB`；两台合计 `8 CPU`、数据盘 `12288 MB`，保留了超过 2 GB 的系统内存余量。

## 正式环境回归

- 两台 iOS Simulator 均为 `ready/healthy`。
- Android Host 与 iOS Host 均为 online，心跳持续更新。
- Server 容器内 Baguette `http://127.0.0.1:4842/` 返回 200；iOS 远控网关继续监听。
- 最终开放 Reservation=0、开放 Device Session=0、pending/leased Host Command=0。
- Server `/healthz` 和 `/readyz` 均返回 200。
- 验收期间未输出或提交 Service Token、Agent Token、STF Token、数据库口令、Console 密码或 SSH 私钥。

## 真实故障与收敛

1. 初版 Reconciler 未识别 runtime update 在途状态，心跳故障判断会抢先排队 self-healing restart；已增加在途保护。
2. `Pixel 9` 与 AVD 名 `pixel_9` 比较未正规化，曾误执行强制创建 AVD；已正规化名称，避免重建 userdata。
3. 持久化目录缺少 AVD `.ini` 索引；已设置 `ANDROID_AVD_HOME` 并补齐引用文件。
4. 直接强制删除容器时 Android 数据可能未落盘；已增加 `adb shell sync` 和最长 30 秒正常停止。
5. v3 镜像遗留 AVD 锁导致目标规格和恢复旧规格都失败；v4 只在没有同名 QEMU 进程时删除持久 AVD 目录中的 `*.lock`，不碰 userdata、APK、应用数据或快照。
6. 心跳可在替换窗口先把设备推进或隔离，r4 因状态不等于 provisioning 而不提交已经成功的结果；r5 支持从 booting、ready 或 quarantined 协调成功/恢复结果，并回到 ready。
7. 镜像验证曾因动态容量账本不足被拒绝；修正验收顺序和规格后完成真实镜像验证。
8. 原计划每台 Container 5120 MB、Guest 4096 MB；主机总内存 15594 MB，加上 STF、数据库、MicroK8s 等服务后，两台无法同时满足 2048 MB 系统余量，因此最终采用 4608/3584。

r4 遗留过一条“容器已被自愈恢复旧 5120/4096、数据库仍 pending 4608/3584”的记录。发布 r5 后先按实际容器值清理该失效 pending，再通过正式 API 重新执行；最终数据库、Docker 限额和 QEMU 参数一致，未把历史成功结果直接写成与容器不符的规格。

## 部署与回滚

- 变更前数据库备份：`/home/kerr/device-farm-backups/device-farm-before-df063-20260910.dump`，SHA-256 `b2e0312fc01bdcb01e69d316953a7bc7d0623b703ef5d13dc0febe0236030efc`。
- 变更前 Agent 二进制备份：`/home/kerr/device-farm-deploy-backups/device-host-agent-before-df063-20260910`，SHA-256 `944ec7dbdbcce64a3df6af31a7bf3aba951a4789c63b695a1bd604dbf445eb99`。
- 原 Server `alcor-device-farm:b808a75-auto-recover` 和验收前 r4 容器均以停止状态保留用于回滚。
- r5 预发布 Server 版本为 `0.1.0-df063-preacceptance-r5`；最终提交后以同一已验收源码按 commit hash 重建并再次核对版本、健康和双机状态。
