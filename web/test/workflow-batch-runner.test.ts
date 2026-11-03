import { describe, expect, test } from "bun:test";
import type {
    BatchItem,
    BatchLog,
} from "../src/pages/projects/detail/workflow-batch-plan";
import {
    type BatchRunnerDependencies,
    createWorkflowBatchRunner,
    submissionRetryDelay,
} from "../src/pages/projects/detail/workflow-batch-runner";
import { TASK_SUBMISSION_REASONS } from "../src/services/api/error-codes";
import { ApiError } from "../src/services/api/request";
import { makeBatchTask } from "./helpers/workflow-batch-fixture";

function makeRunnerTask(
    shotId: string,
    status: "queued" | "running" | "succeeded" | "failed" | "cancelled" = "succeeded",
    overrides: Partial<ReturnType<typeof makeBatchTask>> = {},
) {
    return {
        ...makeBatchTask(shotId, status, "storyboard"),
        id: `task-${shotId}`,
        ...overrides,
    };
}

function createDeferred<T>() {
    let resolve!: (value: T | PromiseLike<T>) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

function makeRunnerLog(shotIds = ["shot-1", "shot-2", "shot-3"]): BatchLog {
    return {
        version: 1,
        ownerScope: "user-1",
        projectId: "proj-1",
        unitId: "unit-1",
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
        items: shotIds.map((shotId, idx) => ({
            itemId: `item-${idx + 1}`,
            shotId,
            revisionId: `rev-${idx + 1}`,
            fingerprint: `fp-${idx + 1}`,
            operation: "create" as const,
            state: "pending" as const,
            attempts: 0,
            updatedAt: "2026-10-29T00:00:00Z",
        })),
        createdAt: "2026-10-29T00:00:00Z",
        updatedAt: "2026-10-29T00:00:00Z",
    };
}

function createFakeSleeper() {
    const queue: Array<{
        ms: number;
        wake: () => void;
    }> = [];
    const sleep = (ms: number, signal: AbortSignal) =>
        new Promise<void>((resolve, reject) => {
            if (signal.aborted) {
                reject(new DOMException("Aborted", "AbortError"));
                return;
            }
            const entry = {
                ms,
                wake: () => {
                    signal.removeEventListener("abort", onAbort);
                    resolve();
                },
            };
            const onAbort = () => {
                const idx = queue.indexOf(entry);
                if (idx >= 0) queue.splice(idx, 1);
                reject(new DOMException("Aborted", "AbortError"));
            };
            signal.addEventListener("abort", onAbort, { once: true });
            queue.push(entry);
        });

    return {
        sleep,
        queue,
        wakeNext(): number | undefined {
            const next = queue.shift();
            if (!next) return undefined;
            next.wake();
            return next.ms;
        },
    };
}

async function flushMicrotasks(ticks = 25) {
    for (let i = 0; i < ticks; i += 1) {
        await Promise.resolve();
    }
}

describe("workflow-batch-runner", () => {
    test("submits strictly one active task at a time and saves submitting before submit and submitted+taskId immediately after", async () => {
        const submittedShots: string[] = [];
        const savedSnapshots: BatchLog[] = [];
        const taskStatuses = new Map<string, "queued" | "running" | "succeeded" | "failed">([
            ["task-shot-1", "running"],
            ["task-shot-2", "succeeded"],
        ]);
        const sleeper = createFakeSleeper();

        let stateAtSubmitCall: string | undefined;
        const deps: BatchRunnerDependencies = {
            save: async log => {
                savedSnapshots.push(structuredClone(log));
            },
            preflight: async () => {},
            submit: async (item, _signal, beforePost) => {
                beforePost();
                stateAtSubmitCall = savedSnapshots[savedSnapshots.length - 1]?.items.find(
                    entry => entry.itemId === item.itemId,
                )?.state;
                submittedShots.push(item.shotId);
                return makeRunnerTask(item.shotId, "queued");
            },
            query: async taskId => {
                const status = taskStatuses.get(taskId) ?? "succeeded";
                return makeRunnerTask("shot-1", status, { id: taskId });
            },
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:01:00Z",
            onChange: () => {},
        };

        const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), deps);
        const controller = new AbortController();
        const runPromise = runner.start(controller.signal);

        await flushMicrotasks();
        expect(submittedShots).toEqual(["shot-1"]);
        expect(stateAtSubmitCall).toBe("submitting");
        expect(runner.snapshot().items[0]?.state).toBe("submitted");
        expect(runner.snapshot().items[0]?.taskId).toBe("task-shot-1");
        expect(runner.snapshot().items[1]?.state).toBe("pending");
        expect(sleeper.queue.length).toBe(1);
        expect(sleeper.queue[0]?.ms).toBe(2000);

        // Double start while running does not launch a second loop
        const duplicatePromise = runner.start(controller.signal);
        await flushMicrotasks();
        expect(submittedShots).toEqual(["shot-1"]);

        // Complete shot-1 on next poll
        taskStatuses.set("task-shot-1", "succeeded");
        sleeper.wakeNext();

        await runPromise;
        await duplicatePromise;

        expect(submittedShots).toEqual(["shot-1", "shot-2"]);
        expect(runner.snapshot().state).toBe("finished");
        expect(runner.snapshot().items.map(item => item.state)).toEqual(["succeeded", "succeeded"]);
    });

    test("stop during real generation service media preparation prevents its createTask callback", async () => {
        const { submitBackendGenerationTask } = await import("../src/services/api/generation-task");
        const { makeImageConfig } = await import("./helpers/workflow-batch-fixture");
        const originalFetch = globalThis.fetch;
        const media = createDeferred<Response>();
        let fetching = false;
        let posts = 0;
        globalThis.fetch = (() => { fetching = true; return media.promise; }) as typeof fetch;
        const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1"]), {
            save: async () => {}, preflight: async () => {},
            submit: (item, signal, beforePost) => submitBackendGenerationTask({
                mode: "image", prompt: "reference image", config: makeImageConfig(), signal,
                referenceImages: [{ id: "local-reference", name: "reference.png", type: "image/png", dataUrl: "data:image/png;base64,AA==" }],
            }, {
                createTask: async () => { beforePost(); posts += 1; return makeRunnerTask(item.shotId); },
                waitTask: async () => makeRunnerTask(item.shotId), createId: () => "id",
            }),
            query: async () => makeRunnerTask("shot-1"), hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true, sleep: async () => {}, now: () => "2026-10-29T00:00:00Z", onChange: () => {},
        });
        const { apiClient } = await import("../src/services/api/request");
        const adapter = apiClient.defaults.adapter;
        apiClient.defaults.adapter = async config => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { resource: { id: "uploaded-reference", mimeType: "image/png" } } } });
        try {
            const running = runner.start(new AbortController().signal);
            await flushMicrotasks();
            expect(fetching).toBe(true);
            await runner.stopRemaining();
            media.resolve(new Response(new Blob(["test"], { type: "image/png" })));
            await running;
            expect(posts).toBe(0);
            expect(runner.snapshot().items[0]!.state).toBe("stopped");
        } finally { globalThis.fetch = originalFetch; apiClient.defaults.adapter = adapter; }
    });

    test("handles partial success (s1 succeeded, s2 failed, s3 succeeded) and syncing timeout", async () => {
        const sleeper = createFakeSleeper();
        const deps: BatchRunnerDependencies = {
            save: async () => {},
            preflight: async () => {},
            submit: async (item, _signal, beforePost) => { beforePost(); return makeRunnerTask(item.shotId, "queued"); },
            query: async taskId =>
                makeRunnerTask("shot-1", taskId === "task-shot-2" ? "failed" : "succeeded", {
                    id: taskId,
                    errorMessage: taskId === "task-shot-2" ? "上游生成失败" : undefined,
                }),
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:02:00Z",
            onChange: () => {},
        };

        const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2", "shot-3"]), deps);
        await runner.start(new AbortController().signal);

        expect(runner.snapshot().state).toBe("finished");
        expect(runner.snapshot().items.map(item => item.state)).toEqual([
            "succeeded",
            "failed",
            "succeeded",
        ]);

        // Now test syncing timeout (30s = 15 x 2000ms) when task is succeeded but artifact never becomes ready
        const syncSleeper = createFakeSleeper();
        const syncRunner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
            ...deps,
            query: async taskId => makeRunnerTask("shot-1", "succeeded", { id: taskId }),
            hasReadyArtifact: async () => false,
            sleep: syncSleeper.sleep,
        });

        const syncPromise = syncRunner.start(new AbortController().signal);
        await flushMicrotasks();
        expect(syncRunner.snapshot().items[0]?.state).toBe("syncing");

        for (let i = 0; i < 15; i += 1) {
            await flushMicrotasks();
            expect(syncSleeper.wakeNext()).toBe(2000);
        }
        await syncPromise;

        expect(syncRunner.snapshot().state).toBe("paused");
        expect(syncRunner.snapshot().items[0]?.state).toBe("syncing");
        expect(syncRunner.snapshot().items[0]?.taskId).toBe("task-shot-1");
        expect(syncRunner.snapshot().items[1]?.state).toBe("pending");
    });

    test("backs off on capacity limit (2/4/8/16/30s) and rate_limited (respecting retryAfterMs=60000), blocks on other 4xx, and never replays unknown", async () => {
        expect(
            submissionRetryDelay(
                new ApiError("容量已满", {
                    status: 400,
                    code: 400,
                    reason: TASK_SUBMISSION_REASONS.activeLimitExceeded,
                    retryable: true,
                }),
                0,
            ),
        ).toBe(2000);
        expect(
            submissionRetryDelay(
                new ApiError("容量已满", {
                    status: 400,
                    code: 400,
                    reason: TASK_SUBMISSION_REASONS.activeLimitExceeded,
                    retryable: true,
                }),
                4,
            ),
        ).toBe(30000);
        expect(
            submissionRetryDelay(
                new ApiError("容量已满", {
                    status: 400,
                    code: 400,
                    reason: TASK_SUBMISSION_REASONS.activeLimitExceeded,
                    retryable: true,
                }),
                5,
            ),
        ).toBeNull();

        // Retry-After 60s must not be shortened to 30s
        expect(
            submissionRetryDelay(
                new ApiError("请求过于频繁", {
                    status: 429,
                    code: 42901,
                    reason: TASK_SUBMISSION_REASONS.rateLimited,
                    retryAfterMs: 60000,
                    retryable: true,
                }),
                0,
            ),
        ).toBe(60000);

        // invalid_argument with different msg must never retry
        expect(
            submissionRetryDelay(
                new ApiError("同时排队或运行的任务最多 2 个", {
                    status: 400,
                    code: 400,
                    reason: "invalid_argument",
                }),
                0,
            ),
        ).toBeNull();

        // Capacity backoff in runner: retries only that item and runs preflight after backoff
        const sleeper = createFakeSleeper();
        let preflightCalls = 0;
        let submitCalls = 0;
        const capacityRunner = createWorkflowBatchRunner(makeRunnerLog(["shot-1"]), {
            save: async () => {},
            preflight: async () => {
                preflightCalls += 1;
            },
            submit: async (item, _signal, beforePost) => {
                beforePost();
                submitCalls += 1;
                if (submitCalls <= 2) {
                    throw new ApiError("容量已满", {
                        status: 400,
                        code: 400,
                        reason: TASK_SUBMISSION_REASONS.activeLimitExceeded,
                        retryable: true,
                    });
                }
                return makeRunnerTask(item.shotId, "succeeded");
            },
            query: async taskId => makeRunnerTask("shot-1", "succeeded", { id: taskId }),
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:03:00Z",
            onChange: () => {},
        });

        const capPromise = capacityRunner.start(new AbortController().signal);
        await flushMicrotasks();
        expect(capacityRunner.snapshot().state).toBe("waiting");
        expect(capacityRunner.snapshot().items[0]?.state).toBe("pending");
        expect(sleeper.wakeNext()).toBe(2000);

        await flushMicrotasks();
        expect(sleeper.wakeNext()).toBe(4000);

        await capPromise;
        expect(submitCalls).toBe(3);
        expect(preflightCalls).toBe(3);
        expect(capacityRunner.snapshot().state).toBe("finished");

        // Unknown error (500 with retryable=true) is NEVER replayed
        let unknownSubmits = 0;
        const unknownRunner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
            save: async () => {},
            preflight: async () => {},
            submit: async (_item, _signal, beforePost) => {
                beforePost();
                unknownSubmits += 1;
                throw new ApiError("Gateway Timeout", {
                    status: 504,
                    code: 504,
                    retryable: true,
                });
            },
            query: async taskId => makeRunnerTask("shot-1", "succeeded", { id: taskId }),
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:04:00Z",
            onChange: () => {},
        });
        await unknownRunner.start(new AbortController().signal);
        expect(unknownSubmits).toBe(1);
        expect(unknownRunner.snapshot().state).toBe("paused");
        expect(unknownRunner.snapshot().items[0]?.state).toBe("unknown");
        expect(unknownRunner.snapshot().items[0]?.issueCode).toBe("submit_unknown");
        expect(unknownRunner.snapshot().items[1]?.state).toBe("pending");

        // Even if start() is called again while an unknown item exists, it must NOT submit anything
        await unknownRunner.start(new AbortController().signal);
        expect(unknownSubmits).toBe(1);
    });

    test("handles query failure backoff (3 retries then paused) and storage failure after submit", async () => {
        const sleeper = createFakeSleeper();
        let queryCalls = 0;
        const queryFailRunner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
            save: async () => {},
            preflight: async () => {},
            submit: async (item, _signal, beforePost) => { beforePost(); return makeRunnerTask(item.shotId, "queued"); },
            query: async () => {
                queryCalls += 1;
                throw new ApiError("Bad Gateway", { status: 502, retryable: true });
            },
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:05:00Z",
            onChange: () => {},
        });

        const qPromise = queryFailRunner.start(new AbortController().signal);
        for (let i = 0; i < 3; i += 1) {
            await flushMicrotasks();
            expect(sleeper.wakeNext()).toBeDefined();
        }
        await qPromise;
        expect(queryCalls).toBe(4);
        expect(queryFailRunner.snapshot().state).toBe("paused");
        expect(queryFailRunner.snapshot().items[0]?.taskId).toBe("task-shot-1");
        expect(queryFailRunner.snapshot().items[0]?.issueCode).toBe("task_read_failed");
        expect(queryFailRunner.snapshot().items[1]?.state).toBe("pending");

        // Storage failure after submit keeps taskId in memory and stops further submissions
        let saveCalls = 0;
        let submitCount = 0;
        const storageFailRunner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
            save: async () => {
                saveCalls += 1;
                if (saveCalls === 2) {
                    throw new Error("批次记录未可靠保存，已停止新增提交");
                }
            },
            preflight: async () => {},
            submit: async (item, _signal, beforePost) => {
                beforePost();
                submitCount += 1;
                return makeRunnerTask(item.shotId, "succeeded");
            },
            query: async taskId => makeRunnerTask("shot-1", "succeeded", { id: taskId }),
            hasReadyArtifact: async () => true,
            isOwnerCurrent: () => true,
            sleep: sleeper.sleep,
            now: () => "2026-10-29T00:06:00Z",
            onChange: () => {},
        });

        await storageFailRunner.start(new AbortController().signal);
        expect(submitCount).toBe(1);
        expect(storageFailRunner.snapshot().state).toBe("paused");
        expect(storageFailRunner.snapshot().items[0]?.state).toBe("submitted");
        expect(storageFailRunner.snapshot().items[0]?.taskId).toBe("task-shot-1");
        expect(storageFailRunner.snapshot().items[0]?.issueCode).toBe("storage_failed");
        expect(storageFailRunner.snapshot().items[1]?.state).toBe("pending");
    });

    test("supports stopRemaining across all 5 timing windows: preflight, save, submit, poll, and sleep", async () => {
        // 1. Stop during preflight
        {
            const preflightGate = createDeferred<void>();
            let submitCalled = false;
            const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
                save: async () => {},
                preflight: async () => preflightGate.promise,
                submit: async (_item, _signal, beforePost) => {
                beforePost();
                    submitCalled = true;
                    return makeRunnerTask("shot-1");
                },
                query: async () => makeRunnerTask("shot-1"),
                hasReadyArtifact: async () => true,
                isOwnerCurrent: () => true,
                sleep: async () => {},
                now: () => "2026-10-29T00:07:00Z",
                onChange: () => {},
            });
            const p = runner.start(new AbortController().signal);
            await flushMicrotasks();
            await runner.stopRemaining();
            preflightGate.resolve();
            await p;
            expect(submitCalled).toBe(false);
            expect(runner.snapshot().state).toBe("stopped");
            expect(runner.snapshot().items.map(i => i.state)).toEqual(["stopped", "stopped"]);
        }

        // 2. Stop during save (before submit)
        {
            const saveGate = createDeferred<void>();
            let saveCount = 0;
            let submitCalled = false;
            const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
                save: async () => {
                    saveCount += 1;
                    if (saveCount === 1) {
                        await saveGate.promise;
                    }
                },
                preflight: async () => {},
                submit: async (_item, _signal, beforePost) => {
                beforePost();
                    submitCalled = true;
                    return makeRunnerTask("shot-1");
                },
                query: async () => makeRunnerTask("shot-1"),
                hasReadyArtifact: async () => true,
                isOwnerCurrent: () => true,
                sleep: async () => {},
                now: () => "2026-10-29T00:07:00Z",
                onChange: () => {},
            });
            const p = runner.start(new AbortController().signal);
            await flushMicrotasks();
            const stopP = runner.stopRemaining();
            saveGate.resolve();
            await Promise.all([p, stopP]);
            expect(submitCalled).toBe(false);
            expect(runner.snapshot().state).toBe("stopped");
            expect(runner.snapshot().items.map(i => i.state)).toEqual(["stopped", "stopped"]);
        }

        // 3. Stop during submit (waits for in-flight submit receipt, observes task, marks remaining stopped)
        {
            const submitGate = createDeferred<BatchItem>();
            const submitted: string[] = [];
            const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
                save: async () => {},
                preflight: async () => {},
                submit: async (item, _signal, beforePost) => {
                beforePost();
                    submitted.push(item.shotId);
                    await submitGate.promise;
                    return makeRunnerTask(item.shotId, "succeeded");
                },
                query: async taskId => makeRunnerTask("shot-1", "succeeded", { id: taskId }),
                hasReadyArtifact: async () => true,
                isOwnerCurrent: () => true,
                sleep: async () => {},
                now: () => "2026-10-29T00:07:00Z",
                onChange: () => {},
            });
            const p = runner.start(new AbortController().signal);
            await flushMicrotasks();
            await runner.stopRemaining();
            expect(runner.snapshot().items[1]?.state).toBe("stopped");
            submitGate.resolve(runner.snapshot().items[0]!);
            await p;
            expect(submitted).toEqual(["shot-1"]);
            expect(runner.snapshot().state).toBe("stopped");
            expect(runner.snapshot().items[0]?.state).toBe("succeeded");
            expect(runner.snapshot().items[0]?.taskId).toBe("task-shot-1");
            expect(runner.snapshot().items[1]?.state).toBe("stopped");
        }

        // 4. Stop during poll (continues observing running s1 until terminal, does not submit s2)
        {
            const sleeper = createFakeSleeper();
            let s1Status: "running" | "succeeded" = "running";
            const submitted: string[] = [];
            const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
                save: async () => {},
                preflight: async () => {},
                submit: async (item, _signal, beforePost) => {
                beforePost();
                    submitted.push(item.shotId);
                    return makeRunnerTask(item.shotId, "queued");
                },
                query: async taskId => makeRunnerTask("shot-1", s1Status, { id: taskId }),
                hasReadyArtifact: async () => true,
                isOwnerCurrent: () => true,
                sleep: sleeper.sleep,
                now: () => "2026-10-29T00:07:00Z",
                onChange: () => {},
            });
            const p = runner.start(new AbortController().signal);
            await flushMicrotasks();
            expect(sleeper.queue.length).toBe(1);
            await runner.stopRemaining();
            expect(runner.snapshot().items[1]?.state).toBe("stopped");
            s1Status = "succeeded";
            sleeper.wakeNext();
            await p;
            expect(submitted).toEqual(["shot-1"]);
            expect(runner.snapshot().state).toBe("stopped");
            expect(runner.snapshot().items.map(i => i.state)).toEqual(["succeeded", "stopped"]);
        }

        // 5. Stop during capacity backoff sleep (wakes immediately and marks item stopped)
        {
            const sleeper = createFakeSleeper();
            let submitCount = 0;
            const runner = createWorkflowBatchRunner(makeRunnerLog(["shot-1", "shot-2"]), {
                save: async () => {},
                preflight: async () => {},
                submit: async (_item, _signal, beforePost) => {
                beforePost();
                    submitCount += 1;
                    throw new ApiError("容量已满", {
                        status: 400,
                        code: 400,
                        reason: TASK_SUBMISSION_REASONS.activeLimitExceeded,
                        retryable: true,
                    });
                },
                query: async () => makeRunnerTask("shot-1"),
                hasReadyArtifact: async () => true,
                isOwnerCurrent: () => true,
                sleep: sleeper.sleep,
                now: () => "2026-10-29T00:07:00Z",
                onChange: () => {},
            });
            const p = runner.start(new AbortController().signal);
            await flushMicrotasks();
            expect(runner.snapshot().state).toBe("waiting");
            await runner.stopRemaining();
            await p;
            expect(submitCount).toBe(1);
            expect(runner.snapshot().state).toBe("stopped");
            expect(runner.snapshot().items.map(i => i.state)).toEqual(["stopped", "stopped"]);
        }
    });
});
