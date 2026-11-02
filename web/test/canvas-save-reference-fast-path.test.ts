import { describe, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { resourceFileUrl } from "../src/services/api/resources";
import { ensureRemoteResourceReferences } from "../src/services/user-data-sync-media";
import { buildCanvasOperations } from "../src/services/user-data-sync";
import { useCanvasStore, flushCanvasStorePersistence, type CanvasProject } from "../src/stores/canvas/use-canvas-store";
import { useSyncProgressStore } from "../src/stores/use-sync-progress-store";
import { CanvasNodeType } from "../src/types/canvas";

function makeProject(id: string, title = id): CanvasProject {
    return {
        id,
        revision: 1,
        title,
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
        nodes: [
            {
                id: `${id}-node-1`,
                type: CanvasNodeType.Text,
                title: "节点 1",
                position: { x: 0, y: 0 },
                width: 240,
                height: 160,
                metadata: { prompt: "hello", content: "hello" },
            },
        ],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "dots",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        previsScenes: [],
    };
}

describe("canvas save reference fast path", () => {
    test("unchanged non-resource and already-remote resource trees keep object identity", async () => {
        const plain = {
            title: "plain",
            nodes: [
                { id: "a", metadata: { prompt: "hello" } },
                {
                    id: "b",
                    type: "image",
                    metadata: {
                        storageKey: "resource:res-1",
                        content: resourceFileUrl("res-1"),
                    },
                },
            ],
            connections: [],
        };
        const prepared = await ensureRemoteResourceReferences(plain);
        expect(prepared).toBe(plain);
        expect(prepared.nodes).toBe(plain.nodes);
        expect(prepared.nodes[0]).toBe(plain.nodes[0]);
        expect(prepared.nodes[1]).toBe(plain.nodes[1]);
    });

    test("copy-on-write preserves sibling node references when one node rewrites its resource URL", async () => {
        const unchangedNode = { id: "keep", metadata: { prompt: "unchanged" } };
        const needsRewriteNode = {
            id: "rewrite",
            type: "image",
            metadata: {
                storageKey: "resource:res-2",
                content: "blob:https://example.com/stale-url",
            },
        };
        const value = {
            title: "mixed",
            nodes: [unchangedNode, needsRewriteNode],
            connections: [],
        };
        const prepared = await ensureRemoteResourceReferences(value);
        expect(prepared).not.toBe(value);
        expect(prepared.nodes[0]).toBe(unchangedNode);
        expect(prepared.nodes[1]).not.toBe(needsRewriteNode);
        expect(prepared.nodes[1]?.metadata.content).toBe(resourceFileUrl("res-2"));
        expect(prepared.connections).toBe(value.connections);
    });

    test("selectedProjectIds sync does not run sameCanvasContent on unselected projects", async () => {
        const dir = mkdtempSync(join(tmpdir(), "canvas-selected-sync-fast-path-"));
        try {
            const root = new URL("../src/", import.meta.url);
            const stubPath = join(dir, "dependencies.ts");
            const paths: Record<string, string> = {
                "json-canonicalize": import.meta.resolve("json-canonicalize"),
                zustand: import.meta.resolve("zustand"),
                "@/services/api/resources": join(dir, "resources.ts"),
                "@/lib/canvas/sync-recovery": fileURLToPath(new URL("lib/canvas/sync-recovery.ts", root)),
                "@/lib/canvas/canvas-sync-rebase": fileURLToPath(new URL("lib/canvas/canvas-sync-rebase.ts", root)),
                "@/lib/canvas/canvas-text-merge": fileURLToPath(new URL("lib/canvas/canvas-text-merge.ts", root)),
                "@/lib/canvas/canvas-content": join(dir, "content.ts"),
                "@/stores/use-sync-progress-store": fileURLToPath(new URL("stores/use-sync-progress-store.ts", root)),
                "@/lib/canvas/canvas-task-mutation": fileURLToPath(new URL("lib/canvas/canvas-task-mutation.ts", root)),
                "./canvas-mutation-batches": fileURLToPath(new URL("services/canvas-mutation-batches.ts", root)),
                "@/lib/canvas/canvas-batch-input-signature": fileURLToPath(new URL("lib/canvas/canvas-batch-input-signature.ts", root)),
                "./user-data-sync-media": join(dir, "media.ts"),
                "./canvas-edit-leases": stubPath,
                "@/lib/canvas/agent-canvas-patch": fileURLToPath(new URL("lib/canvas/agent-canvas-patch.ts", root)),
            };
            const copySource = (source: string, name: string) => {
                const code = readFileSync(new URL(source, root), "utf8");
                const imports = code.replace(
                    /"(@\/[^\"]+|\.\/user-data-sync-media|\.\/canvas-edit-leases|\.\/canvas-mutation-batches|json-canonicalize|zustand)"/g,
                    (_, specifier: string) => JSON.stringify(paths[specifier] ?? stubPath),
                );
                writeFileSync(join(dir, name), imports);
            };
            writeFileSync(
                stubPath,
                `
import { createStore } from ${JSON.stringify(import.meta.resolve("zustand/vanilla"))};
export const comparedIds = [];
export const remoteById = new Map();
export const useCanvasStore = createStore((set, get) => ({ projects: [], openProject: id => get().projects.find(p => p.id === id) }));
export const useAssetStore = createStore(() => ({ assets: [] }));
export const getActiveUserScope = () => "fast-path-user";
const data = new Map();
export const localForageStorageForScope = () => ({ getItem: async k => data.get(k) ?? null, setItem: async (k, v) => { data.set(k, v); }, removeItem: async k => { data.delete(k); } });
export const flushCanvasStorePersistence = async () => {};
export const flushAssetStorePersistence = async () => {};
export const getGenerationAssetDefaults = () => undefined;
export const appQueryClient = { invalidateQueries: async () => {} };
export const repairMissingCanvasAssets = () => ({ createdAssets: 0, updatedProjects: 0 });
export const rebindInconsistentCanvasAssets = () => ({ createdAssets: 0, updatedProjects: 0, reboundNodes: 0 });
export class ApiError extends Error { retryable = false; constructor(message, options = {}) { super(message); Object.assign(this, options); } }
export const apiBaseURL = "/api";
const unexpected = () => { throw new Error("unexpected"); };
export const http = { get: unexpected, post: unexpected, put: unexpected, patch: unexpected, delete: unexpected };
export async function applyRemoteCanvasOperations(id, request) {
    const current = structuredClone(remoteById.get(id));
    current.revision = (current.revision || 0) + 1;
    remoteById.set(id, current);
    return { project: current };
}
export async function getRemoteCanvasProject(id) { return { project: structuredClone(remoteById.get(id)) }; }
export async function getRemoteCanvasRevision(id) { return { project: structuredClone(remoteById.get(id)) }; }
export const getRemoteAsset = unexpected, upsertRemoteAsset = unexpected, deleteRemoteAssets = unexpected, deleteRemoteCanvasProject = unexpected, getRemoteUserDataSnapshot = unexpected, listRemoteAssetsPage = unexpected;
export const assetForRemoteSync = a => a, getRemoteAssetsByIds = async () => ({ assets: [] }), parseAssetRecordList = r => r;
export const getImageBlob = unexpected, getMediaBlob = unexpected, rebaseAssetSnapshot = unexpected, canvasNodeToAsset = unexpected;
export const canvasEditLeases = { reset() {}, session: () => "s", versions: () => ({}), proofs: async () => [], acknowledged() {}, invalidate() {}, async yield() {} };
export const recordAcceptedAssetDeletions = () => {};
export const useCanvasHistoryStore = {};
`,
            );
            copySource("services/api/resources.ts", "resources.ts");
            copySource("services/user-data-sync-media.ts", "media.ts");
            const contentCode = readFileSync(new URL("lib/canvas/canvas-content.ts", root), "utf8")
                .replace(
                    '"json-canonicalize"',
                    JSON.stringify(paths["json-canonicalize"]),
                )
                .replace(
                    "export function sameCanvasContent(left: CanvasProject | undefined, right: CanvasProject | undefined) {",
                    `import { comparedIds } from ${JSON.stringify(stubPath)};\nexport function sameCanvasContent(left: CanvasProject | undefined, right: CanvasProject | undefined) {\n    if (right?.id) comparedIds.push(right.id);`,
                );
            writeFileSync(join(dir, "content.ts"), contentCode);
            copySource("services/user-data-sync.ts", "sync.ts");

            const { canvasContentHash } = await import(join(dir, "content.ts"));
            const sync: typeof import("@/services/user-data-sync") = await import(join(dir, "sync.ts"));
            const { comparedIds, remoteById, useCanvasStore: stubCanvasStore } = await import(stubPath);

            const projectA = makeProject("canvas-a", "画布 A");
            const projectB = makeProject("canvas-b", "画布 B");
            projectA.remoteContentHash = await canvasContentHash(projectA);
            projectB.remoteContentHash = await canvasContentHash(projectB);
            remoteById.set(projectA.id, structuredClone(projectA));
            remoteById.set(projectB.id, structuredClone(projectB));
            stubCanvasStore.setState({ projects: [projectA, projectB] });
            await sync.initializeRemoteUserDataSession("fast-path-user");

            comparedIds.length = 0;
            stubCanvasStore.setState({
                projects: [
                    { ...projectA, title: "画布 A 已修改" },
                    projectB,
                ],
            });
            await sync.saveRemoteUserDataNow(projectA.id);
            expect(comparedIds.filter((id: string) => id === projectB.id)).toHaveLength(0);
        } finally {
            rmSync(dir, { recursive: true, force: true });
        }
    });

    test("buildCanvasOperations skips node field diff when node reference is identical", () => {
        let propertyReads = 0;
        const rawNode = makeProject("p1").nodes[0]!;
        const trackedMetadata = new Proxy(rawNode.metadata!, {
            get(target, prop, receiver) {
                propertyReads += 1;
                return Reflect.get(target, prop, receiver);
            },
        });
        const sharedNode = { ...rawNode, metadata: trackedMetadata };
        const base: CanvasProject = { ...makeProject("p1"), nodes: [sharedNode] };
        const next: CanvasProject = { ...base, activeChatId: "chat-2" };

        propertyReads = 0;
        const ops = buildCanvasOperations(base, next);
        expect(ops).toEqual([{ type: "set_field", field: "activeChatId", value: "chat-2", beforeValue: null }]);
        expect(propertyReads).toBe(0);
    });

    test("local persistence progress only notifies changed projects and settles all queued projects", async () => {
        await flushCanvasStorePersistence();
        useSyncProgressStore.getState().clearAll();

        const projectA = makeProject("persist-a");
        const projectB = makeProject("persist-b");
        useCanvasStore.setState({ projects: [projectA, projectB] });
        await flushCanvasStorePersistence();
        useSyncProgressStore.getState().clearAll();

        const notificationsByProject = new Map<string, number>();
        const unsubscribe = useSyncProgressStore.subscribe((state, prevState) => {
            for (const id of Object.keys(state.syncingProjects)) {
                if (state.syncingProjects[id] !== prevState.syncingProjects[id]) {
                    notificationsByProject.set(id, (notificationsByProject.get(id) || 0) + 1);
                }
            }
        });

        try {
            // Two consecutive edits to project A while project B is untouched
            useCanvasStore.setState({
                projects: [{ ...projectA, title: "A-edit-1" }, projectB],
            });
            useCanvasStore.setState({
                projects: [{ ...projectA, title: "A-edit-2" }, projectB],
            });

            expect(notificationsByProject.get(projectA.id)).toBe(1);
            expect(notificationsByProject.get(projectB.id) || 0).toBe(0);

            await flushCanvasStorePersistence();
            expect(useSyncProgressStore.getState().syncingProjects[projectA.id]?.localPhase).toBe("saved");
        } finally {
            unsubscribe();
        }
    });
});
