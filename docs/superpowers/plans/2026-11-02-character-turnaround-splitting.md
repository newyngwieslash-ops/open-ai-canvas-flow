---
archived-with: 2026-11-03-add-character-turnaround-splitting
status: final
---
# Character Turnaround Splitting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task with TDD (`tdd_mode: tdd`).

**Goal:** 为项目角色三视图整图（`turnaround_sheet`）提供视觉识别草稿建议、人工逐框确认、服务端 PNG/JPEG 原像素无损裁切、保留既有表现与声音的幂等 CAS 版本提交，以及前端角色裁框审阅弹窗与下游默认引用回归。

**Architecture:** 在后端 `backend/internal/app/character_turnaround_split.go`、`backend/internal/repository/character_turnaround_split.go` 与 `backend/internal/handler/project_asset_routes.go` 中实现识别草稿准入与解析、PNG/JPEG + EXIF 原像素裁切、暂存输出与幂等 CAS 合并提交、引用保护与孤儿清理；在前端 `web/src/pages/projects/detail/character-turnaround-split.ts` 与 `character-turnaround-split-modal.tsx` 实现裁框预览、逐框确认、键盘微调与保存交互。

**Tech Stack:** Go 1.25 (`image`, `image/png`, `image/jpeg`), GORM, Gin, React 19, TypeScript, Bun Test.

---

## Task Checklist

- [x] 1.1 实现角色整图识别入口、来源/版本/权限及视觉能力准入；专项测试覆盖伪造源、普通人像、无视觉模型、多人/缺视角与取消，更新 HTTP API。 <!-- comet-task:11111111-1111-4111-8111-111111111101 -->
- [x] 1.2 实现标签/框 schema 和安全草稿投影；测试覆盖未知字段、非有限坐标、无效标签和不确定留空，验证不伪造置信度或自动保存。 <!-- comet-task:11111111-1111-4111-8111-111111111102 -->
- [x] 2.1 实现 owned ready 源资源读取、PNG/JPEG 头部/像素预算和方向一致化裁切；像素 fixture 测试覆盖缩放坐标、EXIF、边界、超限/格式伪装，证明没有重绘补画。 <!-- comet-task:11111111-1111-4111-8111-111111111201 -->
- [x] 2.2 实现幂等本地处理任务与暂存输出；故障注入测试覆盖重放、同键异参、取消/提交竞争和对象存储失败，同步任务/API 合同。 <!-- comet-task:11111111-1111-4111-8111-111111111202 -->
- [x] 2.3 实现完整表现合并及资源/版本 CAS 提交；SQLite/PostgreSQL 测试覆盖版本冲突、部分写失败、声音/定义/整图/primary/表情保留，验证旧整组接口不会丢表现。 <!-- comet-task:11111111-1111-4111-8111-111111111203 -->
- [x] 2.4 实现来源 metadata、历史引用保护及孤儿清理；资源测试验证源图有引用不删、无引用暂存可回收、物理删除失败保留记录，同步 backend-database 与资源说明。 <!-- comet-task:11111111-1111-4111-8111-111111111204 -->
- [x] 3.1 在真实角色详情接入原图预览、建议框、标签调整、键盘移动和逐框确认；组件测试覆盖模糊/多人/缺视角转人工、取消、未确认禁用和原像素转换。 <!-- comet-task:11111111-1111-4111-8111-111111111301 -->
- [x] 3.2 接入处理进度/刷新恢复/CAS/重放提示及保存后视角展示；组件/API 测试覆盖失败不半套显示，更新功能和 pending-test 文档。 <!-- comet-task:11111111-1111-4111-8111-111111111302 -->
- [x] 4.1 使用可控识图 mock 在真实页面完成框选→裁切→保存，检查明暗主题、滚动和键盘；像素对照证明输出来源，未获收费授权不得真实模型调用。 <!-- comet-task:11111111-1111-4111-8111-111111111401 -->
- [x] 4.2 回归默认 turnaround_sheet→primary→front 与下游单图输入，执行最小专项测试及必要类型/构建检查，记录实际证据和未验证项，不把静态页面当真实验收。 <!-- comet-task:11111111-1111-4111-8111-111111111402 -->

---

### Task 1: 识别草稿准入与严格 Schema 投影 (Tasks 1.1 & 1.2)

**Files:**
- Create: `backend/internal/app/character_turnaround_split.go`
- Create: `backend/internal/app/character_turnaround_split_test.go`
- Modify: `backend/internal/handler/project_asset_routes.go`
- Modify: `backend/internal/service/aliases_types.go`

### Task 2: 服务端 PNG/JPEG + EXIF 原像素裁切、幂等暂存、完整表现合并与引用保护 (Tasks 2.1, 2.2, 2.3 & 2.4)

**Files:**
- Create: `backend/internal/repository/character_turnaround_split.go`
- Modify: `backend/internal/app/character_turnaround_split.go`
- Modify: `backend/internal/app/character_turnaround_split_test.go`

### Task 3: 前端角色裁框组件、键盘微调、逐框确认与默认引用回归 (Tasks 3.1, 3.2, 4.1 & 4.2)

**Files:**
- Create: `web/src/pages/projects/detail/character-turnaround-split.ts`
- Create: `web/src/pages/projects/detail/character-turnaround-split-modal.tsx`
- Create: `web/test/character-turnaround-split.test.tsx`
- Modify: `web/src/services/api/projects.ts`
- Modify: `web/src/pages/projects/detail/assets.tsx`
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`
