import { CanvasNodeType, type CanvasNodeData, type Position } from "@/types/canvas";
import { isFrameNode } from "@/lib/canvas/canvas-frame";
import { buildCanvasSpatialIndex, canvasNodeBounds } from "@/lib/canvas/canvas-spatial-index";

export function buildCanvasConnectionHitIndex(nodes: CanvasNodeData[]) {
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const order = new Map(nodes.map((node, index) => [node.id, index]));
    const eligible = nodes.filter((node) => {
        if (isFrameNode(node)) return false;
        const root = node.metadata?.batchRootId ? nodeById.get(node.metadata.batchRootId) : undefined;
        if (root && !root.metadata?.imageBatchExpanded) return false;
        const parent = node.parentId ? nodeById.get(node.parentId) : undefined;
        return !(parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed);
    });
    const dynamic = eligible.filter((node) => node.type === CanvasNodeType.Script || node.type === CanvasNodeType.BatchTable);
    const dynamicIds = new Set(dynamic.map((node) => node.id));
    const index = buildCanvasSpatialIndex(eligible.filter((node) => !dynamicIds.has(node.id)).map((node) => {
        const bounds = canvasNodeBounds(node);
        return { id: node.id, value: node, bounds: { left: bounds.left - 1e-6, top: bounds.top - 1e-6, right: bounds.right + 1e-6, bottom: bounds.bottom + 1e-6 } };
    }));
    return {
        nodeById,
        candidates(point: Position, radius: number) {
            if (!Number.isFinite(point.x) || !Number.isFinite(point.y) || !Number.isFinite(radius) || radius < 0) return [];
            const distance = Math.max(radius, 1e-6);
            const local = index.query({ left: point.x - distance, top: point.y - distance, right: point.x + distance, bottom: point.y + distance });
            return [...local, ...dynamic].sort((a, b) => order.get(b.id)! - order.get(a.id)!);
        },
    };
}
