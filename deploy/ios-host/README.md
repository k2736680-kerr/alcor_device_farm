# DF-041 macOS iOS Host 部署与验证

本目录只部署 DF-041 的专用 macOS Host：固定工具链、Appium Device Farm 本机 Node、只读 inventory/health Adapter 和现有 Device Host Agent。它不创建 iOS Session、不安装 IPA、不启用跨 Host Hub、不开放 Dashboard，也不管理真机签名。

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

## 3. Simulator allowlist

DF-041 只接受管理员预先选择、已经 `Booted` 的 Simulator。UDID 只写入 macOS 上权限受控的 Host Agent 环境文件，不提交仓库或验收文档。未知 Simulator 会被 Adapter 标记为 `allowlisted=false` 和非就绪，Server 不会自动建 Device 或把它变成 ready。

Appium Device Farm 12.0.1 的 `bootedSimulators` 在“没有任何 Booted Simulator”时会回退返回全部 Simulator。设备农场不能依赖该选项作为安全边界；本项目仍按 allowlist、`state=Booted`、busy 和 Node readiness 共同判定可用性。

## 4. 启动本机 Appium Node

先启动 allowlist 内的 Simulator，再启动 Node：

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

首期必须满足：

- Endpoint 只监听 loopback；Adapter 会拒绝非 loopback 地址；
- 不配置 `hub`，每台 macOS Host 是独立 Node；
- 不启用 Dashboard 和跨主机自由分配；
- 不启用 Appium Device Farm 业务 Team/Allocation；
- 插件数据库只作本机技术状态，不同步到 PostgreSQL 业务表；
- 插件状态接口可能返回 `version=unknown`，固定版本以 Appium CLI 的 installed JSON 为准。

## 5. Host Agent 配置

以 [host-agent.env.example](host-agent.env.example) 为模板创建权限受控的本机环境文件。Agent 会上报：macOS/架构、Xcode build、可用 iOS Runtime、固定 Node/Appium/插件/XCUITest/WDA/go-ios 版本、Appium doctor、Node health 和脱敏 readiness。

readiness 失败、inventory 读取失败或版本漂移时，心跳把 Host 置为 `maintenance`，Scheduler 不会给该 Host 新预约；恢复后下一次健康心跳回到 `online`。Android Agent 没有 `host_readiness` 时继续保持原行为。

## 6. 验收入口

在 macOS 上设置下面的临时环境变量后运行版本化集成测试；测试只输出数量和结论，不输出完整 UDID：

```bash
export DEVICE_FARM_IOS_INTEGRATION_ENDPOINT=http://127.0.0.1:4723
export DEVICE_FARM_IOS_INTEGRATION_UDID='<ALLOWLISTED_UDID>'
export DEVICE_FARM_NODE_BINARY="$IOS_HOST_ROOT/node/bin/node"
export DEVICE_FARM_APPIUM_BINARY="$IOS_HOST_ROOT/runtime/node_modules/.bin/appium"
export DEVICE_FARM_GO_IOS_BINARY="$IOS_HOST_ROOT/runtime/node_modules/go-ios/dist/go-ios-darwin-arm64_darwin_arm64/ios"
export DEVICE_FARM_IOS_WDA_PACKAGE_JSON="$IOS_HOST_ROOT/appium-home/node_modules/appium-xcuitest-driver/node_modules/appium-webdriveragent/package.json"

go test -count=1 -v ./internal/adapters/appiumdevicefarm ./internal/ioshost
```

故障验收至少包含：关闭 Simulator 后 allowlist 设备不再 ready；停止本项目 Node 后 readiness 失败；重新启动 Node 和 Simulator 后在目标时间内恢复；未知设备不自动创建；证据中隐藏 Host 地址、完整 UDID、硬件序列号和所有 Secret。

## 7. 回滚

先 drain Host 或禁用 iOS Pool，再停止 Host Agent 和本机 Appium Node。隔离目录可整体保留以便复盘，也可在确认没有活动 Reservation/Session 后移走；不要修改 PostgreSQL Reservation 伪造释放，不要删除其他全局 Node/npm/Xcode 工具。Android Host、STF 和 Android Appium Endpoint 不受该回滚影响。
