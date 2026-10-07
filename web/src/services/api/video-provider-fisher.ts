import axios, { AxiosHeaders } from "axios";
import { buildFisherH3Payload, fisherH3ImageReferences } from "@/lib/fisher-h3-video";
import { modelOptionName } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";
import type { RequestOptions, ResolvedAiConfig, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";
import type { VideoProviderDeps } from "./video-provider-deps";
import { resolveVideoReferenceImageUrl, resolveVideoReferenceMediaUrl } from "./video-reference-media";

export async function createFisherH3VideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, model: string, prompt: string, images: ReferenceImage[], videos: ReferenceVideo[], audios: ReferenceAudio[], options?: RequestOptions): Promise<VideoGenerationTask> {
    const imagePlan = fisherH3ImageReferences(images, options);
    const input = { model: modelOptionName(model), prompt, resolution: config.vquality, duration: config.videoSeconds, ratio: config.size, options: options?.fisherH3 };
    // 在读取/上传素材之前先校验参数，真正的请求使用下面解析出的媒体地址。
    const pending = <T extends { id: string }>(media: T) => ({ ...media, url: `https://pending.invalid/${encodeURIComponent(media.id)}` });
    buildFisherH3Payload({ ...input, images: imagePlan.map(pending), videos: videos.map(pending), audios: audios.map(pending) });
    const [resolvedImages, resolvedVideos, resolvedAudios] = await Promise.all([
        Promise.all(imagePlan.map(async (image) => ({ ...image, url: await resolveVideoReferenceImageUrl(image, "Fisher-H3") }))),
        Promise.all(videos.map(async (video) => ({ ...video, url: await resolveVideoReferenceMediaUrl(video, "Fisher-H3 参考视频", options?.signal) }))),
        Promise.all(audios.map(async (audio) => ({ ...audio, url: await resolveVideoReferenceMediaUrl(audio, "Fisher-H3 参考音频", options?.signal) }))),
    ]);
    const payload = buildFisherH3Payload({ ...input, images: resolvedImages, videos: resolvedVideos, audios: resolvedAudios });
    try {
        const created = await deps.transport.post<{ task_id?: unknown }>(fisherVideoUrl(config, "/video_generation"), payload, options);
        const id = validFisherTaskId(created?.task_id, config.apiKey);
        if (!id) throw new Error("Fisher-H3 未返回 task_id，创建结果待核对，请勿直接重发");
        return { id, provider: "fisher-h3", model };
    } catch (error) {
        if (axios.isCancel(error) || options?.signal?.aborted) throw error;
        const headers = axios.isAxiosError(error) ? error.response?.headers : undefined;
        const headerId = headers instanceof AxiosHeaders ? headers.get("x-task-id") : headers?.["x-task-id"] ?? headers?.["X-Task-Id"];
        const id = validFisherTaskId(headerId, config.apiKey);
        if (id) return { id, provider: "fisher-h3", model };
        throw new Error(deps.response.readAxiosError(error, "Fisher-H3 创建结果不确定，请勿直接重发付费任务"));
    }
}

export async function pollFisherH3VideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    const response = await deps.transport.get<{ task?: { status?: string; content?: { url?: string }; error?: { code?: string | number; message?: string } | null } }>(fisherVideoUrl(config, `/query/video_generation/${encodeURIComponent(task.id)}`), options);
    const state = response?.task;
    if (!state) throw new Error("Fisher-H3 查询没有返回 task");
    const errorCode = state.error?.code;
    const failed = errorCode !== undefined && errorCode !== "" ? String(errorCode) !== "0" : Boolean(state.error?.message?.trim());
    if (failed || ["failed", "cancelled"].includes(state.status ?? "")) return { status: "failed", error: `${errorCode ? `${errorCode}：` : ""}${state.error?.message || (state.status === "cancelled" ? "Fisher-H3 任务已取消" : "Fisher-H3 视频生成失败")}` };
    if (state.status === "succeeded") {
        if (!state.content?.url) return { status: "failed", error: "Fisher-H3 任务成功但未返回视频地址" };
        return { status: "completed", result: await deps.response.videoResultFromUrl(state.content.url, options) };
    }
    if (!["queued", "running"].includes(state.status ?? "")) throw new Error("Fisher-H3 返回未知任务状态");
    return { status: "pending" };
}

function fisherVideoUrl(config: ResolvedAiConfig, path: string) {
    const base = config.baseUrl.replace(/\/+$/, "");
    return /\/v2$/i.test(base) ? `${base}${path}` : `${base}/v2${path}`;
}

function validFisherTaskId(value: unknown, apiKey: string) {
    return typeof value === "string" && !(apiKey && value.includes(apiKey)) && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/.test(value) ? value : "";
}
