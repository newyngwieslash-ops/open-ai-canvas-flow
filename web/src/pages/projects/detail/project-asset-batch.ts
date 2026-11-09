import type {
    PreflightProjectAssetBatchRequest,
    ProjectAsset,
    ProjectAssetBatchItemInput,
    ProjectAssetBatchReferenceSource,
    ProjectAssetBatchView,
} from "@/services/api/projects";
import { backendProviderConfig } from "@/services/api/generation-task";
import { logicalModelIDForConfig, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";
import { currentAssetImageId } from "./project-reference-image";
import { textValue } from "./shared";

export const MAX_PROJECT_ASSET_BATCH_ITEMS = 50;

export type BatchDraftItem = ProjectAssetBatchItemInput & {
    assetName: string;
    assetCategory: string;
    referenceCount: number;
    hasCurrentImage: boolean;
};

export function buildProjectAssetBatchRequest(
    config: AiConfig,
    routedModel: string,
    items: BatchDraftItem[],
): PreflightProjectAssetBatchRequest {
    const generationConfig: AiConfig = { ...config, model: routedModel, imageModel: routedModel, count: "1" };
    const requestConfig = resolveModelRequestConfig(generationConfig, routedModel);
    const logicalModelId = logicalModelIDForConfig(generationConfig);
    return {
        provider: logicalModelId ? "managed" : requestConfig.channelId ? "system" : "custom",
        model: requestConfig.model || routedModel,
        logicalModelId: logicalModelId || undefined,
        // 与画布生成共用后端 Provider 配置：带上模型能力合同和图片参数，只含当前渠道凭据。
        config: backendProviderConfig(generationConfig, "image") as Record<string, unknown>,
        items: items.map((item) => ({
            assetId: item.assetId,
            expectedVersionId: item.expectedVersionId,
            referenceSource: item.referenceSource,
            prompt: item.prompt.trim(),
            operation: item.operation,
        })),
    };
}

export function isBatchEligibleProjectAsset(asset: ProjectAsset): boolean {
    if (!asset.primaryVersionId) return false;
    return (
        asset.category === "character" ||
        asset.category === "environment" ||
        asset.category === "prop"
    );
}

export function defaultReferenceSourceForProjectAsset(
    asset: ProjectAsset,
): ProjectAssetBatchReferenceSource {
    const refCount = asset.referenceImages?.length ?? 0;
    if (refCount > 0) return "original";
    if (currentAssetImageId(asset)) return "current";
    return "none";
}

export function defaultPromptForProjectAsset(asset: ProjectAsset): string {
    if (asset.category === "character") {
        const def = asset.character?.definition;
        const parts = [
            asset.title,
            textValue(def?.appearance),
            textValue(def?.physique),
            textValue(def?.clothing),
            textValue(def?.props),
        ]
            .map((v) => v.trim())
            .filter(Boolean);
        return parts.length > 0 ? `${parts.join("，")}，角色三视图设定` : `${asset.title}，角色三视图设定`;
    }
    const def = asset.visual?.definition;
    const explicitPrompt = textValue(def?.prompt);
    if (explicitPrompt) return explicitPrompt;
    const desc = textValue(def?.description) || (asset.previewText ?? "").trim();
    if (desc) return `${asset.title}，${desc}`;
    return asset.title.trim();
}

export function buildDefaultBatchDraftItems(assets: ProjectAsset[]): BatchDraftItem[] {
    const seen = new Set<string>();
    const out: BatchDraftItem[] = [];
    for (const asset of assets) {
        if (!isBatchEligibleProjectAsset(asset)) continue;
        if (seen.has(asset.id)) continue;
        seen.add(asset.id);
        const refCount = asset.referenceImages?.length ?? 0;
        const hasCurrentImage = Boolean(currentAssetImageId(asset));
        out.push({
            assetId: asset.id,
            assetName: asset.title,
            assetCategory: asset.category,
            expectedVersionId: asset.primaryVersionId || "",
            referenceSource: defaultReferenceSourceForProjectAsset(asset),
            prompt: defaultPromptForProjectAsset(asset),
            operation:
                asset.category === "character"
                    ? "character_turnaround"
                    : "project_asset_image",
            referenceCount: refCount,
            hasCurrentImage,
        });
        if (out.length >= MAX_PROJECT_ASSET_BATCH_ITEMS) break;
    }
    return out;
}

export function validateBatchDraftItems(items: BatchDraftItem[]): string | null {
    if (items.length === 0) {
        return "请至少选择 1 个资产进行批量生成";
    }
    if (items.length > MAX_PROJECT_ASSET_BATCH_ITEMS) {
        return `单次批量生成最多支持 ${MAX_PROJECT_ASSET_BATCH_ITEMS} 个资产`;
    }
    const seen = new Set<string>();
    for (const item of items) {
        if (!item.assetId.trim()) {
            return "存在无效的资产 ID";
        }
        if (seen.has(item.assetId)) {
            return `批次中不能包含重复资产（${item.assetName || item.assetId}）`;
        }
        seen.add(item.assetId);
        if (!item.expectedVersionId.trim()) {
            return `资产「${item.assetName || item.assetId}」缺少当前主版本标识，请刷新后重试`;
        }
        if (!item.prompt.trim()) {
            return `资产「${item.assetName || item.assetId}」的生成提示词不能为空`;
        }
    }
    return null;
}

export function formatBatchPreflightCostSummary(input: {
    totalKnownMicrocredits: number;
    priceUnknownCount: number;
    totalCount: number;
}): {
    knownCreditsLabel: string;
    hasUnknownPrice: boolean;
    warningText: string;
} {
    const credits = Math.max(0, input.totalKnownMicrocredits) / 1_000_000;
    const knownCreditsLabel = Number.isInteger(credits)
        ? String(credits)
        : credits.toFixed(2).replace(/\.?0+$/, "");
    const hasUnknownPrice = input.priceUnknownCount > 0;
    const warningText = hasUnknownPrice
        ? `含 ${input.priceUnknownCount} 项自定义渠道或动态计价任务，实际消耗以任务执行结算为准`
        : "";
    return {
        knownCreditsLabel,
        hasUnknownPrice,
        warningText,
    };
}

export function summarizeBatchExecution(batch: ProjectAssetBatchView) {
    let pendingCount = 0;
    let runningCount = 0;
    let cancelRequestedCount = 0;
    let succeededCount = 0;
    let failedCount = 0;
    let stoppedCount = 0;
    let cancelledCount = 0;
    let reconcileRequiredCount = 0;

    for (const item of batch.items) {
        switch (item.status) {
            case "pending":
                pendingCount++;
                break;
            case "running":
                runningCount++;
                break;
            case "cancel_requested":
                cancelRequestedCount++;
                break;
            case "succeeded":
                succeededCount++;
                break;
            case "failed":
                failedCount++;
                break;
            case "stopped":
                stoppedCount++;
                break;
            case "cancelled":
                cancelledCount++;
                break;
            case "reconcile_required":
                reconcileRequiredCount++;
                break;
        }
    }

    const canStopUnsubmitted = pendingCount > 0;
    const canCancelActiveTasks = runningCount > 0 || cancelRequestedCount > 0;
    const hasPollingItems =
        pendingCount > 0 || runningCount > 0 || cancelRequestedCount > 0;

    return {
        pendingCount,
        runningCount,
        cancelRequestedCount,
        succeededCount,
        failedCount,
        stoppedCount,
        cancelledCount,
        reconcileRequiredCount,
        canStopUnsubmitted,
        canCancelActiveTasks,
        hasPollingItems,
    };
}
