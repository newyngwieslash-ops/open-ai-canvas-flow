import { afterAll, expect, test } from "bun:test";
import { Window } from "happy-dom";

const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();

for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    localStorage: dom.localStorage,
    sessionStorage: dom.sessionStorage,
    customElements: dom.customElements,
    MutationObserver: dom.MutationObserver,
    ResizeObserver: dom.ResizeObserver,
    HTMLElement: dom.HTMLElement,
    HTMLTextAreaElement: dom.HTMLTextAreaElement,
    HTMLInputElement: dom.HTMLInputElement,
    Element: dom.Element,
    Node: dom.Node,
    Text: dom.Text,
    SVGElement: dom.SVGElement,
    getComputedStyle: dom.getComputedStyle.bind(dom),
    IS_REACT_ACT_ENVIRONMENT: true,
})) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}

const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { http } = await import("../../src/services/api/request");
const { clearResourceAccessCache } = await import("../../src/services/api/resources");
const { useAssetStore } = await import("../../src/stores/use-asset-store");
const { useResolvedCanvasResourceReferences } = await import("../../src/components/canvas/use-resolved-canvas-resource-references");
const { CanvasResourceMentionTextarea } = await import("../../src/components/canvas/canvas-resource-mention-textarea");
const { ReferencePreview } = await import("../../src/components/canvas/canvas-mention-menu");
const { CanvasNodeType } = await import("../../src/types/canvas");

const originalPost = http.post;

afterAll(() => {
    http.post = originalPost;
    clearResourceAccessCache();
    useAssetStore.setState({ assets: [] });
    dom.happyDOM.abort();
    for (const [key, value] of originals) {
        if (value) Object.defineProperty(globalThis, key, value);
        else Reflect.deleteProperty(globalThis, key);
    }
});

test("request matrix: empty requestedIds=0, single ID=1, referenced in prompt resolves while 2000 unreferenced assets make 0 requests", async () => {
    const requestedIds: string[] = [];
    http.post = (async (url: string, body?: unknown) => {
        if (url === "/resources/access" && Array.isArray(body)) {
            const now = new Date();
            return {
                items: body.map((entry: { resourceId: string; variant?: string }) => {
                    requestedIds.push(entry.resourceId);
                    return {
                        resourceId: entry.resourceId,
                        access: {
                            resourceId: entry.resourceId,
                            requestedVariant: entry.variant || "original",
                            actualVariant: entry.variant || "original",
                            url: `https://cdn.example.com/${entry.resourceId}.jpg`,
                            delivery: "cdn",
                            issuedAt: now.toISOString(),
                            expiresAt: new Date(now.getTime() + 60_000).toISOString(),
                            refreshAt: new Date(now.getTime() + 30_000).toISOString(),
                            revision: "rev-1",
                        },
                    };
                }),
            } as never;
        }
        throw new Error(`unexpected post: ${url}`);
    }) as typeof http.post;

    const references = [
        {
            id: "node-a",
            nodeId: "node-a",
            label: "图片1",
            title: "镜头A",
            kind: "image" as const,
            sourceType: CanvasNodeType.Image,
            storageKey: "resource:res-a",
            previewUrl: "",
            active: false,
        },
        {
            id: "node-b",
            nodeId: "node-b",
            label: "图片2",
            title: "镜头B",
            kind: "image" as const,
            sourceType: CanvasNodeType.Image,
            storageKey: "resource:res-b",
            previewUrl: "",
            active: false,
        },
    ];

    let latestResolved: typeof references = [];
    function HookProbe({ ids }: { ids?: ReadonlySet<string> }) {
        latestResolved = useResolvedCanvasResourceReferences(references, { requestedIds: ids });
        return null;
    }

    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);

    const flush = async () => {
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 30));
        });
    };

    try {
        clearResourceAccessCache();
        requestedIds.length = 0;
        await act(async () => {
            root.render(<HookProbe ids={new Set()} />);
        });
        await flush();
        expect(requestedIds).toEqual([]);
        expect(latestResolved.map((item) => item.id)).toEqual(["node-a", "node-b"]);

        await act(async () => {
            root.render(<HookProbe ids={new Set(["node-a"])} />);
        });
        await flush();
        expect(requestedIds).toEqual(["res-a"]);
        expect(latestResolved.find((item) => item.id === "node-a")?.previewUrl).toBe("https://cdn.example.com/res-a.jpg");
        expect(latestResolved.find((item) => item.id === "node-b")?.previewUrl).toBe("");

        clearResourceAccessCache();
        requestedIds.length = 0;
        useAssetStore.setState({
            assets: Array.from({ length: 2000 }, (_, index) => ({
                id: `asset-${index}`,
                kind: "image" as const,
                title: `素材${index}`,
                coverUrl: "",
                tags: [],
                createdAt: "2026-10-28T00:00:00.000Z",
                updatedAt: "2026-10-28T00:00:00.000Z",
                data: {
                    dataUrl: "",
                    storageKey: `resource:asset-res-${index}`,
                    width: 320,
                    height: 180,
                    bytes: 1024,
                    mimeType: "image/jpeg",
                },
            })),
        });

        await act(async () => {
            root.render(
                <CanvasResourceMentionTextarea
                    value="使用 @[asset:asset-7] 作为参考"
                    references={references}
                    includeAssetLibrary
                    onChange={() => {}}
                />,
            );
        });
        await flush();

        expect(requestedIds).toEqual(["asset-res-7"]);
        const chipImg = host.querySelector("img.canvas-resource-inline-preview");
        expect(chipImg?.getAttribute("src")).toBe("https://cdn.example.com/asset-res-7.jpg");
    } finally {
        await act(async () => root.unmount());
        host.remove();
    }
});

test("ReferencePreview only mounts fallback <video> for active candidate", async () => {
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const videoRef = {
        id: "vid-1",
        nodeId: "vid-1",
        label: "视频1",
        title: "无海报视频",
        kind: "video" as const,
        sourceType: CanvasNodeType.Video,
        mediaUrl: "https://cdn.example.com/raw-video.mp4",
        active: true,
    };
    try {
        await act(async () => {
            root.render(<ReferencePreview reference={videoRef} active={false} />);
        });
        expect(host.querySelectorAll("video").length).toBe(0);

        await act(async () => {
            root.render(<ReferencePreview reference={videoRef} active={true} />);
        });
        expect(host.querySelectorAll("video").length).toBe(1);
    } finally {
        await act(async () => root.unmount());
        host.remove();
    }
});
