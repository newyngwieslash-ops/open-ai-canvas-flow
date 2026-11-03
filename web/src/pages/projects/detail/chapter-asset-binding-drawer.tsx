// 用途：真实章节分镜工作台旧镜头资产批量匹配预览与确认侧栏。
// 消费 token 组：--workspace-surface / --workspace-border / --workspace-accent / --foreground / --fs-* / --r-*。
// 对标容器：AppDrawer 产品侧栏壳；明暗自适应由语义 CSS 变量驱动，动画服从 prefers-reduced-motion。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "antd";
import type { QueryClient } from "@tanstack/react-query";

import { StatusBadge } from "@/components/ui/base/badges";
import { AppDrawer } from "@/components/ui/product/app-drawer";
import { Callout } from "@/components/ui/product/callout";
import { EmptyState } from "@/components/ui/product/empty-state";
import { formatShotOrdinal } from "@/lib/shot-label";
import {
    STORYBOARD_ASSET_BINDING_CODES,
    STORYBOARD_ASSET_BINDING_REASONS,
    storyboardAssetBindingErrorMessage,
} from "@/services/api/error-codes";
import {
    appendProjectUnitShotAssetBindings,
    previewProjectUnitShotAssetBindings,
    type BindingAppendRequest,
    type BindingAppendResult,
    type BindingCandidate,
    type BindingEvidence,
    type BindingIssue,
    type BindingPreview,
    type BindingShotPreview,
    type ProjectDetail,
} from "@/services/api/projects";
import { ApiError } from "@/services/api/request";
import { useUserStore } from "@/stores/use-user-store";

import { assetCategoryLabel } from "./workflow-shared";

export const MAX_PREVIEW_SHOT_COUNT = 100;
export const MAX_SHOT_ASSET_VERSION_CAPACITY = 6;
export const ARTIFACT_INVALIDATION_NOTICE = "仅实增镜头的相关产物将过期并取消选中，历史内容保留";

export type ChapterAssetBindingDrawerProps = {
    projectId: string;
    unitId: string;
    shotIds: string[];
    shotTitles?: Record<string, string>;
    open: boolean;
    onClose: () => void;
    onApplied: (result: BindingAppendResult) => void;
};

export function deduplicateShotIds(shotIds: string[]): string[] {
    const seen = new Set<string>();
    const result: string[] = [];
    for (const raw of shotIds || []) {
        const trimmed = typeof raw === "string" ? raw.trim() : "";
        if (!trimmed || seen.has(trimmed)) continue;
        seen.add(trimmed);
        result.push(trimmed);
    }
    return result;
}

export function deduplicateShotCandidates(candidates: BindingCandidate[]): BindingCandidate[] {
    const result: BindingCandidate[] = [];
    const indexByKey = new Map<string, number>();
    const hasCleanOccurrenceByKey = new Map<string, boolean>();

    for (const candidate of candidates || []) {
        const assetId = candidate.assetId?.trim() || "";
        const versionKey = candidate.assetVersionId ?? "";
        const dedupKey = assetId ? `${assetId}::${versionKey}` : "";
        const isCleanBindable =
            candidate.bindable &&
            !(candidate.issues || []).some((issue) => issue.kind === "context_warning");

        if (!dedupKey || !indexByKey.has(dedupKey)) {
            const cloned: BindingCandidate = {
                ...candidate,
                evidence: [],
                issues: [],
            };
            const seenEvidence = new Set<string>();
            for (const ev of candidate.evidence || []) {
                const evKey = `${ev.kind}::${ev.field}::${ev.start}::${ev.end}::${ev.text}`;
                if (!seenEvidence.has(evKey)) {
                    seenEvidence.add(evKey);
                    cloned.evidence.push(ev);
                }
            }
            const seenIssues = new Set<string>();
            for (const issue of candidate.issues || []) {
                const issueKey = `${issue.kind}::${issue.message}`;
                if (!seenIssues.has(issueKey)) {
                    seenIssues.add(issueKey);
                    cloned.issues.push(issue);
                }
            }
            if (dedupKey) {
                indexByKey.set(dedupKey, result.length);
                hasCleanOccurrenceByKey.set(dedupKey, isCleanBindable);
            }
            result.push(cloned);
            continue;
        }

        const existingIndex = indexByKey.get(dedupKey)!;
        const existing = result[existingIndex]!;
        if (isCleanBindable) {
            hasCleanOccurrenceByKey.set(dedupKey, true);
        }
        const seenEvidence = new Set(
            existing.evidence.map((ev) => `${ev.kind}::${ev.field}::${ev.start}::${ev.end}::${ev.text}`),
        );
        for (const ev of candidate.evidence || []) {
            const evKey = `${ev.kind}::${ev.field}::${ev.start}::${ev.end}::${ev.text}`;
            if (!seenEvidence.has(evKey)) {
                seenEvidence.add(evKey);
                existing.evidence.push(ev);
            }
        }
        const seenIssues = new Set(
            existing.issues.map((issue) => `${issue.kind}::${issue.message}`),
        );
        for (const issue of candidate.issues || []) {
            const issueKey = `${issue.kind}::${issue.message}`;
            if (!seenIssues.has(issueKey)) {
                seenIssues.add(issueKey);
                existing.issues.push(issue);
            }
        }
        existing.bindable = existing.bindable || candidate.bindable;
        existing.defaultSelected = existing.defaultSelected || candidate.defaultSelected;
        existing.missingImage = existing.missingImage && candidate.missingImage;
    }

    for (const [dedupKey, hasClean] of hasCleanOccurrenceByKey.entries()) {
        if (!hasClean) continue;
        const idx = indexByKey.get(dedupKey);
        if (idx === undefined) continue;
        const item = result[idx]!;
        item.issues = item.issues.filter((issue) => issue.kind !== "context_warning");
    }

    return result;
}

export function computeAutoSelectableVersionIds(shot: BindingShotPreview): string[] {
    if ((shot.issues || []).some((issue) => issue.kind === "capacity_exceeded")) {
        return [];
    }

    const existingDistinctVersionIds = new Set(
        (shot.existingReferences || [])
            .map((ref) => ref.assetVersionId)
            .filter((id): id is string => Boolean(id)),
    );
    const existingVersionCount = shot.existingReferences?.length
        ? existingDistinctVersionIds.size
        : Math.max(0, shot.existingVersionCount || 0);
    const remainingCapacity = Math.max(0, MAX_SHOT_ASSET_VERSION_CAPACITY - existingVersionCount);
    if (remainingCapacity === 0) {
        return [];
    }

    const candidates = deduplicateShotCandidates(shot.candidates || []);
    const allEligibleInOrder: string[] = [];
    const tierDefaultSelected: string[] = [];
    const tierClean: string[] = [];
    const tierContextWarning: string[] = [];
    const seen = new Set<string>();

    for (const candidate of candidates) {
        if (
            !candidate.bindable ||
            !candidate.assetVersionId ||
            existingDistinctVersionIds.has(candidate.assetVersionId) ||
            seen.has(candidate.assetVersionId)
        ) {
            continue;
        }
        const hasBlockingIssue = (candidate.issues || []).some(
            (issue) => issue.kind === "ambiguous" || issue.kind === "capacity_exceeded",
        );
        if (hasBlockingIssue) {
            continue;
        }
        seen.add(candidate.assetVersionId);
        allEligibleInOrder.push(candidate.assetVersionId);

        if (candidate.defaultSelected) {
            tierDefaultSelected.push(candidate.assetVersionId);
        } else if ((candidate.issues || []).some((issue) => issue.kind === "context_warning")) {
            tierContextWarning.push(candidate.assetVersionId);
        } else {
            tierClean.push(candidate.assetVersionId);
        }
    }

    if (allEligibleInOrder.length <= remainingCapacity) {
        return allEligibleInOrder;
    }

    if (tierDefaultSelected.length > remainingCapacity) {
        return [];
    }

    return [...tierDefaultSelected, ...tierClean, ...tierContextWarning].slice(0, remainingCapacity);
}

export function formatBindingEvidenceExcerpt(
    evidence: BindingEvidence,
    sourceFieldText?: string,
): string {
    if (evidence.start === 0 && evidence.end === 0) {
        return "";
    }
    if (
        typeof sourceFieldText === "string" &&
        sourceFieldText.length > 0 &&
        evidence.start >= 0 &&
        evidence.end > evidence.start
    ) {
        const codePoints = Array.from(sourceFieldText);
        if (evidence.end <= codePoints.length) {
            return codePoints.slice(evidence.start, evidence.end).join("");
        }
    }
    if (typeof evidence.text === "string" && evidence.text.length > 0) {
        const codePoints = Array.from(evidence.text);
        const spanLength = evidence.end - evidence.start;
        if (
            evidence.start >= 0 &&
            spanLength > 0 &&
            evidence.end <= codePoints.length &&
            codePoints.length > spanLength
        ) {
            return codePoints.slice(evidence.start, evidence.end).join("");
        }
        return evidence.text;
    }
    return "";
}

function evidenceKindLabel(kind: BindingEvidence["kind"] | string): string {
    switch (kind) {
        case "structured_id":
            return "结构化标识";
        case "exact_name":
            return "唯一精确名称";
        case "visual_label":
            return "明确视觉标签";
        case "structured_continuity":
            return "结构化连续场景";
        case "mention":
            return "正文提及（待确认）";
        case "speaker":
            return "台词说话人（待确认）";
        default:
            return kind || "文本线索";
    }
}

function evidenceFieldLabel(field: string): string {
    switch (field) {
        case "plotDescription":
            return "镜头画面";
        case "description":
            return "镜头描述";
        case "action":
            return "表演与动作";
        case "dialogue":
            return "对白 / 旁白";
        case "imagePrompt":
            return "画面提示词";
        case "videoPrompt":
            return "视频提示词";
        case "continuityNotes":
            return "接戏备注";
        case "projectAssetRefs":
            return "结构化资产引用";
        default:
            return field || "字段";
    }
}

export type ShotBindingCapacitySummary = {
    existingDistinctVersionIds: Set<string>;
    existingVersionCount: number;
    remainingCapacity: number;
    bindableNewVersionIds: Set<string>;
    selectedNewVersionIds: string[];
    candidateTotalVersions: number;
    selectedTotalVersions: number;
    hasCapacityIssue: boolean;
    blocksConfirm: boolean;
};

export function computeShotBindingCapacity(
    shot: BindingShotPreview,
    selectedVersionIds: string[],
    shotTouched: boolean,
): ShotBindingCapacitySummary {
    const existingDistinctVersionIds = new Set(
        (shot.existingReferences || [])
            .map((ref) => ref.assetVersionId)
            .filter((id): id is string => Boolean(id)),
    );
    const existingVersionCount = shot.existingReferences?.length
        ? existingDistinctVersionIds.size
        : Math.max(0, shot.existingVersionCount || 0);
    const remainingCapacity = Math.max(0, MAX_SHOT_ASSET_VERSION_CAPACITY - existingVersionCount);

    const candidates = deduplicateShotCandidates(shot.candidates || []);
    const bindableNewVersionIds = new Set<string>();
    for (const candidate of candidates) {
        if (
            candidate.bindable &&
            candidate.assetVersionId &&
            !existingDistinctVersionIds.has(candidate.assetVersionId)
        ) {
            bindableNewVersionIds.add(candidate.assetVersionId);
        }
    }

    const seenSelected = new Set<string>();
    const selectedNewVersionIds: string[] = [];
    for (const versionId of selectedVersionIds || []) {
        if (!versionId || seenSelected.has(versionId) || existingDistinctVersionIds.has(versionId)) {
            continue;
        }
        seenSelected.add(versionId);
        selectedNewVersionIds.push(versionId);
    }

    const candidateTotalVersions = existingVersionCount + bindableNewVersionIds.size;
    const selectedTotalVersions = existingVersionCount + selectedNewVersionIds.length;
    const hasShotCapacityIssue =
        (shot.issues || []).some((issue) => issue.kind === "capacity_exceeded") ||
        candidates.some((candidate) =>
            (candidate.issues || []).some((issue) => issue.kind === "capacity_exceeded"),
        );
    const hasCapacityIssue =
        candidateTotalVersions > MAX_SHOT_ASSET_VERSION_CAPACITY ||
        selectedTotalVersions > MAX_SHOT_ASSET_VERSION_CAPACITY ||
        hasShotCapacityIssue;

    const blocksConfirm =
        selectedTotalVersions > MAX_SHOT_ASSET_VERSION_CAPACITY ||
        (hasCapacityIssue && selectedNewVersionIds.length === 0 && !shotTouched);

    return {
        existingDistinctVersionIds,
        existingVersionCount,
        remainingCapacity,
        bindableNewVersionIds,
        selectedNewVersionIds,
        candidateTotalVersions,
        selectedTotalVersions,
        hasCapacityIssue,
        blocksConfirm,
    };
}

function buildInitialSelections(preview: BindingPreview): Record<string, string[]> {
    const initial: Record<string, string[]> = {};
    for (const shot of preview.shots || []) {
        const existingSet = new Set(
            (shot.existingReferences || [])
                .map((ref) => ref.assetVersionId)
                .filter((id): id is string => Boolean(id)),
        );
        const selected: string[] = [];
        const seen = new Set<string>();
        for (const candidate of deduplicateShotCandidates(shot.candidates || [])) {
            if (
                candidate.defaultSelected &&
                candidate.bindable &&
                candidate.assetVersionId &&
                !existingSet.has(candidate.assetVersionId) &&
                !seen.has(candidate.assetVersionId)
            ) {
                seen.add(candidate.assetVersionId);
                selected.push(candidate.assetVersionId);
            }
        }
        initial[shot.shotId] = selected;
    }
    return initial;
}

function mergeShotAutoSelections(shot: BindingShotPreview, currentSelected: string[]): string[] {
    const existingDistinctVersionIds = new Set(
        (shot.existingReferences || [])
            .map((ref) => ref.assetVersionId)
            .filter((id): id is string => Boolean(id)),
    );
    const existingVersionCount = shot.existingReferences?.length
        ? existingDistinctVersionIds.size
        : Math.max(0, shot.existingVersionCount || 0);
    const remainingCapacity = Math.max(0, MAX_SHOT_ASSET_VERSION_CAPACITY - existingVersionCount);
    const autoIds = computeAutoSelectableVersionIds(shot);
    const merged: string[] = [];
    const seen = new Set<string>();
    for (const id of [...autoIds, ...(currentSelected || [])]) {
        if (!id || seen.has(id) || existingDistinctVersionIds.has(id)) continue;
        if (merged.length >= remainingCapacity) break;
        seen.add(id);
        merged.push(id);
    }
    return merged;
}

type PreviewSessionState = {
    scopeKey: string;
    preview: BindingPreview | null;
    loading: boolean;
    previewError: string;
    selectedByShot: Record<string, string[]>;
    touchedShots: Record<string, boolean>;
    submitting: boolean;
    submitError: string;
    conflicted: boolean;
    pendingRetryPayload: BindingAppendRequest | null;
};

function createEmptySession(scopeKey: string, loading = false): PreviewSessionState {
    return {
        scopeKey,
        preview: null,
        loading,
        previewError: "",
        selectedByShot: {},
        touchedShots: {},
        submitting: false,
        submitError: "",
        conflicted: false,
        pendingRetryPayload: null,
    };
}

export function mergeBindingAppendResultIntoProjectCache(
    queryClient: QueryClient,
    projectId: string,
    unitId: string,
    result: BindingAppendResult,
) {
    const shotResultById = new Map((result.shots || []).map((item) => [item.shotId, item]));
    const applyWorkspacePatch = <
        T extends {
            shotReferences?: ProjectDetail["shotReferences"];
            shotArtifacts?: ProjectDetail["shotArtifacts"];
            project?: ProjectDetail["project"];
            assets?: ProjectDetail["assets"];
        },
    >(
        current: T | undefined,
    ): T | undefined => {
        if (!current) return current;
        const next: T = { ...current };
        if (next.project && typeof result.projectRevision === "number") {
            next.project = { ...next.project, revision: result.projectRevision };
        }
        if (Array.isArray(next.shotReferences)) {
            const existingByRefId = new Map(next.shotReferences.map((ref) => [ref.id, ref]));
            const assetByPrimaryVersionId = new Map(
                (current.assets || [])
                    .filter((a) => a.primaryVersionId)
                    .map((a) => [a.primaryVersionId as string, a]),
            );
            const untouchedRefs = next.shotReferences.filter((ref) => !shotResultById.has(ref.shotId));
            const updatedRefs: ProjectDetail["shotReferences"] = [];
            for (const shotResult of result.shots || []) {
                for (const ref of shotResult.references || []) {
                    const prev = existingByRefId.get(ref.referenceId);
                    const matchedAsset = prev?.asset || assetByPrimaryVersionId.get(ref.assetVersionId);
                    updatedRefs.push({
                        id: ref.referenceId,
                        shotId: shotResult.shotId,
                        assetVersionId: ref.assetVersionId,
                        role: ref.role,
                        status: ref.status,
                        createdAt: prev?.createdAt || new Date().toISOString(),
                        ...(matchedAsset ? { asset: matchedAsset } : {}),
                        ...(prev?.referencedVersion ? { referencedVersion: prev.referencedVersion } : {}),
                    });
                }
            }
            next.shotReferences = [...untouchedRefs, ...updatedRefs];
        }
        if (Array.isArray(next.shotArtifacts)) {
            next.shotArtifacts = next.shotArtifacts.map((artifact) => {
                const shotResult = shotResultById.get(artifact.shotId);
                if (!shotResult) return artifact;
                const updatedArt = (shotResult.artifacts || []).find((a) => a.artifactId === artifact.id);
                if (!updatedArt) return artifact;
                return {
                    ...artifact,
                    status: updatedArt.status,
                    selected: updatedArt.selected,
                };
            });
        }
        return next;
    };

    queryClient.setQueryData<{ project: ProjectDetail["project"] }>(
        ["project", projectId, "core"],
        (current) =>
            current?.project && typeof result.projectRevision === "number"
                ? { ...current, project: { ...current.project, revision: result.projectRevision } }
                : current,
    );
    queryClient.setQueryData(
        ["project", projectId, "unit-workspace", unitId],
        (current: Record<string, unknown> | undefined) => applyWorkspacePatch(current as never),
    );
    queryClient.setQueryData(
        ["project", projectId],
        (current: ProjectDetail | undefined) => applyWorkspacePatch(current),
    );
}

export function ChapterAssetBindingDrawer({
    projectId,
    unitId,
    shotIds,
    shotTitles,
    open,
    onClose,
    onApplied,
}: ChapterAssetBindingDrawerProps) {
    const userId = useUserStore((state) => state.user?.id || "");
    const dedupedShotIds = useMemo(() => deduplicateShotIds(shotIds), [shotIds]);
    const shotIdsKey = useMemo(() => dedupedShotIds.join("\u0000"), [dedupedShotIds]);
    const scopeKey = `${userId}::${projectId}::${unitId}::${shotIdsKey}`;

    const [session, setSession] = useState<PreviewSessionState>(() => createEmptySession(scopeKey, false));
    const activeSession = session.scopeKey === scopeKey ? session : createEmptySession(scopeKey, false);

    const previewAbortRef = useRef<AbortController | null>(null);
    const submittingRef = useRef(false);
    const activeScopeKeyRef = useRef(scopeKey);
    activeScopeKeyRef.current = scopeKey;

    const abortInFlightPreview = useCallback(() => {
        if (previewAbortRef.current) {
            previewAbortRef.current.abort();
            previewAbortRef.current = null;
        }
    }, []);

    const loadPreview = useCallback(
        (targetScopeKey: string, targetProjectId: string, targetUnitId: string, targetShots: string[]) => {
            abortInFlightPreview();
            if (
                !targetProjectId.trim() ||
                !targetUnitId.trim() ||
                targetShots.length === 0 ||
                targetShots.length > MAX_PREVIEW_SHOT_COUNT
            ) {
                setSession(createEmptySession(targetScopeKey, false));
                return;
            }

            const controller = new AbortController();
            previewAbortRef.current = controller;
            setSession({
                ...createEmptySession(targetScopeKey, true),
            });

            void previewProjectUnitShotAssetBindings(
                targetProjectId,
                targetUnitId,
                { shotIds: targetShots },
                controller.signal,
            )
                .then((previewData) => {
                    if (controller.signal.aborted || activeScopeKeyRef.current !== targetScopeKey) {
                        return;
                    }
                    setSession({
                        scopeKey: targetScopeKey,
                        preview: previewData,
                        loading: false,
                        previewError: "",
                        selectedByShot: buildInitialSelections(previewData),
                        touchedShots: {},
                        submitting: false,
                        submitError: "",
                        conflicted: false,
                        pendingRetryPayload: null,
                    });
                })
                .catch((error: unknown) => {
                    if (
                        controller.signal.aborted ||
                        activeScopeKeyRef.current !== targetScopeKey ||
                        (error instanceof Error && error.name === "AbortError")
                    ) {
                        return;
                    }
                    const message =
                        error instanceof ApiError
                            ? storyboardAssetBindingErrorMessage(error.reason)
                            : "预览分镜资产匹配失败，请稍后重试。";
                    setSession({
                        ...createEmptySession(targetScopeKey, false),
                        previewError: message,
                    });
                })
                .finally(() => {
                    if (previewAbortRef.current === controller) {
                        previewAbortRef.current = null;
                    }
                });
        },
        [abortInFlightPreview],
    );

    useEffect(() => {
        if (!open) {
            abortInFlightPreview();
            submittingRef.current = false;
            setSession(createEmptySession(scopeKey, false));
            return;
        }
        loadPreview(scopeKey, projectId, unitId, dedupedShotIds);
        return () => {
            abortInFlightPreview();
        };
    }, [open, scopeKey, projectId, unitId, dedupedShotIds, loadPreview, abortInFlightPreview]);

    const handleCancel = useCallback(() => {
        abortInFlightPreview();
        onClose();
    }, [abortInFlightPreview, onClose]);

    const toggleCandidateVersion = useCallback(
        (shotId: string, versionId: string, checked: boolean) => {
            setSession((current) => {
                if (
                    current.scopeKey !== activeScopeKeyRef.current ||
                    current.submitting ||
                    current.conflicted ||
                    current.pendingRetryPayload
                ) {
                    return current;
                }
                const prevSelected = current.selectedByShot[shotId] || [];
                const nextSelected = checked
                    ? prevSelected.includes(versionId)
                        ? prevSelected
                        : [...prevSelected, versionId]
                    : prevSelected.filter((id) => id !== versionId);
                return {
                    ...current,
                    submitError: "",
                    selectedByShot: {
                        ...current.selectedByShot,
                        [shotId]: nextSelected,
                    },
                    touchedShots: {
                        ...current.touchedShots,
                        [shotId]: true,
                    },
                };
            });
        },
        [],
    );

    const markShotSkipAdditions = useCallback((shotId: string) => {
        setSession((current) => {
            if (
                current.scopeKey !== activeScopeKeyRef.current ||
                current.submitting ||
                current.conflicted ||
                current.pendingRetryPayload
            ) {
                return current;
            }
            return {
                ...current,
                submitError: "",
                selectedByShot: {
                    ...current.selectedByShot,
                    [shotId]: [],
                },
                touchedShots: {
                    ...current.touchedShots,
                    [shotId]: true,
                },
            };
        });
    }, []);

    const handleSelectShotBindable = useCallback((shot: BindingShotPreview) => {
        setSession((current) => {
            if (
                current.scopeKey !== activeScopeKeyRef.current ||
                current.submitting ||
                current.conflicted ||
                current.pendingRetryPayload
            ) {
                return current;
            }
            const nextSelected = mergeShotAutoSelections(
                shot,
                current.selectedByShot[shot.shotId] || [],
            );
            return {
                ...current,
                submitError: "",
                selectedByShot: {
                    ...current.selectedByShot,
                    [shot.shotId]: nextSelected,
                },
                touchedShots: {
                    ...current.touchedShots,
                    [shot.shotId]: true,
                },
            };
        });
    }, []);

    const handleSelectAllBindable = useCallback(() => {
        setSession((current) => {
            if (
                current.scopeKey !== activeScopeKeyRef.current ||
                !current.preview ||
                current.submitting ||
                current.conflicted ||
                current.pendingRetryPayload
            ) {
                return current;
            }
            const nextSelectedByShot: Record<string, string[]> = { ...current.selectedByShot };
            const nextTouchedShots: Record<string, boolean> = { ...current.touchedShots };
            for (const shot of current.preview.shots || []) {
                nextSelectedByShot[shot.shotId] = mergeShotAutoSelections(
                    shot,
                    current.selectedByShot[shot.shotId] || [],
                );
                nextTouchedShots[shot.shotId] = true;
            }
            return {
                ...current,
                submitError: "",
                selectedByShot: nextSelectedByShot,
                touchedShots: nextTouchedShots,
            };
        });
    }, []);

    const handleClearAllSelections = useCallback(() => {
        setSession((current) => {
            if (
                current.scopeKey !== activeScopeKeyRef.current ||
                !current.preview ||
                current.submitting ||
                current.conflicted ||
                current.pendingRetryPayload
            ) {
                return current;
            }
            const nextSelectedByShot: Record<string, string[]> = {};
            const nextTouchedShots: Record<string, boolean> = { ...current.touchedShots };
            for (const shot of current.preview.shots || []) {
                nextSelectedByShot[shot.shotId] = [];
                nextTouchedShots[shot.shotId] = true;
            }
            return {
                ...current,
                submitError: "",
                selectedByShot: nextSelectedByShot,
                touchedShots: nextTouchedShots,
            };
        });
    }, []);

    const dedupedCandidatesByShot = useMemo(() => {
        const map = new Map<string, BindingCandidate[]>();
        for (const shot of activeSession.preview?.shots || []) {
            map.set(shot.shotId, deduplicateShotCandidates(shot.candidates || []));
        }
        return map;
    }, [activeSession.preview]);

    const autoSelectableByShot = useMemo(() => {
        const map = new Map<string, string[]>();
        for (const shot of activeSession.preview?.shots || []) {
            map.set(shot.shotId, computeAutoSelectableVersionIds(shot));
        }
        return map;
    }, [activeSession.preview]);

    const shotSummaries = useMemo(() => {
        const map = new Map<string, ShotBindingCapacitySummary>();
        for (const shot of activeSession.preview?.shots || []) {
            map.set(
                shot.shotId,
                computeShotBindingCapacity(
                    shot,
                    activeSession.selectedByShot[shot.shotId] || [],
                    Boolean(activeSession.touchedShots[shot.shotId]),
                ),
            );
        }
        return map;
    }, [activeSession.preview, activeSession.selectedByShot, activeSession.touchedShots]);

    const totalAutoSelectableNewCount = useMemo(() => {
        let count = 0;
        for (const ids of autoSelectableByShot.values()) {
            count += ids.length;
        }
        return count;
    }, [autoSelectableByShot]);

    const totalSelectedNewCount = useMemo(() => {
        let count = 0;
        for (const summary of shotSummaries.values()) {
            count += summary.selectedNewVersionIds.length;
        }
        return count;
    }, [shotSummaries]);

    const allAutoSelectableChecked = useMemo(() => {
        if (totalAutoSelectableNewCount === 0) return false;
        for (const shot of activeSession.preview?.shots || []) {
            const autoIds = autoSelectableByShot.get(shot.shotId) || [];
            const selected = activeSession.selectedByShot[shot.shotId] || [];
            for (const id of autoIds) {
                if (!selected.includes(id)) return false;
            }
        }
        return true;
    }, [activeSession.preview, activeSession.selectedByShot, autoSelectableByShot, totalAutoSelectableNewCount]);

    const anyShotBlocksConfirm = useMemo(() => {
        for (const summary of shotSummaries.values()) {
            if (summary.blocksConfirm) return true;
        }
        return false;
    }, [shotSummaries]);

    const isOversizedScope = dedupedShotIds.length > MAX_PREVIEW_SHOT_COUNT;
    const isEmptyScope = dedupedShotIds.length === 0;

    const confirmDisabled =
        isEmptyScope ||
        isOversizedScope ||
        activeSession.loading ||
        Boolean(activeSession.previewError) ||
        !activeSession.preview ||
        activeSession.submitting ||
        activeSession.conflicted ||
        anyShotBlocksConfirm;

    const handleConfirmWithSelections = useCallback(
        async (overrideSelectedByShot?: Record<string, string[]>) => {
            const effectiveSelectedByShot = overrideSelectedByShot ?? activeSession.selectedByShot;
            const effectiveTouchedShots = overrideSelectedByShot
                ? Object.fromEntries((activeSession.preview?.shots || []).map((s) => [s.shotId, true]))
                : activeSession.touchedShots;
            const effectiveBlocksConfirm = (activeSession.preview?.shots || []).some((shot) =>
                computeShotBindingCapacity(
                    shot,
                    effectiveSelectedByShot[shot.shotId] || [],
                    Boolean(effectiveTouchedShots[shot.shotId]),
                ).blocksConfirm,
            );
            const isBlocked =
                isEmptyScope ||
                isOversizedScope ||
                activeSession.loading ||
                Boolean(activeSession.previewError) ||
                !activeSession.preview ||
                activeSession.submitting ||
                activeSession.conflicted ||
                effectiveBlocksConfirm;

            if (isBlocked || submittingRef.current || !activeSession.preview) {
                return;
            }
            const requestScopeKey = scopeKey;
            const preview = activeSession.preview;
            const baselineShotIds = preview.baseline.shotIds?.length
                ? preview.baseline.shotIds
                : preview.shots.map((shot) => shot.shotId);

            const payload: BindingAppendRequest = activeSession.pendingRetryPayload ?? {
                baseline: preview.baseline,
                selections: baselineShotIds.map((shotId) => ({
                    shotId,
                    assetVersionIds: effectiveSelectedByShot[shotId] || [],
                })),
            };

            submittingRef.current = true;
            setSession((current) =>
                current.scopeKey === requestScopeKey
                    ? {
                          ...current,
                          selectedByShot: effectiveSelectedByShot,
                          touchedShots: effectiveTouchedShots,
                          submitting: true,
                          submitError: "",
                          pendingRetryPayload: payload,
                      }
                    : current,
            );

            try {
                const result = await appendProjectUnitShotAssetBindings(projectId, unitId, payload);
                if (activeScopeKeyRef.current !== requestScopeKey) {
                    return;
                }
                submittingRef.current = false;
                setSession((current) =>
                    current.scopeKey === requestScopeKey
                        ? {
                              ...current,
                              submitting: false,
                              submitError: "",
                              conflicted: false,
                              pendingRetryPayload: null,
                          }
                        : current,
                );
                onApplied(result);
                onClose();
            } catch (error: unknown) {
                submittingRef.current = false;
                if (activeScopeKeyRef.current !== requestScopeKey) {
                    return;
                }
                const isConflict =
                    error instanceof ApiError &&
                    (error.status === 409 ||
                        error.code === STORYBOARD_ASSET_BINDING_CODES.conflict ||
                        error.reason === STORYBOARD_ASSET_BINDING_REASONS.conflict);
                const isValidationFailure =
                    error instanceof ApiError &&
                    (error.status === 400 ||
                        error.status === 422 ||
                        error.reason === STORYBOARD_ASSET_BINDING_REASONS.invalidRequest ||
                        error.reason === STORYBOARD_ASSET_BINDING_REASONS.invalidReference ||
                        error.reason === STORYBOARD_ASSET_BINDING_REASONS.capacityExceeded);

                if (isConflict) {
                    setSession((current) =>
                        current.scopeKey === requestScopeKey
                            ? {
                                  ...current,
                                  submitting: false,
                                  conflicted: true,
                                  pendingRetryPayload: null,
                                  submitError: storyboardAssetBindingErrorMessage(
                                      STORYBOARD_ASSET_BINDING_REASONS.conflict,
                                  ),
                              }
                            : current,
                    );
                    return;
                }

                if (isValidationFailure && error instanceof ApiError) {
                    setSession((current) =>
                        current.scopeKey === requestScopeKey
                            ? {
                                  ...current,
                                  submitting: false,
                                  pendingRetryPayload: null,
                                  submitError: storyboardAssetBindingErrorMessage(error.reason),
                              }
                            : current,
                    );
                    return;
                }

                const retryMessage =
                    error instanceof ApiError && error.reason === STORYBOARD_ASSET_BINDING_REASONS.busy
                        ? storyboardAssetBindingErrorMessage(STORYBOARD_ASSET_BINDING_REASONS.busy)
                        : "确认请求未完成或连接中断，服务端可能已提交；已保留原基线与原选择，请使用原请求重试确认。";

                setSession((current) =>
                    current.scopeKey === requestScopeKey
                        ? {
                              ...current,
                              submitting: false,
                              pendingRetryPayload: payload,
                              submitError: retryMessage,
                          }
                        : current,
                );
            }
        },
        [
            activeSession.conflicted,
            activeSession.loading,
            activeSession.pendingRetryPayload,
            activeSession.preview,
            activeSession.previewError,
            activeSession.selectedByShot,
            activeSession.submitting,
            activeSession.touchedShots,
            isEmptyScope,
            isOversizedScope,
            onApplied,
            onClose,
            projectId,
            scopeKey,
            unitId,
        ],
    );

    const handleConfirm = useCallback(() => handleConfirmWithSelections(), [handleConfirmWithSelections]);

    const handleRefreshPreview = useCallback(() => {
        submittingRef.current = false;
        loadPreview(scopeKey, projectId, unitId, dedupedShotIds);
    }, [dedupedShotIds, loadPreview, projectId, scopeKey, unitId]);

    const isSelectionLocked =
        activeSession.submitting ||
        activeSession.conflicted ||
        Boolean(activeSession.pendingRetryPayload);

    const showQuickSelectAndConfirm =
        totalSelectedNewCount === 0 &&
        totalAutoSelectableNewCount > 0 &&
        !activeSession.pendingRetryPayload &&
        !activeSession.conflicted;

    const handleQuickSelectAndConfirm = useCallback(() => {
        if (!activeSession.preview) return;
        const autoSelectedByShot: Record<string, string[]> = {};
        for (const shot of activeSession.preview.shots || []) {
            autoSelectedByShot[shot.shotId] = mergeShotAutoSelections(
                shot,
                activeSession.selectedByShot[shot.shotId] || [],
            );
        }
        void handleConfirmWithSelections(autoSelectedByShot);
    }, [activeSession.preview, activeSession.selectedByShot, handleConfirmWithSelections]);

    return (
        <AppDrawer
            open={open}
            title="匹配镜头资产"
            size={680}
            onClose={handleCancel}
            footer={
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="text-xs text-foreground/60">
                        {ARTIFACT_INVALIDATION_NOTICE}
                    </div>
                    <div className="flex items-center gap-2">
                        <Button onClick={handleCancel} disabled={activeSession.submitting}>
                            取消
                        </Button>
                        {activeSession.conflicted ? (
                            <Button type="default" onClick={handleRefreshPreview}>
                                重新预览
                            </Button>
                        ) : null}
                        {activeSession.pendingRetryPayload && !activeSession.conflicted ? (
                            <Button
                                type="primary"
                                loading={activeSession.submitting}
                                disabled={confirmDisabled}
                                onClick={() => void handleConfirm()}
                            >
                                重试确认
                            </Button>
                        ) : null}
                        {showQuickSelectAndConfirm ? (
                            <Button
                                type="primary"
                                loading={activeSession.submitting}
                                disabled={confirmDisabled}
                                onClick={handleQuickSelectAndConfirm}
                            >
                                全选并确认绑定（{totalAutoSelectableNewCount} 项）
                            </Button>
                        ) : null}
                        <Button
                            type={
                                activeSession.pendingRetryPayload || showQuickSelectAndConfirm
                                    ? "default"
                                    : "primary"
                            }
                            loading={activeSession.submitting}
                            disabled={confirmDisabled}
                            onClick={() => void handleConfirm()}
                        >
                            确认追加绑定
                        </Button>
                    </div>
                </div>
            }
        >
            <div
                className="flex flex-col gap-4 text-foreground"
                data-slot="chapter-asset-binding-drawer"
                data-canvas-no-zoom
                data-canvas-wheel-scroll
            >
                <Callout tone="info" title="只追加引用，不覆盖脚本或移除已有引用">
                    {ARTIFACT_INVALIDATION_NOTICE}
                </Callout>

                {isEmptyScope ? (
                    <EmptyState
                        size="compact"
                        title="当前没有可匹配的分镜镜头"
                        description="请先在当前章节创建或选择至少 1 个镜头后再执行资产匹配。"
                    />
                ) : null}

                {isOversizedScope ? (
                    <div
                        role="alert"
                        className="rounded-lg border border-border bg-surface p-4 text-sm text-foreground"
                    >
                        <p className="font-semibold">镜头数量超过单次预览上限（最多 100 个镜头）</p>
                        <p className="mt-1 text-xs text-foreground/70">
                            当前已选 {dedupedShotIds.length} 个镜头。请先缩小镜头范围至 100 个以内再预览确认，系统不会自动分批提交或截断。
                        </p>
                    </div>
                ) : null}

                {activeSession.loading ? (
                    <div role="status" className="py-8 text-center text-sm text-foreground/60">
                        正在读取分镜与项目资产匹配预览…
                    </div>
                ) : null}

                {activeSession.previewError ? (
                    <div
                        role="alert"
                        className="flex items-center justify-between gap-3 rounded-lg border border-border bg-surface p-3 text-xs text-foreground"
                    >
                        <span>{activeSession.previewError}</span>
                        <Button size="small" onClick={handleRefreshPreview}>
                            重新预览
                        </Button>
                    </div>
                ) : null}

                {activeSession.submitError ? (
                    <div
                        role="alert"
                        className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-surface p-3 text-xs text-foreground"
                    >
                        <span>{activeSession.submitError}</span>
                        <div className="flex items-center gap-2">
                            {activeSession.conflicted ? (
                                <Button size="small" type="primary" onClick={handleRefreshPreview}>
                                    重新预览
                                </Button>
                            ) : null}
                            {activeSession.pendingRetryPayload && !activeSession.conflicted ? (
                                <Button
                                    size="small"
                                    type="primary"
                                    loading={activeSession.submitting}
                                    onClick={() => void handleConfirm()}
                                >
                                    重试确认
                                </Button>
                            ) : null}
                        </div>
                    </div>
                ) : null}

                {activeSession.preview && !isEmptyScope && !isOversizedScope ? (
                    <div
                        className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-surface px-3 py-2.5 text-xs text-foreground"
                        data-slot="chapter-asset-binding-quick-bar"
                    >
                        <span className="text-foreground/80">
                            共发现 {totalAutoSelectableNewCount} 项可新增绑定候选 · 当前已勾选 {totalSelectedNewCount} 项
                        </span>
                        <div className="flex flex-wrap items-center gap-2">
                            <Button
                                size="small"
                                type={totalSelectedNewCount === 0 && totalAutoSelectableNewCount > 0 ? "primary" : "default"}
                                disabled={isSelectionLocked || totalAutoSelectableNewCount === 0 || allAutoSelectableChecked}
                                onClick={handleSelectAllBindable}
                            >
                                一键全选可绑定项
                            </Button>
                            <Button
                                size="small"
                                disabled={isSelectionLocked || totalSelectedNewCount === 0}
                                onClick={handleClearAllSelections}
                            >
                                清空新增勾选
                            </Button>
                        </div>
                    </div>
                ) : null}

                {activeSession.preview ? (
                    <div className="flex flex-col gap-4">
                        {activeSession.preview.shots.map((shot, shotIndex) => {
                            const summary = shotSummaries.get(shot.shotId)!;
                            const selectedList = activeSession.selectedByShot[shot.shotId] || [];
                            const candidates = dedupedCandidatesByShot.get(shot.shotId) || [];
                            const autoSelectableIds = autoSelectableByShot.get(shot.shotId) || [];
                            const shotAllAutoSelected =
                                autoSelectableIds.length > 0 &&
                                autoSelectableIds.every((id) => selectedList.includes(id));
                            const customTitle = shotTitles?.[shot.shotId]?.trim();
                            const hasCustomTitle = Boolean(customTitle && customTitle !== shot.shotId);
                            const totalCandidateOrSelected = Math.max(
                                summary.candidateTotalVersions,
                                summary.selectedTotalVersions,
                            );
                            return (
                                <section
                                    key={shot.shotId}
                                    aria-label={`镜头 ${shotIndex + 1}（${shot.shotId}）`}
                                    className="rounded-lg border border-border bg-surface p-4"
                                >
                                    <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border/70 pb-3">
                                        <div className="flex items-center gap-2">
                                            <span className="text-xs font-semibold tabular-nums text-foreground/60">
                                                {formatShotOrdinal(shotIndex)}
                                            </span>
                                            {hasCustomTitle ? (
                                                <>
                                                    <strong className="text-sm font-semibold">{customTitle}</strong>
                                                    <span className="text-xs text-foreground/50">{shot.shotId}</span>
                                                </>
                                            ) : (
                                                <strong className="text-sm font-semibold">{shot.shotId}</strong>
                                            )}
                                        </div>
                                        <div className="flex flex-wrap items-center gap-2 text-xs tabular-nums text-foreground/70">
                                            <span>已有 {summary.existingVersionCount}</span>
                                            <span>·</span>
                                            <span>新增已选 {summary.selectedNewVersionIds.length}</span>
                                            <span>·</span>
                                            <span>剩余容量 {summary.remainingCapacity}</span>
                                            <span>（上限 {MAX_SHOT_ASSET_VERSION_CAPACITY}）</span>
                                            {autoSelectableIds.length > 0 && !shotAllAutoSelected ? (
                                                <Button
                                                    size="small"
                                                    data-shot-select-all={shot.shotId}
                                                    disabled={isSelectionLocked}
                                                    onClick={() => handleSelectShotBindable(shot)}
                                                >
                                                    全选本镜
                                                </Button>
                                            ) : null}
                                            {summary.hasCapacityIssue ? (
                                                <StatusBadge
                                                    tone={summary.blocksConfirm ? "warning" : "neutral"}
                                                    label={`总量 ${totalCandidateOrSelected} / 剩余容量 ${summary.remainingCapacity}`}
                                                />
                                            ) : null}
                                        </div>
                                    </header>

                                    {summary.hasCapacityIssue ? (
                                        <div
                                            role="status"
                                            className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/80 bg-background/60 px-3 py-2 text-xs text-foreground/80"
                                        >
                                            <span>
                                                当前镜头已有 {summary.existingVersionCount} 个版本，含候选共{" "}
                                                <strong>总量 {totalCandidateOrSelected}</strong>，
                                                <strong>剩余容量 {summary.remainingCapacity}</strong>
                                                。请手动选择或缩减至不超过 {MAX_SHOT_ASSET_VERSION_CAPACITY} 个版本后再确认。
                                            </span>
                                            <Button
                                                size="small"
                                                disabled={isSelectionLocked}
                                                onClick={() => markShotSkipAdditions(shot.shotId)}
                                            >
                                                本镜不新增
                                            </Button>
                                        </div>
                                    ) : null}

                                    {(shot.issues || []).length > 0 ? (
                                        <ul
                                            aria-label={`${shot.shotId} 镜头级提示`}
                                            className="mt-3 flex flex-col gap-1 rounded-md border border-border/70 bg-background/40 px-3 py-2 text-xs text-foreground/75"
                                        >
                                            {shot.issues.map((issue: BindingIssue, issueIdx: number) => (
                                                <li key={`${issue.kind}-${issueIdx}`} data-issue-kind={issue.kind}>
                                                    {issue.message}
                                                </li>
                                            ))}
                                        </ul>
                                    ) : null}

                                    <div className="mt-3">
                                        <div className="text-xs font-medium text-foreground/60">
                                            已有资产引用（只读，按不同版本计入容量）
                                        </div>
                                        {(shot.existingReferences || []).length > 0 ? (
                                            <ul className="mt-1.5 flex flex-col gap-1.5">
                                                {shot.existingReferences.map((reference, refIdx) => (
                                                    <li
                                                        key={`${reference.referenceId || refIdx}-${reference.role}`}
                                                        className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5 text-xs"
                                                    >
                                                        <div className="flex flex-wrap items-center gap-2">
                                                            <span className="font-medium text-foreground">
                                                                {reference.assetVersionId}
                                                            </span>
                                                            <span className="text-foreground/50">
                                                                引用ID: {reference.referenceId}
                                                            </span>
                                                            <span className="text-foreground/50">
                                                                角色: {reference.role}
                                                            </span>
                                                            <span className="text-foreground/50">
                                                                状态: {reference.status}
                                                            </span>
                                                        </div>
                                                        <span className="text-foreground/45">已有引用 · 只读</span>
                                                    </li>
                                                ))}
                                            </ul>
                                        ) : (
                                            <p className="mt-1 text-xs text-foreground/45">暂无已有引用</p>
                                        )}
                                    </div>

                                    <div className="mt-3">
                                        <div className="text-xs font-medium text-foreground/60">
                                            候选资产建议
                                        </div>
                                        {candidates.length > 0 ? (
                                            <div className="mt-1.5 flex flex-col gap-2">
                                                {candidates.map((candidate: BindingCandidate, candidateIdx: number) => {
                                                    const versionId = candidate.assetVersionId;
                                                    const canBind = Boolean(candidate.bindable && versionId);
                                                    const alreadyInExisting = Boolean(
                                                        versionId && summary.existingDistinctVersionIds.has(versionId),
                                                    );
                                                    const isChecked = Boolean(
                                                        canBind &&
                                                            versionId &&
                                                            (alreadyInExisting || selectedList.includes(versionId)),
                                                    );
                                                    const checkboxDisabled =
                                                        !canBind || alreadyInExisting || isSelectionLocked;

                                                    return (
                                                        <div
                                                            key={`${candidate.assetId}-${versionId || "none"}-${candidateIdx}`}
                                                            className="rounded-md border border-border/75 bg-background/30 p-2.5 text-xs"
                                                        >
                                                            <label className="flex cursor-pointer items-start gap-2.5">
                                                                <input
                                                                    type="checkbox"
                                                                    className="mt-0.5 size-4 shrink-0 accent-[var(--workspace-accent)] focus-visible:outline-2 focus-visible:outline-[var(--ring)]"
                                                                    data-shot-id={shot.shotId}
                                                                    data-candidate-asset-id={candidate.assetId}
                                                                    data-candidate-version-id={versionId || ""}
                                                                    checked={isChecked}
                                                                    disabled={checkboxDisabled}
                                                                    onChange={(event) => {
                                                                        if (canBind && versionId) {
                                                                            toggleCandidateVersion(
                                                                                shot.shotId,
                                                                                versionId,
                                                                                event.target.checked,
                                                                            );
                                                                        }
                                                                    }}
                                                                />
                                                                <div className="min-w-0 flex-1">
                                                                    <div className="flex flex-wrap items-center gap-1.5">
                                                                        <strong className="text-sm font-semibold text-foreground">
                                                                            {candidate.name}
                                                                        </strong>
                                                                        <span className="rounded border border-border/70 px-1.5 py-0.5 text-foreground/65">
                                                                            {assetCategoryLabel(candidate.category)}
                                                                        </span>
                                                                        {versionId ? (
                                                                            <span className="text-foreground/50">
                                                                                版本: {versionId}
                                                                            </span>
                                                                        ) : (
                                                                            <StatusBadge
                                                                                tone="warning"
                                                                                label="无可用版本 · 不可绑定"
                                                                            />
                                                                        )}
                                                                        {!candidate.bindable && versionId ? (
                                                                            <StatusBadge tone="warning" label="不可绑定" />
                                                                        ) : null}
                                                                        {candidate.missingImage ? (
                                                                            <StatusBadge
                                                                                tone="warning"
                                                                                label="缺图 · 暂无参考图"
                                                                            />
                                                                        ) : null}
                                                                        {alreadyInExisting ? (
                                                                            <StatusBadge tone="neutral" label="已在当前镜头引用" />
                                                                        ) : null}
                                                                    </div>

                                                                    {(candidate.evidence || []).length > 0 ? (
                                                                        <ul className="mt-1.5 flex flex-col gap-1 text-foreground/65">
                                                                            {candidate.evidence.map((ev, evIdx) => {
                                                                                const excerpt = formatBindingEvidenceExcerpt(ev);
                                                                                return (
                                                                                    <li key={`${ev.kind}-${ev.field}-${evIdx}`}>
                                                                                        <span>
                                                                                            [{evidenceKindLabel(ev.kind)} ·{" "}
                                                                                            {evidenceFieldLabel(ev.field)}]
                                                                                        </span>{" "}
                                                                                        {excerpt ? (
                                                                                            <span>
                                                                                                「{excerpt}」
                                                                                                {ev.end > ev.start
                                                                                                    ? ` [${ev.start}, ${ev.end})`
                                                                                                    : ""}
                                                                                            </span>
                                                                                        ) : (
                                                                                            <span className="text-foreground/45">
                                                                                                结构化依据（无文本摘录）
                                                                                            </span>
                                                                                        )}
                                                                                    </li>
                                                                                );
                                                                            })}
                                                                        </ul>
                                                                    ) : null}

                                                                    {(candidate.issues || []).length > 0 ? (
                                                                        <ul className="mt-1.5 flex flex-col gap-1 text-foreground/75">
                                                                            {candidate.issues.map((issue, issueIdx) => (
                                                                                <li
                                                                                    key={`${issue.kind}-${issueIdx}`}
                                                                                    data-issue-kind={issue.kind}
                                                                                >
                                                                                    {issue.message}
                                                                                </li>
                                                                            ))}
                                                                        </ul>
                                                                    ) : null}
                                                                </div>
                                                            </label>
                                                        </div>
                                                    );
                                                })}
                                            </div>
                                        ) : (
                                            <p className="mt-1 text-xs text-foreground/45">当前镜头未发现待绑定资产候选</p>
                                        )}
                                    </div>
                                </section>
                            );
                        })}
                    </div>
                ) : null}
            </div>
        </AppDrawer>
    );
}

export default ChapterAssetBindingDrawer;
