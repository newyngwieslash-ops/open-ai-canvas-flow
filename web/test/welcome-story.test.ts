import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { getWelcomeChapters, getWelcomeLook, showcases, welcomeLooks } from "../src/pages/welcome/story";

const publicFile = (url: string) => resolve(import.meta.dir, "../public", url.replace(/^\//, ""));

describe("welcome story", () => {
    test("resolves the opening chapter title from the displayed brand", () => {
        const chapters = getWelcomeChapters("源铭振跃");
        expect(chapters).toHaveLength(6);
        expect(chapters[0].title).toBe("源铭振跃");
        expect(chapters[0].subtitle).toBe("让一个故事，从文字走向银幕。");
        expect(chapters[0].label).toBe("序幕");
        // 其余五章标题与素材版本保持原样，不随品牌变化。
        expect(chapters.map((chapter) => chapter.id)).toEqual(["opening", "story", "world", "shots", "motion", "canvas"]);
        expect(chapters[1].title).toBe("一念，成故事。");
        expect(getWelcomeChapters("HIMA Studio")[0].title).toBe("HIMA Studio");
    });

    test("uses only the three approved looks", () => {
        expect(welcomeLooks.map((look) => look.id)).toEqual(["spring", "charge", "wing-it"]);
        expect(getWelcomeLook("").id).toBe("spring");
        expect(getWelcomeLook("?look=unknown").id).toBe("spring");
        for (const look of welcomeLooks) expect(getWelcomeLook(`?look=${look.id}`)).toBe(look);
    });

    test("every reel slot has local media and screenplay", () => {
        for (const look of welcomeLooks) {
            expect(look.frames).toHaveLength(12);
            expect(look.screenplay).toHaveLength(12);
            expect(look.screenplay.every((line) => line.trim().length > 0)).toBe(true);
            for (const frame of look.frames) expect(existsSync(publicFile(frame))).toBe(true);
            if (look.video) expect(existsSync(publicFile(look.video))).toBe(true);
        }
        expect(welcomeLooks.filter((look) => look.video).map((look) => look.id)).toEqual(["charge"]);
        for (const showcase of showcases) expect(existsSync(publicFile(showcase.image))).toBe(true);
    });

    test("credits cover all looks separately from the code license", () => {
        const credits = readFileSync(publicFile("/welcome/credits.html"), "utf8");
        expect(credits).toContain("https://creativecommons.org/licenses/by/4.0/");
        for (const look of welcomeLooks) expect(credits).toContain(`id="${look.id}"`);
        expect(credits).not.toContain("竹影");
        expect(existsSync(publicFile("/welcome/sequence.mp4"))).toBe(false);
    });
});
