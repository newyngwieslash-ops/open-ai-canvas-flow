import { describe, expect, test } from "bun:test";
import { buildWorkflowImageInput, prepareWorkflowImageSubmission } from "../src/pages/projects/detail/workflow-image-generation";
import { makeBatchArtifact, makeImageConfig, makeWorkflowBatchDetail } from "./helpers/workflow-batch-fixture";

describe("workflow-image-generation", () => {
    test("previz preserves image semantics and saved revision without reading storyboard artifact", async () => {
        const detail = makeWorkflowBatchDetail();
        detail.shotArtifacts = [makeBatchArtifact("s1", "ready", "storyboard")];
        const shot = detail.shots[0]!;
        const revision = detail.shotRevisions[0]!;
        const config = makeImageConfig();
        const options = await prepareWorkflowImageSubmission({
            detail, shot, revision, stage: "previz",
            settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
            config,
        }, { workflowStepId: "step-previz", skills: [], batch: { batchId: "batch-1", itemId: "item-1" } });
        expect(options.mode).toBe("image");
        expect(options.prompt).toContain("黑白分镜草图，清晰动作节拍，电影构图");
        expect(options.referenceAudios).toEqual([]);
        expect(options.referenceImages).toEqual([]);
        expect(options.metadata?.shotRevisionId).toBe(revision.id);
        expect(options.metadata?.artifactType).toBe("action_board");
        expect(options.metadata?.chapterImageBatchId).toBe("batch-1");
        expect(options.metadata?.chapterImageBatchItemId).toBe("item-1");
    });

    test("isolates prompt, bound asset references, and metadata per shot", async () => {
        const detail = makeWorkflowBatchDetail();
        detail.assets = [
            {
                id: "char-1", title: "角色甲", category: "character", mediaType: "entity",
                primaryVersionId: "cv-1", status: "confirmed", versionCount: 1,
                storageKey: "", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
                character: {
                    versionId: "cv-1",
                    definition: {},
                    representations: [{ id: "rep-1", resourceId: "res-char-1", mediaType: "image/png", role: "primary" }],
                },
            },
            {
                id: "scene-2", title: "竹林", category: "environment", mediaType: "image",
                primaryVersionId: "sv-2", status: "confirmed", versionCount: 1,
                storageKey: "resource:res-scene-2", createdAt: "2026-10-29T00:00:00.000Z", updatedAt: "2026-10-29T00:00:00.000Z",
            },
        ];
        detail.shotReferences = [
            {
                id: "ref-1", projectId: "p1", shotId: "s1", assetVersionId: "cv-1",
                role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z",
            },
            {
                id: "ref-2", projectId: "p1", shotId: "s2", assetVersionId: "sv-2",
                role: "reference", status: "linked", createdAt: "2026-10-29T00:00:00.000Z",
            },
        ];
        detail.shotRevisions[0]!.imagePrompt = "@[asset:char-1] 拔剑特写";
        detail.shotRevisions[1]!.imagePrompt = "风吹 @[asset:scene-2] 远景";

        const config = makeImageConfig();
        const settings = { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] };

        const first = await prepareWorkflowImageSubmission({
            detail, shot: detail.shots[0]!, revision: detail.shotRevisions[0]!,
            stage: "storyboard", settings, config,
        }, { workflowStepId: "step-storyboard", skills: [] });

        const second = await prepareWorkflowImageSubmission({
            detail, shot: detail.shots[1]!, revision: detail.shotRevisions[1]!,
            stage: "storyboard", settings, config,
        }, { workflowStepId: "step-storyboard", skills: [] });

        expect(first.prompt).toContain("图片1 拔剑特写");
        expect(first.prompt).toContain("角色甲");
        expect(first.prompt).not.toContain("竹林");
        expect(first.referenceImages?.map(item => item.storageKey)).toEqual(["resource:res-char-1"]);
        expect(first.metadata?.shotId).toBe("s1");
        expect(first.metadata?.shotRevisionId).toBe("r1");
        expect(first.metadata?.resolvedCharacterVersions).toEqual([{ assetId: "char-1", versionId: "cv-1" }]);

        expect(second.prompt).toContain("风吹 图片1 远景");
        expect(second.prompt).toContain("竹林");
        expect(second.prompt).not.toContain("角色甲");
        expect(second.referenceImages?.map(item => item.storageKey)).toEqual(["resource:res-scene-2"]);
        expect(second.metadata?.shotId).toBe("s2");
        expect(second.metadata?.shotRevisionId).toBe("r2");
        expect(second.metadata?.resolvedCharacterVersions).toEqual([]);
    });

    test("rejects mismatched revision, missing content, or unresolved asset mention", () => {
        const detail = makeWorkflowBatchDetail();
        const config = makeImageConfig();
        const settings = { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] };

        expect(() => buildWorkflowImageInput({
            detail, shot: detail.shots[0]!, revision: detail.shotRevisions[1]!,
            stage: "storyboard", settings, config,
        })).toThrow();

        const emptyRevision = { ...detail.shotRevisions[0]!, imagePrompt: "   ", plotDescription: "" };
        expect(() => buildWorkflowImageInput({
            detail, shot: detail.shots[0]!, revision: emptyRevision,
            stage: "storyboard", settings, config,
        })).toThrow("镜头缺少画面内容");

        const badMentionRevision = { ...detail.shotRevisions[0]!, imagePrompt: "@[asset:missing-asset] 站在门口" };
        expect(() => buildWorkflowImageInput({
            detail, shot: detail.shots[0]!, revision: badMentionRevision,
            stage: "storyboard", settings, config,
        })).toThrow("未绑定到当前镜头");
    });
});
