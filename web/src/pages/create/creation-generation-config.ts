import { modelCapabilityConfigFor, normalizeVideoValue } from "@/lib/model-capabilities";
import { resolveModelVideoBooleanOptions } from "@/lib/model-selection";
import { resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";
import type { CreationSettings } from "./creation-types";

// 创作台没有音频和水印开关；报价与提交共用所选模型的默认值和能力限制。
export function creationVideoConfig(config: AiConfig, model: string, settings: Pick<CreationSettings, "ratio" | "seconds" | "videoQuality">): AiConfig {
    const profile = modelCapabilityConfigFor(config, model).video!;
    const fisher = resolveModelRequestConfig(config, model).interfaceType === "fisher-h3-video";
    const normalized = normalizeVideoValue(profile, {
        ratio: settings.ratio,
        seconds: settings.seconds,
        resolution: settings.videoQuality,
    });
    return {
        ...config,
        model,
        videoModel: model,
        size: fisher ? settings.ratio : normalized.ratio,
        videoSeconds: fisher ? settings.seconds : normalized.seconds,
        vquality: fisher ? settings.videoQuality : normalized.resolution.replace(/p$/i, ""),
        ...resolveModelVideoBooleanOptions(
            config,
            model,
            {},
            {
                videoGenerateAudio: config.videoGenerateAudio,
                videoWatermark: config.videoWatermark,
            },
        ),
    };
}
