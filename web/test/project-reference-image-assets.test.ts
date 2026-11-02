import { describe, expect, test } from "bun:test";
import { projectVisualImageRequest } from "../src/pages/projects/detail/project-visual-asset";
import { defaultConfig } from "../src/stores/use-config-store";
import type { ProjectAsset } from "../src/services/api/projects";
import { characterTurnaroundRequest } from "../src/pages/projects/detail/project-character-media";
import { characterHasTurnaround, currentAssetImageId, requireRemoteImage, referenceImageForAsset, referenceImagesForAsset, directCharacterRepresentations, referenceModelError, nextCandidateCursor, isActiveAssetImageTask } from "../src/pages/projects/detail/project-reference-image";

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
        const portraitCharacter: ProjectAsset = { ...asset, category: "character", character: { version: 1, visualStatus: "partial", voiceStatus: "missing", definition: {}, representations: directCharacterRepresentations("image", false) } };
        const turnaroundCharacter: ProjectAsset = { ...asset, category: "character", character: { version: 2, visualStatus: "ready", voiceStatus: "missing", definition: {}, representations: directCharacterRepresentations("image", true) } };
        expect(characterHasTurnaround(portraitCharacter)).toBe(false);
        expect(characterHasTurnaround(turnaroundCharacter)).toBe(true);
        expect(currentAssetImageId(portraitCharacter)).toBe("image");
        expect(currentAssetImageId(turnaroundCharacter)).toBe("image");
    });
    test("有序原图、当前图、无参考相互独立，缺失来源拒绝", () => {
        const current = { ...asset, referenceImages: [{ resourceId: "original-2" }, { resourceId: "original-1" }], mediaType: "image", storageKey: "resource:current" };
        expect(referenceImagesForAsset(current, "original").map((item) => item.storageKey)).toEqual(["resource:original-2", "resource:original-1"]);
        expect(referenceImageForAsset(current, "original")?.storageKey).toBe("resource:original-2");
        expect(referenceImagesForAsset(current, "current").map((item) => item.storageKey)).toEqual(["resource:current"]);
        expect(referenceImagesForAsset(current, "none")).toEqual([]);
        expect(() => referenceImagesForAsset(asset, "original")).toThrow();
        expect(current.referenceImages.map((item) => item.resourceId)).toEqual(["original-2", "original-1"]);
    });
    test("未知模型或张数超限拒绝，但无参考文生图不受此限制", () => {
        expect(referenceModelError(defaultConfig, "unknown", true)).toBeTruthy();
        expect(referenceModelError(defaultConfig, "unknown", false)).toBe("");
        expect(referenceModelError(defaultConfig, "unknown", 0)).toBe("");
        const singleModel = { ...defaultConfig, channels: [{ ...defaultConfig.channels[0], id: "explicit", models: ["image"], modelCosts: [{ model: "image", capabilityConfig: { version: 1, image: { references: { maxImages: 1 } } } }] }] } as typeof defaultConfig;
        expect(referenceModelError(singleModel, "explicit::image", 1)).toBe("");
        expect(referenceModelError(singleModel, "explicit::image", 2)).toContain("最多支持 1 张");
        const multiModel = { ...defaultConfig, channels: [{ ...defaultConfig.channels[0], id: "multi", models: ["image-multi"], modelCosts: [{ model: "image-multi", capabilityConfig: { version: 1, image: { references: { maxImages: 20 } } } }] }] } as typeof defaultConfig;
        expect(referenceModelError(multiModel, "multi::image-multi", 16)).toBe("");
        expect(referenceModelError(multiModel, "multi::image-multi", 17)).toContain("最多支持 16 张");
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
