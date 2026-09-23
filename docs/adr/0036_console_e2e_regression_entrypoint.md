# ADR-0036：控制台本地 E2E 启动与发布前回归入口

状态：accepted

## 背景

控制台页面单元测试和生产构建已通过，但本地 Playwright E2E 还需要自动准备独立二进制、测试配置、隔离数据库和 Mock STF。Windows 的 Corepack/pnpm 版本检查不能成为页面回归的隐性阻断。

## 决策

1. 继续复用 `scripts/run-console-local-e2e.ps1` 作为本地 E2E 唯一入口，测试数据库只允许 loopback 且数据库名必须以 `device_farm_` 开头；脚本先应用全部有序 migration，再加载固定 fixture。
2. 脚本保存并临时设置 `COREPACK_ENABLE_PROJECT_SPEC=0`，让兼容的 pnpm 11.x 运行完成回归；执行结束恢复调用进程原有环境变量。
3. Playwright 默认 webServer 只负责启动已构建的本地 Server；缺失配置或 PostgreSQL 时应明确失败，不连接 220、171 或生产数据库。

## 边界

- 不改变控制台业务 API、数据库模型或生产部署。
- 不让 E2E fixture、Mock STF 或测试 Token 进入正式构建产物。
