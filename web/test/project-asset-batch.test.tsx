import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { ProjectAsset, ProjectAssetBatchView } from "../src/services/api/projects";
import {
    MAX_PROJECT_ASSET_BATCH_ITEMS,
    buildDefaultBatchDraftItems,
    formatBatchPreflightCostSummary,
    summarizeBatchExecution,
    validateBatchDraftItems,
} from "../src/pages/projects/detail/project-asset-batch";
import { ProjectAssetBatchExecutionList } from "../src/pages/projects/detail/project-asset-batch-modal";

const sampleAssets: ProjectAsset[] = [
    {
        id: "asset-char-1",
        title: "沈青云",
        mediaType: "entity",
        category: "character",
        status: "confirmed",
        primaryVersionId: "ver-char-1",
        versionCount: 2,
        usages: [],
        position: 0,
        updatedAt: "2026-11-02T10:00:00Z",
        referenceImages: [{ resourceId: "res-ref-1" }, { resourceId: "res-ref-2" }],
        character: {
            definition: {
                appearance: "青衫剑客",
                clothing: "玄色劲装",
            },
            representations: [],
        },
    },
    {
        id: "asset-env-1",
        title: "听雨楼",
        mediaType: "entity",
        category: "environment",
        status: "confirmed",
        primaryVersionId: "ver-env-1",
        versionCount: 1,
        usages: [],
        position: 1,
        updatedAt: "2026-11-02T10:00:00Z",
        storageKey: "resource:res-cur-1",
        visual: {
            status: "ready",
            definition: { description: "江南雨夜木结构酒楼" },
        },
    },
    {
        id: "asset-prop-1",
        title: "青霜剑",
        mediaType: "entity",
        category: "prop",
        status: "draft",
        primaryVersionId: "ver-prop-1",
        versionCount: 1,
        usages: [],
        position: 2,
        updatedAt: "2026-11-02T10:00:00Z",
        visual: {
            status: "missing",
            definition: { prompt: "寒光凛冽的古剑特写" },
        },
    },
];

describe("project-asset-batch domain helpers", () => {
    test("buildDefaultBatchDraftItems selects cross-category visual assets, default reference source, and operation", () => {
        const drafts = buildDefaultBatchDraftItems(sampleAssets);
        expect(drafts).toHaveLength(3);

        expect(drafts[0]).toMatchObject({
            assetId: "asset-char-1",
            expectedVersionId: "ver-char-1",
            referenceSource: "original",
            operation: "character_turnaround",
        });
        expect(drafts[0].prompt).toContain("沈青云");

        expect(drafts[1]).toMatchObject({
            assetId: "asset-env-1",
            expectedVersionId: "ver-env-1",
            referenceSource: "current",
            operation: "project_asset_image",
        });

        expect(drafts[2]).toMatchObject({
            assetId: "asset-prop-1",
            expectedVersionId: "ver-prop-1",
            referenceSource: "none",
            operation: "project_asset_image",
        });
        expect(drafts[2].prompt).toContain("寒光凛冽的古剑特写");
    });

    test("validateBatchDraftItems rejects empty list, duplicate assetId, missing version, or >50 items", () => {
        expect(validateBatchDraftItems([])).toContain("至少选择 1 个资产");

        const dup = buildDefaultBatchDraftItems(sampleAssets);
        dup.push({ ...dup[0] });
        expect(validateBatchDraftItems(dup)).toContain("不能包含重复资产");

        const many = Array.from({ length: MAX_PROJECT_ASSET_BATCH_ITEMS + 1 }, (_, idx) => ({
            assetId: `asset-${idx}`,
            assetName: `资产${idx}`,
            assetCategory: "prop",
            expectedVersionId: `ver-${idx}`,
            referenceSource: "none" as const,
            prompt: "测试提示词",
            operation: "project_asset_image",
        }));
        expect(validateBatchDraftItems(many)).toContain("50");
    });

    test("formatBatchPreflightCostSummary formats known microcredits and warns on priceUnknownCount", () => {
        const summary = formatBatchPreflightCostSummary({
            totalKnownMicrocredits: 2_500_000,
            priceUnknownCount: 1,
            totalCount: 3,
        });
        expect(summary.knownCreditsLabel).toBe("2.5");
        expect(summary.hasUnknownPrice).toBe(true);
        expect(summary.warningText).toContain("1 项");
    });

    test("summarizeBatchExecution distinguishes stop unsubmitted vs cancel active vs retry vs reconcile_required", () => {
        const batch: ProjectAssetBatchView = {
            id: "batch-1",
            projectId: "proj-1",
            status: "running",
            model: "gemini-2.5-flash-image",
            provider: "custom",
            totalCount: 5,
            totalKnownMicrocredits: 1_000_000,
            priceUnknownCount: 0,
            createdAt: "2026-11-02T10:00:00Z",
            updatedAt: "2026-11-02T10:01:00Z",
            items: [
                {
                    id: "item-1",
                    batchId: "batch-1",
                    position: 0,
                    assetId: "asset-char-1",
                    assetName: "沈青云",
                    assetCategory: "character",
                    baseVersionId: "ver-char-1",
                    referenceSource: "original",
                    status: "succeeded",
                    taskId: "task-1",
                    attempt: 1,
                    knownMicrocredits: 500_000,
                    priceUnknown: false,
                    canRetry: false,
                    outputUrl: "/api/resources/res-out-1/file",
                    candidate: {
                        assetId: "asset-char-1",
                        baseVersionId: "ver-char-1",
                        state: "pending_confirmation",
                    },
                    updatedAt: "2026-11-02T10:01:00Z",
                },
                {
                    id: "item-2",
                    batchId: "batch-1",
                    position: 1,
                    assetId: "asset-env-1",
                    assetName: "听雨楼",
                    assetCategory: "environment",
                    baseVersionId: "ver-env-1",
                    referenceSource: "current",
                    status: "running",
                    taskId: "task-2",
                    attempt: 1,
                    knownMicrocredits: 500_000,
                    priceUnknown: false,
                    canRetry: false,
                    updatedAt: "2026-11-02T10:01:00Z",
                },
                {
                    id: "item-3",
                    batchId: "batch-1",
                    position: 2,
                    assetId: "asset-prop-1",
                    assetName: "青霜剑",
                    assetCategory: "prop",
                    baseVersionId: "ver-prop-1",
                    referenceSource: "none",
                    status: "pending",
                    attempt: 1,
                    knownMicrocredits: 0,
                    priceUnknown: true,
                    canRetry: false,
                    updatedAt: "2026-11-02T10:01:00Z",
                },
                {
                    id: "item-4",
                    batchId: "batch-1",
                    position: 3,
                    assetId: "asset-prop-2",
                    assetName: "玉笛",
                    assetCategory: "prop",
                    baseVersionId: "ver-prop-2",
                    referenceSource: "none",
                    status: "failed",
                    taskId: "task-4",
                    attempt: 1,
                    knownMicrocredits: 0,
                    priceUnknown: true,
                    errorReason: "task_failed",
                    errorMessage: "上游生成超时",
                    canRetry: true,
                    updatedAt: "2026-11-02T10:01:00Z",
                },
                {
                    id: "item-5",
                    batchId: "batch-1",
                    position: 4,
                    assetId: "asset-prop-3",
                    assetName: "令牌",
                    assetCategory: "prop",
                    baseVersionId: "ver-prop-3",
                    referenceSource: "none",
                    status: "reconcile_required",
                    taskId: "task-5",
                    attempt: 1,
                    knownMicrocredits: 0,
                    priceUnknown: true,
                    errorReason: "media_recovery_pending",
                    errorMessage: "任务存在待恢复媒体检查点或未决结果，请先在任务中心恢复或核对账单",
                    canRetry: false,
                    updatedAt: "2026-11-02T10:01:00Z",
                },
            ],
        };

        const stats = summarizeBatchExecution(batch);
        expect(stats.pendingCount).toBe(1);
        expect(stats.runningCount).toBe(1);
        expect(stats.succeededCount).toBe(1);
        expect(stats.failedCount).toBe(1);
        expect(stats.reconcileRequiredCount).toBe(1);
        expect(stats.canStopUnsubmitted).toBe(true);
        expect(stats.canCancelActiveTasks).toBe(true);
        expect(stats.hasPollingItems).toBe(true);

        const html = renderToStaticMarkup(
            <ProjectAssetBatchExecutionList
                batch={batch}
                adoptingTaskId=""
                retryingItemId=""
                stopping={false}
                cancelling={false}
                onStopUnsubmitted={() => {}}
                onCancelActiveTasks={() => {}}
                onRetryItem={() => {}}
                onAdoptCandidate={() => {}}
            />,
        );
        expect(html).toContain("停止未提交项");
        expect(html).toContain("请求取消活动任务");
        expect(html).toContain("采用此候选");
        expect(html).toContain("重试此项");
        expect(html).toContain("需核对账单/恢复");
        expect(html).not.toContain("一键全部采用");

        const adoptedBatch: ProjectAssetBatchView = {
            ...batch,
            status: "completed",
            items: [
                {
                    ...batch.items[0],
                    candidate: {
                        assetId: "asset-char-1",
                        baseVersionId: "ver-char-1",
                        state: "adopted",
                        adoptedVersionId: "ver-char-2",
                    },
                },
            ],
        };
        const adoptedHtml = renderToStaticMarkup(
            <ProjectAssetBatchExecutionList
                batch={adoptedBatch}
                adoptingTaskId=""
                retryingItemId=""
                stopping={false}
                cancelling={false}
                onStopUnsubmitted={() => {}}
                onCancelActiveTasks={() => {}}
                onRetryItem={() => {}}
                onAdoptCandidate={() => {}}
            />,
        );
        expect(adoptedHtml).toContain("已采用为当前成品");
        expect(adoptedHtml).not.toContain("采用此候选");
    });
});
