# 文档入口

开发和评审按以下顺序阅读：

1. [设备农场方案原文](reference/app_evaluation_device_farm_design.md)：设备域功能和内部链路基线；
2. [新版 Alcor 开发方案](reference/alcor_next_generation_plan.txt)：最新平台对象、接口、存储、前端和集成边界；
3. [三方对齐说明](03_alcor_generation_alignment.md)：新版方案、旧版 master 与设备方案的冲突裁决；
4. [项目范围与系统边界](00_scope_and_boundaries.md)：当前工作区做什么、不做什么；
5. [现有能力复用矩阵](01_existing_capability_reuse_matrix.md)：哪些必须复用、哪些允许新增；
6. [设备方案对齐表](02_architecture_alignment.md)：当前设备域如何落地；
7. [MVP 功能方案](04_mvp_functional_spec.md)：模块、流程、API、数据和错误模型；
8. [逐步实施清单](05_step_by_step_implementation.md)：后续逐项开发的任务编号、依赖、产出和单项验收；
9. [开发实施计划](06_development_plan.md)：阶段关系和防跑偏检查；
10. [MVP 验收方案](07_acceptance_test_plan.md)：阶段 Gate、测试用例、非功能指标和最终签收；
11. `adr/`：已经确认的架构决策。

如有冲突，先按三方对齐说明判断所属领域；仍无法判断时停止开发并取得需求方确认，不允许用旧版代码事实或临时实现悄悄改变方案。
