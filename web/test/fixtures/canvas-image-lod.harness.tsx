import { afterAll, expect, test } from "bun:test";
import { Window } from "happy-dom";

const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();

class MockImage {
    src = "";
    decoding = "async";
    complete = true;
    naturalWidth = 640;
    naturalHeight = 360;
    onload: ((event: Event) => void) | null = null;
    onerror: ((event: Event) => void) | null = null;
    removeAttribute(name: string) {
        if (name === "src") this.src = "";
    }
    decode() {
        return Promise.resolve();
    }
}

for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    localStorage: dom.localStorage,
    sessionStorage: dom.sessionStorage,
    customElements: dom.customElements,
    MutationObserver: dom.MutationObserver,
    HTMLElement: dom.HTMLElement,
    HTMLImageElement: dom.HTMLImageElement,
    Element: dom.Element,
    Node: dom.Node,
    Text: dom.Text,
    SVGElement: dom.SVGElement,
    Image: MockImage,
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
const { canvasThemes } = await import("../../src/lib/canvas-theme");
const { ImageContent } = await import("../../src/components/canvas/canvas-node-media-content");
const { CanvasNodeType } = await import("../../src/types/canvas");

const originalPost = http.post;

afterAll(() => {
    http.post = originalPost;
    clearResourceAccessCache();
    dom.happyDOM.abort();
    for (const [key, value] of originals) {
        if (value) Object.defineProperty(globalThis, key, value);
        else Reflect.deleteProperty(globalThis, key);
    }
});

test("ImageContent keeps retained img src across shell LOD and suppresses new shell loads", async () => {
    clearResourceAccessCache();
    const requestedIds: string[] = [];
    http.post = (async (url: string, body?: unknown) => {
        if (url === "/resources/access" && Array.isArray(body)) {
            const now = new Date();
            const items = body.map((entry: { resourceId: string; variant?: string }) => {
                requestedIds.push(entry.resourceId);
                return {
                    resourceId: entry.resourceId,
                    access: {
                        resourceId: entry.resourceId,
                        requestedVariant: entry.variant || "thumbnail",
                        actualVariant: entry.variant || "thumbnail",
                        url: `https://cdn.example.com/${entry.resourceId}.jpg`,
                        delivery: "cdn",
                        issuedAt: now.toISOString(),
                        expiresAt: new Date(now.getTime() + 60_000).toISOString(),
                        refreshAt: new Date(now.getTime() + 30_000).toISOString(),
                        revision: "rev-1",
                        originalWidth: 640,
                        originalHeight: 360,
                    },
                };
            });
            return { items } as never;
        }
        throw new Error(`unexpected post: ${url}`);
    }) as typeof http.post;

    const theme = canvasThemes.dark;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);

    const makeNode = (storageKey: string) => ({
        id: "img-node-1",
        type: CanvasNodeType.Image,
        title: "测试图",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: { storageKey },
    });

    const renderAndFlush = async (renderLOD: "shell" | "preview" | "full", storageKey: string) => {
        await act(async () => {
            root.render(
                <ImageContent
                    renderLOD={renderLOD}
                    node={makeNode(storageKey)}
                    theme={theme}
                    isBatchRoot={false}
                    batchCount={1}
                    batchExpanded={false}
                    batchOpening={false}
                    batchRecovering={false}
                />,
            );
        });
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 35));
        });
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 5));
        });
    };

    try {
        await renderAndFlush("preview", "resource:img-1");

        const imgElement = host.querySelector("img");
        expect(imgElement).not.toBeNull();
        expect(imgElement?.getAttribute("src")).toBe("https://cdn.example.com/img-1.jpg");
        expect(requestedIds).toEqual(["img-1"]);

        await renderAndFlush("shell", "resource:img-1");

        expect(host.querySelector("img")).toBe(imgElement);
        expect(host.querySelector("img")?.getAttribute("src")).toBe("https://cdn.example.com/img-1.jpg");
        expect(requestedIds).toEqual(["img-1"]);

        await renderAndFlush("shell", "resource:img-2");

        expect(requestedIds).toEqual(["img-1"]);
        expect(host.querySelector("img")?.getAttribute("src") ?? "").toBe("");

        await renderAndFlush("preview", "resource:img-2");

        expect(requestedIds).toEqual(["img-1", "img-2"]);
        expect(host.querySelector("img")?.getAttribute("src")).toBe("https://cdn.example.com/img-2.jpg");
    } finally {
        await act(async () => root.unmount());
        host.remove();
    }
});
