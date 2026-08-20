# macOS iOS Host 部署与验证

本目录部署专用 macOS Host：Appium Device Farm Hub/Node 与 Session Fence 只负责 inventory、明确 UDID 的自动化 Session 和异常清理；Host Agent 负责 CoreSimulator 动态生命周期；Baguette 0.1.92 只提供目标 Simulator 的原生 Web 画面和 Host HID。它不安装 IPA、不实现 DaFit 业务步骤、不启用跨 Host Hub、不开放 Appium Dashboard、不控制 macOS 桌面，也不管理真机签名。

## 1. 固定版本

| 组件 | 版本 |
|---|---|
| Node.js | 22.23.2 LTS |
| Appium | 3.6.0 |
| Appium Device Farm | 12.0.1 |
| XCUITest Driver | 12.4.0 |
| WebDriverAgent | 16.2.0 |
| go-ios | 1.3.2 |
| Baguette | 0.1.92 |

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

Baguette 使用独立目录 `$IOS_HOST_ROOT/baguette-runtime`。部署包必须固定为 0.1.92，当前验收的 arm64 bottle SHA-256 为：

```text
902c5cba54e44408d40dbb08ff7faf6a34edef24d50700a5679bcfbf5377fe84
```

安装后执行 `$IOS_HOST_ROOT/baguette-runtime/bin/baguette --version`，版本不符时禁止启动。Baguette 只监听 `127.0.0.1:8421`，Server 通过 SSH 安全通道访问，不得直接发布到局域网。

安装后必须核对插件和 Driver 的 JSON 输出，并读取 XCUITest 安装目录中的 `appium-webdriveragent/package.json`，不能根据网站首页或浮动 npm tag 推断版本。

## 3. Simulator allowlist 与动态目录

DF-041 固定库存只接受管理员预先选择、已经 `Booted` 的 Simulator。DF-044 动态库存还接受由 Server 生成的保留名称前缀，并要求 Runtime ID 与 iPhone Device Type ID 同时处于部署 allowlist 和 Host 实际可用目录中。固定 UDID、完整动态 UDID 都不得提交仓库或验收文档。其他未知 Simulator 会被 Adapter 标记为 `allowlisted=false` 和非就绪，Server 不会自动建 Device 或把它变成 ready。

Appium Device Farm 12.0.1 的 `bootedSimulators` 在“没有任何 Booted Simulator”时会回退返回全部 Simulator。设备农场不能依赖该选项作为安全边界；本项目仍按 allowlist、`state=Booted`、busy 和 Node readiness 共同判定可用性。

## 4. 启动本机 Appium Hub 与动态发现 Node

先启动本机 Hub。Hub 只负责 Session Fence 的唯一上游入口，不扫描 Simulator；把设备类型固定为当前环境不存在的 `real` 可避免 Hub 与动态 Node 各登记一份同一 Simulator：

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
  --plugin-device-farm-ios-device-type=real \
  --plugin-device-farm-remove-devices-from-database-before-running-the-plugin \
  --plugin-device-farm-bind-host-or-ip=127.0.0.1
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
- `4723` Hub 只路由，不能再启用 Simulator 扫描；`4724` Node 是唯一 Simulator inventory 来源，禁止两边重复发现；
- 每台 macOS Host 的 Node 只注册到本机 Hub，不配置也不允许跨 Host Hub；
- PostgreSQL Scheduler 先确定 Host、Device 和 UDID，Device Farm 插件不得跨主机或跨设备自由分配；
- Session Fence 只连接 `127.0.0.1:4723` 的本机 Hub，不连接 `4724` 动态发现 Node；
- 不启用 Dashboard；
- 不启用 Appium Device Farm 业务 Team/Allocation；
- 插件数据库只作本机技术状态，不同步到 PostgreSQL 业务表；
- 插件状态接口可能返回 `version=unknown`，固定版本以 Appium CLI 的 installed JSON 为准。

### 4.1 使用 launchd 常驻运行

生产式宿主机必须用当前 macOS 服务账号的 LaunchAgent 管理 Hub、动态发现 Node、Host Agent 和 Baguette，不能依赖 SSH 终端中的前台进程。仓库提供的四个 `*.plist.example` 与 `run-service.sh` 是同一套版本化入口：

```bash
install -m 755 ./run-service.sh "$IOS_HOST_ROOT/run-service.sh"
mkdir -p "$IOS_HOST_ROOT/logs" "$HOME/Library/LaunchAgents"

for service in appium-hub appium-node host-agent baguette; do
  sed "s|__IOS_HOST_ROOT__|$IOS_HOST_ROOT|g" \
    "./com.alcor.device-farm.${service}.plist.example" \
    > "$HOME/Library/LaunchAgents/com.alcor.device-farm.${service}.plist"
  plutil -lint "$HOME/Library/LaunchAgents/com.alcor.device-farm.${service}.plist"
done

uid="$(id -u)"
launchctl bootstrap "gui/$uid" "$HOME/Library/LaunchAgents/com.alcor.device-farm.appium-hub.plist"
launchctl bootstrap "gui/$uid" "$HOME/Library/LaunchAgents/com.alcor.device-farm.appium-node.plist"
launchctl bootstrap "gui/$uid" "$HOME/Library/LaunchAgents/com.alcor.device-farm.host-agent.plist"
launchctl bootstrap "gui/$uid" "$HOME/Library/LaunchAgents/com.alcor.device-farm.baguette.plist"
```

首次安装后使用 `launchctl print gui/$(id -u)/<label>` 检查 `state=running`，并确认 4723、4724 和 Fence 端口只监听 `127.0.0.1`。三个服务都设置 `KeepAlive`；进程异常退出后由 launchd 自动拉起。Node 每次启动先清理自身旧设备表，并每 5 秒检查 stale 设备，避免已从 CoreSimulator 删除的动态 UDID 长期残留；Hub 启动时同样清理旧路由库存。Node 单独设置 4 GiB V8 堆上限；`run-service.sh` 会每 10 秒分别探测 Hub 与 Node 的 `/status` 和 iOS inventory，任一服务连续三次在 5 秒内无响应就只终止对应进程并交给 launchd 拉起。这样既覆盖 OOM/崩溃，也覆盖“进程仍在但 inventory 接口挂死”；Agent 同时把 Hub/Node health 和 inventory 纳入 Host readiness，恢复前 Host 不接收新预约。Host 因组件探测失败自动进入维护后，Reconciler 默认保留 90 秒恢复宽限；宽限内设备停止调度但不累计隔离次数，超时后仍按既有阈值隔离。

不要用 `sudo` 安装为系统级 LaunchDaemon。CoreSimulator 和 Xcode 会话属于当前登录服务账号，必须由相同账号的 `gui/<uid>` LaunchAgent 启动。

Hub plist 的 `ProcessType` 固定为 `Interactive`，因为它会为 Simulator 启动 Xcodebuild/XCTest/WebDriverAgent；这只是 macOS 的进程调度类别，不会打开 Hub 或 Mac 桌面窗口。动态发现 Node 和 Host Agent 不启动 XCTest，继续使用 `Background`。

### 4.2 停止、升级和回滚

停止顺序固定为 Host Agent、动态发现 Node、Hub；启动顺序相反。操作前必须先 drain Host 或禁用 iOS Pool，并等待所有人工/自动化 Session 释放：

```bash
uid="$(id -u)"
launchctl bootout "gui/$uid/com.alcor.device-farm.host-agent"
launchctl bootout "gui/$uid/com.alcor.device-farm.appium-node"
launchctl bootout "gui/$uid/com.alcor.device-farm.appium-hub"
```

升级时把新二进制、独立 Node/runtime 目录和 plist 先放入新的版本目录，完成版本与 `plutil -lint` 校验后再切换三个 plist 中的绝对路径；不要原地覆盖正在执行的二进制。重新 `bootstrap` 后验证 Host readiness、inventory 和一条明确 UDID 的最小 Session，再显式解除排空。失败时按相同顺序停止新版本，把 plist 路径切回上一版本目录并重新启动；回滚不修改 Reservation 数据，不删除 Simulator，也不停止 Android Host、STF 或 Android Appium。

只有更新环境变量且 plist 路径未变化时，才可用下面的受控重载方式；`kickstart -k` 会终止当前进程，因此同样要求 Host 已排空且没有活动 Session：

```bash
launchctl kickstart -k "gui/$(id -u)/com.alcor.device-farm.appium-hub"
launchctl kickstart -k "gui/$(id -u)/com.alcor.device-farm.appium-node"
launchctl kickstart -k "gui/$(id -u)/com.alcor.device-farm.host-agent"
```

### 4.3 日志与敏感信息

Hub、Node 和 Agent 日志分别写入专用目录的 `logs/appium-hub.log`、`logs/appium-node.log` 和 `logs/host-agent.log`。排障证据只保存时间、服务状态、错误分类和脱敏 ID 前缀；不得收集完整 UDID、Session ID、Session Grant、Service/Agent Token、Cookie、Apple Account、证书/Profile 内容、内部 WDA/MJPEG 地址或 `host-agent.env` 内容。

`host-agent.env` 必须由服务账号持有且权限为 `600`，`run-service.sh` 会在启动 Agent 前强制校验。环境文件、实际 plist、日志和运行目录都不得提交 Git。Node OOM 或其他崩溃恢复的验收应记录旧/新 PID、恢复用时和最终 inventory 数量，不复制包含能力参数或内部地址的整段 Appium 日志。

## 5. Host Agent 配置

以 [host-agent.env.example](host-agent.env.example) 为模板创建权限受控的本机环境文件，权限必须为 `600`。Agent 会上报：macOS/架构、Xcode build、可用 iOS Runtime/iPhone Device Type、固定 Node/Appium/插件/XCUITest/WDA/go-ios 版本、Appium doctor、Hub health 和脱敏 readiness。

Hub/Node 分离时，`DEVICE_FARM_IOS_APPIUM_ENDPOINT` 指向本机 Hub 的 `127.0.0.1:4723`，`DEVICE_FARM_IOS_APPIUM_NODE_ENDPOINT` 指向本机 Node 的 `127.0.0.1:4724`。动态 Simulator 删除成功后，Agent 会通过 Appium Device Farm 官方本机注册接口同时注销 Hub 与 Node 清单中的目标受管 UDID；只允许精确匹配 `DEVICE_FARM_IOS_MANAGED_NAME_PREFIX` 的记录，不能删除固定库存或真机。若注销恰好遇到 Node 看门狗恢复窗口，Agent 会在 Host Command 上下文内幂等重试，单个 Endpoint 最长等待 90 秒；永久失败仍按正式错误隔离，不能伪装成删除成功。

readiness 失败、inventory 读取失败或版本漂移时，心跳把 Host 置为 `maintenance`，Scheduler 不会给该 Host 新预约；恢复后下一次健康心跳回到 `online`。`appium driver doctor xcuitest` 是 Agent 进程启动门禁：本次进程首次通过后不在 WDA 编译或 Session 运行期间重复执行，避免 doctor 的瞬时占用结果误下线 Host；Agent 每次重启仍会重新校验，Appium/Device Farm 实时 health 和 inventory 也始终继续探测。Android Agent 没有 `host_readiness` 时继续保持原行为。

## 6. Session Fence 网络边界

Host Agent 启动 DF-042 Session Fence，并通过心跳上报 `session_fence_endpoint`。本机 Hub 仍只能监听 `127.0.0.1:4723`，动态发现 Node 只能监听 `127.0.0.1:4724`；客户端不得直接连接任一 Appium 进程或 Device Farm Dashboard。Fence 只连接 Hub，只接受一次性 `Session-Grant`，只转发 `POST /session` 和 Grant 已绑定的精确 `/session/{id}/...` 路径。

- `DEVICE_FARM_IOS_SESSION_FENCE_LISTEN` 是本机监听地址，默认 `127.0.0.1:4810`；
- `DEVICE_FARM_IOS_SESSION_FENCE_ADVERTISE_URL` 是 Server 返回给可信 Worker 的入口；
- advertise URL 为非 loopback 地址时必须使用 HTTPS，可由内网反向代理完成 TLS，并把固定前缀转发到 Fence；
- 本机验收可通过 SSH 端口转发继续使用 loopback URL，不得为了测试把 Appium 改成对外监听；
- Service Token、Agent Token 和 Session Grant 均不得写入 URL、日志、证据或插件数据库。

Reservation release 和 Reaper 会先通过 Fence 删除上游 Appium Session；清理失败时 Reservation 保持 active，Device 进入 quarantine，禁止静默释放后把残留 Session 留在 macOS Host。

Fence 重启不会结束仍然有效的自动化 Appium/WDA Session。Fence 不提供任何人工画面、截图或动作路由，也不为 Session 注入 MJPEG 端口。

## 7. iOS 人工远控

不需要开启 macOS Remote Management、Screen Sharing 或 VNC，也不安装 noVNC/websockify。人工远控先取得指定 Simulator 的 active Reservation，Server 再返回独立 Baguette Gateway 的短时入口。浏览器使用 Baguette 原生页面、WebSocket 画面和 Host HID；不会创建 Appium Session，也不会占用 WDA。

入口票据只用于首次建立 HttpOnly 会话 Cookie。后续 HTTP/WebSocket 请求都由 Gateway 重新校验 active Reservation 和唯一 UDID；Console 心跳持续滑动续约，因此没有固定一小时上限。释放或超时后，Gateway 会拒绝新请求并主动中断正在使用的长连接。

本机检查：

```bash
curl --fail http://127.0.0.1:4723/status
lsof -nP -iTCP:4810 -sTCP:LISTEN
curl --fail http://127.0.0.1:8421/simulators.json
```

本机部署的 4723、4724、4810 和 8421 必须只监听回环。4810 只服务可信自动化 Worker；8421 只通过 Server 侧 SSH 隧道进入 Baguette Gateway。人工预约必须通过 Console 正常结束或由 Reservation Reaper 清理。

## 8. 验收入口

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

## 9. 回滚

先 drain Host 或禁用 iOS Pool，等待人工预约和自动化 Appium Session 全部释放，再停止 Baguette、Host Agent、本机动态发现 Node 和 Hub。关闭 `DEVICE_FARM_IOS_REMOTE_CONTROL_ENABLED` 并删除 Gateway Secret 只会停用 iOS 人工远控；Session Fence 仍供自动化使用。不要修改 PostgreSQL Reservation 伪造释放。Android Host、STF 和 Android Appium Endpoint 不受影响。
