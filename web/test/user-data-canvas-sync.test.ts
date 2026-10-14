import { afterEach, beforeEach, expect, test } from "bun:test";
import localforage from "localforage";
import { apiClient, ApiError } from "@/services/api/request";
import type { CanvasOperationsRequest } from "@/services/api/user-data";
import { applyAgentCanvasPatches, initializeRemoteUserDataSession, loadCanvasProjectForEditing, refreshCanvasAfterAgent, resetRemoteUserDataSync, saveRemoteUserDataNow, subscribeAgentCanvasRefresh, syncRemoteUserData } from "@/services/user-data-sync";
import { canvasContentHash } from "@/lib/canvas/canvas-content";
import { flushCanvasStorePersistence, useCanvasStore, type CanvasProject } from "@/stores/canvas/use-canvas-store";
import { flushAssetStorePersistence, useAssetStore } from "@/stores/use-asset-store";
import { CanvasNodeType } from "@/types/canvas";

const originalWindow = globalThis.window;
const originalAdapter = apiClient.defaults.adapter;
const originalGet = localforage.getItem;
const originalSet = localforage.setItem;
const originalRemove = localforage.removeItem;
const indexed = new Map<string, string>();
let sequence = 0;

function canvas(): CanvasProject {
    return {
        id: "canvas",
        title: "initial",
        revision: 1,
        createdAt: "",
        updatedAt: "",
        nodes: [{ id: "source", type: CanvasNodeType.Text, title: "source", position: { x: 0, y: 0 }, width: 200, height: 100, metadata: {} }],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        viewport: { x: 0, y: 0, k: 1 },
    };
}

beforeEach(async () => {
    resetRemoteUserDataSync();
    indexed.clear();
    const scope = `canvas-sync-test-${++sequence}`;
    Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
            setTimeout: () => 1,
            clearTimeout: () => {},
            localStorage: { getItem: () => scope, setItem: () => {} },
        },
    });
    localforage.getItem = (async (key: string) => indexed.get(key) ?? null) as typeof localforage.getItem;
    localforage.setItem = (async (key: string, value: string) => {
        indexed.set(key, value);
        return value;
    }) as typeof localforage.setItem;
    localforage.removeItem = async (key: string) => {
        indexed.delete(key);
    };
    useAssetStore.setState({ assets: [] });
    const initial = canvas();
    initial.remoteContentHash = await canvasContentHash(initial);
    useCanvasStore.setState({ projects: [initial] });
    await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
    await initializeRemoteUserDataSession(scope);
});

afterEach(async () => {
    resetRemoteUserDataSync();
    useCanvasStore.setState({ projects: [] });
    await flushCanvasStorePersistence();
    apiClient.defaults.adapter = originalAdapter;
    localforage.getItem = originalGet;
    localforage.setItem = originalSet;
    localforage.removeItem = originalRemove;
    if (originalWindow === undefined) delete (globalThis as { window?: unknown }).window;
    else Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
});

test("saving concurrent local edits preserves Agent nodes and connections and updates the live editor", async () => {
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    const draft = { ...remote.nodes[0], id: "agent-draft", title: "Agent draft" };
    const edge = { id: "reference", fromNodeId: "source", toNodeId: "agent-draft" };
    const observed: CanvasProject[] = [];
    const unsubscribe = subscribeAgentCanvasRefresh((project) => {
        observed.push(project);
    });
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            const request = JSON.parse(config.data) as CanvasOperationsRequest;
            requests.push(request);
            if (requests.length > 2) throw new Error("unexpected save loop");
            for (const op of request.operations) {
                if (op.type === "set_field" && op.field === "title") remote = { ...remote, title: op.value as string };
                if (op.type === "remove_node") remote = { ...remote, nodes: remote.nodes.filter((node) => node.id !== op.id) };
                if (op.type === "remove_connection") remote = { ...remote, connections: remote.connections.filter((connection) => connection.id !== op.id) };
            }
            remote = { ...remote, revision: remote.revision! + 1 };
        } else if (config.method === "get" && requests.length === 1) {
            // The server adds a draft after accepting this save, while the user
            // continues editing before the authoritative snapshot arrives.
            remote = { ...remote, nodes: [...remote.nodes, draft], connections: [edge], revision: remote.revision! + 1 };
            useCanvasStore.getState().updateProject("canvas", { title: "typed while saving" });
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    try {
        useCanvasStore.getState().updateProject("canvas", { title: "first edit" });
        await saveRemoteUserDataNow("canvas");
        const saved = useCanvasStore.getState().openProject("canvas")!;
        expect(saved.nodes.map((node) => node.id)).toEqual(["source", "agent-draft"]);
        expect(saved.connections).toEqual([edge]);
        expect(saved.title).toBe("typed while saving");
        expect(requests).toHaveLength(2);
        expect(requests[1].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "first edit", value: "typed while saving" }]);
        expect(observed[0]?.nodes.map((node) => node.id)).toEqual(["source", "agent-draft"]);
    } finally {
        unsubscribe();
    }
});

test("a failed authoritative read retains the original mutation for retry", async () => {
    const requests: CanvasOperationsRequest[] = [];
    let remote = canvas();
    let failRead = true;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            requests.push(JSON.parse(config.data) as CanvasOperationsRequest);
            remote = { ...remote, title: "saved edit", revision: 2 };
        } else if (config.method === "get" && failRead) {
            failRead = false;
            throw new Error("snapshot unavailable");
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    useCanvasStore.getState().updateProject("canvas", { title: "saved edit" });
    await expect(saveRemoteUserDataNow("canvas")).rejects.toThrow("snapshot unavailable");
    expect(requests).toHaveLength(1);
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(2);
    expect(requests[1]).toEqual(requests[0]);
    expect(useCanvasStore.getState().openProject("canvas")?.revision).toBe(2);
});

test("rejected orphan-only journal is reconciled without restoring a deleted node or losing newer edits", async () => {
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    const edges = [1, 2, 3].map((id) => ({ id: `reference-${id}`, fromNodeId: "source", toNodeId: "deleted-board" }));
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            const request = JSON.parse(config.data) as CanvasOperationsRequest;
            requests.push(request);
            if (requests.length === 1) throw new ApiError("offline", { retryable: true });
            if (request.operations.some((op) => op.type === "create_connection")) throw new ApiError("missing endpoint", { status: 400 });
            for (const op of request.operations) {
                if (op.type === "set_field" && op.field === "title") remote = { ...remote, title: op.value as string, revision: 2 };
            }
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    useCanvasStore.getState().updateProject("canvas", { connections: edges });
    await expect(saveRemoteUserDataNow("canvas")).rejects.toThrow("offline");
    // Simulate reopening the browser: recover the durable journal, not just memory.
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    expect((await loadCanvasProjectForEditing("canvas"))?.connections).toEqual(edges);
    useCanvasStore.getState().updateProject("canvas", { title: "keep my edit" });
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(3);
    expect(requests[1]).toEqual(requests[0]);
    expect(requests[2].mutationId).not.toBe(requests[0].mutationId);
    expect(requests[2].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "initial", value: "keep my edit" }]);
    expect(useCanvasStore.getState().openProject("canvas")?.connections).toEqual([]);
    expect(
        useCanvasStore
            .getState()
            .openProject("canvas")
            ?.nodes.map((node) => node.id),
    ).toEqual(["source"]);
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(3);
});

test.each(["remote endpoint exists", "remote connection exists", "local endpoint restored", "mixed operations", "server unavailable"])("orphan recovery keeps the journal when %s", async (scenario) => {
    const remote = canvas();
    if (scenario === "remote endpoint exists") remote.nodes.push({ ...remote.nodes[0], id: "deleted-board" });
    if (scenario === "remote connection exists") remote.connections.push({ id: "reference", fromNodeId: "source", toNodeId: "source" });
    const requests: CanvasOperationsRequest[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            requests.push(JSON.parse(config.data) as CanvasOperationsRequest);
            if (scenario === "local endpoint restored") useCanvasStore.getState().updateProject("canvas", { nodes: [...canvas().nodes, { ...canvas().nodes[0], id: "deleted-board" }] });
            throw new ApiError("rejected", { status: scenario === "server unavailable" ? 503 : 400 });
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    const edges = [{ id: "reference", fromNodeId: "source", toNodeId: "deleted-board" }];
    useCanvasStore.getState().updateProject("canvas", { connections: edges, ...(scenario === "mixed operations" ? { title: "keep edit" } : {}) });
    await expect(saveRemoteUserDataNow("canvas")).rejects.toThrow("rejected");
    await expect(saveRemoteUserDataNow("canvas")).rejects.toThrow("rejected");
    expect(requests).toHaveLength(2);
    expect(requests[1]).toEqual(requests[0]);
    expect(useCanvasStore.getState().openProject("canvas")?.connections).toEqual(edges);
});

test.each([false, true])("opening dirty canvas merges remote additions and preserves intentional deletion: %s", async (deleteSource) => {
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            const request = JSON.parse(config.data) as CanvasOperationsRequest;
            requests.push(request);
            for (const op of request.operations) {
                if (op.type === "set_field" && op.field === "title") remote = { ...remote, title: op.value as string };
                if (op.type === "remove_node") remote = { ...remote, nodes: remote.nodes.filter((node) => node.id !== op.id) };
            }
            remote = { ...remote, revision: remote.revision! + 1 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local edit", ...(deleteSource ? { nodes: [] } : {}) });
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    let rendered: CanvasProject | undefined;
    const loaded = await loadCanvasProjectForEditing("canvas", {
        onLoad: (project) => {
            rendered = project;
        },
    });
    expect(loaded?.nodes.map((node) => node.id)).toEqual(deleteSource ? ["agent-draft"] : ["source", "agent-draft"]);
    expect(rendered).toBe(loaded);
    expect(loaded?.title).toBe("local edit");
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(1);
    expect(requests[0].operations.filter((op) => op.type === "remove_node").map((op) => op.id)).toEqual(deleteSource ? ["source"] : []);
    expect(remote.nodes.some((node) => node.id === "agent-draft")).toBe(true);
});

test("opening a same-field conflict preserves the old baseline and local edit", async () => {
    let remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local title" });
    remote = { ...remote, revision: 2, title: "remote title" };
    await expect(loadCanvasProjectForEditing("canvas")).rejects.toThrow("冲突");
    expect(useCanvasStore.getState().openProject("canvas")?.title).toBe("local title");
    expect(useCanvasStore.getState().openProject("canvas")?.revision).toBe(1);
});

test("a dirty cache without its old baseline cannot be rebased or silently saved", async () => {
    const local = { ...useCanvasStore.getState().openProject("canvas")!, title: "offline edit" };
    useCanvasStore.setState({ projects: [local] });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    const remote = { ...canvas(), revision: 2, title: "other device edit" };
    let writes = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") writes++;
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    await expect(loadCanvasProjectForEditing("canvas")).rejects.toThrow("基线");
    await expect(saveRemoteUserDataNow("canvas")).rejects.toThrow("基线");
    expect(useCanvasStore.getState().openProject("canvas")?.title).toBe("offline edit");
    expect(writes).toBe(0);
});

test("an unchanged cloud revision and hash recover the baseline of an offline edit", async () => {
    useCanvasStore.getState().updateProject("canvas", { title: "offline edit" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            const request = JSON.parse(config.data) as CanvasOperationsRequest;
            requests.push(request);
            remote = { ...remote, title: "offline edit", revision: 2 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    expect((await loadCanvasProjectForEditing("canvas"))?.title).toBe("offline edit");
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(1);
    expect(requests[0].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "initial", value: "offline edit" }]);
});

test("Agent refresh retains a confirmed baseline across a session restart with unsaved edits", async () => {
    let remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "unsaved local title" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    const refreshed = await refreshCanvasAfterAgent("canvas");
    expect(refreshed.title).toBe("unsaved local title");
    expect(refreshed.nodes.map((node) => node.id)).toEqual(["source", "agent-draft"]);
    expect(refreshed.revision).toBe(2);
});

test("Agent refresh can recover a legacy dirty cache when the cloud still matches its recorded hash", async () => {
    useCanvasStore.getState().updateProject("canvas", { title: "offline edit" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    const remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    expect((await refreshCanvasAfterAgent("canvas")).title).toBe("offline edit");
});

test.each(["open", "delta", "save"])("confirmed baseline survives restart for %s", async (path) => {
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            const request = JSON.parse(config.data) as CanvasOperationsRequest;
            requests.push(request);
            remote = { ...remote, title: "offline edit", revision: remote.revision! + 1 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } };
    };
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "offline edit" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    const draft = { ...remote.nodes[0], id: "agent-draft" };
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, draft] };
    if (path === "open") await loadCanvasProjectForEditing("canvas");
    if (path === "delta") await applyAgentCanvasPatches("canvas", [{ canvasId: "canvas", baseRevision: 1, revision: 2, updatedAt: "", nodes: [{ before: null, after: draft }], connections: [] }]);
    await saveRemoteUserDataNow("canvas");
    expect(requests).toHaveLength(1);
    expect(requests[0].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "initial", value: "offline edit" }]);
    expect(
        useCanvasStore
            .getState()
            .openProject("canvas")
            ?.nodes.map((node) => node.id),
    ).toEqual(["source", "agent-draft"]);
});

test("a receipt write failure does not advance the live document or lose its original baseline", async () => {
    let remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local edit" });
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    const setItem = localforage.setItem;
    localforage.setItem = (async (key: string, value: string) => {
        if (key.includes("canvas-acknowledged")) throw new Error("disk full");
        return setItem(key, value);
    }) as typeof localforage.setItem;
    await expect(refreshCanvasAfterAgent("canvas")).rejects.toThrow("disk full");
    expect(useCanvasStore.getState().openProject("canvas")?.revision).toBe(1);
    localforage.setItem = setItem;
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    expect((await refreshCanvasAfterAgent("canvas")).title).toBe("local edit");
});

test("a scoped save still runs when an in-flight save of another canvas fails", async () => {
    const other = { ...canvas(), id: "other", title: "unsaved", remoteContentHash: "unknown-baseline" };
    useCanvasStore.setState((state) => ({ projects: [...state.projects, other] }));
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    let remote = canvas();
    const writes: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            writes.push(config.url!);
            remote = { ...remote, title: "local edit", revision: 2 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(config.url?.endsWith("other") ? { ...other, title: "server edit" } : remote) } } };
    };
    useCanvasStore.getState().updateProject("canvas", { title: "local edit" });
    const unrelated = saveRemoteUserDataNow("other");
    const current = saveRemoteUserDataNow("canvas");
    await expect(unrelated).rejects.toThrow("基线");
    await current;
    expect(writes).toEqual(["/canvas-projects/canvas/operations"]);
    expect(useCanvasStore.getState().openProject("other")?.title).toBe("unsaved");
});

test.each(["another account", "corrupt receipt"])("baseline recovery rejects %s", async (scenario) => {
    let remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local edit" });
    if (scenario === "another account") window.localStorage.getItem = () => "another-account";
    else {
        const key = [...indexed.keys()].find((key) => key.includes("canvas-acknowledged"))!;
        const saved = JSON.parse(indexed.get(key)!);
        for (const item of saved) item.title = "corrupted baseline";
        indexed.set(key, JSON.stringify(saved));
    }
    await initializeRemoteUserDataSession(scenario === "another account" ? "another-account" : `canvas-sync-test-${sequence}`);
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    await expect(refreshCanvasAfterAgent("canvas")).rejects.toThrow("基线");
    expect(useCanvasStore.getState().openProject("canvas")?.title).toBe("local edit");
});

test("an interrupted publication retains the old receipt after a session restart", async () => {
    let remote = canvas();
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(remote) } } });
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local edit" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    const setItem = localforage.setItem;
    localforage.setItem = (async (key: string, value: string) => {
        const result = await setItem(key, value);
        if (key.includes("canvas-acknowledged")) useCanvasStore.getState().updateProject("canvas", { viewport: { x: 1, y: 0, k: 1 } });
        return result;
    }) as typeof localforage.setItem;
    await expect(refreshCanvasAfterAgent("canvas")).rejects.toThrow("仍在更新");
    localforage.setItem = setItem;
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    const recovered = await refreshCanvasAfterAgent("canvas");
    expect(recovered.title).toBe("local edit");
    expect(recovered.revision).toBe(2);
});

test("full snapshot recovery uses the original confirmed baseline for offline edits", async () => {
    let remote = canvas();
    const requests: CanvasOperationsRequest[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") {
            requests.push(JSON.parse(config.data) as CanvasOperationsRequest);
            remote = { ...remote, title: "local edit", revision: 3 };
        }
        const data = config.url === "/user-data/snapshot" ? { projects: [structuredClone(remote)], assets: [] } : { project: structuredClone(remote) };
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data } };
    };
    await loadCanvasProjectForEditing("canvas");
    useCanvasStore.getState().updateProject("canvas", { title: "local edit" });
    remote = { ...remote, revision: 2, nodes: [...remote.nodes, { ...remote.nodes[0], id: "agent-draft" }] };
    await syncRemoteUserData(`canvas-sync-test-${sequence}`);
    await saveRemoteUserDataNow("canvas");
    expect(requests[0].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "initial", value: "local edit" }]);
    expect(useCanvasStore.getState().openProject("canvas")?.nodes).toHaveLength(2);
});

test.each(["open", "refresh", "save"])("legacy cache recovers an exact server receipt before %s without deleting remote additions", async (path) => {
    const original = canvas();
    useCanvasStore.getState().updateProject("canvas", { title: "offline title" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    let remote = { ...original, revision: 2, nodes: [...original.nodes, { ...original.nodes[0], id: "agent-draft" }] };
    const requests: CanvasOperationsRequest[] = [];
    const reads: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        reads.push(config.url!);
        if (config.method === "patch") {
            requests.push(JSON.parse(config.data) as CanvasOperationsRequest);
            remote = { ...remote, title: "offline title", revision: 3 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(config.url?.endsWith("/revisions/1") ? original : remote) } } };
    };
    if (path === "open") await loadCanvasProjectForEditing("canvas");
    if (path === "refresh") await refreshCanvasAfterAgent("canvas");
    await saveRemoteUserDataNow("canvas");
    expect(reads).toContain("/canvas-projects/canvas/revisions/1");
    expect(requests).toHaveLength(1);
    expect(requests[0].operations).toEqual([{ type: "set_field", field: "title", beforeValue: "initial", value: "offline title" }]);
    expect(useCanvasStore.getState().openProject("canvas")?.nodes).toHaveLength(2);
});

test.each(["wrong hash", "wrong revision", "wrong canvas", "missing", "forbidden", "offline", "field conflict"])("historical recovery preserves local edits for %s", async (scenario) => {
    const original = canvas();
    useCanvasStore.getState().updateProject("canvas", { title: "offline title" });
    await initializeRemoteUserDataSession(`canvas-sync-test-${sequence}`);
    const remote = { ...original, revision: 2, title: "other device title" };
    const receipt = { ...original, ...(scenario === "wrong hash" ? { title: "incorrect" } : scenario === "wrong revision" ? { revision: 9 } : scenario === "wrong canvas" ? { id: "other" } : {}) };
    let writes = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "patch") writes++;
        const historical = config.url?.endsWith("/revisions/1");
        if (historical && ["missing", "forbidden", "offline"].includes(scenario)) throw new ApiError(scenario, { status: scenario === "missing" ? 404 : scenario === "forbidden" ? 403 : 503 });
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, msg: "ok", data: { project: structuredClone(historical ? receipt : remote) } } };
    };
    await expect(loadCanvasProjectForEditing("canvas")).rejects.toThrow(scenario === "field conflict" ? "冲突" : scenario === "forbidden" || scenario === "offline" ? scenario : "基线");
    expect(writes).toBe(0);
    expect(useCanvasStore.getState().openProject("canvas")?.title).toBe("offline title");
    expect(useCanvasStore.getState().openProject("canvas")?.revision).toBe(1);
});
