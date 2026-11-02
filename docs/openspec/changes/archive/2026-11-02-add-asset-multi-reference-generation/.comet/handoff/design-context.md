# Comet Design Handoff

- Change: add-asset-multi-reference-generation
- Phase: design
- Mode: compact
- Context hash: 3f5058fd58dd860acadc313458d3f46b0d5b80bec8f990335ae9e445f3b605a5

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-asset-multi-reference-generation/proposal.md

- Source: docs/openspec/changes/add-asset-multi-reference-generation/proposal.md
- Lines: 1-28
- SHA256: 29aec5cc1c662fcb490600a670571c7f651d08368f78b247c0b60f30aa9853d9

```md
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

```

## docs/openspec/changes/add-asset-multi-reference-generation/design.md

- Source: docs/openspec/changes/add-asset-multi-reference-generation/design.md
- Lines: 1-49
- SHA256: 068ddd66ba38711c164cc5aa09266e69e1e7741be6973b3568b25bed4b2d58ed

```md
# Design

## Context

动机见 proposal.md。`project_reference_image.go` 当前只有 `referenceImage.resourceId`，并在 definition 编辑时保护该字段。`project_image_candidate.go:113–115,163–204` 拒绝多参考并从版本重建单张输入；通用任务输入已有 referenceImages 数组，不能将通用数组存在误当项目合同已支持多图。

`model_capability_defaults.go:20–95` 的服务端默认包括 16、可美 10、Grok 1、即梦 14 等不同张数；`web/src/lib/model-capabilities.ts` 还含按模型修正，`model-selection.ts` 已使用 maxImages。默认值只是已配置能力基线，不证明上游任意模型支持。本文件是待批准建议，状态仍为 open。

## Goals / Non-Goals

**Goals:** 单一规范集合、稳定输入顺序、可信模型准入与跨数据库资源生命周期一致性。

**Non-Goals:** 不新增参考图实体表或重量级媒体编排；不以双字段兼容代替迁移；不改变单输出/手动采用及 current/none 的语义。

## Decisions

### 1. 版本 definition 作为唯一存储
采用 `referenceImages: [{resourceId}]` 有序数组；空数组表示明确清空，重复资源拒绝。建议专用 `PUT /projects/:id/assets/:assetId/reference-images` 接收完整集合和 expectedVersionId；创建/摘要/角色编辑 DTO 同步调整，旧单数写接口移除，不长期双写。普通 definition 更新不得修改集合；复制版本时必须保留其值。复用不可变版本优于新表，因为参考与档案、声音/表现共同受同一 CAS 控制。

同一集合与当前顺序完全相同的请求可返回当前版本而不新建；并发旧基准仍明确冲突，生成任务幂等采用继续以任务事实为准。

### 2. 有界上限与服务端准入
建议应用持久集合上限 16，来源是现有通用默认最大参考基线，作用是界面/请求/锁集合有界；这不是用户指定产品数字，需随本 proposal 一并确认。实际生成上限 `min(16, resolvedModel.references.maxImages)`，并分别验证单文件及请求总预算。单次任务请求自带的 capabilityConfig 不能扩大服务端可信解析结果；经授权配置并由服务端校验的持久渠道能力可作为可信来源，不以硬编码默认值覆盖合法渠道配置。最终仍受协议实际传图能力与应用上限约束；未知协议或能力为未知直接拒绝。

后端应用能力解析是准入权威，前端展示相同结果。保存 3 张不因临时选择仅支持 1 张的模型而删图；只禁用该组合的生成。协议适配器必须证明全数组顺序实际写入 payload，某协议只有单图参数即拒绝多图，不能只通过上层计数校验。

### 3. 生成基准与资源锁
original 取版本完整集合，current 取该版本 primary 单图，none 为空；拒绝客户端列表与规范来源不匹配。任务固化实际列表及顺序，仍强制 count=1、manual。资源行加锁按 resourceId 稳定排序，业务数组顺序单独保留；在事务内再验证资源与版本，避免反向排序死锁和删除竞态。历史版本 definition、payload 快照、任务输入与成品引用扫描一并适配。

### 4. 显式一次性数据迁移
以下只是待批准的部署建议，本任务不执行迁移、停止任务或暂停写入；实际备份、生产数据变更与任务处置须另获环境操作授权。选择维护窗口批量迁移，而非长期读时双字段 fallback。备份后暂停新引用写入并让首版任务完成或明确取消/恢复处理，扫描所有 AssetVersion.definition 与实体 payload 中的旧单字段：旧值有效则转单元素；无值则空数组；两字段并存且不一致、损坏 JSON 或非法资源生成报告并阻止切换，不猜测合并。迁移只做等价结构改写，保留版本 ID/资源/候选基准，记录批次审计与可重入标记。

任务既有 referenceImages 输入快照不重写语义；旧首版待采用候选保持相同基准。迁移校验摘要和历史原图仍可见、引用计数不减少，再启用新合同。无需计划新增表列，但 JSON 迁移必须同步数据库专题；不能直接运行旧 worker 读取新结构。回滚优先关闭新入口、保留新版读写；恢复备份需维护窗口，存在多图后不做截断式逆迁移。

## Risks / Trade-offs

- [多参考是否被上游实际采纳不透明] → 对实际协议 payload 做顺序断言；不宣称所有声明兼容模型已真实验证。
- [旧历史 JSON 迁移影响审计] → 备份、等价映射和审计报告；不改变版本业务内容/ID，出现异常停止。
- [集合膨胀与总图片预算] → 16 张应用界限外仍有大小/像素/协议请求预算，入队前检查。
- [删除扫描遗漏旧位置] → 完整迁移并做历史、任务、payload 的资源引用回归。

## Migration Plan

先完成数据审计与迁移专项测试，再同步后端模型/API/锁和前端真实弹窗；更新 HTTP API、backend-database 与 pending-test。发布停写、备份、执行幂等迁移、核对数量和资源可见性后切换，不调整或归档首版任务清单。

## Open Questions

- 各已部署自定义渠道的真实总请求字节上限需在实施前读取渠道配置并形成测试矩阵；未知值不通过准入，不能拿默认 16 当上游保证。
- 参考缩略图的排序交互可选择拖拽加键盘移动按钮，最终措辞与布局在真实组件验收时收敛。

```

## docs/openspec/changes/add-asset-multi-reference-generation/tasks.md

- Source: docs/openspec/changes/add-asset-multi-reference-generation/tasks.md
- Lines: 1-22
- SHA256: 5621eb7a0a8714642218b5ae72d19a5a27962855b16890a0be63e80b13551111

```md
# Tasks

仅规划，应用上限 16 与迁移方案随内容统一确认；当前不执行产品验证。

## 1. 规范集合与迁移

- [ ] 1.1 将创建/摘要/专用更新 DTO 与 definition 保护统一为有序 referenceImages；API 测试覆盖清空、排序、重复、越权、CAS 及保留声音/成品，同步 HTTP API。
- [ ] 1.2 实现显式可重入单图迁移与异常报告；迁移测试覆盖历史版本、payload、无图、冲突双字段及已有候选基准，验证原图可见且无长期双写。
- [ ] 1.3 补充备份/维护窗口/回滚说明及 backend-database JSON 文档；使用迁移前后统计和资源映射 fixture 核对记录不丢失。

## 2. 生成与资源安全

- [ ] 2.1 实现应用/可信模型的最小张数上限及格式、大小预算；能力测试覆盖 0/1/多张、未知协议和客户端扩张能力，证明拒绝发生在入队前。
- [ ] 2.2 从版本重建有序 original/current/none 输入并维持单输出 manual；协议 payload 测试断言全部参考顺序，拒绝伪造来源及多输出旁路。
- [ ] 2.3 资源锁按稳定 ID 排序并扩展历史/任务/payload 引用扫描；SQLite/PostgreSQL 并发测试覆盖反向排序、删除竞争、事务回滚，同步资源合同。
- [ ] 2.4 回归单候选取消、失败、恢复、采用 CAS 和幂等；测试确保不自动采用、不重复版本、不回滚已保存参考。

## 3. 真实界面与集成

- [ ] 3.1 扩展真实资产弹窗的有序缩略图、移除/清空、键盘排序及模型限制提示；组件/API 测试覆盖三分类、刷新保序和不兼容模型不丢图，同步功能/pending-test。
- [ ] 3.2 真实页面以可控协议 mock 验证保存→生成→手动采用、取消/失败/冲突，检查明暗主题与弹窗滚动；无收费授权不真实调用模型。
- [ ] 3.3 执行最小专项测试及必要构建/类型检查，核对迁移部署演练与已知渠道矩阵；记录实际结果和未覆盖项，不把静态阅读当端到端通过。

```

## docs/openspec/changes/add-asset-multi-reference-generation/.openspec.yaml

- Source: docs/openspec/changes/add-asset-multi-reference-generation/.openspec.yaml
- Lines: 1-2
- SHA256: 0ad492a63d87e00a8bc135b0a1a2b19270dbb9ffd997a69b89fd4ab72015a944

```md
schema: spec-driven
created: 2026-10-22

```

## docs/openspec/changes/add-asset-multi-reference-generation/specs/asset-multi-reference-generation/spec.md

- Source: docs/openspec/changes/add-asset-multi-reference-generation/specs/asset-multi-reference-generation/spec.md
- Lines: 1-65
- SHA256: aadcace1aa462f16f76673d3311b326d647d83d3d1146ffd8740ff519f8a7389

```md
# Spec Delta

## Purpose

使项目内同一角色、场景或道具能够保存并明确排序多张参考图，在模型真实支持的范围内共同参与单张候选生成，同时保持手动采用、版本冲突控制和历史资源保护，不将参考集合误作成品集合。

## ADDED Requirements

### Requirement: 有序参考的持久化与权限
系统 SHALL 为单一资产维护有界、有序、无重复的参考集合；新增、移除及排序必须携带 expectedVersionId，校验用户/项目/资产关联及每个 ready 图片资源归属，原子创建新版本，保留声音、定义和成品。

#### Scenario: 保存并恢复顺序
- **WHEN** 用户保存三张参考并调整顺序后刷新
- **THEN** 当前版本显示同一顺序，历史版本保留原顺序，成品和声音不变

#### Scenario: 非法集合与写竞争
- **WHEN** 集合包含重复、越权、未就绪资源，或 expectedVersionId 已过期
- **THEN** 整体拒绝，不部分保存、不覆盖新版本；普通 definition 编辑不能注入或清除参考集合

### Requirement: 模型能力与应用上限准入
系统 SHALL 同时执行有界应用上限和服务端确认的模型参考张数、文件大小及格式限制。生成前必须展示有效限制；能力未知、不支持或任一参考超限时 MUST 在上游调用前拒绝，不能静默裁掉图片或切换模型/无参考模式。

#### Scenario: 不同模型限制
- **WHEN** 已存三张参考而所选模型仅支持一张
- **THEN** 集合仍完整保存，生成按钮说明不兼容，提交也被服务端拒绝，用户可显式调整参考或模型

#### Scenario: 伪造能力
- **WHEN** 客户端宣称更高张数但解析出的可信模型能力不支持
- **THEN** 使用可信能力拒绝，不能因客户端参数绕过限制

### Requirement: 所选来源全量有序输入和单候选
系统 SHALL 从目标基准版本解析 original 有序集合，current 仅取显式当前成品，none 不携带参考；提交来源必须明确且不能混合。任务只生成一个候选，成功不会自动采用。

#### Scenario: 多参考共同生成
- **WHEN** 能力允许且用户以 original 提交有序参考集合
- **THEN** 所有参考按保存顺序进入实际模型协议，候选关联该基准与输入快照，仅手动确认才能采用

#### Scenario: 伪造来源或多输出参数
- **WHEN** 客户端提交与版本来源不一致的资源、顺序、URL，或通过其他参数要求多个输出
- **THEN** 服务端拒绝，不接受被篡改的输入快照或多候选结果

### Requirement: 引用移除不等于资源删除
系统 SHALL 在版本、任务输入及其他业务引用存在时阻止物理删除，资源锁顺序不依赖用户排序；从当前集合移除图片不能破坏历史版本或正在运行的任务。

#### Scenario: 排序与删除并发
- **WHEN** 两个请求反向排序同批资源并与资源删除并发
- **THEN** 请求不得因锁顺序互相永久等待，只有满足 CAS 和资源有效性的写入可成功，有引用资源仍可访问

### Requirement: 首版单图显式迁移
系统 SHALL 将已有单参考数据迁移为同内容的单元素有序集合，不丢失历史资源或已有候选来源；迁移后只有一个规范字段作为写入事实，冲突或损坏数据必须报告而非猜测覆盖。

#### Scenario: 已有单图与在途任务
- **WHEN** 部署迁移包含首版资产历史、摘要快照和待采用候选
- **THEN** 原图仍可见、候选仍关联原基准且可按原规则采用，新写入使用规范集合，不需要长期双字段存储

### Requirement: 失败取消与采用重放
系统 SHALL 沿用单候选的取消、脱敏失败、手动采用、CAS 和幂等规则，输入顺序快照可追踪；取消或失败不会回滚用户已保存参考或自动覆盖成品。

#### Scenario: 取消或迟到完成
- **WHEN** 多参考生成取消或上游失败后刷新页面
- **THEN** 显示真实任务状态，原参考集合和成品保留，不自动重试收费任务

#### Scenario: 采用冲突和重放
- **WHEN** 成功候选采用遇到版本变化，或相同采用请求重放
- **THEN** 冲突保留候选且不覆盖；已成功的同任务采用返回原结果而不重复创建版本

```
