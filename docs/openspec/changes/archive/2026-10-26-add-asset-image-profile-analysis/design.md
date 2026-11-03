# Design

## Context

本 change 为已有项目资产的原始参考图提供档案分析草稿，依赖首版原图/成品分离及版本、资源保护。本文件保留 OpenSpec 设计索引，不维护第二份详细方案。

用户于2026-10-22确认推荐设计，主要实现和已批准增量已有实现及验证证据。2026-10-24用户明确批准收敛范围：当前验收覆盖资产图片分析主功能、来源引用保护和保守删除；完整生产者恢复、远端结算、旧数据维护迁至后续 change。此裁定优先于旧附件中将全部恢复作为5.2门槛的阶段性措辞，详见 [.comet/scope-convergence.md](.comet/scope-convergence.md)。

正式技术设计：[资产参考图档案分析技术设计](../../../../superpowers/specs/2026-10-22-asset-image-profile-analysis-design.md)。Comet design_doc 登记该文件。

## Goals / Non-Goals

目标、非目标及验收以 proposal.md 和 specs/asset-image-profile-analysis/spec.md 为准，不引入 Agent、多参考、批量生成或自动覆盖。

未知写者、无证据旧对象及未结算playback/远端义务保持安全拒绝，不清物理对象或最终业务记录。已完成P1/P2/P3-B、Windows C1与媒体补验、C2-A账本/设计成果及独立审查均保留；不要求本项使所有对象最终可删，不启用新产品入口。后续分别落在 [完整恢复](../../add-resource-producer-full-recovery/proposal.md)、[远端结算](../../add-remote-storage-physical-settlement/proposal.md)、[旧资源维护](../../add-legacy-resource-evidence-maintenance/proposal.md)，各自环境和操作授权不因拆分扩大。

## Decisions

专用 API、可信视觉文本模型、安全草稿和人工逐字段采用，以及 schema、长度预算、来源、收据、CAS/replay、真实媒体溯源，统一见正式设计第1–6节。旧 Open 建议由已确认设计替代，不再使用原文32,000字符或不存在的版本来源元数据假设。

## Risks / Trade-offs

幻觉、取消/完成竞争、资源生命周期及跨数据库事务验证见正式设计第4–7节。任务清理后的历史列表 UNION 不属于首发要求；必要幂等重放和历史采用来源仍保留。

## Migration Plan

分析草稿/收据采用既有 JSON 服务端保留键；已批准的媒体索引、删除意图及生产者增量迁移代码保留，不把初稿“无新增DDL”应用于这些后续成果。本轮范围收敛不增加schema、不操作业务库，不删除已采用历史结果。

## Open Questions

核心技术方案及full/zh-CN/autonomous/tdd/standard/current配置保持。最终Verify依收敛后的规范与实际证据执行；后续恢复/云端/旧数据维护继续遵守各自审批边界，不将移出项标为通过。
