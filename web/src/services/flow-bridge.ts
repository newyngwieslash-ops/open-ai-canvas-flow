import type { ShotRevision } from "@/services/api/projects";
import { resourceFileUrl } from "@/services/api/resources";

export const FLOW_BRIDGE_CHANNEL = "yingce-flow-bridge-v1";
export const FLOW_MEDIA_CHUNK_BYTES = 256 * 1024;
export const FLOW_FILE_MAX_BYTES = 128 * 1024 * 1024;
export const FLOW_BUNDLE_MAX_BYTES = 512 * 1024 * 1024;

export type FlowExportMedia = {
    id: string;
    name: string;
    mimeType?: string;
    roles: string[];
    shotIds: string[];
    assetId?: string;
    assetVersionId?: string;
    artifactId?: string;
    status?: string;
    byteLength?: number;
    sha256?: string;
};

export type FlowExportBundle = {
    version: 1;
    id: string;
    createdAt: string;
    source: { origin: string; projectId: string; unitId: string; projectTitle: string; unitTitle: string };
    shots: Array<{
        id: string;
        title: string;
        position: number;
        revisionId: string;
        revision: ShotRevision;
        prompt: string;
        durationMs: number;
        settings: { aspectRatio: string; resolution: string };
        mediaIds: string[];
        warnings: string[];
    }>;
    media: FlowExportMedia[];
};

type BridgeRequest = "PING" | "BEGIN" | "MEDIA_CHUNK" | "COMMIT" | "ABORT";

async function bridgeRequest<T>(type: BridgeRequest, data: unknown, signal?: AbortSignal): Promise<T> {
    signal?.throwIfAborted();
    const origin = window.location.origin;
    if (!/^https?:\/\//.test(origin)) throw new Error("Flow 提交仅支持 HTTP/HTTPS 项目页面");
    const requestId = crypto.randomUUID();
    return new Promise<T>((resolve, reject) => {
        const finish = (error?: Error, value?: T) => {
            clearTimeout(timer);
            window.removeEventListener("message", onMessage);
            signal?.removeEventListener("abort", onAbort);
            if (error) reject(error);
            else resolve(value as T);
        };
        const onAbort = () => finish(new DOMException("Flow 提交已取消", "AbortError"));
        const onMessage = (event: MessageEvent) => {
            if (event.source !== window || event.origin !== origin) return;
            const reply = event.data;
            if (!reply || reply.channel !== FLOW_BRIDGE_CHANNEL || reply.replyTo !== requestId) return;
            if (reply.ok === true) finish(undefined, reply.data as T);
            else if (reply.ok === false) finish(new Error(typeof reply.error === "string" ? reply.error : "Flow 插件拒绝了此次提交"));
        };
        const timer = setTimeout(() => finish(new Error(type === "PING"
            ? "未连接到 Flow 插件，请安装或启用插件、允许访问当前站点后刷新页面"
            : type === "COMMIT"
                ? "Flow 插件提交确认超时，请先在插件中核对是否已收到，避免重复提交"
                : "Flow 插件响应超时，请检查插件状态后重试")), type === "PING" ? 2_000 : 30_000);
        window.addEventListener("message", onMessage);
        signal?.addEventListener("abort", onAbort, { once: true });
        try {
            window.postMessage({ channel: FLOW_BRIDGE_CHANNEL, requestId, type, data }, origin);
        } catch (error) {
            finish(error instanceof Error ? error : new Error("无法连接 Flow 插件"));
        }
    });
}

export function pingFlowExtension(signal?: AbortSignal) {
    return bridgeRequest("PING", {}, signal);
}

async function readMedia(media: FlowExportMedia, remainingBytes: number, signal?: AbortSignal) {
    // Use same-origin credentials so /api/resources/:id/file receives the session cookie,
    // while a 307 redirect to cross-origin OSS/CDN omits credentials and passes wildcard CORS.
    const response = await fetch(resourceFileUrl(media.id), { credentials: "same-origin", signal });
    if (!response.ok) throw new Error(`读取“${media.name}”失败（HTTP ${response.status}），提交未完成`);
    const mimeType = (response.headers.get("content-type") || "").split(";")[0]!.trim().toLowerCase();
    const limit = Math.min(FLOW_FILE_MAX_BYTES, remainingBytes);
    const declaredSize = Number(response.headers.get("content-length") || 0);
    const sizeError = () => new Error(`“${media.name}”超出大小限制：单文件 128 MiB、整批 512 MiB`);
    if (declaredSize > limit) {
        await response.body?.cancel();
        throw sizeError();
    }
    if (!/^(image\/(png|jpeg|webp|gif|avif)|video\/(mp4|webm|quicktime)|audio\/[\w.+-]+)$/.test(mimeType)) {
        await response.body?.cancel();
        throw new Error(`“${media.name}”的文件类型 ${mimeType || "未知"} 不受 Flow 插件支持；请提供 PNG/JPEG/WebP/GIF/AVIF、MP4/WebM/MOV 或音频文件`);
    }
    const reader = response.body?.getReader();
    if (!reader) throw new Error(`“${media.name}”没有可读取的文件内容`);
    const chunks: Uint8Array<ArrayBuffer>[] = [];
    let byteLength = 0;
    try {
        while (true) {
            signal?.throwIfAborted();
            const { done, value } = await reader.read();
            if (done) break;
            byteLength += value.byteLength;
            if (byteLength > limit) throw sizeError();
            chunks.push(new Uint8Array(value));
        }
    } catch (error) {
        await reader.cancel().catch(() => undefined);
        throw error;
    } finally {
        reader.releaseLock();
    }
    if (!byteLength) throw new Error(`“${media.name}”是空文件，提交未完成`);
    const blob = new Blob(chunks, { type: mimeType });
    const digest = await crypto.subtle.digest("SHA-256", await blob.arrayBuffer());
    signal?.throwIfAborted();
    const sha256 = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
    return { blob, descriptor: { ...media, mimeType, byteLength, sha256 } };
}

/** COMMIT only queues a bundle for extension review; it never starts generation. */
export async function submitFlowBundle(bundle: FlowExportBundle, options: { signal?: AbortSignal; onProgress?: (message: string) => void } = {}) {
    const { signal, onProgress } = options;
    if (bundle.source.origin !== window.location.origin) throw new Error("导出来源与当前项目页面不一致");
    if (!bundle.shots.length || bundle.shots.length > 500) throw new Error("每次请提交 1–500 个镜头");
    const ids = [bundle.id, bundle.source.projectId, bundle.source.unitId, ...bundle.shots.flatMap(shot => [shot.id, shot.revisionId]), ...bundle.media.map(media => media.id)];
    if (ids.some(id => !/^[\w-]{1,200}$/.test(id))) throw new Error("镜头包包含无效 ID，请刷新项目后重试");
    if (bundle.media.length > 2000) throw new Error("素材数量超过 2000，请分批提交");
    const mediaIds = new Set(bundle.media.map(media => media.id));
    if (mediaIds.size !== bundle.media.length || bundle.media.some(media => !media.id.trim())) throw new Error("导出资源 ID 无效或重复");
    if (bundle.shots.some(shot => shot.mediaIds.some(id => !mediaIds.has(id)))) throw new Error("镜头引用的文件未包含在导出包中");
    onProgress?.("正在连接 Flow 插件…");
    await pingFlowExtension(signal);
    const files: Blob[] = [];
    const prepared: FlowExportBundle = { ...bundle, media: [] };
    let totalBytes = 0;
    for (const [index, media] of bundle.media.entries()) {
        onProgress?.(`正在读取文件 ${index + 1}/${bundle.media.length}：${media.name}`);
        const { blob, descriptor } = await readMedia(media, FLOW_BUNDLE_MAX_BYTES - totalBytes, signal);
        totalBytes += blob.size;
        prepared.media.push(descriptor);
        files.push(blob);
    }
    let transferId: string | undefined;
    let committing = false;
    try {
        const begin = await bridgeRequest<{ transferId: string }>("BEGIN", { bundle: prepared }, signal);
        if (!begin || typeof begin.transferId !== "string" || !begin.transferId.trim()) throw new Error("Flow 插件没有返回有效的传输编号");
        transferId = begin.transferId;
        for (const [fileIndex, file] of files.entries()) {
            const media = prepared.media[fileIndex]!;
            const total = Math.ceil(file.size / FLOW_MEDIA_CHUNK_BYTES);
            for (let index = 0; index < total; index++) {
                signal?.throwIfAborted();
                onProgress?.(`正在传输 ${fileIndex + 1}/${files.length}：${media.name}（${index + 1}/${total}）`);
                const bytes = new Uint8Array(await file.slice(index * FLOW_MEDIA_CHUNK_BYTES, (index + 1) * FLOW_MEDIA_CHUNK_BYTES).arrayBuffer());
                let binary = "";
                for (let offset = 0; offset < bytes.length; offset += 8_192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8_192));
                await bridgeRequest("MEDIA_CHUNK", { transferId, mediaId: media.id, index, total, data: btoa(binary) }, signal);
            }
        }
        signal?.throwIfAborted();
        onProgress?.("正在提交到插件待确认队列…");
        committing = true;
        await bridgeRequest("COMMIT", { transferId }, signal);
        return { bundleId: prepared.id, shotCount: prepared.shots.length };
    } catch (error) {
        // COMMIT may have succeeded despite a lost ACK; never retry or delete that queue item.
        if (transferId && !committing) await bridgeRequest("ABORT", { transferId }).catch(() => undefined);
        if (committing && signal?.aborted) throw new Error("已停止等待 Flow 插件确认，镜头包可能已收到；请先在插件中核对，避免重复提交");
        throw error;
    }
}
