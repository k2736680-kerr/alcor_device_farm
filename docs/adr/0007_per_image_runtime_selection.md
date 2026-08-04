# ADR-0007：设备镜像按自身运行引用启动

## 状态

已接受。

## 背景

`device_images` 已能登记多个 Android API Level，但 Host Agent 原先只读取全局 `DEVICE_FARM_DOCKER_IMAGE`。因此后台选择不同 Image 时，实际仍会启动同一个 Docker 镜像，无法支持 Android 14、15、16 并存。

## 决策

- `device_images` 增加 `docker_image`，保存显式版本标签或不可变 digest 引用；`docker_digest` 继续保存期望摘要；
- 历史 Image 不猜测镜像仓库，迁移后退回 `draft` 并标记 `IMAGE_REFERENCE_REQUIRED`；
- validation、create、管理员 rebuild、释放后 rebuild 的 Host Command 都携带 `docker_image` 和 `docker_digest`；
- Host Agent 在删除或创建设备前，先校验命令中的运行引用和摘要；Docker Provider 每次按命令选择镜像，不再依赖全局镜像作为生产选择；
- 容器标签记录实际运行镜像，Provider 原生 rebuild 从标签恢复同一镜像；
- `DEVICE_FARM_DOCKER_IMAGE` 仅保留为直接 Provider 集成测试的可选回退值，正式 Host Command 缺少镜像字段必须失败；
- 后台增加 Android 版本时只新增并验证 Device Image，不修改 Scheduler、Reservation、Pool、Device 或真机扩展架构。

## 后果

- Android 14、15、16 可以在同一 Host Agent 下按 Image 独立创建；
- 镜像引用与摘要不匹配时，不会删除现有设备或启动错误版本；
- 后续接入私有镜像仓库只改变 Image 数据和宿主机镜像分发，不改变设备调度链路；
- 真机没有 `image_id`，不受 Docker 镜像字段影响。
