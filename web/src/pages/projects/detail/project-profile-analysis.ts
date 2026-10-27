import { decodeChannelModel, type AiConfig } from "@/stores/use-config-store";
import type { ProfileAnalysisAdoptInput, ProfileAnalysisCategory, ProfileAnalysisField, ProfileAnalysisModelSelection, ProjectAsset } from "@/services/api/projects";
import localforage from "localforage";
import { scopedStorageKey } from "@/lib/user-scope";
import type { ModelRequirements } from "@/lib/model-selection";

export const profileAnalysisRequirements: ModelRequirements = { capability: "text", input: { textCount: 1, imageCount: 1, videoCount: 0, audioCount: 0, characterCount: 0 } };
export const profileFieldLabels: Record<ProfileAnalysisField, string> = { appearance: "外貌", physique: "体型", clothing: "服装", props: "随身道具", description: "描述", prompt: "提示词" };
export const profileAnalysisKey = (userId: string, projectId: string, assetId: string, taskId?: string) => ["project-profile-analysis", userId, projectId, assetId, ...(taskId ? [taskId] : [])];
export const profileAnalysisConfig = (config: AiConfig, model: string): AiConfig => ({ ...config, model, textModel: model });

export type PendingProfileAdoption = { taskId: string; request: ProfileAnalysisAdoptInput };
export function profileAdoptionStorage(userId: string, projectId: string, assetId: string) {
    const key = scopedStorageKey(`profile-adoption:${JSON.stringify([projectId, assetId])}`, userId);
    // Unlike best-effort UI persistence, a failed durable write must prevent POST.
    return {
        load: () => localforage.getItem<PendingProfileAdoption>(key),
        save: (record: PendingProfileAdoption) => localforage.setItem(key, record),
        clear: () => localforage.removeItem(key),
    };
}

export function profileAnalysisModel(config: AiConfig, selected: string): ProfileAnalysisModelSelection | undefined {
    const decoded = decodeChannelModel(selected);
    // Never use resolveModelChannel's first-channel fallback for a paid request.
    const channel = decoded ? config.channels.find((item) => item.id === decoded.channelId) : undefined;
    if (!channel || channel.scope !== "system" || channel.enabled === false || !channel.models.includes(decoded!.model)) return;
    const cost = channel.modelCosts?.find((item) => item.model === decoded!.model);
    const references = cost?.capabilityConfig?.text?.references;
    if (!cost || cost.capability !== "text" || cost.available === false || !references || !Number.isFinite(references.maxImages) || references.maxImages < 1 || !Number.isFinite(references.maxImageBytes) || references.maxImageBytes <= 0) return;
    return cost.logicalModelId ? { kind: "logical", logicalModelId: cost.logicalModelId } : { kind: "system", channelId: channel.id, modelKey: decoded!.model };
}

export function profileAnalysisFields(category: ProfileAnalysisCategory): ProfileAnalysisField[] {
    return category === "character" ? ["appearance", "physique", "clothing", "props"] : ["description", "prompt"];
}
export function validProfileDraft(category: ProfileAnalysisCategory, draft: unknown): draft is Partial<Record<ProfileAnalysisField, string>> {
    if (!draft || typeof draft !== "object" || Array.isArray(draft)) return false;
    const fields = profileAnalysisFields(category);
    const value = draft as Record<string, unknown>;
    return Object.keys(value).length === fields.length && fields.every((field) => typeof value[field] === "string" && Array.from(value[field] as string).length <= 2000 && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value[field] as string));
}
export function profileValue(value: unknown) {
    if (value === undefined) return "未填写";
    return typeof value === "string" ? value || "未填写" : JSON.stringify(value, null, 2);
}
export function profileDefinition(asset: ProjectAsset): Record<string, unknown> {
    return asset.character?.definition || asset.visual?.definition || {};
}
