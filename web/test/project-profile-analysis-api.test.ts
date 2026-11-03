import { afterEach, expect, test } from "bun:test";
import * as projects from "../src/services/api/projects";
import { apiClient, ApiError } from "../src/services/api/request";

const original = apiClient.defaults.adapter;
afterEach(() => { apiClient.defaults.adapter = original; });

test("专用分析 API 只发送冻结 DTO，编码路径、解包信封并传递读取取消", async () => {
    const calls: any[] = [];
    apiClient.defaults.adapter = async (config) => {
        calls.push(config);
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { analysis: { taskId: "task" }, analyses: [], nextCursor: "next", adoptedVersionId: "v2", replayed: false, asset: { id: "asset" } }, msg: "" } };
    };
    expect(typeof projects.createProjectAssetProfileAnalysis).toBe("function");
    const input = { expectedVersionId: "v1", sourceResourceId: "original", modelSelection: { kind: "system" as const, channelId: "channel", modelKey: "vision" } };
    const result = await projects.createProjectAssetProfileAnalysis("p/1", "a/1", input);
    expect(result.analysis.taskId).toBe("task");
    expect(calls[0].url).toBe("/projects/p%2F1/assets/a%2F1/profile-analyses");
    expect(JSON.parse(calls[0].data)).toEqual(input);
    const controller = new AbortController();
    const page = await projects.listProjectAssetProfileAnalyses("p", "a", { limit: 20, beforeTaskId: "before", signal: controller.signal });
    expect(page.nextCursor).toBe("next");
    expect(calls[1].params).toEqual({ limit: 20, beforeTaskId: "before" });
    expect(calls[1].signal).toBe(controller.signal);
    await projects.getProjectAssetProfileAnalysis("p", "a", "t/1", controller.signal);
    expect(calls[2].url).toBe("/projects/p/assets/a/profile-analyses/t%2F1");
    expect(calls[2].signal).toBe(controller.signal);
    const adoption = { expectedVersionId: "v1", selectedFields: ["clothing" as const], idempotencyKey: "0123456789abcdef" };
    await projects.adoptProjectAssetProfileAnalysis("p", "a", "t", adoption);
    expect(calls[3].url).toBe("/projects/p/assets/a/profile-analyses/t/adopt");
    expect(JSON.parse(calls[3].data)).toEqual(adoption);
});

test("分析 API 业务失败保留机器原因而非成功默认值", async () => {
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 409, reason: "project_asset_version_changed", msg: "conflict" } });
    expect(typeof projects.adoptProjectAssetProfileAnalysis).toBe("function");
    try {
        await projects.adoptProjectAssetProfileAnalysis("p", "a", "t", { expectedVersionId: "v1", selectedFields: ["props"], idempotencyKey: "0123456789abcdef" });
        throw new Error("must reject");
    } catch (error) {
        expect(error).toBeInstanceOf(ApiError);
        expect((error as ApiError).reason).toBe("project_asset_version_changed");
    }
});
