import assert from "node:assert/strict";
import test from "node:test";

// @ts-expect-error -- Node 原生 TypeScript 测试运行器需要保留扩展名。
import { buildMetasoPayload, METASO_PROTOCOL } from "./metaso-minimax-video.ts";

const base = { model: "MiniMax-H3", prompt: "平稳转身", resolution: "768P", duration: 5, ratio: "16:9" };

test("METASO_PROTOCOL 常量与文生视频仅含文本条目", () => {
    assert.equal(METASO_PROTOCOL, "metaso-minimax-video");
    const payload = buildMetasoPayload(base);
    assert.deepEqual(payload, {
        model: "MiniMax-H3",
        content: [{ type: "text", text: "平稳转身" }],
        resolution: "768P",
        duration: 5,
        ratio: "16:9",
    });
});

test("图生视频发送首帧角色，多模态按序组装且无 start_time_seconds", () => {
    const payload = buildMetasoPayload({
        ...base,
        resolution: "2K",
        images: [{ id: "a", url: "https://media.example.com/first.jpg", role: "first_frame" }],
        videos: [{ id: "b", url: "https://media.example.com/ref.mp4" }],
        audios: [{ id: "c", url: "https://media.example.com/dialogue.wav" }],
    }) as Record<string, unknown>;
    assert.equal(payload.resolution, "2K");
    assert.deepEqual(payload.content, [
        { type: "text", text: "平稳转身" },
        { type: "image_url", image_url: { url: "https://media.example.com/first.jpg" }, role: "first_frame" },
        { type: "video_url", video_url: { url: "https://media.example.com/ref.mp4" }, role: "reference_video" },
        { type: "audio_url", audio_url: { url: "https://media.example.com/dialogue.wav" }, role: "reference_audio" },
    ]);
    const video = payload.content[2];
    assert.equal("start_time_seconds" in video, false);
});

test("无 role 的图片缺省 first_frame，参考图允许 reference_image", () => {
    const payload = buildMetasoPayload({
        ...base,
        images: [{ id: "a", url: "https://media.example.com/x.jpg" }, { id: "b", url: "https://media.example.com/y.jpg", role: "reference_image" }],
    }) as Record<string, unknown>;
    assert.equal(payload.content[1].role, "first_frame");
    assert.equal(payload.content[2].role, "reference_image");
});

test("档位归一与拒绝：480p 转大写、2K 原样、2160p/adaptive/3 秒/16 秒/超长提示词拒绝", () => {
    assert.equal(buildMetasoPayload({ ...base, resolution: "480p" }).resolution, "480P");
    assert.equal(buildMetasoPayload({ ...base, resolution: "2k" }).resolution, "2K");
    for (const mutate of [
        () => ({ ...base, resolution: "2160p" }),
        () => ({ ...base, resolution: "adaptive" }),
        () => ({ ...base, duration: 3 }),
        () => ({ ...base, duration: 16 }),
        () => ({ ...base, duration: 5.5 }),
        () => ({ ...base, prompt: "  " }),
        () => ({ ...base, prompt: "长".repeat(10001) }),
        () => ({ ...base, model: "MiniMax-H3-Max" }),
        () => ({ ...base, ratio: "2:3" }),
        () => ({ ...base, ratio: undefined }),
        () => ({ ...base, images: [{ id: "a", role: "last_frame" }] }),
        () => ({ ...base, images: [{ id: "a", role: "first_frame" }, { id: "b", role: "first_frame" }] }),
        () => ({ ...base, images: Array.from({ length: 10 }, (_, i) => ({ id: String(i), role: "reference_image" })) }),
        () => ({ ...base, videos: Array.from({ length: 4 }, (_, i) => ({ id: String(i) })) }),
        () => ({ ...base, audios: Array.from({ length: 4 }, (_, i) => ({ id: String(i) })) }),
        () => ({ ...base, images: [{ id: "a" }] }),
    ]) {
        assert.throws(() => buildMetasoPayload(mutate() as never), Error, JSON.stringify(mutate()));
    }
});

test("有参考素材时比例缺省 16:9", () => {
    const payload = buildMetasoPayload({ ...base, ratio: undefined, images: [{ id: "a", role: "reference_image", url: "https://media.example.com/x.jpg" }] }) as Record<string, unknown>;
    assert.equal(payload.ratio, "16:9");
});
