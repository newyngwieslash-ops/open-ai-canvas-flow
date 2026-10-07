import { expect, spyOn, test } from "bun:test";
import axios from "axios";
import { createChannelTransport } from "../src/services/api/channel-transport";

test("Fisher 自定义中转显式携带协议身份，不靠模型名猜测", async () => {
    const calls: Array<Record<string, unknown>> = [];
    const request = spyOn(axios, "request").mockImplementation(async (config) => { calls.push(config as Record<string, unknown>); return { data: { task_id: "task-1" } } as never; });
    try {
        const config = { baseUrl: "https://api.work-fisher.com", apiKey: "test-only", apiFormat: "openai", interfaceType: "fisher-h3-video" };
        await createChannelTransport(config as never, "video").postJson("https://api.work-fisher.com/v2/video_generation", { model: "MiniMax-H3" });
        expect((calls[0]?.headers as Record<string, string>)["x-canvas-upstream-protocol"]).toBe("fisher-h3-video");
        await createChannelTransport({ ...config, interfaceType: "minimax-video" } as never, "video").postJson("https://api.work-fisher.com/v2/video_generation", { model: "MiniMax-H3" });
        expect((calls[1]?.headers as Record<string, string>)["x-canvas-upstream-protocol"]).toBeUndefined();
    } finally { request.mockRestore(); }
});
