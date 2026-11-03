import { describe, expect, test } from "bun:test";
import {
    batchArtifactType,
    batchPriceLabel,
    buildBatchPreview,
    classifyBatchCandidates,
    fingerprintBatchShot,
    summarizeBatchPrices,
} from "../src/pages/projects/detail/workflow-batch-plan";
import { makeBatchArtifact, makeBatchTask, makeImageConfig, makeWorkflowBatchDetail } from "./helpers/workflow-batch-fixture";

describe("workflow-batch-plan", () => {
    test("missing scope does not regenerate ready, active, failed or stale shots", () => {
        const detail = makeWorkflowBatchDetail();
        detail.shots.push(
            {
                id: "s3", projectId: "p1", unitId: "u1", currentRevisionId: "r3",
                title: "镜头03", description: "画面3", position: 2,
                durationMs: 3000, status: "draft", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
            {
                id: "s4", projectId: "p1", unitId: "u1", currentRevisionId: "r4",
                title: "镜头04", description: "画面4", position: 3,
                durationMs: 3000, status: "draft", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
        );
        detail.shotRevisions.push(
            { ...detail.shotRevisions[0]!, id: "r3", shotId: "s3", imagePrompt: "画面3" },
            { ...detail.shotRevisions[0]!, id: "r4", shotId: "s4", imagePrompt: "画面4" },
        );
        detail.shotArtifacts = [
            makeBatchArtifact("s1", "ready", "storyboard"),
            makeBatchArtifact("s4", "stale", "storyboard"),
        ];
        detail.tasks = [
            makeBatchTask("s2", "running", "storyboard"),
            makeBatchTask("s3", "failed", "storyboard"),
        ];
        const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "missing" });
        expect(rows.map(row => row.category)).toEqual(["ready", "active", "failed", "stale"]);
        expect(rows.filter(row => row.eligible)).toEqual([]);
    });

    test("storyboard readiness never hides missing action board", () => {
        const detail = makeWorkflowBatchDetail();
        detail.shotArtifacts = [makeBatchArtifact("s1", "ready", "storyboard")];
        const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "previz", scope: "missing" });
        expect(batchArtifactType("previz")).toBe("action_board");
        expect(rows[0]!.category).toBe("missing");
        expect(rows[0]!.eligible).toBe(true);
    });

    test("newer failed task never hides older running task, and ready artifact overrides failed retry", () => {
        const detail = makeWorkflowBatchDetail();
        const olderRunning = {
            ...makeBatchTask("s1", "running", "storyboard"),
            id: "task-s1-running",
            updatedAt: "2026-10-29T00:01:00.000Z",
        };
        const newerFailed = {
            ...makeBatchTask("s1", "failed", "storyboard"),
            id: "task-s1-failed",
            updatedAt: "2026-10-29T00:02:00.000Z",
        };
        const failedOnReadyShot = {
            ...makeBatchTask("s2", "failed", "storyboard"),
            id: "task-s2-failed",
            updatedAt: "2026-10-29T00:03:00.000Z",
        };
        detail.tasks = [olderRunning, newerFailed, failedOnReadyShot];
        detail.shotArtifacts = [makeBatchArtifact("s2", "ready", "storyboard")];

        const failedScopeRows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "failed" });
        expect(failedScopeRows[0]!.category).toBe("active");
        expect(failedScopeRows[0]!.eligible).toBe(false);
        expect(failedScopeRows[1]!.category).toBe("ready");
        expect(failedScopeRows[1]!.eligible).toBe(false);
    });

    test("distinguishes ready without resourceId, old-revision ready as stale, and succeeded without artifact as syncing", () => {
        const detail = makeWorkflowBatchDetail();
        detail.shots.push({
            id: "s3", projectId: "p1", unitId: "u1", currentRevisionId: "r3",
            title: "镜头03", description: "画面3", position: 2,
            durationMs: 3000, status: "draft", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
        });
        detail.shotRevisions.push({ ...detail.shotRevisions[0]!, id: "r3", shotId: "s3", imagePrompt: "画面3" });
        detail.shotArtifacts = [
            { ...makeBatchArtifact("s1", "ready", "storyboard"), resourceId: "" },
            { ...makeBatchArtifact("s2", "ready", "storyboard"), revisionId: "r2-old" },
        ];
        detail.tasks = [makeBatchTask("s3", "succeeded", "storyboard")];

        const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "stale" });
        expect(rows[0]!.category).toBe("missing");
        expect(rows[0]!.eligible).toBe(false);
        expect(rows[1]!.category).toBe("stale");
        expect(rows[1]!.eligible).toBe(true);
        expect(rows[2]!.category).toBe("syncing");
        expect(rows[2]!.eligible).toBe(false);
    });

    test("does not fall back to historical revision when currentRevisionId is missing, and isolates chapters and duplicate titles", () => {
        const detail = makeWorkflowBatchDetail();
        detail.shots[0]!.currentRevisionId = "";
        detail.shots[0]!.title = "同名镜头";
        detail.shots[1]!.title = "同名镜头";
        const otherUnitTask = {
            ...makeBatchTask("s2", "running", "storyboard"),
            id: "task-other-unit",
            inputJson: JSON.stringify({ metadata: { domainProjectId: "p1", unitId: "u2", shotId: "s2", shotRevisionId: "r2", artifactType: "storyboard" } }),
        };
        detail.tasks = [otherUnitTask];

        const rows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "missing" });
        expect(rows[0]!.category).toBe("blocked");
        expect(rows[0]!.eligible).toBe(false);
        expect(rows[1]!.category).toBe("missing");
        expect(rows[1]!.eligible).toBe(true);
    });

    test("failed scope requires retryable failed task on current revision and blocks media recovery or revision mismatch", () => {
        const detail = makeWorkflowBatchDetail();
        const recoverableTask = {
            ...makeBatchTask("s1", "failed", "storyboard"),
            canRecoverMedia: true,
        };
        const staleRevisionFailedTask = {
            ...makeBatchTask("s2", "failed", "storyboard"),
            inputJson: JSON.stringify({ metadata: { domainProjectId: "p1", unitId: "u1", shotId: "s2", shotRevisionId: "r2-old", artifactType: "storyboard" } }),
        };
        detail.tasks = [recoverableTask, staleRevisionFailedTask];

        const blockedRows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "failed" });
        expect(blockedRows[0]!.category).toBe("blocked");
        expect(blockedRows[0]!.eligible).toBe(false);
        expect(blockedRows[0]!.issues.join(" ")).toContain("重试保存");
        expect(blockedRows[1]!.category).toBe("blocked");
        expect(blockedRows[1]!.eligible).toBe(false);

        detail.tasks = [makeBatchTask("s1", "failed", "storyboard")];
        const retryRows = classifyBatchCandidates({ detail, unitId: "u1", stage: "storyboard", scope: "failed" });
        expect(retryRows[0]!.category).toBe("failed");
        expect(retryRows[0]!.eligible).toBe(true);
        expect(retryRows[0]!.taskId).toBe("task-s1-storyboard");
    });

    test("all scopes block unresolved tasks across revisions even behind a newer failed task", async () => {
        const config = makeImageConfig();
        for (const hazard of [
            { canRecoverMedia: true },
            { providerCancelStatus: "requested" },
            { providerCancelStatus: "uncertain" },
            { billing: { status: "uncertain" } },
            { billing: { status: "review_required" } },
        ]) {
            const detail = makeWorkflowBatchDetail();
            detail.shots = detail.shots.slice(0, 1);
            detail.shotArtifacts = [{ ...makeBatchArtifact("s1", "stale", "storyboard"), revisionId: "r0", taskId: "old-success-task" }];
            detail.tasks = [
                { ...makeBatchTask("s1", "failed", "storyboard"), ...hazard, id: "unresolved-task" } as ReturnType<typeof makeBatchTask>,
                { ...makeBatchTask("s1", "failed", "storyboard"), id: "new-task", updatedAt: "2026-10-29T00:02:00Z" },
            ];
            for (const scope of ["missing", "failed", "stale"] as const) {
                const preview = await buildBatchPreview({
                    identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
                    detail, stage: "storyboard", scope, config, skills: [],
                    settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
                });
                expect(preview.candidates[0]!.eligible).toBe(false);
                expect(preview.candidates[0]!.taskId).toBe("unresolved-task");
                expect(preview.items).toEqual([]);
            }
        }
    });

    test("fingerprintBatchShot changes on revision or reference image order change, but ignores other shots and project updatedAt", async () => {
        const detail = makeWorkflowBatchDetail();
        detail.assets = [
            {
                id: "a1", title: "角色A", category: "character", mediaType: "image",
                primaryVersionId: "v1", status: "confirmed", versionCount: 1,
                storageKey: "resource:img-1", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
            {
                id: "a2", title: "场景B", category: "environment", mediaType: "image",
                primaryVersionId: "v2", status: "confirmed", versionCount: 1,
                storageKey: "resource:img-2", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
        ];
        detail.shotReferences = [
            { id: "ref-1", projectId: "p1", shotId: "s1", assetVersionId: "v1", role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z" },
            { id: "ref-2", projectId: "p1", shotId: "s1", assetVersionId: "v2", role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z" },
        ];
        const config = makeImageConfig();
        const settings = { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] };

        const baseHash = await fingerprintBatchShot({
            detail, shot: detail.shots[0]!, revision: detail.shotRevisions[0]!,
            stage: "storyboard", settings, config,
        });

        const detailOtherChanged = structuredClone(detail);
        detailOtherChanged.project.updatedAt = "2026-10-30T00:00:00.000Z";
        detailOtherChanged.shotArtifacts = [makeBatchArtifact("s2", "ready", "storyboard")];
        const sameHash = await fingerprintBatchShot({
            detail: detailOtherChanged, shot: detailOtherChanged.shots[0]!, revision: detailOtherChanged.shotRevisions[0]!,
            stage: "storyboard", settings, config,
        });
        expect(sameHash).toBe(baseHash);

        const detailReorderedRefs = structuredClone(detail);
        detailReorderedRefs.shotReferences = [detail.shotReferences[1]!, detail.shotReferences[0]!];
        const reorderedImagesHash = await fingerprintBatchShot({
            detail: detailReorderedRefs, shot: detailReorderedRefs.shots[0]!, revision: detailReorderedRefs.shotRevisions[0]!,
            stage: "storyboard", settings, config,
        });
        expect(reorderedImagesHash).not.toBe(baseHash);

        const detailPromptChanged = structuredClone(detail);
        detailPromptChanged.shotRevisions[0]!.imagePrompt = "修改后的画面描述";
        const changedPromptHash = await fingerprintBatchShot({
            detail: detailPromptChanged, shot: detailPromptChanged.shots[0]!, revision: detailPromptChanged.shotRevisions[0]!,
            stage: "storyboard", settings, config,
        });
        expect(changedPromptHash).not.toBe(baseHash);
    });

    test("unknown prices are never advertised as free and overflow falls back to unknown", () => {
        expect(summarizeBatchPrices([1000000, null, 0], true)).toEqual({
            knownMicrocredits: 1000000,
            unknownCount: 1,
            estimated: true,
        });
        expect(summarizeBatchPrices([Number.MAX_SAFE_INTEGER, 10], false)).toEqual({
            knownMicrocredits: 0,
            unknownCount: 2,
            estimated: true,
        });
        expect(batchPriceLabel({ knownMicrocredits: 1500000, unknownCount: 1, estimated: true })).toBe(
            "已知部分预计 1.5 积分，另有 1 项费用无法预估",
        );
        expect(batchPriceLabel({ knownMicrocredits: 2000000, unknownCount: 0, estimated: false })).toBe(
            "当前报价合计 2 积分",
        );
    });

    test("buildBatchPreview quotes per item, deduplicates identical requests, warns on missing bound image, and invalidates on settings change", async () => {
        const detail = makeWorkflowBatchDetail();
        detail.assets = [
            {
                id: "a1", title: "角色A", category: "character", mediaType: "image",
                primaryVersionId: "v1", status: "confirmed", versionCount: 1,
                storageKey: "resource:img-1", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
            {
                id: "a2", title: "无图角色", category: "character", mediaType: "entity",
                primaryVersionId: "v2", status: "confirmed", versionCount: 1,
                storageKey: "", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
        ];
        detail.shotReferences = [
            {
                id: "ref-1", projectId: "p1", shotId: "s1", assetVersionId: "v1",
                role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z",
            },
            {
                id: "ref-2", projectId: "p1", shotId: "s2", assetVersionId: "v2",
                role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z",
                referencedVersion: { id: "v2", assetId: "a2", version: 1, representations: [] },
            },
        ];
        const config = makeImageConfig();
        config.channels[0]!.scope = "system";
        config.channels[0]!.modelCosts![0]!.logicalModelId = "logical-img-1";
        let quoteCalls = 0;
        const previewA = await buildBatchPreview({
            identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
            detail,
            stage: "storyboard",
            scope: "missing",
            settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
            config,
            skills: [],
            quoteFetcher: async (req) => {
                quoteCalls += 1;
                const imageCount = req.intent.inputs?.image ?? 0;
                return {
                    quote: {
                        logicalModelId: "logical-img-1",
                        billingMode: "fixed_request",
                        quantity: 1,
                        amountMicrocredits: imageCount > 0 ? 2_000_000 : 1_000_000,
                        estimated: false,
                    },
                };
            },
        });
        // s1 has 1 reference image, s2 has 0 resolved reference images (missing image warning).
        expect(quoteCalls).toBe(2);
        expect(previewA.items).toHaveLength(2);
        expect(previewA.price).toEqual({
            knownMicrocredits: 3_000_000,
            unknownCount: 0,
            estimated: false,
        });
        expect(previewA.candidates[1]!.eligible).toBe(true);
        expect(previewA.candidates[1]!.issues.join(" ")).toContain("暂无参考图");

        const previewB = await buildBatchPreview({
            identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
            detail,
            stage: "storyboard",
            scope: "missing",
            settings: { model: config.imageModel, size: "9:16", quality: "auto", skillIds: [] },
            config,
            skills: [],
        });
        expect(previewB.confirmationToken).not.toBe(previewA.confirmationToken);

        // Missing model marks candidates blocked and yields 0 items
        const emptyModelPreview = await buildBatchPreview({
            identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
            detail,
            stage: "storyboard",
            scope: "missing",
            settings: { model: "", size: "16:9", quality: "auto", skillIds: [] },
            config: { ...config, model: "", imageModel: "" },
            skills: [],
        });
        expect(emptyModelPreview.items).toHaveLength(0);
        expect(emptyModelPreview.candidates.every(c => c.category === "blocked" && !c.eligible)).toBe(true);

        // Aborted signal rejects without returning stale preview
        const abortController = new AbortController();
        abortController.abort();
        await expect(buildBatchPreview({
            identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
            detail,
            stage: "storyboard",
            scope: "missing",
            settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
            config,
            skills: [],
            signal: abortController.signal,
        })).rejects.toThrow("Aborted");
    });
});
