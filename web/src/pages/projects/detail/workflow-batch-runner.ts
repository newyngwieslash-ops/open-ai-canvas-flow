import { TASK_SUBMISSION_REASONS } from "../../../services/api/error-codes";
import { ApiError } from "../../../services/api/request";
import type { GenerationTask } from "../../../services/api/task-center";
import type { BatchItem, BatchLog } from "./workflow-batch-plan";

export type BatchRunnerDependencies = {
    save(log: BatchLog): Promise<void>;
    preflight(item: BatchItem, signal: AbortSignal): Promise<void>;
    submit(item: BatchItem, signal: AbortSignal, beforePost: () => void, savePreparation: () => Promise<void>): Promise<GenerationTask>;
    query(taskId: string, signal: AbortSignal): Promise<GenerationTask>;
    hasReadyArtifact(item: BatchItem, task: GenerationTask): Promise<boolean>;
    isOwnerCurrent(): boolean;
    sleep(ms: number, signal: AbortSignal): Promise<void>;
    now(): string;
    onChange(log: BatchLog): void;
};

export function submissionRetryDelay(error: unknown, attempt: number): number | null {
    if (!(error instanceof ApiError) || attempt >= 5) return null;
    if (error.reason === TASK_SUBMISSION_REASONS.activeLimitExceeded && error.status === 400) {
        return [2000, 4000, 8000, 16000, 30000][attempt] ?? null;
    }
    if (
        error.reason === TASK_SUBMISSION_REASONS.rateLimited &&
        error.code === 42901 &&
        error.status === 429
    ) {
        return error.retryAfterMs ?? 30000;
    }
    return null;
}

function isCapacityOrRateError(error: unknown): error is ApiError {
    if (!(error instanceof ApiError)) return false;
    if (error.reason === TASK_SUBMISSION_REASONS.activeLimitExceeded && error.status === 400) {
        return true;
    }
    if (
        error.reason === TASK_SUBMISSION_REASONS.rateLimited &&
        error.code === 42901 &&
        error.status === 429
    ) {
        return true;
    }
    return false;
}

function isDeterministic4xxRejection(error: unknown): error is ApiError {
    if (!(error instanceof ApiError)) return false;
    if (typeof error.status !== "number") return false;
    return error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429;
}

function isAbortError(error: unknown): boolean {
    return error instanceof DOMException && error.name === "AbortError";
}

function extractIssueCode(error: unknown): string | undefined {
    if (error && typeof error === "object" && "issueCode" in error) {
        const code = (error as { issueCode?: unknown }).issueCode;
        if (typeof code === "string" && code.trim()) {
            return code.trim();
        }
    }
    return undefined;
}

function cloneLog(log: BatchLog): BatchLog {
    return {
        ...log,
        settings: {
            ...log.settings,
            skillIds: [...log.settings.skillIds],
        },
        items: log.items.map(item => ({ ...item })),
    };
}

export function createWorkflowBatchRunner(
    initial: BatchLog,
    deps: BatchRunnerDependencies,
): {
    start(signal: AbortSignal, options?: { observeOnly?: boolean }): Promise<void>;
    stopRemaining(): Promise<void>;
    snapshot(): BatchLog;
} {
    const log = cloneLog(initial);
    let activeRunPromise: Promise<void> | null = null;
    let stopRequested = log.state === "stopped";
    let storageFailed = false;
    const inFlightSubmitItemIds = new Set<string>();
    let backoffAbortController: AbortController | null = null;

    const notify = () => {
        deps.onChange(cloneLog(log));
    };

    const persist = async (targetItem?: BatchItem): Promise<boolean> => {
        notify();
        try {
            await deps.save(cloneLog(log));
            return true;
        } catch {
            storageFailed = true;
            if (targetItem) {
                targetItem.issueCode = "storage_failed";
                targetItem.updatedAt = deps.now();
            }
            log.state = "paused";
            log.updatedAt = deps.now();
            notify();
            return false;
        }
    };

    const sleepWithBackoffAbort = async (ms: number, outerSignal: AbortSignal): Promise<void> => {
        const controller = new AbortController();
        backoffAbortController = controller;
        const onOuterAbort = () => controller.abort();
        if (outerSignal.aborted || stopRequested) {
            backoffAbortController = null;
            throw new DOMException("Aborted", "AbortError");
        }
        outerSignal.addEventListener("abort", onOuterAbort, { once: true });
        try {
            await deps.sleep(ms, controller.signal);
        } finally {
            outerSignal.removeEventListener("abort", onOuterAbort);
            if (backoffAbortController === controller) {
                backoffAbortController = null;
            }
        }
    };

    const observeTask = async (
        item: BatchItem,
        signal: AbortSignal,
        initialTask?: GenerationTask,
    ): Promise<boolean> => {
        if (!item.taskId) {
            item.state = "unknown";
            item.issueCode = "submit_unknown";
            item.updatedAt = deps.now();
            log.state = "paused";
            log.updatedAt = deps.now();
            await persist(item);
            return false;
        }

        let queryFailures = 0;
        let syncElapsedMs = 0;
        let currentTask: GenerationTask | undefined = initialTask;
        let useInitialTask =
            currentTask !== undefined &&
            (currentTask.status === "succeeded" ||
                currentTask.status === "failed" ||
                currentTask.status === "cancelled");

        while (true) {
            if (signal.aborted || !deps.isOwnerCurrent() || storageFailed) {
                return false;
            }

            if (!useInitialTask) {
                try {
                    currentTask = await deps.query(item.taskId, signal);
                    queryFailures = 0;
                } catch (error) {
                    if (signal.aborted || !deps.isOwnerCurrent() || isAbortError(error)) {
                        return false;
                    }
                    queryFailures += 1;
                    if (queryFailures <= 3) {
                        const retryDelay = [2000, 4000, 8000][queryFailures - 1] ?? 8000;
                        try {
                            await deps.sleep(retryDelay, signal);
                        } catch {
                            return false;
                        }
                        continue;
                    }
                    item.issueCode = "task_read_failed";
                    item.updatedAt = deps.now();
                    log.state = "paused";
                    log.updatedAt = deps.now();
                    await persist(item);
                    return false;
                }
            }
            useInitialTask = false;

            if (!currentTask || signal.aborted || !deps.isOwnerCurrent()) {
                return false;
            }

            if (currentTask.status === "queued" || currentTask.status === "running") {
                try {
                    await deps.sleep(2000, signal);
                } catch {
                    return false;
                }
                continue;
            }

            if (currentTask.status === "failed" || currentTask.status === "cancelled") {
                item.state = "failed";
                item.issueCode = currentTask.canRecoverMedia
                    ? "media_recovery_required"
                    : "task_failed";
                item.updatedAt = deps.now();
                log.updatedAt = deps.now();
                const saved = await persist(item);
                return saved;
            }

            if (currentTask.status === "succeeded") {
                let ready = false;
                try {
                    ready = await deps.hasReadyArtifact(item, currentTask);
                } catch {
                    ready = false;
                }
                if (signal.aborted || !deps.isOwnerCurrent()) {
                    return false;
                }
                if (ready) {
                    item.state = "succeeded";
                    item.issueCode = undefined;
                    item.updatedAt = deps.now();
                    log.updatedAt = deps.now();
                    const saved = await persist(item);
                    return saved;
                }

                if (item.state !== "syncing") {
                    item.state = "syncing";
                    item.updatedAt = deps.now();
                    log.updatedAt = deps.now();
                    if (!(await persist(item))) {
                        return false;
                    }
                }

                if (syncElapsedMs >= 30000) {
                    item.state = "syncing";
                    item.issueCode = "artifact_sync_timeout";
                    item.updatedAt = deps.now();
                    log.state = "paused";
                    log.updatedAt = deps.now();
                    await persist(item);
                    return false;
                }

                try {
                    await deps.sleep(2000, signal);
                } catch {
                    return false;
                }
                syncElapsedMs += 2000;
                continue;
            }

            return false;
        }
    };

    const runLoop = async (
        signal: AbortSignal,
        options?: { observeOnly?: boolean },
    ): Promise<void> => {
        const observeOnly = options?.observeOnly === true;

        // First observe any already submitted or syncing items
        for (const item of log.items) {
            if (signal.aborted || !deps.isOwnerCurrent() || storageFailed) {
                return;
            }
            if (item.state === "submitted" || item.state === "syncing") {
                const ok = await observeTask(item, signal);
                if (!ok) {
                    return;
                }
            }
        }

        if (observeOnly) {
            return;
        }

        // Never submit new items if any item is in unknown state
        if (log.items.some(item => item.state === "unknown")) {
            if (log.state !== "paused" && log.state !== "stopped") {
                log.state = "paused";
                log.updatedAt = deps.now();
                await persist();
            }
            return;
        }

        const isBatchStopped = () => stopRequested || (log.state as BatchLog["state"]) === "stopped";

        if (isBatchStopped()) {
            return;
        }

        if (log.state !== "running") {
            log.state = "running";
            log.updatedAt = deps.now();
            if (!(await persist())) {
                return;
            }
        }

        for (const item of log.items) {
            if (
                signal.aborted ||
                !deps.isOwnerCurrent() ||
                isBatchStopped() ||
                storageFailed
            ) {
                break;
            }
            if (item.state !== "pending") {
                continue;
            }

            let rejectionAttempt = 0;
            let submittedTask: GenerationTask | null = null;
            let shouldBreakBatch = false;

            while (true) {
                if (
                    signal.aborted ||
                    !deps.isOwnerCurrent() ||
                    isBatchStopped() ||
                    storageFailed
                ) {
                    shouldBreakBatch = true;
                    break;
                }

                try {
                    await deps.preflight(item, signal);
                } catch (error) {
                    if (
                        signal.aborted ||
                        !deps.isOwnerCurrent() ||
                        isBatchStopped() ||
                        isAbortError(error)
                    ) {
                        if (isBatchStopped()) {
                            item.state = "stopped";
                            item.updatedAt = deps.now();
                            await persist(item);
                        }
                        shouldBreakBatch = true;
                        break;
                    }
                    item.state = "blocked";
                    item.issueCode = extractIssueCode(error) ?? "preflight_blocked";
                    item.updatedAt = deps.now();
                    log.state = "paused";
                    log.updatedAt = deps.now();
                    await persist(item);
                    shouldBreakBatch = true;
                    break;
                }

                if (
                    signal.aborted ||
                    !deps.isOwnerCurrent() ||
                    isBatchStopped()
                ) {
                    if (isBatchStopped()) {
                        item.state = "stopped";
                        item.updatedAt = deps.now();
                        await persist(item);
                    }
                    shouldBreakBatch = true;
                    break;
                }

                item.state = "submitting";
                item.updatedAt = deps.now();
                if (!isBatchStopped()) {
                    log.state = "running";
                }
                log.updatedAt = deps.now();

                if (!(await persist(item))) {
                    item.state = "pending";
                    notify();
                    shouldBreakBatch = true;
                    break;
                }

                // 必须再次检查：存储 await 期间可能停止、离开或换号。
                if (
                    signal.aborted ||
                    !deps.isOwnerCurrent() ||
                    isBatchStopped()
                ) {
                    item.state = isBatchStopped() ? "stopped" : "pending";
                    item.updatedAt = deps.now();
                    log.updatedAt = deps.now();
                    await persist(item);
                    shouldBreakBatch = true;
                    break;
                }

                let postStarted = false;
                const beforePost = () => {
                    if (signal.aborted || !deps.isOwnerCurrent() || isBatchStopped() || storageFailed) {
                        throw new DOMException("Aborted", "AbortError");
                    }
                    postStarted = true;
                    inFlightSubmitItemIds.add(item.itemId);
                };
                try {
                    const task = await deps.submit(item, signal, beforePost, async () => {
                        if (!(await persist(item))) throw new Error("批次记录未可靠保存，已停止新增提交");
                    });
                    inFlightSubmitItemIds.delete(item.itemId);
                    submittedTask = task;
                    item.state = "submitted";
                    item.taskId = task.id;
                    item.attempts += 1;
                    item.issueCode = undefined;
                    item.updatedAt = deps.now();
                    if (!isBatchStopped() && log.state === "waiting") {
                        log.state = "running";
                    }
                    log.updatedAt = deps.now();
                    if (!(await persist(item))) {
                        shouldBreakBatch = true;
                    }
                    break;
                } catch (error) {
                    inFlightSubmitItemIds.delete(item.itemId);
                    if (!postStarted) {
                        item.state = isBatchStopped() ? "stopped" : signal.aborted || !deps.isOwnerCurrent() ? "pending" : "blocked";
                        item.issueCode = item.state === "blocked" ? extractIssueCode(error) ?? "preflight_blocked" : undefined;
                        item.updatedAt = deps.now();
                        if (!isBatchStopped()) log.state = "paused";
                        log.updatedAt = deps.now();
                        await persist(item);
                        shouldBreakBatch = true;
                        break;
                    }

                    const delay = submissionRetryDelay(error, rejectionAttempt);
                    if (delay !== null) {
                        rejectionAttempt += 1;
                        item.attempts += 1;
                        item.state = isBatchStopped() ? "stopped" : "pending";
                        item.issueCode =
                            error instanceof ApiError &&
                            error.reason === TASK_SUBMISSION_REASONS.activeLimitExceeded
                                ? "capacity_wait"
                                : "rate_wait";
                        item.updatedAt = deps.now();
                        if (!isBatchStopped()) {
                            log.state = "waiting";
                        }
                        log.updatedAt = deps.now();
                        if (!(await persist(item))) {
                            shouldBreakBatch = true;
                            break;
                        }
                        if (isBatchStopped()) {
                            shouldBreakBatch = true;
                            break;
                        }
                        try {
                            await sleepWithBackoffAbort(delay, signal);
                        } catch {
                            if (isBatchStopped()) {
                                item.state = "stopped";
                                item.updatedAt = deps.now();
                                log.updatedAt = deps.now();
                                await persist(item);
                            }
                            shouldBreakBatch = true;
                            break;
                        }
                        continue;
                    }

                    if (isCapacityOrRateError(error)) {
                        item.attempts += 1;
                        item.state = isBatchStopped() ? "stopped" : "pending";
                        item.issueCode =
                            error.reason === TASK_SUBMISSION_REASONS.activeLimitExceeded
                                ? "capacity_wait"
                                : "rate_wait";
                        item.updatedAt = deps.now();
                        if (!isBatchStopped()) {
                            log.state = "paused";
                        }
                        log.updatedAt = deps.now();
                        await persist(item);
                        shouldBreakBatch = true;
                        break;
                    }

                    if (isDeterministic4xxRejection(error)) {
                        item.attempts += 1;
                        item.state = "blocked";
                        item.issueCode =
                            extractIssueCode(error) ?? (error.reason || "submit_rejected");
                        item.updatedAt = deps.now();
                        if (!isBatchStopped()) {
                            log.state = "paused";
                        }
                        log.updatedAt = deps.now();
                        await persist(item);
                        shouldBreakBatch = true;
                        break;
                    }

                    // Network / timeout / 5xx / abort during submit -> unknown, never auto-replayed
                    item.attempts += 1;
                    item.state = "unknown";
                    item.issueCode = "submit_unknown";
                    item.updatedAt = deps.now();
                    if (!isBatchStopped()) {
                        log.state = "paused";
                    }
                    log.updatedAt = deps.now();
                    await persist(item);
                    shouldBreakBatch = true;
                    break;
                }
            }

            if (shouldBreakBatch) {
                break;
            }

            if (submittedTask) {
                const ok = await observeTask(item, signal, submittedTask);
                if (!ok) {
                    break;
                }
            }
        }

        if (
            !signal.aborted &&
            deps.isOwnerCurrent() &&
            !storageFailed &&
            !isBatchStopped() &&
            log.state !== "paused"
        ) {
            const allDone = log.items.every(
                item => item.state === "succeeded" || item.state === "failed",
            );
            if (allDone) {
                log.state = "finished";
                log.updatedAt = deps.now();
                await persist();
            }
        }
    };

    return {
        start(signal: AbortSignal, options?: { observeOnly?: boolean }): Promise<void> {
            if (activeRunPromise) {
                return activeRunPromise;
            }
            activeRunPromise = runLoop(signal, options).finally(() => {
                activeRunPromise = null;
            });
            return activeRunPromise;
        },
        async stopRemaining(): Promise<void> {
            stopRequested = true;
            log.state = "stopped";
            log.updatedAt = deps.now();
            for (const item of log.items) {
                if (
                    item.state === "pending" ||
                    (item.state === "submitting" && !inFlightSubmitItemIds.has(item.itemId))
                ) {
                    item.state = "stopped";
                    item.updatedAt = deps.now();
                }
            }
            backoffAbortController?.abort();
            await persist();
        },
        snapshot(): BatchLog {
            return cloneLog(log);
        },
    };
}
