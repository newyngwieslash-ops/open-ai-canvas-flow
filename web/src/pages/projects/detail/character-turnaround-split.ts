import type {
    CharacterRepresentation,
    TurnaroundConfirmedCropBox,
    TurnaroundCropRect,
    TurnaroundSplitPlanDraft,
    TurnaroundViewRole,
} from "@/services/api/projects";

export const TURNAROUND_VIEW_ROLES: readonly TurnaroundViewRole[] = ["front", "side", "back"] as const;

export const TURNAROUND_VIEW_LABELS: Record<TurnaroundViewRole, string> = {
    front: "正面",
    side: "侧面",
    back: "背面",
};

export function findTurnaroundSheetRepresentation(representations?: CharacterRepresentation[]): CharacterRepresentation | undefined {
    return representations?.find((rep) => rep.role === "turnaround_sheet" && Boolean(rep.resourceId));
}

export function resolveCharacterDefaultReferenceRepresentation(representations?: CharacterRepresentation[]): CharacterRepresentation | undefined {
    return (
        representations?.find((rep) => rep.role === "turnaround_sheet")
        || representations?.find((rep) => rep.role === "primary")
        || representations?.find((rep) => rep.role === "front")
        || representations?.[0]
    );
}

export function buildDefaultEqualThirdsBoxes(sourceWidth: number, sourceHeight: number, confirmed = false): Record<TurnaroundViewRole, TurnaroundConfirmedCropBox> {
    const safeW = Math.max(3, Math.floor(sourceWidth));
    const safeH = Math.max(1, Math.floor(sourceHeight));
    const third = Math.floor(safeW / 3);
    return {
        front: {
            role: "front",
            x: 0,
            y: 0,
            width: third,
            height: safeH,
            confirmed,
        },
        side: {
            role: "side",
            x: third,
            y: 0,
            width: third,
            height: safeH,
            confirmed,
        },
        back: {
            role: "back",
            x: third * 2,
            y: 0,
            width: safeW - third * 2,
            height: safeH,
            confirmed,
        },
    };
}

export function clampSourceCropRect(rect: TurnaroundCropRect, sourceWidth: number, sourceHeight: number): TurnaroundCropRect {
    const maxW = Math.max(1, Math.floor(sourceWidth));
    const maxH = Math.max(1, Math.floor(sourceHeight));
    const x = Math.max(0, Math.min(maxW - 1, Math.round(rect.x)));
    const y = Math.max(0, Math.min(maxH - 1, Math.round(rect.y)));
    const width = Math.max(1, Math.min(maxW - x, Math.round(rect.width)));
    const height = Math.max(1, Math.min(maxH - y, Math.round(rect.height)));
    return { x, y, width, height };
}

export function scalePreviewToSourceRect(
    previewRect: TurnaroundCropRect,
    previewWidth: number,
    previewHeight: number,
    sourceWidth: number,
    sourceHeight: number,
): TurnaroundCropRect {
    if (previewWidth <= 0 || previewHeight <= 0 || sourceWidth <= 0 || sourceHeight <= 0) {
        return { x: 0, y: 0, width: 1, height: 1 };
    }
    const scaleX = sourceWidth / previewWidth;
    const scaleY = sourceHeight / previewHeight;
    return clampSourceCropRect(
        {
            x: Math.round(previewRect.x * scaleX),
            y: Math.round(previewRect.y * scaleY),
            width: Math.round(previewRect.width * scaleX),
            height: Math.round(previewRect.height * scaleY),
        },
        sourceWidth,
        sourceHeight,
    );
}

export function scaleSourceToPreviewRect(
    sourceRect: TurnaroundCropRect,
    sourceWidth: number,
    sourceHeight: number,
    previewWidth: number,
    previewHeight: number,
): TurnaroundCropRect {
    if (sourceWidth <= 0 || sourceHeight <= 0 || previewWidth <= 0 || previewHeight <= 0) {
        return { x: 0, y: 0, width: 0, height: 0 };
    }
    const scaleX = previewWidth / sourceWidth;
    const scaleY = previewHeight / sourceHeight;
    return {
        x: Math.round(sourceRect.x * scaleX),
        y: Math.round(sourceRect.y * scaleY),
        width: Math.round(sourceRect.width * scaleX),
        height: Math.round(sourceRect.height * scaleY),
    };
}

export function nudgeTurnaroundCropBox(
    box: TurnaroundConfirmedCropBox,
    delta: { dx: number; dy: number },
    sourceWidth: number,
    sourceHeight: number,
    mode: "move" | "resize" = "move",
): TurnaroundConfirmedCropBox {
    const maxW = Math.max(1, Math.floor(sourceWidth));
    const maxH = Math.max(1, Math.floor(sourceHeight));
    if (mode === "resize") {
        const nextWidth = Math.max(1, Math.min(maxW - box.x, Math.round(box.width + delta.dx)));
        const nextHeight = Math.max(1, Math.min(maxH - box.y, Math.round(box.height + delta.dy)));
        return {
            ...box,
            width: nextWidth,
            height: nextHeight,
        };
    }
    const nextX = Math.max(0, Math.min(maxW - box.width, Math.round(box.x + delta.dx)));
    const nextY = Math.max(0, Math.min(maxH - box.height, Math.round(box.y + delta.dy)));
    return {
        ...box,
        x: nextX,
        y: nextY,
    };
}

export function boxesOverlap(a: TurnaroundCropRect, b: TurnaroundCropRect): boolean {
    return a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
}

export type TurnaroundValidationResult = {
    canSave: boolean;
    reason?: string;
    orderedBoxes: TurnaroundConfirmedCropBox[];
};

export function validateTurnaroundSplitBoxes(
    boxes: TurnaroundConfirmedCropBox[],
    sourceWidth: number,
    sourceHeight: number,
): TurnaroundValidationResult {
    if (sourceWidth <= 0 || sourceHeight <= 0) {
        return { canSave: false, reason: "源图真实像素尺寸尚未就绪", orderedBoxes: [] };
    }
    if (boxes.length !== 3) {
        return { canSave: false, reason: "必须同时配置正面、侧面、背面三个视角裁框", orderedBoxes: [] };
    }
    const byRole = new Map<TurnaroundViewRole, TurnaroundConfirmedCropBox>();
    for (const box of boxes) {
        if (!TURNAROUND_VIEW_ROLES.includes(box.role)) {
            return { canSave: false, reason: "存在无效的视角标签", orderedBoxes: [] };
        }
        if (byRole.has(box.role)) {
            return { canSave: false, reason: `视角「${TURNAROUND_VIEW_LABELS[box.role]}」重复分配`, orderedBoxes: [] };
        }
        if (
            !Number.isInteger(box.x)
            || !Number.isInteger(box.y)
            || !Number.isInteger(box.width)
            || !Number.isInteger(box.height)
            || box.x < 0
            || box.y < 0
            || box.width <= 0
            || box.height <= 0
            || box.x + box.width > sourceWidth
            || box.y + box.height > sourceHeight
        ) {
            return { canSave: false, reason: `视角「${TURNAROUND_VIEW_LABELS[box.role]}」裁框超出源图像素边界`, orderedBoxes: [] };
        }
        byRole.set(box.role, box);
    }
    for (const role of TURNAROUND_VIEW_ROLES) {
        if (!byRole.has(role)) {
            return { canSave: false, reason: `缺少「${TURNAROUND_VIEW_LABELS[role]}」视角裁框`, orderedBoxes: [] };
        }
    }
    const orderedBoxes = TURNAROUND_VIEW_ROLES.map((role) => byRole.get(role)!);
    for (let i = 0; i < orderedBoxes.length; i++) {
        for (let j = i + 1; j < orderedBoxes.length; j++) {
            if (boxesOverlap(orderedBoxes[i], orderedBoxes[j])) {
                return {
                    canSave: false,
                    reason: `「${TURNAROUND_VIEW_LABELS[orderedBoxes[i].role]}」与「${TURNAROUND_VIEW_LABELS[orderedBoxes[j].role]}」裁框存在重叠区域，请调整为互不重叠`,
                    orderedBoxes,
                };
            }
        }
    }
    for (const box of orderedBoxes) {
        if (!box.confirmed) {
            return {
                canSave: false,
                reason: `请先确认「${TURNAROUND_VIEW_LABELS[box.role]}」视角裁框与标签后再保存`,
                orderedBoxes,
            };
        }
    }
    return { canSave: true, orderedBoxes };
}

export function applyVisionPlanDraftToBoxes(
    draft: TurnaroundSplitPlanDraft,
    sourceWidth: number,
    sourceHeight: number,
): {
    boxesByRole: Record<TurnaroundViewRole, TurnaroundConfirmedCropBox>;
    notesByRole: Partial<Record<TurnaroundViewRole, string>>;
    requiresManualReview: boolean;
    manualReason?: string;
} {
    const defaults = buildDefaultEqualThirdsBoxes(sourceWidth, sourceHeight, false);
    const notesByRole: Partial<Record<TurnaroundViewRole, string>> = {};
    for (const view of draft.views || []) {
        if (!TURNAROUND_VIEW_ROLES.includes(view.role)) continue;
        if (view.note) {
            notesByRole[view.role] = view.note;
        }
        if (view.box) {
            const clamped = clampSourceCropRect(view.box, sourceWidth, sourceHeight);
            defaults[view.role] = {
                role: view.role,
                ...clamped,
                // AI suggestions NEVER auto-confirm; user must explicitly review & confirm.
                confirmed: false,
            };
        }
    }
    return {
        boxesByRole: defaults,
        notesByRole,
        requiresManualReview: draft.requiresManualReview || (draft.views || []).length < 3,
        manualReason: draft.manualReason,
    };
}
