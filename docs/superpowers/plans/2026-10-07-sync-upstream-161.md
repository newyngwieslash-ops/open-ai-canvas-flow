---
change: sync-upstream-161
design-doc: docs/superpowers/specs/2026-10-07-sync-upstream-161-design.md
base-ref: b355df6192a079158bf262501966f7c2106a15a9
---

# sync-upstream-161（上游 v1.6.1 融合）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以单个合并提交将上游 `ddcat-ai/open-ai-canvas` v1.6.1（`0b1d5073`）融入本仓库 main，完成 10 个双改文件裁决、后端/前端/迁移验证与文档同步，仅推送 origin。

**Architecture:** 方案 A（Design Doc 已确认）：从 main（`5f17fc92`）切出集成分支 `sync/upstream-1.6.1`，`git merge upstream/main` 三方合并，冲突按 Design Doc 裁决表逐文件处理为一个合并提交；验证通过后补文档提交，仅推 origin。历史保持永久连通。

**Tech Stack:** git（merge / update-ref / ls-remote）、Go 1.25 + GORM（迁移 v47、`go test`）、Bun + Vite + tsc（`bun run build` / `bun test`）。

## 全局约束（每个任务默认继承）

- 方案 A 唯一：`git merge upstream/main` 产生一个合并提交；不重写本地历史，不 format-patch，不 cherry-pick。
- 任何提交与分支只推送到 `origin`（`https://github.com/newyngwieslash-ops/open-ai-canvas-flow.git`）；`upstream`（`https://github.com/ddcat-ai/open-ai-canvas.git`）配置为仅 fetch，不得作为推送目标。
- 工具态产物不参与融合：`AGENTS.md` 的 Comet 注入块、`.zcode/`、`openspec/`、`docs/openspec/` 保持本地状态。已核实上游 `125864f6..0b1d5073` 未触碰这些路径。
- 集成全程不触碰 `feature/20261006/character-script-suggestions` 分支及其未提交产物；不执行 `git reset --hard`、`git checkout --`、宽范围删除；不使用 `git add -A` / `git add .` / `git commit -a`（脏文件只允许存在于工作区，绝不入暂存区）。
- 验证未通过不得推送；迁移目标为 v47（`upload_reservations`、`skill_curation`、`skill_curation_roots` 三张表）。
- 合并中途放弃用 `git merge --abort`；推送后回退用 `git revert -m 1 <merge-commit>`。
- 合并提交使用默认 message（`--no-edit`）；文档提交遵循仓库约定 `<type>(<scope>): <业务模块> - <变更摘要>`。
- 计划中"预期冲突 10 文件"是 Design Doc 的裁决表全集；实际冲突可能少于 10（部分文件 hunk 不同区域会被 git 自动合并）。凡自动合并的双改文件，仍须按对应任务的锚点清单核对双方内容并存。

## 事实基线（执行者必读，全部已核实）

- 当前分支 `feature/20261006/character-script-suggestions`，HEAD = `b355df61`（本计划 frontmatter 的 base-ref）；main = `5f17fc92`；上游头 `0b1d5073`（v1.6.1）；merge-base = `125864f6`。
- 本地**没有** `upstream` remote，也没有任何 `upstream/*` 跟踪引用；但上游全部对象已在本地对象库（深度 fetch 已完成），`git rev-parse 0b1d5073` 可解析。因此 Task 1 用 `git update-ref` 离线建立 `refs/remotes/upstream/main`，不依赖网络。
- 未提交脏文件共 5 项：`M AGENTS.md`、`M docs/openspec/changes/archive/2026-10-05-asset-image-binding/.comet.yaml`、`M docs/openspec/changes/archive/2026-10-05-fix-asset-sync-remote-conflict/.comet.yaml`、`?? .zcode/`、`?? openspec/`。已核实：三个被跟踪脏文件在 `HEAD` 与 `main` 内容完全一致（切换分支原样携带，无冲突）；`openspec/`、`.zcode/` 不在 main 树中（未跟踪，无碰撞）。
- 上游迁移列表（`backend/internal/database/migrations.go` @ `0b1d5073`）实际条目：v45 `upload_reservations`、v46 `skill_curation`、v47 `skill_curation_roots`，`CurrentSchemaVersion = 47`。
- 上游随合并新增的引用目标（`creation-login-dialog.tsx`、`skill-curation.mdx` 等）随合并一起进入，不单独处理。

## 文件结构（本次会改动/裁决的全部文件）

| 文件 | 操作 | 所属任务 |
| --- | --- | --- |
| `.gitignore` | 裁决：双方并存 | Task 3 |
| `docs/content/docs/backend/backend-database.mdx` | 裁决：双方并存（含 v47 说明） | Task 4 |
| `docs/content/docs/overview/features.mdx` | 裁决：双方并存 | Task 4 |
| `docs/content/docs/progress/pending-test.mdx` | 裁决：双方并存（5 个章节） | Task 4 |
| `web/src/components/layout/workspace-sidebar-nav.tsx` | 裁决：hunk 并存 | Task 5 |
| `web/src/pages/admin/components/admin-shell.tsx` | 裁决：hunk 并存 | Task 5 |
| `web/src/pages/create/index.tsx` | 裁决：hunk 并存 | Task 5 |
| `web/src/pages/welcome/index.tsx` | 裁决：**取本地动态 effect** | Task 5 |
| `web/src/services/user-data-sync.ts` | 裁决：hunk 并存（唯一语义交织区） | Task 6 |
| `web/src/stores/use-appearance-store.ts` | 裁决：hunk 并存 | Task 6 |
| `CHANGELOG.md` | 合并后追加融合事实 | Task 10 |

其余数百个上游新增/修改文件由合并自动带入，不逐个操作。

---

### Task 1: 分支准备与隔离检查（tasks.md 1.1、1.2）

**Files:** 无文件修改；只做 remote 配置与分支操作。

**Interfaces:**
- Consumes: 本地对象库中的上游提交 `0b1d5073`。
- Produces: `upstream` remote（仅 fetch）、`refs/remotes/upstream/main = 0b1d5073`、集成分支 `sync/upstream-1.6.1`（基于 main `5f17fc92`）。后续所有任务都在该分支上进行。

- [x] **Step 1: 确认起点状态**

```bash
git remote -v
git rev-parse --short main
git status --short
```

预期：remote 仅有 `origin` 两行；`main` 输出 `5f17fc92`；status 输出恰为事实基线列出的 5 项脏文件（3 M + 2 ??）。把 status 输出记录为隔离基线。若与预期不符，停止并报告，不继续。

- [x] **Step 2: 添加 fetch-only upstream remote 并离线建立跟踪引用**

```bash
git remote add upstream https://github.com/ddcat-ai/open-ai-canvas.git
git remote set-url --push upstream NO_PUSH_ALLOWED
git update-ref refs/remotes/upstream/main 0b1d5073
```

`set-url --push` 把推送地址改为非法占位符，使任何对 upstream 的意外推送直接失败（落实"仅 fetch、不得作为推送目标"）。`update-ref` 不走网络：对象已在本地对象库。

- [x] **Step 3: 校验 remote 与引用**

```bash
git remote -v
git rev-parse upstream/main
```

预期：`upstream` 的 fetch 行为 `https://github.com/ddcat-ai/open-ai-canvas.git`，push 行为 `NO_PUSH_ALLOWED`；`git rev-parse upstream/main` 输出以 `0b1d5073` 开头。

- [x] **Step 4: 切分支前碰撞预检**

```bash
git diff HEAD main -- AGENTS.md "docs/openspec/changes/archive/2026-10-05-asset-image-binding/.comet.yaml" "docs/openspec/changes/archive/2026-10-05-fix-asset-sync-remote-conflict/.comet.yaml"
git ls-tree main --name-only openspec/ .zcode/
```

预期：两条命令均无输出（脏文件跨分支同内容 → 切换安全；main 树中无 openspec/.zcode → 无碰撞）。

- [x] **Step 5: 从 main 切出集成分支**

```bash
git switch -c sync/upstream-1.6.1 main
git rev-parse --short HEAD
git status --short
```

预期：HEAD 为 `5f17fc92`；status 与 Step 1 记录的基线完全一致（5 项脏文件原样保留）。feature 分支未被动过。

---

### Task 2: 执行合并并记录实际冲突清单（tasks.md 2.1）

**Files:** 修改工作区（合并半途状态），尚无提交。

**Interfaces:**
- Consumes: Task 1 的 `upstream/main` 引用与集成分支。
- Produces: 处于冲突状态的合并会话 + 实际冲突文件清单（后续任务逐个消除）。合并提交在 Task 6 才产生。

- [x] **Step 1: 执行合并**

```bash
git merge --no-edit upstream/main
```

预期：输出 `Auto-merging ...` 多行后以 `Automatic merge failed; fix conflicts and then commit the result.` 结束，退出码非 0。若 git 直接完成合并（自动产生合并提交），后续任务的裁决步骤退化为纯核对：仍逐一执行 Task 3-6 中各文件的 rg 锚点校验，确认双改文件双方内容并存；`welcome/index.tsx` 若被自动合并，还需确认硬编码 title 已不存在且本地品牌 effect 存在。全部通过后直接进入 Task 7。

- [x] **Step 2: 记录实际冲突文件清单**

```bash
git diff --name-only --diff-filter=U
```

预期：输出为 Design Doc 裁决表的 10 个文件或其子集。将清单原样记录（写入执行记录/verification report）。**若出现 10 个之外的新冲突文件**：按 Design Doc"边界与错误处理"——逐文件重新核实双方意图、按"双方语义并存"同一原则裁决；语义交织无法并存的，记录到 verification report 并征询用户，不得擅自取单侧。

- [x] **Step 3: 确认放弃路径可用（仅记录，不执行）**

放课前命令：`git merge --abort`（回到 main 干净状态，脏文件不受影响）。仅在需要放弃本次合并时使用。

---

### Task 3: 裁决 `.gitignore`——双方条目并存（tasks.md 2.2）

**Files:**
- Modify: `.gitignore`（冲突区：文件尾 `.zocde/` 行附近）

**Interfaces:**
- Produces: 同时含上游 `skill-curation.mdx` 反忽略行与本地 Comet 状态块的 `.gitignore`。

- [x] **Step 1: 查看冲突标记**

```bash
git diff .gitignore
```

冲突成因：双方都改写了文件尾 `.zocde/` 行（上游去掉"无换行符"标记，本地在其后追加 Comet 块）；上游另一处改动在 `docs/content/docs/backend/*` 反忽略区。

- [x] **Step 2: 编辑为最终形态**

目标内容（两处并存，按行给出）：

1. 反忽略区保留上游新增行（位于 `docs/content/docs/backend/*` 之后、`!docs/content/docs/backend/task-runtime-stability.mdx` 之前）：

```gitignore
docs/content/docs/backend/*
!docs/content/docs/backend/skill-curation.mdx
!docs/content/docs/backend/task-runtime-stability.mdx
```

2. 文件尾保留本地 Comet 块（`.zocde/` 行无尾随空格、文件以换行结尾）：

```gitignore
.zocde/
# >>> Comet managed project state >>>
!/.comet/
/.comet/*
!/.comet/config.yaml
# <<< Comet managed project state <<<
```

- [x] **Step 3: 校验双方锚点并存**

```bash
rg -n "skill-curation.mdx" .gitignore
rg -n "Comet managed project state" .gitignore
```

预期：各至少 1 行输出。

- [x] **Step 4: 暂存**

```bash
git add .gitignore
```

---

### Task 4: 裁决 3 个文档 mdx——条目级并存（tasks.md 2.3）

**Files:**
- Modify: `docs/content/docs/backend/backend-database.mdx`
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`

**Interfaces:**
- Produces: 三个 mdx 均同时保留本地与上游章节；backend-database.mdx 的 v47 说明由上游"可选技能策展"章节承载。

- [x] **Step 1: `backend-database.mdx` 保留双方内容**

本地侧仅 +1 行（`asset_representations` 表说明，位于既有 schema 32 章节）；上游侧 +14 行：文件头 `# 后端数据库` 之后新增 `## 分片上传预留` 章节，文件尾新增 `## 可选技能策展` 章节。逐个消除冲突标记，结果为：

- 本地 `asset_representations` 行：保留原位。
- 上游 `## 分片上传预留`（Schema 45 `upload_reservations`）章节：保留在文件头区。
- 上游 `## 可选技能策展` 章节：保留在文件尾。该章节末两行即 tasks.md 2.3 要求的"v47 表说明"（"迁移 46 新增 skill_curation_settings…"、"迁移 47 依赖 46，新增 skill_curation_roots 与 skill_curation_root_assignments"），无需另写。

- [x] **Step 2: 校验 backend-database.mdx**

```bash
rg -n "asset_representations|## 分片上传预留|## 可选技能策展|skill_curation_roots" docs/content/docs/backend/backend-database.mdx
```

预期：4 个模式各至少 1 行。

- [x] **Step 3: `features.mdx` 保留双方内容**

本地 +2 行（资产图片绑定段，含 `PUT /projects/:id/assets/:assetId/representations` 与角色专属接口，约 192 行区）；上游 +6 行（文件尾 `## 平台技能策展` 章节）。两处不同区域，消除标记后并存。

```bash
rg -n "projects/:id/assets/:assetId/representations|## 平台技能策展" docs/content/docs/overview/features.mdx
```

预期：两个模式各至少 1 行。

- [x] **Step 4: `pending-test.mdx` 保留双方 5 个章节**

本地 +37 行（`## 场景/道具资产图片绑定（2026-10-05）`、`## 角色资产云同步误报"素材远端版本已变化"修复（2026-10-05）`）；上游 +35 行（`## 复制生图节点后节点消失`、`## Agent 视频首尾帧选择`、`## 上传内容与附件交付`）。双方都插在 `# 待测试` 标题之后的同一锚点，必然冲突。裁决：**5 个章节全部保留**，顺序沿用"本地两个章节在前、上游三个章节在后"（同为新章节，顺序不承载语义）。逐个消除冲突标记时注意保留每章的 `- [ ]` 清单行。

```bash
rg -n "^## " docs/content/docs/progress/pending-test.mdx | head -8
```

预期：前 6 行含 `## 场景/道具资产图片绑定`、`## 角色资产云同步误报`、`## 复制生图节点后节点消失`、`## Agent 视频首尾帧选择`、`## 上传内容与附件交付`（及原有旧章节）。

- [x] **Step 5: 暂存**

```bash
git add docs/content/docs/backend/backend-database.mdx docs/content/docs/overview/features.mdx docs/content/docs/progress/pending-test.mdx
```

---

### Task 5: 裁决 4 个 web 页面文件（tasks.md 2.4）

**Files:**
- Modify: `web/src/components/layout/workspace-sidebar-nav.tsx`
- Modify: `web/src/pages/admin/components/admin-shell.tsx`
- Modify: `web/src/pages/create/index.tsx`
- Modify: `web/src/pages/welcome/index.tsx`

**Interfaces:**
- Consumes: 本地 `@/lib/brand-display`（`displayBrandName` / `displayBrandTitle`，合并基线已存在）；上游随合并进入的 `./creation-login-dialog` 模块。
- Produces: 4 个文件同时保留本地品牌化逻辑与上游游客模式/技能分类/登录弹窗逻辑。

- [x] **Step 1: `workspace-sidebar-nav.tsx`——hunk 并存**

本地（13 行 import `displayBrandName`；96 行区 `brandLabel`；110-116 行品牌行用 `brandLabel`）与上游（70-73 行 `WorkspaceSidebarProfile` 登录入口→游客模式 `<div ... aria-label="游客模式">`）位于不同区域。若自动合并则仅核对；若有冲突标记，双方 hunk 全保留。

```bash
rg -n "displayBrandName|brandLabel|游客模式" web/src/components/layout/workspace-sidebar-nav.tsx
```

预期：三类锚点均有输出。

- [x] **Step 2: `admin-shell.tsx`——hunk 并存**

本地（import `displayBrandName`；`label={displayBrandName(appearance.brandName)}`）与上游（`adminNavigation` 平台配置组内新增 `{ path: "/admin/skill-curation", label: "技能分类", ... }`）不同区域，并存。

```bash
rg -n "displayBrandName\(appearance.brandName\)|/admin/skill-curation" web/src/pages/admin/components/admin-shell.tsx
```

预期：两个模式各至少 1 行。

- [x] **Step 3: `create/index.tsx`——hunk 并存**

本地（import `displayBrandName`；`const brandName = displayBrandName(useAppearanceStore((state) => state.appearance.brandName));`）与上游（import `CreationLoginDialog`；`userId`/`userSessionHydrated`/`conversationScope`/`loginDialogOpen` 状态；会话按用户 scope 隔离的两个 effect；提交前未登录拦截 `setLoginDialogOpen(true)`；JSX 尾部 `<CreationLoginDialog ...>`）不同区域，并存。上游用到的 `useUserStore` import 在合并基线已存在（25 行），无需补。

```bash
rg -n "displayBrandName\(useAppearanceStore|CreationLoginDialog|conversationScope" web/src/pages/create/index.tsx
```

预期：三类锚点均有输出。

- [x] **Step 4: `welcome/index.tsx`——取本地动态 effect（裁决表唯一"取单侧"项）**

双方删除同一行硬编码 title（`document.title = "影策 · 让一个故事从文字走向银幕";`）：上游只删这一行；本地删除后另建独立品牌 effect（`useEffect(() => { document.title = \`${brandName} · 让一个故事从文字走向银幕\`; }, [brandName])`），且本地同文件还有 story import 改造等上游未触碰的改动。裁决：**title 区取本地**——本地动态 effect 是上游"标题不硬编码"意图的更强实现，同时服务 og:title 分享卡；上游无其他独立改动需要保留。

操作：消除该文件冲突标记时，title 相关区段一律取本地版本（删除标记与上游侧残留，保留品牌 effect 与 `getWelcomeChapters(brandName)` 等本地改动）。

```bash
rg -n "getWelcomeChapters\(brandName\)" web/src/pages/welcome/index.tsx
rg -n "影策 · 让一个故事从文字走向银幕" web/src/pages/welcome/index.tsx
```

预期：第一条有输出；第二条无输出（硬编码 title 不存在）。

- [x] **Step 5: 暂存**

```bash
git add web/src/components/layout/workspace-sidebar-nav.tsx web/src/pages/admin/components/admin-shell.tsx web/src/pages/create/index.tsx web/src/pages/welcome/index.tsx
```

---

### Task 6: 裁决 `user-data-sync.ts` 与 `use-appearance-store.ts`，完成合并提交（tasks.md 2.5）

**Files:**
- Modify: `web/src/services/user-data-sync.ts`（唯一语义交织区：`saveRemoteUserDataBatch` 双方都重写）
- Modify: `web/src/stores/use-appearance-store.ts`

**Interfaces:**
- Consumes: 本地侧 `5f17fc92:web/src/services/user-data-sync.ts`、上游侧 `0b1d5073:web/src/services/user-data-sync.ts`（用 `git show <sha>:<path>` 提取任一侧原样代码）；上游的 `mergeThreeWayValue`（`agent-canvas-patch`）、`getGenerationAssetDefaults`（use-asset-store）、`canonicalize`、`resourceFileUrl`/`resourceIdFromStorageKey`（resources API）。
- Produces: 融合后的同步服务（同时具备上游"空缓存重建三方合并"与本地"角色基线认领 + 单素材冲突隔离"能力）与合并提交。

- [x] **Step 1: `use-appearance-store.ts`——hunk 并存**

本地（import `displayBrandName, displayBrandTitle`；`applyAppearanceMetadata` 全量改用展示名/title）与上游（`DEFAULT_PUBLIC_APPEARANCE` 与 `normalizePublicAppearance` 各加 `redeemPurchaseUrl`）位于不同函数，并存。

```bash
rg -n "redeemPurchaseUrl|displayBrandTitle" web/src/stores/use-appearance-store.ts
```

预期：两个模式均有输出。

- [x] **Step 2: `user-data-sync.ts`——import 区取上游**

上游改动的 import（新增 `import { canonicalize } from "json-canonicalize";`、`import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";`、use-asset-store 增加 `getGenerationAssetDefaults`、agent-canvas-patch 增加 `mergeThreeWayValue`）与本地无重叠，取上游侧。`ApiError` import 在合并基线已存在，保留。

- [x] **Step 3: `user-data-sync.ts`——本地 `acknowledgeRemoteAssets` 原样保留**

`acceptRemoteAssets` 之后的导出函数 `acknowledgeRemoteAssets(assets: Asset[])`（含完整中文注释）来自本地，原样保留。

- [x] **Step 4: `user-data-sync.ts`——`saveRemoteUserDataBatch` 区段按以下顺序拼装**

消除冲突后，函数内各区块顺序与来源（区块内部代码**逐字取对应侧**，不重写）：

1. `if (!dirtyProjects.length && !dirtyAssets.length) return;` 之后：**两组声明并存**——上游的 `adoptedAssetIds`/`mergedAssets`/`assetSyncEpoch` 三行 + 本地的 `conflictedAssetIds`/`conflictedAssetTitles` 两行。
2. **上游 pre-pass 整块**（空缓存重建三方合并：逐素材取远端、`content()` 投影、`mergeThreeWayValue`、`applyEdits`，以 `git show 0b1d5073:web/src/services/user-data-sync.ts` 对应区段为准），原样置于 `if (incrementalSession)` 之前。
3. **本地 incremental 块**（取 `git show 5f17fc92:...` 对应区段）：

```ts
    if (incrementalSession) {
        for (const source of dirtyAssets) {
            const baseline = acknowledgedAssets.get(source.id);
            if (!baseline || verifiedAssets.has(source.id)) continue;
            try {
                const { asset } = await getRemoteAsset(source.id);
                if (!remoteAssetConflictsWithBaseline(baseline, asset)) {
                    verifiedAssets.add(source.id);
                    continue;
                }
                if (!options.force) {
                    // 单个素材冲突只隔离该素材：继续校验其余素材，不在此中断整批同步。
                    conflictedAssetIds.add(source.id);
                    conflictedAssetTitles.set(source.id, source.title || source.id);
                    continue;
                }
                // 强制覆盖是用户显式指令：采纳远端版本为新基线后继续用本地内容覆盖。
                acknowledgedAssets.set(source.id, asset);
                verifiedAssets.add(source.id);
            } catch (error) {
                if (error instanceof ApiError && error.status === 404) {
                    // 服务端不存在该素材：本地为新增，交由下方 upsert 创建。
                    continue;
                }
                throw error;
            }
        }
    }
```

4. **upsert 循环为双方融合后的唯一新写代码**（上游的 adopted/merged 遍历骨架 + 本地的隔离跳过与 entity 跳过）：

```ts
    for (const dirty of dirtyAssets) {
        if (adoptedAssetIds.has(dirty.id)) continue;
        if (conflictedAssetIds.has(dirty.id)) continue;
        const source = mergedAssets.get(dirty.id) ?? dirty;
        if (source.kind === "entity") {
            // 角色 payload 由后端角色域接口（版本链）权威维护；前端列表投影缺少
            // representations 等完整字段，经通用覆盖接口会破坏服务端 payload，
            // 因此这里只对齐基线并告警，不发起覆盖请求。
            acknowledgedAssets.set(source.id, source);
            verifiedAssets.add(source.id);
            console.warn("角色素材由角色卡操作维护，跳过通用素材覆盖推送", source.id);
            continue;
        }
        const remotePayload = await ensureRemoteResourceReferences(assetForRemoteSync(source), uploaded);
        await upsertRemoteAsset(remotePayload);
        acknowledgedAssets.set(source.id, source);
        verifiedAssets.add(source.id);
    }
    if (conflictedAssetIds.size) {
        // 被隔离素材之外的素材与画布都已推送完成；此处收尾报告，避免静默丢失用户数据。
        const names = [...new Set(conflictedAssetTitles.values())].join("、");
        throw new Error(`素材远端版本已变化，已停止覆盖：${names}。其余内容已保存，请重新打开素材库后处理这些素材`);
    }
```

5. 之后的 `const errors: unknown[] = []` 画布/项目区块双方未同时改动，按无冲突合并结果保留。
6. 文件尾部两个 helper 并存：上游 `function assetRemoteVersionConflict()`（pre-pass 的 catch 引用它）+ 本地 `function remoteAssetConflictsWithBaseline(baseline, remote)`。

- [x] **Step 5: 提交前专项测试（针对交织区）**

```bash
cd web && bun test user-data-asset-sync
```

预期：全部通过（该测试覆盖 `acknowledgeRemoteAssets` 与冲突隔离逻辑，是交织区正确性的直接证据）。若失败，回 Step 4 核对拼装，不进入提交。

- [x] **Step 6: 完成合并提交**

```bash
cd ..
git add web/src/services/user-data-sync.ts web/src/stores/use-appearance-store.ts
git status --short
```

预期：status 显示 `All conflicts fixed but you are still merging` 语义（无 `UU` 残留），脏的工具态文件（AGENTS.md 等）**不在暂存区**。然后：

```bash
git commit --no-edit
git log --oneline -1
```

预期：提交 message 为 `Merge remote-tracking branch 'upstream/main'`（Design Doc 指定默认 message）。若 `git status` 仍有 `UU`，禁止 commit。

---

### Task 7: 后端验证——go test 与迁移 v47（tasks.md 3.1、3.3）

**Files:** 无修改。

**Interfaces:**
- Consumes: Task 6 的合并提交。
- Produces: 后端测试通过记录 + 迁移 v47 证据（进入 Task 11 推送的前置条件）。

- [x] **Step 1: 后端全量测试**

```bash
cd backend && go test ./...
```

预期：全部 PASS。迁移测试覆盖 v45–v47（`upload_reservations` / `skill_curation` / `skill_curation_roots`）。已知例外：若 `internal/app` 出现与本机临时 Redis 启动失败相关的预存环境失败（本地 pending-test.mdx 有记录），在 main 基线上跑同一包对比，确认非合并引入后才可继续，并把对比结果写入执行记录。

- [x] **Step 2: 迁移行为实证（有数据目录时）**

```bash
cd backend && CANVAS_BACKEND_DATA_DIR=../.local/project-workbench-debug go run ./cmd/server
```

启动日志确认 schema 迁移推进至 v47 后 Ctrl-C 退出。若本机无可用数据目录或启动受阻，以 Step 1 的迁移测试结果为准（Design Doc 测试策略允许二选一），在执行记录中注明采用的是哪条路径。

---

### Task 8: 前端验证——bun test 与 build（tasks.md 3.2）

**Files:** 无修改。

**Interfaces:**
- Consumes: Task 6 的合并提交（含交织区融合代码）。
- Produces: 前端构建通过记录（推送前置条件）。

- [x] **Step 1: 前端构建（tsc + vite，兜住 hunk 合并的 import/类型错误）**

```bash
cd web && bun run build
```

预期：tsc 无类型错误，vite 构建成功。

- [x] **Step 2: 交织区专项测试复核**

```bash
cd web && bun test user-data-asset-sync
```

预期：通过（Task 6 已跑过一次；此处作为全量构建后的最终确认，防止后续文件操作引入回归）。

---

### Task 9: 合并完整性核对（tasks.md 3.4）

**Files:** 无修改。

**Interfaces:**
- Produces: 双方历史完整性与本地业务改动无损的证据清单。

- [x] **Step 1: 合并提交的双方父提交**

```bash
git rev-parse --short HEAD^1
git rev-parse --short HEAD^2
```

预期：`5f17fc92`（本地 main）与 `0b1d5073`（上游 v1.6.1）。

- [x] **Step 2: 双方提交均在祖先链上**

```bash
git merge-base --is-ancestor main HEAD && echo OK-local
git merge-base --is-ancestor 0b1d5073 HEAD && echo OK-upstream
git merge-base --is-ancestor c98c9b39 HEAD && echo OK-c98c9b39
git merge-base --is-ancestor 853c7d2c HEAD && echo OK-853c7d2c
```

预期：四行 OK（后两条为 Design Doc 指定的抽查提交：角色资产同步修复、Midjourney 创作配置）。

- [x] **Step 3: 本地业务内容抽查**

```bash
rg -n "acknowledgeRemoteAssets" web/src/services/user-data-sync.ts
rg -in "midjourney" web/src/stores/use-config-store.ts backend/internal/app/model_capability.go | head -3
```

预期：第一条有输出（c98c9b39 的核心导出仍在）；第二条有输出（853c7d2c 的 Midjourney 创作配置仍在）。

---

### Task 10: CHANGELOG 与文档同步提交（tasks.md 4.1、4.2）

**Files:**
- Modify: `CHANGELOG.md`
- 复核（无新改动则跳过）：`docs/content/docs/backend/backend-database.mdx`、`docs/content/docs/overview/features.mdx`、`docs/content/docs/progress/pending-test.mdx`

**Interfaces:**
- Consumes: Task 4 已并存的 mdx；上游随合并带入的 `## v1.6.1` CHANGELOG 段。
- Produces: 记录融合事实的文档提交。

- [x] **Step 1: CHANGELOG 记录融合事实**

上游 v1.6.1 条目已随合并位于文件顶部。在其标题行下插入一行融合说明（保留原 BOM 与既有条目不动）：

```markdown
## v1.6.1

> 上游融合说明：本版本经集成分支 `sync/upstream-1.6.1` 以 `git merge upstream/main`（`0b1d5073`）并入本仓库；10 个双改文件按裁决表并存/取舍，数据库迁移升至 v47，提交仅推送本仓库 origin。

- 3D 预演台全面重构：…
```

（`- 3D 预演台…` 起为上游原有条目，仅示意位置，不改动其内容。）

- [x] **Step 2: 专题文档缺口复核**

```bash
git check-ignore -v docs/content/docs/backend/skill-curation.mdx
git status --short docs/content/docs/
```

预期：`check-ignore` 无输出（退出码 1，上游新文档未被忽略规则吞掉）；status 显示 skill-curation.mdx 等上游新文件已被合并跟踪。Task 4 已保证三个 mdx 双方并存；如复核发现 v47/技能策展描述缺口才补写，否则不加内容。

- [x] **Step 3: 提交**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): 上游同步 - 记录 v1.6.1 融合事实"
```

预期：工作区回到仅剩 5 项工具态脏文件的状态（3 M + 2 ??）。

---

### Task 11: 仅推送 origin 并核对远端（tasks.md 4.3）

**Files:** 无修改。

**Interfaces:**
- Consumes: Task 7/8 验证通过 + Task 9/10 完成记录。
- Produces: origin 上的 `sync/upstream-1.6.1` 分支；upstream 远端零推送。

- [x] **Step 1: 推送前置检查**

```bash
git remote -v
git log --oneline -2
```

预期：upstream push 仍为 `NO_PUSH_ALLOWED`；HEAD 为文档提交，其父为合并提交。Task 7、8 的验证记录均为通过——任一未通过则停止，不推送。

- [x] **Step 2: 推送**

```bash
git push origin sync/upstream-1.6.1
```

预期：输出显示 `newyngwieslash-ops/open-ai-canvas-flow` 上新建 `sync/upstream-1.6.1`。若需合入 main 后推送 main，须先经用户确认（tasks.md 4.3 括注），不在本计划默认路径内。

- [x] **Step 3: 远端核对**

```bash
git ls-remote origin refs/heads/sync/upstream-1.6.1
```

预期：输出一行，SHA 为本地 `sync/upstream-1.6.1` HEAD。可选（需网络，读-only）：`git ls-remote https://github.com/ddcat-ai/open-ai-canvas.git refs/heads/sync/upstream-1.6.1` 无输出，实证上游未收到任何推送；因 push URL 已被禁用，此步只作双保险。

---

## 回退

- 推送前：`git switch main && git branch -D sync/upstream-1.6.1`（合并会话中先 `git merge --abort`），main 与 feature 分支不受影响。
- 推送后：`git revert -m 1 <merge-commit>`，不重写历史。

## 自查记录（计划完成后执行）

1. **Spec 覆盖**：delta spec 4 条 Requirement → 合并提交方式（Task 2/6 + Task 9 场景证据）、工具态边界与文档并存（全局约束 + Task 3/4/5/6）、仅推 origin（Task 1 Step 2 + Task 11）、验证覆盖迁移与构建（Task 7/8）。tasks.md 14 项 → Task 1（1.1/1.2）、Task 2（2.1）、Task 3（2.2）、Task 4（2.3）、Task 5（2.4）、Task 6（2.5）、Task 7（3.1/3.3）、Task 8（3.2）、Task 9（3.4）、Task 10（4.1/4.2）、Task 11（4.3），无遗漏。
2. **Design Doc 裁决表**：10 文件逐文件落在 Task 3-6，welcome/index 为唯一取单侧项，其余并存——与裁决表一一对应。
3. **事实来源**：所有锚点、SHA、迁移条目、脏文件清单均来自本仓库只读核查（2026-10-07），未引入 Design Doc 之外的新需求。
