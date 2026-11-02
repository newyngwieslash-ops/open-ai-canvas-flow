# Comet Design Handoff

- Change: add-resource-producer-full-recovery
- Phase: design
- Mode: compact
- Context hash: 69fd48aff5866d34381f905c94e8ded503d0d01fb930a3cf29b5b909dbb6bdbf

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-resource-producer-full-recovery/proposal.md

- Source: docs/openspec/changes/add-resource-producer-full-recovery/proposal.md
- Lines: 1-27
- SHA256: 65c8c80551870ed913532afdeb7e4d7c09cdde7ca0271db0e20ac9543161da45

```md
# Proposal

## Why

资产图片分析已具备可独立验收的主功能；完整生产者恢复需要专门的OS环境、权限和产品接入验收。用户批准将这部分未完成工作迁至本change，避免把保守拒删误写成恢复完成，也避免阻塞分析功能交付。

## What Changes

- 承接Linux C2-A具名独占VM/UID/namespace/权限验证及C2-B产品并发身份、文件隔离、持久绑定与恢复接入。
- 继续解决Windows全部原句柄/控制者丢失且无持久终态的缺口；不能以重建空域、同名Job或unknown当恢复成功。
- 复用P1/P2/P3-B、Windows有限C1、真实媒体及C2-A私有账本的已完成成果与审查记录，不重置旧审查轮次。
- 所有新增环境、OS权限、schema和正常产品入口必须按具体最小范围另行审批；本次仅创建待办提案，不实施。
- 非目标：不操作业务库、远端云端点、旧对象维护或付费模型；不放宽未知义务的删除阻断，不默认禁用任何平台。

## Capabilities

### New Capabilities
- `resource-producer-recovery`: 在可信控制域及具名环境内恢复物理写入生产者，保留无法证明终态时的安全阻断。

### Modified Capabilities
无。原资产图片分析的来源保护与保守删除合同保持。

## Impact

- 候选接入面为后端app/repository的producer、转码、删除worker、持久绑定与部署配置；实际修改以能力验证和后续批准为准。
- 依赖已有[成果审计](../add-asset-image-profile-analysis/.comet/completion-audit.md)、[完整恢复设计](../add-asset-image-profile-analysis/.comet/producer-full-recovery-design.md)及[具名环境审批稿](../add-asset-image-profile-analysis/.comet/producer-c2a-os-environment-design.md)。已有逻辑VM候选未创建/绑定，OS矩阵NOT RUN。
- 核心验收：全部控制者退出、无原控制句柄借用后，新控制者核对原代际恢复；全部写者已停且不再迟到进入后才结算/清理；身份或环境不明继续unknown。候选失败不改变当前产品安全栅栏。

```

## docs/openspec/changes/add-resource-producer-full-recovery/design.md

- Source: docs/openspec/changes/add-resource-producer-full-recovery/design.md
- Lines: 1-26
- SHA256: 4699243a426054d5fe562faf76a11bf36ea3d084997ee9515b1e401f5491c7c7

```md
# Design

## Context

见proposal。P1/P2/P3-B、有限Windows C1、媒体补验及C2-A临时账本已交付，证据仍在原change；具名OS方案仅设计PASS，VM不存在且未绑定。该文件是迁出待办的设计入口，不是具体实施批准。

## Goals / Non-Goals

目标是先证明OS终态与隔离，再独立审批产品接入。保持单operation能力实验与多operation产品合同分离；不扩展到远端结算或旧数据维护。

## Decisions

- 复用原[完整恢复设计](../add-asset-image-profile-analysis/.comet/producer-full-recovery-design.md)和[具名环境稿](../add-asset-image-profile-analysis/.comet/producer-c2a-os-environment-design.md)，不复制第二套实现方案。先绑定实际平台/镜像/hash/路径/管理员，再提出精确权限及实验审批。
- Linux C2-A优先验证真实控制域及本地生命周期门；保留原files_struct连续性、身份/保管、能力/NNP、IPC、迟到入域及精确收尾硬前提。旧账本模型通过不能替代OS证据。
- C2-B待A成立后才细化产品身份/文件沙箱、schema、全部调用方及正常入口；不默认实施候选列或启用环境变量。
- Windows维持有限C1的unknown边界，完整方案须另有实证或产品裁定；普通同名Job与增加监督者不作为既成证明。

## Risks / Trade-offs

- 环境或内核前提不成立 → 候选失败，保留阻断，不增权绕过。
- 单例实验外推并发安全 → 产品接入单独验证身份/文件/进程隔离。
- 旧报告路径在原change归档后变化 → 实施恢复时核对归档实际路径及原hash，不重写已关闭审查历史。

## Migration Plan

本次不迁移或启用。未来具体schema/部署变更须有临时库验证、回滚策略和独立批准；保留所有未决义务与旧协议行为。

```

## docs/openspec/changes/add-resource-producer-full-recovery/tasks.md

- Source: docs/openspec/changes/add-resource-producer-full-recovery/tasks.md
- Lines: 1-15
- SHA256: 90792147954ab875d1d57cc108eb2146edeeb99736c7bbf694d0da8db5d751e9

```md
# Tasks

承接原change的未完成恢复验收；已有P1/P2/P3-B/C1/媒体/C2-A账本和设计成果不重复实施。当前仅创建open待办，以下均未完成；真实环境、具体实现及正常入口另批。

## 1. OS能力与恢复

- [ ] 1.1 绑定实际独占VM、离线镜像/hash、路径、管理员、工具与持续保管合同，提交精确namespace/UID/权限/清理方案审批。 <!-- comet-task:2cbe9f87-0308-4034-a426-02f552a8ed79 -->
- [ ] 1.2 获批后实现C2-A最小OS适配与fixture，完成真实创建/全部控制者崩溃/迟到入域/父files_struct连续性/身份与IPC隔离/双库/四组媒体及收尾矩阵和独立审查。 <!-- comet-task:40e4f06a-037d-474f-b3d0-f62c7138e499 -->
- [ ] 1.3 解决Windows全原句柄及控制者丢失后的可信恢复路线，完成具名平台验收；不可行时提交明确产品裁定，保持unknown阻断。 <!-- comet-task:e9084af1-0192-4d11-afb3-e33c9aa638a7 -->

## 2. 产品接入与交付

- [ ] 2.1 基于能力证据细化并审批C2-B每operation身份/文件/进程隔离、持久绑定/schema、全部调用方及回滚方案。 <!-- comet-task:f263dd5b-2809-43af-ac82-eecc3e152bdd -->
- [ ] 2.2 获批后接入产品，完成临时迁移、SQLite/独占PG并发、真实上传/删除/HTTP鉴权/Range/播放、既有协议回归与独立审查。 <!-- comet-task:3ad641e2-e802-4457-b826-67e54875dc50 -->
- [ ] 2.3 另批具名正常入口与部署策略，完成最终Verify和文档；业务数据、真实删除、归档/交付仍按有效授权处理。 <!-- comet-task:6ce77325-47a2-418b-b4a2-200363be1079 -->

```

## docs/openspec/changes/add-resource-producer-full-recovery/.openspec.yaml

- Source: docs/openspec/changes/add-resource-producer-full-recovery/.openspec.yaml
- Lines: 1-2
- SHA256: e2ebaebee255ccc1e09b64ba38d9b09c360a7392fc110a48ca2525e1ea49fa46

```md
schema: spec-driven
created: 2026-10-24

```

## docs/openspec/changes/add-resource-producer-full-recovery/specs/resource-producer-recovery/spec.md

- Source: docs/openspec/changes/add-resource-producer-full-recovery/specs/resource-producer-recovery/spec.md
- Lines: 1-35
- SHA256: df09717c2371ce208dd9fba70f14307ac700452767e608f8d8cd9ebedfb60166

```md
# Spec Delta

## Purpose

为已登记物理写入生产者提供在控制者崩溃后的可信恢复，使删除和发布依据可核对终态处理；持续保留身份、环境或停写证据不足时的安全阻断，不把有限平台实验或unknown状态当作恢复成功。

## ADDED Requirements

### Requirement: 具名环境与授权门槛
系统 SHALL 仅在已核实且明确批准的独占环境代际内验证和启用恢复能力；未绑定平台、工具、权限及持续保管条件时 MUST 拒绝启动，不默认安装、提权或启用产品入口。

#### Scenario: 候选环境尚未具备
- **WHEN** 仅有逻辑VM名称或环境设计PASS，实际绑定与执行授权尚缺
- **THEN** 验证保持未执行，不启动写者、不变更系统配置，不签发能力通过结论

### Requirement: 全控制者丢失恢复
系统 SHALL 在全部原控制者退出且无旧控制能力借用后，从可信持久绑定恢复同一原代际。只有证明全部写者已停且后续创建被封闭，才允许物理结算和清理。

#### Scenario: 崩溃与在途创建
- **WHEN** 控制者在登记、创建、关闭或清理窗口全部死亡
- **THEN** 新控制者核对原身份和启动屏障恢复，无法排除迟到入域或写入时保留义务，不重建空域洗白

### Requirement: 产品并发隔离与结果分离
系统 SHALL 分离业务结果与物理停止证据，保证每个operation不能影响其他operation的身份、文件或进程；缺失成功结果时 MUST NOT 发布ready。

#### Scenario: 并发操作及丢失结果
- **WHEN** 多个操作并发或写者停止但成功结果丢失
- **THEN** 各自对象和控制域保持隔离，丢失结果记未知，不用可解码文件补造成功或清除其他操作义务

### Requirement: 平台完整性与保守失败
系统 SHALL 对实际支持的平台分别提供能力证据和明确策略，不外推Linux证据为Windows通过；身份断裂或恢复能力不足时 MUST 保留阻断。

#### Scenario: Windows原句柄全部丢失
- **WHEN** 原控制句柄及可靠持久终态均已丢失
- **THEN** 在新方案真实验收前继续unknown，不以同名空Job或监督者重启推定原写者停止；改变平台支持范围须另有产品裁定

```
