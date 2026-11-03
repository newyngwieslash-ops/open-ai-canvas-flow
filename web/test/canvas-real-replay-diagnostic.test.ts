import { readFileSync } from "node:fs";
import { expect, test } from "bun:test";
import { applyAgentCanvasPatch, mergeAgentCanvasEditor, type AgentCanvasPatch } from "@/lib/canvas/agent-canvas-patch";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { CanvasNodeData } from "@/types/canvas";

test("真实三步 SSE 序列可从基线重建并与终态本地收敛", () => {
    const steps = JSON.parse(readFileSync("../.local/canvas-conflict-steps.json", "utf8")) as AgentCanvasPatch[];
    const baseline: CanvasProject = { id: steps[0]!.canvasId, title: "画布", nodes: [], connections: [], chatSessions: [], activeChatId: null, viewport: { x: 0, y: 0, k: 1 }, createdAt: "", updatedAt: "" };
    let remote = baseline;
    for (const step of steps) remote = applyAgentCanvasPatch(remote, step);
    const finalNode = remote.nodes.find((node) => node.id === "img-fig1-ermaduo-v2");
    expect(finalNode?.metadata?.taskStatus).toBe("failed");
    const local: CanvasProject = { ...baseline, nodes: [finalNode as CanvasNodeData] };
    const merged = mergeAgentCanvasEditor(baseline, remote, local.nodes, local.connections, local);
    expect(merged.nodes.find((node) => node.id === "img-fig1-ermaduo-v2")?.metadata?.taskStatus).toBe("failed");
});
