import { expect, test } from "bun:test";
import { createCanvasResourceReferenceIndex, buildCanvasNodeMentionReferenceMap, buildCanvasResourceReferences, normalizeCanvasNodeMentionTokens } from "../src/lib/canvas/canvas-resource-references";
import { performanceNode } from "./helpers/canvas-performance-fixture";
import { CanvasNodeType } from "../src/types/canvas";

test("lazy mention reads preserve identity and existing slot order", () => {
    const a = performanceNode("a", { type: CanvasNodeType.Image });
    const b = performanceNode("b", { type: CanvasNodeType.Image });
    const target = performanceNode("target");
    const nodes = [a, b, target];
    const edges = [{ id: "b-first", fromNodeId: b.id, toNodeId: target.id }, { id: "a-second", fromNodeId: a.id, toNodeId: target.id }];
    const index = createCanvasResourceReferenceIndex(nodes, edges);
    expect(index.mention(target.id)).toBe(index.mention(target.id));
    expect(index.mention(target.id)).toEqual(buildCanvasNodeMentionReferenceMap(nodes, edges).get(target.id));
    expect(index.mention(target.id).map((ref) => ref.nodeId)).toEqual(["b", "a"]);
    expect(index.mention("missing")).toEqual([]);
});

test("reference index preserves Config override, batch child inheritance, cycle protection, mixed resource numbering, and offscreen prompt tokens", () => {
    const img1 = performanceNode("img-1", { type: CanvasNodeType.Image, title: "图一" });
    const img2 = performanceNode("img-2", { type: CanvasNodeType.Image, title: "图二" });
    const charNode = performanceNode("char-1", {
        type: CanvasNodeType.Text,
        title: "主角",
        metadata: { content: "角色设定", workflowKind: "character", characterAssetId: "char-asset-1" },
    });
    const drawingNode = performanceNode("draw-1", {
        type: CanvasNodeType.Drawing,
        title: "草图",
        metadata: { drawingId: "drawing-1", drawingPreviewUrl: "data:image/png;base64,AAA=" },
    });
    const configNode = performanceNode("cfg-1", { type: CanvasNodeType.Config, metadata: {} });
    const batchRoot = performanceNode("batch-root", {
        type: CanvasNodeType.Image,
        metadata: { isBatchRoot: true, batchChildIds: ["batch-child"] },
    });
    const batchChild = performanceNode("batch-child", {
        type: CanvasNodeType.Image,
        metadata: { batchRootId: "batch-root" },
    });
    const offscreenConsumer = performanceNode("offscreen", {
        type: CanvasNodeType.Text,
        metadata: { prompt: "保持 @[node:img-2|@图片1] 与 @[asset:asset-1|@素材]" },
    });
    const nodes = [img1, img2, charNode, drawingNode, configNode, batchRoot, batchChild, offscreenConsumer];
    const edges = [
        { id: "e-cfg-input", fromNodeId: img2.id, toNodeId: configNode.id },
        { id: "e-own-input", fromNodeId: img1.id, toNodeId: batchRoot.id },
        { id: "e-root-to-cfg", fromNodeId: batchRoot.id, toNodeId: configNode.id },
        { id: "e-char", fromNodeId: charNode.id, toNodeId: offscreenConsumer.id },
        { id: "e-draw", fromNodeId: drawingNode.id, toNodeId: offscreenConsumer.id },
        { id: "e-img", fromNodeId: img2.id, toNodeId: offscreenConsumer.id },
        { id: "e-cycle-1", fromNodeId: batchChild.id, toNodeId: batchRoot.id },
    ];

    const index = createCanvasResourceReferenceIndex(nodes, edges);
    // Config inputs override own inputs, and batch child inherits from batch root
    expect(index.mention(batchRoot.id).map((ref) => ref.nodeId)).toEqual(["img-2"]);
    expect(index.mention(batchChild.id).map((ref) => ref.nodeId)).toEqual(["img-2"]);

    // Mixed numbering (character, drawing, image share the visual slot sequence)
    expect(index.mention(offscreenConsumer.id).map((ref) => [ref.nodeId, ref.label])).toEqual([
        ["char-1", "角色1"],
        ["draw-1", "绘图2"],
        ["img-2", "图片3"],
    ]);

    // Edge reorder updates slot numbering
    const reorderedIndex = createCanvasResourceReferenceIndex(nodes, [
        { id: "e-img", fromNodeId: img2.id, toNodeId: offscreenConsumer.id },
        { id: "e-img-1", fromNodeId: img1.id, toNodeId: offscreenConsumer.id },
    ]);
    expect(reorderedIndex.mention(offscreenConsumer.id).map((ref) => [ref.nodeId, ref.label])).toEqual([
        ["img-2", "图片1"],
        ["img-1", "图片2"],
    ]);

    // Offscreen node prompt normalization with full index preserves both node token and asset token regardless of visibleNodes subset
    const visibleSubset = [img1];
    expect(index.resources(offscreenConsumer.id, visibleSubset)).toEqual(
        buildCanvasResourceReferences(nodes, edges, offscreenConsumer.id, visibleSubset),
    );
    const normalized = normalizeCanvasNodeMentionTokens(offscreenConsumer.metadata?.prompt || "", reorderedIndex.mention(offscreenConsumer.id));
    expect(normalized).toBe("保持 @[node:img-2|@图片1] 与 @[asset:asset-1|@素材]");
});
