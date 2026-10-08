import { buildMetasoPayload } from "@/lib/metaso-minimax-video";
import { modelOptionName } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";
import { resolveVideoReferenceImageUrl, resolveVideoReferenceMediaUrl } from "./video-reference-media";
import { resolveVideoImageReferences } from "./video-reference-roles";
import type { ResolvedAiConfig, RequestOptions, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";
import type { VideoProviderDeps } from "./video-provider-deps";

export async function createMetasoVideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, model: string, prompt: string, references: ReferenceImage[], videoReferences: ReferenceVideo[], audioReferences: ReferenceAudio[], options?: RequestOptions): Promise<VideoGenerationTask> {
    const input = { model: modelOptionName(model), prompt, resolution: config.vquality, duration: config.videoSeconds, ratio: config.size };
    const imagePlan = resolveVideoImageReferences(references, options);
    // 与 Fisher provider 相同的两次构建：先用占位 URL 完成档位/角色校验，避免为非法请求上传或解析素材。
    const pending = <T extends { id: string }>(media: T) => ({ ...media, url: `https://pending.invalid/${encodeURIComponent(media.id)}` });
    buildMetasoPayload({ ...input, images: imagePlan.map(({ image, role }) => ({ ...pending(image), role })), videos: videoReferences.map(pending), audios: audioReferences.map(pending) });
    const [imageUrls, videoUrls, audioUrls] = await Promise.all([
        Promise.all(references.map(async (image) => resolveVideoReferenceImageUrl(image, "密塔 H3"))),
        Promise.all(videoReferences.map(async (video) => resolveVideoReferenceMediaUrl(video, "密塔 H3 参考视频", options?.signal))),
        Promise.all(audioReferences.map(async (audio) => resolveVideoReferenceMediaUrl(audio, "密塔 H3 参考音频", options?.signal))),
    ]);
    const payload = buildMetasoPayload({
        ...input,
        images: imagePlan.map(({ image, role }, index) => ({ id: image.id, url: imageUrls[index], role })),
        videos: videoReferences.map((video, index) => ({ id: video.id, url: videoUrls[index] })),
        audios: audioReferences.map((audio, index) => ({ id: audio.id, url: audioUrls[index] })),
    });
    try {
        const created = await deps.transport.post<{ task_id?: unknown }>(metasoVideoUrl(config, "/video_generation"), payload, options);
        const id = validMetasoTaskId(created?.task_id, config.apiKey);
        if (!id) throw new Error("密塔 H3 未返回 task_id，创建结果不确定，请勿直接重发付费任务");
        return { id, provider: "metaso-minimax", model };
    } catch (error) {
        throw new Error(deps.response.readAxiosError(error, "密塔 H3 创建结果不确定，请勿直接重发付费任务"));
    }
}

export async function pollMetasoVideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    const response = await deps.transport.get<{ task?: { status?: string; content?: { url?: string }; error?: { code?: string | number; message?: string } | null } }>(metasoVideoUrl(config, `/query/video_generation/${encodeURIComponent(task.id)}`), options);
    const state = response?.task;
    if (!state) throw new Error("密塔 H3 查询没有返回 task");
    const errorCode = state.error?.code;
    const failed = errorCode !== undefined && errorCode !== "" ? String(errorCode) !== "0" : Boolean(state.error?.message?.trim());
    if (failed || ["failed", "cancelled"].includes(state.status ?? "")) return { status: "failed", error: `${errorCode ? `${errorCode}：` : ""}${state.error?.message || (state.status === "cancelled" ? "密塔 H3 任务已取消" : "密塔 H3 视频生成失败")}` };
    if (state.status === "succeeded") {
        if (!state.content?.url) return { status: "failed", error: "密塔 H3 任务已完成但没有返回视频地址" };
        return { status: "completed", result: await deps.response.videoResultFromUrl(state.content.url, options) };
    }
    if (!["queued", "running"].includes(state.status ?? "")) throw new Error("密塔 H3 返回未知任务状态");
    return { status: "pending" };
}

function metasoVideoUrl(config: ResolvedAiConfig, path: string) {
    const base = config.baseUrl.replace(/\/+$/, "");
    return /\/v2$/i.test(base) ? `${base}${path}` : `${base}/v2${path}`;
}

function validMetasoTaskId(value: unknown, apiKey: string) {
    return typeof value === "string" && !(apiKey && value.includes(apiKey)) && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/.test(value) ? value : "";
}
