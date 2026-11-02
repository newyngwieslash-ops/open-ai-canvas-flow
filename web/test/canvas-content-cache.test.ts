import { expect, test } from "bun:test";
import { canvasContentHash, canvasContentSnapshot, sameCanvasContent } from "../src/lib/canvas/canvas-content";
import { performanceProject } from "./helpers/canvas-performance-fixture";

test("viewport and revision copies reuse the snapshot and hash Promise", async () => {
    const base = performanceProject(1000);
    const view = { ...base, viewport: { x: 20, y: 40, k: 0.5 }, revision: 5 };
    expect(canvasContentSnapshot(view).nodes).toBe(canvasContentSnapshot(base).nodes);
    const hash = canvasContentHash(base);
    expect(canvasContentHash(view)).toBe(hash);
    expect(await canvasContentHash(view)).toBe(await hash);
});

test("bookkeeping stays ignored while real content invalidates", async () => {
    const base = performanceProject(2);
    const stamped = { ...base, nodes: base.nodes.map((node) => ({ ...node, updatedAt: "2026-10-28T01:00:00.000Z" })) };
    expect(sameCanvasContent(base, stamped)).toBe(true);
    expect(await canvasContentHash(stamped)).toBe(await canvasContentHash(base));
    const changed = { ...base, nodes: [{ ...base.nodes[0], title: "已编辑" }, base.nodes[1]] };
    expect(sameCanvasContent(base, changed)).toBe(false);
    expect(await canvasContentHash(changed)).not.toBe(await canvasContentHash(base));
    expect(canvasContentSnapshot(changed).nodes[1]).toBe(canvasContentSnapshot(base).nodes[1]);
});

test("connections, chatSessions, title, and projectId changes invalidate hash", async () => {
    const base = performanceProject(2);
    const baseHash = canvasContentHash(base);
    const baseDigest = await baseHash;

    const withConnections = { ...base, connections: [] };
    expect(sameCanvasContent(base, withConnections)).toBe(false);
    expect(canvasContentHash(withConnections)).not.toBe(baseHash);
    expect(await canvasContentHash(withConnections)).not.toBe(baseDigest);

    const withChat = {
        ...base,
        chatSessions: [{ id: "chat-1", title: "对话", createdAt: "2026-10-28T00:00:00.000Z", updatedAt: "2026-10-28T00:00:00.000Z", messages: [] }],
    };
    expect(sameCanvasContent(base, withChat)).toBe(false);
    expect(await canvasContentHash(withChat)).not.toBe(baseDigest);

    const withTitle = { ...base, title: "新标题" };
    expect(sameCanvasContent(base, withTitle)).toBe(false);
    expect(await canvasContentHash(withTitle)).not.toBe(baseDigest);

    const withProjectId = { ...base, projectId: "project-123" };
    expect(sameCanvasContent(base, withProjectId)).toBe(false);
    expect(await canvasContentHash(withProjectId)).not.toBe(baseDigest);
});
