import { describe, expect, test } from "bun:test";
import * as agentPatchModule from "../src/lib/canvas/agent-canvas-patch";
import { applyAgentCanvasPatch, type AgentCanvasPatch } from "../src/lib/canvas/agent-canvas-patch";
import { performanceProject } from "./helpers/canvas-performance-fixture";

const getBatchApply = () => {
    const fn = (agentPatchModule as Record<string, unknown>).applyAgentCanvasPatchBatch as
        | ((project: ReturnType<typeof performanceProject>, patches: AgentCanvasPatch[]) => ReturnType<typeof performanceProject>)
        | undefined;
    if (!fn) throw new Error("applyAgentCanvasPatchBatch is not exported");
    return fn;
};

describe("agent canvas patch batch", () => {
    test("batch application matches ordered single patch semantics and builds node index once", () => {
        const applyAgentCanvasPatchBatch = getBatchApply();
        const base = performanceProject(1000);
        const patches: AgentCanvasPatch[] = base.nodes.slice(0, 50).map((node, index) => ({
            canvasId: base.id,
            baseRevision: index,
            revision: index + 1,
            updatedAt: `update-${index}`,
            nodes: [{ before: node, after: { ...node, title: `${node.title}-done` } }],
            connections: [],
        }));
        const expected = patches.reduce((project, patch) => applyAgentCanvasPatch(project, patch), base);
        expect(applyAgentCanvasPatchBatch(base, patches)).toEqual(expected);
        expect(base.nodes[0]?.title).toBe("perf-0");

        // Verify 50 pure-update patches do not scan all 1000 nodes 50 times
        let fullNodeReads = 0;
        const trackedNodes = new Proxy(base.nodes, {
            get(target, prop, receiver) {
                if (typeof prop === "string" && /^\d+$/.test(prop)) fullNodeReads += 1;
                return Reflect.get(target, prop, receiver);
            },
        });
        const result = applyAgentCanvasPatchBatch({ ...base, nodes: trackedNodes }, patches);
        expect(result.nodes[0]?.title).toBe("perf-0-done");
        // 1 pass to build Map (1000) + 1 pass to materialize array (1000) = 2000 reads (vs 50,000+ for 50 patches)
        expect(fullNodeReads).toBeLessThanOrEqual(2500);
    });

    test("handles repeated updates to the same node, terminal task protection, and concurrent local edits", () => {
        const applyAgentCanvasPatchBatch = getBatchApply();
        const base = performanceProject(10);
        const node0 = {
            ...base.nodes[0]!,
            metadata: {
                ...base.nodes[0]!.metadata,
                taskId: "task-1",
                status: "loading" as const,
                taskStatus: "running",
                taskProgress: 10,
                taskStage: "step-1",
            },
        };
        const node1 = {
            ...base.nodes[1]!,
            metadata: {
                ...base.nodes[1]!.metadata,
                taskId: "task-2",
                status: "success" as const,
                taskStatus: "succeeded",
                taskProgress: 100,
            },
        };
        const localProject = {
            ...base,
            nodes: [
                // User concurrently dragged and renamed node0 locally while task was running
                { ...node0, title: "local-renamed", position: { x: 999, y: 888 }, metadata: { ...node0.metadata, taskProgress: 25 } },
                node1,
                ...base.nodes.slice(2),
            ],
        };

        const step1After = {
            ...node0,
            metadata: { ...node0.metadata, taskProgress: 50, taskStage: "step-2" },
        };
        const step2After = {
            ...step1After,
            metadata: { ...step1After.metadata, status: "success" as const, taskStatus: "succeeded", taskProgress: 100, taskStage: "done" },
        };
        const staleNode1Before = {
            ...node1,
            metadata: { ...node1.metadata, status: "loading" as const, taskStatus: "running", taskProgress: 40 },
        };
        const staleNode1After = {
            ...node1,
            metadata: { ...node1.metadata, status: "loading" as const, taskStatus: "running", taskProgress: 60 },
        };

        const patches: AgentCanvasPatch[] = [
            {
                canvasId: base.id,
                baseRevision: 1,
                revision: 2,
                updatedAt: "t-1",
                nodes: [
                    { before: node0, after: step1After },
                    { before: staleNode1Before, after: staleNode1After },
                ],
                connections: [],
            },
            {
                canvasId: base.id,
                baseRevision: 2,
                revision: 3,
                updatedAt: "t-2",
                nodes: [{ before: step1After, after: step2After }],
                connections: [],
            },
        ];

        const expected = patches.reduce((project, patch) => applyAgentCanvasPatch(project, patch), localProject);
        const actual = applyAgentCanvasPatchBatch(localProject, patches);
        expect(actual).toEqual(expected);
        expect(actual.nodes[0]?.title).toBe("local-renamed");
        expect(actual.nodes[0]?.position).toEqual({ x: 999, y: 888 });
        expect(actual.nodes[0]?.metadata?.taskStatus).toBe("succeeded");
        // Terminal node1 must not regress to running
        expect(actual.nodes[1]?.metadata?.taskStatus).toBe("succeeded");
    });

    test("conflict on second patch throws without mutating input project", () => {
        const applyAgentCanvasPatchBatch = getBatchApply();
        const base = performanceProject(10);
        const node0 = base.nodes[0]!;
        const node1 = {
            ...base.nodes[1]!,
            metadata: { ...base.nodes[1]!.metadata, taskId: "local-task-new" },
        };
        const project = {
            ...base,
            nodes: [node0, node1, ...base.nodes.slice(2)],
        };
        const snapshot = structuredClone(project);

        const patches: AgentCanvasPatch[] = [
            {
                canvasId: base.id,
                baseRevision: 1,
                revision: 2,
                updatedAt: "t-1",
                nodes: [{ before: node0, after: { ...node0, title: "ok-1" } }],
                connections: [],
            },
            {
                canvasId: base.id,
                baseRevision: 2,
                revision: 3,
                updatedAt: "t-2",
                nodes: [
                    {
                        before: { ...node1, metadata: { ...node1.metadata, taskId: "old-task" } },
                        after: { ...node1, metadata: { ...node1.metadata, taskId: "old-task", taskProgress: 80 } },
                    },
                ],
                connections: [],
            },
        ];

        expect(() => applyAgentCanvasPatchBatch(project, patches)).toThrow("画布增量冲突");
        expect(project).toEqual(snapshot);
    });
});
