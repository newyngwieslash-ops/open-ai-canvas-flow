import { describe, expect, test } from "bun:test";

import { dropConvergedCreateNodeOperations } from "@/lib/canvas/canvas-task-mutation";
import type { CanvasOperationsRequest } from "@/services/api/user-data";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

function canvas(nodes: CanvasProject["nodes"]): CanvasProject {
    return {
        id: "canvas",
        title: "Canvas",
        createdAt: "",
        updatedAt: "",
        nodes,
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "dots",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        previsScenes: [],
    } as unknown as CanvasProject;
}

const node = (id: string, content: string) => ({ id, type: "text", metadata: { content } }) as CanvasProject["nodes"][number];

function request(operations: CanvasOperationsRequest["operations"]): CanvasOperationsRequest {
    return { mutationId: "original-id", baseRevision: 4, operations };
}

describe("dropConvergedCreateNodeOperations", () => {
    test("drops create_node ops whose node id already exists on the remote document", () => {
        const original = request([{ type: "create_node", node: node("text-1", "hello") }, { type: "patch_node", id: "text-2", patch: { x: 7 }, before: { x: 3 } }]);
        const saved = structuredClone(original);
        const remote = canvas([node("text-1", "server copy")]);
        const result = dropConvergedCreateNodeOperations(original, remote);
        expect(result).toBeDefined();
        expect(result?.operations).toEqual([original.operations[1]]);
        // 输入请求不被原地修改。
        expect(original).toEqual(saved);
    });

    test("returns the request unchanged when every create_node is still missing remotely", () => {
        const original = request([{ type: "create_node", node: node("text-1", "hello") }]);
        const remote = canvas([node("text-9", "other")]);
        expect(dropConvergedCreateNodeOperations(original, remote)).toBe(original);
    });

    test("keeps non-create operations and returns undefined when nothing is dropped", () => {
        const original = request([{ type: "patch_node", id: "text-2", patch: { x: 7 }, before: { x: 3 } }, { type: "set_field", field: "title", value: "new", beforeValue: "old" }]);
        expect(dropConvergedCreateNodeOperations(original, canvas([]))).toBe(original);
    });

    test("may produce an empty operation list for an all-create batch", () => {
        const original = request([{ type: "create_node", node: node("text-1", "hello") }]);
        const result = dropConvergedCreateNodeOperations(original, canvas([node("text-1", "server copy")]));
        expect(result).toBeDefined();
        expect(result?.operations).toEqual([]);
    });

    test("never drops anything without a remote document", () => {
        const original = request([{ type: "create_node", node: node("text-1", "hello") }]);
        expect(dropConvergedCreateNodeOperations(original, undefined)).toBe(original);
    });
});
