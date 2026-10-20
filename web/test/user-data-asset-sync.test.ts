import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import localforage from "localforage";
import { apiClient } from "@/services/api/request";
import {
    acknowledgeRemoteAssets,
    initializeRemoteUserDataSession,
    resetRemoteUserDataSync,
    saveRemoteUserDataNow,
} from "@/services/user-data-sync";
import { flushAssetStorePersistence, useAssetStore, type Asset, type EntityAsset, type ImageAsset } from "@/stores/use-asset-store";

const originalWindow = globalThis.window;
const originalAdapter = apiClient.defaults.adapter;
const originalGet = localforage.getItem;
const originalSet = localforage.setItem;
const indexed = new Map<string, string>();
let sequence = 0;
let scope = "";
let autoSave: (() => void) | undefined;
/** 服务端素材库（id -> 记录），模拟 GET /assets/:id 与 PUT /assets/:id 的真实行为。 */
let remoteAssets = new Map<string, Asset>();
let requests: Array<{ method: string; url: string; body?: unknown }> = [];
let warn: ReturnType<typeof spyOn> | undefined;

const entityAsset = (id: string, overrides: Partial<EntityAsset> = {}): EntityAsset =>
    ({
        id,
        kind: "entity",
        title: `角色-${id}`,
        coverUrl: "",
        tags: [],
        category: "character",
        status: "confirmed",
        primaryVersionId: `version-1-${id}`,
        createdAt: "2026-09-21T00:00:00.000Z",
        updatedAt: "2026-09-21T00:00:00.000Z",
        data: { definition: { role: "主角" } },
        ...overrides,
    }) as EntityAsset;

const imageAsset = (id: string, updatedAt = "2026-09-21T00:00:00.000Z"): ImageAsset =>
    ({
        id,
        kind: "image",
        title: id,
        coverUrl: "",
        tags: [],
        status: "confirmed",
        createdAt: "2026-09-21T00:00:00.000Z",
        updatedAt,
        data: { dataUrl: "/api/resources/img/file", storageKey: "resource:img", width: 10, height: 10, bytes: 10, mimeType: "image/png" },
    }) as ImageAsset;

beforeEach(async () => {
    resetRemoteUserDataSync();
    scope = `asset-sync-test-${++sequence}`;
    indexed.clear();
    remoteAssets = new Map();
    requests = [];
    autoSave = undefined;
    Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
            setTimeout: (callback: () => void) => {
                autoSave = callback;
                return 1;
            },
            clearTimeout: () => {
                autoSave = undefined;
            },
            localStorage: { getItem: () => scope, setItem: () => undefined },
        },
    });
    localforage.getItem = (async (key: string) => indexed.get(key) ?? null) as typeof localforage.getItem;
    localforage.setItem = (async (key: string, value: string) => {
        indexed.set(key, value);
        return value;
    }) as typeof localforage.setItem;
    apiClient.defaults.adapter = async (config) => {
        const method = config.method || "get";
        const url = String(config.url);
        const id = decodeURIComponent(url.split("/").at(-1)!);
        const body = config.data ? JSON.parse(config.data) : undefined;
        requests.push({ method, url, body });
        let data: unknown;
        let status = 200;
        if (method === "get" && id === "snapshot") {
            data = { projects: [], assets: [...remoteAssets.values()] };
        } else if (method === "get" && url.includes("/assets/")) {
            const remote = remoteAssets.get(id);
            if (!remote) {
                status = 404;
                data = { msg: "not found" };
            } else {
                data = { asset: structuredClone(remote) };
            }
        } else if (method === "put" && url.includes("/assets/")) {
            const payload = body?.asset as Asset;
            remoteAssets.set(payload.id, structuredClone(payload));
            data = { asset: { id: payload.id, title: payload.title, createdAt: payload.createdAt, updatedAt: payload.updatedAt } };
        } else {
            data = {};
        }
        return { config, status, statusText: "", headers: {}, data: { code: status === 200 ? 0 : status, data, msg: status === 200 ? "ok" : "fail" } };
    };
    useAssetStore.setState({ assets: [] });
    await flushAssetStorePersistence();
});

afterEach(async () => {
    warn?.mockRestore();
    warn = undefined;
    resetRemoteUserDataSync();
    useAssetStore.setState({ assets: [] });
    await flushAssetStorePersistence();
    apiClient.defaults.adapter = originalAdapter;
    localforage.getItem = originalGet;
    localforage.setItem = originalSet;
    if (originalWindow === undefined) delete (globalThis as { window?: unknown }).window;
    else Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
});

test("角色域写入后 acknowledge 投影，后续保存不再误报远端版本已变化", async () => {
    // 会话基线是旧投影（版本 version-1）；服务端已被角色域接口推进到 version-2。
    const stale = entityAsset("char-1");
    const server = entityAsset("char-1", { primaryVersionId: "version-2-char-1", updatedAt: "2026-09-22T00:00:00.000Z" });
    useAssetStore.setState({ assets: [structuredClone(stale)] });
    await flushAssetStorePersistence();
    remoteAssets.set("char-1", server);
    await initializeRemoteUserDataSession(scope);

    // 模拟 syncPersonalCharacterProjection：写 store 并认领服务端权威投影。
    const projected = entityAsset("char-1", { primaryVersionId: "version-2-char-1", updatedAt: "2026-09-22T00:00:00.000Z" });
    useAssetStore.getState().replaceAssets([projected]);
    acknowledgeRemoteAssets([projected]);

    await saveRemoteUserDataNow();
    // 不应有任何针对该素材的覆盖请求，也不应抛"素材远端版本已变化"。
    expect(requests.filter((request) => request.method === "put" && request.url.includes("/assets/"))).toEqual([]);
});

test.skip("未 acknowledge 的旧基线仍会隔离冲突素材并抛出含素材名的聚合错误", async () => { // TODO(entity-projection): v1.6.8.12 上游以"备份采用云端"统一了素材冲突语义；entity 投影链路（跳过覆盖推送/专用接口）依赖后端 /characters 域整体重实现，留待独立 change（见 openspec/changes/fix-canvas-sync-upstream-rewrite proposal Non-Goals）。
    const stale = entityAsset("char-2");
    useAssetStore.setState({ assets: [stale] });
    await flushAssetStorePersistence();
    remoteAssets.set("char-2", entityAsset("char-2", { primaryVersionId: "version-9-char-2", updatedAt: "2026-09-23T00:00:00.000Z" }));
    await initializeRemoteUserDataSession(scope);
    // 本地内容被编辑 → 判脏，且未认领新基线。
    useAssetStore.getState().updateAsset("char-2", { title: "本地改名" });

    await expect(saveRemoteUserDataNow()).rejects.toThrow("素材远端版本已变化");
    await expect(saveRemoteUserDataNow()).rejects.toThrow("本地改名");
});

test.skip("entity 冲突只隔离该素材，其余素材与画布照常推送", async () => { // TODO(entity-projection): v1.6.8.12 上游以"备份采用云端"统一了素材冲突语义；entity 投影链路（跳过覆盖推送/专用接口）依赖后端 /characters 域整体重实现，留待独立 change（见 openspec/changes/fix-canvas-sync-upstream-rewrite proposal Non-Goals）。
    const stale = entityAsset("char-3");
    const media = imageAsset("img-3");
    useAssetStore.setState({ assets: [structuredClone(stale), media] });
    await flushAssetStorePersistence();
    remoteAssets.set("char-3", entityAsset("char-3", { primaryVersionId: "version-5-char-3", updatedAt: "2026-09-24T00:00:00.000Z" }));
    await initializeRemoteUserDataSession(scope);
    useAssetStore.getState().updateAsset("char-3", { title: "本地改名" });
    useAssetStore.getState().updateAsset("img-3", { title: "图片改名" });

    await expect(saveRemoteUserDataNow()).rejects.toThrow("素材远端版本已变化");

    // 图片素材已成功推送，冲突的角色没有发起覆盖。
    const puts = requests.filter((request) => request.method === "put" && request.url.includes("/assets/"));
    expect(puts).toHaveLength(1);
    expect((puts[0].body as { asset: { id: string } }).asset.id).toBe("img-3");
});

test.skip("entity 素材判脏后跳过通用覆盖推送，仅对齐基线", async () => { // TODO(entity-projection): v1.6.8.12 上游以"备份采用云端"统一了素材冲突语义；entity 投影链路（跳过覆盖推送/专用接口）依赖后端 /characters 域整体重实现，留待独立 change（见 openspec/changes/fix-canvas-sync-upstream-rewrite proposal Non-Goals）。
    warn = spyOn(console, "warn").mockImplementation(() => {});
    const local = entityAsset("char-4", { primaryVersionId: undefined } as Partial<EntityAsset>);
    useAssetStore.setState({ assets: [structuredClone(local)] });
    await flushAssetStorePersistence();
    await initializeRemoteUserDataSession(scope);
    // 服务端没有该素材（新增 entity），本地判脏。
    useAssetStore.getState().updateAsset("char-4", { title: "本地改名" });

    await saveRemoteUserDataNow();

    expect(requests.filter((request) => request.method === "put" && request.url.includes("/assets/"))).toEqual([]);
    expect(warn).toHaveBeenCalledWith("角色素材由角色卡操作维护，跳过通用素材覆盖推送", "char-4");
    // 基线对齐后再次保存不再判脏、不再告警。
    requests = [];
    warn.mockClear();
    await saveRemoteUserDataNow();
    expect(requests.filter((request) => request.method === "put" && request.url.includes("/assets/"))).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
});

test("entity 版本号相同而服务端 updatedAt 变化时不再误判冲突", async () => {
    const local = entityAsset("char-5", { updatedAt: "2026-09-21T00:00:00.000Z" });
    useAssetStore.setState({ assets: [structuredClone(local)] });
    await flushAssetStorePersistence();
    // 服务端 payload 内嵌 updatedAt 被刷新（如列级修复），但版本链未动。
    remoteAssets.set("char-5", entityAsset("char-5", { updatedAt: "2026-09-25T12:00:00.000Z" }));
    await initializeRemoteUserDataSession(scope);
    useAssetStore.getState().updateAsset("char-5", { title: "本地改名" });

    await saveRemoteUserDataNow();

    expect(requests.filter((request) => request.method === "put" && request.url.includes("/assets/"))).toEqual([]);
});
