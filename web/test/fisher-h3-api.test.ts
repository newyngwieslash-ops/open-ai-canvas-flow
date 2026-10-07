import { expect, test } from "bun:test";
import { AxiosError } from "axios";
import { createFisherH3VideoTask, pollFisherH3VideoTask } from "../src/services/api/video-provider-fisher";
import { defaultConfig } from "../src/stores/use-config-store";
import { videoResponseTools } from "../src/services/api/video-response";
import type { VideoProviderDeps } from "../src/services/api/video-provider-deps";

const config = { ...defaultConfig, interfaceType: "fisher-h3-video", model: "MiniMax-H3", baseUrl: "https://api.work-fisher.com", apiKey: "test-only", videoSeconds: "5", vquality: "1080P", size: "16:9" };
const image = { id: "tail", name: "tail.png", type: "image/png", dataUrl: "", url: "https://media.example.com/tail.png" };

test("Fisher 直接路径发送真实 payload，尾帧比例不被覆盖", async () => {
    let body: Record<string, unknown> = {};
    const deps = { transport: { post: async (url: string, value: Record<string, unknown>) => { expect(url).toBe("https://api.work-fisher.com/v2/video_generation"); body = value; return { task_id: "task-1" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    const task = await createFisherH3VideoTask(deps, config as never, config.model, "平稳转身", [image], [], [], { videoEndFrameNodeId: "tail" });
    expect(task.id).toBe("task-1");
    expect(body.resolution).toBe("1080P");
    expect(body.ratio).toBe("16:9");
    expect(body.content).toContainEqual({ type: "image_url", image_url: { url: image.url }, role: "last_frame" });
});

test("非法参数不发 POST，request_id 不充当 task_id", async () => {
    let posts = 0;
    const deps = { transport: { post: async () => { posts++; return { request_id: "trace-only" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    await expect(createFisherH3VideoTask(deps, { ...config, videoSeconds: "16" } as never, config.model, "运动", [], [], [])).rejects.toThrow();
    expect(posts).toBe(0);
    await expect(createFisherH3VideoTask(deps, config as never, config.model, "运动", [], [], [])).rejects.toThrow();
    expect(posts).toBe(1);
});

test("创建错误有 X-Task-Id 时只查询已有任务，无编号错误不重发", async () => {
    let posts = 0, gets = 0;
    const deps = { transport: {
        post: async () => { posts++; throw new AxiosError("gateway error", "ERR_BAD_RESPONSE", undefined, undefined, { status: 502, headers: { "x-task-id": "accepted-1" }, data: {}, statusText: "Bad Gateway", config: {} } as never); },
        get: async (url: string) => { gets++; expect(url).toBe("https://api.work-fisher.com/v2/query/video_generation/accepted-1"); return { task: { status: "succeeded", content: { url: "https://media.example.com/result.mp4" } } }; },
    }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    const task = await createFisherH3VideoTask(deps, config as never, config.model, "运动", [], [], []);
    expect(await pollFisherH3VideoTask(deps, config as never, task)).toEqual({ status: "completed", result: { url: "https://media.example.com/result.mp4" } });
    expect(posts).toBe(1); expect(gets).toBe(1);
    deps.transport.post = async () => { posts++; throw new Error("timeout"); };
    await expect(createFisherH3VideoTask(deps, config as never, config.model, "运动", [], [], [])).rejects.toThrow();
    expect(posts).toBe(2);
});

test("HTTP 200 task.error 和 cancelled 均为失败", async () => {
    const deps = { transport: { get: async () => ({ task: { status: "running", error: { code: "quota", message: "额度不足" } } }) }, response: videoResponseTools } as unknown as VideoProviderDeps;
    const task = { id: "task-1", model: config.model, provider: "fisher-h3" } as never;
    expect(await pollFisherH3VideoTask(deps, config as never, task)).toEqual({ status: "failed", error: "quota：额度不足" });
    deps.transport.get = async () => ({ task: { status: "cancelled" } }) as never;
    expect((await pollFisherH3VideoTask(deps, config as never, task)).status).toBe("failed");
});

test("上游不能把 API Key 作为任务编号回传", async () => {
    const keyed = { ...config, apiKey: "secret-test-key" };
    const deps = { transport: { post: async () => { throw new AxiosError("gateway error", "ERR_BAD_RESPONSE", undefined, undefined, { status: 502, headers: { "x-task-id": keyed.apiKey }, data: {}, config: {} } as never); } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    await expect(createFisherH3VideoTask(deps, keyed as never, keyed.model, "运动", [], [], [])).rejects.toThrow("创建结果不确定");
});

test("成功响应中 error.code=0 不应成为失败", async () => {
    const deps = { transport: { get: async () => ({ task: { status: "succeeded", error: { code: 0 }, content: { url: "https://media.example.com/result.mp4" } } }) }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    expect((await pollFisherH3VideoTask(deps, config as never, { id: "task-1", model: config.model, provider: "fisher-h3" })).status).toBe("completed");
});
