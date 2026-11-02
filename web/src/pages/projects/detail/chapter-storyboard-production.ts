import type { StoryboardRow } from "@/types/canvas";
import { formatShotOrdinal } from "@/lib/shot-label";
import { ApiError } from "@/services/api/request";
import { replaceProjectUnitShots, resolveGeneratedProjectUnitShotAssetBindings, type BindingIssue, type GeneratedBindingBaseline, type GeneratedBindingResolution, type ProjectDetail, type ShotRevisionInput } from "@/services/api/projects";
import { ensureShotAssetMentionPrompt } from "./workflow-shot-references";
import { getActiveUserScope } from "@/lib/user-scope";
import { useUserStore } from "@/stores/use-user-store";
import { batchHasUnsettledItems, resolveBatchStorage, withBatchChapterLock } from "./workflow-batch-storage";

export type ChapterStoryboardAsset = {
    id: string;
    title: string;
    type: "image" | "video" | "audio" | "character";
    category?: string;
    tags: string[];
    prompt: string;
    characterAssetId?: string;
    characterVersionId?: string;
};

export type ChapterStoryboardCharacter = {
    assetId?: string;
    versionId?: string;
    name: string;
    definition: Record<string, unknown>;
};

export type ProjectShotReplacementInput = {
    title: string;
    description: string;
    durationMs: number;
    revision: ShotRevisionInput;
    assetVersionIds: string[];
};

export type ChapterStoryboardReplaceImpact = {
    shotCount: number;
    revisionCount: number;
    referenceCount: number;
    artifactCount: number;
    candidateCount: number;
};

export type PendingGeneratedStoryboardSave = {
    shots: ProjectShotReplacementInput[];
    expectedShotIds: string[];
    generatedBinding: {
        taskId: string;
        baseline: GeneratedBindingBaseline;
        rowIndices: number[];
    };
    issues: Array<BindingIssue & { rowIndex: number }>;
};

const pendingGeneratedStoryboardSaves = new Map<string, PendingGeneratedStoryboardSave>();

function pendingGeneratedStoryboardKey(projectId: string, unitId: string, taskId: string) {
    return `${projectId}::${unitId}::${taskId}`;
}

function isTerminalGeneratedSaveError(error: unknown): boolean {
    if (!(error instanceof ApiError)) return false;
    const status = error.status ?? error.code;
    return status === 400 || status === 404 || status === 409 || status === 422;
}

export function clearPendingGeneratedStoryboardSave(projectId?: string, unitId?: string, taskId?: string) {
    if (!projectId) {
        pendingGeneratedStoryboardSaves.clear();
        return;
    }
    if (unitId && taskId) {
        pendingGeneratedStoryboardSaves.delete(pendingGeneratedStoryboardKey(projectId, unitId, taskId));
        return;
    }
    const prefix = unitId ? `${projectId}::${unitId}::` : `${projectId}::`;
    for (const key of pendingGeneratedStoryboardSaves.keys()) {
        if (key.startsWith(prefix)) pendingGeneratedStoryboardSaves.delete(key);
    }
}

export async function saveGeneratedChapterStoryboard(detail: ProjectDetail, unitId: string, result: { taskId: string; rows: StoryboardRow[] }, confirmIssues?: (issues: Array<BindingIssue & { rowIndex: number }>, resolution: GeneratedBindingResolution) => Promise<boolean | string[][]>, beforeReplace?: () => void) {
    const pendingKey = pendingGeneratedStoryboardKey(detail.project.id, unitId, result.taskId);
    let pending = pendingGeneratedStoryboardSaves.get(pendingKey);
    if (!pending) {
        const resolution = await resolveGeneratedProjectUnitShotAssetBindings(detail.project.id, unitId, { taskId: result.taskId });
        if (resolution.taskId !== result.taskId || resolution.rows.length !== result.rows.length || resolution.rows.some((row, index) => row.rowIndex !== index)) throw new Error("生成任务行与解析结果不一致，请重新载入任务");
        const issues = resolution.rows.flatMap((row) => row.issues.map((issue) => ({ ...issue, rowIndex: row.rowIndex })));
        const overCapacity = resolution.rows.some((row) => new Set(row.assetVersionIds).size > 6);
        const selection = (issues.length || overCapacity) && confirmIssues ? await confirmIssues(issues, resolution) : true;
        if (!selection) throw new DOMException("已保留原分镜", "AbortError");
        if (overCapacity && !Array.isArray(selection)) throw new Error("请为超限镜头明确选择最多 6 个版本");
        if (Array.isArray(selection)) {
            if (selection.length !== resolution.rows.length || selection.some((ids, index) => new Set(ids).size > 6 || ids.some((id) => !resolution.rows[index].assetVersionIds.includes(id)))) throw new Error("分镜资产选择无效");
            resolution.rows = resolution.rows.map((row, index) => ({ ...row, assetVersionIds: Array.from(new Set(selection[index])) }));
        }
        if (issues.some((issue) => issue.kind !== "missing_image") && !confirmIssues) throw new Error(issues.map((issue) => `镜头 ${issue.rowIndex + 1}：${issue.message}`).join("\n"));
        const shots = storyboardRowsToProjectShots(result.rows, detail, resolution);
        const expectedShotIds = detail.shots.filter((shot) => shot.unitId === unitId).map((shot) => shot.id);
        pending = {
            shots,
            expectedShotIds,
            generatedBinding: {
                taskId: result.taskId,
                baseline: resolution.baseline,
                rowIndices: resolution.rows.map((row) => row.rowIndex),
            },
            issues,
        };
        pendingGeneratedStoryboardSaves.set(pendingKey, pending);
    }
    try {
        beforeReplace?.();
        await replaceProjectUnitShots(detail.project.id, unitId, pending.shots, pending.expectedShotIds, pending.generatedBinding);
        pendingGeneratedStoryboardSaves.delete(pendingKey);
        return { shotCount: pending.shots.length, issues: pending.issues };
    } catch (error) {
        if (isTerminalGeneratedSaveError(error)) {
            pendingGeneratedStoryboardSaves.delete(pendingKey);
        }
        throw error;
    }
}

// Browser entry for every chapter replacement, including automatic recovery on the chapters page.
export async function saveGeneratedChapterStoryboardWithBatchGuard(
    detail: ProjectDetail,
    unitId: string,
    result: Parameters<typeof saveGeneratedChapterStoryboard>[2],
    confirmIssues?: Parameters<typeof saveGeneratedChapterStoryboard>[3],
    assertCallerCurrent?: () => void,
) {
    const ownerScope = getActiveUserScope();
    const assertCurrent = () => {
        const session = useUserStore.getState();
        if (!session.hydrated || !session.user?.id || session.user.id !== ownerScope || getActiveUserScope() !== ownerScope) {
            throw new DOMException("Aborted", "AbortError");
        }
        assertCallerCurrent?.();
    };
    assertCurrent();
    const identity = { ownerScope, projectId: detail.project.id, unitId };
    const execute = async () => {
        const log = await resolveBatchStorage(identity).read();
        assertCurrent();
        if (batchHasUnsettledItems(log)) throw new Error("当前章节正在参与批量任务，请先完成核对或停止剩余提交后再替换");
        return saveGeneratedChapterStoryboard(detail, unitId, result, confirmIssues, assertCurrent);
    };
    return typeof navigator !== "undefined" && typeof navigator.locks?.request === "function"
        ? withBatchChapterLock(identity, execute)
        : execute();
}

export function chapterStoryboardCharacters(detail: ProjectDetail, unitId?: string): ChapterStoryboardCharacter[] {
    const confirmed = detail.assets.flatMap((asset): ChapterStoryboardCharacter[] => {
        const card = asset.character;
        if (asset.category !== "character" || !card?.versionId) return [];
        return [{ assetId: asset.id, versionId: card.versionId, name: asset.title, definition: card.definition || {} }];
    });
    const seenNames = new Set(confirmed.map((character) => normalizeCharacterName(character.name)));
    const pending = detail.assetCandidates.flatMap((candidate): ChapterStoryboardCharacter[] => {
        if (candidate.category !== "character" || candidate.status !== "pending_confirmation") return [];
        if (unitId && candidate.unitId && candidate.unitId !== unitId) return [];
        const name = candidate.name.trim();
        const normalizedName = normalizeCharacterName(name);
        if (!normalizedName || seenNames.has(normalizedName)) return [];
        seenNames.add(normalizedName);
        return [{ name, definition: candidateDefinition(candidate.detailsJson) }];
    });
    return [...confirmed, ...pending];
}

export function chapterStoryboardAssets(detail: Pick<ProjectDetail, "assets">): ChapterStoryboardAsset[] {
    return detail.assets.flatMap((asset): ChapterStoryboardAsset[] => {
        const type = storyboardAssetType(asset.mediaType, asset.category);
        if (!type) return [];
        const characterVersionId = asset.character?.versionId;
        return [{
            id: asset.id,
            title: asset.title,
            type,
            category: asset.category || undefined,
            tags: [],
            prompt: assetPrompt(asset.previewText, asset.character?.definition || asset.visual?.definition, asset.title),
            ...(type === "character" ? { characterAssetId: asset.id, characterVersionId } : {}),
        }];
    });
}

export function storyboardRowsToProjectShots(rows: StoryboardRow[], detail?: ProjectDetail, resolution?: GeneratedBindingResolution): ProjectShotReplacementInput[] {
    const versionByAssetId = new Map((detail?.assets || []).flatMap((asset) => asset.primaryVersionId ? [[asset.id, asset.primaryVersionId] as const] : []));
    const mentionReferenceByAssetId = new Map((detail?.assets || []).map((asset) => [asset.id, {
        assetId: asset.id,
        kind: asset.category === "character" ? "character" as const : "image" as const,
        label: asset.title,
        title: asset.title,
    }]));
    return rows.map((row, index) => {
        const shotNumber = index + 1;
        const description = row.plotDescription.trim() || `镜头 ${shotNumber}`;
        const durationMs = Math.max(1000, Math.round((Number(row.durationSeconds) || 1) * 1000));
        const resolvedRow = resolution?.rows.find((item) => item.rowIndex === index);
        const mentionReferences = resolvedRow ? resolvedRow.candidates.filter((candidate) => !candidate.missingImage && candidate.assetVersionId && resolvedRow.assetVersionIds.includes(candidate.assetVersionId)).map((candidate) => ({ assetId: candidate.assetId, kind: candidate.category === "character" ? "character" as const : "image" as const, label: candidate.name, title: candidate.name })) : storyboardRowAssetIds(row).flatMap((assetId) => {
            const reference = mentionReferenceByAssetId.get(assetId);
            return reference ? [reference] : [];
        });
        return {
            title: formatShotOrdinal(index),
            description,
            durationMs,
            assetVersionIds: resolution ? (resolvedRow?.assetVersionIds || []) : storyboardRowAssetVersionIds(row, versionByAssetId),
            revision: {
                plotDescription: description,
                action: joinStoryboardAction(row),
                dialogue: row.dialogue.trim(),
                shotSize: row.shotSize.trim(),
                cameraAngle: row.camera.trim(),
                cameraMovement: row.motion.trim(),
                durationMs,
                imagePrompt: row.imageGenerationPrompt.trim(),
                videoPrompt: ensureShotAssetMentionPrompt(row.videoMotionPrompt, mentionReferences),
                negativePrompt: row.negativePrompt.trim(),
                continuityNotes: row.continuityOut.trim(),
                actionBeats: row.timeBeats.trim() ? [{ description: row.timeBeats.trim() }] : [],
            },
        };
    });
}

export function chapterStoryboardReplaceImpact(detail: ProjectDetail, unitId: string): ChapterStoryboardReplaceImpact {
    const shots = detail.shots.filter((shot) => shot.unitId === unitId);
    const shotIds = new Set(shots.map((shot) => shot.id));
    return {
        shotCount: shots.length,
        revisionCount: detail.shotRevisions.filter((revision) => shotIds.has(revision.shotId)).length,
        referenceCount: detail.shotReferences.filter((reference) => shotIds.has(reference.shotId)).length,
        artifactCount: detail.shotArtifacts.filter((artifact) => artifact.unitId === unitId).length,
        candidateCount: detail.assetCandidates.filter((candidate) => Boolean(candidate.shotId && shotIds.has(candidate.shotId))).length,
    };
}

function storyboardRowAssetVersionIds(row: StoryboardRow, versionByAssetId: Map<string, string>) {
    return storyboardRowAssetIds(row).flatMap((assetId) => {
        const versionId = versionByAssetId.get(assetId);
        return versionId ? [versionId] : [];
    }).slice(0, 6);
}

function storyboardRowAssetIds(row: StoryboardRow) {
    return Array.from(new Set([
        ...row.assetBindings.map((binding) => binding.nodeId),
        ...row.characters.flatMap((character) => character.characterAssetId ? [character.characterAssetId] : []),
    ])).slice(0, 6);
}

function storyboardAssetType(mediaType: string, category: string): ChapterStoryboardAsset["type"] | null {
    if (category === "character") return "character";
    if (mediaType === "image" || mediaType === "video" || mediaType === "audio") return mediaType;
    if (category === "environment" || category === "prop") return "image";
    return null;
}

function assetPrompt(previewText: string | undefined, definition: Record<string, unknown> | undefined, title: string) {
    const preview = previewText?.replace(/\s+/g, " ").trim();
    if (preview) return preview.slice(0, 600);
    if (definition && Object.keys(definition).length) return JSON.stringify(definition).slice(0, 600);
    return title.trim();
}

function candidateDefinition(value: string): Record<string, unknown> {
    try {
        const parsed: unknown = JSON.parse(value || "{}");
        return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {};
    } catch {
        return {};
    }
}

function normalizeCharacterName(value: string) {
    return value.trim().toLocaleLowerCase();
}

function joinStoryboardAction(row: StoryboardRow) {
    return [
        labeledText("叙事意图", row.narrativeIntent),
        labeledText("表演调度", row.performanceBlocking),
        labeledText("时间节拍", row.timeBeats),
        labeledText("情绪", row.emotion),
        labeledText("光线与氛围", row.lightingAndAtmosphere),
        labeledText("声音", row.audioEffects),
    ].filter(Boolean).join("\n");
}

function labeledText(label: string, value: string) {
    const text = value.trim();
    return text ? `${label}：${text}` : "";
}
