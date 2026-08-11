# Android 16 历史验收资料

此目录只保留 DF-034 已验收的第三方许可、UiAutomator2 预安装补丁和历史说明，不再提供独立 Dockerfile 或构建脚本。

DF-035 起，Android 16 与其他候选一样由 Console 中的官方稳定目录选择，并通过 [`../images`](../images/README.md) 的唯一 Build Agent 流程按需构建、推送和验证。目录条目不会自动创建四台设备；Pool 仍只使用管理员设置的总目标与默认已验证 Image。

## 固定兼容基线

- 上游：`budtmo/docker-android`
- 上游提交：`e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2`
- Appium 基础版本：`3.5.2-p0`
- UiAutomator2：`8.2.2`
- 历史验收标签：`alcor-device-farm/android-emulator:16.0-api36-r3`

共享补丁与本目录的 `uiautomator2-preinstall.patch` 只处理设备农场需要的 KVM、启动等待、API 36 Appium 兼容和匿名统计关闭差异。容器运行时只映射 `/dev/kvm`，不使用 `--privileged`；CPU、内存、端口、网络和数据卷仍由 Docker Provider 按每台 Device 的有效运行规格管理。

Android 16 的真实验收必须创建 UiAutomator2 Session。当前 API 36 兼容配置允许 `appium:skipDeviceInitialization=true` 和 `appium:ignoreHiddenApiPolicyError=true`；UiAutomator2 Server 安装、instrumentation 启动与 Session 删除仍须真实执行。

第三方许可原文保存在 `UPSTREAM_LICENSE.md`，部署前仍需按内部开源合规流程复核。
