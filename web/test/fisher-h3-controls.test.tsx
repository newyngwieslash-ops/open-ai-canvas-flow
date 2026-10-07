import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { Children, isValidElement, type ReactNode } from "react";
import type { CanvasNodeMetadata } from "../src/types/canvas";
import { FisherH3ReferenceControls } from "../src/components/fisher-h3-reference-controls";
import { CanvasVideoPromptTools } from "../src/components/canvas/canvas-video-prompt-tools";
import { createFisherH3Channel, defaultConfig, encodeChannelModel } from "../src/stores/use-config-store";

test("Fisher controls expose keyboard native inputs and preserve explicit zero and false", () => {
    const html = renderToStaticMarkup(<FisherH3ReferenceControls images={[{ id: "end", label: "尾帧图" }]} audios={[{ id: "driver", label: "对白" }]} videos={[{ id: "video", label: "镜头" }]} metadata={{ videoEndFrameNodeId: "end", providerOptions: { "fisher-h3-video": { driveAudioId: "driver", audio_control: { mode: "native", denoise_strength: 0, add_drive_as_reference: false }, videoStartTimes: { video: 0 } } } }} onChange={() => {}} />);
    expect(html).toContain('aria-label="尾帧"');
    expect(html).toContain('aria-label="驱动音频"');
    expect(html).toContain('aria-label="镜头 起始秒数"');
    expect(html).toContain('value="end" selected=""');
    expect(html).toContain('value="native" selected=""');
    expect(html).not.toContain('checked=""');
    expect(html).toContain("4–60 秒");
    expect(html).toContain("1920×1088");
});

test("removed offset video has a working reset action without clearing driver settings", () => {
    let patch: Partial<CanvasNodeMetadata> | undefined;
    const tree = FisherH3ReferenceControls({ images: [], audios: [], videos: [], metadata: { providerOptions: { "fisher-h3-video": { driveAudioId: "driver", audio_control: { mode: "native", denoise_strength: 0 }, videoStartTimes: { removed: 2 } } } }, onChange: (value) => { patch = value; } });
    const pending: ReactNode[] = [tree];
    let reset: (() => void) | undefined;
    while (pending.length) {
        const node = pending.pop();
        if (!isValidElement<{ children?: ReactNode; "aria-label"?: string; onClick?: () => void }>(node)) continue;
        if (node.props["aria-label"] === "重置视频起始偏移") reset = node.props.onClick;
        pending.push(...Children.toArray(node.props.children));
    }
    expect(reset).toBeDefined();
    reset!();
    expect(patch?.providerOptions?.["fisher-h3-video"]).toEqual({ driveAudioId: "driver", audio_control: { mode: "native", denoise_strength: 0 }, videoStartTimes: {} });
});

test("real canvas controls render channel audio defaults merged with node overrides", () => {
    const channel = createFisherH3Channel();
    channel.scope = "system";
    channel.modelCosts![0].defaultOptions = { audio_control: { mode: "native", denoise_strength: 0.2, add_drive_as_reference: true } };
    const model = encodeChannelModel(channel.id, "MiniMax-H3");
        const html = renderToStaticMarkup(<CanvasVideoPromptTools config={{ ...defaultConfig, channels: [channel], videoModel: model, model }} frameOptions={[]} audioOptions={[{ id: "driver", label: "对白" }]} metadata={{ model, providerOptions: { "fisher-h3-video": { driveAudioId: "driver", audio_control: { add_drive_as_reference: false } } } }} onMetadataChange={() => {}} />);
        expect(html).toContain('value="native" selected=""');
        expect(html).toContain('value="0.2"');
        expect(html).not.toContain('checked=""');
});
