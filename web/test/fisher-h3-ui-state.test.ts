import { expect, test } from "bun:test";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import { fisherH3VideoProfile, validateFisherH3Submission } from "../src/lib/fisher-h3-ui-state";
import { createFisherH3Channel, defaultConfig, encodeChannelModel } from "../src/stores/use-config-store";
import { buildGenerationConfig, buildVideoGenerationMetadata } from "../src/lib/canvas/canvas-project-generation";
import { creationVideoConfig } from "../src/pages/create/creation-generation-config";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

test("duration UI follows selected driver without mutating stored profile", () => {
    const profile = defaultModelCapabilityConfig("fisher-h3-video").video!;
    expect(fisherH3VideoProfile(profile, {}).duration.max).toBe(15);
    expect(fisherH3VideoProfile(profile, { providerOptions: { "fisher-h3-video": { driveAudioId: "a" } } }).duration.max).toBe(60);
    expect(profile.duration.max).toBe(60);
});

test("Fisher driver cannot expand tighter channel duration bounds or enum choices", () => {
    const profile = defaultModelCapabilityConfig("fisher-h3-video").video!;
    profile.duration = { selection: "range", min: 6, max: 10, step: 2, default: 6 };
    const driven = { providerOptions: { "fisher-h3-video": { driveAudioId: "a" } } };
    expect(fisherH3VideoProfile(profile, driven).duration).toEqual(profile.duration);
    profile.duration = { selection: "enum", values: [4, 8, 20, 40], default: 8 };
    expect(fisherH3VideoProfile(profile, {}).duration.values).toEqual([4, 8]);
    expect(fisherH3VideoProfile(profile, driven).duration.values).toEqual([4, 8, 20, 40]);
});

test("Fisher preflight preserves defaults, rejects stale driver and video offsets", () => {
    const channel = createFisherH3Channel();
    const model = encodeChannelModel(channel.id, "MiniMax-H3");
    const config = { ...defaultConfig, channels: [channel], model, videoModel: model, vquality: "768P", size: "16:9", videoSeconds: "5" };
    const media = { referenceImages: [], referenceVideos: [], referenceAudios: [{ id: "driver", name: "voice", type: "audio/wav", url: "", durationMs: 45000 }] };
    const metadata = { providerOptions: { "fisher-h3-video": { driveAudioId: "driver", audio_control: { denoise_strength: 0, add_drive_as_reference: false } } } };
    expect(() => validateFisherH3Submission({ ...config, videoSeconds: "45" }, metadata, media, "角色说话")).not.toThrow();
    expect(() => validateFisherH3Submission(config, metadata, { ...media, referenceAudios: [] }, "角色说话")).toThrow("驱动音频");
    expect(() => validateFisherH3Submission(config, { providerOptions: { "fisher-h3-video": { videoStartTimes: { old: 0 } } } }, media, "镜头移动")).toThrow("失效");
    channel.modelCosts![0].defaultOptions = { driveAudioId: "missing" };
    expect(() => validateFisherH3Submission(config, {}, media, "角色说话")).toThrow("驱动音频");
});

test("canvas and creation preserve fractional requested duration for strict rejection", () => {
    const channel = createFisherH3Channel();
    const model = encodeChannelModel(channel.id, "MiniMax-H3");
    const config = { ...defaultConfig, channels: [channel], model, videoModel: model, vquality: "768P", size: "16:9" };
    const node: CanvasNodeData = { id: "target", type: CanvasNodeType.Video, title: "video", position: { x: 0, y: 0 }, width: 100, height: 100, metadata: { model, seconds: "5.5", providerOptions: { "fisher-h3-video": { driveAudioId: "a" } } } };
    expect(() => buildGenerationConfig(config, node, "video")).toThrow("整数");
    expect(creationVideoConfig(config, model, { ratio: "16:9", seconds: "5.5", videoQuality: "768" }).videoSeconds).toBe("5.5");
    expect(buildVideoGenerationMetadata(node, { referenceImages: [], referenceVideos: [], referenceAudios: [] }, config).providerOptions).toEqual(node.metadata!.providerOptions);
});
