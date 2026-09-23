# DF-051 普通成员远控入口与独立网关可达性验收证据

验收日期：2026-08-20

## 结论

通过。问题根因是 Console 前端只为 admin 显示远控入口，而 Alcor 设备 API 已允许普通设备农场成员执行 Reservation 级远控，导致 operator 后端有权限但页面没有入口。Device Farm 独立控制台的 Server 校验也只接受 admin，形成第二处角色不一致。

本任务只修改和部署 Device Farm，未修改、提交、部署或重启正式 Alcor。

## 修正内容

- operator/admin 均可看到并使用 Android STF、iOS Baguette 的“远程连接”和“挂断”；
- viewer 继续只读，不能启动远控；
- 重建、删除、隔离等管理员危险操作没有下放给 operator；
- Server 独立 Console 与 Alcor 嵌入 Console 使用一致的 operator/admin 远控权限；
- iOS Gateway Public URL 从裸内网 IP 调整为与 Android STF 一致的 30.171 可访问主机名 `10-0-30-171.nip.io:18081`；
- 没有恢复已删除的 Alcor iOS 旧代理，也没有新增 Alcor 路由。

## 真实环境证据

- 30.171 新 Device Farm 镜像运行状态 `healthy`；Mac Baguette SSH 安全通道已绑定新容器且 inventory 可访问。
- 使用 Service Principal 携带普通 Alcor 成员 Actor 模拟真实嵌入调用：
  - iOS Reservation 成功进入 `connected`，transport 为 `baguette`；
  - 返回入口使用 `http://10-0-30-171.nip.io:18081/entry/...`；
  - Baguette 原生页面返回 HTTP 200，页面大小 6684 字节；
  - 挂断和 Reservation 释放成功；
  - Android Reservation 成功进入 `connected`，transport 为 `stf`；
  - Android 挂断和 Reservation 释放成功。
- Gateway 未持有预约时继续返回 HTTP 401；其他 UDID、设备墙和生命周期命令的既有隔离不变。

## 自动化门禁

- `DevicesPage.test.tsx`：operator 显示远控但不显示重建/删除，viewer 不显示任何操作；iOS operator 可打开 Baguette Gateway。
- `internal/api/console_test.go`：operator/admin 通过远控角色校验，viewer 返回 HTTP 403。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- Console `pnpm test`：通过。
- Console `pnpm build`：通过。
- `git diff --check`：通过。
