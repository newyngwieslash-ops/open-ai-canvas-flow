import { describe, expect, test } from "bun:test";
import { buildCanvasConnectionHitIndex } from "../src/lib/canvas/canvas-connection-hit-index";
import { CanvasNodeType } from "../src/types/canvas";
import { performanceNode, performanceProject } from "./helpers/canvas-performance-fixture";

describe("canvas connection hit index", () => {
    test("connection hit candidates preserve reverse source order", () => {
        const a = performanceNode("a");
        const b = performanceNode("b");
        const index = buildCanvasConnectionHitIndex([a, b]);
        expect(index.candidates({ x: 320, y: 90 }, 30).map((node) => node.id)).toEqual(["b", "a"]);
    });

    test("ordinary sparse graphs do not scan all nodes per query", () => {
        const index = buildCanvasConnectionHitIndex(performanceProject(50000).nodes);
        expect(index.candidates({ x: 320, y: 90 }, 30).length).toBeLessThan(20);
    });

    test("retains dynamic Script and BatchTable nodes while filtering hidden frame/batch children", () => {
        const frame = performanceNode("frame", {
            type: CanvasNodeType.Frame,
            metadata: { frame: { collapsed: true } },
        });
        const hiddenFrameChild = performanceNode("frame-child", { parentId: "frame" });
        const batchRoot = performanceNode("batch-root", {
            metadata: { isBatchRoot: true, imageBatchExpanded: false },
        });
        const hiddenBatchChild = performanceNode("batch-child", {
            metadata: { batchRootId: "batch-root" },
        });
        const script = performanceNode("script", {
            type: CanvasNodeType.Script,
            position: { x: 10000, y: 10000 },
        });
        const batchTable = performanceNode("table", {
            type: CanvasNodeType.BatchTable,
            position: { x: 20000, y: 20000 },
        });
        const index = buildCanvasConnectionHitIndex([
            frame,
            hiddenFrameChild,
            batchRoot,
            hiddenBatchChild,
            script,
            batchTable,
        ]);
        const candidates = index.candidates({ x: 320, y: 90 }, 30).map((node) => node.id);
        expect(candidates).toEqual(["table", "script", "batch-root"]);
    });
});
