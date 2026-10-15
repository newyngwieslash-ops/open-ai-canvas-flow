import { expect, test } from "bun:test";
import { agentCopy, DEFAULT_CANVAS_APPEARANCE } from "../src/lib/canvas/agent-appearance";
import { normalizePublicAppearance } from "../src/stores/use-appearance-store";

test("public appearance converts only the exact legacy Agent name and preserves templates and Live2D", () => {
    for (const [agentName, want] of [["影策", "源铭振跃"], ["星河助手", "星河助手"], ["影策小助手", "影策小助手"]]) {
        const canvas = {
            ...DEFAULT_CANVAS_APPEARANCE, agentName,
            welcomeTitle: "影策和{agentName}", welcomeDescription: "用户影策欢迎文案",
            avatarType: "live2d" as const, live2dResourceId: "custom-model", live2dEntry: "avatar/model.model3.json", avatarHeight: 300,
        };
        const raw = { brandName: "星河工作室", canvas };
        const got = normalizePublicAppearance(raw);
        expect(got.canvas).toEqual({ ...canvas, agentName: want });
        expect(normalizePublicAppearance(got)).toEqual(got);
        expect(raw.canvas.agentName).toBe(agentName);
        expect(agentCopy(got.canvas!.welcomeTitle, got.canvas!.agentName)).toBe(`影策和${want}`);
    }
});

test("missing Canvas configuration uses the current Agent default", () => {
    expect(normalizePublicAppearance({}).canvas?.agentName).toBe("源铭振跃");
    expect(normalizePublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE } }).canvas?.agentName).toBe("源铭振跃");
});
