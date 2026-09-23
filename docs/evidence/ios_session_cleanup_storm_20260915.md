# iOS 会话清理风暴与宿主机告警根因取证（2026-09-15）

## 结论速览

| 现象 | 量级（近 24h，09-14） | 根因 | 处置 |
|---|---|---|---|
| `ios_session_cleanup_failed`（critical） | 12352 条，iPhone17-1，09-14 03:00–07:06 UTC，恒定 3600 条/小时 | Reaper（`ReaperInterval=1s`）对同一失败 cleanup 每秒重试；cleanup 失败 → `quarantine` → 自愈流程把设备恢复 ready → 下秒再失败。quarantine 原幂等护栏（设备已 quarantined 则跳过事件）被恢复循环绕过 | `internal/iossession/reconcile.go` quarantine() 增加 (device_id,event_type) 5 分钟节流（仅限健康事件+审计；状态更新与连续失败计数不受影响）。部署 BUILD_DATE 2026-09-15T03:50:47Z 后零新增 |
| `host_unavailable`（warning） | 12 条孤立事件 | 实为 agent 上报「宿主机环境未就绪 `appium_node_not_ready`」，非宿主机真离线。Mac uptime 32 天无重启、pmset 无睡眠记录 | 观察项，暂不改文案 |
| agent 心跳间歇失败 | host-agent.log 偶发 WARN | **Mac（10.0.33.68）出站流量全部走 Clash Verge TUN**：`route get 10.0.80.220` → `utun1024 (198.18.0.1)`；错误 `read tcp 198.18.0.1:xxxxx->10.0.80.220:18182: connection reset by peer`。171 侧 70 次 `STF_INVENTORY_FAILED connection refused` 疑同源 | 建议在 Mac Clash 配置加内网直连规则（`IP-CIDR,10.0.0.0/8,DIRECT` 或 TUN 排除 10.0.0.0/8）；未动用户代理配置，待宿主负责人决定 |

## 证据链

1. **风暴形态**：`device_health_events` 按小时分布 1150/3600/3601/3601/400（03–07 时），每秒 1 条的匀速重试；payload 恒为 `{"host_id":"2c9b341c-…","cleanup_failed":true}`；07:06:39 戛然而止（对应预约到期后 `CloseForReservation` 的 SELECT 无行返回，循环自然终止）。
2. **Reaper 配置**：`internal/config/config.go:127` `ReaperInterval: time.Second`；`internal/reservation/service.go:708` cleanup 失败仅返回错误，预约保持未关闭 → 下一个 tick 重试。
3. **护栏绕过路径**：`quarantine()` 幂等检查依赖 `lifecycle == quarantined` 提前返回；但设备在两次重试之间被恢复流程置回 ready/healthy（代码注释自身已预警此风险）。
4. **host_unavailable 时刻吻合**：DB 12 条（UTC 04:12/06:17/06:54/12:33/18:16/23:58）与 Mac `host-agent.log`「宿主机环境未就绪」批次（本地 12:12/14:17/14:54/20:33/02:16/07:58，每批 8 行、5 秒间隔）逐批对应（UTC+8）。
5. **Clash TUN 实证**：Mac 路由表 `1/8, 2/7, 4/6, 8/5 → utun1024 (198.18.0.1)` 超网劫持（仅 10.0.32/22 走 en1 直连）；`route get 10.0.80.220` 返回 `gateway 198.18.0.1 / interface utun1024`；LaunchAgents 存在 `io.github.clash-verge-rev.clash-verge-rev.service.plist`。

## 修复与验证

- 提交 `2e613e9`（codex/device-farm-v2）：quarantine 节流 + scheduler STF claim 失败 ERROR 日志（上一项修复随本批提交）。
- `go build` / `go vet` / `go test ./internal/iossession/` 通过；镜像内 SQL 指纹验证 1 处命中；三容器重建后 healthy；嵌入代理端到端复验全绿（POST 202 → 1s active → GET connected → DELETE ended）。
- 控制面 server 旧容器日志随容器重建丢失（`docker logs --since 09-14` 为空），风暴起始触发点无法从服务端日志追溯，以 DB 事件分布为准。
