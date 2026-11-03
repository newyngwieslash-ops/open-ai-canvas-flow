import { afterEach, describe, expect, test } from "bun:test";
import { decodeChunk, validateBundle } from "../../extensions/yingce-flow/core.mjs";
import {
    FLOW_BRIDGE_CHANNEL, FLOW_FILE_MAX_BYTES, FLOW_MEDIA_CHUNK_BYTES,
    pingFlowExtension, submitFlowBundle, type FlowExportBundle,
} from "../src/services/flow-bridge";

const origin = "http://localhost:5178";
const originalWindow = globalThis.window;
const originalFetch = globalThis.fetch;
const originalSetTimeout = globalThis.setTimeout;

type Request = { channel: string; requestId: string; type: string; data: any };
class ExtensionWindow {
    location = { origin };
    listeners = new Set<(event: MessageEvent) => void>();
    requests: Request[] = [];
    onRequest: (request: Request) => void = request => this.reply(request, { ok: true, data: { transferId: "transfer-1" } });
    addEventListener(_name: string, listener: (event: MessageEvent) => void) { this.listeners.add(listener); }
    removeEventListener(_name: string, listener: (event: MessageEvent) => void) { this.listeners.delete(listener); }
    postMessage(request: Request, targetOrigin: string) {
        expect(targetOrigin).toBe(origin);
        expect(request.channel).toBe(FLOW_BRIDGE_CHANNEL);
        expect(request.requestId).toMatch(/^[\da-f-]{36}$/);
        this.requests.push(request);
        queueMicrotask(() => this.onRequest(request));
    }
    reply(request: Request, fields: object, overrides: object = {}) {
        const event = { source: this, origin, data: { channel: FLOW_BRIDGE_CHANNEL, replyTo: request.requestId, ...fields }, ...overrides } as unknown as MessageEvent;
        for (const listener of this.listeners) listener(event);
    }
}

function extension() {
    const target = new ExtensionWindow();
    Object.defineProperty(globalThis, "window", { configurable: true, writable: true, value: target });
    return target;
}

function bundle(media = true): FlowExportBundle {
    return {
        version: 1, id: crypto.randomUUID(), createdAt: new Date().toISOString(),
        source: { origin, projectId: "project-1", unitId: "unit-1", projectTitle: "短剧", unitTitle: "第一章" },
        shots: [{
            id: "shot-1", title: "镜头01", position: 0, revisionId: "revision-1", durationMs: 4000,
            revision: {
                id: "revision-1", shotId: "shot-1", version: 1, durationMs: 4000,
                plotDescription: "递碗", action: "动作与节拍", dialogue: "喝粥", shotSize: "中景", cameraAngle: "平视", cameraMovement: "缓推",
                imagePrompt: "递碗", videoPrompt: "递碗后坐下", negativePrompt: "", continuityNotes: "", actionBeatsJson: "[]", createdAt: "2026-11-01T00:00:00Z",
            },
            prompt: "递碗后坐下，文件“陶碗”", settings: { aspectRatio: "16:9", resolution: "720" }, mediaIds: media ? ["image-1"] : [], warnings: [],
        }],
        media: media ? [{ id: "image-1", name: "陶碗", roles: ["reference"], shotIds: ["shot-1"] }] : [],
    };
}

afterEach(() => {
    if (originalWindow === undefined) Reflect.deleteProperty(globalThis, "window");
    else Object.defineProperty(globalThis, "window", { configurable: true, writable: true, value: originalWindow });
    globalThis.fetch = originalFetch;
    globalThis.setTimeout = originalSetTimeout;
});

describe("Flow project bridge", () => {
    test("accepts ACK without a type field, only from this window, origin, channel and request", async () => {
        const target = extension();
        target.onRequest = request => {
            target.reply(request, { ok: false, error: "wrong source" }, { source: {} });
            target.reply(request, { ok: false, error: "wrong origin" }, { origin: "https://untrusted.example" });
            target.reply(request, { ok: false, channel: "other", error: "wrong channel" });
            target.reply(request, { ok: false, replyTo: "another-request", error: "wrong request" });
            target.reply(request, { ok: true, data: { connected: true } });
        };
        expect(await pingFlowExtension()).toEqual({ connected: true });
        expect(target.listeners.size).toBe(0);
    });

    test("PING times out at 2 seconds and releases the listener", async () => {
        const target = extension();
        target.onRequest = () => {};
        let timeout = 0;
        globalThis.setTimeout = ((callback: () => void, ms: number) => {
            timeout = ms;
            return originalSetTimeout(callback, 1);
        }) as typeof setTimeout;
        await expect(pingFlowExtension()).rejects.toThrow("未连接到 Flow 插件");
        expect(timeout).toBe(2_000);
        expect(target.listeners.size).toBe(0);
    });

    test("transfers original bytes with SHA-256 and matches the extension's actual validator and chunk decoder", async () => {
        const target = extension();
        const source = bundle();
        const data = new Uint8Array(FLOW_MEDIA_CHUNK_BYTES * 2 + 17).map((_, index) => index % 251);
        const controller = new AbortController();
        let fetchCount = 0;
        globalThis.fetch = (async (url, init) => {
            fetchCount++;
            expect(String(url)).toEndWith("/resources/image-1/file");
            expect(init?.credentials).toBe("include");
            expect(init?.signal).toBe(controller.signal);
            return new Response(data, { headers: { "content-type": "image/png; charset=binary" } });
        }) as typeof fetch;
        let received: ReturnType<typeof validateBundle>;
        const receivedChunks: Uint8Array[] = [];
        target.onRequest = request => {
            if (request.type === "BEGIN") received = validateBundle(request.data.bundle, origin);
            if (request.type === "MEDIA_CHUNK") {
                expect(request.data.transferId).toBe("transfer-1");
                expect(request.data.mediaId).toBe("image-1");
                receivedChunks.push(decodeChunk(request.data.data, received.media[0], request.data.index, request.data.total));
            }
            target.reply(request, { ok: true, data: request.type === "BEGIN" ? { transferId: "transfer-1" } : {} });
        };
        expect(await submitFlowBundle(source, { signal: controller.signal })).toEqual({ bundleId: source.id, shotCount: 1 });
        expect(fetchCount).toBe(1);
        expect(received!.media[0].mimeType).toBe("image/png");
        expect(received!.media[0].byteLength).toBe(data.length);
        expect(received!.media[0].sha256).toBe(new Bun.CryptoHasher("sha256").update(data).digest("hex"));
        expect(new Uint8Array(await new Blob(receivedChunks).arrayBuffer())).toEqual(data);
        expect(receivedChunks.map(chunk => chunk.length)).toEqual([FLOW_MEDIA_CHUNK_BYTES, FLOW_MEDIA_CHUNK_BYTES, 17]);
        expect(target.requests.map(request => request.type)).toEqual(["PING", "BEGIN", "MEDIA_CHUNK", "MEDIA_CHUNK", "MEDIA_CHUNK", "COMMIT"]);
        expect(source.media[0].sha256).toBeUndefined();
        expect(JSON.stringify(received!)).not.toContain("/resources/");
        expect(target.listeners.size).toBe(0);
    });

    test("a rejected chunk aborts the transfer and never commits or retries it", async () => {
        const target = extension();
        globalThis.fetch = (async () => new Response("image", { headers: { "content-type": "image/webp" } })) as typeof fetch;
        target.onRequest = request => target.reply(request, request.type === "MEDIA_CHUNK"
            ? { ok: false, error: "磁盘空间不足" }
            : { ok: true, data: { transferId: "transfer-1" } });
        await expect(submitFlowBundle(bundle())).rejects.toThrow("磁盘空间不足");
        expect(target.requests.map(request => request.type)).toEqual(["PING", "BEGIN", "MEDIA_CHUNK", "ABORT"]);
        expect(target.listeners.size).toBe(0);
    });

    test("cancelling during transfer aborts the extension staging item", async () => {
        const target = extension();
        const controller = new AbortController();
        globalThis.fetch = (async () => new Response("image", { headers: { "content-type": "image/jpeg" } })) as typeof fetch;
        target.onRequest = request => {
            if (request.type === "MEDIA_CHUNK") controller.abort();
            else target.reply(request, { ok: true, data: { transferId: "transfer-1" } });
        };
        await expect(submitFlowBundle(bundle(), { signal: controller.signal })).rejects.toHaveProperty("name", "AbortError");
        expect(target.requests.at(-1)?.type).toBe("ABORT");
        expect(target.listeners.size).toBe(0);
    });

    test("an uncertain COMMIT uses the 30-second limit and is never repeated or deleted", async () => {
        const target = extension();
        const timeouts: number[] = [];
        globalThis.setTimeout = ((callback: () => void, ms: number) => {
            timeouts.push(ms);
            return originalSetTimeout(callback, 2);
        }) as typeof setTimeout;
        target.onRequest = request => {
            if (request.type !== "COMMIT") target.reply(request, { ok: true, data: { transferId: "transfer-1" } });
        };
        await expect(submitFlowBundle(bundle(false))).rejects.toThrow("先在插件中核对");
        expect(timeouts).toEqual([2000, 30000, 30000]);
        expect(target.requests.map(request => request.type)).toEqual(["PING", "BEGIN", "COMMIT"]);
        expect(target.listeners.size).toBe(0);
    });

    test("cancelling after COMMIT reports the uncertainty instead of claiming the queue item was cancelled", async () => {
        const target = extension();
        const controller = new AbortController();
        target.onRequest = request => {
            if (request.type === "COMMIT") controller.abort();
            else target.reply(request, { ok: true, data: { transferId: "transfer-1" } });
        };
        await expect(submitFlowBundle(bundle(false), { signal: controller.signal })).rejects.toThrow("镜头包可能已收到");
        expect(target.requests.map(request => request.type)).toEqual(["PING", "BEGIN", "COMMIT"]);
        expect(target.listeners.size).toBe(0);
    });

    test("HTTP errors, disallowed MIME and oversized files cannot create a partial batch", async () => {
        for (const [response, error] of [
            [new Response("unauthorized", { status: 401 }), "HTTP 401"],
            [new Response("login", { headers: { "content-type": "text/html" } }), "不受 Flow 插件支持"],
            [new Response("svg", { headers: { "content-type": "image/svg+xml" } }), "不受 Flow 插件支持"],
            [new Response("image", { headers: { "content-type": "image/png", "content-length": String(FLOW_FILE_MAX_BYTES + 1) } }), "大小限制"],
        ] as const) {
            const target = extension();
            globalThis.fetch = (async () => response) as typeof fetch;
            await expect(submitFlowBundle(bundle())).rejects.toThrow(error);
            expect(target.requests.map(request => request.type)).toEqual(["PING"]);
            expect(target.listeners.size).toBe(0);
        }
    });

    test("rejects foreign origins, invalid IDs and missing binary references before contacting the extension", async () => {
        const target = extension();
        const foreign = bundle();
        foreign.source.origin = "https://another.example";
        await expect(submitFlowBundle(foreign)).rejects.toThrow("来源");
        const invalid = bundle();
        invalid.media[0].id = "https://remote.example/file?secret=yes";
        await expect(submitFlowBundle(invalid)).rejects.toThrow("无效 ID");
        const missing = bundle();
        missing.media = [];
        await expect(submitFlowBundle(missing)).rejects.toThrow("未包含");
        expect(target.requests).toHaveLength(0);
    });
});
