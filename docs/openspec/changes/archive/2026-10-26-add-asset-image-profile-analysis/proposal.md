# Proposal

## Why

项目已有参考图保存与手动候选采用，但从图整理资产档案仍需手工录入。提供只描述可见内容的分析草稿，可减少录入工作而不把模型猜测变成资产事实。

## What Changes

- 对当前项目的角色、场景、道具参考图发起可取消、可追踪的多模态文本任务，服务端绑定来源和基准版本。
- 角色仅分析 appearance、physique、clothing、props；场景/道具仅 description、prompt。不确定内容留空或标为待确认，不生成伪造置信度。
- 展示字段差异，用户一次选择若干字段采用；服务端白名单校验、CAS 和幂等提交新版本，保留参考图、声音、表现及未选字段。
- 保护分析来源、任务和历史版本引用；保留已实现的删除意图、共享对象保护、生产者记录及状态栅栏。物理失败、部分完成或结算证据不足时保留业务记录和删除意图，不以超时、业务成功或缩小验收范围放行清理。
- 非目标：不猜姓名、性格、关系、声音；不从一张图自动创建多个资产；不自动覆盖，不以 Agent 为必要路径，不加入多参考或批量生成。

## 已批准范围收敛（2026-10-24）

用户已明确同意将完整生产者恢复、远端存储结算和旧数据维护移至后续 change，保留当前安全阻断及已完成成果。本项只要求分析来源引用保护与保守删除验收，不承诺每种生产者均可恢复或所有对象最终可删。未知写者、旧对象无证据、未结算 playback/远端义务仍拒绝物理清理及最终业务记录清理；这不是将原缺口判为通过。

- [完整生产者恢复](../../add-resource-producer-full-recovery/proposal.md)：Linux C2-A 真实 OS 能力、C2-B 产品接入及 Windows 全控制者丢失恢复。
- [远端存储物理结算](../../add-remote-storage-physical-settlement/proposal.md)：具名端点的 attempts、会话、版本及 parts 的可靠终态。
- [旧资源证据与维护](../../add-legacy-resource-evidence-maintenance/proposal.md)：可信 manifest、未知缩略图归属及受控维护。

上述三项为独立待办，引用仅用于追溯迁出范围，不纳入本次归档或提交；其实施与验收仍须各自授权。

既有 P1/P2/P3-B、Windows C1、媒体补验、C2-A 临时账本和设计/审查记录全部保留，不回滚、不扩大正常产品入口；后续 change 的环境、云端或旧库操作仍须具体审批。范围迁移与当前验收映射见 [.comet/scope-convergence.md](.comet/scope-convergence.md)。

## Capabilities

### New Capabilities
- `asset-image-profile-analysis`: 从受权资产参考图生成结构化可见档案草稿并按字段安全采用。

### Modified Capabilities
无。现有 `project-asset-image-binding` 描述成品绑定，本项不改变其行为。

## Impact

- 依赖首版 `add-project-reference-image-assets` 的原图/成品分离、版本与资源保护；首版仍处于 build，本项不归档或修改它。
- 涉及项目资产真实组件、projects API、文本任务创建/结果安全投影、app/repository 版本提交与资源引用。借鉴 `web/src/pages/canvas/use-canvas-media-tools.ts:981–1001`，不照搬画布客户端信任边界。
- HTTP API、JSON 数据合同和 pending-test 随实现同步；初始提案的“本轮仅规划”为历史阶段。当前按收敛合同完成最终 Verify，不操作业务库、不调用收费模型、不 push。
- 核心验收：合法分析→逐字段预览→手动采用；伪造字段/来源、越权、模型无视觉能力、取消/失败、旧版本及重放均安全失败或幂等返回。
- Open 阶段提案及识图技术方案已批准，主要实现及专项/真实页面受控 mock 证据已形成，正式设计见 design.md；最终 Verify 状态以 Comet Runtime 为准，不以文档范围修订宣称验收通过。
