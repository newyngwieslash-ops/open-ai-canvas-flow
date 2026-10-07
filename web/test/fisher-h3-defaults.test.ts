import { expect, test } from "bun:test";
import { generationMetadata } from "../src/services/api/generation-task";
import { defaultConfig, createFisherH3Channel } from "../src/stores/use-config-store";
import { fisherH3OptionsFromMetadata } from "../src/lib/fisher-h3-video";

test("局部音频选项覆盖保留渠道其他默认字段", () => {
    const channel = createFisherH3Channel();
    channel.modelCosts![0]!.defaultOptions = { audio_control: { mode: "native", denoise_strength: 0.2, add_drive_as_reference: true } };
    const config = { ...defaultConfig, model: "MiniMax-H3", channels: [channel] };
    const effective = generationMetadata(config, { providerOptions: { "fisher-h3-video": { driveAudioId: "driver", audio_control: { add_drive_as_reference: false } } } });
    expect(fisherH3OptionsFromMetadata(effective).audio_control).toEqual({ mode: "native", denoise_strength: 0.2, add_drive_as_reference: false });
});
