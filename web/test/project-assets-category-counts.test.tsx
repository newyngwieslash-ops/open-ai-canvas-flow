import { afterAll, expect, test } from "bun:test";
import { Window } from "happy-dom";
import type { ProjectAssetCandidate, ProjectDetail } from "../src/services/api/projects";

const dom = new Window({ url: "http://localhost/" });
dom.Element.prototype.animate = undefined as never;
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    localStorage: dom.localStorage,
    ResizeObserver: dom.ResizeObserver,
    HTMLElement: dom.HTMLElement,
    Element: dom.Element,
    Node: dom.Node,
    SVGElement: dom.SVGElement,
    ShadowRoot: dom.ShadowRoot,
    getComputedStyle: dom.getComputedStyle.bind(dom),
    requestAnimationFrame: dom.requestAnimationFrame.bind(dom),
    cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom),
    IS_REACT_ACT_ENVIRONMENT: true,
})) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}

const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { App, ConfigProvider } = await import("antd");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const ProjectAssetsView = (await import("../src/pages/projects/detail/assets")).default;
const { apiClient } = await import("../src/services/api/request");

const adapter = apiClient.defaults.adapter;

afterAll(() => {
    apiClient.defaults.adapter = adapter;
    dom.happyDOM.abort();
    for (const [key, value] of originals) {
        if (value) Object.defineProperty(globalThis, key, value);
        else Reflect.deleteProperty(globalThis, key);
    }
});

const candidates: ProjectAssetCandidate[] = [
    ...Array.from({ length: 7 }, (_, index) => ({
        id: `char-${index + 1}`,
        projectId: "project-1",
        unitId: "unit-1",
        name: `角色${index + 1}`,
        category: "character" as const,
        status: "pending_confirmation" as const,
        detailsJson: "{}",
        createdAt: "",
        updatedAt: "",
    })),
    {
        id: "scene-1",
        projectId: "project-1",
        unitId: "unit-1",
        name: "城市非遗文化展厅",
        category: "environment",
        status: "pending_confirmation",
        detailsJson: "{}",
        createdAt: "",
        updatedAt: "",
    },
    ...Array.from({ length: 11 }, (_, index) => ({
        id: `prop-${index + 1}`,
        projectId: "project-1",
        unitId: "unit-1",
        name: `道具${index + 1}`,
        category: "prop" as const,
        status: "pending_confirmation" as const,
        detailsJson: "{}",
        createdAt: "",
        updatedAt: "",
    })),
];

const detail = {
    project: {
        id: "project-1",
        userId: "user-1",
        name: "东北测试",
        type: "short_drama",
        aspectRatio: "16:9",
        sourceType: "novel",
        description: "",
        stylePresetId: "",
        status: "active",
        revision: 1,
        createdAt: "",
        updatedAt: "",
    },
    units: [],
    canvases: [],
    canvasUnitLinks: [],
    assets: [],
    assetFolders: [],
    workflows: [],
    shots: [],
    shotRevisions: [],
    shotArtifacts: [],
    shotReferences: [],
    assetCandidates: candidates,
    tasks: [],
} as ProjectDetail;

test("左侧分类筛选包含角色、场景与道具的待确认候选计数并与当前切换分类保持一致", async () => {
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        if (url.endsWith("/assets")) {
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        assets: [],
                        categoryCounts: {},
                        folderCounts: {},
                        page: 1,
                        pageSize: 40,
                        total: 0,
                        hasMore: false,
                    },
                    msg: "",
                },
            };
        }
        if (url.endsWith("/asset-folders")) {
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: { code: 0, data: { folders: [] }, msg: "" },
            };
        }
        if (url.endsWith("/asset-candidates")) {
            const category = config.params?.category as string | undefined;
            const pageSize = Number(config.params?.pageSize || 100);
            const filtered = candidates.filter((item) => !category || item.category === category);
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        candidates: filtered.slice(0, pageSize),
                        page: 1,
                        pageSize,
                        total: filtered.length,
                        hasMore: filtered.length > pageSize,
                    },
                    msg: "",
                },
            };
        }
        return {
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: { code: 0, data: [], msg: "" },
        };
    };

    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    try {
        await act(async () => {
            root.render(
                <ConfigProvider theme={{ token: { motion: false } }}>
                    <App>
                        <QueryClientProvider client={client}>
                            <ProjectAssetsView detail={detail} refreshProject={() => {}} onCreateCanvas={() => {}} />
                        </QueryClientProvider>
                    </App>
                </ConfigProvider>,
            );
        });
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 50));
        });

        const nav = host.querySelector('nav[aria-label="素材目录与资产分类"]')!;
        const categoryButtonText = (label: string) => {
            const btn = [...nav.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.firstElementChild?.textContent === label);
            return btn?.textContent?.trim();
        };

        expect(categoryButtonText("全部资产")).toBe("全部资产19");
        expect(categoryButtonText("角色")).toBe("角色7");
        expect(categoryButtonText("场景")).toBe("场景1");
        expect(categoryButtonText("道具")).toBe("道具11");
        expect(categoryButtonText("素材")).toBe("素材0");
        expect(categoryButtonText("其他")).toBe("其他0");

        const sceneBtn = [...nav.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.firstElementChild?.textContent === "场景")!;
        await act(async () => {
            sceneBtn.click();
            await new Promise((resolve) => setTimeout(resolve, 50));
        });
        expect(host.textContent).toContain("剧情识别出的场景");
        expect(host.textContent).toContain("城市非遗文化展厅");
        expect(categoryButtonText("场景")).toBe("场景1");
        expect(categoryButtonText("道具")).toBe("道具11");
    } finally {
        await act(async () => root.unmount());
        client.clear();
        host.remove();
    }
});
