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
