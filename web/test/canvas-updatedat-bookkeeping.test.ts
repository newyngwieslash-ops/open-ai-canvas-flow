import { describe, expect, test } from "bun:test";

import { buildCanvasOperations } from "@/services/user-data-sync";
import { sameCanvasContent } from "@/lib/canvas/canvas-content";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

function canvas(nodes: CanvasProject["nodes"]): CanvasProject {
    return {
        id: "canvas",
        title: "自由画布 1",
        createdAt: "",
        updatedAt: "",
        nodes,
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "dots" as CanvasProject["backgroundMode"],
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        previsScenes: [],
    };
}

describe("节点 updatedAt 簿记字段不参与同步", () => {
    test("仅节点 updatedAt 不同视为内容相同", () => {
        const base = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T14:19:13.817Z" } as CanvasProject["nodes"][number]]);
        const restamped = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T15:23:31.435Z" } as CanvasProject["nodes"][number]]);
        expect(sameCanvasContent(base, restamped)).toBe(true);
    });

    test("节点真实字段变化仍然判脏", () => {
        const base = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T14:19:13.817Z" } as CanvasProject["nodes"][number]]);
        const edited = canvas([{ id: "image", type: "image", title: "图2", updatedAt: "2026-10-20T14:19:13.817Z" } as CanvasProject["nodes"][number]]);
        expect(sameCanvasContent(base, edited)).toBe(false);
    });

    test("仅节点 updatedAt 变化不生成保存操作", () => {
        const before = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T14:19:13.817Z" } as CanvasProject["nodes"][number]]);
        const after = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T15:30:05.254Z" } as CanvasProject["nodes"][number]]);
        expect(buildCanvasOperations(before, after)).toEqual([]);
    });

    test("真实字段混合 updatedAt 变化时保留真实字段", () => {
        const before = canvas([{ id: "image", type: "image", title: "图1", updatedAt: "2026-10-20T14:19:13.817Z" } as CanvasProject["nodes"][number]]);
        const after = canvas([{ id: "image", type: "image", title: "图2", updatedAt: "2026-10-20T15:30:05.254Z" } as CanvasProject["nodes"][number]]);
        const operations = buildCanvasOperations(before, after);
        expect(operations).toHaveLength(1);
        expect(operations[0]).toMatchObject({ type: "patch_node", id: "image" });
        expect(operations[0].patch).toEqual({ title: "图2" });
    });
});
