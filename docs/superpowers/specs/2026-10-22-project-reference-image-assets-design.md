---
comet_change: add-project-reference-image-assets
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-add-project-reference-image-assets
status: final
---

# 项目参考图生成资产技术设计

本文件是简洁技术索引；规范与完整决策以 `docs/openspec/changes/add-project-reference-image-assets/specs/project-reference-image-assets/spec.md` 和同目录 `design.md` 为准。本次仅规划，未实现或完成运行验收。

## 架构与接口

- UI 在真实项目资产页统一 character/environment/prop 创建，区分直接使用成品和参考图生成，单图本地上传/库选择；名称必填，生成提供文字要求和模型选择。
- 新增 `POST /api/projects/:id/asset-entities`（仅 environment/prop，角色走现有 create）、`PUT /api/projects/:id/assets/:assetId/reference-image`、`POST /api/projects/:id/assets/:assetId/image-candidates/:taskId/adopt`，成功信封 data 为 `{asset}`。
- 角色 create 支持 referenceImage、imageResourceId、imageRole=portrait|turnaround_sheet（默认 portrait）及 folderId 原子关联。普通照片仅 primary，明确三视图才额外 turnaround_sheet。
- 原图只存 `AssetVersion.DefinitionJSON.referenceImage.resourceId`，摘要返回 referenceImage；后端校验 owned/ready/image，设置必须有 resourceId/expectedVersionId，本版不清除。角色编辑保留受保护字段，版本更新使用 CAS。
- 现有图片任务显式 adoptionMode=manual|auto，缺省 auto、未知值拒绝；创建阶段校验参考图模型能力，不静默 fallback。已有资产显式选择 original/current/none，单参考单输出。
- manual 完成、恢复、读取补偿仅保存候选；采用校验任务项目/目标/基准/归属/成功唯一结果，事务创建版本并 CAS 绑定，保留原图/声音/设定。重复采用幂等；冲突 HTTP 409、reason=project_asset_version_changed。

## 候选恢复

`GET /api/tasks?projectId=...&assetImageCandidates=true&assetId=...&pageSize=50&beforeTaskId=...` 返回 TaskSummary[]，增加 assetImageCandidate，previewUrl 预览；assetId 可选、游标为上一页末 ID。limit 前过滤，created_at DESC,id DESC keyset，少于 pageSize 结束；非法/越权游标与 activeOnly 组合拒绝。候选 state=pending_confirmation|adopted，adoptedVersionId 可选，不依赖弹窗内存。

## 安全与交付边界

复用资源、任务和版本事务，不增表列。覆盖历史 JSON 引用的删除保护与事务资源检查；跨账号/跨项目、伪造结果、双采用、编辑竞争、失败回滚、任务恢复及旧 auto 均需测试。第一版不做自动识图、多图融合、批量、拆分。

无 DDL 回填。回退不能直接启用不理解 manual 的旧 worker，防止候选自动绑定。规划与状态统一使用项目已配置的 docs/openspec 根，通过 comet classic openspec 包装命令及正常阶段 guard 推进；不自动提交或归档。文档校验不等于产品验证，全部真实页面/模型和数据库并发场景仍待执行。
