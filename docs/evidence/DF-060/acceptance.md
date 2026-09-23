# DF-060 设备名称与指定设备预约验收

## 结论

通过。设备名称已合入现有设备详情进行编辑和保存，没有新增独立“改名”入口；日常列表和资源引用以名称为主，完整设备 ID 仅在详情中保留。预约接口支持指定具体设备，目标设备正在使用时预约保持等待，释放后只会分配到原先指定的同一台设备。

## 实现范围

- Device 新增可编辑 `name`，长度限制为 2–40 个字符；Android 与 iOS 新设备均生成可读默认名称。
- 控制台设备列表以名称为主，设备详情内提供名称输入框和“保存”按钮；设备 ID、序列号继续在详情中完整展示。
- `PATCH /api/v1/devices/{id}` 更新名称并记录审计事件，资源之间仍使用不可变 ID 关联，因此名称更新不会破坏设备池、预约或历史记录。
- `POST /api/v1/reservations` 新增可选 `requested_device_id`；服务端校验设备属于请求的活动设备池。
- 指定设备处于 busy/reserved 时，Reservation 保持 pending；调度器释放后仅匹配该设备，不会改派同池其他设备。
- Alcor Adapter 请求类型同步支持 `requested_device_id`，为平台创建运行选择具体设备提供接口契约。

## 自动化验收

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `pnpm test -- --run` | 10 个测试文件、54 项测试全部通过 |
| 设备详情内编辑名称专项测试 | 通过 |
| 指定 busy 设备等待并在释放后分配同一设备的调度测试 | 通过 |
| `pnpm run build` | 通过 |
| `git diff --check` | 通过 |

## 数据与兼容性

- 数据迁移为已有设备回填可读名称，并为名称建立约束；回滚脚本同步提供。
- 未传 `requested_device_id` 的旧客户端仍按原设备池逻辑预约。
- 名称只负责展示和人工识别，内部引用继续使用设备 ID；修改名称后当前页面重新查询最新值，既有运行快照保留创建时名称作为历史兜底。

## 30.171 正式部署

- 上线前 active、pending、releasing Reservation、开放 Session 和在途 Host Command 均为 0。
- PostgreSQL 迁移前备份保存在 `/home/kerr/device-farm-backups/device-farm-before-8cd498d-20260901.dump`，SHA-256 为 `b8acc5e1a11b2db8cd2e8ecc0763ee43e44c1e5b1af0cba79ff3aabd8cea9ded`；`pg_restore --list` 验证通过。
- `000018` 迁移成功，为 4 台已有设备全部回填合法名称。
- 正式 Server 镜像为 `alcor-device-farm:8cd498d-targeted-device-20260901`，内嵌版本和提交均为 `8cd498d`；原 `97c0d8e` Server 与原 iOS 隧道保留为停止状态的即时回滚容器。
- Server `/healthz`、`/readyz` 和正式 HTTPS Console 均返回 200；iOS SSH 隧道已按新 Server 网络空间重建，Baguette inventory 可访问。
- 两台宿主机均为 online、非 draining；4 台长期设备均为 `ready/healthy`，设备列表 API 返回非空名称、平台和所属 Pool。

Alcor 侧改动不随 Device Farm 直接部署，只通过现有 PR 审核上线。
