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
- `DEVICE_FARM_DATABASE_URL`；
- `DEVICE_FARM_LOG_LEVEL`；
- `DEVICE_FARM_LOG_FORMAT`；
- `DEVICE_FARM_SECURITY_SERVICE_TOKEN`；
- `DEVICE_FARM_SECURITY_SERVICE_PREVIOUS_TOKEN`；
- `DEVICE_FARM_SECURITY_AGENT_TOKEN`；
- `DEVICE_FARM_SECURITY_AGENT_PREVIOUS_TOKEN`。

真实 Token 只能通过部署 Secret 或环境变量注入，不写入已提交 YAML。`*_PREVIOUS_TOKEN` 只用于轮换宽限期，不能脱离对应 current Token 单独配置；四个非空 Token 必须互不相同。具体轮换、审计和脱敏规则见 [安全与审计](security_and_audit.md)。

管理 API 需要 `DEVICE_FARM_DATABASE_URL`。URL 为空时 Server 只提供健康检查，受保护的管理路径返回 503；不会退化为不持久化的内存管理模式。

`/healthz` 只表示进程存活；`/readyz` 会实际检查 PostgreSQL，未配置或不可连接时返回 503；`/metrics` 提供 Prometheus 文本格式。指标、Dashboard 和告警见 [可观测性和告警](observability.md)。

构建产物位于 `bin/`，已被 `.gitignore` 排除。

## Console 本地浏览器验收

DF-027/DF-028 的 E0 浏览器回归使用仓库内固定夹具，不依赖开发者手工保留的 `tmp/seed-e2e.sql` 或后台 Mock STF。先准备已经执行 migrations 的本地 PostgreSQL 测试库，再运行：

```powershell
$env:DEVICE_FARM_GO = "<go.exe 的绝对路径>"
$env:DEVICE_FARM_POSTGRES_BIN = "<PostgreSQL bin 绝对路径>"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/run-console-local-e2e.ps1
```

脚本会执行 pnpm 冻结安装与生产构建、编译临时 Server、重置 E0 测试夹具、启动 Mock STF、运行完整 Playwright 套件，并在结束时检查开放预约、有效 Console 会话和 reserved/busy 设备均为 0。默认只接受回环 PostgreSQL，且数据库名必须以 `device_farm_` 开头；不允许把该入口指向共享或生产数据库。

真实 Linux/STF/Appium 用例仍需显式设置 `DEVICE_FARM_E3=1` 并使用外部 Server，E0 Mock 结果不能替代 E3 验收。

## PostgreSQL migration 验证

Windows 不需要安装系统服务。将 `DEVICE_FARM_POSTGRES_BIN` 指向 PostgreSQL 的 `bin` 目录，然后运行：

```powershell
$env:DEVICE_FARM_POSTGRES_BIN = "<PostgreSQL bin 绝对路径>"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1
```

脚本只在项目 `tmp/df004-postgres-data` 创建一次性实例，依次执行 `up → 约束检查 → down → up`，最后停止实例并清理临时数据。脚本会校验清理目标必须位于项目 `tmp` 目录，避免误删其他 PostgreSQL 数据。

需要同时运行 Repository 真实并发测试时，再设置 `DEVICE_FARM_GO` 并增加参数：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```
