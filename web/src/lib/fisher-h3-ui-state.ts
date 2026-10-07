import { buildFisherH3Payload, fisherH3ImageReferences, fisherH3OptionsFromMetadata } from "@/lib/fisher-h3-video";
import type { VideoCapabilityConfig } from "@/lib/model-capabilities";
import { generationMetadata } from "@/services/api/generation-task";
import { modelOptionName, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

export function fisherH3VideoProfile(profile: VideoCapabilityConfig, metadata?: Record<string, unknown>): VideoCapabilityConfig {
    const max = fisherH3OptionsFromMetadata(metadata).driveAudioId ? 60 : 15;
    const duration = profile.duration.selection === "enum"
        ? { ...profile.duration, values: profile.duration.values?.filter((value) => value >= 4 && value <= max) }
        : { ...profile.duration, min: Math.max(4, profile.duration.min ?? 4), max: Math.min(max, profile.duration.max ?? max) };
    return { ...profile, duration, ratios: metadata?.videoStartFrameNodeId || metadata?.videoEndFrameNodeId ? profile.ratios : profile.ratios.filter((ratio) => ratio !== "adaptive" && ratio !== "auto") };
}

export function validateFisherH3Submission(config: AiConfig, metadata: Record<string, unknown> | undefined, media: { referenceImages: ReferenceImage[]; referenceVideos: ReferenceVideo[]; referenceAudios: ReferenceAudio[] }, prompt: string) {
    const effective = generationMetadata(config, metadata);
    // 仅做付费提交前的结构校验；素材上传与真实 URL 仍由现有任务服务处理。
    const pendingUrl = (item: { id: string }) => ({ ...item, url: `https://pending.invalid/${encodeURIComponent(item.id)}` });
    buildFisherH3Payload({ model: modelOptionName(config.model), prompt, resolution: config.vquality, duration: config.videoSeconds, ratio: config.size, images: fisherH3ImageReferences(media.referenceImages, effective).map(pendingUrl), videos: media.referenceVideos.map(pendingUrl), audios: media.referenceAudios.map(pendingUrl), options: fisherH3OptionsFromMetadata(effective) });
}
