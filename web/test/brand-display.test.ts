import { describe, expect, test } from "bun:test";

import { displayBrandName, displayBrandTitle, SOURCE_UI_BRAND, UI_BRAND_NAME } from "../src/lib/brand-display";

describe("UI brand display mapping", () => {
    test("maps the legacy default brand to the new wordmark", () => {
        expect(displayBrandName("影策")).toBe(UI_BRAND_NAME);
        expect(UI_BRAND_NAME).toBe("源铭振跃");
        expect(SOURCE_UI_BRAND).toBe("影策");
    });

    test("keeps custom brands untouched", () => {
        expect(displayBrandName("HIMA Studio")).toBe("HIMA Studio");
        expect(displayBrandName("星河映像")).toBe("星河映像");
        expect(displayBrandName("")).toBe("");
    });

    test("maps the legacy default value with surrounding whitespace", () => {
        expect(displayBrandName(" 影策 ")).toBe(UI_BRAND_NAME);
        expect(displayBrandName("影策工作室")).toBe("影策工作室");
    });

    test("falls back to the display brand when the SEO title is empty", () => {
        expect(displayBrandTitle("", "影策")).toBe(UI_BRAND_NAME);
        expect(displayBrandTitle("", "HIMA Studio")).toBe("HIMA Studio");
    });

    test("recognizes the exact legacy default title after the site name is normalized", () => {
        expect(displayBrandTitle("影策", "源铭振跃")).toBe("源铭振跃");
        expect(displayBrandTitle(" 影策 ", "HIMA Studio")).toBe("源铭振跃");
        expect(displayBrandTitle("源铭振跃", "源铭振跃")).toBe("源铭振跃");
    });

    test("does not rewrite custom SEO titles in bulk", () => {
        expect(displayBrandTitle("影策 - 定制长标题", "影策")).toBe("影策 - 定制长标题");
        expect(displayBrandTitle("HIMA Studio - AI 影视工作台", "HIMA Studio")).toBe("HIMA Studio - AI 影视工作台");
        expect(displayBrandTitle("HIMA Studio", "影策")).toBe("HIMA Studio");
    });

    test("never mutates the appearance object it reads", () => {
        const appearance = { brandName: "影策", seoTitle: "影策", footerCopyright: "© 2026 影策. All rights reserved." };
        const snapshot = { ...appearance };
        expect(displayBrandName(appearance.brandName)).toBe(UI_BRAND_NAME);
        expect(displayBrandTitle(appearance.seoTitle, appearance.brandName)).toBe(UI_BRAND_NAME);
        expect(appearance).toEqual(snapshot);
    });
});
