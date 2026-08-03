# 项目开发强制规则

1. 修改或新增代码前，必须阅读：
   - `docs/reference/app_evaluation_device_farm_design.md`
   - `docs/reference/alcor_next_generation_plan.txt`
   - `docs/00_scope_and_boundaries.md`
   - `docs/01_existing_capability_reuse_matrix.md`
   - `docs/02_architecture_alignment.md`
   - `docs/03_alcor_generation_alignment.md`
   - `docs/04_mvp_functional_spec.md`
   - `docs/05_step_by_step_implementation.md`
   - `docs/07_acceptance_test_plan.md`
   - `docs/adr/0001_project_origin_and_dafit_reuse.md`
   - `docs/adr/0003_authoritative_design_precedence.md`
   - `docs/adr/0004_align_with_alcor_next_generation.md`
   - `docs/adr/0005_fixed_two_device_pool.md`
   - `docs/adr/0006_fixed_min_ready_warm_pool.md`
2. 开发前必须先搜索以下现有项目，确认没有可直接复用的能力：
   - `E:/AutoTestTools/Projects/Alcor`
   - `E:/AutoTestTools/Projects/dafit_auto_platform`
3. 本项目只实现设备域。禁止新增新版 Alcor 业务域的 Case、Dataset、Target、Config、Run、RunAttempt、评分、业务报告、Artifact 业务索引和发布门禁。
4. 禁止在本项目重写 DaFit 已有的 Appium WebDriver 执行、页面对象、元素动作、断言、证据、Runner 和 HTML/JSON 报告。
5. STF 只通过 Adapter 使用。禁止重写 STF 的远程看屏、设备日志、文件管理、claim、release 和 remoteConnect。
6. Appium Adapter 只管理 Endpoint、端口和健康状态，不执行 DaFit 业务步骤。
7. 只有 `docs/01_existing_capability_reuse_matrix.md` 标记为“允许新建”的能力可以直接实现。出现新能力时，必须先更新复用矩阵或新增 ADR。
8. 新增功能必须具有单一职责；不得通过复制现有文件形成第二套兼容实现。
9. 设备农场内部能力以 `app_evaluation_device_farm_design.md` 为基线；Alcor 平台对象、接口、存储、前端和集成边界以 `alcor_next_generation_plan.txt` 为最新基线。冲突时按 `docs/03_alcor_generation_alignment.md` 和 ADR-0004 处理，禁止继续沿用旧 Alcor 假设。
10. 本仓库不得发展成第二套评估平台、第二套 Eval Console 或第二套 Run/Result 数据库；设备域可以独立运行，并通过新版 Worker 的 Device Farm Adapter 接入。
11. 新增 API、表、状态或模块前，必须在 `docs/02_architecture_alignment.md` 和 `docs/03_alcor_generation_alignment.md` 找到对应项；找不到时先更新对齐表并说明依据。
12. 禁止把本地旧版 Alcor master 的 `test_items`、`eval_tasks`、整数 ID、本地 `/tasks` 报告路径和进程内 goroutine 执行方式写入新的设备接口；旧版只用于理解历史和迁移来源。
13. 开发任务必须使用 `docs/05_step_by_step_implementation.md` 中的编号和顺序；未满足该任务验收标准、未保存证据时不得标记 completed 或进入依赖它的任务。
14. MVP 整体验收以 `docs/07_acceptance_test_plan.md` 为准；Mock 通过不得替代 Linux KVM、Docker Emulator、STF、Appium 和 DaFit 的真实验收。
15. 每个 DF 任务通过验收并更新证据后，必须单独执行一次 Git commit。提交说明使用简洁、直白的中文，直接说明该步骤完成了什么；未通过验收的任务不得以完成名义提交。
