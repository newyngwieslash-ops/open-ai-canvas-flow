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
