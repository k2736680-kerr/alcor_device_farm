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

- `bin/device-farm-server.exe`：设备农场控制面；HTTP 运行入口从 DF-002 开始实现；
- `bin/device-host-agent.exe`：宿主机 Agent；运行时从 DF-013 开始实现。

查看构建信息：

```powershell
bin/device-farm-server.exe --version
bin/device-host-agent.exe --version
```

构建产物位于 `bin/`，已被 `.gitignore` 排除。
