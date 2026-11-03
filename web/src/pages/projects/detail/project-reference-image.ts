import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey } from "@/services/api/resources";
import type { ProjectAsset } from "@/services/api/projects";
import type { GenerationTask } from "@/services/api/task-center";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";

export type ReferenceSource = "original" | "current" | "none";

export const MAX_PROJECT_ASSET_REFERENCE_IMAGES = 16;

export function assetReferenceImages(asset?: ProjectAsset): Array<{ resourceId: string }> {
    if (!asset) return [];
    if (Array.isArray(asset.referenceImages)) {
        return asset.referenceImages.filter((item) => Boolean(item?.resourceId));
    }
    return asset.referenceImage?.resourceId ? [{ resourceId: asset.referenceImage.resourceId }] : [];
}

export function requireRemoteImage(image: { storageKey?: string; pendingRemoteUpload?: boolean }) {
    const resourceId = resourceIdFromStorageKey(image.storageKey);
    if (image.pendingRemoteUpload || !resourceId) throw new Error("图片仅保存在本地，尚未上传到服务端；请重试上传后继续");
    return resourceId;
}

export function directCharacterRepresentations(resourceId: string, turnaround: boolean) {
    return [{ role: "primary", resourceId }, ...(turnaround ? [{ role: "turnaround_sheet", resourceId }] : [])];
}

export function characterHasTurnaround(asset?: ProjectAsset) {
    return asset?.category === "character" && Boolean(asset.character?.representations.some((item) => item.role === "turnaround_sheet"));
}

export function currentAssetImageId(asset: ProjectAsset) {
    return asset.category === "character"
        ? (asset.character?.representations.find((item) => item.role === "primary")
            || asset.character?.representations.find((item) => item.role === "turnaround_sheet"))?.resourceId || ""
        : resourceIdFromStorageKey(asset.storageKey) || "";
}

export function referenceImagesForAsset(asset: ProjectAsset, source: ReferenceSource): ReferenceImage[] {
    if (source === "none") return [];
    if (source === "current") {
        const resourceId = currentAssetImageId(asset);
        if (!resourceId) throw new Error("当前资产还没有成品图");
        return [{ id: resourceId, name: asset.title, type: "image/*", dataUrl: "", url: resourceFileUrl(resourceId), storageKey: resourceStorageKey(resourceId), role: "reference" }];
    }
    const items = assetReferenceImages(asset);
    if (!items.length) throw new Error("请先保存原始参考图");
    return items.map((item, index) => ({
        id: item.resourceId,
        name: items.length > 1 ? `${asset.title || "参考图"} #${index + 1}` : asset.title,
        type: "image/*",
        dataUrl: "",
        url: resourceFileUrl(item.resourceId),
        storageKey: resourceStorageKey(item.resourceId),
        role: "reference" as const,
    }));
}

export function referenceImageForAsset(asset: ProjectAsset, source: ReferenceSource): ReferenceImage | undefined {
    return referenceImagesForAsset(asset, source)[0];
}

export function referenceModelError(config: AiConfig, model: string, referenceCount: number | boolean) {
    const count = typeof referenceCount === "boolean" ? (referenceCount ? 1 : 0) : referenceCount;
    if (count <= 0) return "";
    // 不使用按协议猜测的默认maxImages；参考输入必须在已配置的模型能力中明确声明。
    const capability = resolveModelChannel(config, model).modelCosts?.find((item) => item.model === modelOptionName(model))?.capabilityConfig?.image;
    const rawMax = capability && Number.isFinite(capability.references?.maxImages) ? capability.references.maxImages : 0;
    if (rawMax < 1) {
        return "当前模型未声明支持参考图，请选择支持参考输入的图片模型，或明确选择无参考";
    }
    const effectiveMax = Math.min(MAX_PROJECT_ASSET_REFERENCE_IMAGES, rawMax);
    if (count > effectiveMax) {
        return `当前模型最多支持 ${effectiveMax} 张参考图，当前已选择 ${count} 张`;
    }
    return "";
}

export function isActiveAssetImageTask(task: Pick<GenerationTask, "projectId" | "status" | "clientContext">, projectId: string, asset: Pick<ProjectAsset, "id" | "category">) {
    const prefix = asset.category === "character" ? "character-turnaround:" : "project-asset-image:";
    return task.projectId === projectId && task.clientContext?.nodeId === `${prefix}${asset.id}` && (task.status === "queued" || task.status === "running");
}

export function nextCandidateCursor(tasks: Array<{ id: string }>) {
    return tasks.length === 50 ? tasks[tasks.length - 1].id : undefined;
}
