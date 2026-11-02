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
