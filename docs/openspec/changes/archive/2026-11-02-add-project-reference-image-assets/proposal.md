# Proposal

## Why

项目角色、场景与道具需要统一的手动创建入口，并能把用户照片作为生成参考，而不是误当最终形象或三视图。第一版通过单参考、单输出和显式采用，使原图可追溯、候选可恢复，避免失败或并发操作覆盖现有资产。

## What Changes

- 统一新建 character、environment、prop，区分「直接使用成品」和「参考图生成」；支持单图本地上传/素材库选择、名称、文字要求及支持参考图的模型。
- 原图经后端 owned/ready/image 校验后保存在 `AssetVersion.DefinitionJSON.referenceImage.resourceId`，与成品表现分离。
- 现有图片任务增加显式 `manual` adoption 模式：成功只保存任务候选，用户确认后以基准版本 CAS 创建并绑定新版本；旧调用默认自动绑定。
- task center 提供候选读取与刷新恢复；已有资产重生成明确选择原图或当前成品；无参考保留文生图。
- 普通照片直用只写 `primary`，只有明确三视图才写 `turnaround_sheet`；角色编辑保留受保护字段并增强 CAS。
- 不新增数据库表列，不做自动识图、多图融合、批量或图片拆分。本次只规划，不实施、不提交、不归档。

## Capabilities

### New Capabilities

- `project-reference-image-assets`: 项目视觉实体的单参考图生命周期、生成候选及显式采用行为。

### Modified Capabilities

无。当前根 `openspec list --specs` 仅列出 `upstream-sync`，已读取且与本需求无关。另只读 `docs/openspec/specs/project-asset-image-binding/spec.md` 作为历史背景；它不属于当前根的规范清单，其分类/多图描述也与当前实现不同，不迁移或冒充当前根既有 delta。

## Impact

- 后端项目资产接口、角色与视觉资产版本事务、任务创建及完成/恢复路径、资源引用保护和模型能力校验。
- 前端真实项目资产页、编辑/预览、`services/api/projects.ts` 与 task center；业务调用继续使用统一 `http`。
- 复用 Asset、AssetVersion、AssetRepresentation、Task、Resource；新增手动实体创建、设置单参考、采用候选接口，无新表列或新依赖。
- 实现阶段同步 API、JSON 数据合同与待验证专题；本次仅创建 OpenSpec 和指定 superpowers 规划文件。
- 产物留在 `openspec/changes/add-project-reference-image-assets`；Comet state 仍位于 `docs/openspec/changes/.../.comet.yaml`，不移动、不改配置/state、不执行 guard。
