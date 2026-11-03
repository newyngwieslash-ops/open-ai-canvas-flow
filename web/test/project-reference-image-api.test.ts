import { afterAll, describe, expect, test } from "bun:test";
import { apiClient, ApiError } from "../src/services/api/request";
import { createProjectAssetEntity, createProjectCharacter, updateProjectAssetReferenceImages, adoptProjectAssetImageCandidate } from "../src/services/api/projects";
import { listAssetImageCandidateTasks, listGenerationTasks } from "../src/services/api/task-center";

const original = apiClient.defaults.adapter;
afterAll(() => { apiClient.defaults.adapter = original; });

describe("项目参考图API合同", () => {
    test("创建直接成品与有序多原图采用http信封并保留目录和portrait", async () => {
        const requests: Array<{ url?: string; data: Record<string, unknown> }> = [];
        apiClient.defaults.adapter = async (config) => {
            requests.push({ url: config.url, data: JSON.parse(config.data) });
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { asset: { id: "asset" } }, msg: "" } };
        };
        expect((await createProjectAssetEntity("project", { name: "阁楼", category: "environment", definition: {}, referenceImages: [{ resourceId: "scene-ref-1" }, { resourceId: "scene-ref-2" }], imageResourceId: "image", folderId: "folder" })).asset.id).toBe("asset");
        await createProjectCharacter("project", { name: "人物", referenceImages: [{ resourceId: "original-1" }, { resourceId: "original-2" }], imageResourceId: "portrait", imageRole: "portrait", folderId: "folder" });
        expect(requests[0].url).toBe("/projects/project/asset-entities");
        expect(requests[0].data).toMatchObject({ imageResourceId: "image", folderId: "folder", referenceImages: [{ resourceId: "scene-ref-1" }, { resourceId: "scene-ref-2" }] });
        expect(requests[1].data).toMatchObject({ imageRole: "portrait", referenceImages: [{ resourceId: "original-1" }, { resourceId: "original-2" }] });
    });
    test("写入有序referenceImages与清空传递expectedVersionId，不吞机器可读冲突", async () => {
        const requests: Array<{ url?: string; data: Record<string, unknown> }> = [];
        apiClient.defaults.adapter = async (config) => {
            requests.push({ url: config.url, data: JSON.parse(config.data) });
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 409, reason: "project_asset_version_changed", msg: "版本冲突" } };
        };
        try { await updateProjectAssetReferenceImages("project", "asset", { referenceImages: [{ resourceId: "original-2" }, { resourceId: "original-1" }], expectedVersionId: "v1" }); throw new Error("应拒绝"); }
        catch (error) { expect(error).toBeInstanceOf(ApiError); expect((error as ApiError).reason).toBe("project_asset_version_changed"); }
        try { await updateProjectAssetReferenceImages("project", "asset", { referenceImages: [], expectedVersionId: "v1" }); throw new Error("应拒绝"); }
        catch (error) { expect(error).toBeInstanceOf(ApiError); expect((error as ApiError).reason).toBe("project_asset_version_changed"); }
        await expect(adoptProjectAssetImageCandidate("project", "asset", "task", { expectedVersionId: "v1" })).rejects.toBeInstanceOf(ApiError);
        expect(requests).toEqual([
            { url: "/projects/project/assets/asset/reference-images", data: { referenceImages: [{ resourceId: "original-2" }, { resourceId: "original-1" }], expectedVersionId: "v1" } },
            { url: "/projects/project/assets/asset/reference-images", data: { referenceImages: [], expectedVersionId: "v1" } },
            { url: "/projects/project/assets/asset/image-candidates/task/adopt", data: { expectedVersionId: "v1" } },
        ]);
    });
    test("活动探测在服务端按目标筛选，不依赖项目最近100条，保留signal", async () => {
        let seen: any;
        apiClient.defaults.adapter = async (config) => {
            seen = config;
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: [{ id: "active", projectId: "project", status: "running", updatedAt: "", clientContext: { nodeId: "project-asset-image:asset" } }], msg: "" } };
        };
        const controller = new AbortController();
        expect((await listGenerationTasks(1, { projectId: "project", activeOnly: true, assetId: "asset" }, undefined, controller.signal))[0].id).toBe("active");
        expect(seen.params).toEqual({ projectId: "project", activeOnly: true, assetId: "asset", pageSize: 1 });
        expect(seen.signal).toBe(controller.signal);
        controller.abort();
        await expect(listGenerationTasks(1, { projectId: "project", activeOnly: true, assetId: "asset" }, undefined, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
    });
    test("候选恢复查询专用筛选、keyset且保留取消signal，不限定activeOnly", async () => {
        let seen: any;
        apiClient.defaults.adapter = async (config) => { seen = config; return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: [{ id: "old", assetImageCandidate: { state: "adopted" } }], msg: "" } }; };
        const controller = new AbortController();
        const result = await listAssetImageCandidateTasks("project", { assetId: "asset", beforeTaskId: "cursor", signal: controller.signal });
        expect(result[0].assetImageCandidate?.state).toBe("adopted");
        expect(seen.params).toEqual({ projectId: "project", assetId: "asset", assetImageCandidates: true, pageSize: 50, beforeTaskId: "cursor" });
        expect(seen.signal).toBe(controller.signal);
        controller.abort();
        await expect(listAssetImageCandidateTasks("project", { signal: controller.signal })).rejects.toMatchObject({ name: "AbortError" });
    });
});
