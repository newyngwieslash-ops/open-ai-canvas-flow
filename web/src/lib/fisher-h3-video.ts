export const FISHER_H3_PROTOCOL = "fisher-h3-video";

export type FisherH3Options = {
    driveAudioId?: string;
    audio_control?: {
        mode?: "native" | "lock_source" | "remix_source" | "reference_only";
        denoise_strength?: number;
        add_drive_as_reference?: boolean;
    };
    videoStartTimes?: Record<string, number>;
};

type FisherMedia = { id: string; url?: string; durationMs?: number };
export type FisherH3Input = {
    model: string;
    prompt: string;
    resolution: string;
    duration: string | number;
    ratio?: string;
    images?: Array<FisherMedia & { role?: string }>;
    videos?: FisherMedia[];
    audios?: FisherMedia[];
    options?: FisherH3Options;
};

export function fisherH3OptionsFromMetadata(metadata?: Record<string, unknown>): FisherH3Options {
    const namespaces = metadata?.providerOptions;
    if (!namespaces || typeof namespaces !== "object" || Array.isArray(namespaces)) return {};
    const options = (namespaces as Record<string, unknown>)[FISHER_H3_PROTOCOL];
    if (options === undefined) return {};
    if (!options || typeof options !== "object" || Array.isArray(options)) throw new Error("Fisher-H3 扩展参数必须是对象");
    return options as FisherH3Options;
}

export function fisherH3ImageReferences<T extends { id: string }>(images: T[], metadata?: Record<string, unknown>): Array<T & { role: string }> {
    const first = String(metadata?.videoStartFrameNodeId ?? "").trim();
    const last = String(metadata?.videoEndFrameNodeId ?? "").trim();
    if (first && first === last) throw new Error("Fisher-H3 首尾帧需要选择不同素材");
    for (const id of [first, last]) if (id && !images.some((image) => image.id === id)) throw new Error("Fisher-H3 选中的首尾帧不在提交素材中");
    return images.map((image) => ({ ...image, role: image.id === first ? "first_frame" : image.id === last ? "last_frame" : "reference_image" }));
}

export function buildFisherH3Payload(input: FisherH3Input): Record<string, unknown> {
    if (input.model !== "MiniMax-H3") throw new Error("Fisher-H3 模型名称必须为 MiniMax-H3");
    const prompt = input.prompt.trim();
    if (!prompt || [...prompt].length > 10000) throw new Error("Fisher-H3 提示词不能为空，且最多 10000 字符");
    const resolution = input.resolution.trim().toUpperCase().replace(/^(480|768|1080)$/, "$1P");
    if (!["480P", "768P", "1080P"].includes(resolution)) throw new Error("Fisher-H3 分辨率仅支持 480P / 768P / 1080P");
    const images = input.images ?? [], videos = input.videos ?? [], audios = input.audios ?? [];
    const options = input.options ?? {};
    if (options.driveAudioId !== undefined && typeof options.driveAudioId !== "string") throw new Error("Fisher-H3 驱动音频 ID 无效");
    const driveAudioId = options.driveAudioId?.trim();
    const drivers = audios.filter((audio) => audio.id === driveAudioId);
    if (driveAudioId && drivers.length !== 1) throw new Error("Fisher-H3 驱动音频必须是唯一的已提交素材");
    const driven = Boolean(driveAudioId);
    const duration = Number(input.duration);
    if (!Number.isInteger(duration) || duration < 4 || duration > (driven ? 60 : 15)) throw new Error(`Fisher-H3 时长必须为整数 ${driven ? "4–60" : "4–15"} 秒`);
    const role = (image: NonNullable<FisherH3Input["images"]>[number]) => image.role ?? "reference_image";
    if (images.some((image) => !["first_frame", "last_frame", "reference_image"].includes(role(image)))) throw new Error("Fisher-H3 图片角色无效");
    for (const [name, max] of [["first_frame", 1], ["last_frame", 1], ["reference_image", 9]] as const) {
        if (images.filter((image) => role(image) === name).length > max) throw new Error(`Fisher-H3 ${name} 最多 ${max} 张`);
    }
    if (videos.length > 3 || audios.length - drivers.length > 3) throw new Error("Fisher-H3 最多 3 条参考视频及 3 条参考音频，驱动音频独立计数");
    const frames = images.some((image) => ["first_frame", "last_frame"].includes(role(image)));
    const hasMedia = images.length + videos.length + audios.length > 0;
    const ratio = input.ratio?.trim() || (hasMedia ? "16:9" : "");
    if (!["1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9", "21:9"].includes(ratio) && !(frames && ["adaptive", "auto"].includes(ratio))) throw new Error("Fisher-H3 需要有效固定比例，adaptive / auto 仅用于首尾帧");
    const control = options.audio_control ?? {};
    if (typeof control !== "object" || Array.isArray(control)) throw new Error("Fisher-H3 audio_control 必须是对象");
    const mode = control.mode ?? (driven ? "lock_source" : "native");
    if (!["native", "lock_source", "remix_source", "reference_only"].includes(mode)) throw new Error("Fisher-H3 音频模式无效");
    if (!driven && mode !== "native") throw new Error("Fisher-H3 非 native 音频模式需要驱动音频");
    const denoise = control.denoise_strength ?? (driven ? 0 : 0.35);
    const addDrive = control.add_drive_as_reference ?? driven;
    if (typeof denoise !== "number" || !Number.isFinite(denoise) || denoise < 0 || denoise > 1) throw new Error("Fisher-H3 去噪强度必须为 0–1");
    if (typeof addDrive !== "boolean" || (mode === "reference_only" && !addDrive)) throw new Error("Fisher-H3 reference_only 必须将驱动作为参考");
    if (drivers.some((audio) => audio.durationMs !== undefined && (!Number.isFinite(audio.durationMs) || audio.durationMs < 0 || audio.durationMs > 60000))) throw new Error("Fisher-H3 驱动音频时长无效或超过 60 秒");
    const offsets = options.videoStartTimes ?? {};
    if (!offsets || typeof offsets !== "object" || Array.isArray(offsets)) throw new Error("Fisher-H3 视频偏移必须按素材 ID 配置");
    for (const [id, offset] of Object.entries(offsets)) {
        if (!videos.some((video) => video.id === id)) throw new Error("Fisher-H3 视频偏移引用的素材已失效");
        if (typeof offset !== "number" || !Number.isFinite(offset) || offset < 0) throw new Error("Fisher-H3 视频偏移必须为非负有限秒数");
    }
    const content: Array<Record<string, unknown>> = [{ type: "text", text: prompt }];
    const mediaUrl = (media: FisherMedia) => {
        if (!media.url?.trim()) throw new Error("Fisher-H3 参考素材缺少可用地址");
        return media.url;
    };
    images.forEach((image) => content.push({ type: "image_url", image_url: { url: mediaUrl(image) }, role: role(image) }));
    videos.forEach((video) => {
        const hasOffset = Object.hasOwn(offsets, video.id);
        const offset = hasOffset ? offsets[video.id]! : 0;
        if (video.durationMs !== undefined && video.durationMs !== 0 && (!Number.isFinite(video.durationMs) || video.durationMs < 2000 || video.durationMs > 15000 || video.durationMs - offset * 1000 < 2000)) throw new Error("Fisher-H3 每条参考视频及偏移后素材需为 2–15 秒");
        content.push({ type: "video_url", video_url: { url: mediaUrl(video) }, role: "reference_video", ...(hasOffset ? { start_time_seconds: offset } : {}) });
    });
    audios.forEach((audio) => content.push({ type: "audio_url", audio_url: { url: mediaUrl(audio) }, role: audio.id === driveAudioId ? "drive_audio" : "reference_audio" }));
    return { model: input.model, content, resolution, duration, ratio, audio_control: { mode, denoise_strength: denoise, add_drive_as_reference: addDrive } };
}
