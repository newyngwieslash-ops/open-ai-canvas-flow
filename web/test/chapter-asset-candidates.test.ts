import { afterAll, afterEach, describe, expect, test } from "bun:test";
import { Window } from "happy-dom";
import * as candidatesModule from "@/pages/projects/detail/chapter-asset-candidates";

const dom = new Window({ url: "http://localhost/" });
const globals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, IS_REACT_ACT_ENVIRONMENT: true })) {
    globals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}
const { act, createElement } = await import("react");
const { createRoot } = await import("react-dom/client");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const { setActiveUserScope } = await import("@/lib/user-scope");
afterAll(() => {
    dom.happyDOM.abort();
    for (const [key, descriptor] of globals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});
const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

async function candidateView() {
    expect(typeof candidatesModule.useChapterAssetCandidates).toBe("function");
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const host = document.createElement("div");
    const root = createRoot(host);
    let current!: ReturnType<typeof candidatesModule.useChapterAssetCandidates>;
    function View({ enabled }: { enabled: boolean }) {
        current = candidatesModule.useChapterAssetCandidates("project", enabled);
        return null;
    }
    const render = async (enabled = true) => act(async () => { root.render(createElement(QueryClientProvider, { client }, createElement(View, { enabled }))); await settle(); });
    await render();
    return { current: () => current, client, render, dispose: async () => { await act(async () => root.unmount()); client.clear(); } };
}
import { loadChapterAssetCandidates as loadCandidates } from "@/pages/projects/detail/chapter-asset-candidates";
import { apiClient } from "@/services/api/request";
import { listProjectAssetsPage, type ProjectAsset, type ProjectAssetPage } from "@/services/api/projects";

const originalAdapter = apiClient.defaults.adapter;
afterEach(() => { apiClient.defaults.adapter = originalAdapter; });

function asset(id: string, category: ProjectAsset["category"] = "prop"): ProjectAsset {
    return { id, title: id, category, mediaType: "image", status: "draft", versionCount: 0, usages: [], position: 0, updatedAt: "" };
}
function page(assets: ProjectAsset[], hasMore = false, pageNumber = 1): ProjectAssetPage {
    return { assets, hasMore, page: pageNumber, pageSize: 80, total: 999, categoryCounts: {}, folderCounts: {} };
}

describe("章节完整资产候选", () => {
    test("零引用章节按项目目录发现三类，包括无版本资产", async () => {
        const assets = [asset("character", "character"), asset("scene", "environment"), asset("prop"), asset("other", "other")];
        apiClient.defaults.adapter = async (config) => {
            expect(config.url).toBe("/projects/project/assets");
            expect(config.params.category).toBeUndefined();
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page(assets), msg: "" } };
        };
        expect((await loadCandidates("project")).map((item) => item.id)).toEqual(["character", "scene", "prop"]);
    });
    test("81 项跨页不截断到 60，按 ID 去重，直到 hasMore=false", async () => {
        const requests: number[] = [];
        const first = Array.from({ length: 80 }, (_, i) => asset(String(i)));
        const pages = [page(first, true), page([first[0], asset("80")], true, 2), page([], false, 3)];
        apiClient.defaults.adapter = async (config) => {
            requests.push(config.params.page);
            expect(config.params.pageSize).toBeLessThanOrEqual(80);
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: pages[config.params.page - 1], msg: "" } };
        };
        const result = await loadCandidates("project");
        expect(result).toHaveLength(81);
        expect(new Set(result.map((item) => item.id)).size).toBe(81);
        expect(requests).toEqual([1, 2, 3]);
    });
    test("末页失败拒绝整个目录，不返回部分成功", async () => {
        const failure = new Error("last page failed");
        apiClient.defaults.adapter = async (config) => {
            if (config.params.page === 2) throw failure;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([asset("first")], true), msg: "" } };
        };
        await expect(loadCandidates("project")).rejects.toBe(failure);
    });
    test("中途取消原样上抛，不交付已加载首页", async () => {
        const controller = new AbortController();
        const cancellation = new DOMException("cancelled", "AbortError");
        let requests = 0;
        apiClient.defaults.adapter = async (config) => {
            requests++;
            expect(config.signal).toBe(controller.signal);
            if (config.params.page === 2) throw cancellation;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([asset("first")], true), msg: "" } };
        };
        await expect(loadCandidates("project", controller.signal)).rejects.toBe(cancellation);
        expect(requests).toBe(2);
    });
    test("已取消时不发送请求", async () => {
        let requests = 0;
        apiClient.defaults.adapter = async () => { requests++; throw new Error("must not request"); };
        const controller = new AbortController();
        const reason = new DOMException("closed", "AbortError");
        controller.abort(reason);
        await expect(loadCandidates("project", controller.signal)).rejects.toBe(reason);
        expect(requests).toBe(0);
    });
    test("页面只在末页完成后 complete，用户 scope 独立", async () => {
        setActiveUserScope("user-a");
        let release!: () => void;
        const gate = new Promise<void>((resolve) => { release = resolve; });
        let reads = 0;
        apiClient.defaults.adapter = async (config) => {
            reads++;
            if (config.params.page === 2) await gate;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([asset(`p${config.params.page}`)], config.params.page === 1), msg: "" } };
        };
        const view = await candidateView();
        try {
            await act(settle);
            expect(view.current().state).toEqual({ status: "loading" });
            expect(view.client.getQueriesData({ queryKey: ["project", "project", "chapter-asset-candidates"] }).every(([, data]) => data === undefined)).toBe(true);
            await act(async () => { release(); await settle(); });
            expect(view.current().state.status).toBe("complete");
            const completed = view.current().state;
            expect(completed.status === "complete" && completed.assets.length).toBe(2);
            setActiveUserScope("user-b");
            await view.render();
            await act(settle);
            expect(reads).toBe(4);
        } finally { release(); await view.dispose(); setActiveUserScope(null); }
    });
    test("页面末页失败为 error，重试不采用部分缓存", async () => {
        let fail = true;
        apiClient.defaults.adapter = async (config) => {
            if (fail && config.params.page === 2) throw new Error("末页失败");
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([asset(String(config.params.page))], config.params.page === 1), msg: "" } };
        };
        const view = await candidateView();
        try {
            await act(settle);
            expect(view.current().state).toEqual({ status: "error" });
            fail = false;
            await act(async () => { await view.current().retry(); await settle(); });
            expect(view.current().state.status).toBe("complete");
        } finally { await view.dispose(); }
    });
    test("页面取消为 cancelled，迟到结果不能 complete，重开重新加载", async () => {
        let release!: () => void;
        const gate = new Promise<void>((resolve) => { release = resolve; });
        let signal: any;
        let reads = 0;
        apiClient.defaults.adapter = async (config) => {
            reads++; signal = config.signal; await gate;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([asset("late")]), msg: "" } };
        };
        const view = await candidateView();
        try {
            await act(async () => { await view.current().cancel(); });
            expect(signal.aborted).toBe(true);
            expect(view.current().state).toEqual({ status: "cancelled" });
            await act(async () => { release(); await settle(); });
            expect(view.current().state).toEqual({ status: "cancelled" });
            await view.render(false);
            await view.render(true);
            await act(settle);
            expect(reads).toBe(2);
            expect(view.current().state.status).toBe("complete");
        } finally { release(); await view.dispose(); }
    });
    test("真实章节 owner 使用完整目录状态而非 workspace.assets", async () => {
        const source = await Bun.file(new URL("../src/pages/projects/detail/chapters.tsx", import.meta.url)).text();
        expect(source).toContain("useChapterAssetCandidates(detail.project.id, storyboardOpen)");
        expect(source).toContain("assetCandidates: chapterAssetCandidates.state");
        expect(source).not.toContain("assets: chapterStoryboardAssets(detail)");
        expect(source).toContain('chapterAssetCandidates.state.status !== "complete"');
        expect(source).toContain("chapterAssetCandidates.cancel()");
    });
    test("分页 API 保留原 options 并传递 AbortSignal", async () => {
        const controller = new AbortController();
        let seen: any;
        apiClient.defaults.adapter = async (config) => {
            seen = config;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: page([]), msg: "" } };
        };
        await listProjectAssetsPage("project/1", { page: 2, pageSize: 80, category: "prop", mediaType: "image", status: "draft", folderId: "folder", query: "灯" }, controller.signal);
        expect(seen.url).toBe("/projects/project%2F1/assets");
        expect(seen.params).toEqual({ page: 2, pageSize: 80, category: "prop", mediaType: "image", status: "draft", folderId: "folder", q: "灯" });
        expect(seen.signal).toBe(controller.signal);
        controller.abort();
        await expect(listProjectAssetsPage("project/1", {}, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
    });
});
