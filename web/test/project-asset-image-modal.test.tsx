import { afterAll, expect, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import type { Project, ProjectAsset } from "../src/services/api/projects";
import type { GenerationTask } from "../src/services/api/task-center";

const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, localStorage: dom.localStorage, ResizeObserver: dom.ResizeObserver, HTMLElement: dom.HTMLElement, Element: dom.Element, Node: dom.Node, SVGElement: dom.SVGElement, ShadowRoot: dom.ShadowRoot, getComputedStyle: dom.getComputedStyle.bind(dom), requestAnimationFrame: dom.requestAnimationFrame.bind(dom), cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}
const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { App, ConfigProvider } = await import("antd");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const { ProjectAssetImageModal } = await import("../src/pages/projects/detail/project-asset-image-modal");
const { apiClient, ApiError } = await import("../src/services/api/request");
const { useUserStore } = await import("../src/stores/use-user-store");
const { useConfigStore, defaultConfig, createModelChannel } = await import("../src/stores/use-config-store");
const { useAssetStore, flushAssetStorePersistence } = await import("../src/stores/use-asset-store");
const { initializeRemoteUserDataSession, resetRemoteUserDataSync } = await import("../src/services/user-data-sync");
const { getActiveUserScope, setActiveUserScope } = await import("../src/lib/user-scope");
const { default: localforage } = await import("localforage");
const adapter = apiClient.defaults.adapter;
afterAll(() => {
    apiClient.defaults.adapter = adapter;
    dom.happyDOM.abort();
    for (const [key, value] of originals) { if (value) Object.defineProperty(globalThis, key, value); else Reflect.deleteProperty(globalThis, key); }
});
const project = { id: "project", name: "测试", stylePresetId: "" } as Project;
const asset: ProjectAsset = { id: "asset", title: "场景", category: "environment", mediaType: "entity", status: "confirmed", primaryVersionId: "v1", versionCount: 1, usages: [], position: 0, updatedAt: "", visual: { status: "missing", definition: { prompt: "阁楼" } } };
async function fixture(existing?: ProjectAsset, onSaved = () => {}, initialMode: "direct" | "generate" = "direct", options: { client?: InstanceType<typeof QueryClient>; onClose?: () => void } = {}) {
    const host = document.createElement("div"); document.body.append(host);
    const root = createRoot(host);
    const client = options.client ?? new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => root.render(<ConfigProvider theme={{ token: { motion: false } }}><App><QueryClientProvider client={client}><ProjectAssetImageModal project={project} asset={existing} initialMode={initialMode} onClose={options.onClose ?? (() => {})} onSaved={onSaved} /></QueryClientProvider></App></ConfigProvider>));
    return { dispose: async () => { await act(async () => root.unmount()); if (!options.client) client.clear(); host.remove(); }, client };
}
test("采用409保留候选、刷新最新版本且重复点击只提交一次", async () => {
    const previous = useUserStore.getState().user;
    useUserStore.setState({ user: { id: "tester" } as NonNullable<typeof previous> });
    let writes = 0;
    let refreshed = 0;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post") {
            writes++;
            expect(JSON.parse(config.data)).toEqual({ expectedVersionId: "v1" });
            await gate;
            throw new ApiError("conflict", { status: 409, reason: "project_asset_version_changed" });
        }
        const data = config.url?.endsWith("/assets") ? { assets: [{ ...asset, primaryVersionId: "v2" }] } : [{ id: "candidate", status: "succeeded", previewUrl: "data:image/png;base64,", assetImageCandidate: { assetId: "asset", baseVersionId: "v1", state: "pending_confirmation" } }];
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data, msg: "" } };
    };
    const view = await fixture(asset, () => refreshed++);
    try {
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); });
        await act(async () => { const adopt = button("采用为当前成品"); adopt.click(); adopt.click(); });
        expect(writes).toBe(1);
        await act(async () => { release(); await new Promise((resolve) => setTimeout(resolve, 50)); });
        expect(document.body.textContent).toContain("候选已保留");
        expect(document.body.textContent).toContain("版本已过期");
        expect(button("采用为当前成品").disabled).toBe(true);
        expect(refreshed).toBe(1);
    } finally { release(); await view.dispose(); useUserStore.setState({ user: previous }); }
});

test("采用服务端失败不显示已保存、不刷新成品，并可重新打开恢复候选", async () => {
    const previous = useUserStore.getState().user;
    useUserStore.setState({ user: { id: "tester" } as NonNullable<typeof previous> });
    let refreshed = 0;
    let reads = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post") throw new ApiError("采用失败，请重试", { status: 500 });
        if (config.params?.assetImageCandidates) reads++;
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: [{ id: "candidate", status: "succeeded", previewUrl: "data:image/png;base64,", assetImageCandidate: { assetId: "asset", baseVersionId: "v1", state: "pending_confirmation" } }], msg: "" } };
    };
    let view = await fixture(asset, () => refreshed++);
    try {
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); });
        await act(async () => { button("采用为当前成品").click(); await new Promise((resolve) => setTimeout(resolve, 50)); });
        expect(document.body.textContent).toContain("采用失败，请重试");
        expect(refreshed).toBe(0);
        expect(button("采用为当前成品").disabled).toBe(false);
        await view.dispose();
        view = await fixture(asset);
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); });
        expect(reads).toBe(2);
        expect(button("采用为当前成品").disabled).toBe(false);
    } finally { await view.dispose(); useUserStore.setState({ user: previous }); }
});

const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.textContent?.replace(/\s/g, "").includes(text))!;
const settle = () => new Promise((resolve) => setTimeout(resolve, 50));
const radio = (value: string) => document.querySelector<HTMLInputElement>(`input[type="radio"][value="${value}"]`)!;
function fill(label: string, value: string) {
    const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[aria-label="${label}"]`)!;
    const prototype = input.tagName === "TEXTAREA" ? dom.HTMLTextAreaElement.prototype : dom.HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new dom.Event("input", { bubbles: true }) as unknown as Event);
}
async function generationFixtureSetup() {
    const previousUser = useUserStore.getState().user;
    const previousConfig = useConfigStore.getState().config;
    const { defaultModelCapabilityConfig } = await import("../src/lib/model-capabilities");
    useUserStore.setState({ user: { id: "tester" } as NonNullable<typeof previousUser> });
    useConfigStore.setState({ config: { ...defaultConfig, imageModel: "test::gpt-image-1", channels: [createModelChannel({ id: "test", scope: "system", apiKey: "system", models: ["gpt-image-1"], modelCosts: [{ model: "gpt-image-1", capability: "image", protocol: "openai-image", capabilityConfig: defaultModelCapabilityConfig("openai-image", "gpt-image-1") }] })] } });
    return () => { useUserStore.setState({ user: previousUser }); useConfigStore.setState({ config: previousConfig }); };
}

test("真实弹窗不支持参考图的模型阻止提交且不静默退化文生图", async () => {
    const restore = await generationFixtureSetup();
    const config = useConfigStore.getState().config;
    useConfigStore.setState({ config: { ...config, channels: config.channels.map((channel) => ({ ...channel, modelCosts: channel.modelCosts?.map((cost) => ({ ...cost, capabilityConfig: { ...cost.capabilityConfig!, image: { ...cost.capabilityConfig!.image!, references: { ...cost.capabilityConfig!.image!.references, maxImages: 0 } } } })) })) } });
    let writes = 0;
    apiClient.defaults.adapter = async (request) => {
        if (request.method !== "get" && (request.url === "/tasks" || request.url?.startsWith("/projects/"))) writes++;
        return { config: request, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: [], msg: "" } };
    };
    const view = await fixture({ ...asset, referenceImage: { resourceId: "original" } }, undefined, "generate");
    try {
        await act(async () => { await settle(); });
        expect(radio("original").checked).toBe(true);
        expect(document.body.textContent).toContain("当前模型未声明支持参考图");
        expect(button("生成图片候选").disabled).toBe(true);
        await act(async () => { button("生成图片候选").click(); await settle(); });
        expect(writes).toBe(0);
        expect(radio("original").checked).toBe(true);
        await act(async () => radio("none").click());
        expect(button("生成图片候选").disabled).toBe(false);
        expect(writes).toBe(0);
    } finally { await view.dispose(); restore(); }
});

test("真实弹窗切换账号隔离候选和活动缓存，旧账号数据不复用", async () => {
    const restore = await generationFixtureSetup();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    const candidate = { id: "tester-candidate", projectId: "project", status: "succeeded", previewUrl: "data:image/png;base64,", assetImageCandidate: { assetId: "asset", baseVersionId: "v1", state: "pending_confirmation" } };
    const active = { id: "tester-active", projectId: "project", status: "running", updatedAt: "" };
    const reads: string[] = [];
    let closeCalls = 0;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    apiClient.defaults.adapter = async (request) => {
        const userId = useUserStore.getState().user?.id;
        const kind = request.params?.activeOnly ? "active" : "candidates";
        reads.push(`${userId}:${kind}`);
        if (userId === "other-user") await gate;
        const data = userId === "tester" ? kind === "active" ? [active] : [candidate] : [];
        return { config: request, status: 200, statusText: "OK", headers: {}, data: { code: 0, data, msg: "" } };
    };
    let view = await fixture(asset, undefined, "generate", { client, onClose: () => { closeCalls++; } });
    const candidateKey = (user: string) => ["project-asset-image-candidates", user, "project", "asset"];
    const activeKey = (user: string) => ["project-asset-image-active", user, "project", "asset"];
    try {
        await act(async () => { await settle(); });
        expect(document.body.textContent).toContain("待采用");
        expect(document.body.textContent).toContain("该资产正在生成");
        expect(client.getQueryData(activeKey("tester"))).toEqual([active]);
        expect(client.getQueryData(candidateKey("tester"))).toMatchObject({ pages: [[candidate]] });
        await act(async () => { useUserStore.setState({ user: { id: "other-user" } as NonNullable<ReturnType<typeof useUserStore.getState>["user"]> }); });
        expect(closeCalls).toBeGreaterThan(0);
        expect(document.body.textContent).not.toContain("待采用");
        expect(document.body.textContent).not.toContain("该资产正在生成");
        expect(button("生成图片候选").disabled).toBe(true);
        await view.dispose();
        view = await fixture(asset, undefined, "generate", { client });
        expect(document.body.textContent).not.toContain("待采用");
        expect(button("生成图片候选").disabled).toBe(true);
        await act(async () => { release(); await settle(); });
        expect(reads).toContain("other-user:active");
        expect(reads).toContain("other-user:candidates");
        expect(client.getQueryData(activeKey("other-user"))).toEqual([]);
        expect(client.getQueryData(candidateKey("other-user"))).toMatchObject({ pages: [[]] });
        expect(client.getQueryData(activeKey("tester"))).toEqual([active]);
        expect(client.getQueryData(candidateKey("tester"))).toMatchObject({ pages: [[candidate]] });
        expect(document.body.textContent).not.toContain("待采用");
        expect(document.body.textContent).not.toContain("该资产正在生成");
        expect(button("生成图片候选").disabled).toBe(false);
    } finally { release(); await view.dispose(); client.clear(); restore(); }
});

for (const mode of ["text", "reference", "none"] as const) {
    test(`真实角色弹窗${mode}创建不携带无成品的imageRole`, async () => {
        const restore = await generationFixtureSetup();
        const previousAssets = useAssetStore.getState().assets;
        const previousScope = getActiveUserScope();
        const previousHydrated = useUserStore.getState().hydrated;
        const storage = new Map<string, unknown>();
        const storageSpies = mode === "reference" ? [
            spyOn(localforage, "getItem").mockImplementation(async (key) => storage.get(key) ?? null),
            spyOn(localforage, "setItem").mockImplementation(async (key, value) => { storage.set(key, value); return value; }),
            spyOn(localforage, "removeItem").mockImplementation(async (key) => { storage.delete(key); }),
        ] : [];
        const sourceImage = { id: "source", kind: "image", title: "测试参考", coverUrl: "", tags: [], status: "confirmed", createdAt: "", updatedAt: "", data: { storageKey: "resource:original", dataUrl: "", width: 1, height: 1, bytes: 1, mimeType: "image/png" } } as (typeof previousAssets)[number];
        useAssetStore.setState({ assets: [sourceImage] });
        const remoteAssets = new Map([[sourceImage.id, sourceImage]]);
        const batchReads: string[][] = [];
        let payload: Record<string, unknown> | undefined;
        apiClient.defaults.adapter = async (config) => {
            if (config.method === "post" && config.url === "/projects/project/characters") { payload = JSON.parse(config.data); throw new ApiError("创建请求已捕获", { status: 400 }); }
            if (config.method === "post" && config.url === "/assets/batch") {
                const { ids } = JSON.parse(config.data) as { ids: string[] };
                batchReads.push(ids);
                const assets = ids.flatMap((id) => remoteAssets.has(id) ? [structuredClone(remoteAssets.get(id)!)] : []);
                return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { assets }, msg: "" } };
            }
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { assets: [], total: 0 }, msg: "" } };
        };
        const view = await fixture(undefined, undefined, "generate");
        try {
            if (mode === "reference") {
                setActiveUserScope("tester");
                await initializeRemoteUserDataSession("tester");
                await act(async () => useUserStore.setState({ hydrated: true }));
            }
            await act(async () => { fill("资产名称", "  主角  "); fill("文字要求", "黑发红衣"); });
            if (mode === "text") await act(async () => radio("text").click());
            else if (mode === "none") await act(async () => radio("none").click());
            else {
                await act(async () => { button("上传或选择一张").click(); await settle(); });
                await act(async () => (document.querySelector(".asset-picker-card-action") as HTMLElement).click());
                await act(async () => button("使用已选素材").click());
                const deadline = Date.now() + 2_000;
                while (!button("取消本次选择") && Date.now() < deadline) await act(async () => { await settle(); });
                expect(button("取消本次选择")).toBeDefined();
                expect(batchReads).toEqual([["source"]]);
                expect(payload).toBeUndefined();
            }
            await act(async () => { button(mode === "text" ? "创建设定" : "创建并生成候选").click(); await settle(); });
            expect(payload).toBeDefined();
            expect(payload).toMatchObject({ name: "主角", definition: { appearance: "黑发红衣" } });
            expect(payload).not.toHaveProperty("imageResourceId");
            expect(payload).not.toHaveProperty("imageRole");
            if (mode === "reference") expect(payload?.referenceImage).toEqual({ resourceId: "original" });
            else expect(payload).not.toHaveProperty("referenceImage");
        } finally {
            await view.dispose();
            if (mode === "reference") resetRemoteUserDataSync();
            useAssetStore.setState({ assets: previousAssets });
            if (mode === "reference") {
                await flushAssetStorePersistence();
                setActiveUserScope(previousScope);
                useUserStore.setState({ hydrated: previousHydrated });
            }
            storageSpies.forEach((spy) => spy.mockRestore());
            restore();
        }
    });
}

for (const category of ["character", "environment"] as const) for (const match of ["metadata-node", "metadata-target-only", "top-level-node-only"] as const) {
    test(`真实${category}弹窗${match}重开恢复活动任务，轮询完成后刷新成功候选`, async () => {
        const restore = await generationFixtureSetup();
        const target = { ...asset, category, ...(category === "character" ? { character: { definition: { appearance: "黑发" }, representations: [] } } : {}) } as ProjectAsset;
        const operation = category === "character" ? "character_turnaround" : "project_asset_image";
        // TaskSummary omits inputJson and projects only metadata.nodeId. SQL also matches
        // metadata.operation + characterAssetId/projectAssetId or a top-level nodeId.
        const active: GenerationTask = {
            id: "active", projectId: "project", type: "canvas_image", status: "running", prompt: "参考图", operation, attempts: 0, createdAt: "", updatedAt: "",
            ...(match === "metadata-node" ? { clientContext: { nodeId: `${category === "character" ? "character-turnaround" : "project-asset-image"}:asset` } } : {}),
        };
        let running = true;
        let candidateReads = 0;
        let activeReads = 0;
        const requests: any[] = [];
        apiClient.defaults.adapter = async (config) => {
            requests.push(config);
            let data: unknown = [];
            if (config.params?.activeOnly) { activeReads++; data = running ? [active] : []; }
            else if (config.params?.assetImageCandidates) { candidateReads++; data = running ? [] : [{ ...active, status: "succeeded", previewUrl: "data:image/png;base64,", assetImageCandidate: { assetId: "asset", baseVersionId: "v1", state: "pending_confirmation" } }]; }
            else if (config.url?.endsWith("/assets")) data = { assets: [target] };
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data, msg: "" } };
        };
        let view = await fixture(target, undefined, "generate");
        const submitText = category === "character" ? "生成三视图候选" : "生成图片候选";
        try {
            await act(async () => { await settle(); });
            expect(button(submitText).disabled).toBe(true);
            expect(document.body.textContent).toContain("该资产正在生成");
            if (match !== "metadata-node") {
                expect(active.clientContext?.nodeId).toBeUndefined();
                expect(active.inputJson).toBeUndefined();
            }
            await act(async () => { button(submitText).click(); button(submitText).click(); });
            expect(requests.some((request) => request.method === "post")).toBe(false);
            expect(document.body.textContent).not.toContain("待采用");
            expect(requests.find((request) => request.params?.activeOnly)?.params).toEqual({ projectId: "project", activeOnly: true, assetId: "asset", pageSize: 1 });
            await view.dispose();
            view = await fixture(target, undefined, "generate");
            await act(async () => { await settle(); });
            expect(button(submitText).disabled).toBe(true);
            const beforeRefresh = activeReads;
            await act(async () => { button("刷新").click(); await settle(); });
            expect(activeReads).toBeGreaterThan(beforeRefresh);
            const beforeCompletion = candidateReads;
            running = false;
            await act(async () => { await new Promise((resolve) => setTimeout(resolve, 2_100)); });
            await act(async () => { await settle(); });
            expect(candidateReads).toBeGreaterThan(beforeCompletion);
            expect(document.body.textContent).toContain("待采用");
            expect(button(submitText).disabled).toBe(false);
            expect(requests.some((request) => request.method === "post")).toBe(false);
        } finally { await view.dispose(); restore(); }
    });
}

for (const source of ["original", "current", "none"] as const) {
 test(`真实弹窗${source}来源活动查询失败拒绝提交，刷新后重复生成只提交一次`, async () => {
    const restore = await generationFixtureSetup();
    const target = { ...asset, referenceImage: { resourceId: "original" }, storageKey: "resource:current" };
    let releaseRead!: () => void;
    const readGate = new Promise<void>((resolve) => { releaseRead = resolve; });
    let releaseWrite!: () => void;
    const writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
    let failActive = true;
    let writes = 0;
    let generationPayload: any;
    apiClient.defaults.adapter = async (config) => {
        if (config.params?.activeOnly) {
            await readGate;
            if (failActive) throw new ApiError("读取失败", { status: 500 });
        }
        if (config.method === "post" && config.url === "/tasks") {
            writes++; generationPayload = JSON.parse(config.data);
            await writeGate;
            throw new ApiError("生成拒绝", { status: 500 });
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: config.url?.endsWith("/assets") ? { assets: [target] } : [], msg: "" } };
    };
    const view = await fixture(target, undefined, "generate");
    try {
        await act(async () => radio(source).click());
        expect(button("生成图片候选").disabled).toBe(true);
        await act(async () => { button("生成图片候选").click(); releaseRead(); await settle(); });
        expect(button("生成图片候选").disabled).toBe(true);
        expect(document.body.textContent).toContain("活动任务读取失败");
        expect(writes).toBe(0);
        failActive = false;
        await act(async () => { button("刷新").click(); await settle(); });
        expect(button("生成图片候选").disabled).toBe(false);
        await act(async () => { const submit = button("生成图片候选"); submit.click(); submit.click(); await settle(); });
        expect(writes).toBe(1);
        expect(generationPayload.input.metadata).toMatchObject({ adoptionMode: "manual", referenceSource: source, nodeId: "project-asset-image:asset" });
        expect(generationPayload.input.referenceImages.map((image: { storageKey: string }) => image.storageKey)).toEqual(source === "none" ? [] : [`resource:${source}`]);
        await act(async () => { releaseWrite(); await settle(); });
        expect(document.body.textContent).toContain("生成拒绝");
        expect(writes).toBe(1);
    } finally { releaseRead(); releaseWrite(); await view.dispose(); restore(); }
 });
}

test("真实弹窗拒绝空名称，复用单图picker而不提交创建", async () => {
    let writes = 0;
    apiClient.defaults.adapter = async (config) => { if (config.method !== "get") writes++; return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: [], msg: "" } }; };
    const view = await fixture();
    try {
        expect(document.body.textContent).toContain("新建资产");
        await act(async () => button("保存成品资产").click());
        expect(document.body.textContent).toContain("请输入资产名称");
        expect(writes).toBe(0);
        await act(async () => button("上传或选择一张").click());
        const input = document.querySelector<HTMLInputElement>('input[type="file"]');
        expect(input).not.toBeNull();
        expect(input?.multiple).toBe(false);
        expect(document.body.textContent).toContain("选择单张图片");
    } finally { await view.dispose(); }
});

test("候选由任务查询恢复，已采用/过期不允许再次覆盖，满页可加载更多", async () => {
    const previous = useUserStore.getState().user;
    useUserStore.setState({ user: { id: "tester" } as NonNullable<typeof previous> });
    const calls: any[] = [];
    apiClient.defaults.adapter = async (config) => {
        calls.push(config.params);
        const tasks = Array.from({ length: 50 }, (_, index) => ({ id: `task-${index}`, status: "succeeded", previewUrl: "data:image/png;base64,", assetImageCandidate: { assetId: "asset", baseVersionId: index === 1 ? "old" : "v1", state: index === 0 ? "adopted" : "pending_confirmation" } }));
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: config.params?.beforeTaskId ? [] : tasks, msg: "" } };
    };
    const view = await fixture(asset);
    try {
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); });
        expect(document.body.textContent).toContain("已采用");
        expect(document.body.textContent).toContain("版本已过期");
        const adopt = [...document.querySelectorAll<HTMLButtonElement>("button")].filter((item) => item.textContent?.includes("采用为当前成品"));
        expect(adopt[0].disabled).toBe(true);
        expect(adopt[1].disabled).toBe(true);
        expect(adopt[2].disabled).toBe(false);
        await act(async () => { button("加载更早的候选").click(); await new Promise((resolve) => setTimeout(resolve, 50)); });
        expect(calls.some((params) => params?.assetImageCandidates && params.beforeTaskId === "task-49")).toBe(true);
    } finally { await view.dispose(); useUserStore.setState({ user: previous }); }
});
