# DF-052 多设备远控安装目标固定验收证据

验收日期：2026-08-21

## 结论

通过。iOS 同一浏览器并行打开两台 Simulator 时，每个页面的临时 App 安装都固定到本页 UDID；Android 的 STF claim、远程连接、单设备 Web 入口和释放统一使用预约 Device 的有效 STF serial。测试设备农场已部署，正式 Alcor 与正式环境未修改、未部署、未重启。

## 实现与复用边界

- 继续复用 Baguette 0.1.92 的原生 `POST /simulators/:udid/files` 和 `xcrun simctl install <udid>`，没有新增 IPA 上传器、安装器、App Build 或 Artifact 模型；
- iOS Gateway 的 HttpOnly Cookie 名按 UDID 派生，请求路径或同源 Referer 决定目标会话；多会话下目标不明确、Cookie 与路径不一致时失败关闭；
- 继续复用 STF 原生 APK 安装页，Android Scheduler、Reservation 和远控入口统一使用 `COALESCE(stf_serial, serial)`，不自动选择第一台 ADB 设备。

## 真实 iOS 双设备验收

环境：测试 Device Farm `10.0.30.171`，Mac Host `10.0.33.68`，Baguette 0.1.92。SSH 安全通道恢复后，Server 容器内的 `127.0.0.1:4842/simulators.json` 可访问。

两台处于 `ready / healthy / Booted` 的真实 Simulator：

- Device `6a8e8c42-99a2-4665-bb48-9fab00bbc23c`，UDID `9FECC106-61FF-4D95-9A65-FFA3AAE92F52`；
- Device `8996dd73-330d-4e33-b08b-b6e4e85e2922`，UDID `1EFEE868-20F6-4F85-B7E7-C7E2E48E219C`。

使用公开的 Sauce Labs iOS Simulator 示例包进行真实安装，测试文件 SHA-256 为 `96b08d5ac74dd817d95fbd8332ae9385bb076af38d56d13d8465345cb1797139`。同一 Cookie 容器先后进入两台设备页面后保存了 2 个不同的按 UDID 会话 Cookie，结果如下：

| 检查项 | 结果 |
| --- | --- |
| B 设备 Cookie 向 A 的 `/simulators/<A-UDID>/files` 上传 | HTTP 401，未转发安装 |
| 双会话 Cookie 向 A 的明确 UDID 上传 | HTTP 200，`{"ok":true,"kind":"app"}` |
| 双会话 Cookie 向 B 的明确 UDID 上传 | HTTP 200，`{"ok":true,"kind":"app"}` |
| 释放 A、B Reservation 后继续上传 | HTTP 401 |
| 测试结束后的 pending/active Reservation | 0 |

Baguette 的成功响应只在对应 `xcrun simctl install <udid>` 退出成功后返回，因此文件仅上传到 Mac、出现在下载目录或安装到其他设备都不能产生上述成功结果。

## Android 真实回归

Android 基础设备 `f05429f0-39c2-47be-a7da-8c73a6f34207` 创建真实远控 Reservation 后：

- STF 单设备入口目标：`10-0-30-171.nip.io:32819`；
- 数据库 `COALESCE(NULLIF(stf_serial,''), serial)`：`10-0-30-171.nip.io:32819`；
- 两者完全一致，挂断后 pending/active Reservation 为 0。

自动化测试还覆盖 `stf_serial` 与通用 `serial` 不同的情况，确认 claim、remoteConnect、Web 单设备入口和 release 始终使用同一 `stf_serial`。

## 自动化门禁与部署

- `go test ./...`：通过；
- `go vet ./...`：通过；
- Console 9 个测试文件、44 项测试：通过；
- Console `pnpm build`：通过；
- `git diff --check`：通过；
- 测试 Server 二进制版本标记：`df052-test`，SHA-256 `1fe0dc314b391f6d2c3b5be86fa67caae039eaaacb41c17c71d205f9f2491be6`；
- `http://10.0.30.171:18080/readyz`：`ready`；
- 本地 Alcor `http://127.0.0.1:18888/`：HTTP 200；
- 正式 Alcor 和正式环境未访问、未修改、未部署、未重启。

测试 Server 原二进制保留在 `/usr/local/bin/device-farm-server.df051.bak`，需要时可回滚。验收使用的临时示例包、页面和 Cookie 文件已从测试主机 `/tmp` 删除。
