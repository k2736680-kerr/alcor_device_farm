# DF-031 管理员设备远程控制验收记录

## 1. 结论

DF-031 于 2026-08-10 在真实 Linux KVM、Android 16 Docker Emulator、STF 3.7.9、PostgreSQL 16 和 Edge 浏览器环境通过。管理员可在设备页点击“远程连接”，无需输入 STF 账号密码即可进入指定设备的 STF 原生控制页；看屏、点击、滑动、输入、Home、返回、挂断、关闭标签页、异常退出回收和 STF 主动释放均已验证。

本任务没有新增第二套看屏、触控、日志、文件管理、Appium Runner 或业务报告；设备占用仍以 `device_reservations` 为唯一真相，STF 仅通过 Adapter 和原生页面复用。

## 2. 验收环境与版本

- Linux KVM 主机：`10.0.30.171`
- 正式 Console：`https://10.0.30.171:18443/console/`
- 正式 Server：`alcor-device-farm-server-df017`，镜像 `alcor-device-farm:df031-20260807`
- 验收镜像：`sha256:5a4f311afd510f5cd0a81dac0d4707f5e35ef7fc4404d8eea5645d55254e82df`
- 回滚容器：`alcor-device-farm-server-df017-rollback-df031`，保持停止状态
- STF：`devicefarmer/stf:3.7.9`
- PostgreSQL：`postgres:16-alpine`
- 真实设备 ID：`834af4a4-be48-4144-9587-8b9c33afb8f0`
- STF Web 独立主机名：`http://10-0-30-171.nip.io:7100`

正式用户密码未读取、未修改、未记录。为执行自动验收，隔离 canary 使用临时管理员密码生成与正式管理员非敏感元数据一致的短时会话，随后全部设备页、远控 API、STF 跳转和挂断请求均通过正式 `18443` 入口完成；正式用户文件未变更。

## 3. 自动门禁

- `go vet ./...`：通过。
- `go test ./...`：通过，包含精确 Device 选择、并发单 owner、远控服务、API、配置、Reaper 和 Scheduler 测试。
- Console `pnpm test`：7 个测试文件、18 个测试全部通过。
- Console `pnpm build`：通过；Orval 生成、TypeScript 编译和 Vite 生产构建成功。
- OpenAPI canonical LF SHA256：`778b5ad2d5ebda9a1622e5ecacdf14cc851cc5db63218f008d99b12220126773`，与仓库校验文件一致。
- 真实 PostgreSQL 集成测试：指定设备分配、第二条远控冲突、真实并发单 owner、并发 release、双 Reaper 和 grace period 全部通过；临时验收数据库已删除。

## 4. 真实浏览器与手机操作

正式入口 Playwright 验收通过，STF 最终地址为 `http://10-0-30-171.nip.io:7100/#!/control/<serial>`，JWT 在 STF 登录重定向后从地址移除。浏览器真实完成：

1. 设备页只在管理员与 `ready/healthy` 设备上显示“远程连接”；
2. 点击后自动创建指定 Device 的短 Reservation 并完成 STF claim；
3. 无 STF 二次登录，直接显示指定 serial 的真实 Canvas 画面；
4. 真实点击、滑动、文本输入、Home 和返回均引起设备画面变化；
5. Console 点击“挂断”后显示清理提示，并自动关闭 STF 标签页；
6. 正式挂断预约 `be4b2a19-7c11-4352-8546-fd69c4d27f38` 最终为 `released`，设备恢复 `ready|healthy`，open reservation 为 `0`，STF `using=false`。

截图证据：

- `device-ready.png`
- `stf-connected.png`
- `stf-connected-explore.png`（STF 原生页面结构与 Canvas 辅助核验）
- `stf-click.png`
- `stf-swipe.png`
- `stf-input.png`
- `stf-home-back.png`
- `console-hung-up.png`

## 5. 清理与异常恢复

| 场景 | 真实结果 |
|---|---|
| 点击挂断 | STF 标签页自动关闭；Reservation `released`；设备重建后 `ready|healthy`；STF `using=false` |
| 关闭 STF 标签页 | 结束 API 返回 `ended`；预约 `f0e0ad6d-7198-4532-9f24-c07dc9ddbca1` 为 `released`；open=`0`；STF `using=false` |
| 浏览器异常退出/断网 | 阻断 heartbeat 和 DELETE 后关闭浏览器；预约 `3f34ba89-71cb-4a2a-a5ff-a563c2e48021` 初始为 `active|busy` 且 STF `using=true`；短租约和宽限期后由 Reaper 变为 `expired`，设备 `ready|healthy`，open=`0`，STF `using=false` |
| STF 页面主动释放 | STF release 后下一次 Console 心跳自动收敛并关闭标签页；预约 `8431fe0e-3e33-41ef-8b3c-a2cbb50f8f78` 为 `released`，设备 `ready|healthy`，open=`0`，STF `using=false` |
| STF 重启 | STF 容器恢复 `healthy` 后设备保持/恢复 `ready|healthy`，无 pending/active Reservation |

所有状态均按 Device ID 查询；模拟器重建会改变 ADB/STF serial，未使用旧 serial 冒充恢复成功。

## 6. 安全验收

- 远控 API 响应使用 `Cache-Control: no-store`，不返回 STF API Token 或签名 Secret。
- STF 登录 JWT 最长 60 秒，登录后地址不再包含 `jwt` 查询参数。
- 正式浏览器 URL、Local Storage、Session Storage 和 Cookie 与真实 STF API Token/签名 Secret 做同值比对，结果为零命中。
- 正式 Server、canary Server 和 PostgreSQL data-only dump 与真实 STF API Token/签名 Secret 做同值扫描，结果为零命中；日志中无 `?jwt=`。
- STF 3.7.9 会把含 `--auth-secret` 的子进程命令打印到 INFO stdout。部署已将 STF 主容器日志驱动设为 `none`，避免 Docker 持久化该 Secret；ADB、RethinkDB 日志和 STF 页面内设备 Logcat 不受影响。
- Console HSTS 与 HTTP STF 不共用主机名。验收先加载正式 HSTS，再通过独立 STF 主机名完成真实远控，未发生 HTTPS 错误升级。

## 7. 已知限制与后续演进

- 当前只支持一名管理员控制一台设备；多用户排队、共享观察和 Alcor 身份透传不在 DF-031 范围。
- `10-0-30-171.nip.io` 是当前实验网络的解析名。正式内网 DNS 可用后应替换为受控内部名称；长期方案是为 STF App、WebSocket 和屏幕端口统一提供 HTTPS/WSS。
- STF 进程 stdout 在上游可安全脱敏前不持久化；如需恢复采集，必须先验证 `--auth-secret` 不会进入日志。

## 8. 完成判定

AT-STF-007～AT-STF-010、AT-WEB-016、DF-031 定义的真实功能、清理、异常恢复和安全条件均通过。DF-031 可以标记为 `completed` 并单独提交 Git。
