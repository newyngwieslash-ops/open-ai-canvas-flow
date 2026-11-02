import { describe, expect, test } from "bun:test";
import type { StateStorage } from "zustand/middleware";
import type { BatchIdentity, BatchLog } from "../src/pages/projects/detail/workflow-batch-plan";
import {
    createBatchStorage,
    decodeBatchLog,
    recoverBatchLog,
} from "../src/pages/projects/detail/workflow-batch-storage";

const baseIdentity: BatchIdentity = {
    ownerScope: "user-a",
    projectId: "proj-1",
    unitId: "unit-1",
};

function makeValidLog(overrides: Partial<BatchLog> = {}): BatchLog {
    return {
        version: 1,
        ownerScope: baseIdentity.ownerScope,
        projectId: baseIdentity.projectId,
        unitId: baseIdentity.unitId,
        batchId: "batch-1",
        stage: "storyboard",
        scope: "missing",
        settings: {
            model: "gemini-3.1-flash-image-preview",
            size: "1K",
            quality: "1K",
            skillIds: ["skill-director-1"],
        },
        state: "running",
        items: [
            {
                itemId: "item-1",
                shotId: "shot-1",
                revisionId: "rev-1",
                fingerprint: "fp-1",
                operation: "create",
                state: "pending",
                attempts: 0,
                updatedAt: "2026-10-29T00:00:00Z",
            },
        ],
        createdAt: "2026-10-29T00:00:00Z",
        updatedAt: "2026-10-29T00:00:00Z",
        ...overrides,
    };
}

describe("workflow-batch-storage", () => {
    test("returns null when storage has no record and roundtrips whitelist fields only", async () => {
        const values = new Map<string, string>();
        const fakeStorage: StateStorage = {
            getItem: name => values.get(name) ?? null,
            setItem: (name, value) => {
                values.set(name, value);
            },
            removeItem: name => {
                values.delete(name);
            },
        };

        const storage = createBatchStorage(baseIdentity, fakeStorage);
        expect(await storage.read()).toBeNull();

        const dirtyLog = {
            ...makeValidLog(),
            channels: [{ apiKey: "sk-secret" }],
            apiKey: "sk-top-secret",
            prompt: "secret prompt text",
            referenceImages: ["https://secret.example/ref.png"],
            inputJson: "{\"secret\":true}",
            items: [
                {
                    itemId: "item-1",
                    shotId: "shot-1",
                    revisionId: "rev-1",
                    fingerprint: "fp-1",
                    operation: "create" as const,
                    state: "submitted" as const,
                    taskId: "task-1",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:01Z",
                    prompt: "per-item secret prompt",
                    inputJson: "{\"apiKey\":\"sk-123\"}",
                },
            ],
        } as unknown as BatchLog;

        await storage.write(dirtyLog);

        const expectedKey = "yingce:chapter-image-batch:v1:proj-1:unit-1";
        const raw = values.get(expectedKey);
        expect(raw).toBeDefined();
        expect(raw).not.toContain("channels");
        expect(raw).not.toContain("apiKey");
        expect(raw).not.toContain("prompt");
        expect(raw).not.toContain("referenceImages");
        expect(raw).not.toContain("inputJson");
        expect(raw).not.toContain("sk-secret");
        expect(raw).not.toContain("sk-top-secret");
        expect(raw).not.toContain("sk-123");

        const loaded = await storage.read();
        expect(loaded).not.toBeNull();
        expect(loaded?.batchId).toBe("batch-1");
        expect(loaded?.items[0]?.taskId).toBe("task-1");

        await storage.remove();
        expect(await storage.read()).toBeNull();
    });

    test("rejects corrupted JSON, wrong version, mismatched identity, duplicate IDs, invalid state, and missing taskId", async () => {
        const values = new Map<string, string>();
        const fakeStorage: StateStorage = {
            getItem: name => values.get(name) ?? null,
            setItem: (name, value) => {
                values.set(name, value);
            },
            removeItem: name => {
                values.delete(name);
            },
        };
        const storage = createBatchStorage(baseIdentity, fakeStorage);
        const key = "yingce:chapter-image-batch:v1:proj-1:unit-1";

        values.set(key, "{corrupted-json");
        await expect(storage.read()).rejects.toThrow();

        expect(() =>
            decodeBatchLog(JSON.stringify({ ...makeValidLog(), version: 2 }), baseIdentity),
        ).toThrow();

        expect(() =>
            decodeBatchLog(JSON.stringify({ ...makeValidLog(), ownerScope: "user-b" }), baseIdentity),
        ).toThrow();

        expect(() =>
            decodeBatchLog(JSON.stringify({ ...makeValidLog(), projectId: "proj-2" }), baseIdentity),
        ).toThrow();

        expect(() =>
            decodeBatchLog(JSON.stringify({ ...makeValidLog(), unitId: "unit-2" }), baseIdentity),
        ).toThrow();

        expect(() =>
            decodeBatchLog(JSON.stringify({ ...makeValidLog(), batchId: "" }), baseIdentity),
        ).toThrow();

        // Duplicate itemId
        expect(() =>
            decodeBatchLog(
                JSON.stringify(
                    makeValidLog({
                        items: [
                            {
                                itemId: "dup-item",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "create",
                                state: "pending",
                                attempts: 0,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                            {
                                itemId: "dup-item",
                                shotId: "shot-2",
                                revisionId: "rev-2",
                                fingerprint: "fp-2",
                                operation: "create",
                                state: "pending",
                                attempts: 0,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                        ],
                    }),
                ),
                baseIdentity,
            ),
        ).toThrow();

        // Duplicate shotId within same stage batch
        expect(() =>
            decodeBatchLog(
                JSON.stringify(
                    makeValidLog({
                        items: [
                            {
                                itemId: "item-1",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "create",
                                state: "pending",
                                attempts: 0,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                            {
                                itemId: "item-2",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "create",
                                state: "pending",
                                attempts: 0,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                        ],
                    }),
                ),
                baseIdentity,
            ),
        ).toThrow();

        // Invalid run state or item state
        expect(() =>
            decodeBatchLog(
                JSON.stringify({ ...makeValidLog(), state: "invalid_state" }),
                baseIdentity,
            ),
        ).toThrow();

        expect(() =>
            decodeBatchLog(
                JSON.stringify(
                    makeValidLog({
                        items: [
                            {
                                itemId: "item-1",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "create",
                                state: "bogus" as never,
                                attempts: 0,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                        ],
                    }),
                ),
                baseIdentity,
            ),
        ).toThrow();

        // submitted without taskId
        expect(() =>
            decodeBatchLog(
                JSON.stringify(
                    makeValidLog({
                        items: [
                            {
                                itemId: "item-1",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "create",
                                state: "submitted",
                                attempts: 1,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                        ],
                    }),
                ),
                baseIdentity,
            ),
        ).toThrow();

        // retry submitting without taskId
        expect(() =>
            decodeBatchLog(
                JSON.stringify(
                    makeValidLog({
                        items: [
                            {
                                itemId: "item-1",
                                shotId: "shot-1",
                                revisionId: "rev-1",
                                fingerprint: "fp-1",
                                operation: "retry",
                                state: "submitting",
                                attempts: 1,
                                updatedAt: "2026-10-29T00:00:00Z",
                            },
                        ],
                    }),
                ),
                baseIdentity,
            ),
        ).toThrow();
    });

    test("recoverBatchLog pauses running/waiting and converts all submitting items to unknown including retry with original taskId", () => {
        const log = makeValidLog({
            state: "running",
            items: [
                {
                    itemId: "item-create-submitting",
                    shotId: "shot-1",
                    revisionId: "rev-1",
                    fingerprint: "fp-1",
                    operation: "create",
                    state: "submitting",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:01Z",
                },
                {
                    itemId: "item-retry-submitting",
                    shotId: "shot-2",
                    revisionId: "rev-2",
                    fingerprint: "fp-2",
                    operation: "retry",
                    state: "submitting",
                    taskId: "task-orig-2",
                    attempts: 1,
                    taskAttemptsBefore: 1,
                    taskUpdatedAtBefore: "2026-10-29T00:00:00Z",
                    updatedAt: "2026-10-29T00:00:01Z",
                },
                {
                    itemId: "item-submitted",
                    shotId: "shot-3",
                    revisionId: "rev-3",
                    fingerprint: "fp-3",
                    operation: "create",
                    state: "submitted",
                    taskId: "task-3",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:01Z",
                },
                {
                    itemId: "item-pending",
                    shotId: "shot-4",
                    revisionId: "rev-4",
                    fingerprint: "fp-4",
                    operation: "create",
                    state: "pending",
                    attempts: 0,
                    updatedAt: "2026-10-29T00:00:01Z",
                },
            ],
        });

        const recovered = recoverBatchLog(log);
        expect(recovered.state).toBe("paused");
        expect(recovered.items[0]?.state).toBe("unknown");
        expect(recovered.items[0]?.issueCode).toBe("submit_unknown");
        expect(recovered.items[1]?.state).toBe("unknown");
        expect(recovered.items[1]?.issueCode).toBe("submit_unknown");
        expect(recovered.items[1]?.taskId).toBe("task-orig-2");
        expect(recovered.items[2]?.state).toBe("submitted");
        expect(recovered.items[2]?.taskId).toBe("task-3");
        expect(recovered.items[3]?.state).toBe("pending");

        const waitingRecovered = recoverBatchLog(makeValidLog({ state: "waiting" }));
        expect(waitingRecovered.state).toBe("paused");

        const stoppedRecovered = recoverBatchLog(makeValidLog({ state: "stopped" }));
        expect(stoppedRecovered.state).toBe("stopped");
    });

    test("separate adapters reject stale snapshots and serialize shared storage writes", async () => {
        const values = new Map<string, string>();
        const raw: StateStorage = {
            getItem: key => values.get(key) ?? null,
            setItem: (key, value) => { values.set(key, value); },
            removeItem: key => { values.delete(key); },
        };
        const first = createBatchStorage(baseIdentity, raw);
        await first.write(makeValidLog());
        const second = createBatchStorage(baseIdentity, raw);
        const stale = (await second.read())!;
        const latest = (await first.read())!;
        latest.items[0]!.state = "unknown";
        latest.items[0]!.issueCode = "submit_unknown";
        await first.write(latest);
        await expect(second.write(stale)).rejects.toThrow("批次记录已更新");
        expect((await first.read())!.items[0]!.state).toBe("unknown");

        const third = createBatchStorage(baseIdentity, raw);
        await third.read();
        latest.items[0]!.taskId = "accepted-task";
        await first.write(latest);
        await expect(third.remove()).rejects.toThrow("批次记录已更新");
        expect((await first.read())!.items[0]!.taskId).toBe("accepted-task");
    });

    test("throws when storage write throws or silently no-ops", async () => {
        const noopStorage: StateStorage = {
            getItem: () => null,
            setItem: () => {
                // Silent no-op simulating suppressed persistence
            },
            removeItem: () => {},
        };
        const storage1 = createBatchStorage(baseIdentity, noopStorage);
        await expect(storage1.write(makeValidLog())).rejects.toThrow(
            "批次记录未可靠保存，已停止新增提交",
        );

        const throwingStorage: StateStorage = {
            getItem: () => null,
            setItem: () => {
                throw new Error("QuotaExceededError");
            },
            removeItem: () => {},
        };
        const storage2 = createBatchStorage(baseIdentity, throwingStorage);
        await expect(storage2.write(makeValidLog())).rejects.toThrow("QuotaExceededError");
    });

    test("isolates delayed writes between user A and user B and serializes writes within same adapter", async () => {
        const rawMap = new Map<string, string>();
        let resolvePendingWriteA: (() => void) | null = null;

        const scopedFakeStorage = (scope: string): StateStorage => ({
            getItem: name => rawMap.get(`${name}:user:${scope}`) ?? null,
            setItem: async (name, value) => {
                if (scope === "user-a" && !resolvePendingWriteA) {
                    await new Promise<void>(resolve => {
                        resolvePendingWriteA = resolve;
                    });
                }
                rawMap.set(`${name}:user:${scope}`, value);
            },
            removeItem: name => {
                rawMap.delete(`${name}:user:${scope}`);
            },
        });

        const storageA = createBatchStorage(
            { ownerScope: "user-a", projectId: "proj-1", unitId: "unit-1" },
            scopedFakeStorage("user-a"),
        );
        const storageB = createBatchStorage(
            { ownerScope: "user-b", projectId: "proj-1", unitId: "unit-1" },
            scopedFakeStorage("user-b"),
        );

        const writePromiseA = storageA.write(makeValidLog({ ownerScope: "user-a", batchId: "batch-a" }));
        expect(await storageB.read()).toBeNull();

        resolvePendingWriteA?.();
        await writePromiseA;

        expect(await storageB.read()).toBeNull();
        expect((await storageA.read())?.batchId).toBe("batch-a");
    });
});
