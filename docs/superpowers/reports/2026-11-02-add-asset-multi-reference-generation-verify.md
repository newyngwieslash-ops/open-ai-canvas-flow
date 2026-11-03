# 同一资产多参考图版本化管理与单候选生成验证报告（`add-asset-multi-reference-generation`）

- **变更名称**：`add-asset-multi-reference-generation`
- **验证模式**：`full`（10 项任务、1 个新增能力 `asset-multi-reference-generation`、跨前后端与文档修改）
- **绑定分支**：`sync/upstream-1.6.3`
- **基准提交**：`e2f6656b802b9f76cac1e8a1b3ed691674045e8c`
- **关联设计文档**：`docs/superpowers/specs/2026-11-02-asset-multi-reference-generation-design.md`
- **关联实施计划**：`docs/superpowers/plans/2026-11-02-asset-multi-reference-generation.md`

---

## 1. 全量验证检查项汇总

| 序号 | 检查项 | 结果 | 证据摘要 |
| --- | --- | --- | --- |
| 1 | `tasks.md` 全部任务已完成（`[x]`） | **PASS** | `1.1`–`3.3` 共 10/10 项任务全部完成并通过 `comet-state.mjs sync-plan` 对齐 |
| 2 | 实现与 `openspec/changes/add-asset-multi-reference-generation/design.md` 高层决策一致 | **PASS** | 唯一 `referenceImages: [{ resourceId }]` 有序数组、`PUT /api/projects/:id/assets/:assetId/reference-images`、`min(16, maxImages)` 准入、按 `resourceId` 字典序稳定加锁、显式可重入迁移均已落地 |
| 3 | 实现与技术设计文档（`docs/superpowers/specs/2026-11-02-asset-multi-reference-generation-design.md`）一致 | **PASS** | 后端 `project_reference_image.go`、`project_image_candidate.go`、`repository/project_reference_image.go` 与前端 `projects.ts`、`project-reference-image.ts`、`project-asset-image-modal.tsx` 完全对齐设计文档 |
| 4 | Delta Spec（`specs/asset-multi-reference-generation/spec.md`）全部 6 项 Requirement / 11 个 Scenario 通过 | **PASS** | 见第 2 节逐场景验证矩阵 |
| 5 | `proposal.md` 目标全部达成 | **PASS** | 支持同一角色/场景/道具最多 16 张有序参考图保存、排序、移除、清空、按序生成单张候选与手动采用，且已完成 HTTP API、数据库 JSON 专题与 `pending-test.mdx` 同步 |
| 6 | Delta Spec 与设计文档无矛盾或漂移 | **PASS** | 零漂移，应用上限 `16`、迁移策略与排序交互均与确认设计一致 |
| 7 | `docs/superpowers/specs/` 下关联设计文档存在且可定位 | **PASS** | `docs/superpowers/specs/2026-11-02-asset-multi-reference-generation-design.md` |

---

## 2. 规格场景（Spec Scenarios）验证矩阵

| Requirement | Scenario | 验证测试与结果 |
| --- | --- | --- |
| 有序参考的持久化与权限 | 保存并恢复顺序 | `TestProjectReferenceImagesManageAndPreserveRepresentations`、`project-asset-image-modal.test.tsx`（多图追加、调整顺序、保存后恢复一致，保留成品与声音）：**PASS** |
| 有序参考的持久化与权限 | 非法集合与写竞争 | `TestProjectReferenceImagesRejectInvalidAndPreserveDefinition`（重复、越权、非 `ready`、超过 16 张、过期 `expectedVersionId` 409 拒绝，普通 `definition` 编辑无法篡改 `referenceImages`）：**PASS** |
| 模型能力与应用上限准入 | 不同模型限制 | `TestProjectImageGenerationEnforcesEffectiveReferenceMax`、`project-reference-image-assets.test.ts`、`project-asset-image-modal.test.tsx`（保存 3 张参考图但所选模型仅支持 1 张时保留已存参考图，前端禁用生成并给出明确提示，后端入队前拒绝）：**PASS** |
| 模型能力与应用上限准入 | 伪造能力 | `TestProjectImageGenerationRejectsClientForgedReferenceCapability`（客户端 `capabilityConfig` 宣称更高 `maxImages` 时仍按服务端可信协议上限拒绝）：**PASS** |
| 所选来源全量有序输入和单候选 | 多参考共同生成 | `TestProjectImageGenerationPreservesReferenceOrderInUpstreamPayload`、`project-asset-image-modal.test.tsx`（`original` 按保存顺序完整传入下游协议 payload，仅生成单张候选且需手动采用）：**PASS** |
| 所选来源全量有序输入和单候选 | 伪造来源或多输出参数 | `TestProjectImageGenerationRejectsMismatchedReferenceOrderOrCount`（客户端篡改参考图列表/顺序/URL 或请求多张输出时服务端拒绝）：**PASS** |
| 引用移除不等于资源删除 | 排序与删除并发 | `TestProjectReferenceImagesStableLockOrderAndDeletionProtection`、`TestProjectImageDeleteRechecksDirectUseAfterSnapshot`（按 `resourceId` 字典序升序加锁，历史版本、实体 payload 与任务输入引用阻止物理删除）：**PASS** |
| 首版单图显式迁移 | 已有单图与在途任务 | `TestMigrateLegacyProjectReferenceImagesIdempotentAndAnomalies`（单数 `referenceImage` 等价迁移为单元素 `referenceImages` 并移除旧键，幂等可重入，冲突双字段/损坏 JSON/缺失资源生成异常报告并阻断，既有候选基准仍可采用）：**PASS** |
| 失败取消与采用重放 | 取消或迟到完成 | `TestProjectImageMultiReferenceCancelAndFailurePreserveReferences`（取消或失败不回滚已保存 `referenceImages` 或当前成品）：**PASS** |
| 失败取消与采用重放 | 采用冲突和重放 | `TestProjectImageMultiReferenceAdoptCASAndIdempotency`、`project-asset-image-modal.test.tsx`（采用遇到 409 保留候选不覆盖；同任务幂等重放返回原版本不重复建版）：**PASS** |

---

## 3. 自动化测试与构建命令实跑记录

1. **后端专项与回归测试**：
   - 命令：`go -C backend test ./internal/app ./internal/handler -run "TestProject(Reference|Image|Profile|AssetBinding)|TestProfileAnalysis|TestRegisterCanvasAPI" -count=1`
   - 结果：`ok yingce/backend/internal/app 7.797s`、`ok yingce/backend/internal/handler 1.267s`（exit 0）
2. **前端专项与回归测试**：
   - 命令：`bun test --cwd web test/project-reference-image-api.test.ts test/project-reference-image-assets.test.ts test/project-asset-image-modal.test.tsx test/project-profile-analysis-modal.test.tsx`
   - 结果：`64 pass, 0 fail, 434 expect() calls`（exit 0）
3. **前端类型检查与 Lint**：
   - 命令：`bun run --cwd web typecheck && bun run --cwd web lint`
   - 结果：`tsc --noEmit` 与 `eslint src test` 全部通过（exit 0）

---

## 4. 结论

**验证结论：PASS**。全部 10 项任务、6 项规格要求与 11 个验收场景均已实现并通过前后端自动化验证，文档（`openapi.yaml`、`http-api.mdx`、`backend-database.mdx`、`features.mdx`、`pending-test.mdx`）已完整同步，可进入 `archive` 阶段。
