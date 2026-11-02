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
