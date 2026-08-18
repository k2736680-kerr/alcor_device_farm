# DF-041/DF-042 macOS iOS Host 部署与验证

本目录部署专用 macOS Host：DF-041 提供固定工具链、Appium Device Farm 本机 Hub/Node、只读 inventory/health Adapter；DF-042 在同一个 Host Agent 进程内增加 Reservation 绑定的 Session Fence；DF-044 增加 CoreSimulator 动态创建、重建和删除。它不安装 IPA、不实现 DaFit 业务步骤、不启用跨 Host Hub、不开放 Dashboard，也不管理真机签名。

## 1. 固定版本

| 组件 | 版本 |
|---|---|
| Node.js | 22.23.2 LTS |
| Appium | 3.6.0 |
| Appium Device Farm | 12.0.1 |
| XCUITest Driver | 12.4.0 |
| WebDriverAgent | 16.2.0 |
| go-ios | 1.3.2 |

Node 必须安装到项目独立目录，不能替换 macOS 已有的系统或全局 Node。Apple Silicon 的官方 `node-v22.23.2-darwin-arm64.tar.gz` SHA-256 为：

```text
61130f394c1630d211dd50aecc4353d379480f36d3ac913cd85dbba1aed585c6
```

## 2. 隔离安装

以下示例中的 `IOS_HOST_ROOT` 必须替换为当前 macOS 服务账号拥有的专用绝对目录：

```bash
export IOS_HOST_ROOT=/absolute/path/alcor-device-farm/df041
mkdir -p "$IOS_HOST_ROOT/downloads" "$IOS_HOST_ROOT/runtime" "$IOS_HOST_ROOT/appium-home" "$IOS_HOST_ROOT/logs"

curl -fL --retry 3 -o "$IOS_HOST_ROOT/downloads/node-v22.23.2-darwin-arm64.tar.gz" \
  https://nodejs.org/dist/v22.23.2/node-v22.23.2-darwin-arm64.tar.gz
printf '%s  %s\n' \
  '61130f394c1630d211dd50aecc4353d379480f36d3ac913cd85dbba1aed585c6' \
  "$IOS_HOST_ROOT/downloads/node-v22.23.2-darwin-arm64.tar.gz" | shasum -a 256 -c -

tar -xzf "$IOS_HOST_ROOT/downloads/node-v22.23.2-darwin-arm64.tar.gz" -C "$IOS_HOST_ROOT"
mv "$IOS_HOST_ROOT/node-v22.23.2-darwin-arm64" "$IOS_HOST_ROOT/node"
export PATH="$IOS_HOST_ROOT/node/bin:$PATH"
export APPIUM_HOME="$IOS_HOST_ROOT/appium-home"

npm install --prefix "$IOS_HOST_ROOT/runtime" --no-audit --no-fund appium@3.6.0 go-ios@1.3.2
"$IOS_HOST_ROOT/runtime/node_modules/.bin/appium" plugin install --source=npm appium-device-farm@12.0.1
"$IOS_HOST_ROOT/runtime/node_modules/.bin/appium" driver install --source=npm appium-xcuitest-driver@12.4.0
```

安装后必须核对插件和 Driver 的 JSON 输出，并读取 XCUITest 安装目录中的 `appium-webdriveragent/package.json`，不能根据网站首页或浮动 npm tag 推断版本。

## 3. Simulator allowlist 与动态目录

DF-041 固定库存只接受管理员预先选择、已经 `Booted` 的 Simulator。DF-044 动态库存还接受由 Server 生成的保留名称前缀，并要求 Runtime ID 与 iPhone Device Type ID 同时处于部署 allowlist 和 Host 实际可用目录中。固定 UDID、完整动态 UDID 都不得提交仓库或验收文档。其他未知 Simulator 会被 Adapter 标记为 `allowlisted=false` 和非就绪，Server 不会自动建 Device 或把它变成 ready。

Appium Device Farm 12.0.1 的 `bootedSimulators` 在“没有任何 Booted Simulator”时会回退返回全部 Simulator。设备农场不能依赖该选项作为安全边界；本项目仍按 allowlist、`state=Booted`、busy 和 Node readiness 共同判定可用性。

## 4. 启动本机 Appium Hub 与动态发现 Node

先启动本机 Hub。Hub 负责 Session Fence 的唯一上游入口：

```bash
xcrun simctl boot '<ALLOWLISTED_UDID>'
xcrun simctl bootstatus '<ALLOWLISTED_UDID>' -b

export PATH="$IOS_HOST_ROOT/node/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export APPIUM_HOME="$IOS_HOST_ROOT/appium-home"
"$IOS_HOST_ROOT/runtime/node_modules/.bin/appium" server \
  --address=127.0.0.1 \
  --port=4723 \
  --use-plugins=device-farm \
  --plugin-device-farm-platform=ios \
  --plugin-device-farm-ios-device-type=simulated \
  --plugin-device-farm-booted-simulators
```

再启动动态发现 Node。Node 每 5 秒重新发现本机已启动 Simulator，并把 inventory 注册到本机 Hub，因此后台新建 Simulator 后不需要重启 Hub：

```bash
"$IOS_HOST_ROOT/runtime/node_modules/.bin/appium" server \
  --address=127.0.0.1 \
  --port=4724 \
  --use-plugins=device-farm \
  --plugin-device-farm-platform=ios \
  --plugin-device-farm-ios-device-type=simulated \
  --plugin-device-farm-booted-simulators \
  --plugin-device-farm-hub=http://127.0.0.1:4723 \
  --plugin-device-farm-send-node-devices-to-hub-interval-ms=5000 \
  --plugin-device-farm-bind-host-or-ip=127.0.0.1
```

首期必须满足：

- Hub 和动态发现 Node 都只监听 loopback；Adapter 会拒绝非 loopback Hub 地址；
- 每台 macOS Host 的 Node 只注册到本机 Hub，不配置也不允许跨 Host Hub；
- PostgreSQL Scheduler 先确定 Host、Device 和 UDID，Device Farm 插件不得跨主机或跨设备自由分配；
- Session Fence 只连接 `127.0.0.1:4723` 的本机 Hub，不连接 `4724` 动态发现 Node；
- 不启用 Dashboard；
- 不启用 Appium Device Farm 业务 Team/Allocation；
- 插件数据库只作本机技术状态，不同步到 PostgreSQL 业务表；
- 插件状态接口可能返回 `version=unknown`，固定版本以 Appium CLI 的 installed JSON 为准。

## 5. Host Agent 配置

以 [host-agent.env.example](host-agent.env.example) 为模板创建权限受控的本机环境文件，权限必须为 `600`。Agent 会上报：macOS/架构、Xcode build、可用 iOS Runtime/iPhone Device Type、固定 Node/Appium/插件/XCUITest/WDA/go-ios 版本、Appium doctor、Hub health 和脱敏 readiness。

readiness 失败、inventory 读取失败或版本漂移时，心跳把 Host 置为 `maintenance`，Scheduler 不会给该 Host 新预约；恢复后下一次健康心跳回到 `online`。Android Agent 没有 `host_readiness` 时继续保持原行为。

## 6. Session Fence 网络边界

Host Agent 启动 DF-042 Session Fence，并通过心跳上报 `session_fence_endpoint`。本机 Hub 仍只能监听 `127.0.0.1:4723`，动态发现 Node 只能监听 `127.0.0.1:4724`；客户端不得直接连接任一 Appium 进程或 Device Farm Dashboard。Fence 只连接 Hub，只接受一次性 `Session-Grant`，只转发 `POST /session` 和 Grant 已绑定的精确 `/session/{id}/...` 路径。

- `DEVICE_FARM_IOS_SESSION_FENCE_LISTEN` 是本机监听地址，默认 `127.0.0.1:4810`；
- `DEVICE_FARM_IOS_SESSION_FENCE_ADVERTISE_URL` 是 Server 返回给可信 Worker 的入口；
- advertise URL 为非 loopback 地址时必须使用 HTTPS，可由内网反向代理完成 TLS，并把固定前缀转发到 Fence；
- 本机验收可通过 SSH 端口转发继续使用 loopback URL，不得为了测试把 Appium 改成对外监听；
- Service Token、Agent Token 和 Session Grant 均不得写入 URL、日志、证据或插件数据库。

Reservation release 和 Reaper 会先通过 Fence 删除上游 Appium Session；清理失败时 Reservation 保持 active，Device 进入 quarantine，禁止静默释放后把残留 Session 留在 macOS Host。

## 7. 验收入口

在 macOS 上设置下面的临时环境变量后运行版本化集成测试；测试只输出数量和结论，不输出完整 UDID：

```bash
export DEVICE_FARM_IOS_INTEGRATION_ENDPOINT=http://127.0.0.1:4723
export DEVICE_FARM_IOS_INTEGRATION_UDID='<ALLOWLISTED_UDID>'
export DEVICE_FARM_NODE_BINARY="$IOS_HOST_ROOT/node/bin/node"
export DEVICE_FARM_APPIUM_BINARY="$IOS_HOST_ROOT/runtime/node_modules/.bin/appium"
export DEVICE_FARM_GO_IOS_BINARY="$IOS_HOST_ROOT/runtime/node_modules/go-ios/dist/go-ios-darwin-arm64_darwin_arm64/ios"
export DEVICE_FARM_IOS_WDA_PACKAGE_JSON="$IOS_HOST_ROOT/appium-home/node_modules/appium-xcuitest-driver/node_modules/appium-webdriveragent/package.json"

go test -count=1 -v ./internal/adapters/appiumdevicefarm ./internal/ioshost ./internal/iossessionfence
```

故障验收至少包含：关闭 Simulator 后 allowlist 设备不再 ready；停止本机 Hub 或动态发现 Node 后 readiness/inventory 失败；重新启动 Hub、Node 和 Simulator 后在目标时间内恢复；创建后 inventory 故障会直接清理刚创建的 CoreSimulator；未知设备不自动创建；证据中隐藏 Host 地址、完整 UDID、硬件序列号和所有 Secret。

## 8. 回滚

先 drain Host 或禁用 iOS Pool，再停止 Host Agent、本机动态发现 Node 和 Hub。隔离目录可整体保留以便复盘，也可在确认没有活动 Reservation/Session 后移走；不要修改 PostgreSQL Reservation 伪造释放，不要删除其他全局 Node/npm/Xcode 工具。Android Host、STF 和 Android Appium Endpoint 不受该回滚影响。
