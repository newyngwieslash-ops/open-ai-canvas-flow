# Comet Design Handoff

- Change: add-legacy-resource-evidence-maintenance
- Phase: design
- Mode: compact
- Context hash: 62d811973cec529e00932122e83b36fe0f2af3cc1dcaa2ae588558145d9f68f0

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-legacy-resource-evidence-maintenance/proposal.md

- Source: docs/openspec/changes/add-legacy-resource-evidence-maintenance/proposal.md
- Lines: 1-26
- SHA256: 9c7eaf1e1995847c2447c5810e65c42a6d12ae0d8de2c5b8bed286c5e6617588

```md
# Proposal

## Why

旧资源和未知缩略图缺少可信物理身份及停写证据，不能仅按当前配置或业务状态安全接管。用户批准把证据建立与维护流程迁至独立change；当前资产分析功能继续保留证据不足时的删除阻断。

## What Changes

- 建立有可核验来源的历史manifest，关联资源归属、物理身份、变体、alias、历史writer与全部未决attempt。
- 在隔离副本做逐对象eligible/blocked/conflict dry-run，验证接管、重复执行、失败恢复与审计；未知项保持阻断，不猜key或补造settled。
- 真实维护仅在用户审阅具体差异、备份/恢复、写入屏障与对象清单后另行批准；只读原库、补登记、真实删除、停服务和迁移分别限定。
- 非目标：本提案不读取业务库或inventory、不维护真实数据、不修改provider能力，不把运维确认当作客观停写证明。

## Capabilities

### New Capabilities
- `legacy-resource-evidence-maintenance`: 依可信历史身份和停写来源判定旧对象是否具备安全接管条件，支持隔离演练及逐项授权的维护。

### Modified Capabilities
无。原分析来源保护与unknown阻断不变。

## Impact

- 候选影响只读导出、隔离核证/接管工具、资源删除实际入口与审计；具体代码与维护操作仍须后续审批。
- 继承[P5最小前提](../add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)及[现有保守安全合同](../add-asset-image-profile-analysis/.comet/scope-convergence.md)。已有本地反例不是可信旧manifest或维护授权。
- 核心验收：来源可核对、历史位置/全部writer可追溯，过期快照/跨用户/共享alias/未知变体均正确阻断；隔离实际入口失败不清记录，live差异逐项审阅授权。本次所有实施任务保持未完成。

```

## docs/openspec/changes/add-legacy-resource-evidence-maintenance/design.md

- Source: docs/openspec/changes/add-legacy-resource-evidence-maintenance/design.md
- Lines: 1-26
- SHA256: a59ebdd438fd4f12068943c09447165439c4f441097f694d8119f22a5f10a239

```md
# Design

## Context

见proposal。当前模型不提供可信旧thumbnail清单，已有隔离反例只能证明保守阻断；本次不假设业务库确有或没有问题行。

## Goals / Non-Goals

先核对可信历史身份和停写来源，再设计隔离接管与逐项维护。非目标是通用全库修复器、按当前桶猜历史位置、批量settled或本次操作真实业务数据。

## Decisions

- 以原[P5最小前提](../add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)为合同输入，优先维护方提供已授权脱敏导出；执行者读取原库须单独精确只读授权。
- dry-run只输出eligible/blocked/conflict、依据和最小差异，完整性/来源/停写不明一律阻断，不强制新增签名体系或相信手写trusted标记。
- 隔离副本验证实际入口的权限、引用、alias、事务与失败重入，纯manifest解析测试不足以完成验收。
- live维护按具体对象名单逐项审批，并在执行前重新核对身份/引用/快照/写入屏障；批准补登记不自动批准删除、kill、停服务或迁移。

## Risks / Trade-offs

- 历史writer不遵守新锁 → 新锁空闲不产生接管权，必须另有停写来源。
- 远端已接收请求仍可能物化 → 依赖远端结算change的具名证据，不能用本地进程退出代替。
- 维护中快照变化或共享对象 → 保持blocked并重新审查精确差异，不扩大清理范围。

## Migration Plan

本次无导出、迁移、补登记或删除。未来获批代码先在可丢弃副本/合成对象验证及独立审查；真实操作单独附备份恢复、维护窗口与精确收尾。

```

## docs/openspec/changes/add-legacy-resource-evidence-maintenance/tasks.md

- Source: docs/openspec/changes/add-legacy-resource-evidence-maintenance/tasks.md
- Lines: 1-14
- SHA256: 6fdca63948be24d39b0cbbbfcc9c6b96afba88e6a2577f67538eb60d63bc92be

```md
# Tasks

承接未完成旧对象取证与维护合同；全部任务保持未完成。当前仅open提案，不批准业务库读取、导出、live维护或真实删除。

## 1. 可信证据与隔离方案

- [ ] 1.1 取得维护方可信脱敏manifest/历史writer/停写来源，限定对象及快照范围；如需读取原库，先取得精确只读授权。 <!-- comet-task:b4525ff4-fda3-4419-be0f-c0b153ca726b -->
- [ ] 1.2 细化并审批隔离dry-run及最小接管工具，覆盖来源完整性、归属/变体/alias、未知项和过期快照判定。 <!-- comet-task:ad1dd2b6-cc9f-4443-9e59-3f783ab4ddbf -->

## 2. 隔离验收与具体维护

- [ ] 2.1 获批后实现逐对象eligible/blocked/conflict及差异清单，完成伪造来源/跨用户/未知writer/共享alias/重复执行/失败恢复等隔离验证。 <!-- comet-task:79f3a2b7-f177-49df-96e5-dd98197c9393 -->
- [ ] 2.2 在可丢弃副本和合成对象通过实际入口验证权限、引用、事务和部分失败，经独立审查并完成最终Verify，不写原业务库。 <!-- comet-task:f3c92bbb-1515-41fc-8304-c9c3bcc27944 -->
- [ ] 2.3 如确需live维护，提交精确对象/差异、备份恢复、窗口、写入屏障及分项权限审批；未获批保持blocked，不自动执行。 <!-- comet-task:df9542ac-8816-4baa-8e2b-7cd8538bb4c9 -->

```

## docs/openspec/changes/add-legacy-resource-evidence-maintenance/.openspec.yaml

- Source: docs/openspec/changes/add-legacy-resource-evidence-maintenance/.openspec.yaml
- Lines: 1-2
- SHA256: e2ebaebee255ccc1e09b64ba38d9b09c360a7392fc110a48ca2525e1ea49fa46

```md
schema: spec-driven
created: 2026-10-24

```

## docs/openspec/changes/add-legacy-resource-evidence-maintenance/specs/legacy-resource-evidence-maintenance/spec.md

- Source: docs/openspec/changes/add-legacy-resource-evidence-maintenance/specs/legacy-resource-evidence-maintenance/spec.md
- Lines: 1-28
- SHA256: 11af75348a32e64bc19652ba631b0da7f31c8e8e6357f3757ed65ee8cde64603

```md
# Spec Delta

## Purpose

为缺乏可信身份或停写证据的旧资源建立可核验、可审计的维护判定流程，先在隔离副本证明接管安全性，再按精确对象和操作授权维护；未知历史变体、共享归属或旧写者未决时继续阻断，不默认修补业务状态。

## ADDED Requirements

### Requirement: 可信身份及历史停写来源
系统 SHALL 仅接受来源可核对的manifest及历史物理身份、归属、变体/alias和全部writer停写证据，不以当前配置、文件相似或自报trusted代替。

#### Scenario: 未知旧缩略图或缺少writer证据
- **WHEN** 无法证明必要变体位置、归属或历史写者可靠终态
- **THEN** 判为blocked，保留原记录和物理对象，不猜key、不补settled

### Requirement: 隔离dry-run与实际入口
系统 SHALL 先在授权隔离副本产生逐对象eligible/blocked/conflict、依据和预计差异，再以实际入口验证失败恢复与重复执行。

#### Scenario: 过期快照及共享归属
- **WHEN** 快照已变化、跨用户/namespace或存在同物理alias
- **THEN** 阻断或重取授权证据，不修改其他归属对象；隔离执行失败不清业务记录或伪造结算

### Requirement: 真实维护逐项授权
系统 SHALL 在审阅精确差异、对象清单、备份/恢复、窗口和写入屏障后才执行批准操作；只读、补登记、真实删除、停服务及迁移 MUST 分别明确。

#### Scenario: 只有提案或dry-run批准
- **WHEN** 尚无具体live维护授权
- **THEN** 不读取或写入原业务库、不删真实对象、不控制既有服务；继续提供隔离报告并保留未决项

```
