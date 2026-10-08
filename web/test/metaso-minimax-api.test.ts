import { expect, test } from "bun:test";
import { createMetasoVideoTask, pollMetasoVideoTask } from "../src/services/api/video-provider-metaso";
import { defaultConfig } from "../src/stores/use-config-store";
import { videoResponseTools } from "../src/services/api/video-response";
import type { VideoProviderDeps } from "../src/services/api/video-provider-deps";

const config = { ...defaultConfig, interfaceType: "metaso-minimax-video", model: "MiniMax-H3", baseUrl: "https://metaso.cn/api/minimax", apiKey: "test-only", videoSeconds: "5", vquality: "768P", size: "16:9" };
const image = { id: "first", name: "first.png", type: "image/png", dataUrl: "", url: "https://media.example.com/first.png" };

test("创建发送 content[] 且 base 以 /v2 结尾时不重复补前缀", async () => {
    let body: Record<string, unknown> = {};
    const deps = { transport: { post: async (url: string, value: Record<string, unknown>) => { expect(url).toBe("https://metaso.cn/api/minimax/v2/video_generation"); body = value; return { task_id: "task-1" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    const task = await createMetasoVideoTask(deps, config as never, config.model, "平稳转身", [image], [], []);
    expect(task).toEqual({ id: "task-1", provider: "metaso-minimax", model: config.model });
    expect(body.model).toBe("MiniMax-H3");
    expect(body.resolution).toBe("768P");
    expect(body.ratio).toBe("16:9");
    expect(body.content).toContainEqual({ type: "image_url", image_url: { url: image.url }, role: "first_frame" });
});

test("非法参数不发 POST；request_id 不充当 task_id，缺失时报结果不确定", async () => {
    let posts = 0;
    const deps = { transport: { post: async () => { posts++; return { request_id: "trace-only" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    await expect(createMetasoVideoTask(deps, { ...config, videoSeconds: "16" } as never, config.model, "运动", [], [], [])).rejects.toThrow();
    expect(posts).toBe(0);
    await expect(createMetasoVideoTask(deps, config as never, config.model, "运动", [], [], [])).rejects.toThrow("创建结果不确定");
    expect(posts).toBe(1);
});

test("轮询状态归一：成功取回 URL、失败带上游错误、queued/running 为 pending、未知状态抛错", async () => {
    const task = { id: "task-1", provider: "metaso-minimax", model: config.model } as never;
    const deps = { transport: { get: async () => ({ task: { status: "succeeded", content: { url: "https://media.example.com/result.mp4" } } }) }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "completed", result: { url: "https://media.example.com/result.mp4" } });
    deps.transport.get = async () => ({ task: { status: "failed", error: { code: "1001", message: "额度不足" } } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "failed", error: "1001：额度不足" });
    deps.transport.get = async () => ({ task: { status: "queued" } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "pending" });
    deps.transport.get = async () => ({ task: { status: "mystery" } }) as never;
    await expect(pollMetasoVideoTask(deps, config as never, task)).rejects.toThrow("未知任务状态");
});

test("成功响应中 error.code=0 不应成为失败；成功但无 URL 归为失败", async () => {
    const task = { id: "task-1", provider: "metaso-minimax", model: config.model } as never;
    const deps = { transport: { get: async () => ({ task: { status: "succeeded", error: { code: 0 }, content: { url: "https://media.example.com/result.mp4" } } }) }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    expect((await pollMetasoVideoTask(deps, config as never, task)).status).toBe("completed");
    deps.transport.get = async () => ({ task: { status: "succeeded" } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "failed", error: "密塔 H3 任务已完成但没有返回视频地址" });
});
