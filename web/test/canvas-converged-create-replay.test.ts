import { afterAll, afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

// Reproduces the poisoned-journal replay: create_node for a node the server
// already committed must converge (drop the op) instead of failing forever with
// 409 canvas_field_conflict ("节点已存在，显式新增不能覆盖已有节点").
const dir = mkdtempSync(join(tmpdir(), "canvas-create-convergence-"));
const root = new URL("../src/", import.meta.url);
const paths: Record<string, string> = {
    "json-canonicalize": import.meta.resolve("json-canonicalize"),
    zustand: import.meta.resolve("zustand"),
    "@/services/api/resources": join(dir, "resources.ts"),
    "@/lib/canvas/canvas-content": join(dir, "content.ts"),
    "@/stores/use-sync-progress-store": join(dir, "progress.ts"),
    "@/lib/canvas/canvas-task-mutation": fileURLToPath(new URL("../src/lib/canvas/canvas-task-mutation.ts", import.meta.url)),
    "@/lib/canvas/agent-canvas-patch": fileURLToPath(new URL("lib/canvas/agent-canvas-patch.ts", root)),
    "./user-data-sync-media": join(dir, "media.ts"),
    "./canvas-edit-leases": join(dir, "stub.ts"),
};
const stubPath = join(dir, "stub.ts");
function copySource(source: string, name: string) {
    const code = readFileSync(new URL(source, root), "utf8");
    const imports = code.replace(/"(@\/[^\"]+|\.\/user-data-sync-media|\.\/canvas-edit-leases|json-canonicalize|zustand)"/g, (_, specifier: string) => JSON.stringify(paths[specifier] ?? stubPath));
    writeFileSync(join(dir, name), `import { fixtureWindow as window } from ${JSON.stringify(stubPath)};\n` + imports);
}
writeFileSync(stubPath, `
import { createStore } from ${JSON.stringify(import.meta.resolve("zustand/vanilla"))};
export const fixture = { remote: undefined, writes: [], reads: 0, receipts: new Map(), data: new Map(), scope: "convergence-test-user", saveHook: undefined, setError: undefined, manualTimers: false, timers: new Map(), nextTimer: 100000 };
export const fixtureWindow = { setTimeout: (callback, delay) => { if (!fixture.manualTimers) return setTimeout(callback, delay); const id = fixture.nextTimer++; fixture.timers.set(id, { callback, delay }); return id; }, clearTimeout: id => { if (!fixture.timers.delete(id)) clearTimeout(id); }, addEventListener: () => {} };
export const useCanvasStore = createStore((set, get) => ({ projects: [], openProject: id => get().projects.find(project => project.id === id) }));
export const useAssetStore = createStore(() => ({ assets: [] }));
export const getActiveUserScope = () => fixture.scope;
export const localForageStorageForScope = () => ({ getItem: async key => fixture.data.get(key) ?? null, setItem: async (key, value) => { await fixture.saveHook?.(key, value); if (fixture.setError) throw fixture.setError; fixture.data.set(key, value); }, removeItem: async key => { fixture.data.delete(key); } });
export async function flushCanvasStorePersistence() {}
export const flushAssetStorePersistence = async () => {};
export const getGenerationAssetDefaults = () => undefined;
export const appQueryClient = { invalidateQueries: async () => {} };
export const repairMissingCanvasAssets = () => ({ createdAssets: 0, updatedProjects: 0 });
export class ApiError extends Error { retryable = false; constructor(message, options = {}) { super(message); Object.assign(this, options); } }
export const apiBaseURL = "/api";
const unexpected = () => { throw new Error("unexpected dependency call"); };
export const http = { get: unexpected, post: unexpected, put: unexpected, patch: unexpected, delete: unexpected };
export async function applyRemoteCanvasOperations(id, request) {
    const receiptKey = id + ":" + request.mutationId;
    const receipt = fixture.receipts.get(receiptKey);
    if (receipt) return structuredClone(receipt);
    fixture.writes.push(structuredClone(request));
    let project = structuredClone(fixture.remote);
    const conflict = () => { throw new ApiError("field conflict", { status: 409, reason: "canvas_field_conflict" }); };
    const equal = (a, b) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
    for (const operation of request.operations) {
        if (operation.type === "create_node") {
            if (!project || project.nodes.some(node => node.id === operation.node.id)) conflict();
            project.nodes.push(structuredClone(operation.node));
        } else if (operation.type === "patch_node") {
            const node = project.nodes.find(node => node.id === operation.id);
            if (!node) conflict();
            for (const [path, before] of Object.entries(operation.before ?? {})) {
                const actual = path.split(".").reduce((value, key) => value?.[key], node);
                if (!equal(actual, before)) conflict();
            }
            for (const [path, value] of Object.entries(operation.patch)) {
                const keys = path.split("."); const last = keys.pop(); let target = node;
                for (const key of keys) target = target[key] ??= {};
                target[last] = value;
            }
        } else if (operation.type === "set_field") {
            if (!equal(project[operation.field], operation.beforeValue)) conflict();
            project[operation.field] = operation.value;
        } else throw new Error("unexpected operation");
    }
    project.revision++;
    fixture.remote = structuredClone(project);
    fixture.receipts.set(receiptKey, { project: structuredClone(project) });
    return { project };
}
export async function getRemoteCanvasProject(id) { fixture.reads++; return { project: structuredClone(fixture.remote) }; }
export async function getRemoteCanvasRevision(id, revision) { const project = fixture.remote; if (!project || project.revision !== revision) throw new ApiError("revision missing", { status: 404 }); return { project: structuredClone(project) }; }
export const getRemoteAsset = unexpected, upsertRemoteAsset = unexpected;
export const assetForRemoteSync = asset => asset;
export const deleteRemoteAssets = unexpected, deleteRemoteCanvasProject = unexpected, getRemoteAssetsByIds = unexpected, getRemoteUserDataSnapshot = unexpected, listRemoteAssetsPage = unexpected;
export const getImageBlob = unexpected, getMediaBlob = unexpected, parseAssetRecordList = unexpected, rebaseAssetSnapshot = unexpected, canvasNodeToAsset = unexpected;
export const canvasEditLeases = { reset() {}, session: () => "test-session", versions: () => ({}), proofs: async () => [], acknowledged() {} };
export const useCanvasHistoryStore = {};
`);
writeFileSync(join(dir, "resources.ts"), "export const resourceFileUrl = () => \"\"; export const resourceIdFromStorageKey = () => undefined;");
writeFileSync(join(dir, "media.ts"), "export const numberValue = value => Number(value); export const collectLocalMediaKeys = () => []; export const ensureRemoteResourceReferences = async (project, _uploaded, _onUploaded, preserveTextContent) => project; export const restoreRemoteTextNodeContents = async project => project;");
writeFileSync(join(dir, "content.ts"), readFileSync(new URL("lib/canvas/canvas-content.ts", root), "utf8").replace(/"json-canonicalize"/g, JSON.stringify(paths["json-canonicalize"])));
writeFileSync(join(dir, "progress.ts"), readFileSync(new URL("stores/use-sync-progress-store.ts", root), "utf8").replace(/"zustand"/g, JSON.stringify(paths["zustand"])));
copySource("services/user-data-sync.ts", "sync.ts");
const sync = await import(join(dir, "sync.ts"));
const { useCanvasStore, fixture } = await import(stubPath);
const { useSyncProgressStore } = await import(join(dir, "progress.ts"));

function project(nodes: CanvasProject["nodes"], revision: number): CanvasProject {
    return { id: "canvas", title: "Canvas", createdAt: "2026-01-01", updatedAt: "2026-01-01", revision, nodes, connections: [], chatSessions: [], activeChatId: null, backgroundMode: "dots", showImageInfo: false, viewport: { x: 0, y: 0, k: 1 }, previsScenes: [] } as unknown as CanvasProject;
}
const textNode = (id: string) => ({ id, type: "text", position: { x: 0, y: 0 }, width: 200, height: 100, metadata: { content: "text" } }) as CanvasProject["nodes"][number];
const journalKey = "yingce:canvas-mutation-journal";
const journal = () => JSON.parse(fixture.data.get(journalKey) ?? "[]");

beforeEach(async () => {
    sync.resetRemoteUserDataSync();
    fixture.data.clear();
    fixture.writes = [];
    fixture.reads = 0;
    fixture.receipts.clear();
    fixture.saveHook = undefined;
    fixture.setError = undefined;
    // 阻断 initializeRemoteUserDataSession 排队的自动保存，由用例手动触发。
    fixture.manualTimers = true;
    fixture.timers.clear();
    fixture.remote = project([textNode("text-existing")], 8);
    const baseline = project([], 7);
    const prepared = project([textNode("text-existing")], 7);
    // A lost create response: the server already committed the node (rev 8),
    // but the journal still replays create_node against a rev-7 baseline.
    fixture.data.set(journalKey, JSON.stringify([{
        projectId: "canvas",
        baseline,
        prepared,
        createdAt: "2026-01-01",
        request: { mutationId: "lost-create", baseRevision: 7, operations: [{ type: "create_node", node: textNode("text-existing") }] },
    }]));
    useCanvasStore.setState({ projects: [prepared] });
    await sync.initializeRemoteUserDataSession(fixture.scope);
});

afterEach(async () => {
    await sync.resetRemoteUserDataSync();
});
afterAll(async () => {
    rmSync(dir, { recursive: true, force: true });
});

describe("已收敛 create_node journal 重放", () => {
    test("云端已存在的 create_node 被丢弃，journal 清空，保存成功结束", async () => {
        await sync.saveRemoteUserDataNow("canvas");
        // 原请求 409（必然），修复后不再无限失败：journal 被清空。
        expect(journal()).toEqual([]);
        // 没有新的写请求被发出（第一批已存在，全部丢弃）。
        expect(fixture.writes).toHaveLength(1);
        expect(fixture.writes[0].mutationId).toBe("lost-create");
        // 进度收尾为完成，不再卡在冲突。
        expect(useSyncProgressStore.getState().syncingProjects.canvas.phase).toBe("done");
        expect(useSyncProgressStore.getState().syncingProjects.canvas.pendingChanges).toBe(0);
    });

    test("部分收敛批次只丢弃已存在的 create_node，保留其余操作并换新 ID 提交", async () => {
        const baseline = project([], 7);
        const prepared = project([textNode("text-existing"), textNode("text-new")], 7);
        fixture.data.set(journalKey, JSON.stringify([{
            projectId: "canvas",
            baseline,
            prepared,
            createdAt: "2026-01-01",
            request: {
                mutationId: "mixed-batch",
                baseRevision: 7,
                operations: [
                    { type: "create_node", node: textNode("text-existing") },
                    { type: "create_node", node: textNode("text-new") },
                ],
            },
        }]));
        useCanvasStore.setState({ projects: [prepared] });
        await sync.initializeRemoteUserDataSession(fixture.scope);
        await sync.saveRemoteUserDataNow("canvas");
        expect(fixture.writes).toHaveLength(2);
        expect(fixture.writes[1].mutationId).not.toBe("mixed-batch");
        expect(fixture.writes[1].operations).toEqual([{ type: "create_node", node: textNode("text-new") }]);
        expect(fixture.remote.nodes.map((node: { id: string }) => node.id)).toEqual(["text-existing", "text-new"]);
        expect(journal()).toEqual([]);
    });

    test("纯 patch_node 409 不触发 create_node 收敛，冲突如实上报且 journal 保留", async () => {
        // 云端节点已被他端修改 prompt，本地补丁基线过期 → 409。
        // 该请求不含 create_node，收敛分支必须原样放行，不能吞掉真实冲突。
        const driftedRemote = project([{ ...textNode("text-existing"), metadata: { content: "drifted" } }], 8);
        fixture.remote = driftedRemote;
        const baseline = project([textNode("text-existing")], 7);
        const prepared = project([{ ...textNode("text-existing"), metadata: { content: "edited" } }], 7);
        fixture.data.set(journalKey, JSON.stringify([{
            projectId: "canvas",
            baseline,
            prepared,
            createdAt: "2026-01-01",
            request: { mutationId: "patch-only", baseRevision: 7, operations: [{ type: "patch_node", id: "text-existing", patch: { "metadata.content": "edited" }, before: { "metadata.content": "text" } }] },
        }]));
        useCanvasStore.setState({ projects: [prepared] });
        await sync.initializeRemoteUserDataSession(fixture.scope).then(
            () => undefined,
            () => undefined,
        );
        const failure = await sync.saveRemoteUserDataNow("canvas").then(
            () => undefined,
            (error: unknown) => error,
        );
        expect(String((failure as Error).message)).toContain("field conflict");
        expect(fixture.writes).toHaveLength(1);
        expect(journal()[0].request.mutationId).toBe("patch-only");
        expect(useSyncProgressStore.getState().syncingProjects.canvas.phase).toBe("error");
    });
});
