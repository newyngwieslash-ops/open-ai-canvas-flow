import { expect, test } from "bun:test";
import { buildFisherH3Payload, type FisherH3Input } from "../src/lib/fisher-h3-video";

const input: FisherH3Input = { model: "MiniMax-H3", prompt: "航拍山谷", resolution: "1080p", duration: 5, ratio: "16:9" };
const media = (id: string, extra = {}) => ({ id, url: `https://media.example.com/${id}`, ...extra });

test("Fisher 文生视频保持分辨率和固定比例，使用无驱动默认值", () => {
    expect(buildFisherH3Payload(input)).toEqual({ model: "MiniMax-H3", content: [{ type: "text", text: "航拍山谷" }], resolution: "1080P", duration: 5, ratio: "16:9", audio_control: { mode: "native", denoise_strength: 0.35, add_drive_as_reference: false } });
});

test("Fisher 首尾帧与九图混合独立计数，尾帧单独合法", () => {
    const images = [...Array.from({ length: 9 }, (_, i) => media(`ref${i}`, { role: "reference_image" })), media("start", { role: "first_frame" }), media("end", { role: "last_frame" })];
    expect((buildFisherH3Payload({ ...input, images }).content as unknown[])).toHaveLength(12);
    expect(buildFisherH3Payload({ ...input, images: [images[10]!], ratio: "auto" }).ratio).toBe("auto");
    expect(() => buildFisherH3Payload({ ...input, images: [...images, media("extra")] })).toThrow();
    expect(() => buildFisherH3Payload({ ...input, images: [media("ref")], ratio: "adaptive" })).toThrow();
});

test("Fisher 驱动支持 60 秒，默认控制与显式 0/false 均保留", () => {
    const audios = [media("driver"), ...Array.from({ length: 3 }, (_, i) => media(`voice${i}`))];
    const driven = { ...input, duration: 60, audios, options: { driveAudioId: "driver" } };
    expect(buildFisherH3Payload(driven).audio_control).toEqual({ mode: "lock_source", denoise_strength: 0, add_drive_as_reference: true });
    expect(buildFisherH3Payload({ ...driven, options: { driveAudioId: "driver", audio_control: { mode: "remix_source", denoise_strength: 0, add_drive_as_reference: false } } }).audio_control).toEqual({ mode: "remix_source", denoise_strength: 0, add_drive_as_reference: false });
    expect(() => buildFisherH3Payload({ ...input, duration: 60 })).toThrow();
    expect(() => buildFisherH3Payload({ ...driven, options: { driveAudioId: "missing" } })).toThrow();
    expect(() => buildFisherH3Payload({ ...input, options: { audio_control: { mode: "lock_source" } } })).toThrow();
    expect(() => buildFisherH3Payload({ ...driven, options: { driveAudioId: "driver", audio_control: { mode: "reference_only", add_drive_as_reference: false } } })).toThrow();
    for (const durationMs of [NaN, Infinity, -1, 60001]) expect(() => buildFisherH3Payload({ ...driven, audios: [media("driver", { durationMs })] })).toThrow();
});

test("Fisher 视频偏移按 ID 映射，验证原时长与剩余时长", () => {
    const payload = buildFisherH3Payload({ ...input, videos: [media("a", { durationMs: 5000 }), media("b", { durationMs: 15000 })], options: { videoStartTimes: { b: 2 } } });
    expect(payload.content).toContainEqual({ type: "video_url", video_url: { url: "https://media.example.com/b" }, role: "reference_video", start_time_seconds: 2 });
    for (const offset of [-1, NaN, 14]) expect(() => buildFisherH3Payload({ ...input, videos: [media("b", { durationMs: 15000 })], options: { videoStartTimes: { b: offset } } })).toThrow();
    expect(() => buildFisherH3Payload({ ...input, videos: [media("b", { durationMs: 16000 })] })).toThrow();
    expect(() => buildFisherH3Payload({ ...input, videos: [media("b")], options: { videoStartTimes: { missing: 0 } } })).toThrow();
});

test("Fisher 拒绝空提示、超长文本、非整数时长、不支持分辨率和模型", () => {
    for (const changes of [{ prompt: " " }, { prompt: "字".repeat(10001) }, { duration: 4.5 }, { duration: 3 }, { duration: 16 }, { resolution: "2K" }, { model: "minimax-h3" }, { ratio: "" }]) {
        expect(() => buildFisherH3Payload({ ...input, ...changes })).toThrow();
    }
});
