# Design

## Context

见 proposal.md — Why。角色链路现状：`ReplaceProjectCharacterRepresentations`（project_character.go:358）校验资源可用性 → `createNextCharacterVersion`（含声音/定义继承）→ `SaveCharacterVersion` 事务。非角色资产只有 `CreateProjectAssetVersion`（空 Draft 版本，不带资源）。`AssetRepresentation` 按版本存储（`AssetVersionID`），角色枚举 `validCharacterRepresentationRole` 含 `primary/front/side/back/turnaround_sheet/expression_sheet`。

## Goals / Non-Goals

**Goals:**
- 场景/道具（及材料/其他）项目资产可绑定图片并生成新版本。
- 前端预览弹窗对可绑定分类展示"替换图片"，复用既有选择器。

**Non-Goals:**
- 不改生成链路（场景/道具图进入分镜参考是后续独立变更）。
- 不改角色 representations 接口与角色卡 UI。
- 不改数据库 schema。

## Decisions

1. **新接口挂在资产路由而非放宽角色接口**：`PUT /projects/:id/assets/:assetId/representations`，服务方法 `ReplaceProjectAssetRepresentations`。角色请求仍走原接口——角色需要 `createNextCharacterVersion` 的定义/声音继承与 `kind: entity` payload 重写，与通用资产（仅版本号推进 + representations 替换）语义不同；在角色接口里做分类分支会把两条语义搅在一起。
2. **服务实现复用 `saveCharacterVersion` 事务**：新方法走 `SaveCharacterVersion`（projectID 传当前项目，事务内推 project revision），非角色资产版本 `definitionJSON` 沿用当前版本，payload 仅更新 `updatedAt`/`primaryVersionId`（保持 kind/category 不变）。角色分类请求直接 `BadAuthRequest("角色请使用角色形象接口")`。
3. **角色枚举不放宽**：非角色资产绑定固定使用 `primary` 角色（单图语义；`turnaround_sheet` 等视图角色只对角色有意义）。资源校验复用 `ReplaceProjectCharacterRepresentations` 的规则（image + ready + 1..8 张）。
4. **前端按 `asset.category` 分流**：`replaceable = category === "character" || category === "environment" || category === "prop"`；非角色绑定成功后用新版本号刷新列表，不写角色投影。

## Risks / Trade-offs

- [场景/道具绑定后列表预览展示依赖 `projectAssetSummary.StorageKey`（来自 payload.data.storageKey），新版本 payload 未写图] → 绑定接口同步把首张图资源写入 payload `data.storageKey`（与 `assetFromUploadedResource` 的 `resource:` 键一致），保证卡片预览与下载即时可见。
- [双入口（角色/通用）未来漂移] → 通用接口显式拒绝 character，注释指向角色接口；无共享分支逻辑需要同步。

## Migration Plan

纯增量接口，无数据迁移。旧场景/道具资产绑定一次即获得带图新版本。

## Open Questions

无。
