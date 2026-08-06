# DF-028 控制台部署、安全和真实 Web 验收

## 当前结论

**completed**。Device Farm Console 已纳入真实 Linux KVM 主机的生产镜像、HTTPS 反向代理、健康检查、升级和回滚链路，并在一台 Android 16 Emulator、真实 STF/RethinkDB、Appium 和 Device Farm Server 上完成浏览器 E3 验收。管理员密码、Service/Agent/STF Token、数据库凭据均未写入仓库或证据。

按 ADR-0010，Console 不展示 STF `remoteConnect` 返回的 ADB TCP 地址，也不把它冒充浏览器远控页面；后端 STF Adapter 的 claim、release、remoteConnect 等既有能力保持不变。E3 验收确认预约页没有“STF 远控”按钮，并继续验证真实 STF 设备可见性、预约编排、重启收敛和 Token 隔离。

## 正式部署

- Linux KVM 主机：`10.0.30.171`；访问地址：`https://10.0.30.171:18443/console/`；
- Server 镜像：`alcor-device-farm:df028-web-final-20260807`；镜像 ID：`sha256:fc6e9d70298dee35745953da7f1392a782c928bf900336d964dfe3dd937037c3`；
- Server 容器：`alcor-device-farm-server-df017`；HTTPS 代理：`alcor-device-farm-console-proxy-df017`；STF：`alcor-device-farm-stf-stf-1`；
- 最终静态资源：`index-DFR4sXnn.js`、`index-BCFhMy6H.css`，入口不缓存，内容哈希资源长期 immutable；
- 实验室使用自签名证书，部署自检通过 `DEVICE_FARM_CONSOLE_INSECURE_TLS=1` 显式允许；正式环境禁止启用该选项，必须使用可信证书；
- 本轮最终只读复核：Console HTTPS 200、`/readyz` 200，Server/Proxy 运行，STF `healthy`。

## 自动化与浏览器验收

完整输出摘要见 [local-gate.txt](local-gate.txt) 和 [real-gate.txt](real-gate.txt)。

```text
PASS scripts/dev.ps1 -Task check
PASS Orval + TypeScript + Vite production build
PASS Vitest: 4 files / 7 tests
PASS Playwright local: 4 passed / 3 E3 skipped
PASS Playwright Web security E3: 2 passed
PASS real login/dashboard E3: 1 passed
PASS real reservation/renew/release/audit E3: 1 passed
PASS real quarantine/unquarantine E3: 1 passed
```

真实浏览器流程覆盖：

1. 未认证 API 返回 401，错误密码被拒绝；
2. 缺少 CSRF 的写请求返回 403，注销后的会话再次访问返回 401；
3. CSP、防缓存、哈希资源缓存策略和构建产物敏感标记扫描通过；
4. 管理员登录后查看真实设备、主机、镜像、池和容量总览；
5. 创建人工预约并等待 `active`，按稳定 Device ID `67434725-32ff-4870-8c55-ad46fd5d9486` 跟踪设备；
6. 确认 Console 不显示伪 STF Web 入口，完成续租、释放并在审计页看到操作原因；
7. 对真实 Android 设备执行隔离和解除隔离，页面与 Server 状态分别收敛为 `quarantined` 和 `ready`。

真实 Emulator rebuild 后 serial 和 ADB 端口可能变化，因此 E3 使用数据库稳定 Device ID 跟踪，不依赖瞬时 serial。

## 页面证据

- [登录页](screenshots/login.png)
- [运行总览](screenshots/dashboard.png)
- [活动预约](screenshots/reservation-active.png)
- [设备域审计](screenshots/audit.png)
- [设备已隔离](screenshots/device-quarantined.png)
- [设备恢复 ready](screenshots/device-ready.png)

## 重启、回滚和清理

- Server 重启后状态重新收敛；Console Nginx 代理约 1 秒恢复；STF health 约 5 秒、设备状态约 2 秒恢复；
- 回滚到 `alcor-device-farm:df021-stf-recovery10-20260806` 后 HTTPS/ready 正常，再恢复到最终 DF-028 镜像成功；
- 回滚演练记录：`ROLLBACK converged_seconds=1`，`FORWARD_RESTORE converged_seconds=0`；
- 已删除本轮停止的临时回滚容器、临时镜像和远端构建目录，撤销本轮测试会话；
- 收口状态：开放 Reservation 0，本轮测试会话 0，真实设备 `ready|healthy`；用户部署前已有资源未被删除。

## 非阻塞说明

Vite 仍提示单入口 bundle 大于 500 kB。该项是 P2 前端加载优化，不影响 DF-028 的功能、安全、部署或真实设备验收。
