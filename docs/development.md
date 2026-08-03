# 本地开发

## Go 版本

项目 `go.mod` 保持 Go 1.24 语言兼容级别，与本地旧版 Alcor 基线一致。当前验证工具链为官方 Go 1.26.5；项目代码不得依赖高于 `go.mod` 声明版本的语言特性。

项目不要求修改系统 PATH。Windows 可以设置当前终端变量：

```powershell
$env:DEVICE_FARM_GO = "<go.exe 的绝对路径>"
```

该变量只指向工具，不写入仓库配置或业务日志。

## 开发命令

Windows：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1 -Task check
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1 -Task build
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1 -Task test
```

Linux/macOS 或已安装 Make：

```text
make check
```

`check` 依次执行格式检查、`go vet`、单元测试和两个二进制构建。

## 当前二进制

- `bin/device-farm-server.exe`：设备农场控制面；默认加载配置并启动 HTTP 服务；
- `bin/device-host-agent.exe`：宿主机 Agent；运行时从 DF-013 开始实现。

查看构建信息：

```powershell
bin/device-farm-server.exe --version
bin/device-host-agent.exe --version
```

检查配置但不启动服务：

```powershell
bin/device-farm-server.exe --config config/config.example.yaml --check-config
```

启动服务：

```powershell
bin/device-farm-server.exe --config config/config.example.yaml
```

配置可以由 `DEVICE_FARM_` 前缀的环境变量覆盖。当前支持：

- `DEVICE_FARM_SERVER_ADDRESS`；
- `DEVICE_FARM_SERVER_READ_TIMEOUT`；
- `DEVICE_FARM_SERVER_WRITE_TIMEOUT`；
- `DEVICE_FARM_SERVER_IDLE_TIMEOUT`；
- `DEVICE_FARM_SERVER_SHUTDOWN_TIMEOUT`；
- `DEVICE_FARM_LOG_LEVEL`；
- `DEVICE_FARM_LOG_FORMAT`；
- `DEVICE_FARM_SECURITY_SERVICE_TOKEN`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`。

真实 Token 只能通过部署 Secret 或环境变量注入，不写入已提交 YAML。

构建产物位于 `bin/`，已被 `.gitignore` 排除。
