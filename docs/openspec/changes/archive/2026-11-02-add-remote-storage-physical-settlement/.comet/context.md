# Comet Design Handoff

- Change: add-remote-storage-physical-settlement
- Phase: design
- Mode: compact
- Context hash: 664e3a6b0b9e3b824fb64ad08f58c0d0812103963c0dfa567136930a7053a98a

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-remote-storage-physical-settlement/proposal.md

- Source: docs/openspec/changes/add-remote-storage-physical-settlement/proposal.md
- Lines: 1-26
- SHA256: 116349b73d1b7573b65003f8b4f14b38daa4f17b640549d3a9807826c8e90ffd

```md
# Proposal

## Why

远端响应、客户端取消或本地进程死亡不能独立证明所有已接收请求不再物化。用户批准将远端所有attempt的物理结算和完整清理迁为独立change；资产分析主功能保留unknown阻断，不承担未获证的供应商保证。

## What Changes

- 按具名provider产品/版本、端点、桶类型及配置证明启动、重试、未知init、part/Complete/Abort与版本/残片的终态合同。
- 在获明确批准的独占测试范围内进行真实端点故障验证，再据证据形成产品adapter最小实施范围、临时双库及独立审查。
- 保留已有本地HTTP/真实SDK隔离反例和全部未知义务；不将最后一次成功响应或有限时间无新写入当作全部attempt结算。
- 非目标：不默认限制provider、改业务桶配置或执行全桶扫描；本提案不批准云调用、费用、凭据、对象创建/删除或产品schema实施。

## Capabilities

### New Capabilities
- `remote-storage-physical-settlement`: 在明确端点合同内跟踪全部写入尝试并证明物理结算，按精确归属安全清理所有必要对象。

### Modified Capabilities
无。当前保守删除安全合同及未知义务阻断保持。

## Impact

- 候选影响storage adapter、producer登记/结算、删除意图/worker及临时数据库验证；先证明语义，再批准具体实现。
- 继承[隔离验证结果](../add-asset-image-profile-analysis/.comet/producer-p345-capability-results.md)及[P4/P5最小前提](../add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)。需具名端点、专用账户/桶/前缀、版本/保留配置、精确API权限、预算、凭据注入引用及收尾责任人。
- 核心验收：未知init可发现且扫描后无旧启动；全部SDK重试/迟到请求有可信终态；Complete/Abort竞争与结果丢失可恢复；versions/parts/alias清理完整，权限或证据不足保留记录。未覆盖端点不外推通过。

```

## docs/openspec/changes/add-remote-storage-physical-settlement/design.md

- Source: docs/openspec/changes/add-remote-storage-physical-settlement/design.md
- Lines: 1-26
- SHA256: 3cfbf1bec3b64049931b81a54a2efae876bcc2b6a06242e889fcd027153411a3

```md
# Design

## Context

见proposal。已有HTTP/SDK隔离反例证明普通响应不足；没有具名真实云端点验证。新change承接这份缺口，不重做本地反例或把账户权限当语义保证。

## Goals / Non-Goals

先对实际使用端点建立可核验合同和隔离验证，再审批最小adapter实现。非目标是全桶扫描、业务配置变更、任意provider限制或在本次执行付费云操作。

## Decisions

- 以原[P4验收前提](../add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)为输入模板，一家具名端点逐项闭合；无版本桶结果不替代实际支持的版本桶。
- 依赖正式启动/终止/列举/提交语义与可制造的故障证据共同证明；有限黑盒试验和客户端取消不能排除迟到物化。
- 完整身份/全部attempt先落持久记录，结算、成功发布和删除分别保持现有栅栏。具体字段及调用方待端点语义证明后再设计批准，不先引入通用双表/全量staging。
- 真实端点费用、对象数/字节/时间上限、凭据引用、权限撤销/恢复与精确清理单独批准，报告不记录秘密。

## Risks / Trade-offs

- 端点不提供所需终态保证 → 保持unknown，必要产品范围取舍须明确裁定。
- 漏跟SDK重试/未知init → 必须覆盖每个attempt和全部分页，空列表不单独代表启动屏障。
- 配置切换或权限丢失 → 保存原身份并阻断，不套用当前默认位置。

## Migration Plan

本次无代码/schema/云操作。未来实施先临时SQLite与独占PG、合成故障和具名云实验，再审查产品入口及回滚；不迁移业务库。

```

## docs/openspec/changes/add-remote-storage-physical-settlement/tasks.md

- Source: docs/openspec/changes/add-remote-storage-physical-settlement/tasks.md
- Lines: 1-14
- SHA256: bbf424955e49c2b4dd8dc0736eeaf720db4e227f571153e70cce656ec54d1794

```md
# Tasks

承接原change未完成的远端结算合同，已有本地隔离反例保留。当前仅open提案，不批准任何云费用、凭据、测试对象或产品实施。

## 1. 端点合同与真实实验

- [ ] 1.1 确认实际provider产品/版本/配置及正式终态语义，绑定专用测试端点/桶/前缀、API权限、预算/次数/字节/时间、凭据引用和收尾责任并取得授权。 <!-- comet-task:6d93e144-36ab-4a34-a719-0728c01fd336 -->
- [ ] 1.2 在批准范围验证全部重试、未知init、part/Complete/Abort竞争、响应丢失、版本/parts/alias和权限故障；记录实测/合同支撑/未知项及精确收尾。 <!-- comet-task:a8abbfec-7538-4d8d-96b7-3b79556d4a0b -->

## 2. 产品结算与验收

- [ ] 2.1 基于端点证据细化最小adapter、持久身份/attempt/结算及删除恢复方案，提交具体实施审批。 <!-- comet-task:a05b062f-f4a3-43ab-882e-588bd63b3412 -->
- [ ] 2.2 获批实施后完成临时SQLite/独占PG故障和并发矩阵、实际端点产品入口、配置变化/权限丢失/部分失败回归及独立审查。 <!-- comet-task:57e42667-dd92-4b9b-8217-a98c7eef655f -->
- [ ] 2.3 更新端点支持边界与操作文档并完成最终Verify；未知端点保留阻断，能力限制或业务操作仍须明确裁定/授权。 <!-- comet-task:c3121dbf-341b-4941-b29c-469578182040 -->

```

## docs/openspec/changes/add-remote-storage-physical-settlement/.openspec.yaml

- Source: docs/openspec/changes/add-remote-storage-physical-settlement/.openspec.yaml
- Lines: 1-2
- SHA256: e2ebaebee255ccc1e09b64ba38d9b09c360a7392fc110a48ca2525e1ea49fa46

```md
schema: spec-driven
created: 2026-10-24

```

## docs/openspec/changes/add-remote-storage-physical-settlement/specs/remote-storage-physical-settlement/spec.md

- Source: docs/openspec/changes/add-remote-storage-physical-settlement/specs/remote-storage-physical-settlement/spec.md
- Lines: 1-35
- SHA256: 174cd6ce780ed5d64e0c735b44595979d50f3b434da163db0f6dbf6d25abeedb

```md
# Spec Delta

## Purpose

为具名远端存储建立涵盖全部已接收写入尝试的可信物理结算合同，覆盖会话、分片、版本与派生对象；使删除失败可恢复且不误删共享归属对象，在端点语义或证据不足时持续保留记录和安全阻断。

## ADDED Requirements

### Requirement: 端点合同与执行范围
系统 SHALL 将验收结论限定在实际provider产品/版本、端点和配置；真实实验 MUST 具备专用资源、精确操作、预算及清理授权，不能外推兼容服务等价。

#### Scenario: 缺少真实端点或授权
- **WHEN** 仅有本地HTTP反例或未提供测试资源/权限/费用边界
- **THEN** 不启动云实验，不记端点通过，不默认改变provider支持范围

### Requirement: 全部写入尝试的终态
系统 SHALL 在写前绑定精确物理身份并覆盖全部重试、未知启动和在途提交；只有可信证据排除未来物化后才可物理结算。

#### Scenario: 启动或完成响应丢失
- **WHEN** 远端已可能接受请求但客户端未获得会话ID或成功body
- **THEN** 恢复所有可能会话及对象，并证明扫描后无旧启动；无法证明时保留unknown，不用超时或最后一次响应清除义务

### Requirement: 物理穷尽与失败恢复
系统 SHALL 核对本次所有必要会话、parts、版本和alias，保留其他归属对象；物理失败或证据不足时 MUST 保留业务记录与删除意图。

#### Scenario: 提交终止竞争和版本清理
- **WHEN** part/Complete/Abort竞争、部分删除成功或出现delete marker
- **THEN** 以完整端点合同和对象证据重入，delete marker不当作payload清除；未穷尽时不完成业务清理

### Requirement: 产品接入真实验收
系统 SHALL 对最终adapter执行实际入口、隔离双库、故障恢复和独立审查，不以实验模型签发产品完成结论。

#### Scenario: 配置或权限变化
- **WHEN** 原端点不可达、权限撤销或当前配置切换
- **THEN** 保留原历史身份和义务，不改当前桶继续误删，恢复后按原证据重试并核账

```
