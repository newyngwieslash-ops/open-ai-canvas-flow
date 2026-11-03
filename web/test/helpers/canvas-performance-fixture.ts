import { CanvasNodeType, type CanvasNodeData } from "../../src/types/canvas";
import type { CanvasProject } from "../../src/stores/canvas/use-canvas-store";

export function performanceNode(id: string, patch: Partial<CanvasNodeData> = {}): CanvasNodeData {
    return { id, type: CanvasNodeType.Text, title: id, position: { x: 0, y: 0 }, width: 320, height: 180, metadata: { content: id }, ...patch };
}

export function performanceProject(count = 100): CanvasProject {
    const nodes = Array.from({ length: count }, (_, index) => performanceNode(`perf-${index}`, {
        position: { x: (index % 50) * 360, y: Math.floor(index / 50) * 220 },
    }));
    return {
        id: "canvas-performance", title: `性能夹具 ${count}`, createdAt: "2026-10-28T00:00:00.000Z", updatedAt: "2026-10-28T00:00:00.000Z",
        nodes, connections: nodes.slice(1).map((node, index) => ({ id: `edge-${index}`, fromNodeId: nodes[index].id, toNodeId: node.id })),
        chatSessions: [], activeChatId: null, backgroundMode: "dots", showImageInfo: false, viewport: { x: 0, y: 0, k: 1 }, previsScenes: [],
    };
}
