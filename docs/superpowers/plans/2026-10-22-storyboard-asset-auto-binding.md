---
change: storyboard-asset-auto-binding
design-doc: docs/superpowers/specs/2026-10-22-storyboard-asset-auto-binding-design.md
base-ref: c9656bc9ce9aa805f61e2e6107bbbe8e279231da
archived-with: 2026-11-02-storyboard-asset-auto-binding
---

# 分镜资产自动绑定实施计划

> **For agentic workers:** 执行时由主线程统一确认隔离方式、执行方式、TDD 与 review 策略，再选择 subagent-driven-development 或 executing-plans 逐任务执行。本文采用拟 TDD 的红—绿—整理步骤，不代表用户已选定 TDD，也不授权开始实施、启动服务或提交。

**Goal:** 新生成分镜通过结构化项目资产标识绑定有效版本；旧镜头通过零模型调用的预览确认，只追加引用，保护脚本、人工引用、历史产物及并发写入。

**Architecture:** 完整项目目录与工作区已引用资产分离。app 负责鉴权、确定性解析和事务编排，repository 提供快照、所有者协调锁与原子集合差写入；service 仅保持别名导入面。章节适配消费服务端解析结果，不改变其他共享画布调用方的合同。

**Tech Stack:** React 19 / TypeScript / Bun / TanStack Query，Go / Gin / GORM，文件型 SQLite 与 PostgreSQL。

执行状态由 OpenSpec 的 14 项任务及稳定 ID 维护；各节首行复映射同一任务，节内条目为验收要求，不作为第二套独立完成计数。

## 全局约束与执行准备

- 行为源为 `openspec/changes/storyboard-asset-auto-binding/specs/storyboard-asset-binding/spec.md`；技术依据为 frontmatter 指向的正式设计。任务编号一一对应根 `openspec/changes/storyboard-asset-auto-binding/tasks.md` 的 14 项，不修改该文件或其镜像勾选状态。
- 容量按已有与新增的**不同 assetVersionId**计数，最多 6；同版本多个 role/status/referenceId 全部保留并参与快照/后置比较，不把多角色行算作多个版本。此条采用主线程对正式设计第 6.3 节的修正。
- 预览去重后最多 100 个镜头；每镜提交最多 6 个不同版本；分页每页最多 80；模型摘要序列化 UTF-8 上限为 `24 * 1024` 字节，另服从上游完整 prompt 上下文限制。
- 不新建业务表、alias、向量索引、签名配置、幂等表、资源 deleting 状态或通用并发平台。无实际 alias 字段，不复用任意 definition 中的别名推测。只保留确定性规则，不增加匹配模型调用或自动媒体生成。
- 业务 API 仅使用 `web/src/services/api/request.ts` 的 `http`；写入失败上抛，按 `ApiError.reason/code` 分流，不解析 msg。接口失败返回真实 HTTP 失败状态与非零 code。
- 当前工作区已有大量修改，包括 `projects.ts`、项目资产/媒体、任务、资源删除、OpenAPI、文档及未跟踪文件；base-ref 仅记录 HEAD，**不是干净工作区声明**。执行前重新记录 `git status --short` 与涉及文件的 diff，逐块保留原修改，不覆盖、回滚或归为本次成果。已存在的未跟踪 `project_reference_image.go` 等也属于现有实现，不按新文件重建。
- 本计划只新增此文档。本次未运行测试、构建或规范校验；下文命令都是未来执行步骤及预期，不是已通过记录。不自动 git add/commit，不改 Comet state/guard，不据设计文档历史叙述推断当前状态。
- 不默认启动 dev server。主线程批准真实路由验收后，先核对端口与进程，后端使用 `CANVAS_BACKEND_DATA_DIR=../.local/project-workbench-debug`，前端 Bun；依赖安装不得用 npm/pnpm 覆盖 web 的 Bun 环境。
- PostgreSQL 当前可用性未知。沿用测试约定 `CANVAS_TEST_POSTGRES_DSN`，必须指向真实可用的隔离测试库。跳过、dry-run SQL 或 SQLite 通过均不能写成 PostgreSQL 并发通过；缺环境时记录阻塞并保留上线门禁。
- 下列文件标记“现有”表示已由设计或定向定位确认；“建议新增”表示尚未实现的本次文件。新接口、新类型、新测试名均为本计划约定，不冒充现有 API。

## 文件责任与依赖

| 边界 | 现有文件 | 建议新增文件及责任 |
| --- | --- | --- |
| 完整候选与章节适配 | `web/src/services/api/projects.ts`；`web/src/pages/projects/detail/project-chapter-ai.ts`；`web/src/pages/projects/detail/chapter-storyboard-production.ts` | `web/src/pages/projects/detail/chapter-asset-candidates.ts`：分页状态；`web/src/pages/projects/detail/chapter-asset-binding-drawer.tsx`：章节私有预览 UI |
| 真实页面 | `web/src/pages/projects/detail/workflow.tsx`；`web/src/pages/projects/detail/workflow-production-workbench.tsx`；`web/src/pages/projects/detail/workflow-stage-views.tsx`；`web/src/router.tsx` | 不新增路由、不新增独立 HTML；接入真实章节工作台 |
| 共用解析 | `backend/internal/app/project_shot.go`；`backend/internal/model/models_project.go` | `backend/internal/app/project_asset_binding_resolver.go`：确定性身份/证据；`backend/internal/app/project_asset_binding.go`：预览/确认编排与 DTO |
| 生成可信上下文 | `backend/internal/app/task_creation.go`；`backend/internal/app/provider.go`；`web/src/lib/canvas/canvas-project-domain.ts` | `backend/internal/app/project_asset_binding_generated.go`：已校验任务上下文、生成解析及保存复核；不把项目 ID 写入通用 nodeId |
| 快照与事务 | `backend/internal/repository/repository_projects.go`；`backend/internal/repository/repository_shots_workflows.go` | `backend/internal/repository/project_asset_binding.go`：读取快照与批量写；`backend/internal/repository/project_asset_binding_lock.go`：仅相关写方复用的协调锁 |
| HTTP 与错误 | `backend/internal/handler/project_unit_routes.go`；`backend/internal/service/aliases_types.go`；`backend/internal/kernel/errors.go`；`backend/internal/kernel/error_codes.go`；`web/src/services/api/error-codes.ts` | 沿既有文件增加类型别名和 reason，不在 handler 查库 |

建议执行顺序为 **1 → 2 → 3 → 6 → 7 → 4 → 5 → 8 → 9 → 10 → 11 → 12 → 13 → 14**。任务 4 的生成保存依赖任务 7 的事务协议，不能先发布无协调锁的写链路。任务 8、9 共同完成后才开放追加入口；任务 6 可以先交付独立只读预览。每项均有独立验收，但不得因顺序调整合并或漏掉原 14 项。

## 跨任务接口合同（全部为本次新增设计）

### HTTP 与页面 DTO

新增接口前缀为 `/api/projects/:id/units/:unitId/shot-asset-bindings`，前端 `http` 自动提供 `/api`，模块路径不得再加一次。

- `POST /preview`：`{ shotIds: string[] }` → `BindingPreview`，仅查询。
- `POST /append`：`{ baseline: BindingBaseline, selections: BindingSelection[] }` → `BindingAppendResult`。
- `POST /resolve-generated`：`{ taskId: string }` → `GeneratedBindingResolution`，仅查询真实已完成任务。

在 `projects.ts` 与 app DTO 中使用同名 JSON 字段；Go 类型导出并由 `aliases_types.go` 再导出。以下为精简合同，不是复制完整实现。

```ts
type BindingCategory = "character" | "environment" | "prop";
type BindingEvidence = {
  kind: "structured_id" | "exact_name" | "visual_label" | "structured_continuity" | "mention" | "speaker";
  field: string; start: number; end: number; text: string;
};
type BindingIssue = {
  kind: "invalid_id" | "category_mismatch" | "not_allowed" | "ambiguous" | "unmatched" |
    "missing_version" | "missing_image" | "capacity_exceeded" | "context_warning";
  message: string;
};
type BindingCandidate = {
  assetId: string; category: BindingCategory; name: string; assetVersionId: string | null;
  evidence: BindingEvidence[]; issues: BindingIssue[];
  bindable: boolean; missingImage: boolean; defaultSelected: boolean;
};
type BindingSelection = { shotId: string; assetVersionIds: string[] };
```

证据区间统一为原字段的 Unicode code-point 半开区间 `[start,end)`，Go 按 rune、页面用 `Array.from(text)` 截取；结构化证据没有文本区间时使用 `start=end=0` 与原字段名，不能生成虚假摘录。`message` 只用于展示，业务判断使用 kind/reason。名称仅 TrimSpace，唯一性相对完整项目三类目录计算。

基线类型固定为：

- `BindingBaseline = { projectId, unitId, projectRevision, shotIds, shots, catalog }`。
- `shots[] = { shotId, shotFingerprint, revisionFingerprint, references, artifacts }`。fingerprint 为服务端规范化相关完整字段后的 SHA-256；`references[] = { referenceId, assetVersionId, role, status }`；`artifacts[] = { artifactId, contentFingerprint, status, selected }`。
- `catalog[] = { assetId, assetFingerprint, linkFingerprint, versions }`；`versions[] = { assetVersionId, versionFingerprint, mediaFingerprint, bindable, missingImage }`。完整目录身份/名称/分类与所有已有、可选版本均纳入；无版本资产也保留 catalog 项及空 versions。所有数组稳定排序，禁止客户端漏项后仍按子集比较。
- shot fingerprint 包括稳定身份、project/unit、标题、描述、时长、排序、状态及 CurrentRevisionID；revision fingerprint 包括完整脚本字段。资产与版本 fingerprint 包括 owner、归属、状态、PrimaryVersionID、definition/prompt 等；媒体 fingerprint 包括表现 role/mediaType/resourceId/metadata、原始参考图及资源存在性/owner/存储身份。产物 contentFingerprint 包括 ID、版本、内容；status/selected 单列用于推导失效后置状态。
- 指纹用于比较，不是签名、授权或审批证据；不返回存储密钥、签名 URL 或敏感路径。服务端重新读取完整数据库状态计算对照值，不信任客户端宣称的权限或允许集合。
- `BindingPreview = { baseline, shots: Array<{ shotId, existingReferences, candidates: BindingCandidate[], issues: BindingIssue[], existingVersionCount, remainingCapacity }> }`；existingReferences 使用完整引用元组。
- `BindingAppendResult = { projectRevision, shots: Array<{ shotId, addedCount, unchanged, references, artifacts }> }`；返回提交后同事务获得的刷新数据，再失效对应 query 缓存。
- `GeneratedBindingResolution = { taskId, baseline: GeneratedBindingBaseline, rows: Array<{ rowIndex, candidates: BindingCandidate[], assetVersionIds: string[], issues: BindingIssue[] }> }`；`GeneratedBindingBaseline = { projectId, unitId, projectRevision, taskId, taskResultFingerprint, catalog }`。rowIndex 是服务端解析真实输出的零基序号；catalog 与上面的字段/排序一致。
- 既有 replace 输入新增可选 `generatedBinding: { taskId, baseline: GeneratedBindingBaseline, rowIndices: number[] }`，rowIndices 与 shots 同长度、一一对应、无重复。新章节生成必传，普通人工 replace 调用保持原合同；不能把该字段缺失当成自动生成路径跳过验证的授权。所选版本只能来自对应真实任务行解析的合法子集。

新增前端函数固定为 `previewProjectUnitShotAssetBindings(projectId, unitId, input, signal?)`、`appendProjectUnitShotAssetBindings(projectId, unitId, input)`、`resolveGeneratedProjectUnitShotAssetBindings(projectId, unitId, input, signal?)`，返回上述解包 DTO 的 Promise。确认网络失败保留原 input，不能自动重新预览并替换 baseline。

后端新增 app 方法为 `PreviewProjectUnitShotAssetBindings`、`AppendProjectUnitShotAssetBindings`、`ResolveGeneratedProjectUnitShotAssetBindings`；均接收 `ctx context.Context, userID, projectID, unitID string` 及对应 request DTO，返回对应 result DTO 与 error。handler 仅绑定参数、传登录上下文、响应。

### 所有者事务与集合合同

建议新增 repository 方法 `WithStoryboardAssetOwners(ctx context.Context, ownerIDs []string, fn func(*Repository) error) error`，在新事务里先取协调锁，fn 只接收事务绑定 Repository。锁外发现 scope/owner，锁内重新发现；owner 扩大则完整回滚，重新排序发现/取锁，最多重启 2 次，仍不稳定返回 conflict。不得在已有对象行锁事务内部再调用该入口或隐式启动嵌套事务。

PostgreSQL 对排序去重后的逻辑 ownerID 派生 `SHA-256("storyboard-assets:" + ownerID)`，取前 8 字节大端转换为有符号 int64，调用 `pg_advisory_xact_lock`；排序按逻辑 ownerID 而非 hash。SQLite 在第一次事务读取前执行 `UPDATE assets SET id = id WHERE 1 = 0`，不触及 upload 表。之后按项目、章节、镜头、项目资产关联、资产、版本、表现/资源、引用/产物的固定表序与主键序取得必要行锁。

`B` 是完整原引用元组；所选版本若已在 B，保留其所有旧行，不改 role/status、不另加 role。`D` 仅为尚未存在版本按既有视觉引用规则生成的元组；容量为 `distinct(versionId(B ∪ D))`。旧 referenceId 不变，新行 ID 由服务器/数据库现有机制生成，不以客户端预测 ID 作后置要求。只对 D 非空镜头失效，整批项目 revision 最多 CAS 递增一次；全 D 空为零业务写入。严格后置比较包括脚本、全部目录/媒体、完整引用 B∪D、精确 revision 及一次失效后的完整产物状态；不能用“包含所选 ID”替代。

## 任务 1（1.1）：独立完整分页候选

- [x] 任务 1（1.1）：独立完整分页候选 <!-- comet-task:4d197f51-c532-4c68-a439-df34a51778d4 -->

**文件：** 修改现有 `web/src/services/api/projects.ts`、`web/src/pages/projects/detail/project-chapter-ai.ts`、`web/src/pages/projects/detail/chapter-storyboard-production.ts`；建议新增 `web/src/pages/projects/detail/chapter-asset-candidates.ts`、`web/test/chapter-asset-candidates.test.ts`。参考现有 `web/test/project-chapter-storyboard.test.ts`。

**接口：** 新增 `loadChapterAssetCandidates(projectId: string, signal?: AbortSignal): Promise<ProjectAsset[]>`，使用现有 `ProjectAsset` / `listProjectAssetsPage`；为后者补可选 AbortSignal 而不破坏当前 options。页面维护 loading/complete/error/cancelled 状态，只有 promise 完整完成可进入 complete。

- 先写候选测试：零引用章节已有三类资产；81 项跨页；超过 60 项；重复资产 ID；末页错误；中途取消；无版本资产仍展示。断言请求每页 ≤80、持续到 hasMore=false，失败/取消不返回部分成功。
- 拟红阶段运行下方命令，失败必须是新增断言对应行为缺失，不把依赖故障当红测试。
- 逐页读取完整项目目录、按 ID 去重后过滤三类；不从 workspace.assets 构造全集，也不改变 `ProjectUnitAssets`。将取消原样上抛，不缓存“已完成”的部分数据。
- 接入章节生成加载状态；复用页面现有用户 scope 的 Query 约定，不把媒体对象写入 localStorage。
- 拟绿阶段复跑测试并检查原章节测试；验收为无引用章节也可发现全部三类、任何中断明确不完整。

未来命令（web 目录）：`bun test test/chapter-asset-candidates.test.ts test/project-chapter-storyboard.test.ts`。预期专项断言全部通过。

## 任务 2（1.2）：共用确定性解析与证据分类

- [x] 任务 2（1.2）：共用确定性解析与证据分类 <!-- comet-task:b46557d1-69b3-494c-95af-73e729018aab -->

**文件：** 建议新增 `backend/internal/app/project_asset_binding_resolver.go`、`backend/internal/app/project_asset_binding_resolver_test.go`、`backend/internal/app/project_asset_binding.go`（先定义共用 DTO）；读取现有 `backend/internal/model/models_project.go`、`backend/internal/app/project_shot.go`。不修改模型表。

**接口：** 新增纯函数 `resolveStoryboardAssetReference`，输入一个 `{assetId?, category, name?}`、完整候选快照和可选 allowedIds，输出 BindingCandidate；allowedIds 只允许来自服务端任务上下文。新增 `extractStoryboardAssetEvidence`，输入镜头 description、当前 revision 的正向字段与名称索引，输出带原文字段/区间的候选证据。两者都不读库、不请求模型。

- 表驱动用例写明：合法 ID；非法 ID+合法名仍拒绝；跨项目；类型不符；完整目录同名；仅 trim 的精确名称；缺版本；有效版本缺图；5+2 超限；同版本两 role 只占一个版本但两行均保留。
- 增加中文用例“桑若视线落在领口……裴让停止喝粥……”→两个 mention、不默认选中、不产生服装/碗勺；“桑若：裴让来了”→speaker/mention、不认定视觉在场；重叠长短姓名/同名→ambiguous；negativePrompt 不产生候选；否定/回忆/画外音保留 context_warning。
- 实现 ID 优先且非法 ID 不名称兜底；名称唯一性在完整目录同类别中判断。无 alias 推断，不调用现有候选创建流程里的别名 helper。
- 视觉默认选择只接受合法结构化 ID、明确类别标签或可追溯结构化连续性；普通文字身份唯一仍不默认选择。明确建议整组超容量时全部保持待选择，不能任取前几个。
- 运行拟红/绿命令；断言无证据不复制上一镜场景，缺图可绑定但绝不伪造图片。

未来命令（backend 目录）：`go test ./internal/app -run '^TestStoryboardAssetResolver' -count=1`。新增测试均采用该前缀；预期枚举用例全部执行通过。

## 任务 3（1.3）：摘要进入真实 prompt 与可信任务上下文

- [x] 任务 3（1.3）：摘要进入真实 prompt 与可信任务上下文 <!-- comet-task:0fd6e46a-0b93-448e-9db5-80f28bec0882 -->

**文件：** 修改现有 `backend/internal/app/task_creation.go`、`backend/internal/app/provider.go`、`web/src/pages/projects/detail/project-chapter-ai.ts`；建议新增 `backend/internal/app/project_asset_binding_generated.go`、`backend/internal/app/project_asset_binding_prompt_test.go`。保留 `web/src/lib/canvas/canvas-project-domain.ts` 共享合同；在章节私有类型中扩展。

**接口：** 章节请求新增 `chapterAssetContext: { projectId, unitId, scope: { kind: "all" | "selected", assetIds: string[] } }`；all 要求空 assetIds，selected 要求非空合法 IDs。后端保存经过校验的 `{projectId, unitId, operation, summary, allowedAssetIds, totalCount, sentCount, scope}` 到原任务非敏感输入；operation 与现有章节分镜 operation 取值保持一致，不新造任务系统。

- 请求捕获测试构造 61/81 候选、中文多字节 24576 字节边界/超一字节、selected 隐藏同名、外部项目 scope、没有章节上下文的共享画布请求。
- 后端在创建原生成任务时鉴权并重读真实目录，只接受明确用户范围或已有结构化章节范围；集中常量 `storyboardAssetSummaryMaxBytes = 24 * 1024`。摘要只含 assetId/category/name，不包含媒体正文或 URL。
- 捕获实际 provider 出站请求，保证 prompt 的摘要与持久任务允许集合源自同一次校验；完整 prompt 的模型上下文预算不足时阻止提交并返回清晰问题，不能把字节预算当 token 预算。沿实际模型限制消费链检查，不另建 tokenizer 平台。
- 前端展示总数/发送数/缩减依据；超额无明确可用范围时阻止生成，不自动截前 60/80 项。服务端拒绝客户端直接提交 summary/allowedIds 冒充可信上下文。
- 拟红/绿复跑并记录共享画布无上下文时 payload 不变，仍只有一次原有生成任务。

未来命令（backend）：`go test ./internal/app -run '^TestStoryboardAssetPrompt' -count=1`。前端回归（web）：`bun test test/canvas-storyboard-output-contract.test.ts test/canvas-storyboard-context.test.ts test/project-chapter-storyboard.test.ts`。

## 任务 4（1.4）：生成解析与新镜头保存

- [x] 任务 4（1.4）：生成解析与新镜头保存 <!-- comet-task:8ff8ca0b-e8db-4f53-8cb7-45d75468e546 -->

**依赖：** 任务 2、3、7；不绕过任务 7 锁协议提前放行保存。

**文件：** 修改现有 `web/src/pages/projects/detail/project-chapter-ai.ts`、`web/src/pages/projects/detail/chapter-storyboard-production.ts`、`web/src/services/api/projects.ts`、`backend/internal/app/project_shot.go`、`backend/internal/handler/project_unit_routes.go`、`backend/internal/service/aliases_types.go`、`backend/internal/repository/repository_shots_workflows.go`；扩充建议新增的 `project_asset_binding_generated.go`；建议新增 `backend/internal/app/project_asset_binding_generated_test.go`、`web/test/chapter-generated-asset-binding.test.ts`。

**接口：** 使用跨任务合同的 resolve-generated、GeneratedBindingResolution 和 replace.generatedBinding。章节行输出新增 `projectAssetRefs: Array<{assetId?: string, category: BindingCategory, name?: string}>`；首选 assetId，name 仅缺 ID 时处理。模型不得选 assetVersionId。

- 先写首次章节三类引用落库测试；输入假 allowedIds、另一用户任务、另一 unit/operation、未完成任务、输出合法名字搭非法 ID、返回后版本切换、回传篡改版本、重复/越界 rowIndex 均拒绝且旧镜头不被替换。
- 生成调用保留原 taskId；解析读取服务端已完成任务真实结果，验证 task owner/project/unit/operation 并读取当前有效版本。问题全部展示，不静默丢弃；明确有效且容量合法的引用默认纳入新镜头。
- 更新章节 adapter 消费服务端每行 assetVersionIds，不再从 `assetBindings.nodeId` 推导项目版本。保持共享 StoryboardRow 的其他调用方不变，不在画布通用 nodeId 填项目 assetId。
- 保存走既有 `replaceProjectUnitShots`，继续传 expectedShotIds；在同一保存事务里复核任务、行关联、解析基线、当前权限/目录/媒体及所选合法子集，然后才删除/创建镜头。客户端 resolve 响应不构成授权。
- 捕获下游图像/视频输入，断言来自持久化版本的正确参考图；有效版本缺图只传真实可用图片并持续提示。非生成的既有人工 replace 不被强制依赖 taskId。
- 拟红/绿运行后检查任务恢复路径仍保留 taskId、不会重复创建生成任务。

未来命令（backend）：`go test ./internal/app -run '^TestStoryboardAssetGenerated' -count=1`；（web）：`bun test test/chapter-generated-asset-binding.test.ts test/project-chapter-task-recovery.test.ts test/canvas-storyboard-output-contract.test.ts`。

## 任务 5（1.5）：生成与解析文档同步

- [x] 任务 5（1.5）：生成与解析文档同步 <!-- comet-task:51bb7643-fbf8-4826-8082-17c482d02161 -->

**文件（现有）：** `docs/content/docs/overview/features.mdx`、`docs/content/docs/backend/code-map.mdx`、`docs/content/docs/progress/pending-test.mdx`、`docs/content/docs/backend/http-api.mdx`、`backend/internal/handler/openapi.yaml`。

- 对照任务 1—4 的实际 DTO、路由及测试记录，记录完整目录、24 KiB 与完整上下文双重限制、projectAssetRefs、真实任务绑定及名称边界。
- 为 resolve-generated 和 replace 的 generatedBinding 编写 OpenAPI schema、只读说明及有效/无效引用示例；明确 taskId 不可替代当前鉴权。
- 在 pending-test 保留尚未用户验收及环境未验证项，不把拟测试写成结果；不将已有文档差异覆盖成此次版本。
- 进行合同逐项检查：JSON 命名、必填项、数量约束、错误状态、共享画布不变、无 schema 迁移。结果以实际文件 diff 与专项证据核对，不进行无关重写。

未来静态检查命令（仓库根）：`git diff --check -- docs/content/docs/overview/features.mdx docs/content/docs/backend/code-map.mdx docs/content/docs/progress/pending-test.mdx docs/content/docs/backend/http-api.mdx backend/internal/handler/openapi.yaml`。此命令只检查差异空白，不作为接口运行通过证据。

## 任务 6（2.1）：一致快照的零写入批量预览

- [x] 任务 6（2.1）：一致快照的零写入批量预览 <!-- comet-task:c3f5c071-2e04-4f5b-bef7-3814b7d538fe -->

**文件：** 扩充建议新增 `backend/internal/app/project_asset_binding.go`；建议新增 `backend/internal/repository/project_asset_binding.go`、`backend/internal/app/project_asset_binding_preview_test.go`；修改现有 `backend/internal/handler/project_unit_routes.go`、`backend/internal/service/aliases_types.go`、`web/src/services/api/projects.ts`。

**接口：** 实现 preview 与 BindingBaseline。repository 提供同一事务的完整目录/目标镜头/版本媒体/产物读取；app 在事务快照数据上调用任务 2 的纯解析。

- 测试 0/空 ID/101 镜头、重复去重后 ≤100、跨 unit/用户镜头；合法批次返回全部原引用，包括同版本不同 role/status，不能只用版本 ID 构造基线。
- PostgreSQL 使用只读一致快照事务；SQLite 使用只读快照事务。预览绝不调用写事务协调 helper，不通过无匹配 UPDATE 抢写权。
- 从同一快照读取目录、直接 Shot 字段、完整 revision、引用、可选及既有版本、表现/原始图/资源、历史产物。按跨任务合同排序/规范化并计算指纹；加入一个未选候选变动用例证明其也会影响基线。
- 记录预览前后表状态、项目 revision 与任务数量，断言均不变；注入模型/媒体调用计数器并要求为零。测试关闭请求的 context 取消不会留下写入。
- 输出证据、问题、已有不同版本数及剩余容量；已有数据自身 >6 不静默修复，标记不能确认。
- 拟红/绿运行，覆盖“不改 CurrentRevisionID 直接改 description”也会改变 fingerprint。

未来命令（backend）：`go test ./internal/app -run '^TestStoryboardAssetPreview' -count=1`。

## 任务 7（2.2）：所有者协调锁与直接竞争写方

- [x] 任务 7（2.2）：所有者协调锁与直接竞争写方 <!-- comet-task:11ff978f-e73f-40cf-815a-21b9700b489c -->

**文件：** 建议新增 `backend/internal/repository/project_asset_binding_lock.go`、`backend/internal/repository/project_asset_binding_lock_test.go`、`backend/internal/app/project_asset_binding_concurrency_test.go`；修改下表中的现有事务入口。SQLite 写权机制参考现有 `backend/internal/repository/upload_reservations.go`，但不改/调用 upload 表流程。

**接口：** 实现 `WithStoryboardAssetOwners` 及“协调锁先于行锁”的所有写方协议。所有者由数据库对象关系派生，不用当前管理员 ID 替代对象 owner。锁后重新鉴权/枚举共享资产所有关联项目；不得只锁发起项目。

| 直接竞争入口 | 现有文件与已定位锚点 | 必须纳入的事务边界 |
| --- | --- | --- |
| 脚本、直接 Shot 字段、当前 revision | `backend/internal/app/project_shot.go`；`backend/internal/repository/repository_shots_workflows.go` 的 SaveShot、SaveShotWithRevision | 取锁后读取/检查/修改，同一事务更新版本与失效 |
| 引用增删、role/status | 同上 repository 的 UpsertShotAssetReference、UpsertShotAssetReferenceAndInvalidate、DeleteShotAssetReferenceAndInvalidate | 所有真实调用方共用外层锁事务，不在无锁小 helper 留后门 |
| 新章 replace、镜头/章节/项目删除 | 同上 repository 的 ReplaceProjectUnitShots、DeleteProjectShot；`backend/internal/repository/repository_projects.go` 的 DeleteProjectUnit、DeleteProject | 目标集合发现、复查、替换/删除及 revision 全部持锁 |
| 项目关联增删、身份/分类 | `backend/internal/app/project_asset.go`；`backend/internal/repository/repository_projects.go` 的 LinkProjectAsset、DeleteProjectAssetLink、ProjectAssetShotReferenceCount、UpdateAssetDomain | 引用保护检查与解除关联必须在同一事务；插入不存在的关联也遵循 owner 锁 |
| 有效版本/内容/表现更新 | `backend/internal/app/project_character.go`、`project_asset_representation.go`；`backend/internal/repository/project_character.go` 的 SaveCharacterVersion、ConfirmProjectCharacterCandidate；`repository_projects.go` 的 CreateAssetVersion | 版本/身份/表现变更覆盖所有关联项目并沿既有语义失效/更新 revision |
| 原始图、候选采纳、媒体正式落库 | `backend/internal/app/project_reference_image.go`、`project_asset_image.go`、`project_workflow.go`；`backend/internal/repository/project_reference_image.go` 的 CommitAssetReferenceVersion；`project_asset_image.go` 的 CommitProjectAssetImage；`repository_shots_workflows.go` 的 RegisterWorkflowTaskOutput | 仅数据库正式采纳阶段取锁；媒体生成、网络读取不持锁 |
| 产物新增、失效、selected 变更 | `backend/internal/app/project_workflow.go`；`backend/internal/repository/repository_shots_workflows.go` 的 CreateShotArtifact、MarkShotArtifactsStale、RegisterWorkflowTaskOutput | 包括后续重新选中/新增产物，阻止严格后置检查与实际写入间的窗口 |
| 资源删除、清理、管理员删除、Outbox | `backend/internal/app/resource_delete.go`；`backend/internal/repository/resource_cleanup.go`、`admin_storage_delete.go`、`resource_reference.go`；读取 `backend/internal/app/resource_deletion_worker.go`、`backend/internal/repository/resource_deletion.go` | 数据库引用复查/记录删除/ResourceDeletionJob 同事务；worker 物理删除与重试保持锁外，purge/清空回收站同样遵守当前引用保护 |
| 迟到项目设置保存 | `backend/internal/repository/repository_projects.go` 的 UpdateProject；`backend/internal/app/project.go` | 移除旧 Project.Revision 的绝对回写，改数据库递增或原值 CAS；无关设置不新增 owner 锁 |

- 先建立真实双连接屏障测试，参照现有 `backend/internal/repository/canvas_revision_test.go` 的隔离库约定；SQLite 必须是文件数据库而非每连接私有内存库。屏障使用通道/数据库状态控制，不靠 sleep 猜时序。
- 实现 SHA-256 固定 key、逻辑 owner 稳定排序和 SQLite 首读前写权限；记录 SQL 顺序断言协调锁在任何行锁前。忙等待/死锁使用有界超时与 503，回滚所有写入。
- 逐行修改清单中的实际事务边界，锁内重读对象所有者/权限/项目关联。新发现 owner 时回滚再重启，绝不边持行锁边补更早 owner 锁。共享资产按所有关联项目锁序更新。
- 对每类写入口测试两种顺序：竞争方先提交则确认冲突；确认先提交则竞争方看到新增引用并按原保护规则裁决。管理员操作锁被操作 owner；多 owner 输入反序不形成反向锁等待；不同 owner 的 PostgreSQL 操作不被新增全局锁串行化。
- 资源测试覆盖关联/媒体写入与数据库删除/Outbox 的先后顺序、版本仍有效但图片记录消失、共享物理对象、显式 purge 有引用拒绝及无引用安全准入、本地 worker 失败重试及已入队远端作业证据不足时的实际 worker 拒绝/保留/重试（完整远端清理闭环由现有远端物理结算后续变更验收）。分别断言数据库状态与外部对象可访问性，不混为一谈。
- 验证晚到设置请求不降低 revision，检查无无锁 SQL 旁路；每一行入口都有对应屏障 case 才可完成本任务。只找到 helper 或单接口事务不算完成。

未来命令（backend）：`go test ./internal/repository ./internal/app -run '^TestStoryboardAsset(OwnerLock|Concurrency|ResourceRace|RevisionMonotonic)' -count=1 -v`。

PostgreSQL 门禁命令示例（已由主线程提供隔离 DSN 后，在 backend 执行）：`test -n "$CANVAS_TEST_POSTGRES_DSN" && go test ./internal/repository ./internal/app -run '^TestStoryboardAsset(OwnerLock|Concurrency|ResourceRace|RevisionMonotonic)' -count=1 -v`。DSN 不写进计划、日志或仓库。结果必须明确含 sqlite 与 postgres 子用例实际执行，不得接受 postgres SKIP。环境不可用时停在“未验证”，不宣称多实例安全。

## 任务 8（2.3）：整批追加、权限与原子回滚

- [x] 任务 8（2.3）：整批追加、权限与原子回滚 <!-- comet-task:22adfd25-c1cc-4dfb-95ea-76b298d5f129 -->

**文件：** 扩充建议新增的 app/repository `project_asset_binding.go`；修改现有 `backend/internal/handler/project_unit_routes.go`、`backend/internal/service/aliases_types.go`、`backend/internal/kernel/errors.go`、`backend/internal/kernel/error_codes.go`、`web/src/services/api/projects.ts`、`web/src/services/api/error-codes.ts`；建议新增 `backend/internal/app/project_asset_binding_append_test.go`、`backend/internal/handler/project_asset_binding_test.go`、`web/test/project-asset-binding-api.test.ts`。

**接口：** 实现 append，使用完整 baseline 与 selections；selections 必须恰好覆盖 baseline.shotIds，无选择镜头传空数组。重复的完全相同版本 ID可去重；同一镜头重复冲突条目、缺字段、漏镜头、多镜头拒绝。

- 先写两镜头一合法一非法的原子测试：跨用户、跨项目、错 unit、已删除版本、无效状态、未关联项目、5+2、基线少旧引用、目标集合变化、篡改外部版本均拒绝，全表前后差异为零。
- 解析输入后做初步鉴权/owner 发现，再进入任务 7 事务；锁内对当前用户与每个对象重新授权。管理员仍必须遵守本项目引用与任务允许集合，不泄露其他账户标题或路径。
- 根据原 B 与 selection 计算 D；已存在版本保留原全部元组。容量按版本并集计数，历史多 role 不删不改。所有镜头、版本、基线校验完再执行首条 INSERT。
- 首次分支当前状态必须精确等于 baseline；写入 D、失效和项目 CAS 同事务。唯一约束或中途数据库失败立即回滚，不吞错误成部分成功；事务只做数据库操作。
- 注册 reason：400 invalid_request、422 invalid_reference、422 capacity_exceeded、409 conflict、503 busy，统一前缀 `storyboard_asset_binding_`；鉴权保持既有 401/403/404 策略，内部失败 500。前端按 reason/code 处理，http 正确解包与抛错。
- 拟红/绿运行 API 合同和 app 原子测试，验证旧镜头补绑从不调用 replace。任务 9 未完成前不对用户开放追加入口。

未来命令（backend）：`go test ./internal/app ./internal/handler -run '^TestStoryboardAsset(Append|API)' -count=1`；（web）：`bun test test/project-asset-binding-api.test.ts`。

## 任务 9（2.4）：集合差失效与严格状态幂等

- [x] 任务 9（2.4）：集合差失效与严格状态幂等 <!-- comet-task:e1e8c801-2b1f-4eec-9410-438ef500bde2 -->

**文件：** 扩充建议新增 app/repository `project_asset_binding.go`；修改现有 `backend/internal/repository/repository_shots_workflows.go` 中可在事务内复用的失效部分；建议新增 `backend/internal/app/project_asset_binding_idempotency_test.go`。不循环调用现有 `UpsertShotAssetReferenceAndInvalidate`。

**接口：** append result 的 addedCount 首次为实际 D 行数；严格后置重试为 0/unchanged=true。项目 expectedRevision 为 baselineRevision +（全批任一 D 非空 ? 1 : 0）。首次全 D 空、严格后置均不执行任何业务 UPDATE/INSERT。

- 先写 A 镜实增、B 镜全已有：仅 A 相关产物 stale/selected=false，历史 ID/内容保留；B 的产物与引用逐字段不变；项目只增 1。同版本多 role/status 原行不折叠。
- 写两并发相同原请求测试：一次实增、一次 unchanged；观察数据库语句/快照证明重试没有再次 stale、重选中或 bump revision。
- 实现顺序：重新鉴权→计算原意图 B/D→比较原状态→比较完整预期后置→否则 conflict。后置比较保留旧 referenceId；新增行仅允许准确 D 元组及唯一性，不要求客户端猜新 ID。
- 首次成功后逐一注入改脚本、加/删其他引用、改 role/status、解除关联、换版本、媒体内容改变、重新 selected、新增产物、额外 revision；同一原请求全部冲突。改变 selection 也不得冒充原请求重试。
- CAS 仅主事务对目标项目执行 `WHERE revision=baselineRevision` 的 `revision=revision+1`，影响行数不是 1 即回滚；复用 helper 不得二次递增。锁获取本身的 SQLite 无匹配 UPDATE 不算业务修改，但无变化分支不允许改时间戳。
- 拟红/绿运行，清楚记录这是状态幂等而非永久请求审计；非信任基线不提供预览防伪。

未来命令（backend）：`go test ./internal/app -run '^TestStoryboardAssetIdempotency' -count=1 -v`。再复跑任务 8 原子测试。

## 任务 10（2.5）：真实章节页面预览与确认

- [x] 任务 10（2.5）：真实章节页面预览与确认 <!-- comet-task:ab11b4f8-1ed3-43fd-abc8-79d55fc39a28 -->

**文件：** 修改现有 `web/src/pages/projects/detail/workflow-production-workbench.tsx`、`workflow.tsx`、`workflow-stage-views.tsx` 中实际拥有章节/镜头选择与刷新状态的调用链；建议新增 `web/src/pages/projects/detail/chapter-asset-binding-drawer.tsx`、`web/test/chapter-asset-binding-drawer.test.tsx`。读取 `web/src/router.tsx` 与 `docs/ui-design-system.md`，不新增路由或全局 CSS 覆盖。

**接口：** 私有组件 props 固定为 `{ projectId: string, unitId: string, shotIds: string[], open: boolean, onClose: () => void, onApplied: (result: BindingAppendResult) => void }`。页面负责用户 scope 查询刷新；组件负责一次预览/选择/提交会话，使用 AppDrawer 产品容器与现有 token。

- 先写组件测试：已有只读引用、明确视觉证据默认选中、普通叙述/台词未选中、无版本禁用、缺图可选、5+2 显示总量7/剩余1且要求人工缩减；同版本多 role 显示多行但容量只算一个版本。
- 在真实章节工作台加入“匹配镜头资产”。实际路由为 `/projects/:projectId/chapters/:chapterId`，由现有 ProjectDetailPage 承载；验证该路由中的真实工作台入口，不以独立 HTML 或仅组件静态截图替代。
- 显示逐镜原文证据、歧义、问题与失效提示：“仅实增镜头的相关产物将过期并取消选中，历史内容保留”。目标 >100 时要求明确缩小范围，不隐式拆成多批提交。
- 关闭未完成预览使用 AbortSignal 取消；取消零写入。确认中禁重复点击；确认断连保留原 baseline/selection，允许有界重试原请求，不能认为服务端已回滚。
- conflict 使旧预览不可再次确认；用户重新预览并复核，不后台重放旧选择。成功失效当前用户/project/unit 的既有查询键并合并刷新结果，账号切换不复用预览。
- 拟红/绿通过后，获主线程授权再验收真实路由明暗主题、键盘焦点/ESC/按钮、长列表滚动、空态、超限与重试。动画尊重 reduced-motion；不复制 Modal padding 或新增全局 AntD 补丁。

未来命令（web）：`bun test test/chapter-asset-binding-drawer.test.tsx test/project-asset-binding-api.test.ts`；风险充分验证阶段：`bun run build` 与 `bun run lint`。后两项是未来命令，未在本计划编写中执行。

## 任务 11（2.6）：追加接口和安全语义文档

- [x] 任务 11（2.6）：追加接口和安全语义文档 <!-- comet-task:afbdaa68-5b81-48a3-8687-12ae7e73c216 -->

**文件（现有）：** `backend/internal/handler/openapi.yaml`、`docs/content/docs/backend/http-api.mdx`、`docs/content/docs/overview/features.mdx`、`docs/content/docs/backend/code-map.mdx`、`docs/content/docs/progress/pending-test.mdx`。

- 将 preview/append 的全部 schema、数量约束、完整 selections、baseline 非信任属性与三类响应写入 API/OpenAPI；失败示例覆盖 400/422/409/503，不以 HTTP200 包装失败。
- 明确“取消零写入”“断连结果未知，原请求重试”“冲突必须重新预览”“仅实增失效、历史保留”，以及无变化不推进 revision。
- 文档列出 owner 锁直接竞争范围、资源 Outbox 与 purge 统一引用保护边界；不宣称物理资源永久可用，不把 pending/processing 叫资源 deleting。
- 按任务 7—10 实际结果记录 SQLite/PostgreSQL 与真实页面证据，缺环境保持 pending-test。没有 schema 变化不修改数据库专题；若实施发现必需 schema 变化，停止该扩展交回主线程先修订设计，不擅加表。
- 对照 handler 测试输出与 OpenAPI required/enum，确保前后端字段一致。

未来命令（backend）：`go test ./internal/handler -run '^TestStoryboardAssetAPI' -count=1`；静态文档差异检查沿任务 5 的精确文件命令，不能代替运行验证。

## 任务 12（3.1）：首次章节生成到参考图全链路验收

- [x] 任务 12（3.1）：首次章节生成到参考图全链路验收 <!-- comet-task:c00032ec-b37e-4e0c-9952-1e4a3b7d565c -->

**文件：** 扩展任务 4 建议新增的 `backend/internal/app/project_asset_binding_generated_test.go`、`web/test/chapter-generated-asset-binding.test.ts`；建议新增 `backend/internal/app/project_asset_binding_e2e_test.go`；记录到现有 `docs/content/docs/progress/pending-test.mdx`。复用现有 `web/test/workflow-shot-references.test.ts`、`web/test/workflow-shot-generation.test.ts`。

- 构造隔离项目：零镜头引用，三类资产，81 项分页，同名条目分布两页，一个无版本、一个有效版本缺图。替身文本上游返回结构化 projectAssetRefs，不调用真实付费模型。
- 沿实际任务创建→provider 请求捕获→任务完成→resolve-generated→replace 保存链路执行，不直接插引用代替链路。断言实际 prompt 摘要与任务 allowedAssetIds 同源，完整目录同名判定没有因范围缩减改变。
- 通过持久化镜头触发图像/视频请求，替身上游捕获 payload 与资源映射，断言准确使用选定有效版本的参考图，缺图不发送虚假资源。涉及本地替身 host 时只做精确开发放行，不允许全部私网。
- 注入非法 ID、超预算、无版本、超容量、版本切换、任务挪用，检查无未经允许落库；共享画布无章节上下文输出合同不变。
- 把实际运行命令、子用例、payload 断言与数据库快照结果写入现有 pending-test；不记密钥、Cookie 或敏感 URL。真实路由人工验收未做则明确保留。

未来命令（backend）：`go test ./internal/app -run '^TestStoryboardAsset(Generated|E2EGenerated|Prompt)' -count=1 -v`；（web）：`bun test test/chapter-generated-asset-binding.test.ts test/workflow-shot-references.test.ts test/workflow-shot-generation.test.ts test/canvas-storyboard-output-contract.test.ts`。

## 任务 13（3.2）：旧章多镜头与并发全链路验收

- [x] 任务 13（3.2）：旧章多镜头与并发全链路验收 <!-- comet-task:6355c8e7-2747-4899-a9d9-93ff06528d54 -->

**文件：** 扩展建议新增 `backend/internal/app/project_asset_binding_e2e_test.go`、`project_asset_binding_concurrency_test.go`、`project_asset_binding_idempotency_test.go`、`web/test/chapter-asset-binding-drawer.test.tsx`；记录到现有 pending-test。

- 隔离旧章 fixture 至少三镜：一镜实增、一镜全已有、一镜普通叙述待选择；保留历史产物、人工引用及同版本多 role。保存确认前镜头 ID、脚本、引用完整元组与产物内容快照。
- 沿 HTTP preview→用户选择→append 操作，比较确认后只新增选定 D、仅实增镜头失效、项目增一次。取消与全部已有场景前后业务状态完全一致。
- 对服务端已提交但客户端断连场景，用同一原请求恢复，严格后置一致返回 unchanged；随后重新选中产物/新增引用/改脚本，再次重试必须冲突。
- 复跑任务 7 清单每类入口的两种屏障顺序，使用文件 SQLite 和真实 PostgreSQL 独立连接；混入越权镜头/版本整批失败且无对象信息泄漏。校验管理员 owner、跨项目共享资产、关联解除引用复查与 revision 单调。
- 对预览和确认期间模型/媒体请求计数、任务/Outbox 数据变化分别断言：预览/确认不创建生成任务；Outbox 仅由竞争删除流程按原语义创建，不能误归为追加副作用。
- 在真实章节路由检查取消、超限、冲突刷新、断连重试、账号切换，保存证据。没有双数据库或真实 UI 证据，不能宣布端到端验收通过。

未来命令（backend）：`go test ./internal/app ./internal/repository ./internal/handler -run '^TestStoryboardAsset' -count=1 -v`，PostgreSQL 先执行任务 7 的非空 DSN 门禁；（web）：`bun test test/chapter-asset-binding-drawer.test.tsx test/project-asset-binding-api.test.ts`。

## 任务 14（3.3）：规范—实现—证据一致性与交付

- [x] 任务 14（3.3）：规范—实现—证据一致性与交付 <!-- comet-task:f19cd3aa-72e5-4ced-88f0-0dccc700d041 -->

**文件（现有）：** `docs/content/docs/overview/features.mdx`、`docs/content/docs/backend/http-api.mdx`、`docs/content/docs/backend/code-map.mdx`、`docs/content/docs/progress/pending-test.mdx`、`backend/internal/handler/openapi.yaml`；只读根 change spec/tasks 和正式 Design Doc。

- 按本计划下表核对全部 14 项与 spec 场景，逐项绑定实际测试名称、命令、执行结果与环境；区分通过、失败、跳过、未执行，不用“计划覆盖”替代“已运行”。
- 对照最终 HTTP DTO、handler、app/service 类型别名、前端调用方与 OpenAPI；检查 6 不同版本/100 镜头/80 分页/24 KiB、ID 优先与无 alias、完整 baseline、权限/owner 锁、严格幂等均一致。
- 完成功能概览/代码地图最小更新，用户尚未验收项留 pending-test；只增补本次必要段落，不覆盖已有文档修改，也不把 schema 未变化写成数据库已迁移。
- 执行本 change 的严格检查示例命令 `openspec validate storyboard-asset-auto-binding --strict`；只有 CLI 真实执行成功才记录通过。若命令不可用，记录环境问题交回主线程，不编造替代成功结果、不改 Comet 状态。
- 执行 `git diff --check` 与 `git status --short`，检查新增文件仅为获准实施范围、无密钥/数据库/媒体产物入仓；原有差异仍保留。对存在的历史 diff 问题单独归因，不为让检查绿而修无关文件。
- 交付实际文件、专项测试、双数据库与 UI 证据、剩余阻塞及回退边界。回退只撤新入口与章节适配，不删合法引用/历史产物、不恢复 stale 的 selected、不用整章替换回滚补绑。不自动提交；状态登记与验收由主线程统一处理。

## 自查覆盖矩阵

| 原任务 | 本文任务 | 可验收交付 |
| --- | --- | --- |
| 1.1 | 1 | 完整三类候选、分页中断状态 |
| 1.2 | 2 | 保守确定性解析、中文证据、版本/图/容量 |
| 1.3 | 3 | 真实 prompt、预算、可信允许集合 |
| 1.4 | 4 | taskId 解析、保存重验、版本参考图 |
| 1.5 | 5 | 生成接口与功能文档 |
| 2.1 | 6 | 一致快照、零写入预览 |
| 2.2 | 7 | owner 锁清单、双库屏障、revision 单调 |
| 2.3 | 8 | 权限、集合/容量、整批原子确认 |
| 2.4 | 9 | 差量失效、精确后置幂等 |
| 2.5 | 10 | 真实章节页面、选择与重试 |
| 2.6 | 11 | 追加 API、reason、原子/失效文档 |
| 3.1 | 12 | 新章真实链路与替身上游捕获 |
| 3.2 | 13 | 旧章快照、并发、取消与无付费任务 |
| 3.3 | 14 | 规范/实现/证据与待验收文档一致 |

编写自查：14 项一一映射；接口名称和字段在跨任务合同集中定义；原引用身份/role/status 与容量版本去重明确分离；没有额外平台、自动提交或自动服务启动；没有把未来命令写成已通过。计划执行、TDD、隔离和 review 的选择交由主线程统一确认，本实施规划 agent 不再询问用户。
