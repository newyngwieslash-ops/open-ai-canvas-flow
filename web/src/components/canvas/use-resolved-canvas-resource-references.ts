import { useEffect, useMemo, useState } from "react";

import type { CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { loadCanvasDrawingPreview } from "@/lib/canvas/canvas-drawing-storage";
import { getActiveUserScope } from "@/lib/user-scope";
import { resolveImageUrl } from "@/services/image-storage";

type ResolvedPreview = {
    identity: string;
    url: string;
};

const previewPromiseCache = new Map<string, Promise<string>>();

export function canvasReferencePreviewRequested(id: string, requestedIds?: ReadonlySet<string>) {
    return requestedIds === undefined || requestedIds.has(id);
}

export function useResolvedCanvasResourceReferences(
    references: CanvasResourceReference[],
    options?: { projectId?: string; requestedIds?: ReadonlySet<string> },
) {
    const projectId = options?.projectId;
    const requestedIds = options?.requestedIds;
    const requests = useMemo(
        () => references.flatMap((reference) => {
            if (!canvasReferencePreviewRequested(reference.id, requestedIds)) return [];
            const identity = previewIdentity(reference, projectId);
            return identity ? [{ reference, identity }] : [];
        }),
        [projectId, references, requestedIds],
    );
    const [resolvedById, setResolvedById] = useState<Record<string, ResolvedPreview>>({});

    useEffect(() => {
        if (!requests.length) return;
        let cancelled = false;
        void Promise.all(
            requests.map(async ({ reference, identity }) => ({
                id: reference.id,
                identity,
                url: await resolveReferencePreview(reference, identity, projectId),
            })),
        ).then((resolved) => {
            if (cancelled) return;
            setResolvedById((current) => {
                let changed = false;
                const next = { ...current };
                resolved.forEach(({ id, identity, url }) => {
                    if (!url || (current[id]?.identity === identity && current[id]?.url === url)) return;
                    next[id] = { identity, url };
                    changed = true;
                });
                return changed ? next : current;
            });
        });
        return () => {
            cancelled = true;
        };
    }, [projectId, requests]);

    return useMemo(
        () => references.map((reference) => {
            const identity = previewIdentity(reference, projectId);
            const resolved = identity ? resolvedById[reference.id] : undefined;
            return resolved?.identity === identity && resolved.url !== reference.previewUrl ? { ...reference, previewUrl: resolved.url } : reference;
        }),
        [projectId, references, resolvedById],
    );
}

function previewIdentity(reference: CanvasResourceReference, projectId?: string) {
    // 只有当 drawingId 和 projectId 同时存在时才返回绘图身份,避免不完整的缓存键导致运行时错误
    if (reference.drawingId) {
        if (!projectId) return "";
        return `drawing:${projectId}:${reference.drawingId}:${reference.drawingRevision || 0}`;
    }
    const storageKey = reference.kind === "video" ? reference.previewStorageKey : reference.storageKey;
    if (!storageKey || !["image", "video", "character"].includes(reference.kind)) return "";
    return `${reference.kind}:${storageKey}`;
}

function resolveReferencePreview(reference: CanvasResourceReference, identity: string, projectId?: string) {
    const scopeKey = `${getActiveUserScope()}:${identity}`;
    const cached = previewPromiseCache.get(scopeKey);
    if (cached) return cached;
    if (reference.drawingId && projectId) {
        const pending = loadCanvasDrawingPreview(projectId, reference.drawingId)
            .then((preview) => preview ? blobToDataUrl(preview) : reference.previewUrl || "")
            .catch(() => reference.previewUrl || "")
            .finally(() => {
                if (previewPromiseCache.get(scopeKey) === pending) previewPromiseCache.delete(scopeKey);
            });
        previewPromiseCache.set(scopeKey, pending);
        return pending;
    }
    const storageKey = reference.kind === "video" ? reference.previewStorageKey : reference.storageKey;
    const pending = resolveImageUrl(storageKey, reference.previewUrl || "", { cacheMiss: true })
        .catch(() => reference.previewUrl || "")
        .finally(() => {
            if (previewPromiseCache.get(scopeKey) === pending) previewPromiseCache.delete(scopeKey);
        });
    previewPromiseCache.set(scopeKey, pending);
    return pending;
}

function blobToDataUrl(blob: Blob) {
    return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(reader.error || new Error("读取绘图预览失败"));
        reader.onload = () => resolve(typeof reader.result === "string" ? reader.result : "");
        reader.readAsDataURL(blob);
    });
}
