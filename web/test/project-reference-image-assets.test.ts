import { describe, expect, test } from "bun:test";
import { projectVisualImageRequest } from "../src/pages/projects/detail/project-visual-asset";
import { defaultConfig } from "../src/stores/use-config-store";
import type { ProjectAsset } from "../src/services/api/projects";
import { characterTurnaroundRequest } from "../src/pages/projects/detail/project-character-media";
import { requireRemoteImage, referenceImageForAsset, directCharacterRepresentations, referenceModelError, nextCandidateCursor, isActiveAssetImageTask } from "../src/pages/projects/detail/project-reference-image";

const asset: ProjectAsset = { id: "scene", title: "藏书阁", category: "environment", mediaType: "entity", status: "confirmed", primaryVersionId: "v1", versionCount: 1, usages: [], position: 0, updatedAt: "", visual: { status: "missing", definition: { prompt: "木质书架" } } };

describe("项目参考图手动候选", () => {
    test("本地缓存不是远端上传成功", () => {
        expect(() => requireRemoteImage({ storageKey: "image:local", pendingRemoteUpload: true })).toThrow();
        expect(() => requireRemoteImage({ storageKey: "resource:source", pendingRemoteUpload: true })).toThrow();
        expect(requireRemoteImage({ storageKey: "resource:source" })).toBe("source");
    });
    test("普通照片只写primary，三视图必须明确声明", () => {
        expect(directCharacterRepresentations("image", false)).toEqual([{ role: "primary", resourceId: "image" }]);
        expect(directCharacterRepresentations("image", true).map((item) => item.role)).toEqual(["primary", "turnaround_sheet"]);
    });
    test("原图、当前图、无参考相互独立，缺失来源拒绝", () => {
        const current = { ...asset, referenceImage: { resourceId: "original" }, mediaType: "image", storageKey: "resource:current" };
        expect(referenceImageForAsset(current, "original")?.storageKey).toBe("resource:original");
        expect(referenceImageForAsset(current, "current")?.storageKey).toBe("resource:current");
        expect(referenceImageForAsset(current, "none")).toBeUndefined();
        expect(() => referenceImageForAsset(asset, "original")).toThrow();
        expect(current.referenceImage.resourceId).toBe("original");
    });
    test("未知模型参考能力拒绝，但无参考文生图不受此限制", () => {
        expect(referenceModelError(defaultConfig, "unknown", true)).toBeTruthy();
        expect(referenceModelError(defaultConfig, "unknown", false)).toBe("");
        const configured = { ...defaultConfig, channels: [{ ...defaultConfig.channels[0], id: "explicit", models: ["image"], modelCosts: [{ model: "image", capabilityConfig: { version: 1, image: { references: { maxImages: 1 } } } }] }] } as typeof defaultConfig;
        expect(referenceModelError(configured, "explicit::image", true)).toBe("");
    });
    test("活动任务严格按项目、资产和真实请求nodeId隔离", () => {
        for (const category of ["character", "environment", "prop"] as const) {
            const target = { ...asset, category };
            const request = category === "character"
                ? characterTurnaroundRequest({ projectId: "project", assetId: target.id, versionId: "v1", name: "主角", definition: { appearance: "黑发" }, config: defaultConfig })
                : projectVisualImageRequest({ projectId: "project", asset: target, config: defaultConfig });
            const task = { projectId: "project", status: "running" as const, clientContext: { nodeId: request.nodeId } };
            expect(isActiveAssetImageTask(task, "project", target)).toBe(true);
            expect(isActiveAssetImageTask({ ...task, status: "queued" }, "project", target)).toBe(true);
            expect(isActiveAssetImageTask({ ...task, projectId: "other" }, "project", target)).toBe(false);
            expect(isActiveAssetImageTask({ ...task, projectId: undefined }, "project", target)).toBe(false);
            expect(isActiveAssetImageTask({ ...task, clientContext: { nodeId: `${request.nodeId}-other` } }, "project", target)).toBe(false);
            expect(isActiveAssetImageTask({ ...task, status: "succeeded" }, "project", target)).toBe(false);
            expect(isActiveAssetImageTask({ ...task, clientContext: undefined }, "project", target)).toBe(false);
        }
    });
    test("候选满页继续keyset，空页结束", () => {
        expect(nextCandidateCursor(Array.from({ length: 50 }, (_, index) => ({ id: String(index) })))).toBe("49");
        expect(nextCandidateCursor([{ id: "1" }])).toBeUndefined();
        expect(nextCandidateCursor([])).toBeUndefined();
    });
    test("角色manual仍使用三视图操作且携带版本来源", () => {
        const request = characterTurnaroundRequest({ projectId: "project", assetId: "role", versionId: "v1", name: "主角", definition: { appearance: "黑发" }, config: defaultConfig, adoptionMode: "manual", referenceSource: "none" });
        expect(request.nodeId).toBe("character-turnaround:role");
        expect(request.metadata).toMatchObject({ adoptionMode: "manual", projectAssetVersionId: "v1", referenceSource: "none", characterAssetId: "role" });
    });
    test("manual 请求明确来源和基准版本，不影响缺省自动调用", () => {
        const referenceImages = [{ id: "source", name: "原图", type: "image/png", dataUrl: "", storageKey: "resource:source", role: "reference" as const }];
        const request = projectVisualImageRequest({ projectId: "project", asset, config: defaultConfig, adoptionMode: "manual", referenceSource: "original", referenceImages });
        expect(request.metadata).toMatchObject({ adoptionMode: "manual", referenceSource: "original", projectAssetVersionId: "v1" });
        expect(request.referenceImages).toEqual(referenceImages);
        expect(request.config.count).toBe("1");
        const automatic = projectVisualImageRequest({ projectId: "project", asset, config: defaultConfig });
        expect(automatic.metadata?.adoptionMode).toBeUndefined();
    });
});
