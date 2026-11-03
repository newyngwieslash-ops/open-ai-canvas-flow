import { describe, expect, test } from "bun:test";
import { calculateNodeAlignment, createNodeAlignmentContext } from "../src/lib/canvas/canvas-project-domain";
import { isFrameNode } from "../src/lib/canvas/canvas-frame";
import type { CanvasNodeData, Position } from "../src/types/canvas";
import { performanceNode, performanceProject } from "./helpers/canvas-performance-fixture";

function linearCalculateNodeAlignment(
    nodes: CanvasNodeData[],
    initialPositions: Array<{ id: string; x: number; y: number }>,
    rawOffset: Position,
    threshold: number,
) {
    const movingIds = new Set(initialPositions.map((item) => item.id));
    const initialById = new Map(initialPositions.map((item) => [item.id, item]));
    const movingNodes = nodes.filter((node) => movingIds.has(node.id));
    if (!movingNodes.length) return { offset: rawOffset, guides: {} as { vertical?: number; horizontal?: number } };
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const left = Math.min(...movingNodes.map((node) => initialById.get(node.id)?.x ?? node.position.x));
    const top = Math.min(...movingNodes.map((node) => initialById.get(node.id)?.y ?? node.position.y));
    const right = Math.max(...movingNodes.map((node) => (initialById.get(node.id)?.x ?? node.position.x) + node.width));
    const bottom = Math.max(...movingNodes.map((node) => (initialById.get(node.id)?.y ?? node.position.y) + node.height));
    const targets = nodes.flatMap((node) => {
        if (movingIds.has(node.id)) return [];
        const batchRoot = node.metadata?.batchRootId ? nodeById.get(node.metadata.batchRootId) : null;
        if (batchRoot && !batchRoot.metadata?.imageBatchExpanded) return [];
        const parent = node.parentId ? nodeById.get(node.parentId) : null;
        if (parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed) return [];
        return [{
            x: [node.position.x, node.position.x + node.width / 2, node.position.x + node.width],
            y: [node.position.y, node.position.y + node.height / 2, node.position.y + node.height],
        }];
    });
    const movingX = [left + rawOffset.x, (left + right) / 2 + rawOffset.x, right + rawOffset.x];
    const movingY = [top + rawOffset.y, (top + bottom) / 2 + rawOffset.y, bottom + rawOffset.y];
    let bestXDelta: number | undefined;
    let bestXGuide: number | undefined;
    let bestYDelta: number | undefined;
    let bestYGuide: number | undefined;
    targets.forEach(({ x: targetsX, y: targetsY }) => {
        movingX.forEach((value, anchorIndex) => {
            const target = targetsX[anchorIndex];
            const delta = target - value;
            if (Math.abs(delta) <= threshold && (bestXDelta === undefined || Math.abs(delta) < Math.abs(bestXDelta))) {
                bestXDelta = delta;
                bestXGuide = target;
            }
        });
        movingY.forEach((value, anchorIndex) => {
            const target = targetsY[anchorIndex];
            const delta = target - value;
            if (Math.abs(delta) <= threshold && (bestYDelta === undefined || Math.abs(delta) < Math.abs(bestYDelta))) {
                bestYDelta = delta;
                bestYGuide = target;
            }
        });
    });
    return {
        offset: { x: rawOffset.x + (bestXDelta || 0), y: rawOffset.y + (bestYDelta || 0) },
        guides: { vertical: bestXGuide, horizontal: bestYGuide },
    };
}

describe("canvas node alignment index", () => {
    test("alignment keeps distant same-axis targets", () => {
        const moving = performanceNode("moving", { width: 10, height: 10 });
        const far = performanceNode("far", { position: { x: 100, y: 100000 }, width: 10, height: 10 });
        const context = createNodeAlignmentContext([moving, far], [{ id: moving.id, x: 0, y: 0 }]);
        expect(calculateNodeAlignment(context, { x: 96, y: 0 }, 7)).toEqual({
            offset: { x: 100, y: 0 },
            guides: { vertical: 100, horizontal: undefined },
        });
    });

    test("matches linear oracle across 1000 seeded graphs and edge cases", () => {
        let seed = 0x12345678;
        const nextRand = () => {
            seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
            return seed / 0x100000000;
        };
        const randInt = (min: number, max: number) => Math.floor(min + nextRand() * (max - min + 1));

        for (let sample = 0; sample < 1000; sample += 1) {
            const count = (sample % 12) + 3;
            const nodes: CanvasNodeData[] = [];
            for (let i = 0; i < count; i += 1) {
                nodes.push(
                    performanceNode(`n-${i}`, {
                        position: { x: randInt(-500, 500), y: randInt(-500, 500) },
                        width: randInt(20, 320),
                        height: randInt(20, 240),
                    }),
                );
            }
            const movingCount = sample % 3 === 0 ? 2 : 1;
            const initialPositions = nodes.slice(0, movingCount).map((node) => ({
                id: node.id,
                x: node.position.x,
                y: node.position.y,
            }));
            const rawOffset = { x: randInt(-400, 400), y: randInt(-400, 400) };
            const scale = sample % 5 === 0 ? 0.25 : sample % 7 === 0 ? 2 : 1;
            const threshold = sample % 19 === 0 ? 0 : 7 / scale;
            const context = createNodeAlignmentContext(nodes, initialPositions);
            expect(calculateNodeAlignment(context, rawOffset, threshold)).toEqual(
                linearCalculateNodeAlignment(nodes, initialPositions, rawOffset, threshold),
            );
        }
    });

    test("queries 50k targets with logarithmic table lookups instead of linear scan", () => {
        const nodes = performanceProject(50000).nodes;
        const context = createNodeAlignmentContext(nodes, [{ id: nodes[0].id, x: nodes[0].position.x, y: nodes[0].position.y }])!;
        let reads = 0;
        const wrapArray = <T,>(arr: T[]): T[] =>
            new Proxy(arr, {
                get(target, prop, receiver) {
                    if (typeof prop === "string" && /^\d+$/.test(prop)) reads += 1;
                    return Reflect.get(target, prop, receiver);
                },
            });
        const record = context as unknown as Record<string, unknown>;
        if (Array.isArray(record.targets)) {
            record.targets = wrapArray(record.targets);
        }
        if (Array.isArray(record.x)) {
            record.x = (record.x as unknown[][]).map((table) => wrapArray(table));
        }
        if (Array.isArray(record.y)) {
            record.y = (record.y as unknown[][]).map((table) => wrapArray(table));
        }
        calculateNodeAlignment(context, { x: 315, y: 175 }, 7);
        expect(reads).toBeGreaterThan(0);
        expect(reads).toBeLessThan(200);
    });
});
