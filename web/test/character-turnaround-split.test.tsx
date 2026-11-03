import { describe, expect, test } from "bun:test";

import {
    applyVisionPlanDraftToBoxes,
    boxesOverlap,
    buildDefaultEqualThirdsBoxes,
    findTurnaroundSheetRepresentation,
    nudgeTurnaroundCropBox,
    resolveCharacterDefaultReferenceRepresentation,
    scalePreviewToSourceRect,
    scaleSourceToPreviewRect,
    validateTurnaroundSplitBoxes,
} from "@/pages/projects/detail/character-turnaround-split";
import { projectCharacterCover } from "@/pages/projects/detail/project-character-cover";

describe("character turnaround split & view labeling", () => {
    test("only allows turnaround_sheet representation as split source and rejects ordinary portrait", () => {
        const portraitOnly = [
            { id: "rep-1", assetVersionId: "v1", resourceId: "res-portrait", mediaType: "image", role: "primary", metadata: {}, createdAt: "" },
        ];
        expect(findTurnaroundSheetRepresentation(portraitOnly)).toBeUndefined();

        const withTurnaroundSheet = [
            ...portraitOnly,
            { id: "rep-2", assetVersionId: "v1", resourceId: "res-sheet", mediaType: "image", role: "turnaround_sheet", metadata: {}, createdAt: "" },
        ];
        expect(findTurnaroundSheetRepresentation(withTurnaroundSheet)?.resourceId).toBe("res-sheet");
    });

    test("preserves default character reference priority (turnaround_sheet -> primary -> front) after splitting", () => {
        const afterSplitWithTurnaround = [
            { id: "rep-front", assetVersionId: "v2", resourceId: "res-front", mediaType: "image", role: "front", metadata: {}, createdAt: "" },
            { id: "rep-side", assetVersionId: "v2", resourceId: "res-side", mediaType: "image", role: "side", metadata: {}, createdAt: "" },
            { id: "rep-back", assetVersionId: "v2", resourceId: "res-back", mediaType: "image", role: "back", metadata: {}, createdAt: "" },
            { id: "rep-sheet", assetVersionId: "v2", resourceId: "res-sheet", mediaType: "image", role: "turnaround_sheet", metadata: {}, createdAt: "" },
            { id: "rep-primary", assetVersionId: "v2", resourceId: "res-primary", mediaType: "image", role: "primary", metadata: {}, createdAt: "" },
        ];
        expect(resolveCharacterDefaultReferenceRepresentation(afterSplitWithTurnaround)?.resourceId).toBe("res-sheet");
        expect(projectCharacterCover(afterSplitWithTurnaround)?.resourceId).toBe("res-sheet");

        const withoutSheetButPrimary = afterSplitWithTurnaround.filter((r) => r.role !== "turnaround_sheet");
        expect(resolveCharacterDefaultReferenceRepresentation(withoutSheetButPrimary)?.resourceId).toBe("res-primary");
        expect(projectCharacterCover(withoutSheetButPrimary)?.resourceId).toBe("res-primary");

        const onlySplitViews = withoutSheetButPrimary.filter((r) => r.role !== "primary");
        expect(resolveCharacterDefaultReferenceRepresentation(onlySplitViews)?.resourceId).toBe("res-front");
        expect(projectCharacterCover(onlySplitViews)?.resourceId).toBe("res-front");
    });

    test("scales between preview CSS dimensions and integer source pixel coordinates with boundary clamping", () => {
        const sourceRect = scalePreviewToSourceRect({ x: 50, y: 10, width: 100, height: 80 }, 300, 100, 1500, 500);
        expect(sourceRect).toEqual({ x: 250, y: 50, width: 500, height: 400 });

        const previewRect = scaleSourceToPreviewRect(sourceRect, 1500, 500, 300, 100);
        expect(previewRect).toEqual({ x: 50, y: 10, width: 100, height: 80 });

        const clamped = scalePreviewToSourceRect({ x: -20, y: -10, width: 500, height: 200 }, 300, 100, 300, 100);
        expect(clamped).toEqual({ x: 0, y: 0, width: 300, height: 100 });
    });

    test("supports keyboard arrow nudging for move and resize within source pixel bounds", () => {
        const defaults = buildDefaultEqualThirdsBoxes(300, 100, false);
        const moved = nudgeTurnaroundCropBox(defaults.front, { dx: 10, dy: 5 }, 300, 100, "move");
        expect(moved).toEqual({ role: "front", x: 10, y: 0, width: 100, height: 100, confirmed: false });

        const resized = nudgeTurnaroundCropBox(defaults.front, { dx: -15, dy: -20 }, 300, 100, "resize");
        expect(resized).toEqual({ role: "front", x: 0, y: 0, width: 85, height: 80, confirmed: false });
    });

    test("blocks saving until all 3 roles (front/side/back) are non-overlapping, in-bounds, and explicitly confirmed", () => {
        const unconfirmed = Object.values(buildDefaultEqualThirdsBoxes(300, 100, false));
        const resUnconfirmed = validateTurnaroundSplitBoxes(unconfirmed, 300, 100);
        expect(resUnconfirmed.canSave).toBe(false);
        expect(resUnconfirmed.reason).toContain("请先确认");

        const confirmed = Object.values(buildDefaultEqualThirdsBoxes(300, 100, true));
        const resConfirmed = validateTurnaroundSplitBoxes(confirmed, 300, 100);
        expect(resConfirmed.canSave).toBe(true);
        expect(resConfirmed.orderedBoxes.map((b) => b.role)).toEqual(["front", "side", "back"]);

        const overlapping = [
            { role: "front" as const, x: 0, y: 0, width: 120, height: 100, confirmed: true },
            { role: "side" as const, x: 100, y: 0, width: 100, height: 100, confirmed: true },
            { role: "back" as const, x: 200, y: 0, width: 100, height: 100, confirmed: true },
        ];
        expect(boxesOverlap(overlapping[0], overlapping[1])).toBe(true);
        const resOverlap = validateTurnaroundSplitBoxes(overlapping, 300, 100);
        expect(resOverlap.canSave).toBe(false);
        expect(resOverlap.reason).toContain("重叠");
    });

    test("vision plan draft never auto-confirms boxes and flags incomplete drafts for manual review", () => {
        const applied = applyVisionPlanDraftToBoxes(
            {
                views: [
                    { role: "front", box: { x: 5, y: 5, width: 90, height: 90 }, note: "正面清晰" },
                    { role: "side", box: { x: 105, y: 5, width: 90, height: 90 }, note: "侧面清晰" },
                ],
                manualReason: "未能识别背面视角，请人工框选确认",
                requiresManualReview: true,
            },
            300,
            100,
        );
        expect(applied.requiresManualReview).toBe(true);
        expect(applied.manualReason).toContain("未能识别背面视角");
        expect(applied.boxesByRole.front.confirmed).toBe(false);
        expect(applied.boxesByRole.side.confirmed).toBe(false);
        expect(applied.boxesByRole.back.confirmed).toBe(false);
        expect(validateTurnaroundSplitBoxes(Object.values(applied.boxesByRole), 300, 100).canSave).toBe(false);
    });
});
