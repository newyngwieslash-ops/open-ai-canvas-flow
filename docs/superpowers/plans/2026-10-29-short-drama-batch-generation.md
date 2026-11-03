# 短剧制作：本章批量分镜图与动作预演 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 本宿主对应可调用技能名称为 `subagent-driven-development`、`executing-plans`；不要猜测不存在的命名空间。本文最初作为交接计划编写，后续实施与补修记录在文末独立维护；任务勾选须结合实际测试和浏览器证据阅读，不能把初版全部勾选视为所有安全边界已经验收。

**Goal:** 在短剧制作真实工作台增加紧邻「匹配镜头资产」的「批量生成分镜图」「批量生成动作预演」，支持当前章节安全补缺、确认费用、逐镜进度、有限重试及中断恢复。

**Architecture:** 保留现有单任务服务、后端队列、计费和镜头产物登记，仅增加页面私有的候选规划、确认弹窗、单活动任务调度及用户隔离的轻量恢复日志。单镜与批量图片生成共享参数构造，不把当前表单复制给其他镜头，不把画布节点队列搬进项目页面。后端只补活动任务容量拒绝的机器可读原因及正确 HTTP 投影，不新增批量接口、表或生成引擎。

**Tech Stack:** React 19、TypeScript、TanStack Query、现有 Ant Design/AppModal/ModelPicker、localforage、Bun test/Happy DOM；Go/Gin/GORM 的既有任务服务；原生 Web Locks 用于同源同账号同章节防重。

## Global Constraints

- 范围只有当前章节；不做全剧、不新增镜头多选系统、不新增批量视频、不自动串联两种生成。
- 动作预演保持当前 `mode: "image"`、`artifactType: "action_board"`、黑白草图语义；不承诺动画、视频、固定十二宫格或以已有分镜图为输入。
- 每个镜头每个类型创建一个任务，生图数量固定为 1；供应商返回多图时沿用已有产物行为，不在这里重写媒体协议。
- 第一版每章最多一个批次、每批最多一个已提交且未终结的任务；这是本地调度上限，不是用户账户额度。不同用户/项目的全局配额仍由服务端裁决。
- 缺失产物默认纳入；失败、取消、过期显式纳入；有效成品不批量重做。同类型在途、结果待落库、提交不确定、需恢复保存的任务不进入新生成队列。
- 批量生成不得调用 `saveProjectShot` 或 `createShotRevision`；只使用确切存在的 `currentRevisionId`。缺失修订、脚本无有效内容、资产 token 无法解析或模型不支持的镜头逐项阻止。
- 当前镜头有未保存编辑时先提示返回「保存脚本」，保存完成后重新预览批次；不静默丢弃，也不隐式保存整个章节。
- 预览与报价零生成任务、零脚本写入；工作流初始化只发生于用户确认开始后，并且一次批次只初始化一次目标阶段。
- HTTP 业务请求只能通过现有 `http`/API service；不直连模型、不绕过鉴权/限额/计费、不解析错误 `msg` 判断容量或重试。
- 无响应、超时、5xx 或提交后结果不明不能自动重放；`ApiError.retryable` 不等于非幂等 POST 可以重发。
- 恢复日志使用 `localForageStorageForScope(ownerScope)`，只保存 ID、状态、时间、摘要指纹和白名单小配置。不得保存完整 `AiConfig`、渠道密钥、正文、提示词、媒体、URL、完整 task/input/result。
- 刷新、离开章节、账号切换后停止新增提交；已提交任务继续在服务端运行。返回只恢复观察，继续剩余必须重新预览、报价和确认。
- 同源多标签使用原生 Web Locks；不支持时禁用批量并说明原因，单镜功能保留。不写 localStorage 伪锁，不声称提供跨设备/服务端提交幂等。
- UI 使用真实路由、现有 token、`AppModal flush`、可访问按钮与焦点管理；不生成独立 HTML 冒充应用验收。
- 保留工作区所有已有修改，尤其 `workflow-production-workbench.tsx`、`workflow.css`、`workflow-shot-generation.test.ts` 的未提交双槽时间轴修改；不覆盖、不回滚、不清理。
- 仓库使用 Bun；不安装新依赖，不改锁文件，不使用 npm/pnpm 重装前端。手工修改优先 `apply_patch`；宿主未提供时用专用 Edit/Write，不用 shell 拼接覆盖产品文件。
- 本计划包含最小专项验证；执行方必须记录真实结果。真实模型付费调用需要用户明确授权；没有授权用 mock 验证并把真实模型验收列为待确认。
- 不默认启动服务，不重启已有服务；浏览器验收先检查当前可用实例。需要新启动且未获预览/联调授权时记录阻塞，不虚报通过。
- 用户尚未授权 Git 提交或推送；任务完成逐项勾选与审查即可，不能因技能模板有 commit 步骤而擅自提交。文末提供获授权后的提交粒度。

---

## 1. 已核对的现状与不可误判点

行号以计划编写时的工作区为准；执行前按符号定位并阅读当前 diff，不按行号盲替换。

| 事实 | 当前入口/证据 |
| --- | --- |
| 本章 workspace 而非全剧数据 | `web/src/pages/projects/detail.tsx:52-75`，`getProjectUnitWorkspace` |
| 工作台只有单镜生成 | `web/src/pages/projects/detail/workflow-production-workbench.tsx:327-400` |
| 三个生产阶段共用工作台 | 同文件 `:125-138`、`:584-590`；视频页也应能打开两种批量图片入口 |
| 匹配镜头资产作用于本章 | 同文件 `:490`、`:614-622` |
| 现有标题行空间有限 | `web/src/pages/projects/detail/workflow.css:175-184`，固定 48px；三栏 `:40-48` |
| 预演图片仅引用绑定资产 | workbench `:356-385`；`workflow-shot-references.ts:27-104`，没有读取分镜图产物 |
| 每次保存真的会新建修订并使产物过期 | `backend/internal/app/project_shot.go:123-132`；`backend/internal/repository/repository_shots_workflows.go:58-81` |
| 旧修订任务回填为 stale | `backend/internal/app/project_workflow.go:347-349` |
| 提交后立即返回任务 ID | `web/src/services/api/generation-task.ts:111-120` |
| 现有 batch helper 不是跨镜头调度 | 同文件 `:187`，同一 prompt/config 复制 1–15 份；不能直接套用 |
| 画布已有批量，但强耦合 node/connection | `canvas-script-node.tsx:178-218`、`use-canvas-generation-batches.ts`；只借鉴交互，不搬数据结构 |
| 公开会话返回实际活动上限 | `web/src/services/api/auth.ts:43-55,417`；`GET /auth/session`；默认值不是当前额度证据 |
| 活动上限包含 queued + running | `backend/internal/repository/repository_tasks.go:35-38` |
| 容量满目前无专用 reason | `backend/internal/app/task_creation.go:149-150,243-245`、`task_lifecycle.go:96-97` |
| handler 目前抹掉 service reason | `backend/internal/handler/routes.go:48-50,201-204` 调用 `fail` 而非保留 AppError |
| 创建频率拒绝可安全等待 | `security.go:142-159`，在 CreateTask 前返回 `429/42901/rate_limited` 与 Retry-After |
| 列表有截断，不能靠查无结果重发 | `project_workbench_read.go:250-267,328-340` 最近100+活动100后筛章节；普通 `/tasks` 无通用游标分页 |
| batch 关联数据可写现有 metadata | `generation-task.ts:36-55,289-320`；只关联，不构成幂等键；任务详情 inputJson 可读，摘要 clientContext 不含 batchId/itemId/revisionId |
| 已有同 ID 重试与恢复保存 | `retryGenerationTask` / `queryGenerationTask`，`task_lifecycle.go:33-106`；不能用新任务绕过 retry 安全检查 |
| 用户切换有 drain 栅栏 | `web/src/lib/user-session.ts:23-62` 与 `services/generation-consumer-lifecycle.ts` |

### 1.1 最小范围的明确决定

1. 两个按钮独立存在，桌面相邻，窄屏换行。默认范围是「仅补缺」，另有「仅重试失败」「仅更新过期」三个互斥模式，不加全选/多选镜头 UI。
2. 重试失败只处理可明确关联的同类型失败/取消任务，使用原 task ID；无法确定旧任务或已修改脚本的条目明确阻止，不能当“缺图”悄悄创建新任务。用户可回单镜处理。
3. 一个批次只有一个活动任务，完成/失败后补下一项；单项供应商失败可继续其他项，但不会自动重试失败项。容量/请求频率有界退避；未知提交立即暂停整批。
4. 不做后台守护进程。关闭弹窗不暂停；离开当前章节/关闭页面会停止补位，已接受任务不取消。重新进入需明确继续。
5. 批量确认使用独立图片设置，不能在视频页误读视频模型。优先项目默认图片模型，回退全局图片默认；当前处于图片阶段时可以带入当前图片规格，但必须在弹窗显示并确认。
6. 有未保存内容时不启动预览；提示「请先保存当前镜头脚本，再批量生成」。这是刻意简化，不增加第二套保存/丢弃弹窗。
7. 暂不修改预演提示词、画布十二宫格、资产匹配算法、声音/对白逻辑、时间轴媒体回退。这些不是本批量需求的授权范围。

## 2. 文件职责与任务依赖

### 2.1 新增文件（页面私有，不建通用队列框架）

| 文件 | 单一职责 |
| --- | --- |
| `web/src/pages/projects/detail/workflow-image-generation.ts` | 单镜/批量图片的相同 prompt、引用、模型要求、提交 payload 构造 |
| `web/src/pages/projects/detail/workflow-batch-plan.ts` | 纯候选分类、范围过滤、基线指纹、报价汇总；定义批量领域类型 |
| `web/src/pages/projects/detail/workflow-batch-storage.ts` | 白名单日志编解码、固定 owner 的 localforage、写后确认 |
| `web/src/pages/projects/detail/workflow-batch-runner.ts` | 页面私有单活动任务状态机、限额等待、停止、不确定提交和逐项重试 |
| `web/src/pages/projects/detail/use-workflow-image-batch.ts` | React 生命周期、账号栅栏、Web Locks、现有任务观察和 Query 缓存协调 |
| `web/src/pages/projects/detail/workflow-image-batch-dialog.tsx` | 一个弹窗支持两个产物类型；范围/图片设置/费用/逐项问题/确认 |
| `web/test/helpers/workflow-batch-fixture.ts` | 小而完整、明确版本/产物/任务关系的测试数据 |
| `web/test/workflow-image-generation.test.ts` | payload 对等与逐镜隔离 |
| `web/test/workflow-batch-plan.test.ts` | 候选、费用、指纹 |
| `web/test/workflow-batch-storage.test.ts` | 日志校验、scope、读写故障 |
| `web/test/workflow-batch-runner.test.ts` | 可控 Promise/时钟的队列状态测试 |
| `web/test/workflow-image-batch.test.tsx` | 真实工作台与弹窗交互、账号和路由切换 |
| `backend/internal/app/task_active_limit_error.go` | 唯一容量错误构造，消除三个重复映射 |
| `backend/internal/app/task_active_limit_error_test.go` | 容量分支与零新增账单/任务 |
| `backend/internal/handler/task_active_limit_test.go` | HTTP 原因码保持及路由入口验证 |

修改现有文件：`workflow-production-workbench.tsx`、`workflow.css`、必要时 `workflow.tsx` 的章节 key、`workflow-shot-generation.test.ts`；`kernel/error_codes.go`、`app/error_codes.go`、`service/aliases_consts.go`、`app/task_creation.go`、`app/task_lifecycle.go`、`handler/routes.go`、`web/src/services/api/error-codes.ts`；文末列出的专题文档与 OpenAPI。除确有测试证明需要，不改通用 request、任务 DTO、数据表、user-session 或画布队列。

### 2.2 实施次序

- 任务 1（错误合同）可独立于任务 2–4（图片构造/候选/费用）推进。
- 任务 2 先在 `workflow-batch-plan.ts` 建立第3节类型并建立测试fixture，再提取图片构造；任务3只添加候选/指纹实现。这消除“任务2测试依赖尚未执行的任务3”的循环。类型导入使用 `import type`，不得形成运行时循环。
- 任务 5（日志）依赖任务 3 的领域类型。
- 任务 6（调度器）依赖 1–5；任务 7（恢复和生命周期）依赖 5–6。
- 任务 8（确认弹窗）依赖 2–4；可以与 5–7 并行，但接口采用下节唯一合同。
- 任务 9（工作台集成）依赖 6–8；任务 10（完整集成回归）依赖 9。
- 任务 11（真实路由验收）与任务 12（专题文档/交付）最后完成。

每项任务以专项测试+独立 diff 审查收尾，不以创建空文件或“接入占位按钮”算完成。

## 3. 统一类型与行为合同

以下是本次新增合同，不是声称当前代码已有。后续任务使用同名类型，不另起同义类型。

```ts
// workflow-batch-plan.ts
export type BatchImageStage = "storyboard" | "previz";
export type BatchArtifactType = "storyboard" | "action_board";
export type BatchScope = "missing" | "failed" | "stale";
export type BatchCategory =
    | "missing" | "ready" | "active" | "syncing"
    | "failed" | "stale" | "blocked";
export type BatchItemState =
    | "pending" | "submitting" | "submitted" | "syncing"
    | "succeeded" | "failed" | "blocked" | "unknown" | "stopped";
export type BatchRunState = "paused" | "running" | "waiting" | "stopped" | "finished";
export type BatchImageSettings = {
    model: string;
    size: string;
    quality: string;
    skillIds: string[];
};
export type BatchIdentity = {
    ownerScope: string;
    projectId: string;
    unitId: string;
};
export type BatchCandidate = {
    shotId: string;
    revisionId: string;
    title: string;
    category: BatchCategory;
    taskId?: string;
    eligible: boolean;
    issues: string[];
};
export type BatchItem = {
    itemId: string;
    shotId: string;
    revisionId: string;
    fingerprint: string;
    operation: "create" | "retry";
    state: BatchItemState;
    taskId?: string;
    attempts: number;
    taskAttemptsBefore?: number;
    taskUpdatedAtBefore?: string;
    updatedAt: string;
    issueCode?: string;
};
export type BatchLog = BatchIdentity & {
    version: 1;
    batchId: string;
    stage: BatchImageStage;
    scope: BatchScope;
    settings: BatchImageSettings;
    state: BatchRunState;
    items: BatchItem[];
    createdAt: string;
    updatedAt: string;
};
export type BatchPrice = {
    knownMicrocredits: number;
    unknownCount: number;
    estimated: boolean;
};
export type BatchPreview = {
    identity: BatchIdentity;
    stage: BatchImageStage;
    scope: BatchScope;
    settings: BatchImageSettings;
    candidates: BatchCandidate[];
    items: BatchItem[];
    price: BatchPrice;
    confirmationToken: string;
};
```

`issueCode` 只用固定代码，例如 `revision_changed`、`reference_changed`、`model_unavailable`、`capacity_wait`、`rate_wait`、`submit_unknown`、`media_recovery_required`、`task_read_failed`、`storage_failed`、`archived_project`；完整上游错误仅在内存中显示安全文案，不持久化。

### 3.1 候选判定顺序

以 `(shotId, artifactType)` 为键，严格本项目本章节，不能只按 shotId：

1. 缺镜头/缺确切当前修订、项目归档、已知未确认提交：blocked。
2. 同类型任一 queued/running 或本地 submitting/submitted：active。不能只看最新一条掩盖旧的在途任务。
3. 存在 `ready + resourceId + revisionId === currentRevisionId` 的同类型产物：ready；即使历史中最新一次重做失败，也不默认纳入补缺/失败批量。
4. 同类型成功任务尚未找到对应产物，或 pending/running 产物：syncing；查详情/刷新，不重生成。
5. 最新可关联失败/取消任务且无有效成品：failed；重试前详情校验 `canRecoverMedia`、账单 uncertain、取消 requested/uncertain、原修订和元数据。
6. 同类型有过期产物或旧修订产物：stale。成功但只有 stale 回填不是“补缺成功”。
7. 没有同类型任务/产物证据：missing。
8. 模型不兼容、无可用脚本或失效 `@[asset:...]` token 将本应 eligible 的候选改为 blocked，保留明确原因。

`missing` 模式只选 missing；`failed` 模式只选可安全重试的 failed；`stale` 模式只选 stale。不要把“没有 selected 成品”直接当 missing；不要复用 `currentArtifact` 的最后一项回退作为有效性判定。

### 3.2 基线与时序

- 确认前读取新的 core/workspace，快照至少包含当前修订 ID、修订正文、绑定 reference 的 ID/assetVersionId/role/status、绑定版本的可用资源 ID、解析后的图片顺序、技能版本与最终 prompt 摘要。
- 指纹使用确定性数组+排序后的 JSON 与 `crypto.subtle.digest("SHA-256", ...)`；只落 hash 不落正文，不用随机时间戳或项目 revision 作镜头内容指纹。
- 每次补位前重新读取 workspace，比较该镜头指纹与确认值；变了则该项 blocked、提示重新预览，不默默使用新版本或旧引用。
- 单镜仍可在其他镜头编辑；本地当前批次涉及的镜头暂停“保存、删除、绑定/解绑、重新生成”。本章“匹配镜头资产”和“生成章节分镜”在批次未结束时禁用并提示先停止。其他设备变化靠基线检测，不宣称这是服务端原子资源锁。
- 后端只保证已存在的版本回填行为；确认与真正创建之间仍存在跨设备窗口，本计划不构建分布式批量事务。必须在文档列明限制。

## Task 1：提供可判定的活动任务容量错误

**Files:**
- Create: `backend/internal/app/task_active_limit_error.go`
- Create: `backend/internal/app/task_active_limit_error_test.go`
- Create: `backend/internal/handler/task_active_limit_test.go`
- Modify: `backend/internal/kernel/error_codes.go`、`backend/internal/app/error_codes.go`、`backend/internal/service/aliases_consts.go`
- Modify: `backend/internal/app/task_creation.go` 的两处容量分支；`backend/internal/app/task_lifecycle.go` 的 retry 容量分支
- Modify: `backend/internal/handler/routes.go` 的 POST tasks/retry service 错误分支
- Modify: `web/src/services/api/error-codes.ts`
- Modify: `docs/content/docs/backend/http-api.mdx`、`backend/internal/handler/openapi.yaml`

**Interfaces:**
- Consumes: `repository.ErrActiveTaskLimit`、`AppError`、现有 `failService`。
- Produces: `400 / code 400 / reason task_active_limit_exceeded`，确定该次创建/重试未入队且未预占费用；不承诺参考媒体上传零副作用。

- [x] **Step 1：先写原因构造与 HTTP 投影失败测试。**

```go
func TestTaskActiveLimitError(t *testing.T) {
    err := taskActiveLimitError(2)
    if err.Status != 400 || err.Code != 400 || err.Reason != ReasonTaskActiveLimitExceeded || !err.Retryable {
        t.Fatalf("unexpected capacity error: %#v", err)
    }
}
```

在 handler 测试复用 `responseTestContext()`/`decodeFailureEnvelope()`，不是用字符串匹配：

```go
func TestTaskActiveLimitHTTPProjection(t *testing.T) {
    recorder, ctx := responseTestContext()
    err := service.NewAppError(400, "活动任务已满")
    err.Reason = service.ReasonTaskActiveLimitExceeded
    failService(ctx, err)
    body := decodeFailureEnvelope(t, recorder)
    if recorder.Code != 400 || body.Code != 400 || body.Reason != "task_active_limit_exceeded" {
        t.Fatalf("status=%d body=%#v", recorder.Code, body)
    }
}
```

- [x] **Step 2：执行红灯。** 在 `backend/` 执行 `go test ./internal/app ./internal/handler -run 'TestTaskActiveLimit' -count=1`；预期新常量/函数缺失，不接受 unrelated failure 充当红灯。
- [x] **Step 3：添加唯一错误构造与常量。**

```go
// kernel/error_codes.go 的 ErrorReason 常量组
ReasonTaskActiveLimitExceeded ErrorReason = "task_active_limit_exceeded"

// app/error_codes.go
ReasonTaskActiveLimitExceeded = kernel.ReasonTaskActiveLimitExceeded

// service/aliases_consts.go
ReasonTaskActiveLimitExceeded = app.ReasonTaskActiveLimitExceeded
```

```go
// backend/internal/app/task_active_limit_error.go
package app

import "fmt"

func taskActiveLimitError(limit int) *AppError {
    err := NewAppError(400, fmt.Sprintf("同时排队或运行的任务最多 %d 个，请等待已有任务完成", limit))
    err.Reason = ReasonTaskActiveLimitExceeded
    err.Retryable = true
    return err
}
```

三处容量分支都替换为 `return nil, taskActiveLimitError(policy.Task.ActiveTaskLimit)`；不要顺带改其他 invalid_argument。

为了最小兼容改动，两个 handler 中仅新原因走结构化投影，保留其他错误既有行为：

```go
var appErr *service.AppError
if errors.As(err, &appErr) && appErr.Reason == service.ReasonTaskActiveLimitExceeded {
    failService(c, err)
} else {
    fail(c, http.StatusBadRequest, err)
}
return
```

这段放在 POST `/tasks` 和 `/tasks/:id/retry` 已有 `if err != nil` 内；不新增通用错误框架。

```ts
// web/src/services/api/error-codes.ts
export const TASK_SUBMISSION_REASONS = {
    activeLimitExceeded: "task_active_limit_exceeded",
    rateLimited: "rate_limited",
} as const;
```

- [x] **Step 4：补服务和真实 handler 路由测试，证明没有新增任务/计费。** app 测试复用 `creationTestService(t)`、`creationTextRequest()`，不启动 worker。读取 `s.RuntimePolicy()`，插入恰好 `ActiveTaskLimit` 个该用户 queued task；记录 `Task`、`BillingOrder`、`CreditLedgerEntry` 数量和 `CreditAccount.AvailableMicrocredits`，调用 CreateTask，要求原因精确匹配且各值不变。额外创建一条合法可重试的failed任务（不要把占满名额的queued任务改成failed释放名额），保持活动任务仍恰好等于上限，再调用RetryTask做同样断言。持久化限额竞态复用 repository 的 `ErrActiveTaskLimit` 分支测试，不能只覆盖创建前 Count。
- [x] **Step 5：HTTP 集成夹具沿用 `handler/task_media_recovery_test.go:16-63` 的内存 DB、测试账号/session、`ConfigureRuntime` 与 cleanup；调用实际两条路由。** 添加非容量 invalid_argument 仍保持旧投影、429 仍为 rate_limited、跨账号 retry 不泄露任务正文的回归。不得把 `failService` 单测代替路由测试。
- [x] **Step 6：更新 API 表/OpenAPI reason 枚举与两条接口错误说明，执行绿灯并审查 diff。** `gofmt` 仅格式化本任务 Go 文件；再执行上述专项命令。交付 reason 从 service 到真实 HTTP 的证据。

## Task 2：提取共享图片 payload，禁止批量隐式保存脚本

**Files:**
- Create: `web/src/pages/projects/detail/workflow-image-generation.ts`
- Create: `web/src/pages/projects/detail/workflow-batch-plan.ts`（本任务只放第3节类型，任务3补行为）
- Create: `web/test/helpers/workflow-batch-fixture.ts`（本任务创建，任务3复用）
- Create: `web/test/workflow-image-generation.test.ts`
- Modify: `web/src/pages/projects/detail/workflow-production-workbench.tsx:327-387`
- Modify: `web/test/workflow-shot-generation.test.ts`

**Interfaces:**
- Consumes: `ProjectDetail`、`ProjectShot`、`ShotRevision`、`AiConfig`、`BatchImageStage`、`BatchImageSettings`；现有 reference/model/skill services。
- Produces:

```ts
export type WorkflowImageInput = {
    detail: ProjectDetail;
    shot: ProjectShot;
    revision: ShotRevision;
    stage: BatchImageStage;
    settings: BatchImageSettings;
    config: AiConfig;
};
export function buildWorkflowImageInput(input: WorkflowImageInput): {
    prompt: string;
    config: AiConfig;
    requirements: ModelRequirements;
    references: ShotAssetReferenceContext;
};
export async function prepareWorkflowImageSubmission(
    input: WorkflowImageInput,
    context: {
        workflowStepId: string;
        skills: Skill[];
        batch?: { batchId: string; itemId: string };
    },
): Promise<Parameters<typeof submitBackendGenerationTask>[0]>;
```

`BackendGenerationTaskOptions` 当前没有 export；用 `Parameters<typeof submitBackendGenerationTask>[0]`，不为了命名改公共 API。

- [x] **Step 1：编写两镜资产隔离、预演图片语义、无版本写入的红灯用例。** 测试使用本任务建立、任务3继续复用的fixture；给两镜不同 revision.imagePrompt、assetVersionId/resourceId，断言每次 prompt/reference/metadata 对应自己的镜头。`previz` 包含当前黑白后缀，mode 为 image，referenceAudios 为空，不自动加入 storyboard 产物资源。

```ts
test("previz preserves image semantics and saved revision", async () => {
    const detail = makeWorkflowBatchDetail();
    const shot = detail.shots[0];
    const revision = detail.shotRevisions[0];
    const config = makeImageConfig();
    const options = await prepareWorkflowImageSubmission({
        detail, shot, revision, stage: "previz",
        settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
        config,
    }, { workflowStepId: "step-previz", skills: [] });
    expect(options.mode).toBe("image");
    expect(options.prompt).toContain("黑白分镜草图，清晰动作节拍，电影构图");
    expect(options.referenceAudios).toEqual([]);
    expect(options.metadata?.shotRevisionId).toBe(revision.id);
    expect(options.metadata?.artifactType).toBe("action_board");
});
```

`makeImageConfig` 采用现有 `generation-task-ark-asset.test.ts:6-23` 的真实图片协议，并显式设置 `imageModel`；`workflow-shot-generation.test.ts` 中的模型配置是视频，不能直接当生图夹具。完整基础fixture见附录A。现有 `GenerationTaskDependencies` 仅有 createTask/waitTask/createId；相邻旧测试里的 runLocal/now 不复制进新夹具。

- [x] **Step 2：在 `web/` 执行 `bun test test/workflow-image-generation.test.ts`，确认因为 helper 尚不存在失败。**
- [x] **Step 3：实现纯构造的关键内容。** 显式校验 revision.id 与 shot.currentRevisionId 相等、revision.shotId 属于 shot，project/unit 非空且归属一致。prompt 保持现有语义，不偷修其他生成需求：

```ts
const references = buildShotAssetReferenceContext(detail, shot.id);
const basePrompt = [
    revision.imagePrompt || revision.plotDescription,
    revision.action,
    stage === "previz" ? "黑白分镜草图，清晰动作节拍，电影构图" : "",
].filter(Boolean).join("\n");
if (!(revision.imagePrompt || revision.plotDescription).trim()) {
    throw new Error("镜头缺少画面内容，请先保存脚本");
}
const prompt = resolveShotAssetMentionPrompt(basePrompt, references, {
    dialogue: revision.dialogue,
    mode: "image",
});
const requirements: ModelRequirements = {
    capability: "image",
    input: { textCount: 1, imageCount: references.referenceImages.length, videoCount: 0, audioCount: 0, characterCount: 0 },
    imageSize: settings.size,
    options: { size: settings.size, quality: settings.quality },
};
const routedModel = resolveCompatibleModel(config, settings.model, requirements) || settings.model;
const problem = modelCompatibilityError(config, routedModel, requirements);
if (problem) throw new Error(`当前模型配置不可用：${problem}`);
const generationConfig = { ...config, model: routedModel, imageModel: routedModel, size: settings.size, quality: settings.quality, count: "1" };
```

使用现有 `normalizeImageValue` 在选型时规范一次；确认后不允许静默换不兼容模型。确认预览必须展示实际 routedModel；各镜 routing 不同时逐项显示，不用首镜报价乘总数。

- [x] **Step 4：实现一次技能准备与 metadata 构造。**

```ts
const built = buildWorkflowImageInput(input);
const skillExecution = await skillRuntime.prepare({
    profile: "shortDrama", prompt: built.prompt,
    skills: context.skills, selectedSkillIds: input.settings.skillIds,
});
return {
    projectId: input.detail.project.id,
    mode: "image",
    prompt: skillExecution.prompt,
    config: built.config,
    referenceImages: built.references.referenceImages,
    referenceAudios: [],
    metadata: {
        ...skillExecution.metadata,
        workflowStepId: context.workflowStepId,
        domainProjectId: input.detail.project.id,
        unitId: input.shot.unitId,
        shotId: input.shot.id,
        shotRevisionId: input.revision.id,
        artifactType: input.stage === "previz" ? "action_board" : "storyboard",
        role: "output", source: "short-drama-workflow",
        ...(context.batch ? { chapterImageBatchId: context.batch.batchId, chapterImageBatchItemId: context.batch.itemId } : {}),
        resolvedCharacterVersions: built.references.resolvedCharacterVersions,
        artifactMetadata: {
            model: built.config.model, aspectRatio: input.settings.size,
            durationSeconds: input.revision.durationMs / 1000,
            ...skillExecution.metadata,
        },
    },
};
```

不在 helper 内发任务、刷新 query、保存镜头或 toast。`isAiConfigReady` 由调用入口在确认与补位前显式校验。

- [x] **Step 5：单镜 storyboard/previz 分支改用相同构造逻辑，video 分支保持原行为。** 单镜现有保存流程仍保留，但 helper 接收保存接口返回的新 shot/currentRevisionId 与本次 form 对应 revision 数据，不拿保存前 detail 里的旧 revision；批量完全不走这段保存。必要时把 helper 的 revision 参数收窄为明确字段 DTO，但两处签名同步且不可伪造 ID。
- [x] **Step 6：跑新测试与 `bun test test/workflow-shot-generation.test.ts`。** 原视频 reference_audio、技能、单镜后台提交不等待、双槽时间轴测试必须保留。源码位置断言失效时改为实际 payload 行为测试，不删除覆盖以迁就重构。

## Task 3：实现候选分类与确认基线

**Files:**
- Modify: `web/src/pages/projects/detail/workflow-batch-plan.ts`（任务2已建立类型）
- Modify: `web/test/helpers/workflow-batch-fixture.ts`（复用任务2夹具，仅扩充用例需要的明确数据）
- Create: `web/test/workflow-batch-plan.test.ts`

**Interfaces:**
- Consumes: 第 3 节类型、现有 ProjectDetail/GenerationTask、任务 2 的纯构造。
- Produces:

```ts
export function classifyBatchCandidates(input: {
    detail: ProjectDetail;
    unitId: string;
    stage: BatchImageStage;
    scope: BatchScope;
    localItems?: readonly BatchItem[];
}): BatchCandidate[];
export async function fingerprintBatchShot(input: WorkflowImageInput): Promise<string>;
export function batchArtifactType(stage: BatchImageStage): BatchArtifactType;
```

- [x] **Step 1：建立明确数据 fixture，不用大量 `as any`。** factory 导出 `makeWorkflowBatchDetail()`、`makeBatchTask(shotId,status,artifactType)`、`makeBatchArtifact(shotId,status,artifactType)`、`makeImageConfig()`；镜头固定 s1/r1、s2/r2，项目 p1，章节 u1，所有必填字段真实填满。对象允许通过返回值后修改组成不同场景，不加复杂 builder。
- [x] **Step 2：先写判定顺序测试。**

```ts
test("missing scope does not regenerate ready, active, failed or stale shots", () => {
    const detail = makeWorkflowBatchDetail();
    detail.shotArtifacts = [makeBatchArtifact("s1", "ready", "storyboard")];
    detail.tasks = [makeBatchTask("s2", "running", "storyboard")];
    const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "missing" });
    expect(rows.map(row => row.category)).toEqual(["ready", "active"]);
    expect(rows.filter(row => row.eligible)).toEqual([]);
});

test("storyboard readiness never hides missing action board", () => {
    const detail = makeWorkflowBatchDetail();
    detail.shotArtifacts = [makeBatchArtifact("s1", "ready", "storyboard")];
    const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "previz", scope: "missing" });
    expect(rows[0].category).toBe("missing");
    expect(rows[0].eligible).toBe(true);
});
```

额外必须覆盖：同镜较新 failed 不掩盖较旧 running；ready 没 resourceId 不算有效；ready 旧 revision 属 stale；succeeded 还未登记产物属 syncing；无当前 revision 不允许 `currentRevision` 的历史回退；外章任务不影响本章；两个重名镜头不串数据；failed 并有有效成品跳过。

- [x] **Step 3：执行红灯，再按 3.1 的顺序实现分类。** 对 shots 按 position 排序；一次建立 artifacts/tasks by key Map，不在每个 JSX render 内多次扫描全量历史。用户 scope 在入口验证，helper 不擅自补 guest/defaultUser。
- [x] **Step 4：实现确定性 hash。**

```ts
async function sha256(value: unknown): Promise<string> {
    const bytes = new TextEncoder().encode(JSON.stringify(value));
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
}
```

`fingerprintBatchShot` 的输入是确切 revision 字段、按 reference.id 排序的 ID/版本/role/status、解析后的 referenceImages 中 id/storageKey 顺序、resolvedCharacterVersions、stage；不含 task 状态/项目 updatedAt/生成产物。技能 metadata+最终 prompt hash 纳入确认 token，但不能把整个 config 持久化。

- [x] **Step 5：测试保存/资产/技能基线变化。** 只改变本镜 revision 或参考资源时指纹变；只改变别镜产物状态或项目 updatedAt 时指纹不变；引用集合顺序无意义的部分排序后稳定，但实际 referenceImages 编号顺序必须影响指纹。执行 `bun test test/workflow-batch-plan.test.ts`。
- [x] **Step 6：审查补缺不会触发写路径。** 本文件只导入 type 或纯函数，不能依赖 create task/save shot、React/AntD；无候选时返回空集合并可解释原因。

## Task 4：批次设置、逐项预检和费用汇总

**Files:**
- Modify: `workflow-batch-plan.ts`
- Modify: `workflow-batch-plan.test.ts`
- Modify: `workflow-image-generation.ts`（仅必要的纯模型解析复用）

**Interfaces:**
- Consumes: `modelQuoteRequest`、`quoteModel`、`requestCreditCost`、`resolveModelChannel`、`modelOptionName`。
- Produces: `summarizeBatchPrices(values: readonly (number | null)[], estimated: boolean): BatchPrice`；hook 预览产物 `BatchPreview`，费用逐项以微积分整数合计。

- [x] **Step 1：写 unknown/零价/不同模型价格的测试。**

```ts
test("unknown prices are never advertised as free", () => {
    expect(summarizeBatchPrices([1000000, null, 0], true)).toEqual({
        knownMicrocredits: 1000000, unknownCount: 1, estimated: true,
    });
});
```

- [x] **Step 2：实现价格纯汇总。**

```ts
export function summarizeBatchPrices(values: readonly (number | null)[], estimated: boolean): BatchPrice {
    let knownMicrocredits = 0;
    let unknownCount = 0;
    for (const value of values) {
        if (value === null || !Number.isSafeInteger(value) || value < 0) {
            unknownCount += 1;
            continue;
        }
        const next = knownMicrocredits + value;
        if (!Number.isSafeInteger(next)) {
            return { knownMicrocredits: 0, unknownCount: values.length, estimated: true };
        }
        knownMicrocredits = next;
    }
    return { knownMicrocredits, unknownCount, estimated };
}
```

累计超安全整数时整体设未知，不能静默溢出或显示伪精确总额。

- [x] **Step 3：预览依次执行只读校验。** core 与 workspace 重新加载；确认当前用户 hydrated/id/scope 一致；过滤归档/缺修订/无 prompt/无效 mention；构造本镜 requirements；检查 `isAiConfigReady`；用相同 request 的 JSON key 去重只读报价，但不能拿首镜价格乘全部。当前实际 model、规格、数量和技能清单显示给用户。
- [x] **Step 4：确定费用文案，不夸大报价保证。**

```ts
export function batchPriceLabel(price: BatchPrice): string {
    const amount = (price.knownMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 6 });
    if (price.unknownCount) return `已知部分预计 ${amount} 积分，另有 ${price.unknownCount} 项费用无法预估`;
    return `${price.estimated ? "预计" : "当前报价合计"} ${amount} 积分`;
}
```

自定义渠道追加「渠道费用由供应商结算，非免费」；creditsEnabled=false 不把费用写成0。普通创建不存在整批锁价/预算批准，固定说明「实际逐项按提交时规格计费；本章不是原子提交，余额不足可能只提交部分镜头」。

- [x] **Step 5：设置变化使预览失效。** 改范围/模型/画幅/画质/技能时清空 confirmationToken 和旧价格，禁用开始，重新预检完成才启用；旧异步报价带 epoch 检查，不能覆盖新预览。重试失败模式沿用原任务 config，不允许当前弹窗模型/技能改写旧任务；显示原规格，无法可靠报价则明确未知。
- [x] **Step 6：绿灯。** `bun test test/workflow-batch-plan.test.ts test/workflow-image-generation.test.ts`；覆盖缺模型、能力不支持引用数量、资源缺图警告、全部 blocked、部分 eligible、请求取消后不回写旧报价。

## Task 5：用户隔离的轻量恢复日志

**Files:**
- Create: `web/src/pages/projects/detail/workflow-batch-storage.ts`
- Create: `web/test/workflow-batch-storage.test.ts`

**Interfaces:**
- Consumes: `BatchLog`、`BatchIdentity`、`StateStorage`、`localForageStorageForScope`。
- Produces:

```ts
export function createBatchStorage(identity: BatchIdentity, storage?: StateStorage): {
    read(): Promise<BatchLog | null>;
    write(log: BatchLog): Promise<void>;
    remove(): Promise<void>;
};
export function decodeBatchLog(raw: string, identity: BatchIdentity): BatchLog;
export function recoverBatchLog(log: BatchLog): BatchLog;
```

- [x] **Step 1：写 Map storage 红灯测试。**

```ts
const values = new Map<string, string>();
const fakeStorage: StateStorage = {
    getItem: name => values.get(name) ?? null,
    setItem: (name, value) => { values.set(name, value); },
    removeItem: name => { values.delete(name); },
};
```

覆盖损坏 JSON、version 不是1、owner/project/unit 不符、空 ID、重复 itemId/重复 shotId+type、非法 state、submitting 缺 taskId、恢复不自动 running、storage write 抛错和静默 no-op。

- [x] **Step 2：实现固定 scope adapter 与 key。**

```ts
const fixedStorage = storage ?? localForageStorageForScope(identity.ownerScope);
const key = `yingce:chapter-image-batch:v1:${encodeURIComponent(identity.projectId)}:${encodeURIComponent(identity.unitId)}`;
```

不要再次调用 scopedStorageKey。存储 key 本身不含敏感正文。各写入串行排队，避免提交回执比旧快照先写后又被旧快照覆盖。

- [x] **Step 3：只序列化显式白名单字段，写后回读校验。**

```ts
const encoded = JSON.stringify({
    version: 1, ownerScope: log.ownerScope, projectId: log.projectId, unitId: log.unitId,
    batchId: log.batchId, stage: log.stage, scope: log.scope,
    settings: { model: log.settings.model, size: log.settings.size, quality: log.settings.quality, skillIds: [...log.settings.skillIds] },
    state: log.state,
    items: log.items.map(item => ({
        itemId: item.itemId, shotId: item.shotId, revisionId: item.revisionId,
        fingerprint: item.fingerprint, operation: item.operation, state: item.state,
        taskId: item.taskId, attempts: item.attempts,
        taskAttemptsBefore: item.taskAttemptsBefore, taskUpdatedAtBefore: item.taskUpdatedAtBefore,
        updatedAt: item.updatedAt, issueCode: item.issueCode,
    })),
    createdAt: log.createdAt, updatedAt: log.updatedAt,
});
await fixedStorage.setItem(key, encoded);
if (await fixedStorage.getItem(key) !== encoded) throw new Error("批次记录未可靠保存，已停止新增提交");
```

写后校验是必要的：既有 adapter 在无可用浏览器存储或 persistence suppressed 时可能直接 return。不要把这种情况认定持久化成功。

- [x] **Step 4：实现恢复状态转换。** running/waiting 一律变 paused；所有 submitting 都变 unknown，包含已有原taskId的retry请求；有ID不代表这一次重试已受理。已有 taskId 保留，后续只查询该 ID。pending 不自动提交，stopped 不自动恢复。unknown 记录不删除，不允许新批次绕开它。
- [x] **Step 5：错误不伪装成空批次。** 无记录返回 null，损坏记录报明确错误且禁止启动；UI 可显式清理“无法解析且无可确认任务”的本地记录，但必须提示历史任务仍需任务中心核对，不能自动清理后启动。成功/停止批次只有在所有 unknown 已解决、全部已提交任务已核对后才可被下一次确认替换。
- [x] **Step 6：绿灯。** `bun test test/workflow-batch-storage.test.ts`；测试 serialized text 不含 channels/apiKey/prompt/referenceImages/inputJson，A 的迟到写入仍只到 A key，B 读取不可见。

## Task 6：单活动任务调度、限额等待与部分成功

**Files:**
- Create: `web/src/pages/projects/detail/workflow-batch-runner.ts`
- Create: `web/test/workflow-batch-runner.test.ts`

**Interfaces:**
- Consumes: 上述日志/类型、准备好的每镜提交 service、ApiError。
- Produces:

```ts
export type BatchRunnerDependencies = {
    save(log: BatchLog): Promise<void>;
    preflight(item: BatchItem, signal: AbortSignal): Promise<void>;
    submit(item: BatchItem, signal: AbortSignal): Promise<GenerationTask>;
    query(taskId: string, signal: AbortSignal): Promise<GenerationTask>;
    hasReadyArtifact(item: BatchItem, task: GenerationTask): Promise<boolean>;
    isOwnerCurrent(): boolean;
    sleep(ms: number, signal: AbortSignal): Promise<void>;
    now(): string;
    onChange(log: BatchLog): void;
};
export function createWorkflowBatchRunner(initial: BatchLog, deps: BatchRunnerDependencies): {
    start(signal: AbortSignal): Promise<void>;
    stopRemaining(): Promise<void>;
    snapshot(): BatchLog;
};
```

依赖注入只为隔离页面副作用和可控时序，不能扩展成插件式通用 queue。

- [x] **Step 1：写一个 deferred Promise 的红灯测试，证明不会一口气提交整章。** 测试里 submit 记录 shotId 并返回 queued task；query 的第一个任务保持 running，断言只有 s1 被提交；把它切 terminal 后只补 s2。使用 fake sleep 保存唤醒回调，不能靠真实等待数秒。
- [x] **Step 2：实现一条串行循环，提交前先保存 submitting。**

```ts
for (const item of log.items) {
    if (signal.aborted || !deps.isOwnerCurrent() || log.state === "stopped") break;
    if (item.state !== "pending") continue;
    await deps.preflight(item, signal);
    if (signal.aborted || !deps.isOwnerCurrent() || log.state === "stopped") break;
    item.state = "submitting";
    item.updatedAt = deps.now();
    await deps.save(log);
    // 必须再次检查：存储 await 期间可能停止、离开或换号。
    if (signal.aborted || !deps.isOwnerCurrent() || log.state === "stopped") {
        item.state = "pending";
        await deps.save(log);
        break;
    }
    // submit 的成功/拒绝/不确定及后续查询按下面状态表实现。
}
```

上面是循环控制核心，不是完整 runner；后续分支不得省略，全部由下列测试限定。

| 事件 | 状态和后续动作 |
| --- | --- |
| submit 返回 taskId | 立即保存 submitted+taskId，再观察；不能先 toast/刷新后才记 ID |
| queued/running | 同 ID 每2秒只读查询，不提交下一镜 |
| succeeded 且产物 ready/同 revision/同 taskId | succeeded；刷新一次工作区后补下一镜 |
| succeeded 但未见产物 | syncing；最多30秒观察产物，不新生成；超时暂停并保留 taskId，用户可“刷新状态” |
| failed/cancelled | failed；记录安全原因/任务入口后继续下一镜；不自动重试该项 |
| 容量 reason | item 回 pending，run waiting；2/4/8/16/30秒退避，最多5次，然后 paused |
| `42901/rate_limited` | 使用 retryAfterMs（存在时不得缩短），否则30秒；最多5次，然后 paused |
| 其他明确4xx拒绝 | blocked，整批 paused，显示修复后重新确认；不当容量错误继续狂发 |
| 网络/超时/5xx/不可判定失败 | unknown，整批 paused；不自动重放，即使 retryable=true |
| 查询失败 | 保留 taskId；只读查询可退避3次，仍失败则 paused，不重生成 |
| 持久化失败 | 立即停止补位；已发出请求保留内存回执并显示恢复风险；不得声称日志保存成功 |

- [x] **Step 3：集中实现允许自动重试的白名单判断。**

```ts
export function submissionRetryDelay(error: unknown, attempt: number): number | null {
    if (!(error instanceof ApiError) || attempt >= 5) return null;
    if (error.reason === TASK_SUBMISSION_REASONS.activeLimitExceeded && error.status === 400) {
        return [2000, 4000, 8000, 16000, 30000][attempt];
    }
    if (error.reason === TASK_SUBMISSION_REASONS.rateLimited && error.code === 42901 && error.status === 429) {
        return error.retryAfterMs ?? 30000;
    }
    return null;
}
```

attempt 是该项连续明确拒绝次数，成功受理后归零。退避结束先执行只读 preflight，用户停止/换号后不得继续。对取消 abort 发生于已进入 submit 的情形按 unknown 处理，不据此判断服务器没接单。

- [x] **Step 4：实现停止语义。** stopRemaining 立刻设置 stopped；尚未进入 POST 的 pending 标 stopped；正在提交的不标取消，等待其回执并记录 taskId（或unknown）；已经 queued/running 继续观察且不补位。按钮文字「停止剩余提交」，说明「已提交任务仍继续运行」，不调用 cancelGenerationTask。
- [x] **Step 5：覆盖部分成功与等待边界。** 测试 s1成功/s2失败/s3成功最终数量正确；开始双击只跑一个循环；容量只重试该项；invalid_argument不同msg不重试；Retry-After为60秒不得变30秒；停止发生在 preflight、日志写入、submit、poll、sleep 五个窗口；unknown永远不调用第二次submit。
- [x] **Step 6：绿灯。** `bun test test/workflow-batch-runner.test.ts`；不得在该任务顺手提高全局限额或调整 backend worker。

## Task 7：恢复、失败重试、账号与多标签生命周期

**Files:**
- Create: `web/src/pages/projects/detail/use-workflow-image-batch.ts`
- Modify: `workflow-batch-runner.ts`、`workflow-batch-runner.test.ts`
- Modify: `workflow-batch-storage.test.ts`
- Test: `web/test/workflow-image-batch.test.tsx` 的 lifecycle 组

**Interfaces:**
- Consumes: `getActiveUserScope`、`useUserStore`、`runGenerationConsumer`、Web Locks、任务/query/project API。
- Produces:

```ts
export type WorkflowImageBatchController = {
    stage: BatchImageStage;
    settings: BatchImageSettings;
    scope: BatchScope;
    log: BatchLog | null;
    preview: BatchPreview | null;
    busy: boolean;
    blockedReason: string | null;
    canConfirm: boolean;
    chapterMutationLocked: boolean;
    lockedShotIds: ReadonlySet<string>;
    open(stage: BatchImageStage): Promise<void>;
    refreshPreview(scope: BatchScope, settings: BatchImageSettings): Promise<void>;
    confirm(): Promise<void>;
    stopRemaining(): Promise<void>;
    refreshStatus(): Promise<void>;
    closeDialog(): void;
    dialogOpen: boolean;
};
export function useWorkflowImageBatch(input: {
    detail: ProjectDetail;
    projectId: string;
    unitId: string;
    editorDirty: boolean;
    config: AiConfig;
    skills: Skill[];
    initialSettings: BatchImageSettings;
    onRefresh(): Promise<void>;
}): WorkflowImageBatchController;
```

- [x] **Step 1：红灯覆盖页面重开与账号变化。** 已 taskId 的日志重开后 query 原 ID，零 create；用户未确认继续时 pending 不动；A→B→A 旧 promise 回来不能改变新 epoch 或补位；hydrated=false 和 scope/user不一致不能开始。
- [x] **Step 2：每次消费动作接入现有账号 drain，不包住无限批次循环。**

```ts
const task = await runGenerationConsumer(signal, async consumerSignal => {
    if (!isOwnerCurrent()) throw new DOMException("Aborted", "AbortError");
    return submitBackendGenerationTask({ ...options, signal: consumerSignal });
});
```

日志 owner 在运行前固定；跨 await 后检查 epoch、hydrated、user.id、activeScope。已经发出的旧账号请求可以只写旧 scope 的 taskId日志，不能更新新账号UI或发下一镜。查询也使用 signal 和 owner fence。不要修改现有 user-session 来添加重复的账号切换架构。

- [x] **Step 3：原生 Web Lock 覆盖本账号本章节两种图片批次。**

```ts
const lockName = `yingce:chapter-image-batch:${ownerScope}:${projectId}:${unitId}`;
await navigator.locks.request(lockName, { ifAvailable: true }, async lock => {
    if (!lock) throw new Error("本章已在其他标签页处理批量任务");
    // 获锁后必须重新 read 日志和服务端状态，再决定 start/observe。
    await runner.start(signal);
});
```

执行开始、显式恢复、处理日志变更都在同一锁内；组件读取仅观察不写。锁持有到当前 runner 观察/补位结束或停止/卸载；页面退出 native lock 释放，submitting日志用于下次unknown判定。没有 `navigator.locks` 时禁用批量，不退化为存在竞态的 storage 自旋锁。锁不能提供跨设备保证，UI/文档必须如实说明。

- [x] **Step 4：工作流初始化与补位前校验。** 确认后从最新 workspace 中找到 `instance.unitId` 对应且含 previz 的工作流，按目标 stage 而不是当前 activeStage 选 step。缺失才 `createUnitWorkflow` 一次，随后刷新；创建失败停止，不能每镜重复初始化。step failed 显示不可用，不自动重置工作流。每次提交检查项目仍active、镜头仍在本章、revision/hash未变、没有同类型在途/有效成品、模型可用。
- [x] **Step 5：任务列表截断保守处理。** 用 `listGenerationTasks(100,{ projectId, activeOnly:true })` 辅助发现别处在途；返回100条时不声称已穷尽，暂停批量并说明无法完整确认在途状态。已知taskId一律直接 query，不能依赖最近100历史。仅把账户activeTaskLimit用于说明，不把本章任务数当全账户余量。
- [x] **Step 6：显式失败重试使用原ID。** `failed` 范围先 query 详情；`canRecoverMedia` 为true去任务中心“重试保存”；billing uncertain/cancel requested/uncertain阻止；原metadata中的shot/revision/artifactType必须匹配。提交retry前把原task的attempts/updatedAt保存到 `taskAttemptsBefore`/`taskUpdatedAtBefore`；`retryGenerationTask(taskId)` 重新入队同ID，返回之后照普通已提交任务观察。若retry响应丢失，查询到queued/running或确切晚于基线的新终态才可正向认领；仍是原failed且没有变化不证明retry没受理，必须保持unknown，不能再次自动retry。不要通过创建新任务绕过后端内容审核、账单或取消限制。原设置无法报价时说明未知。任务成功但保存失败不重新生图。
- [x] **Step 7：未知提交只能正向认领，查无不是未创建。** “核对任务”从当前workspace/最近列表发现同shot+type候选，再GET详情核对 `chapterImageBatchId` 和 `chapterImageBatchItemId`；恰好一个匹配才把taskId写入日志。无匹配或多个匹配继续unknown，禁止该批次重放；提供任务中心入口。可输入确切任务ID做同样归属/metadata校验；不能允许“我确认没生成”直接绕过不确定项。
- [x] **Step 8：恢复确认重新计算剩余候选与费用。** 已submitted任务只观察不计入新提交报价；pending重新比较指纹，变更条目blocked；设置变更产生新的确认token，不复用旧批准。当前失败重试次数/费用明确显示；停止的剩余项只有用户重新确认才恢复pending。活跃taskId、unknown未解决前不允许覆盖日志创建另一个同章批次。
- [x] **Step 9：运行生命周期专项。** `bun test test/workflow-batch-runner.test.ts test/workflow-batch-storage.test.ts`；DOM 测试用独立进程 `bun test test/workflow-image-batch.test.tsx -t 'lifecycle'`。范围只限本章控制，不新建跨页面任务中心。

## Task 8：批量确认弹窗与逐镜问题清单

**Files:**
- Create: `web/src/pages/projects/detail/workflow-image-batch-dialog.tsx`
- Test: `web/test/workflow-image-batch.test.tsx`

**Interfaces:**
- Consumes: `WorkflowImageBatchController`、`BatchImageSettings`、`ModelPicker`、`ImageSizePicker`、`SkillRuntimePicker`。
- Produces: 无业务写逻辑的弹窗，所有操作委托controller；关闭不停止已确认的本章批次。

- [x] **Step 1：先写弹窗行为红灯。** 点击storyboard/previz标题准确；视频页入口仍显示图片ModelPicker；关闭或取消零任务；无eligible禁用开始；改设置立即失效旧预览；失败模式锁定原任务设置；费用未知文字不显示免费。
- [x] **Step 2：用单个 AppModal 实现两种目标，避免复制两套 UI。** 组件签名为 `WorkflowImageBatchDialog({ controller, config, skills }: { controller: WorkflowImageBatchController; config: AiConfig; skills: Skill[] })`；先从controller解构 `stage`、`canConfirm`，设置变化调用 `refreshPreview`，不由弹窗自己发任务。

```tsx
<AppModal
    flush
    open={controller.dialogOpen}
    title={stage === "previz" ? "批量生成动作预演" : "批量生成分镜图"}
    onCancel={controller.closeDialog}
    footer={null}
    width="min(880px, calc(100vw - 32px))"
>
    <div className="workflow-batch-dialog">
        <p>仅处理当前章节；每镜提交一个图片任务。</p>
        {stage === "previz" ? <p>动作预演为黑白图片，不是视频，不自动使用已生成分镜图。</p> : null}
        <div className="workflow-batch-dialog-controls">{settingsControls}</div>
        <div className="workflow-batch-dialog-list" role="region" aria-label="批量镜头检查结果">{candidateRows}</div>
        <div role="status" aria-live="polite">{priceAndCountSummary}</div>
        <footer>
            <Button onClick={controller.closeDialog}>取消</Button>
            <Button type="primary" disabled={!canConfirm} loading={controller.busy} onClick={() => void controller.confirm()}>
                确认提交 {controller.preview?.items.length ?? 0} 个任务
            </Button>
        </footer>
    </div>
</AppModal>
```

`settingsControls` 是本组件内实际 JSX：三个范围选项、图片模型、画幅/画质、技能；`candidateRows` 按镜号展示名称、状态、跳过/阻止理由；`priceAndCountSummary` 展示可提交/已完成/在途/过期/失败/阻止计数及任务4价格文案。这三个局部变量不得保留为空内容。

- [x] **Step 3：准确的禁用与警告。** 无镜头/项目归档/账号未就绪/存储不可用/缺WebLocks/有未解决日志/当前脚本dirty时入口给出原因。部分镜头blocked允许提交其余eligible，但确认按钮任务数必须准确，顶部显示跳过数量，不偷偷缩减而不提示。参考资产无图沿用现有“仅用可用图片”语义，作为可见警告；失效token则硬阻止。
- [x] **Step 4：运行态内容。** 显示待提交、提交中、已受理/生成中、待产物同步、成功、失败、阻止、停止；“停止剩余提交”“刷新状态”“重试失败”“继续剩余”各自只在相应状态出现。重试/继续必须回到确认，不直接执行收费动作。
- [x] **Step 5：测试焦点和关闭。** Escape关闭、焦点回入口；按钮可Tab到达；status用aria-live polite，不每秒播报整张列表。`App.useApp().modal` 可用于必要警告，不使用静态 Modal.confirm。
- [x] **Step 6：绿灯。** `bun test test/workflow-image-batch.test.tsx -t 'dialog'`。不新增页面级全局 .ant-modal-* 覆盖。

## Task 9：工作台本章操作区、进度与响应式集成

**Files:**
- Modify: `web/src/pages/projects/detail/workflow-production-workbench.tsx`
- Modify: `web/src/pages/projects/detail/workflow.css`
- Modify: `web/src/pages/projects/detail/workflow.tsx`（仅生命周期 key 必要时）
- Test: `web/test/workflow-image-batch.test.tsx`
- Preserve: `web/test/workflow-shot-generation.test.ts` 的既有时间轴覆盖

**Interfaces:**
- Consumes: `useWorkflowImageBatch`、批量弹窗，已有单镜选择与编辑状态。
- Produces: 真实工作台入口，既有单镜流程不回归。

- [x] **Step 1：真实 workbench 渲染红灯。** 断言三按钮均存在、顺序相邻、分镜/预演/视频页入口均传正确固定stage；未保存时阻止；空章节不创建任务。用真实组件+axios adapter，不仅字符串搜索源码。
- [x] **Step 2：拆分本章与单镜操作行。** `batch` 为hook返回值，`chapterMutationLocked = batch.chapterMutationLocked`，`batchEntryDisabled = editorDirty || Boolean(batch.blockedReason) || batch.busy`；运行态禁止新建批次但保留“查看本章批次”入口。理由在可见状态文本中解释，不仅靠disabled按钮的tooltip。

```tsx
<div className="workflow-chapter-actions" role="group" aria-label="本章操作">
    <span className="workflow-chapter-actions-label">本章操作</span>
    <Button size="small" disabled={chapterMutationLocked} onClick={() => setBindingDrawerOpen(true)}>匹配镜头资产</Button>
    <Button size="small" disabled={batchEntryDisabled} onClick={() => void batch.open("storyboard")}>批量生成分镜图</Button>
    <Button size="small" disabled={batchEntryDisabled} onClick={() => void batch.open("previz")}>批量生成动作预演</Button>
</div>
```

原镜头编号/名称/保存状态/上一镜下一镜留在 `.workflow-panel-header`；「生成章节分镜」是替换脚本的操作，保留独立次级位置，不混成第四个生图按钮。三按钮均不设primary；主确认只在弹窗内。

- [x] **Step 3：局部样式使用现有变量，独立换行。**

```css
.workflow-chapter-actions {
    display: flex;
    flex: 0 0 auto;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    padding: var(--space-2) var(--space-3-half);
    border-bottom: 1px solid var(--workspace-border);
    background: var(--workspace-surface);
}
.workflow-chapter-actions-label {
    font-size: var(--fs-caption);
    color: var(--muted-foreground);
}
.workflow-batch-dialog {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    max-height: 80dvh;
    min-height: 0;
}
.workflow-batch-dialog-list {
    min-height: 0;
    overflow-y: auto;
}
.workflow-batch-dialog footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-2);
}
```

新增选择器前搜索同名规则；`--muted-foreground` 在执行时核对既有token。手机不能因固定高度裁掉弹窗底部；沿现有760/1050/1280断点验证，不删除左右栏已有策略。

- [x] **Step 4：本章进度条放操作区下方，双槽时间轴只补精确状态。** 进度依 `batch.log` 始终可见，不能因 `!selectedShot` 提前return而把孤立批次藏掉；最后一个镜头被外部删除时仍显示批次的停止/核对入口。使用 `(shotId,artifactType)` 映射，不复用 `Set<shotId>` 把storyboard提交标成previz。已有单镜submitting保持兼容，批次局部pending显示“待提交”而不是伪造后端queued。不要改现有时间轴拖拽高度、媒体展示及视频分支异类回退语义。
- [x] **Step 5：阻止竞争写入。** 本批未结束涉及镜头的保存/删除/绑定/解绑/单镜生成按钮给明确原因；用户仍能切换查看。先停止剩余才能执行章节资产匹配或替换脚本。当前在途任务完成后才能解除其镜头写锁；不对其他章节/其他未涉及镜头施加全局禁用。
- [x] **Step 6：章节身份重置。** 如果工作台会在 unitId变化时复用实例，为生产组件添加 `key={`${projectId}:${unit.id}`}`，stage切换不加key以免每次切图片/视频都中断批次。hook cleanup abort观察/补位并保留日志；未知请求不标成停止成功。
- [x] **Step 7：绿灯。** `bun test test/workflow-image-batch.test.tsx -t 'workbench'`，另跑 `bun test test/workflow-shot-generation.test.ts`。

## Task 10：真实调用链与异常矩阵回归

**Files:**
- Complete: `web/test/workflow-image-batch.test.tsx`
- Complete: 新增的 plan/storage/runner/image-generation 测试
- Modify only if required: 相邻既有测试，不能删除原断言能力

**Interfaces:**
- Consumes: 真实工作台、hook、service；只 mock 传输、时钟、存储和供应商结果。
- Produces: 可重复执行的端到端前端合同测试，不需要真实付费模型。

- [x] **Step 1：建立 Happy DOM harness，沿用 `chapter-asset-binding-drawer.test.tsx:16-73,123-175`。** 先安装window/document/navigator/DOM构造器/localStorage/sessionStorage/ResizeObserver/RAF，再动态import React/ReactDOM/AntD/Query/router/真实workbench；组件包裹 MemoryRouter→ConfigProvider(motion=false)→App→QueryClientProvider(retry=false)。每例设置 `setActiveUserScope("user-a")` 与 hydrated user store；finally卸载、clear、恢复adapter/user/scope/globals。
- [x] **Step 2：Axios adapter 拒绝未知请求。**

```ts
apiClient.defaults.adapter = async config => {
    const fixture = respondToBatchRequest(config);
    return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: fixture, msg: "ok" } };
};
```

`respondToBatchRequest` 在本测试文件定义 switch(method+pathname)，仅处理项目core/workspace、任务POST/GET/retry、工作流初始化、必要模型/技能读请求；其余抛错。记录真实请求，不 mock `generateArtifact`/controller 来绕过集成。错误响应用 AxiosError+真实status/data走 `http` 解包。

- [x] **Step 3：实现下表全部关键场景，每项具名test，不只截图。**

| 场景 | 断言 |
| --- | --- |
| 两镜不同文案/资产 | POST metadata/参考图/提示词分别正确；无镜头保存请求 |
| 从视频页打开预演批量 | mode=image，预演step，action_board；无视频模型污染 |
| ready/active/failed/stale/missing混合 | 默认只提交missing，确认数量与实际一致 |
| 无当前修订 | blocked；没有自动保存或回退旧revision |
| 修改当前脚本未保存 | 点击入口零预览写/零生成；提示保存后再试 |
| 相同内容连续点开始 | create次数不翻倍；只一个runner/lock |
| 首个任务running | 下一镜零提交；终态后补一个 |
| 容量满与429 | 正确退避；其间停止/换号不会继续 |
| 确定性拒绝/余额不足 | 保留已提交项；停止剩余，不解析msg |
| POST已接单但响应丢失 | 日志unknown；刷新/重开均不重发 |
| POST中刷新/卸载 | submitting恢复unknown；不能当pending |
| 已知ID在最近100条之外 | GET该ID恢复，不新建 |
| 已成功但产物稍晚出现 | syncing，不能报告图片已生成或提交第二次 |
| 产物变stale/镜头版本变化 | 不算有效成功；剩余项重新预检/阻止 |
| 失败项重试 | 仅POST原id/retry，canRecoverMedia走提示而非新生成 |
| 重试刚被其他请求接受 | 重新query原id，不改为新create |
| retry响应丢失但旧taskId仍为failed | 比较重试前attempts/updatedAt，不能把旧失败当本次完成或允许自动再retry |
| 停止剩余 | 已发任务继续、未发不再POST；停止不是退款承诺 |
| 存储写失败/静默跳过 | 提交前失败零POST；提交后失败停止补位并保留内存ID |
| 两标签锁竞争 | 第二个confirm零POST；获锁后重新读日志，不能用锁前快照 |
| 用户A→B→A | 迟到回执只写原scope；新UI/下一项不受旧epoch驱动 |
| 历史查询无匹配 | unknown保持；不以“没有查到”判定可重发 |
| 全部ready/blocked/空章 | 合理空态，开始禁用，无伪成功 |

- [x] **Step 4：费用请求与样式非逻辑检查。** 部分unknown费用仍标未知；已知0确实显示0但不推断免费；旧报价请求返回不能覆盖新设置；scope隔离缓存。按钮顺序及group标签用DOM断言，布局尺寸仍交真实浏览器。
- [x] **Step 5：分进程运行，避免DOM全局与SSR测试污染。**

```bash
cd web
bun test test/workflow-image-generation.test.ts test/workflow-batch-plan.test.ts test/workflow-batch-storage.test.ts test/workflow-batch-runner.test.ts
bun test test/workflow-image-batch.test.tsx
bun test test/workflow-shot-generation.test.ts
bun test test/chapter-asset-binding-drawer.test.tsx
bun test test/chapter-generated-workflow-entry.test.tsx
bun run typecheck
bun run lint
bun run build
```

预期专项全过、类型/lint/build成功；如果存在用户并行修改导致失败，定位文件/原始错误，不能修改无关模块或称全通过。连续同类失败三次停止盲试并记录新假设。

## Task 11：真实路由浏览器验收

**Files:** 不新增假页面；截图/记录放 `.local/` 受忽略目录，待测试文档只写安全摘要。

**Interfaces:**
- Consumes: 真实 `/projects/:projectId/workflow/:unitId/{storyboard,previz,video}`。
- Produces: 可追溯的布局与交互验收记录；真实模型无授权则明确待验收。

- [x] **Step 1：检查服务和环境。** 阅读当前 `.env.example`、实际启动脚本/端口与进程；不要沿用记忆直接重启。宿主后端只可使用 `.local/project-workbench-debug`，不可触碰backend/data或生产库。（已只读检查：端口 `3000`/`5173`/`8178`/`18178` 均无运行中的前后端开发实例，`8080` 为 `com.docker.backend`，未启动或重启任何服务。）
- [x] **Step 2：使用官方 browser-use 能力检查已有实例。** 初版无可用实例；用户要求补齐待完成项后，补修轮启动仅本机可访问的真实 Vite 应用及独立受控 API，以官方 IAB 验证真实路由，不使用仿制 HTML，不访问真实用户数据库或付费供应商。证据见 `.local/short-drama-batch-gap-20261029/browser-verification.md`。
- [ ] **Step 3：明暗主题各测1680、1280、1050、760与窄手机宽度。** 补修已检查真实路由的浅色弹窗及深色工作台五档宽度（手机390px），无整页横向溢出、三按钮自然换行；修复720px高候选列表只有1px的问题，候选和费用/确认区可见。受IAB后台时间线冻结影响，仅完成有限动画到最终态，天然动效、时间轴高度拖拽及所有明暗/阶段组合仍未完整人工验收，因此保留未全勾选。
- [ ] **Step 4：无付费操作的交互。** 打开两种批量，切范围/图片模型/规格/技能，检查数量、警告、未知费用与键盘焦点；取消零任务；dirty提示、空章/归档状态准确。（补修已在真实路由打开两种图片弹窗、确认12镜数量/未知费用/预演图片说明、取消及受控提交；键盘焦点循环和全部设置操作未逐一完成前台人工验收，保留剩余人工项。真实组件测试另有Escape/焦点、参数切换与dirty回归，不冒充浏览器全覆盖。）
- [ ] **Step 5：在授权的测试渠道或已有模拟环境验收队列。** 准备有ready/failed/stale/active/missing的章节；确认少量样本，观察补位、部分失败、停止、刷新后显式继续、另标签只读/阻止。真实收费模型未授权不能提交整章或用高价视频替代。（补修已用真实路由+独立受控API观察串行补位和模拟资源回填，停止11/12批次后刷新仍stopped、未自动补最后一镜；实际供应商输出/计费/对象存储故障和生产多标签网络验收未执行。没有付费模型授权，不用模拟结果替代供应商验收。）
- [x] **Step 6：记录真实结果。** 每项标通过/失败/未执行及原因。浏览器布局检查、自动化合同测试和真实供应商生成分别列出，不相互替代。

## Task 12：专题文档、收尾审查与交接

**Files:**
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/backend/code-map.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`
- Modify: `docs/content/docs/progress/todo.mdx`（仅已有相关条目时同步）
- Confirm: `docs/content/docs/backend/http-api.mdx`、`backend/internal/handler/openapi.yaml` 的任务1合同

**Interfaces:**
- Consumes: 已实现功能、真实测试与浏览器结果。
- Produces: 用户可以理解的能力边界与未验收清单，不把计划当已上线。

- [x] **Step 1：功能文档写入准确描述。**

```md
## 短剧当前章节批量图片生成

分镜制作支持在「匹配镜头资产」旁分别启动本章分镜图、动作预演批量生成。
默认只补缺；失败重试和过期更新需单独选择并确认。动作预演为黑白图片，
不自动引用已生成分镜图，也不生成视频。每次确认展示实际镜头数、图片设置、
参考图警告与可获得的费用预估；费用未知不代表免费。

任务逐镜进入既有队列。停止剩余提交不取消已接受任务；刷新或离开章节后
不再自动补位，返回需重新确认。提交结果不确定时不会自动重发。
恢复记录仅保存在当前浏览器的用户隔离缓存，不是服务端批次或跨设备幂等保证。
```

- [x] **Step 2：code-map标注入口和职责。** 指向新增plan/storage/runner/hook/dialog及共享image构造；明确旧画布batch不负责项目镜头，`runBackendGenerationTaskBatch`不是此入口。
- [x] **Step 3：pending-test逐条登记未实测路径。** 至少保留真模型分镜/预演、限额等待、任务保存失败恢复、跨标签、账号切换、刷新、窄屏/明暗；用户未确认的变化不能直接标用户验收通过。
- [x] **Step 4：错误合同文档写清四类差异。** `task_active_limit_exceeded`明确未入队/未预占；`rate_limited`请求频率；其他invalid_argument不能自动退避；网络/超时不确定不得自动POST重放。OpenAPI只补现有信封reason，不生成新客户端。
- [x] **Step 5：最终diff审查。** 确认本轮没有修改数据库表、队列全局额度、供应商协议、资产绑定算法或画布性能文件；没有提交密钥/本地DB/截图到产品目录；共享helper的单镜调用和batch类型同步。
- [x] **Step 6：执行并记录最终最小验证。**

```bash
cd backend
go test ./internal/app ./internal/handler -run 'TestTaskActiveLimit' -count=1
cd ../web
bun test test/workflow-image-generation.test.ts test/workflow-batch-plan.test.ts test/workflow-batch-storage.test.ts test/workflow-batch-runner.test.ts
bun test test/workflow-image-batch.test.tsx
bun test test/workflow-shot-generation.test.ts
bun run typecheck
bun run lint
bun run build
```

如果本轮修改了API文档MDX，仅在文档风险需要时追加docs的`bun run types:check`，不把根目录或前端命令拿到docs执行。

- [x] **Step 7：向用户交付完成情况，而不是下一步承诺。** 列实际新增入口、默认范围、队列/恢复边界、测试结果、未执行验收、文件链接；有阻塞逐条说明，不将部分实现称“全量完成”。

## 4. 验收总表（最终合并门槛）

| 要求 | 对应任务 | 必须证据 |
| --- | --- | --- |
| 两个按钮紧邻匹配资产、当前章节语义 | 8、9、11 | DOM + 真实路由布局 |
| 分镜/预演/视频页均不误用视频参数 | 2、8、9、10 | 实际POST payload |
| 每镜脚本/资产/修订隔离 | 2、3、10 | 两镜不同数据测试 |
| 不逐镜保存导致已有产物stale | 2、10 | 零save请求 + 后端行为依据 |
| 缺失/失败/过期区分，ready/active跳过 | 3、10 | 分类矩阵 |
| 费用未知不冒充免费、确认后才写 | 4、8、10 | 请求与文案测试 |
| 容量reason传达且不误判其他错误 | 1、6、10 | Go HTTP + 前端分类 |
| 一项活动/额度等待/部分失败不中断已完成 | 6、10 | 可控时序测试 |
| 停止仅影响未提交 | 6、10、11 | 五个时序窗口 |
| 未知提交不自动重放 | 5–7、10 | response丢失/刷新测试 |
| failed走原任务retry，保存失败不重生 | 7、10 | API调用路径 |
| localforage白名单、用户隔离与写失败 | 5、7、10 | Map storage + scope测试 |
| A→B→A和同源标签防重 | 7、10、11 | epoch + drain + lock测试 |
| 没有跨设备幂等/后台继续的虚假承诺 | 7、12 | 功能文档 |
| 保留已有双槽和单镜视频行为 | 2、9、10 | 相邻回归 |
| 专题文档与待验收项同步 | 12 | 文档diff |

## 5. 明确不纳入与风险上限

- 不新增服务端 batch 表/REST API、跨设备唯一提交键、分布式锁、全剧批次、后台继续执行。若后续要求关闭浏览器仍自动生成，需独立服务端编排设计，不能把本地日志包装成后台任务。
- 原生WebLocks只覆盖同源浏览器上下文。不同设备仍可能并发提交；服务端额度不是每镜唯一性约束。每次补位查最新状态降低重复，不声称完全消除跨设备竞态。
- 普通任务列表被截断，不构成否定证据；未知提交可能需要用户提供任务ID或进一步后台排查，第一版宁可暂停也不能重复计费。
- 当前单镜生成依然可能保存新修订；本计划仅确保批量不这样做，不顺带重构全部版本编辑行为。
- 预演不基于分镜图，黑白也依赖提示词/当前显示方式；本计划不承诺内容一致性、固定动作格数或产物美术质量改善。
- 本地日志只保留当前章最近一个可恢复批次，完成后由下一次用户确认替换；不建设批次历史管理页面。
- 无真实模型授权时，只交付代码/合同/交互验证，模型输出质量和计费真实账单保留待验收。

## 6. 执行前后检查与交接指令

### 执行前

- [x] 阅读本计划、根AGENTS与当前涉及文件diff；当前工作区存在其他agent并行改动。
- [x] 只读 `git status --short` 和 `git diff --` 本计划目标文件；不要创建旧HEAD worktree后假设包含当前未提交实现。
- [x] 若需隔离worktree，先明确如何携带本任务依赖的未提交变更；不得自动stash/reset或覆盖用户成果。
- [x] 逐任务维护勾选，验证记录实际命令/结果；不批量勾选未完成项。

### 获得用户Git提交授权后才使用的粒度

1. `fix(tasks): 任务容量 - 保留活动上限拒绝原因`
2. `refactor(project): 分镜图片 - 复用逐镜请求构造与候选预检`
3. `feat(project): 短剧批量图片 - 增加本章调度与恢复记录`
4. `feat(project): 短剧制作 - 增加批量确认与相邻操作区`
5. `test(project): 短剧批量图片 - 补齐异常回归与验收记录`

不能使用 `git add .` 把其他会话变更一起带入；计划目录被 `.gitignore` 的 `docs/*` 忽略，如获授权要提交此计划，单独审阅后针对本文件 `git add -f`，不改宽泛忽略规则。

### 可直接发送给实施agent

> 请使用 executing-plans 或 subagent-driven-development，完整执行 `docs/superpowers/plans/2026-10-29-short-drama-batch-generation.md` 的12项任务。先读取AGENTS和当前工作区diff，保留所有既有修改；仅实现本章批量分镜图/动作预演，不扩大到全剧/视频/新引擎。按任务进行测试与审查，特别验证不隐式新建镜头修订、容量错误合同、未知提交不重放、账号隔离与刷新恢复。真实收费模型调用、服务启动/重启、Git提交/推送均按当前用户授权执行；不能执行的验收明确记录。交付实际完成项、测试结果和剩余阻塞，不把计划文档当已实现。

## 附录A：任务2可直接使用的基础测试夹具

写入 `web/test/helpers/workflow-batch-fixture.ts`。数据只用于本地模拟，不发真实供应商请求；模型Key是测试值。测试可修改返回的对象构造特殊场景，不在产品代码加入这些fixture。

```ts
import type { ProjectDetail, ShotArtifact } from "../../src/services/api/projects";
import type { GenerationTask, TaskStatus } from "../../src/services/api/task-center";
import { createModelChannel, defaultConfig, encodeChannelModel } from "../../src/stores/use-config-store";

const now = "2026-10-29T00:00:00.000Z";

export function makeImageConfig() {
    const modelKey = "doubao-seedream-4-0-250828";
    const channel = createModelChannel({
        id: "test-image", name: "测试图片渠道",
        baseUrl: "https://ark.cn-beijing.volces.com/api/plan/v3",
        apiKey: "test-key", interfaceType: "volcengine-ark-agent-plan-image",
        models: [modelKey],
        modelCosts: [{
            model: modelKey, capability: "image", protocol: "volcengine-ark-agent-plan-image",
            billingMode: "fixed_request", unitPriceMicrocredits: 1,
        }],
    });
    const model = encodeChannelModel(channel.id, modelKey);
    return { ...defaultConfig, channels: [channel], model, imageModel: model, baseUrl: channel.baseUrl };
}

export function makeWorkflowBatchDetail(): ProjectDetail {
    const shots = [1, 2].map(n => ({
        id: `s${n}`, projectId: "p1", unitId: "u1", currentRevisionId: `r${n}`,
        title: `镜头0${n}`, description: `镜头${n}画面`, position: n - 1,
        durationMs: 3000, status: "draft", createdAt: now, updatedAt: now,
    }));
    return {
        project: {
            id: "p1", userId: "user-a", name: "测试短剧", type: "short_drama",
            aspectRatio: "16:9", sourceType: "manual", description: "测试项目",
            stylePresetId: "", status: "active", revision: 1, createdAt: now, updatedAt: now,
        },
        units: [{
            id: "u1", projectId: "p1", kind: "chapter", title: "第一章",
            sourceText: "测试章节", wordCount: 4, status: "ready", position: 0,
            createdAt: now, updatedAt: now,
        }],
        shots,
        shotRevisions: shots.map((shot, index) => ({
            id: `r${index + 1}`, shotId: shot.id, version: 1,
            plotDescription: shot.description, action: `动作${index + 1}`, dialogue: "",
            shotSize: "中景", cameraAngle: "平视", cameraMovement: "固定",
            durationMs: 3000, imagePrompt: `独立画面${index + 1}`, videoPrompt: "动作描述",
            negativePrompt: "", continuityNotes: "", actionBeatsJson: "[]", createdAt: now,
        })),
        workflows: [{
            instance: { id: "wf1", projectId: "p1", unitId: "u1", scope: "unit", status: "active", revision: 1 },
            steps: ["storyboard", "previz", "video"].map((stepKey, position) => ({
                id: `step-${stepKey}`, workflowInstanceId: "wf1", stepKey, name: stepKey,
                position, status: "ready", updatedAt: now,
            })),
        }],
        canvases: [], canvasUnitLinks: [], assets: [], assetFolders: [],
        shotArtifacts: [], shotReferences: [], assetCandidates: [], tasks: [],
    };
}

export function makeBatchTask(shotId: string, status: TaskStatus, artifactType: string): GenerationTask {
    return {
        id: `task-${shotId}-${artifactType}`, projectId: "p1", type: "canvas_image", status,
        prompt: "测试图片", attempts: 0, createdAt: now, updatedAt: now,
        clientContext: {
            domainProjectId: "p1", shotId, artifactType,
            workflowStepId: artifactType === "action_board" ? "step-previz" : "step-storyboard",
        },
        inputJson: JSON.stringify({ metadata: {
            domainProjectId: "p1", unitId: "u1", shotId,
            shotRevisionId: shotId.replace(/^s/, "r"), artifactType,
        } }),
    };
}

export function makeBatchArtifact(shotId: string, status: string, type: string): ShotArtifact {
    return {
        id: `artifact-${shotId}-${type}`, projectId: "p1", unitId: "u1", shotId,
        revisionId: shotId.replace(/^s/, "r"), taskId: `task-${shotId}-${type}`,
        type, version: 1, resourceId: `resource-${shotId}-${type}`, status,
        selected: status === "ready", metadataJson: "{}", createdAt: now, updatedAt: now,
    };
}
```

## 7. 计划自审记录

- 范围覆盖：本章入口、逐镜版本、资产隔离、费用、限额、部分成功、重试/恢复保存、停止、刷新、账号、多标签、响应式、回归与专题文档均已映射到任务。
- 类型一致：统一使用 BatchImageStage/BatchArtifactType/BatchScope/BatchLog/BatchPreview；后端任务状态与本地队列状态分开；现有API名为 retryGenerationTask 而非 retryTask。
- 真实依据：已核实每次save会增revision并使既有产物stale；已核实GET列表截断、metadata投影限制、容量错误当前被handler抹平，计划没有假设这些能力已存在。
- 削减范围：不创建通用批量引擎/数据库表；单活动任务不提供并发调节UI；无多选、无全剧、无默认重做成品；不过度承诺跨设备幂等。
- 历史规划阶段边界：首次交付仅编写本文，未实施或运行验证；此句不代表后续实施状态。初版执行记录与独立审查存在差异，具体以文末补修验证记录、当前测试断言及真实浏览器证据为准。Git 提交与真实付费模型调用须单独授权。

## 8. 独立审查后的补修与最终验证记录

### 8.1 已闭合的实现缺口

- [x] 恢复保存、取消未确认、账单不确定跨 `missing/failed/stale` 全部阻止新生成，检查所有同镜同类型任务；账单字段改读真实 `task.billing.status`。
- [x] 同章 Web Locks、共享存储写队列与原始快照比较保护所有日志写入；同批次旧恢复预览、旧刷新、第二观察者不能覆盖 `unknown` 或新 `taskId`。
- [x] 提交准备与实际 POST 分界明确；技能读取、媒体准备、retry GET、基线持久化、首次确认初始化期间停止均可阻止尚未发出的请求，已发请求保留回执/unknown。
- [x] 父级确认捕获发起时 controller，跨章后拒绝旧确认；工作台、剧情章节页生成保存和自动恢复统一调用 `saveGeneratedChapterStoryboardWithBatchGuard`，最终 PUT 前复核账号、章节与调用方有效期。
- [x] dirty 下允许查看、核对、停止已有批次，但不允许新预览/收费确认。首次确认尚无日志时也显示真实停止入口。
- [x] 无 Web Locks 只禁批量；无未结日志的普通单镜保存等操作保持可用，有未结日志继续保守阻止。
- [x] 修复低高度弹窗候选区被压到约1px的问题，设置/候选独立滚动、费用与确认区常驻；真实1280×720候选288px、390×844候选338px。
- [x] 后端容量竞态注入改为服务层计数之后，断言服务层与仓储层两次检查；媒体恢复夹具保持物理身份，以同端点故障恢复代替把OSS记录伪改local。
- [x] 自动化补齐真实时序断言：三镜中间拒绝后不发第三镜、running/syncing窗口不补位、POST中卸载重挂、列表真实省略task、A→B→A后旧回执、两份旧预览、实际停止/继续和父级/普通章节入口。
- [x] 独立最终审查首轮发现4项Important，修复后定向复审：Spec compliance通过、Quality批准，四项范围内无残留Critical/Important/Minor。

### 8.2 最终执行证据

| 命令/范围 | 实际结果 |
| --- | --- |
| `bun test --cwd web test/workflow-image-generation.test.ts test/workflow-batch-plan.test.ts test/workflow-batch-storage.test.ts test/workflow-batch-runner.test.ts` | 25 pass，248 assertions |
| `bun test --cwd web test/workflow-image-batch.test.tsx` | 35 pass，204 assertions |
| `bun test --cwd web test/workflow-shot-generation.test.ts test/workflow-shot-references.test.ts test/workflow-shot-delete.test.ts test/project-chapter-storyboard.test.ts test/chapter-generated-asset-binding.test.ts test/chapter-generated-binding-fix1.test.ts` | 29 pass，132 assertions |
| 前端专项合计 | 89 tests，584 assertions，0 fail；主agent最终独立复跑同结果 |
| `bun run --cwd web build` | PASS，含tsc与Vite生产构建 |
| `bun run --cwd web lint` | PASS |
| `go -C backend test ./internal/app ./internal/handler -run 'TestMediaRecovery\|TestTaskMediaRecovery\|TestTaskActiveLimit' -count=1` | PASS，app17.343s、handler8.839s |
| 真实工作台与受控API | 浅色弹窗/深色工作台五档宽度、390px深色弹窗通过最终态布局检查；受控串行产物回填、停止11/12及刷新不补位已观察 |

集成输出存在CSSMotion/Notification act和普通章节Focusable ARIA警告，构建存在大chunk提示，未隐藏。`docs/package.json`不存在，docs类型检查未执行，不声称通过。代码未提交或推送，原有画布性能与其他未提交修改均保留。

### 8.3 仍需外部条件的验收边界

- Task11保留未全勾选：天然前台动效、完整键盘焦点体验、全部生产阶段/主题组合和时间轴拖拽仍非本轮完整人工验收。IAB报告后台时间线冻结，本轮仅完成有限动画后验最终布局，不冒充自然动效通过。
- 真实供应商生成质量、账单和真实对象存储故障恢复需要明确付费/渠道授权；本轮没有调用外部供应商。受控API、后端httptest和真实供应商验收不可互相替代。
- 同源浏览器WebLocks不是跨设备幂等服务，不新增该承诺。

本轮详细证据：`.local/short-drama-batch-gap-20261029/{fix-report,backend-report,media-recovery-report,browser-verification,progress}.md`；代码审查包为 `final-review-round2.diff`。这些是本地验收记录，不进产品构建目录。
