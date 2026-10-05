# Proposal：场景/道具资产图片绑定

## Why

项目资产中只有角色（character）具备图片绑定能力（`asset_representations` + `PUT /characters/:assetId/representations` + 前端"替换图片"按钮）；场景（environment）与道具（prop）资产在候选确认时创建的是 `data.content: ""` 的空文本资产，既无法绑定图片，也被云端生成链路排除在图片参考之外。对以"角色/场景/道具一致性"为核心的短剧工作台而言，这是真实功能缺口，且 docs（features/http-api/backend-database/pending-test）均未记录该缺口。

## What Changes

- 后端新增项目资产通用图片绑定接口 `PUT /projects/:id/assets/:assetId/representations`：复用 `AssetRepresentation` 表与版本化写入（`SaveCharacterVersion` 的事务模式），服务角色（复用现有角色链路）之外的场景/道具/材料/其他分类；每次绑定生成新资产版本。
- 角色 representations 接口行为不变；新接口不接受 character 分类（避免绕过角色专属校验与声音/定义合并逻辑）。
- 前端资产预览弹窗"替换图片"按钮从仅 character 放宽到支持图片绑定的分类（environment/prop），复用现有"绑定图片"选择器 modal，非角色分类不要求三视图文案。
- 同步文档：`backend-database.mdx` 补 `asset_representations` 表记录；`features.mdx` 资产章节写明形象绑定覆盖范围；`pending-test.mdx` 记录待验收项。

## Capabilities

### New Capabilities

- `project-asset-image-binding`: 项目资产（非角色分类）图片绑定的服务端接口与前端入口行为。

### Modified Capabilities

（无既有 spec 覆盖此行为；角色链路 spec 不在本变更范围。）

## Impact

- `backend/internal/app/project_asset.go`（或新文件 `project_asset_representation.go`）：新增服务方法。
- `backend/internal/handler/project_asset_routes.go`：新增路由。
- `web/src/pages/projects/detail/assets.tsx`：预览弹窗按钮条件与绑定 mutation 放宽。
- `web/src/services/api/projects.ts`：新增 API 函数。
- `docs/content/docs/backend/backend-database.mdx`、`docs/content/docs/overview/features.mdx`、`docs/content/docs/progress/pending-test.mdx`：文档同步。
- 不改数据库 schema（`asset_representations` 表已存在）；不改生成链路（场景/道具参考进入分镜生成属后续独立变更）。
