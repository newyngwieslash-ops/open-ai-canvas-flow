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
