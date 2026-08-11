# ADR-0015：官方 Android 目录与按需镜像准备

## 状态

已批准，按 DF-035 实施。

## 决策

Server 同步 Android SDK 官方稳定频道的 System Image 目录，并将 Android/API、映像类型和 ABI 作为可下载候选。Console 只读取该目录并提交受限的准备请求；CPU、内存、数据盘、分辨率、DPI、图形模式仍由 `runtime_profile` 管理。品牌不是官方 System Image 属性，只能作为硬件显示预设。

Server 只创建异步、可审计的构建命令。受控 Build Agent 使用固定版本 `sdkmanager` 下载指定官方包、以 `avdmanager` 创建 AVD，叠加既有 Appium、UiAutomator2 与启动脚本，推送内部 Registry 并获取不可变 digest。通过既有镜像验证后才可写入 `device_images`；相同 digest 直接复用缓存。目录的“官方已更新”不自动替换任何已验证成品。

## 后果

浏览器和 API Server 均不直连 Google，也不接受任意下载 URL 或命令。`device_images` 继续只保存可以被 Host 拉取、验证且 digest 锁定的成品，不承担下载任务或候选目录的角色。该流程复用 Docker、Android SDK、Host Command、Agent、Appium Adapter 与既有验证，不新增 Alcor Run、Artifact 或业务队列。
