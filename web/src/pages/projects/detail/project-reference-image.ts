import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey } from "@/services/api/resources";
import type { ProjectAsset } from "@/services/api/projects";
import type { GenerationTask } from "@/services/api/task-center";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";

export type ReferenceSource = "original" | "current" | "none";

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

export function referenceImageForAsset(asset: ProjectAsset, source: ReferenceSource): ReferenceImage | undefined {
    if (source === "none") return undefined;
    const resourceId = source === "original" ? asset.referenceImage?.resourceId : currentAssetImageId(asset);
    if (!resourceId) throw new Error(source === "original" ? "请先保存原始参考图" : "当前资产还没有成品图");
    return { id: resourceId, name: asset.title, type: "image/*", dataUrl: "", url: resourceFileUrl(resourceId), storageKey: resourceStorageKey(resourceId), role: "reference" };
}

export function referenceModelError(config: AiConfig, model: string, hasReference: boolean) {
    if (!hasReference) return "";
    // 不使用按协议猜测的默认maxImages；参考输入必须在已配置的模型能力中明确声明。
    const capability = resolveModelChannel(config, model).modelCosts?.find((item) => item.model === modelOptionName(model))?.capabilityConfig?.image;
    return capability && Number.isFinite(capability.references?.maxImages) && capability.references.maxImages >= 1
        ? ""
        : "当前模型未声明支持参考图，请选择支持参考输入的图片模型，或明确选择无参考";
}

export function isActiveAssetImageTask(task: Pick<GenerationTask, "projectId" | "status" | "clientContext">, projectId: string, asset: Pick<ProjectAsset, "id" | "category">) {
    const prefix = asset.category === "character" ? "character-turnaround:" : "project-asset-image:";
    return task.projectId === projectId && task.clientContext?.nodeId === `${prefix}${asset.id}` && (task.status === "queued" || task.status === "running");
}

export function nextCandidateCursor(tasks: Array<{ id: string }>) {
    return tasks.length === 50 ? tasks[tasks.length - 1].id : undefined;
}
