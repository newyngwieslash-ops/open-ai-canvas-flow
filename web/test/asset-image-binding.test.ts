import { describe, expect, test } from "bun:test";

import { IMAGE_BINDING_CATEGORIES, assetSupportsImageBinding } from "../src/pages/projects/detail/asset-image-binding";
import type { ProjectAsset } from "../src/services/api/projects";

const asset = (category: string): ProjectAsset =>
    ({
        id: `asset-${category}`,
        title: category,
        mediaType: "text",
        category,
        status: "confirmed",
        versionCount: 1,
        usages: [],
        updatedAt: "2026-10-05T00:00:00Z",
    }) as unknown as ProjectAsset;

describe("资产图片绑定入口分流", () => {
    test("角色、场景、道具、材料、其他可绑定图片", () => {
        for (const category of ["character", "environment", "prop", "material", "other"]) {
            expect(assetSupportsImageBinding(asset(category))).toBe(true);
        }
    });

    test("白名单与实现一致", () => {
        expect([...IMAGE_BINDING_CATEGORIES].sort()).toEqual(["character", "environment", "material", "other", "prop"]);
    });
});
