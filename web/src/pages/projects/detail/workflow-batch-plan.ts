import { requestCreditCost } from "@/constant/credits";
import { modelQuoteRequest } from "@/lib/model-pricing";
import { formatShotOrdinal, customShotTitle } from "@/lib/shot-label";
import { quoteModel, type LogicalModelQuote, type ModelQuoteRequest } from "@/services/api/logical-models";
import type { ProjectDetail, ShotArtifact } from "@/services/api/projects";
import type { Skill } from "@/services/api/skills";
import type { GenerationTask } from "@/services/api/task-center";
import { skillRuntime } from "@/services/skill-runtime";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";
import { buildWorkflowImageInput, type WorkflowImageInput } from "./workflow-image-generation";
import { buildShotAssetReferenceContext, resolveShotAssetMentionPrompt } from "./workflow-shot-references";

export type BatchImageStage = "storyboard" | "previz";
export type BatchArtifactType = "storyboard" | "action_board";
export type BatchScope = "missing" | "failed" | "stale";
export type BatchCategory =
    | "missing" | "ready" | "active" | "syncing"
    | "failed" | "stale" | "blocked";
export type BatchItemState =
    | "pending" | "submitting" | "submitted" | "syncing"
    | "succeeded" | "failed" | "blocked" | "unknown" | "stopped";
export type BatchRunState = "paused" | "running" | "waiting" | "stopped" | "finished";
export type BatchImageSettings = {
    model: string;
    size: string;
    quality: string;
    skillIds: string[];
};
export type BatchIdentity = {
    ownerScope: string;
    projectId: string;
    unitId: string;
};
export type BatchCandidate = {
    shotId: string;
    revisionId: string;
    title: string;
    category: BatchCategory;
    taskId?: string;
    eligible: boolean;
    issues: string[];
    routedModel?: string;
};
export type BatchItem = {
    itemId: string;
    shotId: string;
    revisionId: string;
    fingerprint: string;
    operation: "create" | "retry";
    state: BatchItemState;
    taskId?: string;
    attempts: number;
    taskAttemptsBefore?: number;
    taskUpdatedAtBefore?: string;
    updatedAt: string;
    issueCode?: string;
};
export type BatchLog = BatchIdentity & {
    version: 1;
    batchId: string;
    stage: BatchImageStage;
    scope: BatchScope;
    settings: BatchImageSettings;
    state: BatchRunState;
    items: BatchItem[];
    createdAt: string;
    updatedAt: string;
};
export type BatchPrice = {
    knownMicrocredits: number;
    unknownCount: number;
    estimated: boolean;
};
export type BatchPreview = {
    identity: BatchIdentity;
    stage: BatchImageStage;
    scope: BatchScope;
    settings: BatchImageSettings;
    candidates: BatchCandidate[];
    items: BatchItem[];
    price: BatchPrice;
    confirmationToken: string;
};

export function batchArtifactType(stage: BatchImageStage): BatchArtifactType {
    return stage === "previz" ? "action_board" : "storyboard";
}

type ParsedBatchTaskFacts = {
    task: GenerationTask;
    domainProjectId: string;
    unitId?: string;
    shotId: string;
    artifactType: string;
    shotRevisionId?: string;
    billingUncertain: boolean;
    cancelUncertain: boolean;
};

function parseTaskMetadata(task: GenerationTask): Record<string, unknown> {
    if (!task.inputJson) return {};
    try {
        const parsed = JSON.parse(task.inputJson) as { metadata?: Record<string, unknown> };
        if (parsed && typeof parsed === "object" && parsed.metadata && typeof parsed.metadata === "object") {
            return parsed.metadata;
        }
    } catch {
        // Ignore malformed inputJson in summary projection.
    }
    return {};
}

function extractChapterTaskFacts(detail: ProjectDetail, unitId: string): Map<string, ParsedBatchTaskFacts[]> {
    const projectId = detail.project.id;
    const shotIdsInUnit = new Set(
        (detail.shots || [])
            .filter((shot) => shot.projectId === projectId && shot.unitId === unitId)
            .map((shot) => shot.id),
    );
    const byKey = new Map<string, ParsedBatchTaskFacts[]>();
    for (const task of detail.tasks || []) {
        const meta = parseTaskMetadata(task);
        const domainProjectId = String(meta.domainProjectId || task.clientContext?.domainProjectId || task.projectId || "");
        if (domainProjectId && domainProjectId !== projectId) continue;
        const taskUnitId = typeof meta.unitId === "string" && meta.unitId.trim() ? meta.unitId.trim() : undefined;
        if (taskUnitId && taskUnitId !== unitId) continue;
        const shotId = String(meta.shotId || task.clientContext?.shotId || "").trim();
        if (!shotId || !shotIdsInUnit.has(shotId)) continue;
        const artifactType = String(meta.artifactType || task.clientContext?.artifactType || "").trim();
        if (!artifactType) continue;
        const shotRevisionId = typeof meta.shotRevisionId === "string" && meta.shotRevisionId.trim()
            ? meta.shotRevisionId.trim()
            : undefined;
        const billingStatus = String(task.billing?.status || "");
        const billingUncertain = billingStatus === "uncertain" || billingStatus === "review_required";
        const cancelStatus = String(task.providerCancelStatus || "");
        const cancelUncertain = cancelStatus === "requested" || cancelStatus === "uncertain";
        const key = `${shotId}:${artifactType}`;
        const list = byKey.get(key) || [];
        list.push({
            task,
            domainProjectId: domainProjectId || projectId,
            unitId: taskUnitId,
            shotId,
            artifactType,
            shotRevisionId,
            billingUncertain,
            cancelUncertain,
        });
        byKey.set(key, list);
    }
    for (const list of byKey.values()) {
        list.sort((left, right) => right.task.updatedAt.localeCompare(left.task.updatedAt));
    }
    return byKey;
}

function extractChapterArtifacts(detail: ProjectDetail, unitId: string): Map<string, ShotArtifact[]> {
    const projectId = detail.project.id;
    const shotIdsInUnit = new Set(
        (detail.shots || [])
            .filter((shot) => shot.projectId === projectId && shot.unitId === unitId)
            .map((shot) => shot.id),
    );
    const byKey = new Map<string, ShotArtifact[]>();
    for (const artifact of detail.shotArtifacts || []) {
        if (artifact.projectId && artifact.projectId !== projectId) continue;
        if (artifact.unitId && artifact.unitId !== unitId) continue;
        if (!shotIdsInUnit.has(artifact.shotId)) continue;
        const key = `${artifact.shotId}:${artifact.type}`;
        const list = byKey.get(key) || [];
        list.push(artifact);
        byKey.set(key, list);
    }
    for (const list of byKey.values()) {
        list.sort((left, right) => right.version - left.version);
    }
    return byKey;
}

export function missingBoundImageCount(detail: ProjectDetail, shotId: string): number {
    const references = buildShotAssetReferenceContext(detail, shotId);
    const resolvedIds = new Set(references.referenceImages.map((img) => img.id));
    return (detail.shotReferences || []).filter(
        (reference) =>
            reference.shotId === shotId &&
            reference.status === "linked" &&
            reference.referencedVersion &&
            !resolvedIds.has(reference.referencedVersion.assetId),
    ).length;
}

export function classifyBatchCandidates(input: {
    detail: ProjectDetail;
    unitId: string;
    stage: BatchImageStage;
    scope: BatchScope;
    localItems?: readonly BatchItem[];
    settings?: BatchImageSettings;
    config?: AiConfig;
}): BatchCandidate[] {
    const { detail, unitId, stage, scope, localItems = [], settings, config } = input;
    const projectId = detail.project.id;
    const targetType = batchArtifactType(stage);
    const revisionById = new Map((detail.shotRevisions || []).map((rev) => [rev.id, rev]));
    const tasksByKey = extractChapterTaskFacts(detail, unitId);
    const artifactsByKey = extractChapterArtifacts(detail, unitId);
    const localItemsByShot = new Map<string, BatchItem[]>();
    for (const item of localItems) {
        const list = localItemsByShot.get(item.shotId) || [];
        list.push(item);
        localItemsByShot.set(item.shotId, list);
    }

    const shots = (detail.shots || [])
        .filter((shot) => shot.projectId === projectId && shot.unitId === unitId)
        .slice()
        .sort((left, right) => left.position - right.position);

    return shots.map((shot, index): BatchCandidate => {
        const title = customShotTitle(shot.title, index) || formatShotOrdinal(index);
        const currentRevisionId = shot.currentRevisionId?.trim() || "";
        const revision = currentRevisionId ? revisionById.get(currentRevisionId) : undefined;
        const shotLocalItems = localItemsByShot.get(shot.id) || [];
        const key = `${shot.id}:${targetType}`;
        const shotTasks = tasksByKey.get(key) || [];
        const shotArtifacts = artifactsByKey.get(key) || [];

        // 1. Missing revision, archived project, or unconfirmed local submission -> blocked.
        if (detail.project.status === "archived") {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "blocked",
                eligible: false,
                issues: ["项目已归档，无法提交批量生成任务"],
            };
        }
        if (!currentRevisionId || !revision || revision.shotId !== shot.id) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "blocked",
                eligible: false,
                issues: ["镜头缺少已保存的当前脚本修订"],
            };
        }
        const unknownLocal = shotLocalItems.find(
            (item) => item.state === "unknown" || (item.state === "submitting" && !item.taskId),
        );
        if (unknownLocal) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "blocked",
                taskId: unknownLocal.taskId,
                eligible: false,
                issues: ["存在提交结果未确认的记录，请先核对任务状态"],
            };
        }

        // 2. Active task (queued/running) or local submitting/submitted/syncing -> active.
        const activeTaskFact = shotTasks.find(
            (entry) => entry.task.status === "queued" || entry.task.status === "running",
        );
        const activeLocal = shotLocalItems.find(
            (item) => item.state === "submitting" || item.state === "submitted" || item.state === "syncing",
        );
        if (activeTaskFact || activeLocal) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "active",
                taskId: activeTaskFact?.task.id || activeLocal?.taskId,
                eligible: false,
                issues: ["当前镜头已有同类型任务在途"],
            };
        }

        // Unresolved media, cancellation and billing block every scope, not just retry.
        // Inspect all tasks: a newer failure cannot erase an older unsettled invocation.
        const unresolvedTask = shotTasks.find(entry =>
            entry.task.canRecoverMedia || entry.cancelUncertain || entry.billingUncertain,
        );
        if (unresolvedTask) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "blocked",
                taskId: unresolvedTask.task.id,
                eligible: false,
                issues: [
                    ...(unresolvedTask.task.canRecoverMedia ? ["作品已生成待恢复保存，请到任务中心重试保存"] : []),
                    ...(unresolvedTask.cancelUncertain ? ["上游取消状态仍在确认中，暂不可生成"] : []),
                    ...(unresolvedTask.billingUncertain ? ["上一次调用费用仍在核对中，暂不可生成"] : []),
                ],
            };
        }

        // 3. Ready artifact on currentRevisionId with non-empty resourceId -> ready.
        const readyCurrentArtifact = shotArtifacts.find(
            (artifact) =>
                artifact.status === "ready" &&
                Boolean(artifact.resourceId?.trim()) &&
                artifact.revisionId === currentRevisionId,
        );
        if (readyCurrentArtifact) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "ready",
                taskId: readyCurrentArtifact.taskId,
                eligible: false,
                issues: ["当前版本已有有效成品"],
            };
        }

        // 4. Syncing: pending/running artifact or succeeded task awaiting artifact registration.
        const pendingArtifact = shotArtifacts.find(
            (artifact) => artifact.status === "pending" || artifact.status === "running",
        );
        const artifactTaskIds = new Set(shotArtifacts.map((artifact) => artifact.taskId).filter(Boolean));
        const syncingTaskFact = shotTasks.find(
            (entry) =>
                entry.task.status === "succeeded" &&
                (!entry.shotRevisionId || entry.shotRevisionId === currentRevisionId) &&
                !artifactTaskIds.has(entry.task.id),
        );
        if (pendingArtifact || syncingTaskFact) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "syncing",
                taskId: syncingTaskFact?.task.id || pendingArtifact?.taskId,
                eligible: false,
                issues: ["任务已完成，正在等待产物落库同步"],
            };
        }

        // Validate script content, asset mentions, and model compatibility for candidate evaluation.
        const validationIssues: string[] = [];
        let routedModel: string | undefined;
        const primaryText = (revision.imagePrompt || revision.plotDescription || "").trim();
        if (!primaryText) {
            validationIssues.push("镜头缺少画面内容，请先保存脚本");
        } else {
            try {
                const refs = buildShotAssetReferenceContext(detail, shot.id);
                const basePrompt = [
                    revision.imagePrompt || revision.plotDescription,
                    revision.action,
                    stage === "previz" ? "黑白分镜草图，清晰动作节拍，电影构图" : "",
                ].filter(Boolean).join("\n");
                resolveShotAssetMentionPrompt(basePrompt, refs, {
                    dialogue: revision.dialogue,
                    mode: "image",
                });
                if (config && settings && scope !== "failed") {
                    const built = buildWorkflowImageInput({
                        detail,
                        shot,
                        revision,
                        stage,
                        settings,
                        config,
                    });
                    routedModel = built.routedModel;
                }
            } catch (error) {
                validationIssues.push(error instanceof Error ? error.message : "镜头参数校验未通过");
            }
        }
        const warningIssues: string[] = [];
        const missingImages = missingBoundImageCount(detail, shot.id);
        if (missingImages > 0) {
            warningIssues.push(`有 ${missingImages} 个绑定资产版本暂无参考图，仅使用可用图片生成`);
        }

        // 5. Latest failed/cancelled task without valid ready artifact.
        const latestFailedFact = shotTasks.find(
            (entry) => entry.task.status === "failed" || entry.task.status === "cancelled",
        );
        const staleArtifact = shotArtifacts.find(
            (artifact) =>
                artifact.status === "stale" ||
                (artifact.status === "ready" && Boolean(artifact.resourceId?.trim()) && artifact.revisionId !== currentRevisionId),
        );

        if (latestFailedFact) {
            const retryDisqualifiers: string[] = [];
            if (latestFailedFact.task.canRecoverMedia) {
                retryDisqualifiers.push("作品已生成待恢复保存，请到任务中心重试保存");
            }
            if (latestFailedFact.cancelUncertain) {
                retryDisqualifiers.push("上游取消状态仍在确认中，暂不可重试");
            }
            if (latestFailedFact.billingUncertain) {
                retryDisqualifiers.push("上一次调用费用仍在核对中，暂不可重试");
            }
            if (latestFailedFact.shotRevisionId && latestFailedFact.shotRevisionId !== currentRevisionId) {
                retryDisqualifiers.push("镜头脚本已更新，旧失败任务不可按原任务重试");
            }

            if (scope === "failed") {
                const allIssues = [...retryDisqualifiers, ...validationIssues];
                if (allIssues.length > 0) {
                    return {
                        shotId: shot.id,
                        revisionId: currentRevisionId,
                        title,
                        category: "blocked",
                        taskId: latestFailedFact.task.id,
                        eligible: false,
                        issues: allIssues,
                    };
                }
                return {
                    shotId: shot.id,
                    revisionId: currentRevisionId,
                    title,
                    category: "failed",
                    taskId: latestFailedFact.task.id,
                    eligible: true,
                    issues: warningIssues,
                };
            }

            // In missing or stale scope: if there is a stale artifact and the failed task belonged to an old revision, fall through to stale if in stale scope.
            if (scope === "stale" && staleArtifact) {
                if (validationIssues.length > 0) {
                    return {
                        shotId: shot.id,
                        revisionId: currentRevisionId,
                        title,
                        category: "blocked",
                        eligible: false,
                        issues: validationIssues,
                    };
                }
                return {
                    shotId: shot.id,
                    revisionId: currentRevisionId,
                    title,
                    category: "stale",
                    eligible: true,
                    issues: warningIssues,
                    routedModel,
                };
            }

            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "failed",
                taskId: latestFailedFact.task.id,
                eligible: false,
                issues: retryDisqualifiers.length > 0 ? retryDisqualifiers : ["存在失败任务，可切到“仅重试失败”模式处理"],
            };
        }

        // 6. Stale artifact or ready artifact from an older revision -> stale.
        if (staleArtifact) {
            if (scope === "stale" && validationIssues.length > 0) {
                return {
                    shotId: shot.id,
                    revisionId: currentRevisionId,
                    title,
                    category: "blocked",
                    eligible: false,
                    issues: validationIssues,
                };
            }
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "stale",
                eligible: scope === "stale",
                issues: scope === "stale" ? warningIssues : ["产物已过期，可切到“仅更新过期”模式处理"],
                routedModel,
            };
        }

        // 7 & 8. Missing artifact; if validation failed, mark blocked.
        if (validationIssues.length > 0) {
            return {
                shotId: shot.id,
                revisionId: currentRevisionId,
                title,
                category: "blocked",
                eligible: false,
                issues: validationIssues,
            };
        }
        return {
            shotId: shot.id,
            revisionId: currentRevisionId,
            title,
            category: "missing",
            eligible: scope === "missing",
            issues: scope === "missing" ? warningIssues : ["尚未生成产物，可切到“仅补缺”模式处理"],
            routedModel,
        };
    });
}

export async function sha256Hex(value: unknown): Promise<string> {
    const bytes = new TextEncoder().encode(JSON.stringify(value));
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export async function fingerprintBatchShot(input: WorkflowImageInput): Promise<string> {
    const built = buildWorkflowImageInput(input);
    const { detail, shot, revision, stage } = input;
    const sortedReferences = (detail.shotReferences || [])
        .filter((ref) => ref.shotId === shot.id)
        .slice()
        .sort((left, right) => left.id.localeCompare(right.id))
        .map((ref) => ({
            id: ref.id,
            assetVersionId: ref.assetVersionId,
            role: ref.role,
            status: ref.status,
            resourceId: ref.referencedVersion?.referenceImages?.[0]?.resourceId || ref.referencedVersion?.referenceImage?.resourceId || "",
            representationIds: (ref.referencedVersion?.representations || [])
                .map((rep) => `${rep.id}:${rep.role}:${rep.resourceId}`)
                .sort(),
        }));
    const sortedCharacterVersions = built.references.resolvedCharacterVersions
        .slice()
        .sort((left, right) => `${left.assetId}:${left.versionId}`.localeCompare(`${right.assetId}:${right.versionId}`));
    return sha256Hex({
        projectId: detail.project.id,
        unitId: shot.unitId,
        shotId: shot.id,
        stage,
        revision: {
            id: revision.id,
            shotId: revision.shotId,
            version: revision.version,
            plotDescription: revision.plotDescription || "",
            action: revision.action || "",
            dialogue: revision.dialogue || "",
            shotSize: revision.shotSize || "",
            cameraAngle: revision.cameraAngle || "",
            cameraMovement: revision.cameraMovement || "",
            durationMs: revision.durationMs,
            imagePrompt: revision.imagePrompt || "",
            negativePrompt: revision.negativePrompt || "",
            continuityNotes: revision.continuityNotes || "",
            actionBeatsJson: revision.actionBeatsJson || "",
        },
        references: sortedReferences,
        referenceImageOrder: built.references.referenceImages.map((img) => ({
            id: img.id,
            storageKey: img.storageKey || "",
        })),
        resolvedCharacterVersions: sortedCharacterVersions,
        resolvedPrompt: built.prompt,
    });
}

export function summarizeBatchPrices(values: readonly (number | null)[], estimated: boolean): BatchPrice {
    let knownMicrocredits = 0;
    let unknownCount = 0;
    for (const value of values) {
        if (value === null || !Number.isSafeInteger(value) || value < 0) {
            unknownCount += 1;
            continue;
        }
        const next = knownMicrocredits + value;
        if (!Number.isSafeInteger(next)) {
            return { knownMicrocredits: 0, unknownCount: values.length, estimated: true };
        }
        knownMicrocredits = next;
    }
    return { knownMicrocredits, unknownCount, estimated };
}

export function batchPriceLabel(
    price: BatchPrice,
    options: { customChannel?: boolean; creditsEnabled?: boolean } = {},
): string {
    const suffix = options.customChannel ? "（渠道费用由供应商结算，非免费）" : "";
    if (options.creditsEnabled === false) {
        return `当前未启用平台积分计费，费用无法预估${suffix}`;
    }
    const amount = (price.knownMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 6 });
    if (price.unknownCount) {
        return `已知部分预计 ${amount} 积分，另有 ${price.unknownCount} 项费用无法预估${suffix}`;
    }
    return `${price.estimated ? "预计" : "当前报价合计"} ${amount} 积分${suffix}`;
}

export async function buildBatchPreview(input: {
    identity: BatchIdentity;
    detail: ProjectDetail;
    stage: BatchImageStage;
    scope: BatchScope;
    settings: BatchImageSettings;
    config: AiConfig;
    skills: Skill[];
    localItems?: readonly BatchItem[];
    creditsEnabled?: boolean;
    now?: () => string;
    quoteFetcher?: (request: ModelQuoteRequest, signal?: AbortSignal) => Promise<{ quote: LogicalModelQuote }>;
    signal?: AbortSignal;
}): Promise<BatchPreview> {
    const {
        identity,
        detail,
        stage,
        scope,
        settings,
        config,
        skills,
        localItems,
        creditsEnabled = true,
        now = () => new Date().toISOString(),
        quoteFetcher = quoteModel,
        signal,
    } = input;

    if (!identity.ownerScope?.trim() || !identity.projectId?.trim() || !identity.unitId?.trim()) {
        throw new Error("批次身份与账号作用域不完整");
    }

    const candidates = classifyBatchCandidates({
        detail,
        unitId: identity.unitId,
        stage,
        scope,
        localItems,
        settings,
        config,
    });

    const shotById = new Map((detail.shots || []).map((shot) => [shot.id, shot]));
    const revisionById = new Map((detail.shotRevisions || []).map((rev) => [rev.id, rev]));
    const quoteCache = new Map<string, Promise<LogicalModelQuote | null>>();
    const items: BatchItem[] = [];
    const itemMicrocredits: Array<number | null> = [];
    let anyEstimated = false;
    const tokenFingerprints: string[] = [];

    for (const candidate of candidates) {
        if (!candidate.eligible) continue;
        if (signal?.aborted) {
            throw new DOMException("Aborted", "AbortError");
        }
        const shot = shotById.get(candidate.shotId);
        const revision = revisionById.get(candidate.revisionId);
        if (!shot || !revision) continue;

        if (scope === "failed") {
            if (!candidate.taskId) continue;
            // Failed retry uses original task settings and task ID; price is unknown unless re-quoted by server.
            const refContext = buildShotAssetReferenceContext(detail, shot.id);
            const fallbackSettings: BatchImageSettings = {
                model: settings.model || config.imageModel || config.model,
                size: settings.size || detail.project.aspectRatio || "16:9",
                quality: settings.quality || "auto",
                skillIds: settings.skillIds,
            };
            let fp: string;
            try {
                fp = await fingerprintBatchShot({
                    detail,
                    shot,
                    revision,
                    stage,
                    settings: fallbackSettings,
                    config,
                });
            } catch {
                fp = await sha256Hex({
                    projectId: detail.project.id,
                    unitId: shot.unitId,
                    shotId: shot.id,
                    revisionId: revision.id,
                    stage,
                    referenceImages: refContext.referenceImages.map((img) => img.storageKey || img.id),
                });
            }
            const timestamp = now();
            items.push({
                itemId: `item-${shot.id}-${stage}`,
                shotId: shot.id,
                revisionId: revision.id,
                fingerprint: fp,
                operation: "retry",
                state: "pending",
                taskId: candidate.taskId,
                attempts: 0,
                updatedAt: timestamp,
            });
            itemMicrocredits.push(null);
            anyEstimated = true;
            tokenFingerprints.push(`${shot.id}:${revision.id}:${candidate.taskId}:${fp}`);
            continue;
        }

        const built = buildWorkflowImageInput({
            detail,
            shot,
            revision,
            stage,
            settings,
            config,
        });
        const skillExec = await skillRuntime.prepare({
            profile: "shortDrama",
            prompt: built.prompt,
            skills,
            selectedSkillIds: settings.skillIds,
        });
        const baseFp = await fingerprintBatchShot({
            detail,
            shot,
            revision,
            stage,
            settings,
            config,
        });
        const skillPromptHash = await sha256Hex({
            baseFp,
            skillPrompt: skillExec.prompt,
            skillMeta: skillExec.metadata,
        });
        const timestamp = now();
        items.push({
            itemId: `item-${shot.id}-${stage}`,
            shotId: shot.id,
            revisionId: revision.id,
            fingerprint: baseFp,
            operation: "create",
            state: "pending",
            attempts: 0,
            updatedAt: timestamp,
        });
        tokenFingerprints.push(`${shot.id}:${revision.id}:${built.routedModel}:${skillPromptHash}`);

        if (!creditsEnabled) {
            itemMicrocredits.push(null);
            anyEstimated = true;
            continue;
        }

        const req = modelQuoteRequest(built.config, built.routedModel, "image", built.requirements);
        if (req) {
            const reqKey = JSON.stringify(req);
            let quotePromise = quoteCache.get(reqKey);
            if (!quotePromise) {
                quotePromise = quoteFetcher(req, signal)
                    .then((res) => res.quote)
                    .catch(() => null);
                quoteCache.set(reqKey, quotePromise);
            }
            const quote = await quotePromise;
            if (signal?.aborted) {
                throw new DOMException("Aborted", "AbortError");
            }
            if (quote && Number.isSafeInteger(quote.amountMicrocredits) && quote.amountMicrocredits >= 0) {
                itemMicrocredits.push(quote.amountMicrocredits);
                if (quote.estimated) anyEstimated = true;
                continue;
            }
        }

        const priceChannel = resolveModelChannel(built.config, built.routedModel);
        const configuredCredits = requestCreditCost({
            channelMode: priceChannel.scope === "system" ? "remote" : "local",
            modelCosts: priceChannel.modelCosts,
            model: modelOptionName(built.routedModel),
            count: 1,
            seconds: 1,
            capability: "image",
            config: built.config,
            requirements: built.requirements,
        });
        if (typeof configuredCredits === "number" && Number.isFinite(configuredCredits) && configuredCredits >= 0) {
            const micro = Math.round(configuredCredits * 1_000_000);
            itemMicrocredits.push(Number.isSafeInteger(micro) ? micro : null);
        } else {
            itemMicrocredits.push(null);
            anyEstimated = true;
        }
    }

    const price = summarizeBatchPrices(itemMicrocredits, anyEstimated);
    const confirmationToken = await sha256Hex({
        identity,
        stage,
        scope,
        settings,
        tokenFingerprints,
        price,
    });

    return {
        identity,
        stage,
        scope,
        settings: {
            model: settings.model,
            size: settings.size,
            quality: settings.quality,
            skillIds: [...settings.skillIds],
        },
        candidates,
        items,
        price,
        confirmationToken,
    };
}
