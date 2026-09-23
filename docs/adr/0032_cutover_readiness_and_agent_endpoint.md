# ADR-0032：控制面瞬时切换准备与 Agent 端点

## 状态

已批准，作为 DF-065 的准备基线；只定义切换前准备，不代表已经切换生产流量。

## 决策

1. 220 保留三个独立入口：`18180` 为 HTTPS 控制台/NPS 入口，`18181` 为 iOS Gateway，`18182` 为 Host Agent 内网 API。
2. `18182` 只绑定 220 私网地址，不加入 NPS、不展示给浏览器、不暴露 Docker Socket；171 和未来 Host Agent 在切换窗口改为主动连接该地址。
3. 切换前由 171 生成生产设备域 PostgreSQL custom dump，导入 220 已初始化的独立数据库；不让两个 Server 同时写同一数据库。
4. Host Agent 认证 Token、STF API Token、STF Web Secret、iOS Baguette SSH 隧道和 NPS 转发属于切换窗口动作；预部署阶段只保存配置模板，不复制生产 Secret。
5. 所有切换前检查必须只读；失败时先恢复 Agent 的旧 `DEVICE_FARM_AGENT_SERVER_URL`，再保留 171 正式 Server 和数据，不执行全局 Docker 清理。

## 原因

把浏览器入口、iOS 远控和 Agent 通道拆开，切换时只需要导入数据库、切换 Agent URL、验证心跳，再改变 NPS 后端；不需要现场构建镜像、初始化数据库或修改模拟器。

## 不在本 ADR 范围

- 不在本阶段修改 171 Agent、STF、模拟器或 NPS；
- 不在本阶段复制 STF/RethinkDB 数据；
- 不承诺没有生产备份、Token、Baguette 隧道时可以完成正式切换。
