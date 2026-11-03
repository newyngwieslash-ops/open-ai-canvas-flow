import type { StateStorage } from "zustand/middleware";
import { localForageStorageForScope } from "../../../lib/localforage-storage";
import type {
    BatchIdentity,
    BatchImageSettings,
    BatchImageStage,
    BatchItem,
    BatchItemState,
    BatchLog,
    BatchRunState,
    BatchScope,
} from "./workflow-batch-plan";

const VALID_STAGES = new Set<BatchImageStage>(["storyboard", "previz"]);
const VALID_SCOPES = new Set<BatchScope>(["missing", "failed", "stale"]);
const VALID_RUN_STATES = new Set<BatchRunState>([
    "paused",
    "running",
    "waiting",
    "stopped",
    "finished",
]);
const VALID_ITEM_STATES = new Set<BatchItemState>([
    "pending",
    "submitting",
    "submitted",
    "syncing",
    "succeeded",
    "failed",
    "blocked",
    "unknown",
    "stopped",
]);

function isNonEmptyString(value: unknown): value is string {
    return typeof value === "string" && value.trim().length > 0;
}

function isNonNegativeInt(value: unknown): value is number {
    return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function validateIdentity(identity: BatchIdentity): void {
    if (
        !isNonEmptyString(identity.ownerScope) ||
        !isNonEmptyString(identity.projectId) ||
        !isNonEmptyString(identity.unitId)
    ) {
        throw new Error("批次身份标识不完整");
    }
}

function encodeBatchLog(log: BatchLog): string {
    return JSON.stringify({
        version: 1,
        ownerScope: log.ownerScope,
        projectId: log.projectId,
        unitId: log.unitId,
        batchId: log.batchId,
        stage: log.stage,
        scope: log.scope,
        settings: {
            model: log.settings.model,
            size: log.settings.size,
            quality: log.settings.quality,
            skillIds: [...log.settings.skillIds],
        },
        state: log.state,
        items: log.items.map(item => ({
            itemId: item.itemId,
            shotId: item.shotId,
            revisionId: item.revisionId,
            fingerprint: item.fingerprint,
            operation: item.operation,
            state: item.state,
            taskId: item.taskId,
            attempts: item.attempts,
            taskAttemptsBefore: item.taskAttemptsBefore,
            taskUpdatedAtBefore: item.taskUpdatedAtBefore,
            updatedAt: item.updatedAt,
            issueCode: item.issueCode,
        })),
        createdAt: log.createdAt,
        updatedAt: log.updatedAt,
    });
}

export function decodeBatchLog(raw: string, identity: BatchIdentity): BatchLog {
    validateIdentity(identity);
    let parsed: unknown;
    try {
        parsed = JSON.parse(raw);
    } catch {
        throw new Error("本地批次记录已损坏，无法解析");
    }
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        throw new Error("本地批次记录格式无效");
    }
    const record = parsed as Record<string, unknown>;
    if (record.version !== 1) {
        throw new Error("本地批次记录版本不受支持");
    }
    if (
        !isNonEmptyString(record.ownerScope) ||
        !isNonEmptyString(record.projectId) ||
        !isNonEmptyString(record.unitId) ||
        !isNonEmptyString(record.batchId) ||
        !isNonEmptyString(record.createdAt) ||
        !isNonEmptyString(record.updatedAt)
    ) {
        throw new Error("本地批次记录缺少必要标识字段");
    }
    if (
        record.ownerScope !== identity.ownerScope ||
        record.projectId !== identity.projectId ||
        record.unitId !== identity.unitId
    ) {
        throw new Error("本地批次记录归属与当前章节或账号不匹配");
    }
    if (!VALID_STAGES.has(record.stage as BatchImageStage)) {
        throw new Error("本地批次记录阶段无效");
    }
    if (!VALID_SCOPES.has(record.scope as BatchScope)) {
        throw new Error("本地批次记录范围无效");
    }
    if (!VALID_RUN_STATES.has(record.state as BatchRunState)) {
        throw new Error("本地批次记录运行状态无效");
    }
    if (!record.settings || typeof record.settings !== "object" || Array.isArray(record.settings)) {
        throw new Error("本地批次记录参数配置无效");
    }
    const rawSettings = record.settings as Record<string, unknown>;
    if (
        !isNonEmptyString(rawSettings.model) ||
        typeof rawSettings.size !== "string" ||
        typeof rawSettings.quality !== "string" ||
        !Array.isArray(rawSettings.skillIds) ||
        !rawSettings.skillIds.every(id => isNonEmptyString(id))
    ) {
        throw new Error("本地批次记录模型参数无效");
    }
    const settings: BatchImageSettings = {
        model: rawSettings.model,
        size: rawSettings.size,
        quality: rawSettings.quality,
        skillIds: [...(rawSettings.skillIds as string[])],
    };

    if (!Array.isArray(record.items)) {
        throw new Error("本地批次记录镜头列表无效");
    }

    const seenItemIds = new Set<string>();
    const seenShotIds = new Set<string>();
    const items: BatchItem[] = record.items.map(rawItem => {
        if (!rawItem || typeof rawItem !== "object" || Array.isArray(rawItem)) {
            throw new Error("本地批次记录镜头项格式无效");
        }
        const item = rawItem as Record<string, unknown>;
        if (
            !isNonEmptyString(item.itemId) ||
            !isNonEmptyString(item.shotId) ||
            !isNonEmptyString(item.revisionId) ||
            !isNonEmptyString(item.fingerprint) ||
            !isNonEmptyString(item.updatedAt)
        ) {
            throw new Error("本地批次记录镜头项缺少必要标识");
        }
        if (seenItemIds.has(item.itemId) || seenShotIds.has(item.shotId)) {
            throw new Error("本地批次记录存在重复镜头项");
        }
        seenItemIds.add(item.itemId);
        seenShotIds.add(item.shotId);

        if (item.operation !== "create" && item.operation !== "retry") {
            throw new Error("本地批次记录镜头操作类型无效");
        }
        if (!VALID_ITEM_STATES.has(item.state as BatchItemState)) {
            throw new Error("本地批次记录镜头状态无效");
        }
        if (!isNonNegativeInt(item.attempts)) {
            throw new Error("本地批次记录尝试次数无效");
        }

        const taskId = item.taskId === undefined ? undefined : item.taskId;
        if (taskId !== undefined && !isNonEmptyString(taskId)) {
            throw new Error("本地批次记录任务 ID 无效");
        }
        if (
            (item.state === "submitted" ||
                item.state === "syncing" ||
                item.state === "succeeded" ||
                item.operation === "retry") &&
            !isNonEmptyString(taskId)
        ) {
            throw new Error("本地批次记录缺少必要的任务 ID");
        }

        const taskAttemptsBefore = item.taskAttemptsBefore;
        if (taskAttemptsBefore !== undefined && !isNonNegativeInt(taskAttemptsBefore)) {
            throw new Error("本地批次记录任务重试基线次数无效");
        }
        const taskUpdatedAtBefore = item.taskUpdatedAtBefore;
        if (taskUpdatedAtBefore !== undefined && !isNonEmptyString(taskUpdatedAtBefore)) {
            throw new Error("本地批次记录任务重试基线时间无效");
        }
        const issueCode = item.issueCode;
        if (issueCode !== undefined && !isNonEmptyString(issueCode)) {
            throw new Error("本地批次记录问题代码无效");
        }

        const decodedItem: BatchItem = {
            itemId: item.itemId,
            shotId: item.shotId,
            revisionId: item.revisionId,
            fingerprint: item.fingerprint,
            operation: item.operation,
            state: item.state as BatchItemState,
            attempts: item.attempts,
            updatedAt: item.updatedAt,
        };
        if (taskId !== undefined) decodedItem.taskId = taskId;
        if (taskAttemptsBefore !== undefined) decodedItem.taskAttemptsBefore = taskAttemptsBefore;
        if (taskUpdatedAtBefore !== undefined) decodedItem.taskUpdatedAtBefore = taskUpdatedAtBefore;
        if (issueCode !== undefined) decodedItem.issueCode = issueCode;
        return decodedItem;
    });

    return {
        version: 1,
        ownerScope: record.ownerScope,
        projectId: record.projectId,
        unitId: record.unitId,
        batchId: record.batchId,
        stage: record.stage as BatchImageStage,
        scope: record.scope as BatchScope,
        settings,
        state: record.state as BatchRunState,
        items,
        createdAt: record.createdAt,
        updatedAt: record.updatedAt,
    };
}

export function recoverBatchLog(log: BatchLog): BatchLog {
    let hasRecoveredSubmitting = false;
    const items = log.items.map(item => {
        if (item.state === "submitting") {
            hasRecoveredSubmitting = true;
            return {
                ...item,
                state: "unknown" as const,
                issueCode: "submit_unknown",
            };
        }
        return { ...item };
    });

    const nextState: BatchRunState =
        log.state === "running" || log.state === "waiting" || (log.state === "finished" && hasRecoveredSubmitting)
            ? "paused"
            : log.state;

    return {
        ...log,
        settings: {
            ...log.settings,
            skillIds: [...log.settings.skillIds],
        },
        state: nextState,
        items,
    };
}

export function batchChapterLockName(identity: BatchIdentity): string {
    return `yingce:chapter-image-batch:${identity.ownerScope}:${identity.projectId}:${identity.unitId}`;
}

export async function withBatchChapterLock<T>(identity: BatchIdentity, operation: () => Promise<T>): Promise<T> {
    if (typeof navigator === "undefined" || !navigator.locks?.request) {
        throw new Error("当前浏览器环境不支持 Web Locks 多标签页协调，已禁用批量生成");
    }
    return navigator.locks.request(batchChapterLockName(identity), { ifAvailable: true }, lock => {
        if (!lock) throw new Error("本章已在其他标签页处理批量任务");
        return operation();
    });
}

let storageFactoryForTest: ((identity: BatchIdentity) => ReturnType<typeof createBatchStorage>) | undefined;

export function configureBatchStorageForTest(factory?: (identity: BatchIdentity) => ReturnType<typeof createBatchStorage>): void {
    storageFactoryForTest = factory;
}

export function resolveBatchStorage(identity: BatchIdentity): ReturnType<typeof createBatchStorage> {
    return storageFactoryForTest ? storageFactoryForTest(identity) : createBatchStorage(identity);
}

export function batchHasUnsettledItems(log: BatchLog | null): boolean {
    return Boolean(log && (log.state === "running" || log.state === "waiting" || log.items.some(item =>
        ["pending", "submitting", "submitted", "syncing", "unknown"].includes(item.state),
    )));
}

// Instances in the same document share a queue; Web Locks serialize the CAS across tabs.
const storageQueues = new Map<string, Promise<void>>();

export function createBatchStorage(
    identity: BatchIdentity,
    storage?: StateStorage,
): {
    read(): Promise<BatchLog | null>;
    write(log: BatchLog): Promise<void>;
    remove(): Promise<void>;
} {
    validateIdentity(identity);
    const fixedStorage = storage ?? localForageStorageForScope(identity.ownerScope);
    const key = `yingce:chapter-image-batch:v1:${encodeURIComponent(identity.projectId)}:${encodeURIComponent(identity.unitId)}`;
    const queueKey = JSON.stringify([identity.ownerScope, identity.projectId, identity.unitId]);
    let expectedRaw: string | null = null;

    const enqueue = <T>(operation: () => Promise<T>): Promise<T> => {
        const run = () => {
            if (typeof navigator !== "undefined" && navigator.locks?.request) {
                return navigator.locks.request(`yingce:chapter-image-batch-storage:${queueKey}`, operation);
            }
            return operation();
        };
        const next = (storageQueues.get(queueKey) ?? Promise.resolve()).then(run, run);
        const settled = next.then(() => undefined, () => undefined);
        storageQueues.set(queueKey, settled);
        void settled.then(() => {
            if (storageQueues.get(queueKey) === settled) storageQueues.delete(queueKey);
        });
        return next;
    };
    const assertUnchanged = async () => {
        if ((await fixedStorage.getItem(key) ?? null) !== expectedRaw) {
            throw new Error("批次记录已更新，请重新核对，不能覆盖旧快照");
        }
    };

    return {
        read(): Promise<BatchLog | null> {
            return enqueue(async () => {
                expectedRaw = await fixedStorage.getItem(key) ?? null;
                return expectedRaw === null ? null : decodeBatchLog(expectedRaw, identity);
            });
        },
        write(log: BatchLog): Promise<void> {
            // Capture now, not after a preceding asynchronous write has completed.
            const encoded = encodeBatchLog(decodeBatchLog(encodeBatchLog(log), identity));
            return enqueue(async () => {
                await assertUnchanged();
                await fixedStorage.setItem(key, encoded);
                if ((await fixedStorage.getItem(key)) !== encoded) {
                    throw new Error("批次记录未可靠保存，已停止新增提交");
                }
                expectedRaw = encoded;
            });
        },
        remove(): Promise<void> {
            return enqueue(async () => {
                await assertUnchanged();
                await fixedStorage.removeItem(key);
                expectedRaw = null;
            });
        },
    };
}
