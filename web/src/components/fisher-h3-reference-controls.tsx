import { fisherH3OptionsFromMetadata, type FisherH3Options } from "@/lib/fisher-h3-video";
import type { CanvasNodeMetadata } from "@/types/canvas";

type MediaChoice = { id: string; label: string };
type Props = {
    metadata?: CanvasNodeMetadata;
    images: MediaChoice[];
    audios: MediaChoice[];
    videos: MediaChoice[];
    onChange: (patch: Partial<CanvasNodeMetadata>) => void;
};

export function FisherH3ReferenceControls({ metadata, images, audios, videos, onChange }: Props) {
    const options = fisherH3OptionsFromMetadata(metadata);
    const drive = Boolean(options.driveAudioId);
    const control = options.audio_control || {};
    const update = (patch: Partial<FisherH3Options>) => onChange({ providerOptions: { ...metadata?.providerOptions, "fisher-h3-video": { ...options, ...patch } } });
    const updateControl = (patch: NonNullable<FisherH3Options["audio_control"]>) => update({ audio_control: { ...control, ...patch } });
    const mediaSelect = (label: string, value: string | undefined, choices: MediaChoice[], select: (value: string | undefined) => void) => (
        <label className="flex min-w-0 flex-col gap-1"><span>{label}</span><select aria-label={label} className="h-8 rounded border border-[var(--border)] bg-[var(--card-surface)] px-2 text-[var(--foreground)]" value={value || ""} onChange={(event) => select(event.target.value || undefined)}>
            <option value="">不指定</option>
            {value && !choices.some((item) => item.id === value) ? <option value={value}>素材已失效，请重新选择</option> : null}
            {choices.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select></label>
    );
    return <details className="my-2 min-w-0 text-[var(--fs-label)]" data-canvas-no-zoom data-canvas-wheel-scroll onPointerDown={(event) => event.stopPropagation()} onWheel={(event) => event.stopPropagation()}>
        <summary className="cursor-pointer py-1 focus-visible:outline">Fisher-H3 参考与声音</summary>
        <div className="grid gap-2 pt-2">
            <div className="grid grid-cols-2 gap-2">
                {mediaSelect("首帧", metadata?.videoStartFrameNodeId, images, (value) => onChange({ videoStartFrameNodeId: value }))}
                {mediaSelect("尾帧", metadata?.videoEndFrameNodeId, images, (value) => onChange({ videoEndFrameNodeId: value }))}
            </div>
            {mediaSelect("驱动音频", options.driveAudioId, audios, (value) => update({ driveAudioId: value }))}
            <label className="flex flex-col gap-1"><span>音频模式</span><select aria-label="音频模式" className="h-8 rounded border border-[var(--border)] bg-[var(--card-surface)] px-2 text-[var(--foreground)]" value={control.mode ?? (drive ? "lock_source" : "native")} onChange={(event) => updateControl({ mode: event.target.value as NonNullable<FisherH3Options["audio_control"]>["mode"] })}>
                <option value="native">原生声音</option><option value="lock_source" disabled={!drive}>锁定驱动声音</option><option value="remix_source" disabled={!drive}>混合驱动声音</option><option value="reference_only" disabled={!drive}>仅参考驱动音色</option>
            </select></label>
            <label className="flex items-center justify-between gap-2"><span>去噪强度（0–1）</span><input aria-label="去噪强度" type="number" min="0" max="1" step="0.05" className="w-20 rounded border border-[var(--border)] bg-[var(--card-surface)] px-2 py-1" value={control.denoise_strength ?? (drive ? 0 : 0.35)} onChange={(event) => updateControl({ denoise_strength: event.target.valueAsNumber })} /></label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={control.add_drive_as_reference ?? drive} onChange={(event) => updateControl({ add_drive_as_reference: event.target.checked })} /><span>驱动音频同时作为参考</span></label>
            {videos.map((video) => <label key={video.id} className="flex items-center justify-between gap-2"><span className="min-w-0 truncate" title={video.label}>{video.label} · 起始秒数</span><input aria-label={`${video.label} 起始秒数`} type="number" min="0" step="0.01" className="w-20 shrink-0 rounded border border-[var(--border)] bg-[var(--card-surface)] px-2 py-1" value={options.videoStartTimes?.[video.id] ?? 0} onChange={(event) => update({ videoStartTimes: { ...options.videoStartTimes, [video.id]: event.target.valueAsNumber } })} /></label>)}
            {Object.keys(options.videoStartTimes || {}).length ? <button type="button" className="w-fit rounded border border-[var(--border)] px-2 py-1 focus-visible:outline" aria-label="重置视频起始偏移" onClick={() => update({ videoStartTimes: {} })}>重置视频起始偏移</button> : null}
            <p className="opacity-65">{drive ? "驱动音频：4–60 秒" : "普通生成：4–15 秒"}；参考图 9 张 + 首尾帧各 1 张，参考音频 3 条 + 驱动 1 条。1080P 横屏原生输出为 1920×1088。</p>
        </div>
    </details>;
}
