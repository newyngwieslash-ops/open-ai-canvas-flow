import { readFileSync } from "node:fs";
import { expect, test } from "bun:test";
import { applyAgentCanvasPatch, mergeAgentCanvasEditor, type AgentCanvasPatch } from "@/lib/canvas/agent-canvas-patch";
import { isCanvasNodeGenerating } from "@/lib/canvas/canvas-node-task-state";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

const node: CanvasNodeData = { id: "video-1", title: "满月动画", type: CanvasNodeType.Video, position: { x: 10, y: 20 }, width: 320, height: 180, metadata: { status: "loading", taskId: "task-1", taskStatus: "running", prompt: "原始提示词" } };
const project: CanvasProject = { id: "canvas", title: "画布", nodes: [node], connections: [], chatSessions: [], activeChatId: null, viewport: { x: 12, y: 24, k: 1 }, createdAt: "2026-09-13", updatedAt: "2026-09-13" };
const failed: CanvasNodeData = { ...node, metadata: { ...node.metadata, status: "error", taskStatus: "failed", errorDetails: "上游拒绝" } };
const patch: AgentCanvasPatch = { canvasId: project.id, updatedAt: "2026-09-13T12:00:00Z", nodes: [{ before: node, after: failed }], connections: [] };

test("failed Agent task patches only its node and unlocks generation", () => {
    expect(isCanvasNodeGenerating(node)).toBe(true);
    const result = applyAgentCanvasPatch(project, patch);
    expect(result.nodes[0]).toEqual(failed);
    expect(isCanvasNodeGenerating(result.nodes[0])).toBe(false);
    expect(result.viewport).toBe(project.viewport);
    expect(result.connections).toBe(project.connections);
    expect(project.nodes[0].metadata?.status).toBe("loading");
});

test("independent task error text yields to the cloud snapshot", () => {
    const local = { ...node, metadata: { ...node.metadata, prompt: "正在编辑的新提示词", errorDetails: "本地观察者错误" } };
    const remoteFailed = { ...failed, metadata: { ...failed.metadata, errorDetails: "模型服务暂时不可用（HTTP 503）" } };
    const merged = mergeAgentCanvasEditor(project, { ...project, nodes: [remoteFailed] }, [local], [], { ...project, nodes: [local] });
    expect(merged.nodes[0]?.metadata?.prompt).toBe("正在编辑的新提示词");
    expect(merged.nodes[0]?.metadata?.errorDetails).toBe("模型服务暂时不可用（HTTP 503）");
});

test("unrelated local edits and editor-only dragging survive completion", () => {
    const local = { ...node, position: { x: 800, y: 400 }, metadata: { ...node.metadata, prompt: "正在编辑的新提示词" } };
    const merged = mergeAgentCanvasEditor(project, applyAgentCanvasPatch(project, patch), [local], []);
    expect(merged.nodes[0].position).toEqual(local.position);
    expect(merged.nodes[0].metadata?.prompt).toBe("正在编辑的新提示词");
    expect(merged.nodes[0].metadata?.status).toBe("error");
});

test("replay is idempotent and cannot roll a completed node back to loading", () => {
    const result = applyAgentCanvasPatch(project, patch);
    expect(applyAgentCanvasPatch(result, patch)).toBe(result);
    const old: AgentCanvasPatch = { ...patch, nodes: [{ before: { ...node, metadata: { status: "idle" } }, after: node }] };
    expect(() => applyAgentCanvasPatch(result, old)).toThrow("冲突");
    expect(result.nodes[0].metadata?.status).toBe("error");
});

test("new nodes and reference connections are applied atomically", () => {
    const draft = { ...node, id: "video-2", metadata: { status: "idle" as const } };
    const edge = { id: "edge", fromNodeId: node.id, toNodeId: draft.id };
    const result = applyAgentCanvasPatch(project, { ...patch, nodes: [{ before: null, after: draft }], connections: [{ before: null, after: edge }] });
    expect(result.nodes[0]).toBe(node);
    expect(result.nodes[1]).toEqual(draft);
    expect(result.connections).toEqual([edge]);
});

test("replayed create patch converges when the node already progressed locally", () => {
    // SSE 重放（after=0 全量回放）会把 create 型增量（before=null）重新投递到
    // 已经接收过该节点并观察到任务终态的本地画布上。本地节点是 after 的后继
    // 状态（queued→failed）时必须收敛，不能把"创建重放"当冲突弹给用户。
    const created: CanvasNodeData = { ...node, id: "img-1", metadata: { status: "loading", taskId: "task-1", taskStatus: "queued" } };
    const progressed: CanvasNodeData = { ...created, metadata: { ...created.metadata, status: "error", taskStatus: "failed", errorDetails: "模型服务暂时不可用（HTTP 503）" } };
    const replay: AgentCanvasPatch = { canvasId: project.id, updatedAt: "2026-09-13T12:00:00Z", nodes: [{ before: null, after: created }], connections: [] };
    const result = applyAgentCanvasPatch({ ...project, nodes: [progressed] }, replay);
    expect(result.nodes[0]).toEqual(progressed);
    expect(() => applyAgentCanvasPatch({ ...project, nodes: [progressed] }, replay)).not.toThrow();
});

test("replayed create patch still rejects a divergent local node", () => {
    // 本地对同一节点做了不同方向的真实编辑（比如改了标题），创建重放不能静默
    // 吞掉本地差异后回退成服务器快照，也不能误判收敛。
    const created: CanvasNodeData = { ...node, id: "img-1", title: "服务器标题", metadata: { status: "loading", taskId: "task-1", taskStatus: "queued" } };
    const divergent: CanvasNodeData = { ...created, title: "本地改过的标题", metadata: { ...created.metadata, status: "error", taskStatus: "failed", errorDetails: "503" } };
    const replay: AgentCanvasPatch = { canvasId: project.id, updatedAt: "2026-09-13T12:00:00Z", nodes: [{ before: null, after: created }], connections: [] };
    expect(() => applyAgentCanvasPatch({ ...project, nodes: [divergent] }, replay)).toThrow("冲突");
});

test("真实创建链路的临时空字段可与终态收敛", () => {
    const steps = JSON.parse(readFileSync("../.local/canvas-conflict-steps.json", "utf8")) as AgentCanvasPatch[];
    const baseline: CanvasProject = { id: steps[0]!.canvasId, title: "画布", nodes: [], connections: [], chatSessions: [], activeChatId: null, viewport: { x: 0, y: 0, k: 1 }, createdAt: "", updatedAt: "" };
    let remote = baseline;
    for (const step of steps) remote = applyAgentCanvasPatch(remote, step);
    const finalNode = remote.nodes.find((node) => node.id === "img-fig1-ermaduo-v2");
    const local: CanvasProject = { ...baseline, nodes: [finalNode as CanvasNodeData] };
    expect(() => mergeAgentCanvasEditor(baseline, remote, local.nodes, local.connections, local)).not.toThrow();
});

test("replayed multi-step creation sequence converges onto its own final state", () => {
    // 真实 SSE 重放序列：seq8 创建草案（before=null，无 imageParameters）、
    // seq11 首次绑定任务并补充 imageParameters/generationSpec、seq13 任务终态。
    // 逐 patch 应用到"已终态的本地"会在 seq8 就把中间草案与终态比较并抛冲突，
    // 这是 applyAgentCanvasPatches（服务层重放循环）必须避免的语义。正确的重放：
    // 从基线逐 patch 重建远端终态，再与终态本地做一次三路合并（mergeAgentCanvasEditor）。
    const draft: CanvasNodeData = { ...node, id: "img-1", metadata: { status: "loading", taskId: "task-1", taskStatus: "queued", prompt: "原始提示词", count: 1, agentDraftRunId: "run-1", content: "" } };
    const bound: CanvasNodeData = { ...draft, metadata: { ...draft.metadata, status: "loading", taskId: "task-1", taskStatus: "queued", imageParameters: "{\"count\":1,\"quality\":\"high\"}" } };
    const failedFinal: CanvasNodeData = { ...bound, metadata: { ...bound.metadata, status: "error", taskStatus: "failed", errorDetails: "模型服务暂时不可用（HTTP 503）" } };
    const sequence: AgentCanvasPatch[] = [
        { canvasId: project.id, revision: 49, baseRevision: 48, updatedAt: "2026-09-13T12:00:01Z", nodes: [{ before: null, after: draft }], connections: [] },
        { canvasId: project.id, revision: 50, baseRevision: 49, updatedAt: "2026-09-13T12:00:02Z", nodes: [{ before: draft, after: bound }], connections: [] },
        { canvasId: project.id, revision: 51, baseRevision: 50, updatedAt: "2026-09-13T12:00:03Z", nodes: [{ before: bound, after: failedFinal }], connections: [] },
    ];
    // 基线（无该节点）逐 patch 重建得到远端终态。
    let remote = { ...project, nodes: [] as CanvasNodeData[] };
    for (const step of sequence) remote = applyAgentCanvasPatch(remote, step);
    expect(remote.nodes[0]).toEqual(failedFinal);
    // 终态本地与重建终态做整体三路合并：收敛、无冲突、保留终态。
    const local = { ...project, nodes: [failedFinal] };
    const merged = mergeAgentCanvasEditor({ ...project, nodes: [] as CanvasNodeData[] }, remote, local.nodes, local.connections, local);
    expect(merged.nodes[0]).toEqual(failedFinal);
});

test("Agent refresh applies server reference order when local connections were untouched", () => {
    const imageOne = { id: "image-one", fromNodeId: "image-one", toNodeId: node.id };
    const imageTwo = { id: "image-two", fromNodeId: "image-two", toNodeId: node.id };
    const characterOne = { id: "character-one", fromNodeId: "character-one", toNodeId: node.id };
    const characterTwo = { id: "character-two", fromNodeId: "character-two", toNodeId: node.id };
    const previous = { ...project, connections: [characterOne, characterTwo, imageOne, imageTwo] };
    const incoming = { ...previous, revision: 2, connections: [imageOne, imageTwo, characterOne, characterTwo] };
    const merged = mergeAgentCanvasEditor(previous, incoming, previous.nodes, previous.connections, previous);

    expect(merged.connections.map((connection) => connection.id)).toEqual(["image-one", "image-two", "character-one", "character-two"]);
});

test("Agent refresh preserves moving the first reference to the fifth position locally", () => {
    const roleOne = { id: "role-one", fromNodeId: "role-one", toNodeId: node.id };
    const imageTwo = { id: "image-two", fromNodeId: "image-two", toNodeId: node.id };
    const roleThree = { id: "role-three", fromNodeId: "role-three", toNodeId: node.id };
    const imageFour = { id: "image-four", fromNodeId: "image-four", toNodeId: node.id };
    const imageFive = { id: "image-five", fromNodeId: "image-five", toNodeId: node.id };
    const previous = { ...project, connections: [roleOne, imageTwo, roleThree, imageFour, imageFive] };
    const incoming = { ...previous, revision: 2, connections: [imageFive, roleOne, imageTwo, roleThree, imageFour] };
    const local = { ...previous, connections: [imageTwo, roleThree, imageFour, imageFive, roleOne] };
    const merged = mergeAgentCanvasEditor(previous, incoming, local.nodes, local.connections, local);

    expect(merged.connections.map((connection) => connection.id)).toEqual(["image-two", "role-three", "image-four", "image-five", "role-one"]);
});

test("deleted nodes and a newer task binding are not overwritten", () => {
    expect(() => applyAgentCanvasPatch({ ...project, nodes: [] }, patch)).toThrow("冲突");
    // A replacement task can have the same running status; its task identity is a fence.
    const replacement = { ...node, metadata: { ...node.metadata, taskId: "task-2" } };
    expect(() => applyAgentCanvasPatch({ ...project, nodes: [replacement] }, patch)).toThrow();
});

test("task state covers Agent, manual submissions and historical terminal tasks", () => {
    for (const status of ["queued", "running"]) expect(isCanvasNodeGenerating({ ...node, metadata: { status: "idle", taskId: "task", taskStatus: status } })).toBe(true);
    for (const status of ["succeeded", "failed", "cancelled"]) expect(isCanvasNodeGenerating({ ...node, metadata: { status: "loading", taskId: "old", taskStatus: status } })).toBe(false);
    expect(isCanvasNodeGenerating({ ...node, metadata: { status: "loading" } })).toBe(true);
    expect(isCanvasNodeGenerating({ ...node, metadata: { status: "success", taskId: "historical" } })).toBe(false);
    expect(isCanvasNodeGenerating({ ...node, metadata: { status: "idle" } }, node.id)).toBe(true);
});


test("task progress ahead of the server checkpoint still accepts terminal state", () => {
    const queued = { ...node, metadata: { ...node.metadata, taskStatus: "queued", taskProgress: 0 } };
    const running = { ...node, metadata: { ...node.metadata, taskProgress: 67 } };
    const result = applyAgentCanvasPatch({ ...project, nodes: [running] }, { ...patch, nodes: [{ before: queued, after: failed }] });
    expect(result.nodes[0].metadata?.taskStatus).toBe("failed");
    expect(isCanvasNodeGenerating(result.nodes[0])).toBe(false);
});

test("full Previs refresh merges scene bookkeeping and preserves the local viewport", () => {
    const scene = {
        id: "scene-1",
        version: 1 as const,
        title: "镜头",
        background: "#111111",
        environmentIntensity: 0.7,
        environment: { mode: "color" as const },
        gridVisible: true,
        objects: [],
        cameras: [],
        lights: [],
        shots: [],
        activeShotId: "shot-1",
        createdAt: "2026-09-13T00:00:00Z",
        updatedAt: "2026-09-13T00:00:00Z",
    };
    const previous = { ...project, previsScenes: [scene] };
    const incoming = {
        ...previous,
        revision: 2,
        updatedAt: "2026-09-13T12:00:00Z",
        previsScenes: [{ ...scene, updatedAt: "2026-09-13T12:00:00Z", objects: [{ id: "actor-1", name: "Agent 演员" }] }],
    };
    const local = {
        ...previous,
        viewport: { x: -480, y: -220, k: 0.72 },
        previsScenes: [{ ...scene, updatedAt: "2026-09-13T11:59:00Z" }],
    };
    const merged = mergeAgentCanvasEditor(previous, incoming, local.nodes, local.connections, local);
    expect(merged.previsScenes[0].objects).toEqual(incoming.previsScenes[0].objects);
    expect(merged.previsScenes[0].updatedAt).toBe(incoming.previsScenes[0].updatedAt);
    expect(merged.viewport).toEqual(local.viewport);
});

test("full Previs refresh still rejects a same-field scene edit", () => {
    const scene = {
        id: "scene-1",
        version: 1 as const,
        title: "镜头",
        background: "#111111",
        environmentIntensity: 0.7,
        environment: { mode: "color" as const },
        gridVisible: true,
        objects: [],
        cameras: [],
        lights: [],
        shots: [],
        activeShotId: "shot-1",
        createdAt: "2026-09-13T00:00:00Z",
        updatedAt: "2026-09-13T00:00:00Z",
    };
    const previous = { ...project, previsScenes: [scene] };
    const incoming = { ...previous, revision: 2, previsScenes: [{ ...scene, title: "云端镜头", updatedAt: "2026-09-13T12:00:00Z" }] };
    const local = { ...previous, previsScenes: [{ ...scene, title: "本地镜头", updatedAt: "2026-09-13T11:59:00Z" }] };
    expect(() => mergeAgentCanvasEditor(previous, incoming, local.nodes, local.connections, local)).toThrow("冲突");
});
