# ADR-0016：受控镜像停用和设备池默认镜像选择

## 状态

accepted

## 背景

DF-035 引入官方目录和按需准备后，既有手工镜像仍可能作为历史 `device_images` 保留。直接物理删除会破坏 Device、Pool 和审计外键；继续把它与当前可用镜像并排展示又会误导管理员。现有 Pool 页面可以修改默认镜像，但 Image 页面没有把已验证成品与使用入口连起来。

## 决策

1. “清理旧镜像”定义为受控停用，不物理删除 `device_images`、历史 Device 或审计事件。
2. Image 仍被任一 Pool 作为默认值，或被任何非 `deleted` Device 引用时，停用必须返回冲突。
3. 停用成功时原子把 Image 状态改为 `disabled`、禁用其全部 Pool Image 关系并写设备域审计；历史引用保持可查询。
4. Console 默认只查询 `ready` Image，并提供已停用归档视图。
5. 管理员可从 Image 页面选择一个 `ready` Image 和目标 Pool。Server 原子启用对应 Pool Image 关系并更新 `default_image_id`；该选择只影响后续自动补建，不重装已有 Device。
6. 本决策不删除内部 Registry manifest 或 Host Docker layer。物理镜像垃圾回收若需要，必须另行设计引用扫描、保留期和回滚策略。

## 结果

管理员看到的默认列表只包含实际可选镜像，旧记录仍满足审计和回滚追溯要求。Pool 默认切换有统一服务端校验，不依赖 Console 拼接多个非原子请求，也不扩展到新版 Alcor 业务域。
