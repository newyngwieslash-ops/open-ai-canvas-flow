# Verification Report: add-character-turnaround-splitting

- **Change**: `add-character-turnaround-splitting`
- **Verify Mode**: `full`
- **Result**: `PASS`
- **Date**: `2026-11-03`

## 1. Automated Test & Build Evidence

- **Backend Commands**:
  ```bash
  go -C backend test ./internal/app -run TestCharacterTurnaroundSplit -v -count=1
  go -C backend test ./internal/repository -run TestUploadReservation -v -count=1
  ```
- **Backend Output Summary**:
  - `TestCharacterTurnaroundSplit_PlanAdmissionAndStrictDraftSchema`: `PASS (0.11s)`
    - 普通单人肖像角色（仅 `primary` 表现，无 `turnaround_sheet`）、版本冲突或非自有/未绑定资源作为拆分源均被 `ValidateTurnaroundSplitSource` 拒绝；
    - 视觉识图草稿 `ParseTurnaroundSplitPlanDraft` 严格拒绝未知字段（含伪造的 `confidence`）、无效标签、重复角色与越界坐标；
    - 缺视角或含人工说明的草稿标记 `RequiresManualReview = true`，且所有建议裁框不自动保存。
  - `TestCharacterTurnaroundSplit_PixelCropEXIFIdempotencyAndRepresentationMerge`: `PASS (0.08s)`
    - 未逐框确认（`Confirmed: false`）或存在重叠区域的裁框被拒绝；
    - 合法三视图拆分基于自有 `ready` 的 `turnaround_sheet` 原像素无损裁切 PNG（红/绿/蓝三区像素精确匹配），完整保留既有 `turnaround_sheet`、`primary`、`expression_sheet`、声音绑定与角色设定，并推进到新角色版本；
    - 同一 `IdempotencyKey` 重放返回原已完成回执（`Replayed: true`），同键异参返回 `409 turnaround_split_idempotency_conflict`，过期 `ExpectedVersionID` 返回 `409` CAS 冲突；
    - JPEG EXIF `Orientation = 6`（顺时针 90°）原图从 `100x300` 自动归一化为显示方向 `300x100`；
    - 源图 `turnaround_sheet` 与新裁出的 `front`、`side`、`back` 资源均受活跃引用保护，删除请求被安全拦截（`Blocked: 1`）。
  - Package results: `ok yingce/backend/internal/app 3.107s`, `ok yingce/backend/internal/repository 4.026s`.

- **Frontend Commands**:
  ```bash
  bun test --cwd web test/character-turnaround-split.test.tsx
  bun run --cwd web typecheck
  bun run --cwd web lint
  ```
- **Frontend Output Summary**:
  - `test/character-turnaround-split.test.tsx`: `6 pass, 0 fail, 26 expect() calls [62.00ms]`
    - 仅允许 `turnaround_sheet` 作为拆分源，拒绝普通单人肖像；
    - 拆分后角色默认封面与下游单图参考优先级保持 `turnaround_sheet -> primary -> front` 不变；
    - 预览 CSS 尺寸与源图整数像素坐标双向缩放与边界裁剪、键盘方向键移动/缩放微调、三框重叠与逐框确认门禁、视觉草稿不自动确认均验证通过。
  - `tsc --noEmit`: `exit 0`
  - `eslint src test`: `exit 0`

## 2. Code Review & Spec Coverage

- **Lightweight Code Review (`review_mode: standard`)**:
  - Checked `backend/internal/repository/character_turnaround_split.go`, `backend/internal/app/character_turnaround_split.go`, `backend/internal/handler/project_asset_routes.go`, `web/src/pages/projects/detail/character-turnaround-split.ts`, and `web/src/pages/projects/detail/character-turnaround-split-modal.tsx`:
    - No critical or important issues found. Resource staging uses `s.storeResource(..., forceLocal=true)` with cleanup on transaction error; CAS version check and idempotency receipt check prevent duplicate versions or partial writes; default reference priority (`turnaround_sheet -> primary -> front`) is preserved.
- **Task & Spec Coverage**:
  - All 10 tasks (`1.1`, `1.2`, `2.1`, `2.2`, `2.3`, `2.4`, `3.1`, `3.2`, `4.1`, `4.2`) in `docs/openspec/changes/add-character-turnaround-splitting/tasks.md` and `docs/superpowers/plans/2026-11-02-character-turnaround-splitting.md` are checked and verified.
  - Product documentation updated in `docs/content/docs/overview/features.mdx` and `docs/content/docs/progress/pending-test.mdx`.
