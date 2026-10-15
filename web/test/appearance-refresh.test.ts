import { expect, test } from "bun:test";
import { bootstrapAppearance, refreshPublicAppearance } from "../src/services/appearance-bootstrap";
import { DEFAULT_CANVAS_APPEARANCE } from "../src/lib/canvas/agent-appearance";
import { commitPublicAppearance, DEFAULT_PUBLIC_APPEARANCE, normalizePublicAppearance, useAppearanceStore } from "../src/stores/use-appearance-store";

test("initial store exposes the new default identity before bootstrap", () => {
    expect(useAppearanceStore.getState().resolved).toBe(false);
    expect(useAppearanceStore.getState().appearance.brandName).toBe("源铭振跃");
    expect(useAppearanceStore.getState().appearance.seoTitle).toBe("源铭振跃");
});

test("failed initial bootstrap commits the new default identity", async () => {
    const appearance = await bootstrapAppearance(async () => { throw new Error("offline"); });
    expect(appearance.brandName).toBe("源铭振跃");
    expect(appearance.seoTitle).toBe("源铭振跃");
    expect(appearance.footerCopyright).toBe(`© ${new Date().getFullYear()} 源铭振跃. All rights reserved.`);
    expect(useAppearanceStore.getState().appearance).toBe(appearance);
    expect(useAppearanceStore.getState().resolved).toBe(true);
});

test("successful bootstrap and refresh normalize raw legacy responses at commit", async () => {
    const raw = {
        ...DEFAULT_PUBLIC_APPEARANCE, brandName: "影策", seoTitle: "影策",
        seoDescription: "影策，面向 AI 影视与短剧创作的工作台。",
        footerCopyright: "© 2025 影策. All rights reserved.", configured: true, revision: "legacy-saved",
    };
    for (const load of [bootstrapAppearance, refreshPublicAppearance]) {
        const appearance = await load(async () => raw);
        expect(appearance?.brandName).toBe("源铭振跃");
        expect(appearance?.seoTitle).toBe("源铭振跃");
        expect(appearance?.footerCopyright).toBe("© 2025 源铭振跃. All rights reserved.");
        expect(appearance?.revision).toBe("legacy-saved");
        expect(useAppearanceStore.getState().appearance).toBe(appearance!);
        expect(raw.brandName).toBe("影策");
    }
});

test("custom bootstrap identity survives a failed refresh without resetting state", async () => {
    const custom = {
        ...DEFAULT_PUBLIC_APPEARANCE, brandName: "HIMA Studio", brandSlug: "hima-studio",
        seoTitle: "定制标题", seoDescription: "自定义介绍", footerCopyright: "团队版权所有", revision: "custom-saved",
    };
    const saved = await bootstrapAppearance(async () => custom);
    expect(saved).toMatchObject(custom);
    expect(await refreshPublicAppearance(async () => { throw new Error("offline"); })).toBeUndefined();
    expect(useAppearanceStore.getState().appearance).toBe(saved);
    expect(useAppearanceStore.getState().resolved).toBe(true);
});

test("appearance refresh synchronizes a changed Agent name", async () => {
    const saved = normalizePublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE, agentName: "星河助手" } });
    await refreshPublicAppearance(async () => saved);
    expect(useAppearanceStore.getState().appearance.canvas?.agentName).toBe("星河助手");
});

test("failed refresh preserves the last saved identity", async () => {
    commitPublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE, agentName: "影绘" } });
    await refreshPublicAppearance(async () => {
        throw new Error("offline");
    });
    expect(useAppearanceStore.getState().appearance.canvas?.agentName).toBe("影绘");
});

test("focus and visibility refresh share one request", async () => {
    let resolve!: (value: ReturnType<typeof normalizePublicAppearance>) => void;
    let calls = 0;
    const pending = new Promise<ReturnType<typeof normalizePublicAppearance>>((done) => {
        resolve = done;
    });
    const fetchAppearance = async () => {
        calls++;
        return pending;
    };
    const first = refreshPublicAppearance(fetchAppearance);
    const second = refreshPublicAppearance(fetchAppearance);
    expect(calls).toBe(1);
    resolve(normalizePublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE, agentName: "星河助手" } }));
    await Promise.all([first, second]);
    expect(useAppearanceStore.getState().appearance.canvas?.agentName).toBe("星河助手");
});

test("a late refresh cannot overwrite a newer admin save", async () => {
    let resolve!: (value: ReturnType<typeof normalizePublicAppearance>) => void;
    const pending = new Promise<ReturnType<typeof normalizePublicAppearance>>((done) => {
        resolve = done;
    });
    const refresh = refreshPublicAppearance(async () => pending);
    commitPublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE, agentName: "最新名称" } });
    resolve(normalizePublicAppearance({ canvas: { ...DEFAULT_CANVAS_APPEARANCE, agentName: "旧名称" } }));
    await refresh;
    expect(useAppearanceStore.getState().appearance.canvas?.agentName).toBe("最新名称");
});
