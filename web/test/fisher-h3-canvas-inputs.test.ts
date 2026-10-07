import { expect, test } from "bun:test";
import { buildNodeGenerationContext } from "../src/components/canvas/canvas-node-generation";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

test("explicit mentions retain selected Fisher driver and tail frame", () => {
    const media = (id: string, type: CanvasNodeType, content: string): CanvasNodeData => ({ id, type, title: id, position: { x: 0, y: 0 }, width: 100, height: 100, metadata: { content } });
    const target = media("target", CanvasNodeType.Video, "");
    target.metadata = { composerContent: "看 @图片1", videoEndFrameNodeId: "end", providerOptions: { "fisher-h3-video": { driveAudioId: "driver" } } };
    const nodes = [target, media("ref", CanvasNodeType.Image, "https://example.com/ref.png"), media("end", CanvasNodeType.Image, "https://example.com/end.png"), media("driver", CanvasNodeType.Audio, "https://example.com/voice.wav")];
    const context = buildNodeGenerationContext("target", nodes, nodes.slice(1).map((node) => ({ id: node.id, fromNodeId: node.id, toNodeId: "target" })), "看 @图片1", []);
    expect(context.referenceImages.map((item) => item.id)).toEqual(["ref", "end"]);
    expect(context.referenceAudios.map((item) => item.id)).toEqual(["driver"]);
});
