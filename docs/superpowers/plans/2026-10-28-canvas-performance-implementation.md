# 大型画布性能优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本文是实现计划，不是实施完成记录；没有授权自动提交、推送、改写用户数据或启动服务。

**Goal:** 降低大画布悬停、导航、节点拖拽、连线、内容编辑与任务回写的主线程成本，同时保持节点可见性、引用编号、媒体留帧、撤销和保存一致性。

**Architecture:** 保留现有 React DOM 节点、Leafer 图形层、Zustand 持久状态、实时 DOM/ref 预览与空间索引。先修复引用失稳和无效计算，再将高频查询约束到局部候选或已排序坐标；保存路径只做保持协议的增量优化。拆成渲染、交互、保存三个可独立验收的交付包，不以全局重构为前提。

**Tech Stack:** React 19、TypeScript、Bun、Zustand、Leafer、已有 `@tanstack/react-virtual`、`fflate`、Bun test、happy-dom、真实浏览器 Performance/React Profiler。

## Global Constraints

- 本轮只落本文；所有实现、专项测试、build、浏览器采样均未执行。
- 开工先检查 `git status --short` 和每个目标文件的 diff。当前工作区已有大量修改，尤其 `web/src/lib/canvas/canvas-project-domain.ts`、`web/package.json` 等，不能覆盖、回滚或顺手格式化。
- 不新增依赖，不重写 WebGL/Canvas 节点，不迁移全局状态管理，不新增通用调度框架。
- 不调整后端 API、数据库 schema、revision/CAS、权限、编辑租约、账号 scope、mutationId 和 journal 确认顺序。
- 不取消自动保存，不延后唯一恢复记录，不以增加保存间隔或吞掉冲突来提速。
- 保留节点顶层 `updatedAt` 不参与内容判脏的语义；任务身份及业务内容不能被缓存忽略。
- 保留所有屏内和正在交互的节点；`280/420/720` 只约束屏外保留预算，不能重新成为屏内硬截断。
- 保留 SVG 连线点击、悬停与右键能力；禁止默认以 `scale < 0.27` 删除命中层。
- 图片留帧、原始比例、手工尺寸保护、缩略图/原图区分和用户 scope 均不得退化。
- 普通拖动/框选/视口已有预览与提交分离，不重复实现。Ctrl+图片滚轮期间也已暂停历史和 autosave，不能把其成本误归因于逐帧落盘。
- 手工编辑优先仓库约定的 `apply_patch`；宿主无该工具时使用专用文件编辑工具。不能用宽范围脚本覆盖业务文件。
- 本文测试命令均在 `web/` 目录执行。实施时风险需要专项验证，先跑对应最小测试；集成结束再跑一次 `bun run build` 和 `bun run lint`。
- 默认不启动开发服务。真实路由验收需使用用户授权的现有测试环境；需要新服务时先核对端口、现有进程、`.env.example` 和 `.local/project-workbench-debug` 数据目录。
- 不运行付费生成，不改真实项目来造压测数据；合成图通过现有导入入口进入隔离测试账号/环境。
- 每任务是独立审查和可提交边界，但只有用户明确要求提交时才执行 Git 提交。提交格式：`perf(canvas): 画布性能 - <摘要>`；严禁 `git add .`。
- 同类验证失败连续三次时停止盲试，记录现象与新假设；不能把静态字符串测试、纯函数 50k 数据测试当作真实画布 FPS 证据。

---

## 1. 两份分析的合并结论

### 1.1 已确认且保留的基础设施

`canvas-live-viewport.ts` 的 CSS 合成预览、普通拖动的 DOM 预览、框选差量标记、空间索引、布局引用缓存、三级 LOD、小地图 Canvas、视频封面、图片加载队列、180ms 差分历史和 40ms SSE 聚合均已存在。

旧 `docs/design/canvas-final-performance-architecture.md` 基于较早提交，包含“没有完整 LOD”“小地图全 DOM”等过时现状。它可以提供目标背景，不能替代本计划的现状依据。

### 1.2 对粘贴分析逐项裁决

| 建议/判断 | 裁决 | 本计划处理 |
| --- | --- | --- |
| `toolbarNodeId` 为只写不读状态 | 确认，编辑页和共享页都需清理调用链 | Task 3 删除状态、timer 和无效接口，不留下 no-op |
| 关闭隐藏连线时仍让 hover 影响页面 | 确认存在无效失效链 | Task 3 关闭时不发布父级 hover，并归一筛选依赖 |
| 每次 hover 全量重建 Leafer Path | 表述过强，普通连接已有 ID/signature 复用 | 减少无用遍历；只在 Task 12 处理确有 removeAll 的批量草稿 |
| `renderCanvasNodeContent` 的 scale 完全没用 | 不成立，分镜 `RowHandle` 使用 `32 / scale` | Task 3 先换成已有 committed CSS 变量，再删对应 props 链 |
| 引用计算不应依赖 visibleNodes | 图索引和节点输入解析应解耦；展示候选/全局编号有视口语义 | Task 4 缓存图索引、按节点懒解析，保留原展示范围与编号 |
| 示例 `buildCanvasResourceReferenceBundle` 可直接使用 | 当前仓库不存在该函数，示例签名也非现有 API | 在现有资源引用模块定义明确的新接口，不直接粘贴示例 |
| 先判断 shell，再处理 Image | 有破坏留帧风险，现有特判是显式设计 | Task 8 加载门控，保留同一 RetainedCanvasImage，不直接卸载 |
| 远景移除 SVG 热区 | 会丢连线点击/右键，Leafer 视觉层 hittable=false | 不纳入默认实现；需等价命中替代方案才另立任务 |
| 无 BatchTable 时一行 guard | 方向成立，但应遵循 helper 的 metadata 谓词 | Task 7 在共享 helper 早退，保持现有孤边行为，不夹带修复 |
| 改四处即可消除绝大部分阻塞 | 没有实测证据 | 作为候选，不写倍数或收益比例承诺 |
| 不引入新的列表虚拟化库 | 同意不加依赖，但已有 react-virtual 可复用 | Task 5/9 先阻止无效计算与媒体加载；长列表按测量门槛启用已有库 |

### 1.3 规模记号与目标

- `N`：总节点；`V`：当前挂载节点；`E`：总连线；`S`：批量连接源数；`K`：一个 SSE 批次的 patch 数。
- 不追求“所有操作 O(1)”：显示 V 个节点、框选全部节点、首次建索引本来就需要处理对应数据。
- 目标是让“鼠标在局部移动”“视口变化但图内容未变”“一次只改一个节点”不重复支付整图解析、序列化和组件分配成本。

## 2. 交付包、依赖与文件职责

### 交付包 A：渲染与缓存，必做

Task 1 建基线；Task 2 修内容缓存；Task 3 清理无效 hover/scale 依赖；Task 4 稳定资源引用；Task 5 关闭面板不计算；Task 6 小地图静态底图；Task 7 批量表快路；Task 8 图片留帧加载门控。

### 交付包 B：交互，必做

Task 10 对齐坐标索引；Task 11 连线命中候选；Task 12 批量连线规划/草稿复用；Task 13 Ctrl+滚轮局部尺寸预览。它们依赖 Task 1 的对照数据，不要求保存层重构。

### 交付包 C：编辑与回写，必做小改、条件启用较大改动

Task 9 引用媒体按需加载；Task 14 保存/历史引用快路为必做。Task 15 SSE 批内工作集在基线确认批量回写热点时启用；未启用不宣称该项已优化。Task 16 统一回归和文档收尾。

### 文件地图

| 边界 | 主要文件 | 责任 |
| --- | --- | --- |
| 测试夹具 | `web/test/helpers/canvas-performance-fixture.ts`（新） | 可重复、无真实资源的图数据 |
| 夹具导出 | `web/scripts/create-canvas-performance-fixture.ts`（新） | 生成现有导入格式 ZIP，不注册新产品路由 |
| 内容身份 | `web/src/lib/canvas/canvas-content.ts` | 排除簿记字段、稳定投影、复用 hash |
| 页级编排 | `web/src/pages/canvas/project.tsx`、`shared.tsx`、三个现有 controller/editor/media hook | 删除无消费者状态、消费稳定派生值 |
| 资源语义 | `web/src/lib/canvas/canvas-resource-references.ts` | 唯一输入顺序、引用编号、图解析缓存 |
| 渲染模型 | `web/src/pages/canvas/use-canvas-render-model.ts` | 可见集合与语义解析分开 |
| 节点 UI | `canvas-node.tsx`、`canvas-node-toolbar.tsx`、`canvas-script-node.tsx` | hover、工具栏、缩放热区语义 |
| 辅助 UI | `canvas-workspace-panel.tsx`、`canvas-mini-map.tsx` | 关闭门控、底图与视口框分离 |
| 媒体 | `canvas-node-media-content.tsx`、`use-resolved-canvas-resource-references.ts`、mention 组件 | 留帧与请求门控 |
| 批量引用 | `web/src/lib/canvas/canvas-batch-references.ts` | 所有调用方共享无工作快路 |
| 吸附/命中 | `canvas-project-domain.ts`、`canvas-connection-hit-index.ts`（新）、connection controller | 六轴查询、空间候选、最终合法性检查 |
| 局部几何 | `canvas-live-viewport.ts`、image-wheel hook、Leafer layer | 不持久化的尺寸预览及关联连线 |
| 保存 | `canvas-content.ts`、`user-data-sync.ts`、`user-data-sync-media.ts`、`use-canvas-history.ts` | 保持不可变引用、提前范围筛选 |
| SSE | `agent-canvas-patch.ts`、`user-data-sync.ts` | 有界的批内节点更新快路 |
| 结果文档 | `docs/content/docs/progress/pending-test.mdx`、旧性能设计文档 | 区分实现、测试、真实设备未验收项 |

**并行规则：** Task 2、5、6 可独立实施；Task 3、4、11、12、13 都可能修改 `project.tsx`，必须串行整合。不要让两个工作者同时写同一大文件。Task 14、15 串行修改 `user-data-sync.ts`。

---

## Task 1：建立可重复的基线与验收夹具

**Files**
- Create: `web/test/helpers/canvas-performance-fixture.ts`
- Create: `web/test/canvas-performance-fixture.test.ts`
- Create: `web/scripts/create-canvas-performance-fixture.ts`
- Create during execution: `.local/cache/canvas-performance/` 下 ZIP 和采样结果，不提交二进制/用户数据。

**Interfaces**
- Produces: `performanceNode(id: string, patch?: Partial<CanvasNodeData>): CanvasNodeData`
- Produces: `performanceProject(count?: number): CanvasProject`
- Consumers: 本文所有纯函数回归；真实 `/canvas/:id` 的合成图导入。

- [ ] **1. 写夹具和最小测试。** 夹具采用固定时间，所有节点 ID 唯一，无账号、URL、API Key、resourceId。

```ts
// web/test/helpers/canvas-performance-fixture.ts
import { CanvasNodeType, type CanvasNodeData } from "../../src/types/canvas";
import type { CanvasProject } from "../../src/stores/canvas/use-canvas-store";

export function performanceNode(id: string, patch: Partial<CanvasNodeData> = {}): CanvasNodeData {
    return { id, type: CanvasNodeType.Text, title: id, position: { x: 0, y: 0 }, width: 320, height: 180, metadata: { content: id }, ...patch };
}

export function performanceProject(count = 100): CanvasProject {
    const nodes = Array.from({ length: count }, (_, index) => performanceNode(`perf-${index}`, {
        position: { x: (index % 50) * 360, y: Math.floor(index / 50) * 220 },
    }));
    return {
        id: "canvas-performance", title: `性能夹具 ${count}`, createdAt: "2026-10-28T00:00:00.000Z", updatedAt: "2026-10-28T00:00:00.000Z",
        nodes, connections: nodes.slice(1).map((node, index) => ({ id: `edge-${index}`, fromNodeId: nodes[index].id, toNodeId: node.id })),
        chatSessions: [], activeChatId: null, backgroundMode: "dots", showImageInfo: false, viewport: { x: 0, y: 0, k: 1 }, previsScenes: [],
    };
}
```

```ts
// web/test/canvas-performance-fixture.test.ts
import { expect, test } from "bun:test";
import { performanceProject } from "./helpers/canvas-performance-fixture";

test("performance fixtures have deterministic IDs and valid endpoints", () => {
    const project = performanceProject(2000);
    const ids = new Set(project.nodes.map((node) => node.id));
    expect(ids.size).toBe(2000);
    expect(project.connections).toHaveLength(1999);
    expect(project.connections.every((edge) => ids.has(edge.fromNodeId) && ids.has(edge.toNodeId))).toBe(true);
    expect(project).toEqual(performanceProject(2000));
});
```

- [ ] **2. 生成现有导入格式，不创建仿制 HTML。**

```ts
// web/scripts/create-canvas-performance-fixture.ts
import { mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import { strToU8, zipSync } from "fflate";
import { performanceProject } from "../test/helpers/canvas-performance-fixture";
import type { CanvasExportFile } from "../src/types/canvas-export";

const count = Number(Bun.argv[2]);
const output = Bun.argv[3];
if (!Number.isSafeInteger(count) || count < 1 || count > 50000 || !output) throw new Error("用法：bun run scripts/create-canvas-performance-fixture.ts <1..50000> <output.zip>");
const manifest: CanvasExportFile = { app: "ymzy", version: 4, exportedAt: "2026-10-28T00:00:00.000Z", projects: [{ project: performanceProject(count), files: [] }] };
await mkdir(dirname(output), { recursive: true });
await Bun.write(output, zipSync({ "projects.json": strToU8(JSON.stringify(manifest)) }));
console.log(`已生成 ${count} 节点夹具：${output}`);
```

- [ ] **3. 执行 `bun test test/canvas-performance-fixture.test.ts`；预期通过。执行 `bun run scripts/create-canvas-performance-fixture.ts 2000 ../.local/cache/canvas-performance/2000.zip`；预期生成 ZIP。** 首次加入 helper 前测试应因缺少导出失败，不能把语法/环境失败误作性能缺陷。
- [ ] **4. 记录原始基线。** 100、500、2000 节点；固定可见约 50 个与全景密集同屏分开。每场景连续操作 10 秒、重复 3 次。分别记录冷/热缓存、浏览器版本、DPR、视窗、构建模式、节点/边/媒体数、P50/P95/max 帧间隔、>50ms 长任务、React commit 和 heap。媒体场景使用测试环境已有合法图片/视频，在基线与优化版保持相同资源，不能把纯文本夹具冒充媒体测试。
- [ ] **5. 保存基线为 `.local/cache/canvas-performance/baseline.md`，记录 commit 与工作区 diff 摘要。** 不开启调试模式测完后与生产模式横向比较。无真实路由环境则标记“浏览器基线阻塞”，可以继续确定性测试，但不允许宣布性能通过。

---

## Task 2：修复内容快照缓存失效

**Files**
- Modify: `web/src/lib/canvas/canvas-content.ts:5-59`
- Create: `web/test/canvas-content-cache.test.ts`
- Regression: `web/test/canvas-updatedat-bookkeeping.test.ts`

**Interfaces**：不改 `canvasContentSnapshot`、`sameCanvasContent`、`canvasContentHash` 的对外签名；新增缓存仅为模块私有。

- [ ] **1. 写失败测试。**

```ts
import { expect, test } from "bun:test";
import { canvasContentHash, canvasContentSnapshot, sameCanvasContent } from "../src/lib/canvas/canvas-content";
import { performanceProject } from "./helpers/canvas-performance-fixture";

test("viewport and revision copies reuse the snapshot and hash Promise", async () => {
    const base = performanceProject(1000);
    const view = { ...base, viewport: { x: 20, y: 40, k: 0.5 }, revision: 5 };
    expect(canvasContentSnapshot(view).nodes).toBe(canvasContentSnapshot(base).nodes);
    const hash = canvasContentHash(base);
    expect(canvasContentHash(view)).toBe(hash);
    expect(await canvasContentHash(view)).toBe(await hash);
});

test("bookkeeping stays ignored while real content invalidates", async () => {
    const base = performanceProject(2);
    const stamped = { ...base, nodes: base.nodes.map((node) => ({ ...node, updatedAt: "2026-10-28T01:00:00.000Z" })) };
    expect(sameCanvasContent(base, stamped)).toBe(true);
    expect(await canvasContentHash(stamped)).toBe(await canvasContentHash(base));
    const changed = { ...base, nodes: [{ ...base.nodes[0], title: "已编辑" }, base.nodes[1]] };
    expect(sameCanvasContent(base, changed)).toBe(false);
    expect(await canvasContentHash(changed)).not.toBe(await canvasContentHash(base));
    expect(canvasContentSnapshot(changed).nodes[1]).toBe(canvasContentSnapshot(base).nodes[1]);
});
```

- [ ] **2. 执行 `bun test test/canvas-content-cache.test.ts`，预期第一项引用断言失败。**
- [ ] **3. 在现有文件加入稳定投影，替换原 `content.nodes.map`。**

```ts
type CanvasNode = CanvasProject["nodes"][number];
type ContentNode = Omit<CanvasNode, "updatedAt">;
const contentNodeCache = new WeakMap<CanvasNode, ContentNode>();
const contentNodeArrayCache = new WeakMap<CanvasProject["nodes"], ContentNode[]>();

function stableContentNodes(nodes: CanvasProject["nodes"]) {
    const cached = contentNodeArrayCache.get(nodes);
    if (cached) return cached;
    const projected = nodes.map((source) => {
        const existing = contentNodeCache.get(source);
        if (existing) return existing;
        const { updatedAt: _updatedAt, ...node } = source;
        contentNodeCache.set(source, node);
        return node;
    });
    contentNodeArrayCache.set(nodes, projected);
    return projected;
}
```

`canvasContentSnapshot` 内仅将节点投影改为 `const nodes = stableContentNodes(content.nodes);`。保留原 hash 错误时删缓存、非安全上下文精确 JSON baseline 的实现。

- [ ] **4. 执行 `bun test test/canvas-content-cache.test.ts test/canvas-updatedat-bookkeeping.test.ts`；预期全部通过。** 补充 connections、chatSessions、title、projectId 变化失效断言；不要只测 nodes。
- [ ] **5. 审查 diff：无同步合同变化、无全局强引用缓存。** 可提交摘要：`修复内容快照稳定引用与哈希复用`。

---

## Task 3：移除无效 hover 状态，正确解耦缩放消费端

**Files**
- Modify: `web/src/pages/canvas/project.tsx`
- Modify: `web/src/pages/canvas/shared.tsx`
- Modify: `web/src/pages/canvas/use-canvas-viewport-controller.ts`
- Modify: `web/src/pages/canvas/use-canvas-node-editor.ts`
- Modify: `web/src/pages/canvas/use-canvas-media-tools.ts`
- Modify: `web/src/pages/canvas/canvas-project-world-layers.tsx`
- Modify: `web/src/components/canvas/canvas-node.tsx`
- Modify: `web/src/components/canvas/canvas-node-toolbar.tsx`
- Modify: `web/src/components/canvas/canvas-script-node.tsx`
- Create: `web/test/canvas-render-invalidation.test.ts`
- Regression: `web/test/canvas-node-toolbar.test.ts`、`canvas-node-drag-overlays.test.ts`、`canvas-node-title-interaction.test.ts`

**Interfaces**
- `CanvasNode` 的 `onHoverStart/onHoverEnd` 改为可选，内部 hover 状态保留。
- 删除公共 toolbar 的 `onKeep/onLeave` 和各 hook 的 `setToolbarNodeId` 参数，不传空函数填坑。
- 删除仅 `CanvasScriptNodeContent → RowHandle` 的 `scale` 属性链，世界层几何 scale 保留。

- [ ] **1. 写结构回归以保护完整删除范围；这些断言只证明接线，不当作性能采样。**

```ts
import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");

test("dead toolbar state is removed from all consumers", () => {
    for (const path of ["pages/canvas/project.tsx", "pages/canvas/shared.tsx", "pages/canvas/use-canvas-viewport-controller.ts", "pages/canvas/use-canvas-node-editor.ts", "pages/canvas/use-canvas-media-tools.ts"]) {
        expect(source(path)).not.toMatch(/\b(?:toolbarNodeId|setToolbarNodeId|toolbarHideTimerRef)\b/);
    }
    expect(source("components/canvas/canvas-node-toolbar.tsx")).not.toMatch(/\b(?:onKeep|onLeave)\b/);
});

test("script handles use the existing committed inverse scale", () => {
    expect(source("components/canvas/canvas-script-node.tsx")).toContain("calc(32px * var(--canvas-live-inverse-scale, 1))");
    expect(source("pages/canvas/project.tsx")).not.toContain("scale={viewport.k}");
});
```

- [ ] **2. 执行 `bun test test/canvas-render-invalidation.test.ts`；预期死状态与旧 scale 接线失败。**
- [ ] **3. 完整删除死链。** 保留从 selection 推导的实际 `toolbarNode`、拖拽隐藏条件、单击编辑及浮层边界。修改节点事件为：

```tsx
onMouseEnter={() => {
    setHovered(true);
    onHoverStart?.(data.id);
}}
onMouseLeave={() => {
    setHovered(false);
    onHoverEnd?.(data.id);
}}
```

页面/world props 在 `hideNodeConnections` 关闭时传 `undefined`；开启后仍发布 hover。关闭开关时清理旧 hover，避免再次开启沿用过时目标。

```ts
const effectiveHoveredNodeId = hideNodeConnections ? hoveredNodeId : null;
const visibleDisplayConnections = useMemo(() => filterBatchCellConnections(
    filterCanvasDisplayConnections(displayConnections, {
        enabled: hideNodeConnections,
        hoveredNodeId: effectiveHoveredNodeId,
        selectedNodeIds,
        activeNodeId,
        selectedConnectionId,
    }),
    batchScrollById,
    selectedConnectionId,
), [activeNodeId, batchScrollById, displayConnections, hideNodeConnections, effectiveHoveredNodeId, selectedConnectionId, selectedNodeIds]);
```

`RowHandle` 用下面的值替换两个 `32 * inverseHitScale`，然后删除该变量、三个调用处的 scale、Script props 的 scale 和渲染回调的 `viewport.k` 依赖：

```tsx
width: "calc(32px * var(--canvas-live-inverse-scale, 1))",
height: "calc(32px * var(--canvas-live-inverse-scale, 1))",
```

编辑页两个 connection handler 替换为下列稳定函数（事件类型从 React 导入），世界层改传函数名。共享页只删除已核实无消费者的 toolbar 保活链，不为不存在的世界层接口造代码：

```ts
const handleConnectionSelect = useCallback((connectionId: string) => {
    setSelectedConnectionId(connectionId);
    setSelectedNodeIds(new Set());
    setContextMenu(null);
}, [setSelectedConnectionId, setSelectedNodeIds, setContextMenu]);
const handleConnectionContextMenu = useCallback((event: React.MouseEvent, connectionId: string) => {
    setSelectedConnectionId(connectionId);
    setSelectedNodeIds(new Set());
    closeConnectionCreateMenu();
    setContextMenu({ type: "connection", x: event.clientX, y: event.clientY, connectionId });
}, [setSelectedConnectionId, setSelectedNodeIds, closeConnectionCreateMenu, setContextMenu]);
```

`canvas-live-viewport.ts` 继续在手势结束后才更新根 CSS 变量。

- [ ] **4. 执行本任务测试及三组 toolbar/title/drag 回归。** 真实路由采样：关闭隐藏连线时连续划过 100 个节点，页级 hover 更新应消失；开启时保留相关连线反馈。5%、25%、100%、200% 下分镜行端口屏幕热区在提交后保持约 32px。
- [ ] **5. 审查：共享只读页、工具栏进入/离开、选中/拖动隐藏均正确。** 不宣称“缩放时所有节点零渲染”，节点 chrome 仍合法消费 scale。可提交摘要：`移除悬停死状态并稳定缩放内容回调`。

---

## Task 4：图语义解析与可见窗口解耦，保留引用编号

**Files**
- Modify: `web/src/lib/canvas/canvas-resource-references.ts:361-471`
- Modify: `web/src/pages/canvas/use-canvas-render-model.ts:350-406`
- Modify: `web/src/pages/canvas/project.tsx:1492-1509,2194-2421`
- Modify: `web/src/pages/canvas/canvas-project-world-layers.tsx`
- Create: `web/test/canvas-reference-index.test.ts`
- Regression: `web/test/canvas-resource-references.test.ts`、`canvas-batch-references.test.ts`、`canvas-character-reference.test.ts`

**Interfaces**
- New export in existing module: `createCanvasResourceReferenceIndex(nodes: CanvasNodeData[], connections: CanvasConnection[])`。
- 返回 `mention(nodeId: string): CanvasResourceReference[]` 和 `resources(contextNodeId?: string | null, targetNodes?: CanvasNodeData[]): CanvasResourceReference[]`。
- 旧公开 builders 保留签名并委托此解析器；编辑器返回 `getNodeMentionReferences(nodeId)`，替换仅按 `.get()` 消费的 `mentionReferencesByNodeId`。

- [ ] **1. 写失败测试：相同索引/同节点返回同数组，输入编号与旧实现一致。**

```ts
import { expect, test } from "bun:test";
import { createCanvasResourceReferenceIndex, buildCanvasNodeMentionReferenceMap } from "../src/lib/canvas/canvas-resource-references";
import { performanceNode } from "./helpers/canvas-performance-fixture";
import { CanvasNodeType } from "../src/types/canvas";

test("lazy mention reads preserve identity and existing slot order", () => {
    const a = performanceNode("a", { type: CanvasNodeType.Image });
    const b = performanceNode("b", { type: CanvasNodeType.Image });
    const target = performanceNode("target");
    const nodes = [a, b, target];
    const edges = [{ id: "b-first", fromNodeId: b.id, toNodeId: target.id }, { id: "a-second", fromNodeId: a.id, toNodeId: target.id }];
    const index = createCanvasResourceReferenceIndex(nodes, edges);
    expect(index.mention(target.id)).toBe(index.mention(target.id));
    expect(index.mention(target.id)).toEqual(buildCanvasNodeMentionReferenceMap(nodes, edges).get(target.id));
    expect(index.mention(target.id).map((ref) => ref.nodeId)).toEqual(["b", "a"]);
    expect(index.mention("missing")).toEqual([]);
});
```

- [ ] **2. 执行 `bun test test/canvas-reference-index.test.ts`；预期缺少 export 失败。**
- [ ] **3. 复用现有私有 resolver，增加缓存外壳，不改输入规则。**

```ts
export function createCanvasResourceReferenceIndex(nodes: CanvasNodeData[], connections: CanvasConnection[]) {
    const resolve = buildCanvasResourceInputResolver(nodes, connections);
    const mentions = new Map<string, CanvasResourceReference[]>();
    return {
        mention(nodeId: string) {
            const cached = mentions.get(nodeId);
            if (cached) return cached;
            const references = labelResourceNodes(resolve(nodeId), true);
            mentions.set(nodeId, references);
            return references;
        },
        resources(contextNodeId?: string | null, targetNodes?: CanvasNodeData[]) {
            const contextNodes = contextNodeId ? resolve(contextNodeId, true) : [];
            const sourceNodes = targetNodes ? uniqueCanvasNodes([...targetNodes, ...contextNodes]) : nodes;
            const activeById = new Map(labelResourceNodes(contextNodes, true).map((ref) => [ref.nodeId, ref]));
            return labelResourceNodes(sourceNodes.filter(isResourceNode), false).map((ref) => activeById.get(ref.nodeId) || ref);
        },
    };
}
```

`buildCanvasNodeMentionReferenceMap` 用一个 index 解析目标节点。`buildCanvasResourceReferences` 委托 `index.resources`。不要将 `getMentionResourceNodes(includeSelf=true)` 与生成输入的 `includeSelf=false` 混为一谈。

hook 只在 `[semanticNodes, connections]` 改变时建 index；技能/工具装饰按节点懒缓存：

```ts
const referenceIndex = useMemo(() => createCanvasResourceReferenceIndex(semanticNodes, connections), [semanticNodes, connections]);
const getNodeMentionReferences = useMemo(() => {
    const cache = new Map<string, CanvasResourceReference[]>();
    return (nodeId: string) => {
        const previous = cache.get(nodeId);
        if (previous) return previous;
        const base = referenceIndex.mention(nodeId);
        const tools = toolMentionReferencesByNodeId.get(nodeId) || [];
        const result = skillMentionReferences.length || tools.length ? [...base, ...skillMentionReferences, ...tools] : base;
        cache.set(nodeId, result);
        return result;
    };
}, [referenceIndex, skillMentionReferences, toolMentionReferencesByNodeId]);
```

保留 `resourceReferenceTargetNodes` 的原视口候选语义，但 `resources` 不再重建全图索引；不能通过全图 targetNodes 扩大候选/重排编号。badge 对象可在 hook 内用上次 Map 保留浅相等引用：

```ts
const resourceReferenceCacheRef = useRef(new Map<string, CanvasResourceReference>());
const resourceReferenceByNodeId = useMemo(() => {
    const previous = resourceReferenceCacheRef.current;
    const result = new Map(canvasResourceReferences.map((reference) => {
        const old = previous.get(reference.nodeId);
        const unchanged = old && Object.keys(old).length === Object.keys(reference).length &&
            Object.entries(reference).every(([key, value]) => old[key as keyof CanvasResourceReference] === value);
        return [reference.nodeId, unchanged ? old : reference] as const;
    }));
    resourceReferenceCacheRef.current = result;
    return result;
}, [canvasResourceReferences]);
```

`configInputsById` 的消费者只有 `.get(id)`，因此用具有相同读取接口的懒索引，不改面板代码：

```ts
const configInputsById = useMemo(() => {
    const cache = new Map<string, ReturnType<typeof buildNodeGenerationInputs>>();
    return {
        get(nodeId: string) {
            const cached = cache.get(nodeId);
            if (cached) return cached;
            const inputs = buildNodeGenerationInputs(nodeId, semanticNodes, connections);
            cache.set(nodeId, inputs);
            return inputs;
        },
    };
}, [semanticNodes, connections]);
```

它不再依赖 visibleNodes；首次读取某配置节点仍有原 builder 成本，不宣称已消除所有生成输入扫描。模型提交路径继续执行真实输入重验。

旧 token effect 替换为下列逻辑，候选语义解析只在相应快照变化时执行，提交前校验原 prompt 避免覆盖并发编辑：

```ts
useEffect(() => {
    const changes = new Map<string, { before: string; after: string; composer: boolean }>();
    for (const node of semanticNodes) {
        const before = node.metadata?.composerContent ?? node.metadata?.prompt;
        if (!before?.includes("@[node:")) continue;
        const references = getNodeMentionReferences(node.id);
        if (!references.length) continue;
        const after = normalizeCanvasNodeMentionTokens(before, references);
        if (after !== before) changes.set(node.id, { before, after, composer: node.metadata?.composerContent !== undefined });
    }
    if (!changes.size) return;
    setNodes((current) => {
        let changed = false;
        const next = current.map((node) => {
            const patch = changes.get(node.id);
            if (!patch || (node.metadata?.composerContent ?? node.metadata?.prompt) !== patch.before) return node;
            if ((node.metadata?.composerContent !== undefined) !== patch.composer) return node;
            changed = true;
            return { ...node, metadata: { ...node.metadata, [patch.composer ? "composerContent" : "prompt"]: patch.after } };
        });
        return changed ? next : current;
    });
}, [semanticNodes, getNodeMentionReferences, setNodes]);
```

- [ ] **4. 跑三组语义回归与新测试。** 增加 Config 覆盖、批次子节点继承、环保护、边重排、角色/绘图/图片混合编号、素材 token 和非可见节点既有 prompt 不被平移改写的断言。实际 Profiler 对照窗口平移和每次 12 节点追加：全图引用 resolver 构建次数为零，直到图语义改变。
- [ ] **5. 审查：缓存寿命由 hook/index 实例控制，不积累所有历史图；节点内容与引用实际变化仍刷新。** 可提交摘要：`分离资源图索引与可见窗口解析`。

---

## Task 5：关闭工作区停止搜索，稳定列表回调

**Files**
- Modify: `web/src/components/canvas/canvas-workspace-panel.tsx:46-67`
- Modify: `web/src/components/canvas/canvas-workspace-node-list-panel.tsx:58-83`
- Regression: `web/test/canvas-workspace-sidebar.test.ts`、`web/test/canvas-node-search.test.ts`

**Interfaces**：`CanvasNodeListItem.onSelect` 改为 `(nodeId: string) => void`，父级直接传 `onFocus`。

- [ ] **1. 增加接线回归，之后用真实组件计数验收。**

```ts
test("closed workspace does not evaluate node search", () => {
    const panel = component("canvas-workspace-panel.tsx");
    expect(panel).toContain('open && tab === "nodes"');
    expect(panel).toContain("results={nodeSearchResults}");
    expect(panel).not.toContain("results={searchCanvasNodes(");
});
```

- [ ] **2. 跑 `bun test test/canvas-workspace-sidebar.test.ts`，预期新接线断言失败。**
- [ ] **3. 用一次 memo 代替 JSX 内计算。**

```ts
const nodeSearchResults = useMemo(
    () => open && tab === "nodes" ? searchCanvasNodes(nodes, deferredQuery, nodes.length, config) : [],
    [open, tab, nodes, deferredQuery, config],
);
```

列表改为 `onSelect={onFocus}`，行内点击改为 `onClick={() => onSelect(node.id)}`。行自身 memo 的回调引用不再每次由父级重新分配。保留默认空查询最多 40 项，不人为限制搜索命中。

- [ ] **4. 跑搜索/sidebar 回归；实测关闭时 search 函数调用数为零，打开时按数据/查询变化计算。** 查询结果超过 200 且真实 commit >16ms 时，在这个列表复用现有 `useVirtualizer`，`getItemKey=node.id`、`measureElement` 测实际行高、overscan=8；必须同时完成键盘焦点跨窗口和选中项定位测试才启用。没有触发门槛则不加虚拟化代码。
- [ ] **5. 审查小屏 Drawer 与桌面 aside 共用同一结果。** 可提交摘要：`停止关闭面板的节点搜索与重复行渲染`。

---

## Task 6：小地图只更新视口框

**Files**
- Modify: `web/src/components/canvas/canvas-mini-map.tsx:57-77,112-144`
- Create: `web/test/canvas-minimap-update.test.ts`

**Interfaces**：外部 `MinimapProps` 不变；内部 `drawMinimap` 仅负责节点底图，视口矩形由既有 DOM 承载。

- [ ] **1. 写失败接线断言，约束 preview 回调不能调用 drawMinimap。**

```ts
import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
const source = readFileSync(new URL("../src/components/canvas/canvas-mini-map.tsx", import.meta.url), "utf8");
test("minimap viewport updates do not repaint the node background", () => {
    const callback = source.split("const updateViewportRect = useCallback")[1]?.split("useEffect")[0] || "";
    expect(callback).not.toContain("drawMinimap(");
    expect(source).toContain("ref={viewportRectRef}");
});
```

- [ ] **2. 执行 `bun test test/canvas-minimap-update.test.ts`，预期失败。**
- [ ] **3. 复用已有 DOM 框，替换 preview 回调；删底图函数最后的视口框绘制。**

```ts
const viewportRectRef = useRef<HTMLDivElement>(null);
const updateViewportRect = useCallback((next: ViewportTransform) => {
    liveViewportRef.current = next;
    const element = viewportRectRef.current;
    if (!element) return;
    const start = toMinimap(-next.x / next.k, -next.y / next.k);
    const end = toMinimap((-next.x + viewportSize.width) / next.k, (-next.y + viewportSize.height) / next.k);
    element.style.left = `${start.x}px`;
    element.style.top = `${start.y}px`;
    element.style.width = `${Math.max(end.x - start.x, 4)}px`;
    element.style.height = `${Math.max(end.y - start.y, 4)}px`;
}, [toMinimap, viewportSize.width, viewportSize.height]);
```

底图 effect 只依赖 `displayNodes/theme/toMinimap`；删掉旧第二条重复绘制。节点过滤/world bounds 复用布局快照，元数据更新不改变颜色和折叠时不重画。矩形初始 JSX 样式保留，挂 ref，订阅和卸载继续使用原 helper。

- [ ] **4. 新测试通过；实际组件测试 spy Canvas context：100 次 viewport preview 不增加节点 fillRect 次数，节点几何或主题变化仍重画。** 真实小地图拖动、pointercancel、边界坐标与 5% 缩放必须复测。
- [ ] **5. 审查无新 canvas/worker，无双视口框残留。** 可提交摘要：`分离小地图静态底图与实时视口框`。

---

## Task 7：批量表共享 helper 的无工作快路

**Files**
- Modify: `web/src/lib/canvas/canvas-batch-references.ts:50,107`
- Regression: `web/test/canvas-batch-references.test.ts`、`canvas-batch-column-inheritance.test.ts`、`canvas-batch-row-identity.test.ts`

**Interfaces**：两个现有 helper 的签名和返回引用合同不变。页面保留现有 `useLayoutEffect`，不只在一个调用方加 type guard。

- [ ] **1. 增加不读取边/不构建 Map 的快路测试。**

```ts
import { performanceNode } from "./helpers/canvas-performance-fixture";

test("non-table nodes bypass both table projections", () => {
    const nodes = [performanceNode("plain")];
    const edges = new Proxy([], {
        get(target, key, receiver) {
            if (key === Symbol.iterator || key === "filter") throw new Error("unexpected edge scan");
            return Reflect.get(target, key, receiver);
        },
    });
    expect(syncBatchColumnReferences(nodes, edges)).toBe(nodes);
    expect(reconcileBatchCellConnections(nodes, edges)).toBe(edges);
});
```

- [ ] **2. 跑 batch-references 测试，预期现实现扫描 edges 失败。**
- [ ] **3. 在构造 Map 前分别加与原算法一致的谓词。**

```ts
// syncBatchColumnReferences 函数开头
if (!nodes.some((node) => Boolean(node.metadata?.batchTable) && !node.metadata?.locked)) return nodes;

// reconcileBatchCellConnections 函数开头
if (!nodes.some((node) => node.metadata?.batchTable?.referenceMode === "cells")) return connections;
```

这仍需 O(N) 存在性检查，但省掉 Map/边扫描/投影，不宣称“零全图访问”。不要同时加新的孤边删除规则：现有 reconcile 在无 managed table 时本来保留此类边；正常节点删除由 endpoint 过滤处理。若要修复导入孤边，独立列正确性变更，不借性能任务改语义。

- [ ] **4. 跑三组回归。** 覆盖 locked、metadata 带表但 type 不同、最后一张表删除、单元格覆盖、列继承、AI 行身份、已有边 ID 保留。
- [ ] **5. 审查所有生产调用方均受益，返回相同引用的条件不扩大到有工作场景。** 可提交摘要：`为批量引用投影增加共享无工作快路`。

---

## Task 8：图片 shell 停止新增加载，保留已解码图片

**Files**
- Modify: `web/src/components/canvas/canvas-node-media-content.tsx:466-481`
- Keep behavior: `web/src/components/canvas/canvas-node-content.tsx:64-70`
- Regression: `web/test/canvas-media-performance.test.ts`、`canvas-node-lod.test.ts`
- Create: `web/test/fixtures/canvas-image-lod.harness.tsx`（真实组件 DOM 测试）

**Interfaces**：不改 `useNodeResourceUrl` 参数和返回值；`renderLOD` 已由 ImageNodeContent 传入 ImageContent。

- [ ] **1. 追加最小接线回归。**

```ts
test("shell image gating preserves the retained renderer", () => {
    expect(canvasNodeContentSource).toContain('const loadEnabled = renderLOD !== "shell" && nearViewport;');
    expect(canvasNodeContentSource).toContain("useNodeResourceUrl(node, loadEnabled, resourceVariant)");
    expect(canvasNodeContentSource).toContain("Preserve the decoded image across LOD changes");
});
```

- [ ] **2. 跑媒体测试，预期新门控断言失败。**
- [ ] **3. 仅收紧加载门控。**

```ts
const nearViewport = useNearViewport(imageContainerRef, renderLOD !== "shell");
const loadEnabled = renderLOD !== "shell" && nearViewport;
const resourceVariant = renderLOD === "full" && (node.metadata?.imageLayer || node.metadata?.imageLayerGroup) ? "original" : "thumbnail";
const { url, loading, originalWidth, originalHeight } = useNodeResourceUrl(node, loadEnabled, resourceVariant);
```

不要提前 shell return。`useNodeResourceUrl(eager=false)` 会清候选 URL，但 RetainedCanvasImage 保留同身份已显示 src；旧身份不得留到新 node/storageKey。取消仅针对当前消费者，不承诺取消共享访问请求的网络传输。

- [ ] **4. 在独立 happy-dom 子进程中挂载真实 ImageContent，mock 访问描述和解码边界：preview 显示后切 shell，同一 `<img>` 与已显示 src 保留；shell 更换 storageKey 不新增解析；回 preview 才解析新身份。** 用本地小图做真实浏览器往返平移，确认无成片白闪、手工尺寸不被覆盖。执行 `bun test test/canvas-media-performance.test.ts test/canvas-node-lod.test.ts`。
- [ ] **5. 审查收益表述：减少 shell 新加载，不声称释放所有保留图内存。** 可提交摘要：`限制远端保留图片加载并保持留帧`。

---

## Task 9：引用候选媒体按需解析，保留正文引用恢复

**Files**
- Modify: `web/src/components/canvas/use-resolved-canvas-resource-references.ts`
- Modify: `web/src/components/canvas/canvas-resource-mention-textarea.tsx`
- Modify: `web/src/components/canvas/canvas-node-prompt-panel.tsx`
- Modify: `web/src/components/canvas/canvas-mention-menu.tsx`
- Regression: `web/test/canvas-resource-mention-editor.test.ts`、`canvas-text-concurrent-input.test.ts`、`canvas-resource-references.test.ts`
- Create: `web/test/canvas-reference-preview-demand.test.ts`

**Interfaces**
- 扩展 options：`{ projectId?: string; requestedIds?: ReadonlySet<string> }`；未传 requestedIds 保留旧调用语义，传空集合表示不解析。
- 返回仍是完整 references 元数据数组，不能因媒体懒加载删除搜索候选或正文 token。

- [ ] **1. 写行为测试的请求矩阵：空 requestedIds=0 请求；一个 ID=只解析该项；已引用正文在菜单关闭时仍能恢复封面；未引用素材库 2000 项不启动 2000 个请求。** 对核心筛选增加以下可执行断言：

```ts
import { expect, test } from "bun:test";
import { canvasReferencePreviewRequested } from "../src/components/canvas/use-resolved-canvas-resource-references";
test("preview demand is explicit without filtering reference metadata", () => {
    expect(canvasReferencePreviewRequested("a", new Set())).toBe(false);
    expect(canvasReferencePreviewRequested("a", new Set(["a"]))).toBe(true);
    expect(canvasReferencePreviewRequested("b", new Set(["a"]))).toBe(false);
    expect(canvasReferencePreviewRequested("a")).toBe(true);
});
```

- [ ] **2. 执行新测试，预期缺少导出失败。**
- [ ] **3. 在现有 hook 中加纯请求门控，保持数组投影不变。**

```ts
export function canvasReferencePreviewRequested(id: string, requestedIds?: ReadonlySet<string>) {
    return requestedIds === undefined || requestedIds.has(id);
}
```

`requests` 只对 requestedIds 内的引用执行 previewIdentity。调用方需求来自两部分：正文实际使用的引用/展开预览，以及菜单当前可见行。不能简单 `enabled=menuOpen`，否则刷新后正文缩略图恢复退化。不要让 prompt-panel 和 textarea 对同一全列表各自发起解析。

菜单 `ReferencePreview` 的图片/角色/视频海报使用已有 `CachedResourceImage variant="thumbnail"`，视频使用 `previewStorageKey`，不能误用原视频 storageKey。缺海报视频的原有首帧兜底只在当前焦点/可见项激活，不为所有候选创建 `<video>`。保留文字、标题、种类、素材 ID，键盘选择与 token 插入不依赖媒体加载成功。

现有 `previewPromiseCache` 不得扩大为跨账号永不过期缓存：普通远端资源委托已有 scope-aware 访问缓存；绘图只保留项目/版本/scope 对应的在途合并，结束后删除，不把签名 URL 永久保存在模块 Map。

- [ ] **4. 跑引用/中文组词/正文恢复回归。** 真实菜单中记录请求数和 `<video>` 数；关闭后不新增候选请求，已插入引用仍可恢复。长候选列表若达到 Task 5 同等门槛，复用 `react-virtual` 并用 `scrollToIndex` 代替“未挂载项 scrollIntoView”；分类标题和技能分组不能丢，禁止只 slice 前 100 项。
- [ ] **5. 审查账号切换、绘图项目 ID、失败占位、菜单关闭再打开、键盘上下/Enter/Esc 和 IME。** 可提交摘要：`按引用实际使用范围解析媒体预览`。

---

## Task 10：拖拽吸附改为六轴排序查询

**Files**
- Modify: `web/src/lib/canvas/canvas-project-domain.ts:401-460`
- Create: `web/test/canvas-alignment-index.test.ts`
- Keep caller: `web/src/pages/canvas/use-canvas-selection-controller.ts:211,232,271`

**Interfaces**：保留 `createNodeAlignmentContext`、`calculateNodeAlignment` 的输入输出；仅替换内部 targets 为三组 X、三组 Y 排序表。

- [ ] **1. 写正确性 oracle 对照测试。** 将当前 calculateNodeAlignment 的小型线性实现原样保存在测试中，用固定随机种子生成 1000 组图/offset，对比 offset 和 guides。必须加入以下跨远距离同轴样例：

```ts
import { expect, test } from "bun:test";
import { createNodeAlignmentContext, calculateNodeAlignment } from "../src/lib/canvas/canvas-project-domain";
import { performanceNode } from "./helpers/canvas-performance-fixture";

test("alignment keeps distant same-axis targets", () => {
    const moving = performanceNode("moving", { width: 10, height: 10 });
    const far = performanceNode("far", { position: { x: 100, y: 100000 }, width: 10, height: 10 });
    const context = createNodeAlignmentContext([moving, far], [{ id: moving.id, x: 0, y: 0 }]);
    expect(calculateNodeAlignment(context, { x: 96, y: 0 }, 7)).toEqual({ offset: { x: 100, y: 0 }, guides: { vertical: 100, horizontal: undefined } });
});
```

- [ ] **2. 运行基准语义测试应先通过；另加查询访问计数，旧线性实现对 50k 目标会超计数门槛。** 计数只置于测试版数组 getter/Proxy，不给产品添加常驻 profiler。
- [ ] **3. 每轴每类坐标排序并压缩同坐标项；保留原节点顺序作为 tie。** 下面的内部函数用来替换线性搜索：

```ts
type AlignmentPoint = { value: number; order: number };
function nearestAlignmentPoint(points: AlignmentPoint[], value: number, threshold: number) {
    let low = 0;
    let high = points.length;
    while (low < high) {
        const middle = (low + high) >>> 1;
        if (points[middle].value < value) low = middle + 1;
        else high = middle;
    }
    let best: { delta: number; point: AlignmentPoint } | undefined;
    for (const index of [low - 1, low]) {
        const point = points[index];
        if (!point) continue;
        const delta = point.value - value;
        if (Math.abs(delta) > threshold) continue;
        if (!best || Math.abs(delta) < Math.abs(best.delta) || (Math.abs(delta) === Math.abs(best.delta) && point.order < best.point.order)) best = { delta, point };
    }
    return best;
}
```

构建 `order=nodeOrder*3+anchorIndex`；同坐标仅保留最小 order。保留 createNodeAlignmentContext 现有 movingBounds 和隐藏过滤，计算好旧 targets 后改为以下表生成：

```ts
function buildAlignmentTables(targets: Array<{ x: number[]; y: number[] }>, axis: "x" | "y") {
    return [0, 1, 2].map((anchorIndex) => {
        const points = targets.map((target, nodeOrder) => ({ value: target[axis][anchorIndex], order: nodeOrder * 3 + anchorIndex }))
            .filter((point) => Number.isFinite(point.value))
            .sort((a, b) => a.value - b.value || a.order - b.order);
        return points.filter((point, index) => index === 0 || point.value !== points[index - 1].value);
    });
}
function resolveAlignmentAxis(tables: AlignmentPoint[][], values: number[], threshold: number) {
    let best: ReturnType<typeof nearestAlignmentPoint>;
    for (let index = 0; index < 3; index += 1) {
        const candidate = nearestAlignmentPoint(tables[index], values[index], threshold);
        if (!candidate) continue;
        if (!best || Math.abs(candidate.delta) < Math.abs(best.delta) || (Math.abs(candidate.delta) === Math.abs(best.delta) && candidate.point.order < best.point.order)) best = candidate;
    }
    return best;
}
```

`NodeAlignmentContext` 的 targets 字段替换为 `x: AlignmentPoint[][]; y: AlignmentPoint[][]`，构造器返回 `{ movingBounds, x: buildAlignmentTables(targets, "x"), y: buildAlignmentTables(targets, "y") }`。`calculateNodeAlignment` 保留现有 null guard 与 movingX/movingY 定义，末段替换：

```ts
const x = resolveAlignmentAxis(context.x, movingX, threshold);
const y = resolveAlignmentAxis(context.y, movingY, threshold);
return {
    offset: { x: rawOffset.x + (x?.delta || 0), y: rawOffset.y + (y?.delta || 0) },
    guides: { vertical: x?.point.value, horizontal: y?.point.value },
};
```

因此仍按 `(abs(delta), nodeOrder*3+anchorIndex)` 决策，不改变旧 node-first tie；X/Y 各三次查询。

- [ ] **4. 执行 `bun test test/canvas-alignment-index.test.ts test/canvas-selection.test.ts`。** 边界含等距、重复坐标、负坐标、零 threshold、折叠子节点、锁定节点原规则、多选包围盒与 `7/scale`。
- [ ] **5. 真实多选拖拽比较抬手前后位置和辅助线。** 可提交摘要：`将拖拽吸附替换为保持顺序的坐标索引`。

---

## Task 11：连线目标命中使用局部候选

**Files**
- Create: `web/src/lib/canvas/canvas-connection-hit-index.ts`
- Modify: `web/src/pages/canvas/use-canvas-connection-controller.ts:440-541`
- Reuse: `canvas-spatial-index.ts`、`canvas-frame.ts`、连接 anchor/policy helpers
- Create: `web/test/canvas-connection-hit-index.test.ts`
- Regression: `canvas-connection-create-menu.test.ts`、`canvas-batch-cell-geometry.test.ts`、`canvas-connection-tilt.test.ts`

**Interfaces**
- `buildCanvasConnectionHitIndex(nodes: CanvasNodeData[])` 返回 `{ nodeById, candidates(point: Position, radius: number): CanvasNodeData[] }`。
- 普通节点使用空间候选；Script/BatchTable 初版放入专门的动态列表，仍调用原滚动/row/cell anchor。避免未经证明的包络裁掉端口。

- [ ] **1. 写失败测试：普通候选局部有界、重叠优先级不变、动态节点保留。**

```ts
import { expect, test } from "bun:test";
import { buildCanvasConnectionHitIndex } from "../src/lib/canvas/canvas-connection-hit-index";
import { performanceNode, performanceProject } from "./helpers/canvas-performance-fixture";

test("connection hit candidates preserve reverse source order", () => {
    const a = performanceNode("a");
    const b = performanceNode("b");
    const index = buildCanvasConnectionHitIndex([a, b]);
    expect(index.candidates({ x: 320, y: 90 }, 30).map((node) => node.id)).toEqual(["b", "a"]);
});
test("ordinary sparse graphs do not scan all nodes per query", () => {
    const index = buildCanvasConnectionHitIndex(performanceProject(50000).nodes);
    expect(index.candidates({ x: 320, y: 90 }, 30).length).toBeLessThan(20);
});
```

- [ ] **2. 执行新测试，预期缺少 module 失败。**
- [ ] **3. 构建一次 nodeById/隐藏集合，普通节点按真实端口覆盖范围建立空间索引。** 端口在边缘外的固定偏移加入 bounds；动态类型不套未经验证的矩形。候选合并后按原 source order 倒序排序。controller 用 `for...of` 遍历候选，精确圆形命中后才跑原 policy；首个合法项即可结束。`isNearNode` 在非法端口命中时仍保留 true；batch-cell DOM 命中继续优先。

新索引模块的完整最小实现如下。普通节点左右端口位于节点矩形边界；1e-6 外扩覆盖现有严格相交规则的边界点。Script/BatchTable 保守进入动态列表，不以错误包络漏选；这意味着动态节点非常多时仍有线性项，必须如实报告。

```ts
import { CanvasNodeType, type CanvasNodeData, type Position } from "@/types/canvas";
import { isFrameNode } from "@/lib/canvas/canvas-frame";
import { buildCanvasSpatialIndex, canvasNodeBounds } from "@/lib/canvas/canvas-spatial-index";

export function buildCanvasConnectionHitIndex(nodes: CanvasNodeData[]) {
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const order = new Map(nodes.map((node, index) => [node.id, index]));
    const eligible = nodes.filter((node) => {
        if (isFrameNode(node)) return false;
        const root = node.metadata?.batchRootId ? nodeById.get(node.metadata.batchRootId) : undefined;
        if (root && !root.metadata?.imageBatchExpanded) return false;
        const parent = node.parentId ? nodeById.get(node.parentId) : undefined;
        return !(parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed);
    });
    const dynamic = eligible.filter((node) => node.type === CanvasNodeType.Script || node.type === CanvasNodeType.BatchTable);
    const dynamicIds = new Set(dynamic.map((node) => node.id));
    const index = buildCanvasSpatialIndex(eligible.filter((node) => !dynamicIds.has(node.id)).map((node) => {
        const bounds = canvasNodeBounds(node);
        return { id: node.id, value: node, bounds: { left: bounds.left - 1e-6, top: bounds.top - 1e-6, right: bounds.right + 1e-6, bottom: bounds.bottom + 1e-6 } };
    }));
    return {
        nodeById,
        candidates(point: Position, radius: number) {
            if (!Number.isFinite(point.x) || !Number.isFinite(point.y) || !Number.isFinite(radius) || radius < 0) return [];
            const distance = Math.max(radius, 1e-6);
            const local = index.query({ left: point.x - distance, top: point.y - distance, right: point.x + distance, bottom: point.y + distance });
            return [...local, ...dynamic].sort((a, b) => order.get(b.id)! - order.get(a.id)!);
        },
    };
}
```

controller 内用 ref 按 nodes 数组身份缓存 `buildCanvasConnectionHitIndex`。单线函数在现有 world/handleRadius/isNearNode 声明后，以此替换原复制/filter/reverse/forEach：

```ts
for (const node of hitIndex.candidates(world, handleRadius)) {
    const scrollTop = scriptScrollTopById[node.id] || 0;
    const handleId = node.type === CanvasNodeType.Script ? storyboardHandleAtY(node, world.y, scrollTop) : node.type === CanvasNodeType.BatchTable ? batchReferenceHandleAtY(node, world.y, handleRadius) : undefined;
    if ((node.type === CanvasNodeType.Script || node.type === CanvasNodeType.BatchTable) && !handleId) continue;
    const anchor = getConnectionTargetAnchor(node, current, handleId, scrollTop, batchScrollById?.[node.id]);
    const dx = world.x - anchor.x;
    const dy = world.y - anchor.y;
    if (dx * dx + dy * dy > handleRadius * handleRadius) continue;
    isNearNode = true;
    const normalized = node.id === current.nodeId ? null : normalizeConnection(current.nodeId, node.id, nodesRef.current, current.handleType);
    if (!normalized || canvasConnectionError(config, nodesRef.current, connectionsRef.current, normalized)) continue;
    return { nodeId: node.id, handleId, anchorRatio: undefined, isNearNode };
}
return { nodeId: null, handleId: undefined, anchorRatio: undefined, isNearNode };
```

批量函数保留原 source 选择和 batch-cell 优先分支，后面的精确查询为：

```ts
for (const node of hitIndex.candidates(world, handleRadius)) {
    const scrollTop = scriptScrollTopById[node.id] || 0;
    const handleId = node.type === CanvasNodeType.Script ? storyboardHandleAtY(node, world.y, scrollTop) : node.type === CanvasNodeType.BatchTable ? batchReferenceHandleAtY(node, world.y, handleRadius) : undefined;
    if ((node.type === CanvasNodeType.Script || node.type === CanvasNodeType.BatchTable) && !handleId) continue;
    const anchor = getConnectionTargetAnchor(node, current, handleId, scrollTop, batchScrollById?.[node.id]);
    const dx = world.x - anchor.x;
    const dy = world.y - anchor.y;
    if (dx * dx + dy * dy > handleRadius * handleRadius) continue;
    isNearNode = true;
    if (!hasBatchConnectionCandidate(sourceNodeIds, node.id, nodesRef.current)) continue;
    return { nodeId: node.id, handleId, anchorRatio: undefined, isNearNode };
}
return { nodeId: null, isNearNode };
```

metadata/折叠/层级变化使节点数组失效即重建，抬手重验仍用当前 refs。不得按可见预算限制候选。

- [ ] **4. 新测试与三组回归通过。** 加旧算法 oracle 对照：重叠目标、非法目标覆盖合法目标、自己连自己、锁定、折叠、分镜滚动、表格单元格、目标在屏外。最终 drop 重新按最新图验证。
- [ ] **5. 真实连线采样比较每帧候选数，不只看平均 FPS。** 可提交摘要：`复用空间索引缩小连线目标查询范围`。

---

## Task 12：批量连线规划和草稿 Path 复用

**Files**
- Modify: `web/src/pages/canvas/use-canvas-connection-controller.ts:181-190,748-752`
- Modify: `web/src/components/canvas/canvas-leafer-graphics-layer.tsx:364-384`
- Regression: `web/test/canvas-batch-connection.test.ts`
- Create: `web/test/canvas-batch-preview-cache.test.ts`

**Interfaces**：不改提交用 `planBatchConnections`；预览缓存键含 source IDs、target ID/handle/ratio、nodes、connections、config，不含 mouseWorld。

- [ ] **1. 写调用次数测试：同一图/同一目标 100 次 mouseWorld 更新只规划一次；目标、handle、配置或 graph 引用变化必须重算。** Path 测试断言源集合不变时对象 ID 不变，取消预览后释放全部。
- [ ] **2. 旧实现预期规划次数和 new Path 次数为 100 次级别，记录失败，不使用墙钟阈值。**
- [ ] **3. 预览命中结果与鼠标坐标分开；提交仍重新规划。** 缓存保存在当前 hook ref，不跨项目持有。Leafer OverlayScene 加一个按 source ID 的 Map，复用以下模式：

```ts
const batch = props.batchConnectionPreview;
const nextIds = new Set(batch?.sourceNodeIds || []);
for (const [sourceId, path] of scene.batchDraftEntries) {
    if (!nextIds.has(sourceId)) {
        path.remove();
        scene.batchDraftEntries.delete(sourceId);
    }
}
scene.batchDrafts.visible = Boolean(batch);
if (!batch) return;
const target = batch.targetNodeId ? props.nodeById.get(batch.targetNodeId) : undefined;
const stroke = batch.status === "invalid" ? props.theme.accent.danger : batch.status === "partial" ? props.theme.node.activeStroke : props.theme.accent.primary;
for (const sourceNodeId of batch.sourceNodeIds) {
    const source = props.nodeById.get(sourceNodeId);
    if (!source) continue;
    let path = scene.batchDraftEntries.get(sourceNodeId);
    if (!path) {
        path = new Path({ strokeWidth: 1.4, strokeScaleFixed: true, strokeCap: "round", dashPattern: [8, 8], opacity: 0.72, hittable: false });
        scene.batchDrafts.add(path);
        scene.batchDraftEntries.set(sourceNodeId, path);
    }
    const handle: ConnectionHandle = { nodeId: source.id, handleType: "source" };
    path.set({
        path: activeConnectionPath(source, handle, batch.mouseWorld, target, props.scriptScrollTopById[source.id] || 0, batch.targetHandleId, props.batchScrollById?.[source.id], target ? props.batchScrollById?.[target.id] : undefined, target ? props.scriptScrollTopById[target.id] || 0 : 0),
        stroke,
    });
}
```

`OverlayScene` 增加 `batchDraftEntries: Map<string, Path>`，构造时初始化；场景卸载沿已有 `leafer.destroy(true)` 销毁，不额外双重 destroy。取消预览 remove 并清 Map；节点被删除时也从 nextIds 排除，避免孤 Path 留存。普通连接层现有复用不改。预览规划的单项缓存实现可置于 controller 私有区域：

```ts
type BatchPlanOptions = Parameters<typeof planBatchConnections>[0];
const previewPlanRef = useRef<{ options: BatchPlanOptions; plan: ReturnType<typeof planBatchConnections>; eligible: number } | null>(null);
const options: BatchPlanOptions = { sourceNodeIds, targetNodeId: targetNodeId || "", targetHandleId, targetAnchorRatio, nodes: nodesRef.current, connections: connectionsRef.current, config };
const old = previewPlanRef.current;
const reusable = old && old.options.nodes === options.nodes && old.options.connections === options.connections && old.options.config === options.config &&
    old.options.targetNodeId === options.targetNodeId && old.options.targetHandleId === options.targetHandleId && old.options.targetAnchorRatio === options.targetAnchorRatio &&
    old.options.sourceNodeIds.length === sourceNodeIds.length && old.options.sourceNodeIds.every((id, index) => id === sourceNodeIds[index]);
```

`previewPlanRef` 在 hook 顶层声明，options/reusable 在 previewBatchConnection 回调内计算；只有目标非空才执行 planner。可复用时读取 old.plan/eligible，否则执行现有 planner 和 eligible 计算并缓存；sourceNodeIds 存 `slice()` 防止调用方就地改动，clearBatchConnection 清 ref。最终 commitBatchConnection 不读取此缓存。

- [ ] **4. 跑 batch-connection 与缓存测试。** 覆盖部分成功、容量累计、重复源、重复边、Script 行、BatchTable 单源限制、目标内滚动和取消。
- [ ] **5. 审查缓存不复用最终提交的 ID/过期合法性结果。** 可提交摘要：`复用批量连线预览规划与草稿图形`。

---

## Task 13：Ctrl＋图片滚轮改成局部几何预览

**Files**
- Modify: `web/src/pages/canvas/use-canvas-image-wheel-resize.ts`
- Modify: `web/src/lib/canvas/canvas-live-viewport.ts`
- Modify: `web/src/components/canvas/canvas-leafer-graphics-layer.tsx`
- Modify: `web/src/pages/canvas/project.tsx`（传入 containerRef）
- Create: `web/test/canvas-image-wheel-preview.test.ts`
- Regression: `web/test/canvas-image-edit-copy.test.ts`、`canvas-node-drag-overlays.test.ts`

**Interfaces**
- New runtime type in live-viewport: `CanvasNodeGeometryPreview = { nodeId: string; position: Position; width: number; height: number }`。
- New functions: `applyCanvasNodeGeometryPreview(container, preview: CanvasNodeGeometryPreview | null, clearMode?: "restore" | "committed"): void`、`subscribeCanvasNodeGeometryPreview(container, listener): () => void`，沿用已有 drag-preview 事件模式。clearMode 默认 restore；committed 只清运行时状态并通知图形层，不恢复旧 DOM 样式。
- 新预览不写入 CanvasProject、journal 或服务器。

- [ ] **1. 写 rAF/计时器行为测试。** 连续 20 个 wheel 事件跨 5 帧：onResize=0 次、preview=5 次；idle/Control keyup 后 onResize=1 次；随后普通撤销一次恢复原尺寸。现实现 onResize=5 次，应失败。
- [ ] **2. 验证已有 `imageWheelSize` 比例/中心测试先通过，再建立上述失败基线。**
- [ ] **3. 保留 latestGeometry，不能每次 flush 后重新从 committed node 计算。**

hook 参数新增 `containerRef: RefObject<HTMLDivElement | null>`。预览累积、flush、finish 的核心替换如下；保留现有 isFinite、单选、图片类型、locked/layer guard 和事件注册。

```ts
type WheelGeometry = ReturnType<typeof imageWheelSize>;
const latestGeometry = useRef<{ id: string; size: WheelGeometry } | null>(null);
const initialNode = useRef<CanvasNodeData | null>(null);
const dirtyPreview = useRef(false);
const sameGeometry = (a: CanvasNodeData, b: CanvasNodeData) => a.width === b.width && a.height === b.height && a.position.x === b.position.x && a.position.y === b.position.y;
const flush = useCallback(() => {
    if (frame.current !== null) cancelAnimationFrame(frame.current);
    frame.current = null;
    if (!dirtyPreview.current || !latestGeometry.current) return;
    dirtyPreview.current = false;
    const next = latestGeometry.current;
    applyCanvasNodeGeometryPreview(containerRef.current, { nodeId: next.id, ...next.size });
}, [containerRef]);
```

每个通过原 guard 的 wheel 分支替换原 pending 赋值：

```ts
if (!active.current) initialNode.current = node;
active.current = true;
historyPausedRef.current = true;
const latest = latestGeometry.current;
const geometryNode = latest?.id === node.id ? { ...node, ...latest.size } : node;
latestGeometry.current = { id: node.id, size: imageWheelSize(geometryNode, delta) };
dirtyPreview.current = true;
if (frame.current === null) frame.current = requestAnimationFrame(flush);
if (timer.current) clearTimeout(timer.current);
timer.current = setTimeout(finish, 240);
```

finish 在执行最后一次预览后解除暂停并只提交一次：

```ts
const finish = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    if (!active.current) return;
    flush();
    const next = latestGeometry.current;
    const start = initialNode.current;
    const current = next ? nodesRef.current.find((node) => node.id === next.id) : undefined;
    active.current = false;
    historyPausedRef.current = false;
    if (next && start && current && !current.metadata?.locked && sameGeometry(current, start)) {
        flushSync(() => onResize(next.id, next.size.width, next.size.height, next.size.position));
        applyCanvasNodeGeometryPreview(containerRef.current, null, "committed");
    } else {
        applyCanvasNodeGeometryPreview(containerRef.current, null, "restore");
    }
    latestGeometry.current = null;
    initialNode.current = null;
    dirtyPreview.current = false;
}, [containerRef, flush, historyPausedRef, nodesRef, onResize]);
```

`flushSync` 从现有 `react-dom` 导入，仅在手势最终提交使用，绝不在 wheel/rAF 过程中调用。这样 React DOM 和 Leafer committed props 已更新后，再清预览，避免清预览把新尺寸恢复成旧尺寸并留下一帧回跳。删除原 finish 的 `setNodes(current => [...current])`，因为最终 onResize 已发布真实几何；无变化时现有 handleNodeResize 原引用返回。cleanup 取消 frame/timer、用 restore 清理几何预览与 latest/initial refs、解除暂停但不调用 onResize。

live-viewport 的 DOM 实现以目标 node 元素为键保存原 `transform/width/height`；设置时用 `translate(xpx, ypx)` 与像素尺寸。restore 清理恢复原值；committed 清理仅丢弃保存的旧样式，不覆盖 React 已提交的新值。新增事件的 payload 是完整 CanvasNodeGeometryPreview，必须与 DOM 更新在同一调用内发送，不能用 React state 传递。Leafer 按 nodeId 查现有 `connectionIdsByNodeId`，调用既有 `canvasConnectionPath` 时仅用 `{ ...entry.from/to, ...previewGeometry }` 替换受影响端点；clear 用 committed props 重算相同邻接边。

具体 DOM 预览修改目标元素的 transform/width/height，并保存原内联值以恢复；只查当前节点，不扫描全部节点。Leafer 收到同一帧几何事件后只更新该节点的邻接边；SVG 命中层在手势中沿用交互抑制，结束后用 committed geometry 恢复。

必须实现这些收尾：240ms idle、Control keyup、pointerdown、blur 正常提交；项目卸载或账号切换取消旧项目预览，恢复 pause，不向新项目提交。节点被删除/锁定或其几何被远端改变时取消本地尺寸覆盖，其他 metadata 变化用最新节点保留。终值与初值相同时不制造空撤销点。`manualSize` 仍由现有 handleNodeResize 设置。

- [ ] **4. 新测试与两组回归通过。** 单次手势节点/连线空间索引重建最多一次；普通 wheel 平移/缩放和触控板 pinch 不走此分支。真实图检查图片、连接端点、选框同步，缩放中心不漂移。
- [ ] **5. 审查不把只支持 translate 的 drag helper 当尺寸 helper；不逐帧修改根继承 CSS 变量。** 可提交摘要：`将图片滚轮尺寸更新改为局部预览与单次提交`。

---

## Task 14：保存和历史的引用快路，不改持久化协议

**Files**
- Modify: `web/src/services/user-data-sync.ts:1150,1594-1598`
- Modify: `web/src/services/user-data-sync-media.ts:74-116`
- Modify: `web/src/pages/canvas/use-canvas-history.ts:203`
- Modify only with verified counts: `web/src/stores/canvas/use-canvas-store.ts:168-186,628-648`
- Create: `web/test/canvas-save-reference-fast-path.test.ts`
- Regression: `canvas-sync-progress.test.ts`、`canvas-persistence-loss.test.ts`、`canvas-storage-revision.test.ts`、`generation-storage-consistency.test.ts`

**Interfaces**：现有公开同步和存储格式不变；所有资源转换失败继续抛出。不可把普通保存直接变成生成产物确认。

- [ ] **1. 写纯准备测试与调用计数测试。**

```ts
import { expect, test } from "bun:test";
import { ensureRemoteResourceReferences } from "../src/services/user-data-sync-media";

test("unchanged non-resource trees keep identity", async () => {
    const value = { title: "plain", nodes: [{ id: "a", metadata: { prompt: "hello" } }], connections: [] };
    expect(await ensureRemoteResourceReferences(value)).toBe(value);
});
```

定向同步测试在既有 sync-progress harness 上对非目标项目的内容比较设置计数，要求 0 次，而不只是“未发送请求”。历史测试只改聊天时未变 nodes/connections 不构造实体差分。

- [ ] **2. 跑新测试；旧 prepare 总是重建树，应在 identity 断言失败。**
- [ ] **3. 提前 selected 判断，增加引用早退和写时复制。**

```ts
const dirtyProjects = currentProjects.filter((project) => {
    if (selectedProjectIds !== undefined && !selectedProjectIds.has(project.id)) return false;
    return !remoteDeletedProjects.has(project.id) && (pendingCanvasMutations.has(project.id) || !sameCanvasContent(acknowledgedProjects.get(project.id), project));
});
```

`createEntityPatch` 开头加 `if (before === after) return undefined;`。`diffCanvasObject` 的实际返回结构是 `{ patch, previous }`，在两个空对象声明后加：

```ts
if (before === after) return { patch, previous };
```

循环内部读出 oldValue/newValue 后加 `if (oldValue === newValue) continue;`，仍保留 undefined/null 和原字段排序逻辑。`canvasOperationValueEqual` 改为 `return left === right || canonicalize(left) === canonicalize(right);`，不要用随机 hash 替代精确比较。

递归准备的数组分支使用：

```ts
if (Array.isArray(value)) {
    let result: unknown[] | undefined;
    for (let index = 0; index < value.length; index += 1) {
        const current = value[index];
        const prepared = await ensureRemoteResourceReferencesValue(current, uploaded, onUploaded, currentPreserveTextContent);
        if (result) result[index] = prepared;
        else if (prepared !== current) {
            result = value.slice();
            result[index] = prepared;
        }
    }
    return (result || value) as T;
}
```

对象分支使用下面的写时复制初始化，后续已有 storageKey/inline/local 上传判断不变：

```ts
const original = value as Record<string, unknown>;
let changed: Record<string, unknown> | undefined;
for (const [key, child] of Object.entries(original)) {
    const prepared = await ensureRemoteResourceReferencesValue(child, uploaded, onUploaded, currentPreserveTextContent);
    if (prepared !== child) {
        changed ||= { ...original };
        changed[key] = prepared;
    }
}
const next = changed || original;
```

现有 `applyResourceReference` 会原地赋值，因此不能直接把原对象传入。保持此公开 helper 的现有行为，给本递归路径增加下面的私有纯包装，并将此递归函数中的四个调用改用包装：

```ts
function preparedResourceReference(payload: Record<string, unknown>, storageKey: string, preserveTextContent: boolean) {
    const rewritten = applyResourceReference({ ...payload }, storageKey, preserveTextContent);
    const unchanged = Object.keys(rewritten).length === Object.keys(payload).length &&
        Object.entries(rewritten).every(([key, child]) => child === payload[key]);
    return unchanged ? payload : rewritten;
}
```

最后的 `rebaseCanvasBatchInputSignatures` 已在无 uploaded/no tokens/no changed children 时保留引用，不重复改写它。上传顺序、失败传播与签名重建只证明同一资源身份的规则保持不变。

持久化进度仅为实际变化项目发布，已有相同字段则不通知；但所有 queued 项目的失败/成功状态都要结算，不能只以最后一次局部 patch 决定范围。先补这一计数测试再动 store。450000 字节门槛、400/0ms 调度、writer/token、锁、tombstone、生成确认均不改。

- [ ] **4. 跑新增测试及四组一致性回归。** 包含资源失败、未知网络结果重放、保存期间新编辑、账号切换、同 scope 多标签、离线关闭恢复；记录外部 fixture 依赖造成的既有失败，不改成假通过。
- [ ] **5. 审查 JSON/journal 格式完全未变。** 可提交摘要：`保持保存准备引用并跳过无关画布比较`。

---

## Task 15：SSE 批内节点更新快路（有回写热点时启用）

**启用门槛**：Task 1/14 后，在隔离图真实或可控本地 SSE 回放中，批量任务更新的重复 Map 构建仍进入前五主线程栈，或产生可归因的 >50ms 长任务。未触发则记录不执行；不为完整性强造第二套 merge 引擎。

**Files**
- Modify: `web/src/lib/canvas/agent-canvas-patch.ts:169-194`
- Modify: `web/src/services/user-data-sync.ts:485-511`
- Create: `web/test/agent-canvas-patch-batch.test.ts`
- Regression: `web/test/agent-canvas-sync.test.ts`、`canvas-converged-create-replay.test.ts`

**Interfaces**
- New export: `applyAgentCanvasPatchBatch(project: CanvasProject, patches: AgentCanvasPatch[]): CanvasProject`。
- 首版仅优化所有 change 均为 before/after 同 ID 的既有节点更新且 connections changes 为空的批次。create/delete/边变更用现有单 patch 路径。
- 版本排序、去重、连续验证留在 service；全部验证成功后再应用两个私有工作集（remote/projected），最后发布。

- [ ] **1. 写 oracle 测试，不读真实 `.local` 数据。**

```ts
import { expect, test } from "bun:test";
import { applyAgentCanvasPatch, applyAgentCanvasPatchBatch, type AgentCanvasPatch } from "../src/lib/canvas/agent-canvas-patch";
import { performanceProject } from "./helpers/canvas-performance-fixture";

test("batch application matches ordered single patch semantics", () => {
    const base = performanceProject(1000);
    const patches: AgentCanvasPatch[] = base.nodes.slice(0, 50).map((node, index) => ({
        canvasId: base.id, baseRevision: index, revision: index + 1, updatedAt: `update-${index}`,
        nodes: [{ before: node, after: { ...node, title: `${node.title}-done` } }], connections: [],
    }));
    const expected = patches.reduce((project, patch) => applyAgentCanvasPatch(project, patch), base);
    expect(applyAgentCanvasPatchBatch(base, patches)).toEqual(expected);
    expect(base.nodes[0].title).toBe("perf-0");
});
```

- [ ] **2. 新导出前失败；补重复更新同节点、任务身份切换、终态不倒退、局部标题/位置并发、第二条冲突全批不发布。**
- [ ] **3. 提取现有 task-observation 归一函数，单 patch 与 batch 共同复用，不能复制两份安全逻辑。** batch 一次建立节点 Map，逐 revision 先根据该 revision 开始状态生成 normalized changes，再按原 mergeThreeWayValue 顺序更新工作 Map；最后一次物化 nodes。无变化返回原 project。结构批次用：

```ts
return patches.reduce((current, patch) => applyAgentCanvasPatch(current, patch), project);
```

service 先构造连续且未过期的 applicable patches，再分别调用 batch；把最后 revision 写回现有项目副本。保持 hash/epoch/当前项目身份复核、listeners、ack baseline、落盘确认顺序。

- [ ] **4. 新测试及两组回归通过；计数验收 50 个纯更新事件不再产生 50 次全节点 Map 构建。** 不以“50 事件只调用一次 mock apply”替代真实 reducer 验证。
- [ ] **5. 审查失败时原 project 与 store 不被私有 Map 变更污染，结构批次行为完全沿旧路径。** 可提交摘要：`复用 Agent 节点更新批次的工作索引`。

批次 reducer 的实现骨架以下列完整函数为准。`normalizeAgentNodeChanges` 从现有 `applyAgentCanvasPatch` 的 task 检查/归一代码提取，单条和批量共同使用；服务端连续 revision 校验仍不放进此纯函数。

```ts
function normalizeAgentNodeChanges(byId: ReadonlyMap<string, CanvasNodeData>, changes: AgentCanvasPatch["nodes"]) {
    return changes.map((change) => {
        if (!change.after) return change;
        const current = byId.get(change.after.id);
        const beforeTask = change.before?.metadata?.taskId;
        const afterTask = change.after.metadata?.taskId;
        if (beforeTask && current?.metadata?.taskId !== beforeTask && current?.metadata?.taskId !== afterTask) throw new Error("画布增量冲突：生成节点已删除或绑定另一任务，不能覆盖");
        if (!current || !beforeTask || beforeTask !== afterTask) return change;
        const terminal = (status: string | undefined) => ["succeeded", "failed", "cancelled"].includes(status || "");
        if (terminal(current.metadata?.taskStatus) && !terminal(change.after.metadata?.taskStatus)) return { before: current, after: current };
        if (terminal(current.metadata?.taskStatus)) return change;
        const metadata = { ...change.before!.metadata };
        for (const key of ["status", "taskStatus", "taskProgress", "taskStage"] as const) {
            if (!equal(metadata[key], change.after.metadata?.[key])) Object.assign(metadata, { [key]: current.metadata?.[key] });
        }
        return { ...change, before: { ...change.before!, metadata } };
    });
}

export function applyAgentCanvasPatchBatch(project: CanvasProject, patches: AgentCanvasPatch[]): CanvasProject {
    for (const patch of patches) {
        if (patch.canvasId !== project.id || !Array.isArray(patch.nodes) || !Array.isArray(patch.connections)) throw new Error("画布增量不属于当前画布或格式无效");
    }
    const updateOnly = patches.every((patch) => patch.connections.length === 0 && patch.nodes.every((change) => change.before && change.after && change.before.id === change.after.id));
    if (!updateOnly) return patches.reduce((current, patch) => applyAgentCanvasPatch(current, patch), project);
    const byId = new Map(project.nodes.map((node) => [node.id, node]));
    let changed = false;
    let updatedAt = project.updatedAt;
    for (const patch of patches) {
        const normalized = normalizeAgentNodeChanges(byId, patch.nodes);
        let patchChanged = false;
        for (const { before, after } of normalized) {
            const id = after!.id;
            const current = byId.get(id);
            const merged = mergeThreeWayValue(current ?? null, before, after) as CanvasNodeData;
            if (merged !== current) {
                byId.set(id, merged);
                patchChanged = true;
            }
        }
        if (patchChanged) {
            changed = true;
            updatedAt = patch.updatedAt || updatedAt;
        }
    }
    if (!changed) return project;
    return { ...project, nodes: project.nodes.map((node) => byId.get(node.id)!), updatedAt };
}
```

更新同一 revision 内同 ID 多次时先把 normalized 数组算完，随后 merge 按原变化顺序执行，保持现有行为。若 batch 先改后还原，允许返回语义相等的新节点数组；不能因此跳过后续 baseline/hash 校验。

---

## Task 16：真实路由验收、文档与交付

**Files**
- Modify after implementation: `docs/content/docs/progress/pending-test.mdx`
- Modify after implementation: `docs/design/canvas-final-performance-architecture.md`
- Create during execution: `.local/cache/canvas-performance/after.md`、`comparison.md`、必要 traces；不提交原始账号数据/签名 URL。
- Optional human-readable report: `docs/superpowers/reports/2026-10-28-canvas-performance-verification.md`（只写脱敏结果与证据路径）。

**Interfaces**：用户可见交互不变；交付说明明确每个任务“已实现/测试通过/真实浏览器通过/未启用/阻塞”。

- [ ] **1. 按任务运行专项测试，再执行 `bun run build` 和 `bun run lint`。** 不能通过改测试字面量使行为回归消失；环境预存失败单独列出。所有测试在当前分支实际工作树上运行，不能引用历史通过记录。
- [ ] **2. 在真实 `/canvas/:id` 与 `/share/canvas/:token` 上复测。** 每个基线场景相同环境重跑 3 次；单独记录 dev StrictMode 与 production，不混合比较。覆盖明暗主题、小地图开/关、侧栏开/关、隐藏连线开/关、冷/热缓存、5%/10%/25%/100%/200% 缩放。
- [ ] **3. 检查硬性正确性门槛。**
  - 所有应可见节点仍可见；没有靠预算删掉屏内节点。
  - 远景连线仍可点击/右键；分镜/表格端口能准确连接。
  - 图片往返平移不成片闪白；视频没有无意批量播放。
  - 引用顺序、Config 覆盖、角色/绘图编号、断边重编号不变。
  - 单次拖动/尺寸手势产生一个撤销点；Esc/cancel/失焦不留下预览样式。
  - 保存后刷新节点/连线/提示词不丢；离线、重连、账号切换与生成回写安全合同通过。
- [ ] **4. 检查结构性性能门槛。**
  - 同 nodes 的 viewport/revision-only 内容 hash Promise 复用。
  - 关闭 workspace 时搜索次数 0。
  - 纯 viewport preview 的小地图节点底图重画次数 0。
  - 关闭 hideNodeConnections 时节点 hover 不写页级死状态。
  - 图语义不变时，视口/分批挂载不重建资源输入图索引。
  - 普通稀疏图连线命中候选不随远处 N 线性增长；吸附结果与线性 oracle 一致。
  - Ctrl 图片滚轮过程节点全局提交 0 次，结束有效几何提交 1 次。
  - 同目标批量草稿没有持续 new/destroy Path；SSE 条件任务启用后批内纯更新索引只建一次。
- [ ] **5. 对帧率只报告实测。** 目标设备上，1000 节点/约 50 可见的暖缓存导航以接近 60Hz 帧预算为优化目标；不承诺全部设备/5000 同屏都 60fps。报告 P95、max、>50ms 长任务和输入延迟。三次对照的关键场景中位数不能出现超过 10% 且超出基线波动的退化；若原卡顿主要栈仍存在，不能因局部计数达标宣布任务整体解决。
- [ ] **6. 更新文档状态。** 旧设计文档标记已实现 LOD/Canvas 小地图/已有队列，引用本文；新增 pending-test 条目按确实执行的证据勾选。保留没有真实设备验证的项，不改根 README 不新增未经证实的“万级流畅”宣传。
- [ ] **7. 汇总聚焦 diff 与风险。** 不清理其他会话改动，不提交本地 ZIP/trace/数据库。用户授权提交时每任务独立提交，先指定路径暂存再检查 staged diff；默认停在已验证未提交状态。

---

## 3. 明确不纳入本轮默认实施的项目

这些不是未定义的实现步骤，而是有意排除的范围；触发后应单独批准新计划，不能由执行者顺手扩展。

### 3.1 恢复日志格式与分库持久化

触发条件：完成 Task 2/14 后，真实输入/保存记录仍由 journal stringify 或整 scope 序列化主导，且有 >50ms 可归因长任务。另行设计字段级 before/after 日志与按画布存储；必须覆盖 writer/token、崩溃窗口、跨标签锁、tombstone、generationEffectKeys 和未知网络结果重放。**本轮不改变 450000 字节门槛，不删除 journal，不把唯一恢复保障推迟。**

### 3.2 全景密集节点的渲染预算

触发条件：前述算法/引用开销已下降，但大量同屏时 React mount/commit 或 raster 仍超预算。现有 12/rAF、4ms 只测 ID 入队，不覆盖渲染成本。下一计划可采用“先保留轻量表示，再按真实提交成本升级正文”，但必须保持 active/editing/media 的强制优先及图片留帧。不能先硬裁屏内节点，也不在本轮引入全局自适应调度器。

### 3.3 SVG 热区替代

触发条件：真实同屏大量连线确认 DOM hit path 是主要剩余热点。允许研究 Canvas/Leafer 等价命中或局部 SVG 候选，但必须覆盖贝塞尔距离、屏幕像素命中宽度、重叠优先级、右键菜单、缩放、脚本/表格滚动和读写模式。**未有等价方案之前保留当前 SVG 层。**

### 3.4 Worker / OffscreenCanvas / 引擎替换

上述局部优化和测量未证明需要跨线程/换引擎前不做。复制整图到 Worker、维护双状态和消息协议也有成本，不能作为默认答案。

## 4. 专项命令清单

所有命令在 `web/` 执行；文件名对应各任务产物，不需要先运行整个 `bun test`。新增测试在实现前应因目标行为不满足失败，实施后预期 `0 fail`；环境/导入错误要单独定位，不算业务 red/green 证据。

| 任务 | 最小验证命令 |
| --- | --- |
| 1 | `bun test test/canvas-performance-fixture.test.ts` |
| 2 | `bun test test/canvas-content-cache.test.ts test/canvas-updatedat-bookkeeping.test.ts` |
| 3 | `bun test test/canvas-render-invalidation.test.ts test/canvas-node-toolbar.test.ts test/canvas-node-drag-overlays.test.ts test/canvas-node-title-interaction.test.ts` |
| 4 | `bun test test/canvas-reference-index.test.ts test/canvas-resource-references.test.ts test/canvas-batch-references.test.ts test/canvas-character-reference.test.ts` |
| 5 | `bun test test/canvas-workspace-sidebar.test.ts test/canvas-node-search.test.ts` |
| 6 | `bun test test/canvas-minimap-update.test.ts` |
| 7 | `bun test test/canvas-batch-references.test.ts test/canvas-batch-column-inheritance.test.ts test/canvas-batch-row-identity.test.ts` |
| 8 | `bun test test/canvas-media-performance.test.ts test/canvas-node-lod.test.ts`；DOM 专项单独 `bun test test/fixtures/canvas-image-lod.harness.tsx` |
| 9 | `bun test test/canvas-reference-preview-demand.test.ts test/canvas-resource-mention-editor.test.ts test/canvas-text-concurrent-input.test.ts test/canvas-resource-references.test.ts` |
| 10 | `bun test test/canvas-alignment-index.test.ts test/canvas-selection.test.ts` |
| 11 | `bun test test/canvas-connection-hit-index.test.ts test/canvas-connection-create-menu.test.ts test/canvas-batch-cell-geometry.test.ts test/canvas-connection-tilt.test.ts` |
| 12 | `bun test test/canvas-batch-preview-cache.test.ts test/canvas-batch-connection.test.ts` |
| 13 | `bun test test/canvas-image-wheel-preview.test.ts test/canvas-image-edit-copy.test.ts test/canvas-node-drag-overlays.test.ts` |
| 14 | `bun test test/canvas-save-reference-fast-path.test.ts test/canvas-sync-progress.test.ts test/canvas-persistence-loss.test.ts test/canvas-storage-revision.test.ts test/generation-storage-consistency.test.ts` |
| 15 | `bun test test/agent-canvas-patch-batch.test.ts test/agent-canvas-sync.test.ts test/canvas-converged-create-replay.test.ts` |
| 16 | `bun run build`；`bun run lint`；真实路由浏览器验收 |

现有 `web/test/agent-canvas-patch.test.ts` 含读取本机 `.local/canvas-conflict-steps.json` 的用例，不能当作可移植基线；新增 batch 测试全部使用内联夹具。若集成全测试被此类本机文件阻塞，应报告，不创建伪造生产证据文件。

## 5. 实施检查点与回退边界

1. 完成 Task 1 后固定基线；没有基线不能谈提升倍数。
2. 完成 A 包后先复测悬停/导航/输入；若已有足够收益，可先交付 A，不等待 B/C 合并大改。
3. B 包按单任务落地并校对原算法，不在一个 diff 中同时更改吸附规则和连接合法性。
4. C 包保持协议不变；Task 15 必须先满足启用门槛。
5. 回退只撤销本任务的准确 hunk/已授权提交，不回退用户或其他会话文件。无 schema 变化，所以 A/B 回退不涉及数据迁移。
6. 性能优化不能靠关闭功能验收：小地图、连线热区、引用菜单、保存功能最终都要在正常启用状态通过。

## 6. 计划自审记录

- [x] 两份分析的每个主要方向已对应任务或明确排除项。
- [x] scale 的真实消费端、图片留帧、SVG 命中职责已纠正，未照搬有退化风险的建议。
- [x] 没有把新命名的引用 bundle 当成既有 API；新接口在所属任务定义。
- [x] 没有把普通拖动、viewport 或特殊图片滚轮误写成逐帧持久化。
- [x] journal、SSE revision、账号隔离与资源失败语义没有因性能目标被放宽。
- [x] 静态/纯函数测试与真实浏览器性能证据明确区分。
- [x] 测试/采样是实施时的步骤，本文没有声称已执行或已通过。

**交付状态：计划已编写；业务实现未开始。推荐实施方式是每任务独立执行与审查，按共享文件边界串行整合；也可在同一会话用 executing-plans 分包执行。选择执行方式不代表可以自动推送或修改真实数据。**
