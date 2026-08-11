# DF-036 旧镜像受控停用和可用镜像选择验收

## 结论

**completed（2026-08-11）**。设备镜像页默认只展示 `ready` 镜像，管理员可以在同一页面选择目标设备池并把任意 `ready` 镜像设为默认镜像；已停用镜像进入独立归档视图，不再出现在设备重装或默认镜像候选中。选择默认镜像只影响后续补建，不会重装现有设备。

旧 DF-035 前镜像 `b6492bc3-4e37-457e-a5f8-f4a30f46959f` 已通过受控 API 停用。生产可用列表目前只有新官方 Android 16 镜像，旧记录仅在归档视图保留，以维持设备域审计和历史外键完整性；本任务没有物理删除 Registry 镜像层或历史数据库记录。

## 实现与安全边界

- OpenAPI 提升到 `1.7.0`：镜像列表增加 `status` 过滤，新增 `POST /api/v1/device-images/{id}/retirements` 和 `PUT /api/v1/device-pools/{id}/default-image`。
- 停用事务锁定 Image，拒绝仍是任一 Pool 默认值或被非 `deleted` Device 引用的镜像；成功时原子更新 `device_images.status=disabled`、禁用全部 Pool Image 关系并写设备域审计。
- 通用 Image 更新接口不能再用 `enabled=false` 绕过停用校验，停用只有一条受控入口。
- 默认镜像选择事务只接受 `ready` Image，自动新增或启用 Pool Image 关系、更新 Pool 默认值并审计；已有 Device 的 `image_id` 不变。
- Console 的“可用镜像 / 已停用归档”视图分离；可用镜像提供“选择使用”和“停用”操作，选择时必须明确设备池和原因。
- Devices 页面只查询 `ready` 镜像作为重装候选。没有新增 Case、Dataset、Target、Run、评分、报告或 Artifact 业务索引，也没有复制 DaFit Runner 或 STF 能力。

## 自动化门禁

```text
PostgreSQL 17.10 migration verification
000001～000010 up / constraints / down / up-down-up: passed
repository / scheduler / reaper / reconcile / hostcommand / metrics / API / warmpool: passed

go test ./...
PASS（全部 package）

Console
Test Files  7 passed (7)
Tests       30 passed (30)
tsc -b     passed
vite build passed（仅既有 bundle size warning）
```

API 集成测试覆盖：默认镜像冲突返回 409、活动 Device 引用冲突返回 409、只存在历史 `deleted` Device 引用时停用成功、Pool Image 关系同步禁用、审计只写一次、`ready`/`disabled` 分页过滤、选择候选镜像后成为 Pool 默认值且已有 Device 不发生重装，以及停用镜像不可重新选择。

## 生产备份、部署与清理

真实主机：`10.0.30.171`。清理前 PostgreSQL 备份：

```text
/home/kerr/alcor-device-farm-runtime-20260806/backups/df036-before-image-retirement-20260811.dump
sha256: 97b64ddfbfb1da32c5cefdf64755ee0e950118d6cc7b4342ed1210660bdde8f6
```

最终 Server 镜像与运行容器完全一致：

```text
image: alcor-device-farm:df036-final-20260811
image id: sha256:112339c895984e4c01b2f5327f782aaa83724b8a8b8eda60e64dfd68e5d989c0
binary sha256: bd00bf9a75662ebf254a334e59932f7147bb1aed3afefd8165f1b98530c16db3
user: 65532:65532
entrypoint: /usr/local/bin/device-farm-server-entrypoint.sh
```

部署先用独立端口验证修正版镜像，再切换 `alcor-device-farm-server-df017`；上一组装层保留为 `alcor-device-farm-server-df036-assembly-rollback`，DF-035 Server 也继续保留用于回滚。最终 `/readyz` 返回 `ready`。

旧镜像停用：

```text
Image ID: b6492bc3-4e37-457e-a5f8-f4a30f46959f
名称: android-16-api36-df017
原因: 旧验收镜像已由官方镜像替代
状态: disabled
retire_device_image audit count: 1
```

清理后查询 `status=ready` 返回 `total=1`，只有：

```text
Image ID: 60651f99-400b-4bb7-a915-55bdbd44502c
名称: android-api36-google_apis-x86_64-894537cec0ee-01db1caad6
Digest: sha256:894537cec0eec1039a8fa40a82a5a3bd8366b91c75df8b19fa1d246401331ffa
```

查询 `status=disabled` 返回 `total=1`，即上述旧镜像。旧 Pool Image 关系已禁用，归档仍可追溯。

## 真实选择与设备链路验收

通过新 API 在 Pool `android16-single-df017` 上真实执行一次“选择使用”，理由为“验证镜像页默认选择能力”。结果：

```text
Pool ID: 8b97a9e7-ab6c-41ff-bc1e-bd922b829ab3
default_image_id: 60651f99-400b-4bb7-a915-55bdbd44502c
select_pool_default_image audit count: 1

Device ID: f1d2a677-e05f-4926-80dc-c113cd5b9598
image_id: 60651f99-400b-4bb7-a915-55bdbd44502c
lifecycle_status: ready
health_status: healthy
reimage_status: idle
```

选择前后 Device Image 未变化且没有创建重装命令，最终在途 Host Command 为 0。真实接入复核：

```text
running emulator containers: 1
ADB: 10-0-30-171.nip.io:32795 device
STF API: device present
Appium: ready=true, version=3.5.2
```

## 回滚

应用回滚：停止当前 Server，恢复 `alcor-device-farm-server-df036-assembly-rollback` 或 DF-035 保留容器。数据回滚优先通过受控接口重新选择需要的 `ready` 镜像；若必须撤销本次生产数据变化，先停止新 Server，再恢复上述 PostgreSQL dump。归档镜像没有被物理删除，因此恢复数据后仍可追溯；不得在存在 Pool 默认值或活动 Device 引用时直接改库删除 Image。
