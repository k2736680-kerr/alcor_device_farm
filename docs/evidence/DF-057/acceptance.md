# DF-057 全仓不可达代码与旧策略清理验收

## 结论

通过。全仓按生产入口、测试入口、部署入口和静态调用图联合审计；删除 Go 不可达实现、ADR-0029 已取代的 iOS 自动删除补建完成分支、Console 旧文案、重复前端依赖和两个失效的本地验收脚本。最终代码差异的删除量明显大于新增量，未新增 API、表、状态、兼容实现或第二套 Adapter。

## 已删除内容

- 删除未调用的 `Client.simulatorState`、领域构造器、`ID`/`ClearEvents` 方法和 `CommandRepository.Get`。
- 删除无效 `break`、测试中的无效赋值、未使用 import。
- 删除 Warm Pool 对 `ios_auto_replacement` 的生产查询、成功/失败处理和 membership disable 分支；保留管理员显式 `warm_pool_scale_down` 链路。
- 删除 Console 的旧自动替换事件、审计和“自动补建”提示，inventory 缺失统一提示保留原设备等待恢复。
- 删除 `@orval/core`、`@orval/query`、`@orval/zod` 三个重复直接依赖；它们由 `orval` 提供，生成结果零漂移。
- 删除 `scripts/check-alcor-integration-readiness.ps1` 和 `scripts/run-mvp-local-acceptance.ps1`；仓库无有效入口，且脚本依赖已经失效的 `E:\AutoTestTools\Projects\...` 路径。

## 经核对保留的入口

- `console/orval.config.ts` 由 `pnpm generate` 调用。
- `console/scripts/trim-generated.mjs` 由 Orval `afterAllFilesWrite` 调用。
- `console/e2e/fixtures/mock-stf.mjs` 由 Console E2E 入口调用。
- `scripts/verify-ios-simulator-stability.py` 是 DF-047 独立真实环境验收入口。
- `deploy/docker-emulator/images/test_sdk_catalog.py` 由 Python unittest discovery 调用。
- OpenAPI 生成 Client 的未引用 export 不手工删除；源规范和生成器是唯一真相。
- `ios_auto_replacement` 仅保留在三条“数量必须为 0”的集成测试断言中，用于阻止旧破坏性策略回归。

## 静态检查和自动化验收

- `go run golang.org/x/tools/cmd/deadcode@v0.36.0 -test ./...`：零结果。
- `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 '-checks=all,-ST*' ./...`：通过。
- `go vet ./...`：通过。
- `go mod tidy -diff`：依赖闭包一致；`golang.org/x/sys` 校正为实际直接依赖。
- `go test -p 1 ./...`：通过，包括真实 PostgreSQL 集成测试。
- Console `noUnusedLocals` 和 `noUnusedParameters` 已开启，TypeScript 编译通过。
- Console 9 个测试文件、50 项测试通过；生产构建通过。
- `pnpm generate` 后 `src/api/generated` 零漂移。
- Knip 文件报告逐项核对；三个文件均为脚本或配置入口误报，重复直接依赖已删除。
- `git diff --check`：通过。

## 正式环境验收与清理

- Android 使用正式 Reservation 和 UiAutomator2 Session，`/source` 返回 22,419 字节。
- 两台 iOS 同时占用不同长期 Simulator，均通过一次性 Grant、Session Fence 和 XCUITest；两次 `/source` 均返回 41,127 字符。
- Session 删除和 Reservation 释放后，三台 Device ID、Provider ref 和 Pool membership 不变；没有 delete、rebuild、reimage 或替代 create。
- Server 原容器重启暴露出 SSH 隧道仍绑定旧容器网络命名空间的问题；已只重建无状态隧道容器并复测两台 iOS。当前 Server 网络空间的 `127.0.0.1:4811` 和 `127.0.0.1:4842` 均监听，旧隧道壳已删除。
- 最终三台设备均为 `ready/healthy`，连续失败次数为 0。
- 最终正式库：Reservation、Session、Health Event、Audit Event、Host Command、Provisioning Job、Image Preparation、Idempotency、Console Session 均为 0。

全过程未记录 Token、密码、Cookie、Session Grant、数据库口令或 SSH 私钥。
