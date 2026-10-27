import { afterAll, beforeEach, expect, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import type { Project, ProjectAsset, ProfileAnalysisView } from "../src/services/api/projects";
const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, localStorage: dom.localStorage, ResizeObserver: dom.ResizeObserver, HTMLElement: dom.HTMLElement, Element: dom.Element, Node: dom.Node, SVGElement: dom.SVGElement, ShadowRoot: dom.ShadowRoot, getComputedStyle: dom.getComputedStyle.bind(dom), requestAnimationFrame: dom.requestAnimationFrame.bind(dom), cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key)); Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}
const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { App, ConfigProvider } = await import("antd");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const modulePath = "../src/pages/projects/detail/project-profile-analysis-modal";
const components = await import(modulePath).catch(() => ({}));
const { ProjectCharacterCard } = await import("../src/pages/projects/detail/project-character-card");
const { ProjectVisualAssetCard } = await import("../src/pages/projects/detail/project-visual-asset-card");
const { apiClient, ApiError } = await import("../src/services/api/request");
const { useUserStore } = await import("../src/stores/use-user-store");
const { useConfigStore, defaultConfig, createModelChannel } = await import("../src/stores/use-config-store");
const { defaultModelCapabilityConfig } = await import("../src/lib/model-capabilities");
const { default: localforage } = await import("localforage");
const sync = await import("../src/services/user-data-sync");
const persisted = new Map<string, unknown>();
const storageSpies = [spyOn(localforage, "getItem").mockImplementation(async (key) => persisted.get(key) ?? null), spyOn(localforage, "setItem").mockImplementation(async (key, value) => { persisted.set(key, value); return value; }), spyOn(localforage, "removeItem").mockImplementation(async (key) => { persisted.delete(key); })];
const projectionSpy = spyOn(sync, "loadAssetsForUse").mockResolvedValue(undefined);
beforeEach(() => { persisted.clear(); });
afterAll(() => { storageSpies.forEach((spy) => spy.mockRestore()); projectionSpy.mockRestore(); });
const adapter = apiClient.defaults.adapter;
afterAll(() => { apiClient.defaults.adapter = adapter; dom.happyDOM.abort(); for (const [key, value] of originals) { if (value) Object.defineProperty(globalThis, key, value); else Reflect.deleteProperty(globalThis, key); } });
const project = { id: "project", name: "测试", defaultImageModel: "image-only" } as Project;
const asset: ProjectAsset = { id: "asset", title: "场景", category: "environment", mediaType: "entity", status: "confirmed", primaryVersionId: "v1", referenceImage: { resourceId: "original" }, versionCount: 1, usages: [], position: 0, updatedAt: "", visual: { status: "missing", definition: { description: ["旧", "数组"], prompt: "旧提示" } } };
const analysis: ProfileAnalysisView = { taskId: "task", projectId: "project", assetId: "asset", category: "environment", baseVersionId: "v1", source: { resourceId: "original" }, status: "succeeded", stage: "done", createdAt: "", updatedAt: "", canCancel: false, canAdopt: true, currentVersionId: "v1", blockedReason: "", baseline: { versionId: "v1", fields: { description: ["旧", "数组"], prompt: "旧提示" } }, draft: { description: "新描述", prompt: "" } };
const settle = () => new Promise((resolve) => setTimeout(resolve, 60));
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.textContent?.replace(/\s/g, "").includes(text))!;
async function fixture(target = asset, options: { client?: InstanceType<typeof QueryClient>; onSaved?: () => void; onClose?: () => void; textModel?: string; userId?: string } = {}) {
    expect(typeof components.ProjectProfileAnalysisModal).toBe("function");
    const oldUser = useUserStore.getState().user; const oldConfig = useConfigStore.getState().config;
    useUserStore.setState({ user: { id: options.userId || "tester" } as NonNullable<typeof oldUser> });
    const capabilityConfig = defaultModelCapabilityConfig("openai-chat", "vision");
    capabilityConfig.text!.references.maxImages = 1; capabilityConfig.text!.references.maxImageBytes = 1024;
    useConfigStore.setState({ config: { ...defaultConfig, textModel: options.textModel ?? "system::vision", model: "wrong", channels: [createModelChannel({ id: "system", scope: "system", apiKey: "system", models: ["vision"], modelCosts: [{ model: "vision", capability: "text", protocol: "openai-chat", billingMode: "token", unitPriceMicrocredits: 0, inputTokenPriceMicrocredits: 100, outputTokenPriceMicrocredits: 100, cachedTokenPriceMicrocredits: 100, capabilityConfig }] })] } });
    const host = document.createElement("div"); document.body.append(host); const root = createRoot(host);
    const client = options.client || new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => root.render(<ConfigProvider theme={{ token: { motion: false } }}><App><QueryClientProvider client={client}><components.ProjectProfileAnalysisModal project={project} asset={target} onClose={options.onClose || (() => {})} onSaved={options.onSaved || (() => {})} /></QueryClientProvider></App></ConfigProvider>));
    await act(async () => { await settle(); });
    await act(async () => { await settle(); });
    return { client, dispose: async () => { await act(async () => root.unmount()); if (!options.client) client.clear(); host.remove(); useUserStore.setState({ user: oldUser }); useConfigStore.setState({ config: oldConfig }); } };
}
function respond(config: any, data: unknown) { return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data, msg: "" } }; }
function readData(config: any, task = analysis, target = asset) {
    if (config.url?.endsWith("/assets")) return { assets: [target] };
    return config.url?.endsWith("/profile-analyses") ? { analyses: [task] } : { analysis: task };
}
test("关闭分析弹窗先完成原生Modal回焦点，再通知页面卸载", async () => {
    apiClient.defaults.adapter = async (config) => respond(config, { analyses: [] });
    const trigger = document.createElement("button");
    document.body.append(trigger); trigger.focus();
    let closed = 0;
    const view = await fixture(asset, { onClose: () => { closed++; } });
    try {
        const close = document.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!;
        await act(async () => { close.focus(); close.click(); await settle(); });
        expect(document.activeElement === trigger).toBe(true);
        expect(closed).toBe(1);
    } finally { await view.dispose(); trigger.remove(); }
});
for (const textModel of ["missing::old", ""]) test(`P2 存储模型${textModel || "为空"}在禁用自定义渠道时不得替换为首个付费模型`, async () => {
    const features = useUserStore.getState().features;
    useUserStore.setState({ features: { ...features, customChannelsEnabled: false } });
    apiClient.defaults.adapter = async (config) => respond(config, { analyses: [] });
    const view = await fixture(asset, { textModel });
    try { expect(button("开始分析").disabled).toBe(true); expect(document.body.textContent).toContain("请选择已声明"); }
    finally { await view.dispose(); useUserStore.setState({ features }); }
});

test("P2 持久采用请求先于POST落盘，新QueryClient与任务清理后仍精确重放并隔离账号", async () => {
    const writes: unknown[] = []; let cleaned = false;
    apiClient.defaults.adapter = async (config) => {
        if (config.url?.endsWith("/adopt")) { writes.push(JSON.parse(config.data)); expect(persisted.size).toBe(1); if (!cleaned) throw new Error("lost"); return respond(config, { asset: { ...asset, primaryVersionId: "v2" }, adoptedVersionId: "v2", replayed: true }); }
        if (cleaned && config.url?.endsWith("/task")) throw new ApiError("gone", { status: 404, reason: "asset_profile_analysis_not_found" });
        return respond(config, cleaned && config.url?.endsWith("/profile-analyses") ? { analyses: [] } : readData(config));
    };
    let view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(persisted.size).toBe(1);
        await view.dispose(); cleaned = true;
        view = await fixture(asset, { userId: "other" }); expect(button("重试同一采用请求")).toBeUndefined(); await view.dispose();
        view = await fixture(); expect(button("重试同一采用请求")).toBeDefined();
        await act(async () => { button("重试同一采用请求").click(); await settle(); });
        expect(writes.length).toBe(2); expect(writes[1]).toEqual(writes[0]); expect(persisted.size).toBe(0);
        expect(document.body.textContent).toContain("服务端已保存");
    } finally { await view.dispose(); }
});

test("P2 采用记录持久化失败不得POST", async () => {
    let posts = 0;
    storageSpies[1].mockImplementationOnce(async () => { throw new Error("storage unavailable"); });
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/adopt")) posts++; return respond(config, readData(config)); };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(posts).toBe(0); expect(persisted.size).toBe(0);
        expect(document.body.textContent).toContain("采用请求未能持久保存");
    } finally { await view.dispose(); storageSpies[1].mockImplementation(async (key, value) => { persisted.set(key, value); return value; }); }
});

test("P2 持久化尚未完成或账号已切换时不得提交采用", async () => {
    let release!: () => void; const gate = new Promise<void>((resolve) => { release = resolve; }); let posts = 0;
    storageSpies[1].mockImplementationOnce(async (key, value) => { await gate; persisted.set(key, value); return value; });
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/adopt")) posts++; return respond(config, readData(config)); };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(posts).toBe(0); expect(persisted.size).toBe(0);
        await act(async () => useUserStore.setState({ user: { id: "other" } as NonNullable<ReturnType<typeof useUserStore.getState>["user"]> }));
        await act(async () => { release(); await settle(); });
        expect(posts).toBe(0); expect(document.body.textContent).not.toContain("新描述");
    } finally { release(); await view.dispose(); }
});

test("P2 重试读取收据后立即确认保存，不等待资产刷新且不再POST", async () => {
    let receipt = false; let posts = 0; let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    apiClient.defaults.adapter = async (config) => {
        if (config.url?.endsWith("/adopt")) { posts++; throw new Error("lost"); }
        if (receipt && config.url?.endsWith("/assets")) { await gate; throw new Error("read failed"); }
        return respond(config, readData(config, receipt ? { ...analysis, currentVersionId: "v2", canAdopt: false, adoption: { adoptedVersionId: "v2", selectedFields: ["description"], adoptedAt: "" } } : analysis));
    };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        receipt = true;
        await act(async () => { button("重试同一采用请求").click(); await settle(); });
        expect(document.body.textContent).toContain("服务端已保存"); expect(document.body.textContent).not.toContain("采用结果尚未确认"); expect(posts).toBe(1);
        await act(async () => { release(); await settle(); });
        expect(document.body.textContent).toContain("服务端已保存，但本地刷新失败");
    } finally { release(); await view.dispose(); }
});

test("P2 当前版本读取失败不能将旧卡片内容标为当前值", async () => {
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/assets")) throw new Error("offline"); return respond(config, readData(config, { ...analysis, currentVersionId: "v2", canAdopt: false })); };
    const view = await fixture();
    try { expect(document.body.textContent).toContain("当前值暂不可用"); expect(button("重新分析").disabled).toBe(true); }
    finally { await view.dispose(); }
});

test("P2 刷新返回旧版本时明确不可用，不把旧值当作当前或无限等待", async () => {
    let reads = 0;
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/assets")) reads++; return respond(config, readData(config, { ...analysis, currentVersionId: "v2", canAdopt: false })); };
    const view = await fixture();
    try { expect(reads).toBe(1); expect(document.body.textContent).toContain("当前值暂不可用"); expect(document.body.textContent).not.toContain("当前值正在刷新"); }
    finally { await view.dispose(); }
});

test("P2 实际活跃QueryClient刷新失败应报告已保存但刷新失败", async () => {
    const { QueryObserver } = await import("@tanstack/react-query");
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let fail = false; let reads = 0;
    const observer = new QueryObserver(client, { queryKey: ["project", "project", "assets"], queryFn: async () => { reads++; if (fail) throw new Error("refetch failed"); return {}; } });
    const unsubscribe = observer.subscribe(() => {}); await observer.refetch();
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/adopt")) { fail = true; return respond(config, { asset: { ...asset, primaryVersionId: "v2" }, adoptedVersionId: "v2", replayed: false }); } return respond(config, readData(config)); };
    const view = await fixture(asset, { client });
    try { await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click()); await act(async () => { button("采用所选1项").click(); await settle(); }); expect(reads).toBeGreaterThan(1); expect(document.body.textContent).toContain("服务端已保存，但本地刷新失败"); }
    finally { unsubscribe(); await view.dispose(); client.clear(); }
});

test("P2 丢失采用响应后收据已确认但资产GET失败不得再报写入不确定", async () => {
    let receipt = false;
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/adopt")) { receipt = true; throw new Error("lost response"); } if (receipt && config.url?.endsWith("/assets")) throw new Error("read failed"); return respond(config, readData(config, receipt ? { ...analysis, adoption: { adoptedVersionId: "v2", selectedFields: ["description"], adoptedAt: "" }, currentVersionId: "v2", canAdopt: false } : analysis)); };
    const view = await fixture();
    try { await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click()); await act(async () => { button("采用所选1项").click(); await settle(); }); expect(document.body.textContent).toContain("服务端已保存，但本地刷新失败"); expect(document.body.textContent).not.toContain("采用结果尚未确认"); }
    finally { await view.dispose(); }
});

test("P2 当前版本不匹配时读取最新值，历史任务图与新分析图分开显示", async () => {
    let release!: () => void; const gate = new Promise<void>((resolve) => { release = resolve; }); let reads = 0; const imageReads: string[] = [];
    apiClient.defaults.adapter = async (config) => { if (config.url === "/resources/access") imageReads.push(...JSON.parse(config.data).map((item: { resourceId: string }) => item.resourceId)); if (config.url?.endsWith("/assets")) { reads++; await gate; return respond(config, { assets: [{ ...asset, primaryVersionId: "v2", referenceImage: { resourceId: "new-image" }, visual: { status: "missing", definition: { description: "当前新值" } } }] }); } return respond(config, readData(config, { ...analysis, currentVersionId: "v2", source: { resourceId: "task-original" }, canAdopt: false })); };
    const view = await fixture();
    try { expect(reads).toBeGreaterThan(0); expect(document.body.textContent).toContain("当前值正在刷新"); expect(document.querySelector('figure[aria-label="所选分析任务原图"]')?.textContent).toContain("task-original"); await act(async () => { release(); await settle(); }); expect(document.body.textContent).toContain("当前新值"); expect(document.querySelector('figure[aria-label="新分析原图"]')?.textContent).toContain("new-image"); await act(async () => { await settle(); }); expect(imageReads).toContain("task-original"); expect(imageReads).toContain("new-image"); }
    finally { release(); await view.dispose(); }
});

for (const category of ["character", "environment", "prop"] as const) test(`真实${category}弹窗默认无勾选、未知禁用，保留JSON并明确替换警告`, async () => {
    const target = { ...asset, category, ...(category === "character" ? { character: { versionId: "v1", version: 1, definition: { props: ["剑", "伞"] }, representations: [], visualStatus: "missing", voiceStatus: "missing" } } : {}) };
    const task = { ...analysis, category, baseline: { versionId: "v1", fields: category === "character" ? { props: ["剑", "伞"] } : analysis.baseline!.fields }, draft: category === "character" ? { appearance: "", physique: "", clothing: "红衣", props: "剑" } : analysis.draft };
    apiClient.defaults.adapter = async (config) => respond(config, readData(config, task, target));
    const view = await fixture(target);
    try {
        const inputs = [...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
        expect(inputs.length).toBe(category === "character" ? 4 : 2);
        expect(inputs.every((input) => !input.checked)).toBe(true);
        expect(inputs.some((input) => input.disabled)).toBe(true);
        expect(document.body.textContent).toContain("未知／待确认");
        expect(document.body.textContent).toContain("整个字段替换为文本");
        expect(document.body.textContent).not.toContain("[object Object]");
        expect(button("采用所选0项").disabled).toBe(true);
        await act(async () => inputs.find((input) => !input.disabled)!.click());
        expect(button("采用所选1项").disabled).toBe(false);
    } finally { await view.dispose(); }
});

test("采用响应不确定先读取恢复，锁定字段集与key，重试不创建分析", async () => {
    const posts: any[] = []; const order: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        order.push(`${config.method}:${config.url}`);
        if (config.method === "post" && config.url !== "/resources/access") { posts.push(JSON.parse(config.data)); throw new Error("secret upstream URL"); }
        return respond(config, readData(config));
    };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); button("采用所选1项").click(); await settle(); });
        expect(posts.length).toBe(1);
        expect(document.body.textContent).not.toContain("secret upstream URL");
        expect(document.body.textContent).toContain("结果尚未确认");
        expect(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.disabled).toBe(true);
        await act(async () => { button("重试同一采用请求").click(); await settle(); });
        expect(posts.length).toBe(2); expect(posts[1]).toEqual(posts[0]);
        expect(posts[0].selectedFields).toEqual(["description"]);
        expect(posts[0].idempotencyKey).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
        expect(order.filter((item) => item.startsWith("post:/projects/")).every((item) => item.endsWith("/adopt"))).toBe(true);
        const first = order.indexOf("post:/projects/project/assets/asset/profile-analyses/task/adopt");
        expect(order.slice(first + 1)).toContain("get:/projects/project/assets/asset/profile-analyses/task");
    } finally { await view.dispose(); }
});

test("409保留草稿与原基准，刷新当前对照且不自动收费重跑", async () => {
    let conflict = false; let posts = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post" && config.url !== "/resources/access") { posts++; conflict = true; throw new ApiError("private upstream", { status: 409, reason: "project_asset_version_changed" }); }
        return respond(config, readData(config, conflict ? { ...analysis, currentVersionId: "v2", canAdopt: false, blockedReason: "project_asset_version_changed" } : analysis, conflict ? { ...asset, primaryVersionId: "v2", visual: { status: "missing", definition: { description: "最新描述" } } } : asset));
    };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(document.body.textContent).toContain("新描述"); expect(document.body.textContent).toContain("最新描述");
        expect(document.body.textContent).toContain("草稿已保留"); expect(document.body.textContent).not.toContain("private upstream");
        expect(button("采用所选1项").disabled).toBe(true); expect(posts).toBe(1);
    } finally { await view.dispose(); }
});

test("创建断网先恢复查询，不自动重发收费请求；模型选择只发送文本视觉模型", async () => {
    const calls: string[] = []; let payload: any;
    apiClient.defaults.adapter = async (config) => {
        calls.push(`${config.method}:${config.url}`);
        if (config.method === "post" && config.url !== "/resources/access") { payload = JSON.parse(config.data); throw new Error("private"); }
        return respond(config, config.url?.endsWith("/assets") ? { assets: [asset] } : { analyses: [] });
    };
    const view = await fixture();
    try {
        await act(async () => { button("开始分析").click(); button("开始分析").click(); await settle(); });
        expect(payload).toEqual({ expectedVersionId: "v1", sourceResourceId: "original", modelSelection: { kind: "system", channelId: "system", modelKey: "vision" } });
        expect(calls.filter((call) => call.startsWith("post:/projects/")).length).toBe(1);
        expect(calls.at(-1)).toBe("get:/projects/project/assets/asset/profile-analyses");
        expect(document.body.textContent).toContain("重新分析会创建新任务");
    } finally { await view.dispose(); }
});

test("活动详情2秒轮询、关闭不取消，显式取消调用原任务接口并刷新终态", async () => {
    let task = { ...analysis, status: "running" as ProfileAnalysisView["status"], canCancel: true, canAdopt: false, draft: undefined }; let reads = 0; const posts: string[] = []; let closed = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post" && config.url !== "/resources/access") { posts.push(config.url!); task = { ...task, status: "cancelled", canCancel: false }; }
        else if (config.url?.endsWith("/task")) reads++;
        return respond(config, readData(config, task));
    };
    let view = await fixture(asset, { onClose: () => { closed++; } });
    try {
        expect(document.body.textContent).toContain("不保证供应商停止或退费");
        const before = reads; await act(async () => { await new Promise((resolve) => setTimeout(resolve, 2100)); });
        expect(reads).toBeGreaterThan(before);
        await act(async () => button("关闭").click()); expect(closed).toBe(1); expect(posts).toEqual([]);
        await view.dispose();
        view = await fixture(asset, { onClose: () => { closed++; } });
        expect(posts).toEqual([]);
        await act(async () => { button("取消分析任务").click(); await settle(); });
        expect(posts).toEqual(["/tasks/task/cancel"]); expect(document.body.textContent).toContain("已取消");
    } finally { await view.dispose(); }
}, 10000);

test("旧账号迟到读取不展示草稿，scope包含用户项目资产任务", async () => {
    let release!: () => void; const gate = new Promise<void>((resolve) => { release = resolve; });
    apiClient.defaults.adapter = async (config) => { if (config.url?.endsWith("/task")) await gate; return respond(config, readData(config)); };
    const view = await fixture();
    try {
        await act(async () => useUserStore.setState({ user: { id: "other" } as NonNullable<ReturnType<typeof useUserStore.getState>["user"]> }));
        await act(async () => { release(); await settle(); });
        expect(document.body.textContent).not.toContain("新描述");
        expect(view.client.getQueryCache().getAll().some((query) => JSON.stringify(query.queryKey) === JSON.stringify(["project-profile-analysis", "tester", "project", "asset", "task"]))).toBe(true);
    } finally { release(); await view.dispose(); }
});

test("采用已提交但刷新失败仍显示服务端已保存且不允许二次采用", async () => {
    let adopted = false; let writes = 0;
    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post" && config.url?.endsWith("/adopt")) { writes++; adopted = true; return respond(config, { adoptedVersionId: "v2", replayed: false, asset: { ...asset, primaryVersionId: "v2" } }); }
        if (adopted && config.url?.includes("profile-analyses")) throw new Error("refresh failed");
        return respond(config, readData(config));
    };
    const view = await fixture(asset, { onSaved: () => { throw new Error("local failed"); } });
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(document.body.textContent).toContain("服务端已保存，但本地刷新失败");
        expect(button("采用所选1项").disabled).toBe(true); expect(writes).toBe(1);
    } finally { await view.dispose(); }
});

test("冲突刷新不依赖旧名称或第一页，当前对照按资产ID查找", async () => {
    let conflict = false; const pages: number[] = [];
    apiClient.defaults.adapter = async (config) => {
        if (config.url?.endsWith("/adopt")) { conflict = true; throw new ApiError("conflict", { status: 409, reason: "project_asset_version_changed" }); }
        if (config.url?.endsWith("/assets")) {
            pages.push(config.params?.page);
            return respond(config, config.params?.q || config.params?.page === 1 ? { assets: [], hasMore: !config.params?.q } : { assets: [{ ...asset, title: "重命名", primaryVersionId: "v2", visual: { status: "missing", definition: { description: "第二页最新值" } } }], hasMore: false });
        }
        return respond(config, readData(config, conflict ? { ...analysis, canAdopt: false, currentVersionId: "v2" } : analysis));
    };
    const view = await fixture();
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(document.body.textContent).toContain("第二页最新值"); expect(pages).toEqual([1, 2]);
    } finally { await view.dispose(); }
});

test("历史基准来自详情，当前档案与任务原图独立展示并保留null和missing", async () => {
    const current = { ...asset, primaryVersionId: "v2", visual: { status: "missing" as const, definition: { description: "CURRENT_ONLY", prompt: "CURRENT_PROMPT" } } };
    const task = { ...analysis, currentVersionId: "v2", canAdopt: false, source: { resourceId: "historical-image" }, baseline: { versionId: "v1", fields: { description: { historical: ["BASELINE_ONLY", null, false] }, prompt: null } } };
    apiClient.defaults.adapter = async (config) => respond(config, readData(config, task, current));
    const view = await fixture(current);
    try {
        const description = document.querySelector('section[aria-label="描述"]')!;
        const values = [...description.querySelectorAll("pre")].map((node) => node.textContent);
        expect(values[0]).toContain("BASELINE_ONLY"); expect(values[0]).not.toContain("CURRENT_ONLY");
        expect(values[1]).toBe("CURRENT_ONLY");
        expect(document.querySelector('section[aria-label="提示词"] pre')?.textContent).toBe("null");
        expect(document.querySelector('figure[aria-label="所选分析任务原图"]')?.textContent).toContain("historical-image");
        expect(button("采用所选0项").disabled).toBe(true);
    } finally { await view.dispose(); }
});

for (const baseline of [undefined, { versionId: "wrong", fields: { description: "wrong" } }]) test(`不可用或错误版本baseline禁止首次采用，即便当前版本相同且服务端canAdopt为true (${baseline?.versionId || "missing"})`, async () => {
    apiClient.defaults.adapter = async (config) => respond(config, readData(config, { ...analysis, baseline }));
    const view = await fixture();
    try {
        expect(document.body.textContent).toContain("历史基准不可读取");
        expect([...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].every((input) => input.disabled)).toBe(true);
        expect(button("采用所选0项").disabled).toBe(true);
    } finally { await view.dispose(); }
});

test("历史字段missing不同于显式null，不从当前值补全", async () => {
    apiClient.defaults.adapter = async (config) => respond(config, readData(config, { ...analysis, baseline: { versionId: "v1", fields: { description: null } } }));
    const view = await fixture();
    try {
        expect(document.querySelector('section[aria-label="描述"] pre')?.textContent).toBe("null");
        expect(document.querySelector('section[aria-label="提示词"] pre')?.textContent).toBe("未填写");
    } finally { await view.dispose(); }
});

test("旧基准恢复明确不可读取历史值，不将当前值冒充基准", async () => {
    apiClient.defaults.adapter = async (config) => respond(config, readData(config, { ...analysis, baseVersionId: "old", canAdopt: false }));
    const view = await fixture();
    try { expect(document.body.textContent).toContain("历史基准不可读取"); expect(document.body.textContent).toContain("当前值"); expect(button("采用所选0项").disabled).toBe(true); }
    finally { await view.dispose(); }
});

test("采用后刷新真正的角色详情缓存", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["character-studio", "asset"], { old: true });
    apiClient.defaults.adapter = async (config) => config.url?.endsWith("/adopt") ? respond(config, { adoptedVersionId: "v2", replayed: false, asset: { ...asset, primaryVersionId: "v2" } }) : respond(config, readData(config));
    const view = await fixture(asset, { client });
    try {
        await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]:not(:disabled)')!.click());
        await act(async () => { button("采用所选1项").click(); await settle(); });
        expect(client.getQueryState(["character-studio", "asset"])?.isInvalidated).toBe(true);
    } finally { await view.dispose(); client.clear(); }
});

for (const category of ["character", "environment", "prop"] as const) test(`真实${category}资产卡片有可发现的分析入口`, async () => {
    const host = document.createElement("div"); document.body.append(host); const root = createRoot(host); let calls = 0;
    const common = { asset: { ...asset, category }, folderItems: [], generating: false, onOpen: () => {}, onGenerate: () => {}, onMove: () => {}, onRemove: () => {}, onAnalyze: () => { calls++; } };
    try {
        await act(async () => root.render(category === "character" ? <ProjectCharacterCard {...common} removing={false} onEdit={() => {}} onBindImages={() => {}} onBindVoice={() => {}} /> : <ProjectVisualAssetCard {...common} busy={false} onBind={() => {}} />));
        expect(button("分析参考图")).toBeDefined();
        expect(button("分析参考图").closest("[data-agent-launcher-avoid]")).not.toBeNull();
        await act(async () => button("分析参考图").click()); expect(calls).toBe(1);
    } finally { await act(async () => root.unmount()); host.remove(); }
});

test("缺少原图不能拿当前成品分析，复用原始参考保存入口", async () => {
    let posts = 0;
    apiClient.defaults.adapter = async (config) => { if (config.method === "post" && config.url !== "/resources/access") posts++; return respond(config, config.url?.endsWith("/assets") ? { assets: [{ ...asset, referenceImage: undefined }] } : config.url === "/tasks" ? [] : { analyses: [] }); };
    const view = await fixture({ ...asset, referenceImage: undefined, storageKey: "resource:finished" });
    try { expect(button("开始分析").disabled).toBe(true); await act(async () => { button("选择并保存原始参考图").click(); await settle(); }); expect(document.body.textContent).toContain("图片创作"); expect(document.body.textContent).toContain("保存"); expect(posts).toBe(0); }
    finally { await view.dispose(); }
});
