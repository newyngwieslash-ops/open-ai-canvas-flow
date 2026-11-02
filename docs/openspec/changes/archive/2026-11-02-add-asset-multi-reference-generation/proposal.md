# Proposal

## Why

单张参考无法同时表达同一资产的外观细节和不同角度。通用生成协议已有 referenceImages 数组，但项目资产首版明确限制单参考，必须补齐持久化、能力准入和引用生命周期，不能只让前端多选。

## What Changes

- 同一角色、场景或道具保存有序参考集合，增删/排序通过版本 CAS，已有成品和声音不变。建议新增空集合“清空参考”能力（首版不支持），须随本提案一起确认，不删除历史资源。
- **BREAKING** 将单 referenceImage 合同迁移为唯一有序 referenceImages 合同，同步创建、摘要、编辑保护、任务准备和资源扫描；提供显式一次性迁移，拒绝长期双字段写入。
- 按可信模型能力和有界应用上限准入，服务端重建全部有序参考。保持单输出候选、手动采用，不支持或超限直接拒绝，不静默丢图/回退。
- 非目标：不同资产融合、同资产多候选、批次调度、自动采用、改变全局角色引用优先级。

## Capabilities

### New Capabilities
- `asset-multi-reference-generation`: 同一资产有序参考的版本化管理、能力准入及单候选生成。

### Modified Capabilities
无。已发布 `project-asset-image-binding` 管理成品表现，不等于参考集合；首版仍未归档，本项通过明确依赖扩展其实现，不修改首版规划。

## Impact

- 依赖 `add-project-reference-image-assets`；执行顺序在档案分析之后、项目批量之前，档案分析不是必需运行依赖。
- 涉及 project_reference_image、project_image_candidate、definition 保护、repository 锁/引用扫描、模型能力、projects API 与真实资产弹窗。
- 模型能力代码已有不同上限；design 建议应用最多 16 张，实际生成取应用上限与可信模型张数上限较小值。这是待批准工程建议，不是用户指定数字。
- 核心验收：排序保存→所有参考按序传递→单候选手动采用；越权、超限、未知模型、伪造数组、CAS、取消/失败、迁移和历史资源删除保护。
- 实施须同步 HTTP API、backend-database JSON/迁移说明和 pending-test。本轮仅规划且内容待统一确认。
