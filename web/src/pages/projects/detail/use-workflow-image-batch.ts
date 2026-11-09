import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createClientUUID } from "../../../lib/client-id";
import {
    modelCapabilityConfigFor,
    normalizeImageValue,
} from "../../../lib/model-capabilities";
import { resolveCompatibleModel } from "../../../lib/model-selection";
import { getActiveUserScope } from "../../../lib/user-scope";
import { submitBackendGenerationTask } from "../../../services/api/generation-task";
import { quoteModel } from "../../../services/api/logical-models";
import { ApiError } from "../../../services/api/request";
import {
    createUnitWorkflow,
    getProjectCore,
    getProjectUnitWorkspace,
    type ProjectDetail,
    type ProjectUnitWorkspace,
    type WorkflowStep,
} from "../../../services/api/projects";
import {
    type GenerationTask,
    createGenerationTask,
    waitForGenerationTask,
    listGenerationTasks,
    queryGenerationTask,
    retryGenerationTask,
} from "../../../services/api/task-center";
import { runGenerationConsumer } from "../../../services/generation-consumer-lifecycle";
import type { Skill } from "../../../services/api/skills";
import {
    configuredModelMatchesCapability,
    selectableModelsByCapability,
    type AiConfig,
} from "../../../stores/use-config-store";
import { useUserStore } from "../../../stores/use-user-store";
import {
    type BatchCandidate,
    type BatchIdentity,
    type BatchImageSettings,
    type BatchImageStage,
    type BatchItem,
    type BatchLog,
    type BatchPreview,
    type BatchScope,
    batchArtifactType,
    buildBatchPreview,
    classifyBatchCandidates,
    fingerprintBatchShot,
    sha256Hex,
    summarizeBatchPrices,
} from "./workflow-batch-plan";
import { createWorkflowBatchRunner } from "./workflow-batch-runner";
import {
    createBatchStorage,
    batchChapterLockName,
    withBatchChapterLock,
    configureBatchStorageForTest,
    resolveBatchStorage,
    batchHasUnsettledItems as hasUnsettledItems,
    recoverBatchLog,
} from "./workflow-batch-storage";
import { prepareWorkflowImageSubmission } from "./workflow-image-generation";

export type WorkflowImageBatchController = {
    stage: BatchImageStage;
    settings: BatchImageSettings;
    scope: BatchScope;
    log: BatchLog | null;
    preview: BatchPreview | null;
    busy: boolean;
    confirming: boolean;
    blockedReason: string | null;
    canConfirm: boolean;
    chapterMutationLocked: boolean;
    lockedShotIds: ReadonlySet<string>;
    open(stage: BatchImageStage): Promise<void>;
    refreshPreview(scope: BatchScope, settings: BatchImageSettings): Promise<void>;
    confirm(): Promise<void>;
    stopRemaining(): Promise<void>;
    refreshStatus(manualTaskId?: string): Promise<void>;
    closeDialog(): void;
    dialogOpen: boolean;
    corruptedLogError: string | null;
    runtimeError: string | null;
    statusNotice: string | null;
    activeTaskLimit: number;
    clearCorruptedLog(): Promise<void>;
    prepareResume(): Promise<void>;
    prepareRetryFailed(): Promise<void>;
    runChapterMutation<T>(operation: (assertCurrent: () => void) => Promise<T>, shotId?: string): Promise<T>;
};

type BatchRuntimeOverrides = {
    sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
    now?: () => string;
    createStorage?: (identity: BatchIdentity) => ReturnType<typeof createBatchStorage>;
};

let runtimeOverrides: BatchRuntimeOverrides = {};

export function configureWorkflowBatchRuntimeForTest(overrides: BatchRuntimeOverrides = {}): void {
    runtimeOverrides = overrides;
    configureBatchStorageForTest(overrides.createStorage);
}

function defaultSleep(ms: number, signal: AbortSignal): Promise<void> {
    if (runtimeOverrides.sleep) {
        return runtimeOverrides.sleep(ms, signal);
    }
    return new Promise<void>((resolve, reject) => {
        if (signal.aborted) {
            reject(new DOMException("Aborted", "AbortError"));
            return;
        }
        const timer = setTimeout(() => {
            signal.removeEventListener("abort", onAbort);
            resolve();
        }, ms);
        const onAbort = () => {
            clearTimeout(timer);
            reject(new DOMException("Aborted", "AbortError"));
        };
        signal.addEventListener("abort", onAbort, { once: true });
    });
}

function defaultNow(): string {
    return runtimeOverrides.now ? runtimeOverrides.now() : new Date().toISOString();
}

function normalizeImageSettings(
    raw: BatchImageSettings,
    config: AiConfig,
    aspectRatio?: string,
): BatchImageSettings {
    const imageModels = selectableModelsByCapability(config, "image");
    const preferred =
        raw.model && configuredModelMatchesCapability(config, raw.model, "image")
            ? raw.model
            : config.imageModel && configuredModelMatchesCapability(config, config.imageModel, "image")
              ? config.imageModel
              : imageModels[0] || "";
    const model =
        resolveCompatibleModel(config, preferred, { capability: "image" }) ||
        preferred ||
        raw.model ||
        "";
    const profile = model ? modelCapabilityConfigFor(config, model) : undefined;
    const normalized = profile?.image
        ? normalizeImageValue(profile.image, {
              size: raw.size || aspectRatio || config.size || "16:9",
              quality: raw.quality || config.quality || "auto",
              count: "1",
          })
        : {
              size: raw.size || aspectRatio || config.size || "16:9",
              quality: raw.quality || config.quality || "auto",
          };
    const skillIds = [...new Set((raw.skillIds || []).filter(Boolean))].sort();
    return {
        model,
        size: normalized.size || raw.size || "16:9",
        quality: normalized.quality || raw.quality || "auto",
        skillIds,
    };
}

function mergeWorkspaceIntoDetail(
    base: ProjectDetail,
    coreProject: ProjectDetail["project"] | undefined,
    workspace: ProjectUnitWorkspace | undefined,
    extraTasks: GenerationTask[] = [],
): ProjectDetail {
    const mergedTasksById = new Map<string, GenerationTask>();
    for (const task of base.tasks || []) {
        mergedTasksById.set(task.id, task);
    }
    for (const task of workspace?.tasks || []) {
        mergedTasksById.set(task.id, task);
    }
    for (const task of extraTasks) {
        mergedTasksById.set(task.id, task);
    }
    const unitId = workspace?.unit?.id;
    const otherShots = unitId ? (base.shots || []).filter(s => s.unitId !== unitId) : [];
    const workspaceShots = workspace?.shots ?? base.shots ?? [];
    const shots = unitId ? [...otherShots, ...workspaceShots] : workspaceShots;
    const shotIds = new Set(workspaceShots.map(s => s.id));

    const mergeByShot = <T extends { shotId: string }>(
        existing: T[] = [],
        incoming?: T[],
    ): T[] => {
        if (!incoming) return existing;
        return [...existing.filter(item => !shotIds.has(item.shotId)), ...incoming];
    };

    return {
        ...base,
        project: coreProject ?? base.project,
        units: workspace?.unit
            ? (base.units || []).map(u => (u.id === workspace.unit.id ? workspace.unit : u))
            : base.units,
        workflows: workspace?.workflows?.length ? workspace.workflows : base.workflows,
        assets: workspace?.assets?.length ? workspace.assets : base.assets,
        shots,
        shotRevisions: mergeByShot(base.shotRevisions, workspace?.shotRevisions),
        shotArtifacts: mergeByShot(base.shotArtifacts, workspace?.shotArtifacts),
        shotReferences: mergeByShot(base.shotReferences, workspace?.shotReferences),
        assetCandidates: workspace?.assetCandidates ?? base.assetCandidates,
        tasks: [...mergedTasksById.values()],
    };
}

function parseTaskMetadata(task: GenerationTask): Record<string, unknown> {
    if (task.inputJson) {
        try {
            const parsed = JSON.parse(task.inputJson) as Record<string, unknown>;
            if (parsed.metadata && typeof parsed.metadata === "object" && !Array.isArray(parsed.metadata)) {
                return parsed.metadata as Record<string, unknown>;
            }
            if (
                parsed.input &&
                typeof parsed.input === "object" &&
                !Array.isArray(parsed.input) &&
                (parsed.input as Record<string, unknown>).metadata &&
                typeof (parsed.input as Record<string, unknown>).metadata === "object"
            ) {
                return (parsed.input as Record<string, unknown>).metadata as Record<string, unknown>;
            }
        } catch {
            // ignore invalid JSON
        }
    }
    return {};
}

function hasActiveOrUnknownItems(log: BatchLog | null): boolean {
    if (!log) return false;
    if (log.state === "running" || log.state === "waiting") return true;
    return log.items.some(
        item =>
            item.state === "submitting" ||
            item.state === "submitted" ||
            item.state === "syncing" ||
            item.state === "unknown",
    );
}

function findUnitWorkflowStep(
    detail: ProjectDetail,
    unitId: string,
    stage: BatchImageStage,
): { workflowFound: boolean; step?: WorkflowStep } {
    const wf = (detail.workflows || []).find(
        item =>
            item.instance?.unitId === unitId &&
            (item.steps || []).some(step => step.stepKey === "previz"),
    );
    if (!wf) {
        return { workflowFound: false };
    }
    const step = (wf.steps || []).find(item => item.stepKey === stage);
    return { workflowFound: true, step };
}

export function useWorkflowImageBatch(input: {
    detail: ProjectDetail;
    projectId: string;
    unitId: string;
    editorDirty: boolean;
    config: AiConfig;
    skills: Skill[];
    initialSettings: BatchImageSettings;
    onRefresh(): Promise<void>;
}): WorkflowImageBatchController {
    const { detail, projectId, unitId, editorDirty, config, skills, initialSettings, onRefresh } = input;
    const hydrated = useUserStore(state => state.hydrated);
    const user = useUserStore(state => state.user);
    const activeTaskLimit = useUserStore(state => state.runtimeLimits?.activeTaskLimit ?? 5);

    const [stage, setStage] = useState<BatchImageStage>("storyboard");
    const [scope, setScope] = useState<BatchScope>("missing");
    const [settings, setSettings] = useState<BatchImageSettings>(() =>
        normalizeImageSettings(initialSettings, config, detail.project.aspectRatio),
    );
    const [log, setLog] = useState<BatchLog | null>(null);
    const [preview, setPreview] = useState<BatchPreview | null>(null);
    const [busy, setBusy] = useState(false);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [corruptedLogError, setCorruptedLogError] = useState<string | null>(null);
    const [runtimeError, setRuntimeError] = useState<string | null>(null);
    const [statusNotice, setStatusNotice] = useState<string | null>(null);
    const [isResumeMode, setIsResumeMode] = useState(false);

    const detailRef = useRef(detail);
    detailRef.current = detail;
    const configRef = useRef(config);
    configRef.current = config;
    const skillsRef = useRef(skills);
    skillsRef.current = skills;
    const onRefreshRef = useRef(onRefresh);
    onRefreshRef.current = onRefresh;
    const logRef = useRef<BatchLog | null>(null);
    logRef.current = log;

    const previewSeqRef = useRef(0);
    const epochRef = useRef(0);
    const mountAbortRef = useRef<AbortController | null>(null);
    const activeRunnerRef = useRef<ReturnType<typeof createWorkflowBatchRunner> | null>(null);
    const confirmingRef = useRef(false);
    const stopRequestedRef = useRef(false);
    const consumedTokenRef = useRef<string | null>(null);
    const previewBaselineRef = useRef<string | null>(null);
    const editorDirtyRef = useRef(editorDirty);
    editorDirtyRef.current = editorDirty;

    const activeScope = typeof window !== "undefined" ? getActiveUserScope() : "guest";
    const sessionReady = Boolean(hydrated && user?.id && activeScope === user.id);
    const webLocksSupported =
        typeof navigator !== "undefined" &&
        Boolean(navigator.locks && typeof navigator.locks.request === "function");

    const checkOwnerCurrent = useCallback(
        (expectedOwner: string, expectedEpoch: number): boolean => {
            const state = useUserStore.getState();
            if (!state.hydrated || !state.user?.id) return false;
            if (state.user.id !== expectedOwner) return false;
            if (getActiveUserScope() !== expectedOwner) return false;
            if (epochRef.current !== expectedEpoch || mountAbortRef.current?.signal.aborted) return false;
            return true;
        },
        [],
    );

    const fetchFreshSnapshot = useCallback(
        async (
            expectedOwner: string,
            expectedEpoch: number,
            signal?: AbortSignal,
        ): Promise<{ detail: ProjectDetail; truncatedActiveTasks: boolean }> => {
            const [coreRes, workspaceRes, activeTasks] = await Promise.all([
                runGenerationConsumer(signal, async () => {
                    if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                        throw new DOMException("Aborted", "AbortError");
                    }
                    return getProjectCore(projectId);
                }),
                runGenerationConsumer(signal, async () => {
                    if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                        throw new DOMException("Aborted", "AbortError");
                    }
                    return getProjectUnitWorkspace(projectId, unitId);
                }),
                runGenerationConsumer(signal, async consumerSignal => {
                    if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                        throw new DOMException("Aborted", "AbortError");
                    }
                    return listGenerationTasks(
                        100,
                        { projectId, activeOnly: true },
                        undefined,
                        consumerSignal,
                    );
                }),
            ]);

            if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                throw new DOMException("Aborted", "AbortError");
            }

            const merged = mergeWorkspaceIntoDetail(
                detailRef.current,
                coreRes.project,
                workspaceRes,
                activeTasks,
            );
            detailRef.current = merged;
            return {
                detail: merged,
                truncatedActiveTasks: activeTasks.length >= 100,
            };
        },
        [checkOwnerCurrent, projectId, unitId],
    );

    const executeRunnerWithLock = useCallback(
        async (
            targetLog: BatchLog,
            expectedOwner: string,
            expectedEpoch: number,
            options?: { observeOnly?: boolean; expectedPreviousBatchId?: string | null; expectedSnapshot?: string | null },
        ): Promise<void> => {
            if (!webLocksSupported) {
                throw new Error("当前浏览器环境不支持 Web Locks 多标签页协调，已禁用批量生成");
            }
            const identity: BatchIdentity = {
                ownerScope: expectedOwner,
                projectId,
                unitId,
            };
            const storage = resolveBatchStorage(identity);
            const lockName = batchChapterLockName(identity);
            const signal = mountAbortRef.current?.signal ?? new AbortController().signal;

            await navigator.locks.request(lockName, { ifAvailable: true }, async lock => {
                if (!lock) {
                    if (options?.observeOnly) return;
                    throw new Error("本章已在其他标签页处理批量任务");
                }
                if (!checkOwnerCurrent(expectedOwner, expectedEpoch) || signal.aborted) {
                    return;
                }

                // Re-read storage after acquiring lock
                const storedAfterLock = await storage.read();
                if (!checkOwnerCurrent(expectedOwner, expectedEpoch) || signal.aborted) {
                    return;
                }

                if (!options?.observeOnly && (
                    editorDirtyRef.current ||
                    hasActiveOrUnknownItems(storedAfterLock) ||
                    (options?.expectedSnapshot !== undefined && JSON.stringify(storedAfterLock) !== options.expectedSnapshot)
                )) {
                    logRef.current = storedAfterLock;
                    setLog(storedAfterLock);
                    throw new Error("本章批次或脚本已更新，请先核对当前批次状态并重新预览确认");
                }
                if (!options?.observeOnly && options && "expectedPreviousBatchId" in options) {
                    const prevId = options.expectedPreviousBatchId ?? null;
                    if (storedAfterLock && storedAfterLock.batchId !== prevId && storedAfterLock.batchId !== targetLog.batchId) {
                        const recovered = recoverBatchLog(storedAfterLock);
                        logRef.current = recovered;
                        setLog(recovered);
                        throw new Error("本章已在其他标签页启动或更新了批次，请先核对当前批次状态");
                    }
                    if (
                        storedAfterLock &&
                        storedAfterLock.batchId === prevId &&
                        storedAfterLock.batchId !== targetLog.batchId &&
                        hasActiveOrUnknownItems(storedAfterLock)
                    ) {
                        const recovered = recoverBatchLog(storedAfterLock);
                        logRef.current = recovered;
                        setLog(recovered);
                        throw new Error("本章已有未结束或待核对的批次任务，不能覆盖启动新批次");
                    }
                }

                let activeLog = targetLog;
                if (options?.observeOnly) {
                    if (!storedAfterLock) return;
                    activeLog = recoverBatchLog(storedAfterLock);
                    if (JSON.stringify(activeLog) !== JSON.stringify(storedAfterLock)) await storage.write(activeLog);
                    logRef.current = activeLog;
                    setLog(activeLog);
                }

                // Ensure unit workflow exists when starting non-observe batch
                let targetStepId = "";
                if (!options?.observeOnly) {
                    const { detail: freshDetail, truncatedActiveTasks } = await fetchFreshSnapshot(
                        expectedOwner,
                        expectedEpoch,
                        signal,
                    );
                    if (truncatedActiveTasks) {
                        activeLog = {
                            ...activeLog,
                            state: "paused",
                            updatedAt: defaultNow(),
                        };
                        await storage.write(activeLog);
                        if (checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                            logRef.current = activeLog;
                            setLog(activeLog);
                            setRuntimeError("当前项目活动任务达到列表上限（100 条），无法完整确认在途状态，已暂停批量提交");
                        }
                        return;
                    }

                    let wfInfo = findUnitWorkflowStep(freshDetail, unitId, activeLog.stage);
                    if (!wfInfo.workflowFound) {
                        const created = await runGenerationConsumer(signal, async () => {
                            if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                                throw new DOMException("Aborted", "AbortError");
                            }
                            return createUnitWorkflow(projectId, unitId);
                        });
                        if (created?.workflow) {
                            detailRef.current = {
                                ...detailRef.current,
                                workflows: [...(detailRef.current.workflows || []), created.workflow],
                            };
                        }
                        const refreshed = await fetchFreshSnapshot(expectedOwner, expectedEpoch, signal);
                        await onRefreshRef.current();
                        wfInfo = findUnitWorkflowStep(refreshed.detail, unitId, activeLog.stage);
                    }
                    if (!wfInfo.step) {
                        throw new Error("当前章节缺少目标工作流步骤，无法提交批量任务");
                    }
                    if (wfInfo.step.status === "failed") {
                        throw new Error("当前章节工作流步骤处于失败状态，请先处理后再发起批量生成");
                    }
                    targetStepId = wfInfo.step.id;

                    await storage.write(activeLog);
                    if (checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                        logRef.current = activeLog;
                        setLog(activeLog);
                    }
                }

                const runner = createWorkflowBatchRunner(activeLog, {
                    maxConcurrentTasks: useUserStore.getState().runtimeLimits?.activeTaskLimit ?? 5,
                    save: async nextLog => {
                        await storage.write(nextLog);
                    },
                    onChange: nextLog => {
                        if (checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                            logRef.current = nextLog;
                            setLog(nextLog);
                        }
                    },
                    isOwnerCurrent: () => checkOwnerCurrent(expectedOwner, expectedEpoch),
                    sleep: defaultSleep,
                    now: defaultNow,
                    preflight: async (item, itemSignal) => {
                        const { detail: latestDetail, truncatedActiveTasks } = await fetchFreshSnapshot(
                            expectedOwner,
                            expectedEpoch,
                            itemSignal,
                        );
                        if (truncatedActiveTasks) {
                            const err = new Error("当前项目活动任务达到列表上限（100 条），无法完整确认在途状态") as Error & { issueCode?: string };
                            err.issueCode = "active_list_truncated";
                            throw err;
                        }
                        if (latestDetail.project.status !== "active") {
                            const err = new Error("项目已归档，停止提交") as Error & { issueCode?: string };
                            err.issueCode = "archived_project";
                            throw err;
                        }
                        const wfCheck = findUnitWorkflowStep(latestDetail, unitId, activeLog.stage);
                        if (!wfCheck.step || wfCheck.step.status === "failed") {
                            const err = new Error("章节工作流步骤不可用") as Error & { issueCode?: string };
                            err.issueCode = "workflow_unavailable";
                            throw err;
                        }
                        targetStepId = wfCheck.step.id;

                        const shot = (latestDetail.shots || []).find(
                            s => s.id === item.shotId && s.unitId === unitId,
                        );
                        const revision = shot?.currentRevisionId
                            ? (latestDetail.shotRevisions || []).find(
                                  r => r.id === shot.currentRevisionId && r.shotId === shot.id,
                              )
                            : undefined;
                        if (!shot || !revision || shot.currentRevisionId !== item.revisionId) {
                            const err = new Error("镜头修订已变化，请重新预览确认") as Error & { issueCode?: string };
                            err.issueCode = "revision_changed";
                            throw err;
                        }
                        const currentFp = await fingerprintBatchShot({
                            detail: latestDetail,
                            shot,
                            revision,
                            stage: activeLog.stage,
                            settings: activeLog.settings,
                            config: configRef.current,
                        });
                        if (currentFp !== item.fingerprint) {
                            const err = new Error("镜头内容或绑定资产已变化，请重新预览确认") as Error & { issueCode?: string };
                            err.issueCode = "revision_changed";
                            throw err;
                        }

                        // Check if shot already has active or ready artifact of same type
                        const currentLogWithoutSelf: BatchLog = {
                            ...activeLog,
                            items: (logRef.current?.items ?? activeLog.items).filter(
                                entry => entry.itemId !== item.itemId,
                            ),
                        };
                        const classified = classifyBatchCandidates({
                            detail: latestDetail,
                            unitId,
                            stage: activeLog.stage,
                            scope: activeLog.scope,
                            localItems: currentLogWithoutSelf.items,
                            settings: activeLog.settings,
                            config: configRef.current,
                        }).find(c => c.shotId === item.shotId);
                        if (classified) {
                            if (
                                classified.category === "ready" ||
                                classified.category === "active" ||
                                classified.category === "syncing"
                            ) {
                                const err = new Error("镜头已有同类型在途或有效成品") as Error & { issueCode?: string };
                                err.issueCode = classified.category;
                                throw err;
                            }
                            if (classified.category === "blocked") {
                                const err = new Error(classified.issues[0] || "镜头当前不可生成") as Error & { issueCode?: string };
                                err.issueCode = "preflight_blocked";
                                throw err;
                            }
                        }
                    },
                    submit: async (item, itemSignal, beforePost, savePreparation) => {
                        if (item.operation === "retry") {
                            if (!item.taskId) {
                                throw new Error("重试任务缺少原任务 ID");
                            }
                            const existingTask = await runGenerationConsumer(
                                itemSignal,
                                async consumerSignal => {
                                    if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                                        throw new DOMException("Aborted", "AbortError");
                                    }
                                    return queryGenerationTask(item.taskId!, { signal: consumerSignal });
                                },
                            );
                            if (
                                existingTask.status === "queued" ||
                                existingTask.status === "running" ||
                                existingTask.status === "succeeded"
                            ) {
                                return existingTask;
                            }
                            if (existingTask.canRecoverMedia) {
                                throw new ApiError("该任务图片已生成，请前往任务中心重试保存", {
                                    status: 409,
                                    code: 409,
                                    reason: "media_recovery_required",
                                });
                            }
                            item.taskAttemptsBefore = existingTask.attempts;
                            item.taskUpdatedAtBefore = existingTask.updatedAt;
                            await savePreparation();
                            try {
                                return await runGenerationConsumer(itemSignal, async () => {
                                    if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                                        throw new DOMException("Aborted", "AbortError");
                                    }
                                    beforePost();
                                    return retryGenerationTask(item.taskId!);
                                });
                            } catch (retryErr) {
                                if (retryErr instanceof DOMException && retryErr.name === "AbortError") throw retryErr;
                                // Check if retry was just accepted by another concurrent request
                                try {
                                    const rechecked = await runGenerationConsumer(
                                        itemSignal,
                                        async consumerSignal =>
                                            queryGenerationTask(item.taskId!, { signal: consumerSignal }),
                                    );
                                    if (
                                        rechecked.status === "queued" ||
                                        rechecked.status === "running"
                                    ) {
                                        return rechecked;
                                    }
                                } catch {
                                    // ignore recheck error and throw original retryErr
                                }
                                throw retryErr;
                            }
                        }

                        const latestDetail = detailRef.current;
                        const shot = (latestDetail.shots || []).find(
                            s => s.id === item.shotId && s.unitId === unitId,
                        );
                        const revision = shot?.currentRevisionId
                            ? (latestDetail.shotRevisions || []).find(
                                  r => r.id === shot.currentRevisionId && r.shotId === shot.id,
                              )
                            : undefined;
                        if (!shot || !revision) {
                            throw new Error("镜头或当前修订不存在");
                        }
                        const submission = await prepareWorkflowImageSubmission(
                            {
                                detail: latestDetail,
                                shot,
                                revision,
                                stage: activeLog.stage,
                                config: configRef.current,
                                settings: activeLog.settings,
                            },
                            {
                                workflowStepId: targetStepId,
                                skills: skillsRef.current,
                                batch: {
                                    batchId: activeLog.batchId,
                                    itemId: item.itemId,
                                },
                            },
                        );
                        return runGenerationConsumer(itemSignal, async consumerSignal => {
                            if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                                throw new DOMException("Aborted", "AbortError");
                            }
                            return submitBackendGenerationTask({
                                ...submission,
                                signal: consumerSignal,
                            }, {
                                createTask: taskInput => {
                                    beforePost();
                                    return createGenerationTask(taskInput);
                                },
                                waitTask: waitForGenerationTask,
                                createId: createClientUUID,
                            });
                        });
                    },
                    query: async (taskId, querySignal) =>
                        runGenerationConsumer(querySignal, async consumerSignal => {
                            if (!checkOwnerCurrent(expectedOwner, expectedEpoch)) {
                                throw new DOMException("Aborted", "AbortError");
                            }
                            return queryGenerationTask(taskId, { signal: consumerSignal });
                        }),
                    hasReadyArtifact: async (item, task) => {
                        const { detail: latestDetail } = await fetchFreshSnapshot(
                            expectedOwner,
                            expectedEpoch,
                            signal,
                        );
                        const expectedType = batchArtifactType(activeLog.stage);
                        const readyArtifact = (latestDetail.shotArtifacts || []).some(
                            art =>
                                art.shotId === item.shotId &&
                                art.type === expectedType &&
                                art.status === "ready" &&
                                Boolean(art.resourceId?.trim()) &&
                                art.revisionId === item.revisionId &&
                                (!art.taskId || art.taskId === task.id),
                        );
                        if (readyArtifact) {
                            void onRefreshRef.current();
                            return true;
                        }
                        return false;
                    },
                });

                activeRunnerRef.current = runner;
                try {
                    if (!options?.observeOnly && stopRequestedRef.current) await runner.stopRemaining();
                    await runner.start(signal, { observeOnly: options?.observeOnly });
                } finally {
                    if (activeRunnerRef.current === runner) {
                        activeRunnerRef.current = null;
                    }
                }
            });
        },
        [
            checkOwnerCurrent,
            fetchFreshSnapshot,
            projectId,
            unitId,
            webLocksSupported,
        ],
    );

    // Mount / account / chapter lifecycle effect
    useEffect(() => {
        epochRef.current += 1;
        const currentEpoch = epochRef.current;
        const abortController = new AbortController();
        mountAbortRef.current = abortController;

        setPreview(null);
        setCorruptedLogError(null);
        setRuntimeError(null);
        setStatusNotice(null);
        setIsResumeMode(false);

        if (!sessionReady || !user?.id) {
            logRef.current = null;
            setLog(null);
            return () => {
                abortController.abort();
            };
        }

        const ownerScope = user.id;
        const identity: BatchIdentity = { ownerScope, projectId, unitId };
        const storage = resolveBatchStorage(identity);

        void (async () => {
            try {
                const loaded = await storage.read();
                if (!checkOwnerCurrent(ownerScope, currentEpoch) || abortController.signal.aborted) {
                    return;
                }
                if (!loaded) {
                    logRef.current = null;
                    setLog(null);
                    return;
                }
                const recovered = loaded;
                if (!checkOwnerCurrent(ownerScope, currentEpoch) || abortController.signal.aborted) {
                    return;
                }
                logRef.current = recovered;
                setLog(recovered);
                setStage(recovered.stage);
                setScope(recovered.scope);
                setSettings(recovered.settings);

                // If there are already submitted/syncing items with taskId, observe them without submitting pending items
                if (webLocksSupported && hasActiveOrUnknownItems(recovered)) {
                    await executeRunnerWithLock(recovered, ownerScope, currentEpoch, {
                        observeOnly: true,
                    });
                }
            } catch (error) {
                if (!checkOwnerCurrent(ownerScope, currentEpoch) || abortController.signal.aborted) {
                    return;
                }
                setCorruptedLogError(
                    error instanceof Error ? error.message : "本地批次记录已损坏，无法解析",
                );
            }
        })();

        return () => {
            abortController.abort();
        };
    }, [
        checkOwnerCurrent,
        executeRunnerWithLock,
        projectId,
        sessionReady,
        unitId,
        user?.id,
    ]);

    const buildPreviewInternal = useCallback(
        async (
            targetStage: BatchImageStage,
            targetScope: BatchScope,
            targetSettings: BatchImageSettings,
            resumeFromLog?: BatchLog | null,
        ) => {
            if (editorDirtyRef.current) {
                setPreview(null);
                setRuntimeError("当前镜头脚本有未保存修改，请先保存当前镜头后再执行批量生成");
                return;
            }
            if (!sessionReady || !user?.id) {
                setRuntimeError("当前账号状态尚未就绪，请稍候再试");
                return;
            }
            const ownerScope = user.id;
            const currentEpoch = epochRef.current;
            const seq = ++previewSeqRef.current;
            const normalizedSettings = normalizeImageSettings(
                targetSettings,
                configRef.current,
                detailRef.current.project.aspectRatio,
            );

            setStage(targetStage);
            setScope(targetScope);
            setSettings(normalizedSettings);
            setPreview(null);
            setRuntimeError(null);
            setBusy(true);

            try {
                const stored = await resolveBatchStorage({ ownerScope, projectId, unitId }).read();
                if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) return;
                previewBaselineRef.current = JSON.stringify(stored);
                if (hasActiveOrUnknownItems(stored)) {
                    logRef.current = stored;
                    setLog(stored);
                    throw new Error("本章已有在途或待核对任务，请先核对当前批次");
                }
                if (resumeFromLog) {
                    if (!stored || stored.batchId !== resumeFromLog.batchId) throw new Error("批次记录已更新，请重新打开");
                    resumeFromLog = stored;
                }
                const { detail: freshDetail, truncatedActiveTasks } = await fetchFreshSnapshot(
                    ownerScope,
                    currentEpoch,
                );
                if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) {
                    return;
                }
                if (truncatedActiveTasks) {
                    setRuntimeError(
                        "当前项目活动任务达到列表上限（100 条），无法完整确认在途状态，已暂停批量提交",
                    );
                    return;
                }

                // If resume mode, re-evaluate remaining pending/stopped items in resumeFromLog
                if (resumeFromLog) {
                    const updatedItems: BatchItem[] = [];
                    const resumeSubmitItems: BatchItem[] = [];
                    const candidates = classifyBatchCandidates({
                        detail: freshDetail,
                        unitId,
                        stage: targetStage,
                        scope: targetScope,
                        localItems: [],
                        settings: normalizedSettings,
                        config: configRef.current,
                    });
                    const candidateByShot = new Map(candidates.map(c => [c.shotId, c]));

                    for (const item of resumeFromLog.items) {
                        if (item.state !== "pending" && item.state !== "stopped") {
                            updatedItems.push({ ...item });
                            continue;
                        }
                        const shot = (freshDetail.shots || []).find(
                            s => s.id === item.shotId && s.unitId === unitId,
                        );
                        const revision = shot?.currentRevisionId
                            ? (freshDetail.shotRevisions || []).find(
                                  r => r.id === shot.currentRevisionId && r.shotId === shot.id,
                              )
                            : undefined;
                        if (!shot || !revision || shot.currentRevisionId !== item.revisionId) {
                            updatedItems.push({
                                ...item,
                                state: "blocked",
                                issueCode: "revision_changed",
                                updatedAt: defaultNow(),
                            });
                            const cand = candidateByShot.get(item.shotId);
                            if (cand) {
                                cand.category = "blocked";
                                cand.eligible = false;
                                cand.issues = ["镜头修订已发生变化，已阻止自动继续"];
                            }
                            continue;
                        }
                        const fp = await fingerprintBatchShot({
                            detail: freshDetail,
                            shot,
                            revision,
                            stage: targetStage,
                            settings: normalizedSettings,
                            config: configRef.current,
                        });
                        // If settings stayed identical, fingerprint must match; if settings changed, update fingerprint for new confirmation token
                        const baselineFp = await fingerprintBatchShot({
                            detail: freshDetail,
                            shot,
                            revision,
                            stage: targetStage,
                            settings: resumeFromLog.settings,
                            config: configRef.current,
                        });
                        if (baselineFp !== item.fingerprint) {
                            updatedItems.push({
                                ...item,
                                state: "blocked",
                                issueCode: "revision_changed",
                                updatedAt: defaultNow(),
                            });
                            const cand = candidateByShot.get(item.shotId);
                            if (cand) {
                                cand.category = "blocked";
                                cand.eligible = false;
                                cand.issues = ["镜头脚本或参考资产已变化，已阻止自动继续"];
                            }
                            continue;
                        }
                        const cand = candidateByShot.get(item.shotId);
                        if (
                            cand &&
                            (cand.category === "ready" ||
                                cand.category === "active" ||
                                cand.category === "syncing" ||
                                cand.category === "blocked")
                        ) {
                            updatedItems.push({
                                ...item,
                                state: "blocked",
                                issueCode: cand.category,
                                updatedAt: defaultNow(),
                            });
                            cand.eligible = false;
                            continue;
                        }
                        const nextItem: BatchItem = {
                            ...item,
                            fingerprint: fp,
                            state: "pending",
                            issueCode: undefined,
                            updatedAt: defaultNow(),
                        };
                        updatedItems.push(nextItem);
                        resumeSubmitItems.push(nextItem);
                        if (cand) {
                            cand.eligible = true;
                        }
                    }

                    // Preview is read-only; only the confirmed lock owner may persist changes.
                    const updatedLog: BatchLog = {
                        ...resumeFromLog,
                        items: updatedItems,
                        updatedAt: defaultNow(),
                    };
                    if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) {
                        return;
                    }
                    logRef.current = updatedLog;
                    setLog(updatedLog);

                    // Compute price only for remaining resumeSubmitItems
                    const basePreview = await buildBatchPreview({
                        identity: { ownerScope, projectId, unitId },
                        detail: freshDetail,
                        stage: targetStage,
                        scope: targetScope,
                        settings: normalizedSettings,
                        config: configRef.current,
                        skills: skillsRef.current,
                        localItems: [],
                        creditsEnabled: useUserStore.getState().features.creditsEnabled,
                        now: defaultNow,
                        quoteFetcher: quoteModel,
                    });
                    if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) {
                        return;
                    }
                    const unitQuote =
                        basePreview.items.length > 0
                            ? basePreview.price.unknownCount > 0
                                ? null
                                : Math.round(
                                      basePreview.price.knownMicrocredits / basePreview.items.length,
                                  )
                            : 0;
                    const price = summarizeBatchPrices(
                        resumeSubmitItems.map(() => unitQuote),
                        basePreview.price.estimated,
                    );
                    const confirmationToken = await sha256Hex({
                        identity: { ownerScope, projectId, unitId },
                        resumeBatchId: resumeFromLog.batchId,
                        stage: targetStage,
                        scope: targetScope,
                        settings: normalizedSettings,
                        items: resumeSubmitItems.map(i => ({
                            itemId: i.itemId,
                            shotId: i.shotId,
                            revisionId: i.revisionId,
                            fingerprint: i.fingerprint,
                            operation: i.operation,
                            taskId: i.taskId ?? "",
                        })),
                    });

                    consumedTokenRef.current = null;
                    setIsResumeMode(true);
                    setPreview({
                        identity: { ownerScope, projectId, unitId },
                        stage: targetStage,
                        scope: targetScope,
                        settings: normalizedSettings,
                        candidates,
                        items: resumeSubmitItems,
                        price,
                        confirmationToken,
                    });
                    return;
                }

                setIsResumeMode(false);
                let detailForPreview = freshDetail;
                let failedTaskPriceUnknown = false;

                if (targetScope === "failed") {
                    const expectedArtifactType = batchArtifactType(targetStage);
                    const unitShotIds = new Set(
                        (freshDetail.shots || [])
                            .filter(s => s.unitId === unitId)
                            .map(s => s.id),
                    );
                    const failedTasks = (freshDetail.tasks || []).filter(
                        t =>
                            (t.status === "failed" || t.status === "cancelled") &&
                            t.clientContext?.shotId &&
                            unitShotIds.has(t.clientContext.shotId) &&
                            t.clientContext?.artifactType === expectedArtifactType,
                    );
                    const queriedTasks: GenerationTask[] = [];
                    for (const failedTask of failedTasks) {
                        const fullTask = await runGenerationConsumer(
                            undefined,
                            async consumerSignal => {
                                if (!checkOwnerCurrent(ownerScope, currentEpoch)) {
                                    throw new DOMException("Aborted", "AbortError");
                                }
                                return queryGenerationTask(failedTask.id, {
                                    signal: consumerSignal,
                                });
                            },
                        );
                        queriedTasks.push(fullTask);
                        if (
                            fullTask.model &&
                            !configuredModelMatchesCapability(
                                configRef.current,
                                fullTask.model,
                                "image",
                            )
                        ) {
                            failedTaskPriceUnknown = true;
                        }
                    }
                    detailForPreview = mergeWorkspaceIntoDetail(
                        freshDetail,
                        freshDetail.project,
                        undefined,
                        queriedTasks,
                    );
                    detailRef.current = detailForPreview;
                }

                const effectiveLogForPreview = hasActiveOrUnknownItems(logRef.current)
                    ? logRef.current
                    : null;

                const built = await buildBatchPreview({
                    identity: { ownerScope, projectId, unitId },
                    detail: detailForPreview,
                    stage: targetStage,
                    scope: targetScope,
                    settings: normalizedSettings,
                    config: configRef.current,
                    skills: skillsRef.current,
                    localItems: effectiveLogForPreview?.items,
                    creditsEnabled: useUserStore.getState().features.creditsEnabled,
                    now: defaultNow,
                    quoteFetcher: quoteModel,
                });

                if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) {
                    return;
                }

                // Additional metadata validation for failed scope retry candidates
                if (targetScope === "failed") {
                    const expectedType = batchArtifactType(targetStage);
                    const taskById = new Map(
                        (detailForPreview.tasks || []).map(t => [t.id, t]),
                    );
                    const shotById = new Map(
                        (detailForPreview.shots || []).map(s => [s.id, s]),
                    );
                    const validItems: BatchItem[] = [];
                    const updatedCandidates: BatchCandidate[] = built.candidates.map(cand => {
                        if (!cand.eligible || !cand.taskId) return cand;
                        const task = taskById.get(cand.taskId);
                        const shot = shotById.get(cand.shotId);
                        const meta = task ? parseTaskMetadata(task) : {};
                        if (
                            !task ||
                            !shot ||
                            meta.shotId !== shot.id ||
                            meta.shotRevisionId !== shot.currentRevisionId ||
                            meta.artifactType !== expectedType
                        ) {
                            return {
                                ...cand,
                                category: "blocked" as const,
                                eligible: false,
                                issues: ["原任务元数据与当前镜头或修订不匹配，不可直接重试"],
                            };
                        }
                        return cand;
                    });
                    const eligibleShots = new Set(
                        updatedCandidates.filter(c => c.eligible).map(c => c.shotId),
                    );
                    for (const item of built.items) {
                        if (!eligibleShots.has(item.shotId)) continue;
                        const task = item.taskId ? taskById.get(item.taskId) : undefined;
                        validItems.push({
                            ...item,
                            taskAttemptsBefore: task?.attempts,
                            taskUpdatedAtBefore: task?.updatedAt,
                        });
                    }
                    const price = failedTaskPriceUnknown
                        ? {
                              knownMicrocredits: 0,
                              unknownCount: validItems.length,
                              estimated: true,
                          }
                        : validItems.length === built.items.length
                          ? built.price
                          : summarizeBatchPrices(
                                validItems.map(() =>
                                    built.price.unknownCount > 0
                                        ? null
                                        : built.items.length > 0
                                          ? Math.round(
                                                built.price.knownMicrocredits / built.items.length,
                                            )
                                          : 0,
                                ),
                                true,
                            );
                    consumedTokenRef.current = null;
                    setPreview({
                        ...built,
                        candidates: updatedCandidates,
                        items: validItems,
                        price,
                    });
                    return;
                }

                consumedTokenRef.current = null;
                setPreview(built);
            } catch (error) {
                if (seq !== previewSeqRef.current || !checkOwnerCurrent(ownerScope, currentEpoch)) {
                    return;
                }
                setRuntimeError(error instanceof Error ? error.message : "批量生成预检失败");
            } finally {
                if (seq === previewSeqRef.current && checkOwnerCurrent(ownerScope, currentEpoch)) {
                    setBusy(false);
                }
            }
        },
        [checkOwnerCurrent, fetchFreshSnapshot, projectId, sessionReady, unitId, user?.id],
    );

    const open = useCallback(
        async (targetStage: BatchImageStage) => {
            setRuntimeError(null);
            setStatusNotice(null);
            if (editorDirty && !logRef.current && !corruptedLogError) {
                setRuntimeError("当前镜头脚本有未保存修改，请先保存当前镜头后再执行批量生成");
                return;
            }
            if (!sessionReady || !user?.id) {
                setRuntimeError("当前账号状态尚未就绪，请稍候再试");
                return;
            }
            if (detailRef.current.project.status === "archived") {
                setRuntimeError("归档项目为只读状态，不可发起批量生成");
                return;
            }
            if (!webLocksSupported) {
                setRuntimeError("当前浏览器环境不支持 Web Locks 多标签页协调，已禁用批量生成");
                return;
            }

            setDialogOpen(true);

            if (corruptedLogError) {
                return;
            }

            const currentLog = logRef.current;
            if (currentLog && editorDirtyRef.current) {
                setStage(currentLog.stage);
                setScope(currentLog.scope);
                setSettings(currentLog.settings);
                setPreview(null);
                return;
            }
            if (currentLog && hasActiveOrUnknownItems(currentLog)) {
                setStage(currentLog.stage);
                setScope(currentLog.scope);
                setSettings(currentLog.settings);
                if (currentLog.stage !== targetStage) {
                    setStatusNotice(
                        `当前章节存在未结束的${currentLog.stage === "previz" ? "动作预演" : "分镜图"}批次，请先处理当前批次`,
                    );
                }
                return;
            }

            if (
                currentLog &&
                currentLog.stage === targetStage &&
                currentLog.items.some(i => i.state === "pending" || i.state === "stopped")
            ) {
                await buildPreviewInternal(
                    currentLog.stage,
                    currentLog.scope,
                    currentLog.settings,
                    currentLog,
                );
                return;
            }

            const nextSettings = normalizeImageSettings(
                settings,
                configRef.current,
                detailRef.current.project.aspectRatio,
            );
            await buildPreviewInternal(targetStage, "missing", nextSettings, null);
        },
        [
            buildPreviewInternal,
            corruptedLogError,
            editorDirty,
            sessionReady,
            settings,
            user?.id,
            webLocksSupported,
        ],
    );

    const refreshPreview = useCallback(
        async (nextScope: BatchScope, nextSettings: BatchImageSettings) => {
            const effectiveSettings =
                nextScope === "failed" && scope === "failed" ? settings : nextSettings;
            await buildPreviewInternal(
                stage,
                nextScope,
                effectiveSettings,
                isResumeMode ? logRef.current : null,
            );
        },
        [buildPreviewInternal, isResumeMode, scope, settings, stage],
    );

    const prepareResume = useCallback(async () => {
        const currentLog = logRef.current;
        if (!currentLog || hasActiveOrUnknownItems(currentLog)) {
            return;
        }
        await buildPreviewInternal(
            currentLog.stage,
            currentLog.scope,
            currentLog.settings,
            currentLog,
        );
    }, [buildPreviewInternal]);

    const prepareRetryFailed = useCallback(async () => {
        const currentLog = logRef.current;
        if (currentLog && hasActiveOrUnknownItems(currentLog)) {
            return;
        }
        const targetStage = currentLog?.stage ?? stage;
        const targetSettings = currentLog?.settings ?? settings;
        setIsResumeMode(false);
        await buildPreviewInternal(targetStage, "failed", targetSettings, null);
    }, [buildPreviewInternal, settings, stage]);

    const confirm = useCallback(async () => {
        if (confirmingRef.current) {
            return;
        }
        if (!sessionReady || !user?.id || !preview || preview.items.length === 0) {
            return;
        }
        if (consumedTokenRef.current === preview.confirmationToken) {
            return;
        }
        if (editorDirty) {
            setRuntimeError("当前镜头脚本有未保存修改，请先保存当前镜头后再执行批量生成");
            return;
        }
        if (corruptedLogError) {
            setRuntimeError("本地批次记录已损坏，请先处理后再启动");
            return;
        }
        const currentLog = logRef.current;
        if (currentLog && hasActiveOrUnknownItems(currentLog)) {
            setRuntimeError("当前章节已有在途或待核对的批量任务，不能重复启动");
            return;
        }

        confirmingRef.current = true;
        stopRequestedRef.current = false;
        consumedTokenRef.current = preview.confirmationToken;
        const confirmedPreview = preview;
        const ownerScope = user.id;
        const currentEpoch = epochRef.current;
        const now = defaultNow();

        let targetLog: BatchLog;
        const previousBatchId = currentLog?.batchId ?? null;

        if (isResumeMode && currentLog) {
            const resumeMap = new Map(confirmedPreview.items.map(i => [i.itemId, i]));
            targetLog = {
                ...currentLog,
                settings: confirmedPreview.settings,
                state: "running",
                items: currentLog.items.map(existing => {
                    const resumed = resumeMap.get(existing.itemId);
                    if (resumed) {
                        return {
                            ...resumed,
                            state: "pending" as const,
                            issueCode: undefined,
                            updatedAt: now,
                        };
                    }
                    return { ...existing };
                }),
                updatedAt: now,
            };
        } else {
            targetLog = {
                version: 1,
                ownerScope,
                projectId,
                unitId,
                batchId: `batch-${createClientUUID()}`,
                stage: confirmedPreview.stage,
                scope: confirmedPreview.scope,
                settings: confirmedPreview.settings,
                state: "running",
                items: confirmedPreview.items.map(item => ({
                    ...item,
                    state: "pending" as const,
                    updatedAt: now,
                })),
                createdAt: now,
                updatedAt: now,
            };
        }

        setBusy(true);
        setPreview(null);
        setRuntimeError(null);
        setIsResumeMode(false);

        try {
            await executeRunnerWithLock(targetLog, ownerScope, currentEpoch, {
                observeOnly: false,
                expectedPreviousBatchId: previousBatchId,
                expectedSnapshot: previewBaselineRef.current,
            });
            if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                await onRefreshRef.current();
            }
        } catch (error) {
            if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                setRuntimeError(error instanceof Error ? error.message : "批量生成执行失败");
            }
        } finally {
            confirmingRef.current = false;
            if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                setBusy(false);
            }
        }
    }, [
        checkOwnerCurrent,
        corruptedLogError,
        editorDirty,
        executeRunnerWithLock,
        isResumeMode,
        preview,
        projectId,
        sessionReady,
        unitId,
        user?.id,
    ]);

    const stopRemaining = useCallback(async () => {
        if (!sessionReady || !user?.id) return;
        const ownerScope = user.id;
        const currentEpoch = epochRef.current;
        stopRequestedRef.current = true;
        if (activeRunnerRef.current) {
            await activeRunnerRef.current.stopRemaining();
            return;
        }
        if (confirmingRef.current) {
            setStatusNotice("已请求停止剩余提交，正在等待准备阶段结束");
            return;
        }
        const identity = { ownerScope, projectId, unitId };
        try {
            await withBatchChapterLock(identity, async () => {
                const storage = resolveBatchStorage(identity);
                const stored = await storage.read();
                if (!stored || !checkOwnerCurrent(ownerScope, currentEpoch)) return;
                const currentLog = recoverBatchLog(stored);
                const now = defaultNow();
                const nextLog: BatchLog = {
                    ...currentLog,
                    state: "stopped",
                    items: currentLog.items.map(item => item.state === "pending"
                        ? { ...item, state: "stopped" as const, updatedAt: now } : { ...item }),
                    updatedAt: now,
                };
                await storage.write(nextLog);
                if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                    logRef.current = nextLog;
                    setLog(nextLog);
                    setPreview(null);
                }
            });
        } catch (error) {
            if (checkOwnerCurrent(ownerScope, currentEpoch)) setRuntimeError(error instanceof Error ? error.message : "停止失败");
        }
    }, [checkOwnerCurrent, projectId, sessionReady, unitId, user?.id]);

    const refreshStatus = useCallback(
        async (manualTaskId?: string) => {
            if (!sessionReady || !user?.id) return;
            if (!logRef.current) return;
            const ownerScope = user.id;
            const currentEpoch = epochRef.current;
            if (activeRunnerRef.current || confirmingRef.current) {
                setStatusNotice("当前批次正在自动核对，刷新不会覆盖运行中的日志");
                await onRefreshRef.current();
                return;
            }

            setBusy(true);
            setRuntimeError(null);
            setStatusNotice(null);

            try {
                await withBatchChapterLock({ ownerScope, projectId, unitId }, async () => {
                const storage = resolveBatchStorage({ ownerScope, projectId, unitId });
                const stored = await storage.read();
                if (!stored || !checkOwnerCurrent(ownerScope, currentEpoch)) return;
                const currentLog = recoverBatchLog(stored);
                const { detail: freshDetail } = await fetchFreshSnapshot(ownerScope, currentEpoch);
                if (!checkOwnerCurrent(ownerScope, currentEpoch)) return;

                let recentTasks: GenerationTask[] = [];
                try {
                    recentTasks = await runGenerationConsumer(undefined, async consumerSignal => {
                        if (!checkOwnerCurrent(ownerScope, currentEpoch)) {
                            throw new DOMException("Aborted", "AbortError");
                        }
                        return listGenerationTasks(
                            100,
                            { projectId, activeOnly: false },
                            undefined,
                            consumerSignal,
                        );
                    });
                } catch {
                    recentTasks = [];
                }

                const expectedArtifactType = batchArtifactType(currentLog.stage);
                const now = defaultNow();
                const nextItems: BatchItem[] = [];

                for (const item of currentLog.items) {
                    if (
                        item.state === "submitted" ||
                        item.state === "syncing" ||
                        (item.state === "unknown" && item.operation === "retry" && item.taskId)
                    ) {
                        const task = await runGenerationConsumer(undefined, async consumerSignal => {
                            if (!checkOwnerCurrent(ownerScope, currentEpoch)) {
                                throw new DOMException("Aborted", "AbortError");
                            }
                            return queryGenerationTask(item.taskId!, { signal: consumerSignal });
                        });

                        if (item.state === "unknown" && item.operation === "retry") {
                            const progressed =
                                task.status === "queued" ||
                                task.status === "running" ||
                                task.status === "succeeded" ||
                                (typeof item.taskAttemptsBefore === "number" &&
                                    task.attempts > item.taskAttemptsBefore) ||
                                (Boolean(item.taskUpdatedAtBefore) &&
                                    task.updatedAt > item.taskUpdatedAtBefore!);
                            if (!progressed) {
                                // Still at pre-retry failed baseline; cannot claim retry was not accepted
                                nextItems.push({ ...item });
                                continue;
                            }
                        }

                        if (task.status === "queued" || task.status === "running") {
                            nextItems.push({
                                ...item,
                                state: "submitted",
                                issueCode: undefined,
                                updatedAt: now,
                            });
                            continue;
                        }
                        if (task.status === "failed" || task.status === "cancelled") {
                            nextItems.push({
                                ...item,
                                state: "failed",
                                issueCode: task.canRecoverMedia
                                    ? "media_recovery_required"
                                    : "task_failed",
                                updatedAt: now,
                            });
                            continue;
                        }
                        if (task.status === "succeeded") {
                            const hasReady = (freshDetail.shotArtifacts || []).some(
                                art =>
                                    art.shotId === item.shotId &&
                                    art.type === expectedArtifactType &&
                                    art.status === "ready" &&
                                    Boolean(art.resourceId?.trim()) &&
                                    art.revisionId === item.revisionId &&
                                    (!art.taskId || art.taskId === task.id),
                            );
                            nextItems.push({
                                ...item,
                                state: hasReady ? "succeeded" : "syncing",
                                issueCode: hasReady ? undefined : "artifact_sync_timeout",
                                updatedAt: now,
                            });
                            continue;
                        }
                    }

                    if (item.state === "unknown" && !item.taskId) {
                        const candidateIds = new Set<string>();
                        if (manualTaskId?.trim()) {
                            candidateIds.add(manualTaskId.trim());
                        }
                        for (const t of [...(freshDetail.tasks || []), ...recentTasks]) {
                            if (
                                t.projectId === projectId &&
                                (!t.clientContext?.shotId || t.clientContext.shotId === item.shotId)
                            ) {
                                candidateIds.add(t.id);
                            }
                        }

                        const matchedTasks: GenerationTask[] = [];
                        for (const candidateId of candidateIds) {
                            try {
                                const fullTask = await runGenerationConsumer(
                                    undefined,
                                    async consumerSignal => {
                                        if (!checkOwnerCurrent(ownerScope, currentEpoch)) {
                                            throw new DOMException("Aborted", "AbortError");
                                        }
                                        return queryGenerationTask(candidateId, {
                                            signal: consumerSignal,
                                        });
                                    },
                                );
                                const meta = parseTaskMetadata(fullTask);
                                if (
                                    fullTask.projectId === projectId &&
                                    meta.domainProjectId === projectId &&
                                    meta.unitId === unitId &&
                                    meta.shotId === item.shotId &&
                                    meta.shotRevisionId === item.revisionId &&
                                    meta.artifactType === expectedArtifactType &&
                                    meta.chapterImageBatchId === currentLog.batchId &&
                                    meta.chapterImageBatchItemId === item.itemId
                                ) {
                                    matchedTasks.push(fullTask);
                                }
                            } catch {
                                // ignore individual query error when probing candidates
                            }
                        }

                        if (matchedTasks.length === 1) {
                            const matched = matchedTasks[0]!;
                            if (matched.status === "queued" || matched.status === "running") {
                                nextItems.push({
                                    ...item,
                                    taskId: matched.id,
                                    state: "submitted",
                                    issueCode: undefined,
                                    updatedAt: now,
                                });
                            } else if (matched.status === "failed" || matched.status === "cancelled") {
                                nextItems.push({
                                    ...item,
                                    taskId: matched.id,
                                    state: "failed",
                                    issueCode: matched.canRecoverMedia
                                        ? "media_recovery_required"
                                        : "task_failed",
                                    updatedAt: now,
                                });
                            } else {
                                const hasReady = (freshDetail.shotArtifacts || []).some(
                                    art =>
                                        art.shotId === item.shotId &&
                                        art.type === expectedArtifactType &&
                                        art.status === "ready" &&
                                        Boolean(art.resourceId?.trim()) &&
                                        art.revisionId === item.revisionId &&
                                        (!art.taskId || art.taskId === matched.id),
                                );
                                nextItems.push({
                                    ...item,
                                    taskId: matched.id,
                                    state: hasReady ? "succeeded" : "syncing",
                                    issueCode: hasReady ? undefined : "artifact_sync_timeout",
                                    updatedAt: now,
                                });
                            }
                            continue;
                        }

                        setStatusNotice(
                            "未找到唯一下游任务匹配该未确认提交，已保持未知状态以防止重复生图扣费；请前往任务中心核对或输入确切任务 ID",
                        );
                    }

                    nextItems.push({ ...item });
                }

                const allDone = nextItems.every(
                    i => i.state === "succeeded" || i.state === "failed",
                );
                const nextLog: BatchLog = {
                    ...currentLog,
                    state: allDone ? "finished" : currentLog.state === "stopped" ? "stopped" : "paused",
                    items: nextItems,
                    updatedAt: now,
                };

                if (!checkOwnerCurrent(ownerScope, currentEpoch)) return;
                await storage.write(nextLog);
                if (!checkOwnerCurrent(ownerScope, currentEpoch)) return;
                logRef.current = nextLog;
                setLog(nextLog);
                await onRefreshRef.current();
                });
            } catch (error) {
                if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                    setRuntimeError(error instanceof Error ? error.message : "刷新状态失败");
                }
            } finally {
                if (checkOwnerCurrent(ownerScope, currentEpoch)) {
                    setBusy(false);
                }
            }
        },
        [checkOwnerCurrent, fetchFreshSnapshot, projectId, sessionReady, unitId, user?.id],
    );

    const clearCorruptedLog = useCallback(async () => {
        if (!sessionReady || !user?.id) return;
        const ownerScope = user.id;
        const currentEpoch = epochRef.current;
        try {
            await withBatchChapterLock({ ownerScope, projectId, unitId }, async () => {
                const storage = resolveBatchStorage({ ownerScope, projectId, unitId });
                try {
                    const stored = await storage.read();
                    if (stored) throw new Error("当前批次记录有效，不能作为损坏记录清理");
                    return;
                } catch (error) {
                    if (error instanceof Error && error.message === "当前批次记录有效，不能作为损坏记录清理") throw error;
                }
                if (!checkOwnerCurrent(ownerScope, currentEpoch)) return;
                await storage.remove();
                if (!checkOwnerCurrent(ownerScope, currentEpoch)) return;
                setCorruptedLogError(null);
                logRef.current = null;
                setLog(null);
                setStatusNotice("已清理损坏的本地批次记录；如之前已有提交中的任务，请先在任务中心核对历史任务状态");
            });
        } catch (error) {
            if (checkOwnerCurrent(ownerScope, currentEpoch)) setRuntimeError(error instanceof Error ? error.message : "清理失败");
        }
    }, [checkOwnerCurrent, projectId, sessionReady, unitId, user?.id]);

    const runChapterMutation = useCallback(async <T,>(operation: (assertCurrent: () => void) => Promise<T>, shotId?: string): Promise<T> => {
        if (!sessionReady || !user?.id) throw new Error("当前账号状态尚未就绪");
        const ownerScope = user.id;
        const currentEpoch = epochRef.current;
        const assertCurrent = () => {
            if (!checkOwnerCurrent(ownerScope, currentEpoch)) throw new DOMException("Aborted", "AbortError");
        };
        const identity = { ownerScope, projectId, unitId };
        const execute = async () => {
            const stored = await resolveBatchStorage(identity).read();
            assertCurrent();
            const blocks = (entry: BatchLog | null) => entry && (shotId
                ? entry.items.some(item => item.shotId === shotId && ["pending", "submitting", "submitted", "syncing", "unknown"].includes(item.state))
                : hasUnsettledItems(entry));
            if (activeRunnerRef.current || confirmingRef.current || blocks(stored) || blocks(logRef.current)) {
                throw new Error("当前章节正在参与批量任务，请先完成核对或停止剩余提交后再修改");
            }
            return operation(assertCurrent);
        };
        // Unsupported browsers cannot run batches; preserve ordinary writes only after checking their log.
        return webLocksSupported ? withBatchChapterLock(identity, execute) : execute();
    }, [checkOwnerCurrent, projectId, sessionReady, unitId, user?.id, webLocksSupported]);

    const closeDialog = useCallback(() => {
        setDialogOpen(false);
    }, []);

    const blockedReason = useMemo(() => {
        if (!sessionReady) return "当前账号状态尚未就绪，请稍候再试";
        if (detail.project.status === "archived") return "归档项目为只读状态，不可发起批量生成";
        if (!webLocksSupported) return "当前浏览器环境不支持 Web Locks 多标签页协调，已禁用批量生成";
        if (corruptedLogError) return `本地批次记录已损坏：${corruptedLogError}`;
        if (editorDirty) return "当前镜头脚本有未保存修改，请先保存当前镜头后再执行批量生成";
        return null;
    }, [corruptedLogError, detail.project.status, editorDirty, sessionReady, webLocksSupported]);

    const chapterMutationLocked = useMemo(() => hasUnsettledItems(log), [log]);

    const lockedShotIds = useMemo(() => {
        const ids = new Set<string>();
        if (!log) return ids;
        for (const item of log.items) {
            if (
                item.state === "pending" ||
                item.state === "submitting" ||
                item.state === "submitted" ||
                item.state === "syncing" ||
                item.state === "unknown"
            ) {
                ids.add(item.shotId);
            }
        }
        return ids;
    }, [log]);

    const canConfirm = Boolean(
        !busy &&
            !blockedReason &&
            !runtimeError &&
            preview &&
            preview.items.length > 0 &&
            !hasActiveOrUnknownItems(log),
    );

    return {
        stage,
        settings,
        scope,
        log,
        preview,
        busy,
        confirming: confirmingRef.current,
        blockedReason,
        canConfirm,
        chapterMutationLocked,
        lockedShotIds,
        open,
        refreshPreview,
        confirm,
        stopRemaining,
        refreshStatus,
        closeDialog,
        dialogOpen,
        corruptedLogError,
        runtimeError,
        statusNotice,
        activeTaskLimit,
        clearCorruptedLog,
        prepareResume,
        prepareRetryFailed,
        runChapterMutation,
    };
}
