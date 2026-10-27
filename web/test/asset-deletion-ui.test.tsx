import { afterAll, expect, test } from "bun:test";
import { Window } from "happy-dom";
import localforage from "localforage";
const originalGet = localforage.getItem;
const originalSet = localforage.setItem;
const originalRemove = localforage.removeItem;
const storage = new Map<string, unknown>();
localforage.getItem = (async (key: string) => storage.get(key) ?? null) as typeof localforage.getItem;
localforage.setItem = (async (key: string, value: unknown) => { storage.set(key, value); return value; }) as typeof localforage.setItem;
localforage.removeItem = async (key: string) => { storage.delete(key); };
const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, HTMLAnchorElement: dom.HTMLAnchorElement, HTMLElement: dom.HTMLElement, Element: dom.Element, Node: dom.Node, SVGElement: dom.SVGElement, ShadowRoot: dom.ShadowRoot, getComputedStyle: dom.getComputedStyle.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}
const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { App } = await import("antd");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const { MemoryRouter } = await import("react-router");
const { default: AssetsPage } = await import("@/pages/assets");
const { CanvasCharacterLibraryModal } = await import("@/components/canvas/canvas-character-library-modal");
const { CharacterImageLibrary, CharacterVoiceLibrary } = await import("@/components/canvas/character-media-library");
const { CanvasWorkspaceAssetPanel } = await import("@/components/canvas/canvas-workspace-asset-panel");
const { AssetLibraryPickerModal } = await import("@/components/assets/asset-library-picker-modal");
const { useAssetStore, withAssetStorePersistenceSuppressed } = await import("@/stores/use-asset-store");
const { useUserStore } = await import("@/stores/use-user-store");
const { apiClient } = await import("@/services/api/request");
const { deleteAssetsWithRemoteSync, initializeRemoteUserDataSession, resetRemoteUserDataSync } = await import("@/services/user-data-sync");
import type { Asset } from "@/stores/use-asset-store";
const asset: Asset = { id: "pending", kind: "text", title: "Pending asset", coverUrl: "", tags: [], createdAt: "2026-10-01", updatedAt: "2026-10-01", data: { content: "text" } };
afterAll(() => {
    dom.happyDOM.abort();
    localforage.getItem = originalGet; localforage.setItem = originalSet; localforage.removeItem = originalRemove;
    for (const [key, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});
async function fixture() {
    const host = document.createElement("div"); document.body.append(host);
    const root = createRoot(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const before = useAssetStore.getState().assets;
    const userBefore = useUserStore.getState();
    const adapter = apiClient.defaults.adapter;
    const render = async (element: React.ReactNode) => { await act(async () => root.render(<QueryClientProvider client={client}><MemoryRouter><App>{element}</App></MemoryRouter></QueryClientProvider>)); };
    const dispose = async () => {
        await act(async () => root.unmount()); client.clear(); host.remove();
        withAssetStorePersistenceSuppressed(() => useAssetStore.setState({ assets: before }));
        useUserStore.setState(userBefore); apiClient.defaults.adapter = adapter;
    };
    return { host, client, render, dispose };
}
test.each(["workspace", "image", "voice", "character"] as const)("accepted remote-only deletion blocks stale cached %s reference", async (path) => {
    const f = await fixture();
    const picks: string[] = [];
    const id = `remote-only-${path}`;
    const image: Asset = { ...asset, id, kind: "image", status: "confirmed", data: { dataUrl: "/api/resources/image/file", storageKey: "resource:image", width: 10, height: 10, bytes: 1, mimeType: "image/png" } };
    const remote: Asset = path === "voice" ? { ...asset, id, kind: "audio", status: "confirmed", data: { url: "/api/resources/audio/file", storageKey: "resource:audio", bytes: 1, mimeType: "audio/mpeg" } } : image;
    const character = { versionId: "v1", definition: {}, representations: [], visualStatus: "ready", voiceStatus: "ready" };
    try {
        useUserStore.setState({ user: { id: "ui-user" } as never, hydrated: true });
        withAssetStorePersistenceSuppressed(() => useAssetStore.setState({ assets: [] }));
        await initializeRemoteUserDataSession("ui-user");
        apiClient.defaults.adapter = async (config) => {
            // Reads deliberately remain stale and available, including after DELETE acceptance.
            const data = config.url === "/assets/batch-delete" ? { ids: [id] } : config.url?.includes("characters") ? { characters: [{ asset: { ...asset, id, status: "confirmed", character }, character }], total: 1 } : { assets: [remote], total: 1, page: 1, pageSize: 40 };
            return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data, msg: "ok" } };
        };
        await f.render(path === "workspace" ? <CanvasWorkspaceAssetPanel onInsert={async () => { picks.push("insert"); }} onManage={() => {}} /> : path === "image" ? <CharacterImageLibrary currentResourceId="" busyKey="" onPick={(value) => picks.push(value)} onUpload={() => {}} /> : path === "voice" ? <CharacterVoiceLibrary currentResourceId="" busyKey="" onPick={(value) => picks.push(value)} onUpload={() => {}} /> : <CanvasCharacterLibraryModal open onClose={() => {}} onInsert={() => { picks.push("character"); }} />);
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 40)); });
        const findButton = () => path === "workspace" ? document.querySelector<HTMLButtonElement>('button[aria-label="插入画布：Pending asset"]') : path === "image" ? document.querySelector<HTMLButtonElement>('button[title="Pending asset"]') : Array.from(document.querySelectorAll("button")).find((button) => path === "voice" ? button.textContent?.trim() === "使用" : button.textContent?.includes("Pending asset"));
        const staleButton = findButton();
        expect(staleButton).toBeDefined();
        expect(staleButton).not.toBeNull();
        expect(useAssetStore.getState().assets).toEqual([]);
        await act(async () => { await deleteAssetsWithRemoteSync([id]); });
        await act(async () => { (findButton() || staleButton)!.click(); });
        expect(picks).toEqual([]);
    } finally { resetRemoteUserDataSync(); await f.dispose(); }
});

test("character library excludes deleting character cards", async () => {
    const f = await fixture();
    try {
        apiClient.defaults.adapter = async (config) => ({ config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data: { characters: [{ asset: { ...asset, status: "deleting" }, character: { visualStatus: "ready", voiceStatus: "ready" } }], total: 1 }, msg: "ok" } });
        await f.render(<CanvasCharacterLibraryModal open onClose={() => {}} onInsert={() => {}} />);
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 40)); });
        expect(Array.from(document.querySelectorAll("button")).some((button) => button.textContent?.includes("Pending asset"))).toBe(false);
    } finally { await f.dispose(); }
});

test("character media and workspace prevent deleting remote media selection", async () => {
    const f = await fixture();
    const picks: string[] = [];
    const deleting: Asset = { ...asset, kind: "image", status: "deleting", data: { dataUrl: "/api/resources/image/file", storageKey: "resource:image", width: 10, height: 10, bytes: 1, mimeType: "image/png" } };
    try {
        useUserStore.setState({ user: { id: "ui-user" } as never, hydrated: true });
        apiClient.defaults.adapter = async (config) => ({ config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data: { assets: [deleting], total: 1, page: 1, pageSize: 40 }, msg: "ok" } });
        await f.render(<><CharacterImageLibrary currentResourceId="" busyKey="" onPick={(id) => picks.push(id)} onUpload={() => {}} /><CharacterVoiceLibrary currentResourceId="" busyKey="" onPick={(id) => picks.push(id)} onUpload={() => {}} /><CanvasWorkspaceAssetPanel onInsert={async () => { picks.push("insert"); }} onManage={() => {}} /></>);
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 40)); });
        const image = f.host.querySelector<HTMLButtonElement>('button[title="Pending asset"]');
        if (image) await act(async () => image.click());
        const insert = f.host.querySelector<HTMLButtonElement>('button[aria-label="插入画布：Pending asset"]');
        if (insert) await act(async () => insert.click());
        expect(image === null || image.disabled).toBe(true);
        expect(insert === null || insert.disabled).toBe(true);
        expect(picks).toEqual([]);
    } finally { await f.dispose(); }
});

test("management has a discoverable deleting view with server filter and no retry control", async () => {
    const f = await fixture();
    const statuses: string[] = [];
    try {
        useUserStore.setState({ user: { id: "ui-user" } as never, hydrated: true });
        withAssetStorePersistenceSuppressed(() => useAssetStore.setState({ assets: [] }));
        apiClient.defaults.adapter = async (config) => {
            const status = config.params?.status;
            if (status) statuses.push(status);
            const data = config.url === "/asset-folders" ? { folders: [] } : config.url === "/assets" ? { assets: status === "deleting" ? [{ ...asset, status: "deleting" }] : [], total: status === "deleting" ? 1 : 0, page: 1, pageSize: 40 } : { usage: { usedBytes: 0, totalBytes: 1000 } };
            return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data, msg: "ok" } };
        };
        await f.render(<AssetsPage />);
        const entry = Array.from(f.host.querySelectorAll("button")).find((button) => button.textContent?.trim() === "删除中");
        expect(entry).toBeDefined();
        await act(async () => { entry!.click(); await new Promise((resolve) => setTimeout(resolve, 40)); });
        expect(statuses).toContain("deleting");
        expect(f.host.textContent).toContain("删除中，失败将自动重试");
        expect(f.host.textContent).toContain("Pending asset");
        expect(f.host.textContent).not.toContain("重试删除");
    } finally { await f.dispose(); }
});

test("a selected remote-only item cannot be confirmed after its page reports deleting", async () => {
    const f = await fixture();
    let remote: Asset = asset;
    const confirmations: string[][] = [];
    try {
        useUserStore.setState({ user: { id: "ui-user" } as never, hydrated: true });
        withAssetStorePersistenceSuppressed(() => useAssetStore.setState({ assets: [] }));
        apiClient.defaults.adapter = async (config) => ({ config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data: { assets: [remote], total: 1, page: 1, pageSize: 40 }, msg: "ok" } });
        await f.render(<AssetLibraryPickerModal open remoteLibrary acceptRemoteItems mediaKinds={["text"]} items={[]} categoryLabels={{ all: "全部素材", other: "其他" }} onClose={() => {}} onConfirm={(ids) => { confirmations.push(ids); }} />);
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 40)); });
        const pick = document.querySelector<HTMLButtonElement>(".asset-picker-card-action");
        expect(pick).not.toBeNull();
        await act(async () => pick!.click());
        remote = { ...asset, status: "deleting" };
        await act(async () => { await f.client.invalidateQueries({ queryKey: ["asset-picker"] }); });
        const confirm = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("使用已选素材"))!;
        await act(async () => confirm.click());
        expect(confirm.disabled).toBe(true);
        expect(confirmations).toEqual([]);
    } finally { await f.dispose(); }
});
