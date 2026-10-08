export const METASO_PROTOCOL = "metaso-minimax-video";

// 密塔协议与 Fisher-H3 同构但更严格：无 audio_control/驱动音频/start_time_seconds 扩展，
// 分辨率多 2K 档，比例固定六档，纯文本必须显式选择比例。前端先行校验给出友好错误，后端 manifest 校验兜底。
export type MetasoMedia = { id: string; url?: string };
export type MetasoImageInput = MetasoMedia & { role?: string };
export type MetasoInput = {
    model: string;
    prompt: string;
    resolution: string;
    duration: string | number;
    ratio?: string;
    images?: MetasoImageInput[];
    videos?: MetasoMedia[];
    audios?: MetasoMedia[];
};

export function buildMetasoPayload(input: MetasoInput): Record<string, unknown> {
    if (input.model !== "MiniMax-H3") throw new Error("密塔 H3 模型名称必须为 MiniMax-H3");
    const prompt = input.prompt.trim();
    if (!prompt || [...prompt].length > 10000) throw new Error("密塔 H3 提示词不能为空，且最多 10000 字符");
    const resolution = input.resolution.trim().toUpperCase().replace(/^(480|768|1080)$/, "$1P");
    if (!["480P", "768P", "1080P", "2K"].includes(resolution)) throw new Error("密塔 H3 分辨率仅支持 480P / 768P / 1080P / 2K");
    const images = input.images ?? [], videos = input.videos ?? [], audios = input.audios ?? [];
    const duration = Number(input.duration);
    if (!Number.isInteger(duration) || duration < 4 || duration > 15) throw new Error("密塔 H3 时长必须为 4–15 整数秒");
    const role = (image: MetasoImageInput) => image.role ?? "first_frame";
    if (images.some((image) => !["first_frame", "reference_image"].includes(role(image)))) throw new Error("密塔 H3 图片角色仅支持 first_frame 与 reference_image");
    if (images.filter((image) => role(image) === "first_frame").length > 1) throw new Error("密塔 H3 首帧图片最多 1 张");
    if (images.filter((image) => role(image) === "reference_image").length > 9) throw new Error("密塔 H3 参考图片最多 9 张");
    if (videos.length > 3) throw new Error("密塔 H3 参考视频最多 3 条");
    if (audios.length > 3) throw new Error("密塔 H3 参考音频最多 3 条");
    const hasMedia = images.length + videos.length + audios.length > 0;
    const ratio = input.ratio?.trim() || (hasMedia ? "16:9" : "");
    if (!["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"].includes(ratio)) throw new Error("密塔 H3 需要固定比例；纯文本生成必须显式指定");
    const mediaUrl = (media: MetasoMedia) => {
        if (!media.url?.trim()) throw new Error("密塔 H3 参考素材缺少可用地址");
        return media.url;
    };
    const content: Array<Record<string, unknown>> = [{ type: "text", text: prompt }];
    images.forEach((image) => content.push({ type: "image_url", image_url: { url: mediaUrl(image) }, role: role(image) }));
    videos.forEach((video) => content.push({ type: "video_url", video_url: { url: mediaUrl(video) }, role: "reference_video" }));
    audios.forEach((audio) => content.push({ type: "audio_url", audio_url: { url: mediaUrl(audio) }, role: "reference_audio" }));
    return { model: input.model, content, resolution, duration, ratio };
}
