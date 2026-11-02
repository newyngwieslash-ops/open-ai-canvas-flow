import { expect, test } from "bun:test";
import { performanceProject } from "./helpers/canvas-performance-fixture";

test("performance fixtures have deterministic IDs and valid endpoints", () => {
    const project = performanceProject(2000);
    const ids = new Set(project.nodes.map((node) => node.id));
    expect(ids.size).toBe(2000);
    expect(project.connections).toHaveLength(1999);
    expect(project.connections.every((edge) => ids.has(edge.fromNodeId) && ids.has(edge.toNodeId))).toBe(true);
    expect(project).toEqual(performanceProject(2000));
});
