import { afterAll, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { Window } from "happy-dom";
import { canvasThemes } from "../src/lib/canvas-theme";
import { performanceNode } from "./helpers/canvas-performance-fixture";

const dom = new Window({ url: "http://localhost/" });
dom.HTMLCanvasElement.prototype.getContext = (() => ({})) as never;
const originalDescriptors = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    HTMLElement: dom.HTMLElement,
    HTMLCanvasElement: dom.HTMLCanvasElement,
    Image: dom.Image,
    PointerEvent: dom.PointerEvent,
    MouseEvent: dom.MouseEvent,
    DragEvent: dom.DragEvent,
    WheelEvent: dom.WheelEvent,
    KeyboardEvent: dom.KeyboardEvent,
    CanvasRenderingContext2D: class {},
    Path2D: class {},
})) {
    originalDescriptors.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, {
        configurable: true,
        writable: true,
        value,
    });
}

const { syncBatchConnectionDrafts } = await import("../src/components/canvas/canvas-leafer-graphics-layer");

afterAll(() => {
    dom.happyDOM.abort();
    for (const [key, descriptor] of originalDescriptors) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

describe("canvas batch connection preview cache and draft reuse", () => {
    test("controller caches batch preview planning across mouseWorld updates and clears on cancel", () => {
        const controllerSource = readFileSync(
            resolve(import.meta.dir, "../src/pages/canvas/use-canvas-connection-controller.ts"),
            "utf8",
        );
        expect(controllerSource).toContain("const previewPlanRef = useRef<");
        expect(controllerSource).toContain("old.options.targetNodeId === options.targetNodeId");
        expect(controllerSource).toContain("previewPlanRef.current = null;");
    });

    test("reuses draft Path instances across 100 mouseWorld updates and releases on cancel or node deletion", () => {
        const sourceA = performanceNode("source-a", { position: { x: 0, y: 0 } });
        const sourceB = performanceNode("source-b", { position: { x: 0, y: 200 } });
        const target = performanceNode("target", { position: { x: 600, y: 100 } });
        const nodeById = new Map([
            [sourceA.id, sourceA],
            [sourceB.id, sourceB],
            [target.id, target],
        ]);
        const added: unknown[] = [];
        const removed: unknown[] = [];
        const createdPaths: Array<{ setCalls: number; removed: boolean }> = [];
        const scene = {
            batchDrafts: {
                visible: false,
                add(path: unknown) {
                    added.push(path);
                },
            },
            batchDraftEntries: new Map<string, { set: (attrs: Record<string, unknown>) => void; remove: () => void }>(),
        };
        const createPath = () => {
            const tracker = { setCalls: 0, removed: false };
            createdPaths.push(tracker);
            return {
                set() {
                    tracker.setCalls += 1;
                },
                remove() {
                    tracker.removed = true;
                    removed.push(this);
                },
            };
        };

        for (let frame = 0; frame < 100; frame += 1) {
            syncBatchConnectionDrafts(
                scene as never,
                {
                    batchConnectionPreview: {
                        sourceNodeIds: ["source-a", "source-b"],
                        targetNodeId: "target",
                        mouseWorld: { x: 400 + frame, y: 120 + frame },
                        status: "valid",
                    },
                    nodeById,
                    theme: canvasThemes.dark,
                    scriptScrollTopById: {},
                },
                createPath as never,
            );
        }

        expect(createdPaths.length).toBe(2);
        expect(added.length).toBe(2);
        expect(removed.length).toBe(0);
        expect(createdPaths[0].setCalls).toBe(100);
        expect(createdPaths[1].setCalls).toBe(100);

        const pathA = scene.batchDraftEntries.get("source-a");
        const pathB = scene.batchDraftEntries.get("source-b");
        expect(pathA).toBeDefined();
        expect(pathB).toBeDefined();

        // Deleting a node removes its orphan draft path immediately
        nodeById.delete("source-b");
        syncBatchConnectionDrafts(
            scene as never,
            {
                batchConnectionPreview: {
                    sourceNodeIds: ["source-a", "source-b"],
                    targetNodeId: "target",
                    mouseWorld: { x: 520, y: 220 },
                    status: "valid",
                },
                nodeById,
                theme: canvasThemes.dark,
                scriptScrollTopById: {},
            },
            createPath as never,
        );
        expect(scene.batchDraftEntries.has("source-b")).toBe(false);
        expect(removed.length).toBe(1);

        // Cancelling preview releases all remaining draft paths
        syncBatchConnectionDrafts(
            scene as never,
            {
                batchConnectionPreview: null,
                nodeById,
                theme: canvasThemes.dark,
                scriptScrollTopById: {},
            },
            createPath as never,
        );
        expect(scene.batchDraftEntries.size).toBe(0);
        expect(scene.batchDrafts.visible).toBe(false);
        expect(removed.length).toBe(2);
    });
});
