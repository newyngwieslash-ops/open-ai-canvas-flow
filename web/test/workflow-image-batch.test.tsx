import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { AxiosError, type InternalAxiosRequestConfig } from "axios";
import { Window } from "happy-dom";
import type { ProjectDetail, ProjectShot, ShotArtifact } from "../src/services/api/projects";
import type { GenerationTask } from "../src/services/api/task-center";
import {
    makeBatchArtifact,
    makeBatchTask,
    makeImageConfig,
    makeWorkflowBatchDetail,
} from "./helpers/workflow-batch-fixture";

function makeReadyArtifact(
    shotId: string,
    revisionId: string,
    type: "storyboard" | "action_board",
    taskId: string,
): ShotArtifact {
    return {
        ...makeBatchArtifact(shotId, "ready", type),
        revisionId,
        taskId,
    };
}

const dom = new Window({ url: "http://localhost/projects/p1/workflow/u1/storyboard" });
const originals = new Map<string, PropertyDescriptor | undefined>();

let lockHeldByOtherTab = false;
let lockRequestCount = 0;
const heldChapterLocks = new Set<string>();
const locksMock = {
    request: async (
        _name: string,
        optionsOrCallback: { ifAvailable?: boolean } | ((lock: object | null) => Promise<unknown>),
        maybeCallback?: (lock: object | null) => Promise<unknown>,
    ) => {
        const callback = typeof optionsOrCallback === "function" ? optionsOrCallback : maybeCallback!;
        if (_name.startsWith("yingce:chapter-image-batch:")) {
            lockRequestCount += 1;
            if (lockHeldByOtherTab || heldChapterLocks.has(_name)) return callback(null);
            heldChapterLocks.add(_name);
            try { return await callback({ name: _name }); }
            finally { heldChapterLocks.delete(_name); }
        }
        return callback({ name: _name });
    },
};

Object.defineProperty(dom.navigator, "locks", {
    configurable: true,
    writable: true,
    value: locksMock,
});

for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    localStorage: dom.localStorage,
    sessionStorage: dom.sessionStorage,
    ResizeObserver: dom.ResizeObserver,
    MutationObserver: dom.MutationObserver,
    HTMLElement: dom.HTMLElement,
    HTMLInputElement: dom.HTMLInputElement,
    HTMLTextAreaElement: dom.HTMLTextAreaElement,
    HTMLButtonElement: dom.HTMLButtonElement,
    Element: dom.Element,
    Node: dom.Node,
    SVGElement: dom.SVGElement,
    ShadowRoot: dom.ShadowRoot,
    Event: dom.Event,
    CustomEvent: dom.CustomEvent,
    MouseEvent: dom.MouseEvent,
    KeyboardEvent: dom.KeyboardEvent,
    getComputedStyle: dom.getComputedStyle.bind(dom),
    requestAnimationFrame: dom.requestAnimationFrame.bind(dom),
    cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom),
    IS_REACT_ACT_ENVIRONMENT: true,
})) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}

const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { MemoryRouter } = await import("react-router");
const { App, ConfigProvider } = await import("antd");
const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
const { apiClient } = await import("../src/services/api/request");
const { setActiveUserScope } = await import("../src/lib/user-scope");
const { useUserStore } = await import("../src/stores/use-user-store");
const { createModelChannel, encodeChannelModel, useConfigStore } = await import(
    "../src/stores/use-config-store"
);
const { createBatchStorage } = await import(
    "../src/pages/projects/detail/workflow-batch-storage"
);
const {
    configureWorkflowBatchRuntimeForTest,
    useWorkflowImageBatch,
} = await import("../src/pages/projects/detail/use-workflow-image-batch");
const { buildBatchPreview } = await import("../src/pages/projects/detail/workflow-batch-plan");
const ProjectWorkflowView = (await import("../src/pages/projects/detail/workflow")).default;
const ProjectChaptersView = (await import("../src/pages/projects/detail/chapters")).default;
const WorkflowProductionWorkbench = (
    await import("../src/pages/projects/detail/workflow-production-workbench")
).default;

const originalAdapter = apiClient.defaults.adapter;
const originalUserState = useUserStore.getState();
const originalConfigState = useConfigStore.getState();

const settle = async (ms = 40) => {
    const ticks = Math.max(6, Math.ceil(ms / 8));
    for (let i = 0; i < ticks; i += 1) {
        await new Promise(resolve => setTimeout(resolve, 5));
    }
};

function makeTestConfigWithImageAndVideo() {
    const imageCfg = makeImageConfig();
    imageCfg.channels[0]!.scope = "system";
    imageCfg.channels[0]!.modelCosts![0]!.logicalModelId = "logical-img-1";
    imageCfg.channels[0]!.modelCosts![0]!.pricePolicy = "channel";
    const videoKey = "MiniMax-H3";
    const videoChannel = createModelChannel({
        id: "test-video",
        name: "测试视频渠道",
        baseUrl: "https://api.minimaxi.com",
        apiKey: "test-video-key",
        interfaceType: "minimax-video",
        models: [videoKey],
        modelCosts: [
            {
                model: videoKey,
                capability: "video",
                protocol: "minimax-video",
                billingMode: "fixed_request",
                unitPriceMicrocredits: 500_000,
            },
        ],
    });
    const videoModel = encodeChannelModel(videoChannel.id, videoKey);
    return {
        ...imageCfg,
        channels: [...imageCfg.channels, videoChannel],
        videoModel,
    };
}

type RecordedRequest = {
    method: string;
    pathname: string;
    params?: Record<string, unknown>;
    body?: Record<string, unknown>;
};

const BATCH_STORAGE_KEY = "yingce:chapter-image-batch:v1:p1:u1";
let detailState: ProjectDetail;
let tasksById: Map<string, GenerationTask>;
let recordedRequests: RecordedRequest[];
let memoryStoresByScope: Map<string, Map<string, string>>;
let storageFaultMode: "none" | "silent_skip" | "throw_before_submit" | "throw_after_first_submit" = "none";
let storageWriteCount = 0;
let sleepDelays: number[];
let customPostTaskHandler:
    | ((body: Record<string, unknown>, config: InternalAxiosRequestConfig) => Promise<GenerationTask> | GenerationTask)
    | null = null;
let customGetTaskHandler:
    | ((taskId: string, config: InternalAxiosRequestConfig) => Promise<GenerationTask> | GenerationTask)
    | null = null;
let customRetryTaskHandler:
    | ((taskId: string, config: InternalAxiosRequestConfig) => Promise<GenerationTask> | GenerationTask)
    | null = null;
let customQuoteHandler:
    | ((body: Record<string, unknown>, config: InternalAxiosRequestConfig) => Promise<unknown> | unknown)
    | null = null;
let activeTasksTruncated = false;
let customSkillFileHandler: (() => Promise<void>) | null = null;
let customWorkspaceHandler: (() => Promise<void>) | null = null;
let customStorageWriteHandler: ((log: any) => Promise<void>) | null = null;
let customBindingResolveHandler: (() => Promise<void>) | null = null;
let omittedTaskIds = new Set<string>();

function getScopeStore(scope: string): Map<string, string> {
    let map = memoryStoresByScope.get(scope);
    if (!map) {
        map = new Map<string, string>();
        memoryStoresByScope.set(scope, map);
    }
    return map;
}

function readStoredBatchLog(scope = "user-a"): any {
    const raw = getScopeStore(scope).get(BATCH_STORAGE_KEY);
    return raw ? JSON.parse(raw) : null;
}

function writeStoredBatchLog(log: Record<string, unknown>, scope = "user-a"): void {
    getScopeStore(scope).set(BATCH_STORAGE_KEY, JSON.stringify(log));
}

function makeAxiosReject(
    config: InternalAxiosRequestConfig,
    status: number,
    code: number,
    msg: string,
    reason?: string,
): AxiosError {
    return new AxiosError(
        msg,
        String(status),
        config,
        {},
        {
            config,
            status,
            statusText: String(status),
            headers: {},
            data: { code, msg, ...(reason ? { reason } : {}) },
        },
    );
}

function parseRequestUrl(rawUrl = ""): { pathname: string; searchParams: URLSearchParams } {
    const parsed = new URL(rawUrl.startsWith("http") ? rawUrl : `http://localhost${rawUrl.startsWith("/") ? "" : "/"}${rawUrl}`);
    const pathname = parsed.pathname.replace(/^\/api(?=\/|$)/, "") || "/";
    return { pathname, searchParams: parsed.searchParams };
}

function parseBody(data: unknown): Record<string, unknown> | undefined {
    if (!data) return undefined;
    if (typeof data === "string") {
        try {
            return JSON.parse(data) as Record<string, unknown>;
        } catch {
            return undefined;
        }
    }
    if (typeof data === "object") {
        return data as Record<string, unknown>;
    }
    return undefined;
}

async function respondToBatchRequest(config: InternalAxiosRequestConfig): Promise<unknown> {
    const method = (config.method || "get").toUpperCase();
    const { pathname } = parseRequestUrl(config.url || "");
    const body = parseBody(config.data);
    const params = (config.params || {}) as Record<string, unknown>;

    recordedRequests.push({ method, pathname, params, body });

    const key = `${method} ${pathname}`;
    switch (key) {
        case "GET /projects/p1/core":
            return { project: detailState.project };

        case "GET /projects/p1/units/u1/workspace":
        case "GET /projects/p1/units/u2/workspace": {
            await customWorkspaceHandler?.();
            const unitId = pathname.includes("/u2/") ? "u2" : "u1";
            const unit = detailState.units.find(u => u.id === unitId) || detailState.units[0]!;
            return {
                unit,
                workflows: detailState.workflows || [],
                assets: detailState.assets || [],
                shots: (detailState.shots || []).filter(s => s.unitId === unitId),
                shotRevisions: detailState.shotRevisions || [],
                shotArtifacts: detailState.shotArtifacts || [],
                shotReferences: detailState.shotReferences || [],
                assetCandidates: detailState.assetCandidates || [],
                tasks: [...tasksById.values()].filter(t => !omittedTaskIds.has(t.id)),
            };
        }

        case "POST /projects/p1/workflows": {
            const unitId = String(body?.unitId || "u1");
            const workflow = {
                instance: {
                    id: `wf-${unitId}`,
                    projectId: "p1",
                    unitId,
                    templateKey: "short_drama",
                    status: "running",
                    currentStepKey: "storyboard",
                    createdAt: "2026-10-29T00:00:00.000Z",
                    updatedAt: "2026-10-29T00:00:00.000Z",
                },
                steps: [
                    {
                        id: `step-storyboard-${unitId}`,
                        instanceId: `wf-${unitId}`,
                        stepKey: "storyboard",
                        status: "running",
                        position: 2,
                    },
                    {
                        id: `step-previz-${unitId}`,
                        instanceId: `wf-${unitId}`,
                        stepKey: "previz",
                        status: "pending",
                        position: 3,
                    },
                    {
                        id: `step-video-${unitId}`,
                        instanceId: `wf-${unitId}`,
                        stepKey: "video",
                        status: "pending",
                        position: 4,
                    },
                ],
            };
            detailState = {
                ...detailState,
                workflows: [...(detailState.workflows || []), workflow as any],
            };
            return { workflow };
        }

        case "POST /projects/p1/units/u1/shot-asset-bindings/resolve-generated":
            await customBindingResolveHandler?.();
            return { taskId: body?.taskId, baseline: { projectId: "p1", unitId: "u1", projectRevision: 1, taskId: body?.taskId, taskResultFingerprint: "result-hash", catalog: [] }, rows: [{ rowIndex: 0, candidates: [], assetVersionIds: [], issues: [] }] };
        case "PUT /projects/p1/units/u1/shots":
            return { shots: detailState.shots };
        case "PUT /projects/p1/shots/s1":
        case "POST /projects/p1/shots":
            return { shot: detailState.shots[0] };

        case "GET /projects/p1/assets":
            return {
                assets: detailState.assets || [],
                total: (detailState.assets || []).length,
                page: 1,
                pageSize: 30,
                categoryCounts: {},
            };

        case "GET /skills":
        case "GET /skills/added":
            return { skills: [] };
        case "GET /skills/test-skill/files":
            return { files: [] };
        case "GET /skills/test-skill/file":
            await customSkillFileHandler?.();
            return { file: { binary: false, content: "按镜头生成", file: { path: "SKILL.md", sha256: "hash" } } };

        case "POST /logical-models/quote":
        case "POST /models/logical-img-1/quote":
        case "POST /model-catalog/quote": {
            if (customQuoteHandler) {
                return await customQuoteHandler(body || {}, config);
            }
            return {
                quote: {
                    logicalModel: String(body?.model || "doubao-seedream-4-0-250828"),
                    capability: "image",
                    amountMicrocredits: 200_000,
                    billingMode: "fixed_request",
                    estimated: false,
                },
            };
        }

        case "GET /tasks": {
            if (activeTasksTruncated && params.activeOnly) {
                return Array.from({ length: 100 }, (_, idx) =>
                    makeBatchTask(`s-trunc-${idx}`, "running", "storyboard"),
                );
            }
            const all = [...tasksById.values()].filter(t => !omittedTaskIds.has(t.id));
            const filtered = params.activeOnly
                ? all.filter(t => t.status === "queued" || t.status === "running")
                : all;
            return filtered;
        }

        case "POST /tasks": {
            if (customPostTaskHandler) {
                const task = await customPostTaskHandler(body || {}, config);
                return task;
            }
            const inputObj = (body?.input || {}) as Record<string, unknown>;
            const meta = (inputObj.metadata || {}) as Record<string, unknown>;
            const shotId = String(meta.shotId || "s1");
            const artifactType = (meta.artifactType as "storyboard" | "action_board") || "storyboard";
            const id = `task-${shotId}-${tasksById.size + 1}`;
            const created: GenerationTask = {
                ...makeBatchTask(shotId, "succeeded", artifactType),
                id,
                projectId: "p1",
                prompt: String(body?.prompt || ""),
                model: String(body?.model || "doubao-seedream-4-0-250828"),
                inputJson: JSON.stringify({ input: inputObj, metadata: meta }),
            };
            tasksById.set(id, created);
            const art = makeReadyArtifact(shotId, String(meta.shotRevisionId || "r1"), artifactType, id);
            detailState = {
                ...detailState,
                shotArtifacts: [...(detailState.shotArtifacts || []), art],
                tasks: [...tasksById.values()],
            };
            return created;
        }

        default: {
            const getTaskMatch = pathname.match(/^\/tasks\/([^/]+)$/);
            if (method === "GET" && getTaskMatch) {
                const taskId = decodeURIComponent(getTaskMatch[1]!);
                if (customGetTaskHandler) {
                    const task = await customGetTaskHandler(taskId, config);
                    return task;
                }
                const task = tasksById.get(taskId);
                if (!task) {
                    throw makeAxiosReject(config, 404, 404, "task not found", "not_found");
                }
                return task;
            }

            const retryMatch = pathname.match(/^\/tasks\/([^/]+)\/retry$/);
            if (method === "POST" && retryMatch) {
                const taskId = decodeURIComponent(retryMatch[1]!);
                if (customRetryTaskHandler) {
                    const task = await customRetryTaskHandler(taskId, config);
                    return task;
                }
                const existing = tasksById.get(taskId);
                if (!existing) {
                    throw makeAxiosReject(config, 404, 404, "task not found", "not_found");
                }
                const retried: GenerationTask = {
                    ...existing,
                    status: "succeeded",
                    attempts: (existing.attempts || 1) + 1,
                    updatedAt: "2026-10-29T00:05:00.000Z",
                };
                tasksById.set(taskId, retried);
                const shotId = existing.clientContext?.shotId || "s1";
                const artifactType =
                    (existing.clientContext?.artifactType as "storyboard" | "action_board") ||
                    "storyboard";
                const shot = (detailState.shots || []).find(s => s.id === shotId);
                const art = makeReadyArtifact(
                    shotId,
                    shot?.currentRevisionId || "r1",
                    artifactType,
                    taskId,
                );
                detailState = {
                    ...detailState,
                    shotArtifacts: [...(detailState.shotArtifacts || []), art],
                    tasks: [...tasksById.values()],
                };
                return retried;
            }

            throw new Error(`Unexpected API request in batch test: ${method} ${pathname}`);
        }
    }
}

beforeEach(() => {
    document.body.innerHTML = "";
    lockHeldByOtherTab = false;
    lockRequestCount = 0;
    heldChapterLocks.clear();
    detailState = makeWorkflowBatchDetail();
    tasksById = new Map<string, GenerationTask>();
    recordedRequests = [];
    memoryStoresByScope = new Map();
    storageFaultMode = "none";
    storageWriteCount = 0;
    sleepDelays = [];
    customPostTaskHandler = null;
    customGetTaskHandler = null;
    customRetryTaskHandler = null;
    customQuoteHandler = null;
    activeTasksTruncated = false;
    customSkillFileHandler = null;
    customWorkspaceHandler = null;
    customStorageWriteHandler = null;
    customBindingResolveHandler = null;
    omittedTaskIds = new Set();

    setActiveUserScope("user-a");
    useUserStore.setState({
        hydrated: true,
        user: {
            id: "user-a",
            username: "user-a",
            email: "user-a@example.com",
            role: "user",
            status: "active",
            balanceMicrocredits: 100_000_000,
        } as any,
        features: { creditsEnabled: true, auditLogsEnabled: false, customChannelsEnabled: true } as any,
        runtimeLimits: { activeTaskLimit: 5 } as any,
    });

    const testCfg = makeTestConfigWithImageAndVideo();
    useConfigStore.setState({
        config: testCfg,
    });

    configureWorkflowBatchRuntimeForTest({
        now: () => "2026-10-29T00:01:00.000Z",
        sleep: async (ms, signal) => {
            sleepDelays.push(ms);
            if (signal.aborted) {
                throw new DOMException("Aborted", "AbortError");
            }
            await new Promise(resolve => setTimeout(resolve, 2));
            if (signal.aborted) {
                throw new DOMException("Aborted", "AbortError");
            }
        },
        createStorage: identity => {
            const store = getScopeStore(identity.ownerScope);
            return createBatchStorage(identity, {
                getItem: async (k: string) => {
                    return store.get(k) ?? null;
                },
                setItem: async (k: string, v: string) => {
                    storageWriteCount += 1;
                    await customStorageWriteHandler?.(JSON.parse(v));
                    if (storageFaultMode === "throw_before_submit") {
                        throw new Error("QuotaExceededError");
                    }
                    if (storageFaultMode === "throw_after_first_submit" && storageWriteCount >= 3) {
                        throw new Error("Disk write failed after submit");
                    }
                    if (storageFaultMode === "silent_skip") {
                        return;
                    }
                    store.set(k, String(v));
                },
                removeItem: async (k: string) => {
                    store.delete(k);
                },
            });
        },
    });

    apiClient.defaults.adapter = async config => {
        const fixture = await respondToBatchRequest(config);
        return {
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: { code: 0, data: fixture, msg: "ok" },
        };
    };
});

afterAll(() => {
    configureWorkflowBatchRuntimeForTest({});
    apiClient.defaults.adapter = originalAdapter;
    useUserStore.setState(originalUserState);
    useConfigStore.setState(originalConfigState);
    setActiveUserScope("guest");
    dom.happyDOM.abort();
    for (const [key, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

function findButton(labelSubstring: string, root: ParentNode = document): HTMLButtonElement | undefined {
    const normalized = labelSubstring.replace(/\s+/g, "");
    return [...root.querySelectorAll<HTMLButtonElement>("button")].find(btn =>
        btn.textContent?.replace(/\s+/g, "").includes(normalized),
    );
}

async function mountWorkbench(options: {
    activeStage?: "storyboard" | "previz" | "video";
    detail?: ProjectDetail;
    selectedShot?: ProjectShot;
    unitId?: string;
    parent?: boolean;
    chapters?: boolean;
} = {}) {
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let currentStage = options.activeStage ?? "storyboard";
    let currentUnitId = options.unitId ?? "u1";
    let currentSelectedShot =
        "selectedShot" in options
            ? options.selectedShot
            : (options.detail ?? detailState).shots.find(s => s.unitId === currentUnitId);

    const updateRoot = (override: Partial<typeof options> = {}) => {
        if (override.activeStage) currentStage = override.activeStage;
        if (override.unitId) currentUnitId = override.unitId;
        const activeDetail = override.detail ?? detailState;
        if ("selectedShot" in override) {
            currentSelectedShot = override.selectedShot;
        } else if (currentSelectedShot) {
            currentSelectedShot = activeDetail.shots.find(s => s.id === currentSelectedShot?.id);
        }
        const wf = (activeDetail.workflows || []).find(w => w.instance.unitId === currentUnitId);
        const step = wf?.steps.find(s => s.stepKey === currentStage);

        root.render(
            <MemoryRouter initialEntries={[`/projects/p1/workflow/${currentUnitId}/${currentStage}`]}>
                <ConfigProvider theme={{ token: { motion: false } }}>
                    <App>
                        <QueryClientProvider client={client}>
                            {options.chapters ? <ProjectChaptersView detail={activeDetail} refreshProject={() => {}} /> : options.parent ? <ProjectWorkflowView detail={activeDetail} projectId="p1" unitId={currentUnitId} stage={currentStage} /> : <WorkflowProductionWorkbench
                                key={`p1:${currentUnitId}`}
                                activeStage={currentStage}
                                detail={activeDetail}
                                projectId="p1"
                                unitId={currentUnitId}
                                workflowStep={step}
                                selectedShot={currentSelectedShot}
                                onSelectShot={nextId => {
                                    currentSelectedShot = detailState.shots.find(s => s.id === nextId);
                                }}
                                onRefresh={async () => {
                                    updateRoot({ detail: detailState });
                                }}
                                onAddShot={() => {}}
                                addingShot={false}
                                onGenerateStoryboard={() => {}}
                                generatingStoryboard={false}
                            />}
                        </QueryClientProvider>
                    </App>
                </ConfigProvider>
            </MemoryRouter>,
        );
    };

    const renderWorkbench = async (override: Partial<typeof options> = {}) => {
        await act(async () => {
            updateRoot(override);
            await settle();
        });
    };

    await renderWorkbench();

    return {
        client,
        rerender: renderWorkbench,
        dispose: async () => {
            await act(async () => root.unmount());
            client.clear();
            host.remove();
        },
    };
}

describe("workflow image batch workbench", () => {
    test("parent replacement entry is disabled for an unsettled chapter batch", async () => {
        const log = await seedPausedBatch();
        writeStoredBatchLog({ ...log, items: log.items.map(i => ({ ...i, state: "unknown" })) });
        sessionStorage.setItem("project-storyboard-task:p1:u1", "prior-text-task");
        const mounted = await mountWorkbench({ parent: true });
        try {
            expect(findButton("放弃上次结果并重新生成")?.disabled).toBe(true);
            expect(findButton("生成章节分镜")?.disabled).toBe(true);
            await act(async () => { findButton("放弃上次结果并重新生成")!.click(); await settle(); });
            expect(recordedRequests.filter(r => r.method !== "GET" && (r.pathname === "/tasks" || r.pathname.includes("/shots")))).toHaveLength(0);
        } finally { sessionStorage.removeItem("project-storyboard-task:p1:u1"); await mounted.dispose(); }
    });

    test("parent rechecks the chapter batch after replacement confirmation awaits", async () => {
        const mounted = await mountWorkbench({ parent: true });
        try {
            await act(async () => { findButton("生成章节分镜")!.click(); await settle(); });
            expect(findButton("生成并替换")).toBeTruthy();
            const log = await seedPausedBatch();
            writeStoredBatchLog({ ...log, items: log.items.map(i => ({ ...i, state: "unknown" })) });
            await act(async () => { findButton("生成并替换")!.click(); await settle(); });
            expect(document.body.textContent).toContain("当前章节正在参与批量任务");
            expect(recordedRequests.filter(r => r.method !== "GET" && (r.pathname === "/tasks" || r.pathname.includes("/shots")))).toHaveLength(0);
        } finally { await mounted.dispose(); }
    });

    test("real parent replacement holds the same chapter lock through asynchronous task recovery", async () => {
        sessionStorage.setItem("project-storyboard-task:p1:u1", "chapter-result");
        let release!: (task: GenerationTask) => void;
        customGetTaskHandler = () => new Promise(resolve => { release = resolve; });
        const parent = await mountWorkbench({ parent: true });
        const otherTab = await mountBatchHook();
        try {
            await act(async () => { await otherTab.controller.open("storyboard"); await settle(); });
            await act(async () => { findButton("生成章节分镜")!.click(); await settle(); });
            expect(release).toBeDefined();
            await act(async () => { await otherTab.controller.confirm(); await settle(); });
            expect(imagePosts()).toHaveLength(0);
            expect(otherTab.controller.runtimeError).toContain("其他标签页");
            expect(recordedRequests.some(r => r.method === "PUT" && r.pathname.endsWith("/shots"))).toBe(false);
            await act(async () => {
                release({ ...makeBatchTask("s1", "succeeded", "storyboard"), id: "chapter-result", type: "canvas_text", resultJson: JSON.stringify({ rows: [{ plotDescription: "新镜头", imageGenerationPrompt: "新画面", durationSeconds: 3 }] }) });
                await settle(100);
            });
            expect(recordedRequests.filter(r => r.method === "PUT" && r.pathname === "/projects/p1/units/u1/shots")).toHaveLength(1);
            expect(recordedRequests.some(r => r.pathname.endsWith("/resolve-generated"))).toBe(true);
            expect(imagePosts()).toHaveLength(0);
        } finally { sessionStorage.removeItem("project-storyboard-task:p1:u1"); await parent.dispose(); await otherTab.dispose(); }
    });

    test("parent must recheck owner after binding resolution and immediately before replacement PUT", async () => {
        sessionStorage.setItem("project-storyboard-task:p1:u1", "late-chapter-result");
        tasksById.set("late-chapter-result", { ...makeBatchTask("s1", "succeeded", "storyboard"), id: "late-chapter-result", type: "canvas_text", resultJson: JSON.stringify({ rows: [{ plotDescription: "新镜头", durationSeconds: 3 }] }) });
        let release!: () => void;
        customBindingResolveHandler = () => new Promise(resolve => { release = resolve; });
        const mounted = await mountWorkbench({ parent: true });
        try {
            await act(async () => { findButton("生成章节分镜")!.click(); await settle(); });
            expect(release).toBeDefined();
            await act(async () => {
                setActiveUserScope("user-b");
                useUserStore.setState({ user: { ...useUserStore.getState().user!, id: "user-b" } });
                await settle(); release(); await settle();
            });
            expect(recordedRequests.filter(r => r.method === "PUT" && r.pathname.endsWith("/shots"))).toHaveLength(0);
        } finally { release?.(); sessionStorage.removeItem("project-storyboard-task:p1:u1"); await mounted.dispose(); }
    });

    test("parent confirmation from a departed chapter cannot borrow the next chapter lock", async () => {
        detailState.units.push({ ...detailState.units[0]!, id: "u2", title: "第二章", position: 1 });
        sessionStorage.setItem("project-storyboard-task:p1:u1", "departed-task");
        tasksById.set("departed-task", { ...makeBatchTask("s1", "failed", "storyboard"), id: "departed-task" });
        const mounted = await mountWorkbench({ parent: true });
        try {
            await act(async () => { findButton("放弃上次结果并重新生成")!.click(); await settle(); });
            expect(findButton("放弃并重新生成")).toBeTruthy();
            await mounted.rerender({ unitId: "u2" });
            await act(async () => { findButton("放弃并重新生成")!.click(); await settle(); });
            expect(recordedRequests.filter(r => r.pathname === "/tasks/departed-task")).toHaveLength(0);
            expect(imagePosts()).toHaveLength(0);
        } finally { sessionStorage.removeItem("project-storyboard-task:p1:u1"); await mounted.dispose(); }
    });

    test("without WebLocks ordinary single-shot save works but an unsettled batch still blocks it", async () => {
        Object.defineProperty(navigator, "locks", { configurable: true, writable: true, value: undefined });
        const mounted = await mountWorkbench();
        try {
            const editTitle = async (value: string) => act(async () => {
                const input = document.querySelector<HTMLInputElement>('input[placeholder="用一句话概括这个镜头"]')!;
                Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
                input.dispatchEvent(new Event("input", { bubbles: true }));
                await settle();
            });
            await editTitle("无锁环境仍能保存单镜");
            expect(findButton("批量生成分镜图")?.disabled).toBe(true);
            await act(async () => { findButton("保存脚本")!.click(); await settle(); });
            expect(recordedRequests.filter(r => r.method !== "GET" && r.pathname.includes("/shots"))).toHaveLength(1);
            await seedPausedBatch();
            await editTitle("存在未结批次不能覆盖");
            await act(async () => { findButton("保存脚本")!.click(); await settle(); });
            expect(recordedRequests.filter(r => r.method !== "GET" && r.pathname.includes("/shots"))).toHaveLength(1);
        } finally {
            await mounted.dispose();
            Object.defineProperty(navigator, "locks", { configurable: true, writable: true, value: locksMock });
        }
    });

    test("chapter storyboard completion status waits for persisted shots", async () => {
        const savedDetail = detailState;
        detailState = { ...detailState, shots: [] };
        tasksById.set("unsaved-chapter-result", {
            ...makeBatchTask("s1", "succeeded", "storyboard"), id: "unsaved-chapter-result", type: "canvas_text",
            clientContext: { domainProjectId: "p1", chapterId: "u1", chapterOperation: "storyboard" },
            resultJson: JSON.stringify({ rows: [{ plotDescription: "Unsaved shot", durationSeconds: 3 }] }),
        });
        customBindingResolveHandler = async () => { throw new Error("Cannot save generated result"); };
        const mounted = await mountWorkbench({ chapters: true });
        try {
            await act(async () => { await settle(100); });
            expect(findButton("生成到分镜制作")?.textContent).not.toContain("已生成");
            expect(findButton("生成到分镜制作")?.textContent).toContain("结果待保存");
            expect(recordedRequests.filter(r => r.method === "PUT" && r.pathname.endsWith("/shots"))).toHaveLength(0);
            await mounted.rerender({ detail: savedDetail });
            expect(findButton("生成到分镜制作")?.textContent).toContain("已生成");
        } finally { await mounted.dispose(); }
    });

    test("ordinary chapters automatic recovery cannot replace shots while the chapter batch is unsettled", async () => {
        await seedPausedBatch();
        tasksById.set("ordinary-chapter-result", {
            ...makeBatchTask("s1", "succeeded", "storyboard"), id: "ordinary-chapter-result", type: "canvas_text", updatedAt: "2026-10-29T00:05:00Z",
            clientContext: { domainProjectId: "p1", chapterId: "u1", chapterOperation: "storyboard" },
            resultJson: JSON.stringify({ rows: [{ plotDescription: "旧任务恢复", durationSeconds: 3 }] }),
        });
        const mounted = await mountWorkbench({ chapters: true });
        try {
            await act(async () => { await settle(100); });
            expect(recordedRequests.some(r => r.pathname === "/tasks/ordinary-chapter-result")).toBe(true);
            expect(recordedRequests.filter(r => r.method === "PUT" && r.pathname.endsWith("/shots"))).toHaveLength(0);
        } finally { await mounted.dispose(); }
    });

    test("unknown batch remains viewable and claimable after closing and editing dirty script", async () => {
        const log = await seedPausedBatch();
        writeStoredBatchLog({ ...log, items: log.items.map(i => ({ ...i, state: "unknown" })) });
        const mounted = await mountWorkbench();
        try {
            await act(async () => { findButton("查看本章批次")!.click(); await settle(); });
            await act(async () => { findButton("取消")!.click(); await settle(); });
            const titleInput = document.querySelector<HTMLInputElement>('input[placeholder="用一句话概括这个镜头"]')!;
            await act(async () => {
                Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(titleInput, "未保存的编辑");
                titleInput.dispatchEvent(new Event("input", { bubbles: true }));
                await settle();
            });
            await act(async () => { findButton("查看本章批次")!.click(); await settle(); });
            expect(document.querySelector('input[aria-label="确切任务 ID"]')).toBeTruthy();
            expect(findButton("核对任务")?.disabled).toBe(false);
            expect(findButton("确认提交 0 个任务")?.disabled).toBe(true);
            const task = { ...makeBatchTask("s1", "failed", "storyboard"), id: "manual-claim", inputJson: JSON.stringify({ metadata: {
                domainProjectId: "p1", unitId: "u1", shotId: "s1", shotRevisionId: "r1", artifactType: "storyboard",
                chapterImageBatchId: log.batchId, chapterImageBatchItemId: log.items[0]!.itemId,
            } }) };
            tasksById.set(task.id, task);
            omittedTaskIds.add(task.id);
            await act(async () => {
                const input = document.querySelector<HTMLInputElement>('input[aria-label="确切任务 ID"]')!;
                Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, task.id);
                input.dispatchEvent(new Event("input", { bubbles: true }));
                await settle();
            });
            await act(async () => { findButton("核对任务")!.click(); await settle(); });
            expect(readStoredBatchLog().items[0]).toMatchObject({ taskId: "manual-claim", state: "failed" });
            expect(findButton("确认提交 0 个任务")?.disabled).toBe(true);
            expect(imagePosts()).toHaveLength(0);
        } finally { await mounted.dispose(); }
    });

    test("workbench: 三按钮顺序相邻、分镜/预演/视频页入口均传固定stage且从视频页打开预演批量为mode=image/action_board无视频模型污染", async () => {
        const mounted = await mountWorkbench({ activeStage: "video" });
        try {
            const group = document.querySelector<HTMLElement>('.workflow-chapter-actions[role="group"][aria-label="本章操作"]');
            expect(group).toBeTruthy();
            const groupButtons = [...(group?.querySelectorAll("button") || [])].map(b =>
                b.textContent?.trim(),
            );
            expect(groupButtons).toEqual(["匹配镜头资产", "批量生成分镜图", "批量生成动作预演", "提交到 Flow"]);

            const previzBtn = findButton("批量生成动作预演", group!);
            expect(previzBtn).toBeTruthy();
            await act(async () => {
                previzBtn!.click();
                await settle();
            });

            expect(document.body.textContent).toContain("批量生成动作预演");
            expect(document.body.textContent).toContain("动作预演为黑白图片，不是视频，不自动使用已生成分镜图。");

            const confirmBtn = findButton("确认提交 2 个任务");
            expect(confirmBtn).toBeTruthy();
            expect(confirmBtn?.disabled).toBe(false);

            await act(async () => {
                confirmBtn!.click();
                await settle(80);
            });

            const postTaskReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postTaskReqs).toHaveLength(2);
            for (const req of postTaskReqs) {
                expect(req.body?.type).toBe("canvas_image");
                expect(String(req.body?.model)).toContain("doubao-seedream-4-0-250828");
                expect(String(req.body?.model)).not.toContain("MiniMax");
                const inputObj = req.body?.input as Record<string, any>;
                expect(inputObj?.metadata?.artifactType).toBe("action_board");
                expect(inputObj?.metadata?.workflowStepId).toBe("step-previz");
                expect(inputObj?.referenceAudios).toEqual([]);
                expect(String(req.body?.prompt)).toContain("黑白分镜草图，清晰动作节拍，电影构图");
            }
        } finally {
            await mounted.dispose();
        }
    });

    test("workbench: 两镜不同文案与资产时POST metadata/参考图/提示词分别正确且零镜头保存请求", async () => {
        detailState.assets = [
            {
                id: "asset-1",
                projectId: "p1",
                category: "character",
                title: "林雾",
                mediaType: "image",
                status: "ready",
                versionCount: 1,
                primaryVersionId: "av-1",
                storageKey: "resource:res-char-1",
                createdAt: "2026-10-29T00:00:00.000Z",
                updatedAt: "2026-10-29T00:00:00.000Z",
            } as any,
            {
                id: "asset-2",
                projectId: "p1",
                category: "environment",
                title: "古宅庭院",
                mediaType: "image",
                status: "ready",
                versionCount: 1,
                primaryVersionId: "av-2",
                storageKey: "resource:res-env-2",
                createdAt: "2026-10-29T00:00:00.000Z",
                updatedAt: "2026-10-29T00:00:00.000Z",
            } as any,
        ];
        detailState.shotReferences = [
            {
                id: "ref-1",
                shotId: "s1",
                assetVersionId: "av-1",
                role: "reference",
                status: "linked",
                createdAt: "2026-10-29T00:00:00.000Z",
            } as any,
            {
                id: "ref-2",
                shotId: "s2",
                assetVersionId: "av-2",
                role: "reference",
                status: "linked",
                createdAt: "2026-10-29T00:00:00.000Z",
            } as any,
        ];
        detailState.shotRevisions[0] = {
            ...detailState.shotRevisions[0]!,
            plotDescription: "主角 @[asset:asset-1] 在雨夜街头特写",
            imagePrompt: "主角 @[asset:asset-1] 在雨夜街头特写",
            videoPrompt: "动作描述 @[asset:asset-1]",
        };
        detailState.shotRevisions[1] = {
            ...detailState.shotRevisions[1]!,
            plotDescription: "女主在 @[asset:asset-2] 回身拔剑",
            imagePrompt: "冷月下的 @[asset:asset-2] 剑影",
            videoPrompt: "动作描述 @[asset:asset-2]",
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            const storyboardBtn = findButton("批量生成分镜图");
            await act(async () => {
                storyboardBtn!.click();
                await settle();
            });

            const confirmBtn = findButton("确认提交 2 个任务");
            expect(confirmBtn?.disabled).toBe(false);

            await act(async () => {
                confirmBtn!.click();
                await settle(80);
            });

            const saveReqs = recordedRequests.filter(r => r.pathname.includes("/shots"));
            expect(saveReqs).toHaveLength(0);

            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(2);

            const firstInput = postReqs[0]!.body?.input as Record<string, any>;
            const secondInput = postReqs[1]!.body?.input as Record<string, any>;

            expect(firstInput.metadata.shotId).toBe("s1");
            expect(firstInput.metadata.shotRevisionId).toBe("r1");
            expect(firstInput.metadata.artifactType).toBe("storyboard");
            expect(firstInput.referenceImages.map((r: any) => r.storageKey)).toEqual(["resource:res-char-1"]);
            expect(String(postReqs[0]!.body?.prompt)).toContain("在雨夜街头特写");
            expect(String(postReqs[0]!.body?.prompt)).toContain("林雾");
            expect(String(postReqs[0]!.body?.prompt)).not.toContain("古宅庭院");

            expect(secondInput.metadata.shotId).toBe("s2");
            expect(secondInput.metadata.shotRevisionId).toBe("r2");
            expect(secondInput.metadata.artifactType).toBe("storyboard");
            expect(secondInput.referenceImages.map((r: any) => r.storageKey)).toEqual(["resource:res-env-2"]);
            expect(String(postReqs[1]!.body?.prompt)).toContain("古宅庭院");
            expect(String(postReqs[1]!.body?.prompt)).not.toContain("雨夜街头特写");
        } finally {
            await mounted.dispose();
        }
    });

    test("workbench: 修改当前脚本未保存时点击入口零预览写/零生成并提示保存后再试", async () => {
        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            const titleInput = document.querySelector<HTMLInputElement>('input[placeholder="用一句话概括这个镜头"]');
            expect(titleInput).toBeTruthy();

            await act(async () => {
                const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
                setter?.call(titleInput, "未保存的新镜头标题");
                titleInput!.dispatchEvent(new Event("input", { bubbles: true }));
                titleInput!.dispatchEvent(new Event("change", { bubbles: true }));
                await settle();
            });

            expect(document.body.textContent).toContain("当前镜头脚本有未保存修改，请先保存当前镜头后再执行批量生成");
            const storyboardBtn = findButton("批量生成分镜图");
            const previzBtn = findButton("批量生成动作预演");
            expect(storyboardBtn?.disabled).toBe(true);
            expect(previzBtn?.disabled).toBe(true);

            const workspaceReqsBefore = recordedRequests.filter(r => r.pathname.includes("/workspace")).length;
            await act(async () => {
                storyboardBtn!.click();
                await settle();
            });
            const workspaceReqsAfter = recordedRequests.filter(r => r.pathname.includes("/workspace")).length;
            expect(workspaceReqsAfter).toBe(workspaceReqsBefore);
            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(0);
        } finally {
            await mounted.dispose();
        }
    });

    test.each([1, 5])("workbench: 账户上限 %s 时双槽时间轴准确显示待提交或并行生成，竞争写操作锁定当前涉事镜头", async activeTaskLimit => {
        useUserStore.setState({ runtimeLimits: { ...useUserStore.getState().runtimeLimits!, activeTaskLimit } });
        let releaseFirstTask: (() => void) | null = null;
        customPostTaskHandler = async body => {
            const inputObj = (body?.input || {}) as Record<string, any>;
            const shotId = String(inputObj.metadata?.shotId || "s1");
            const runningTask = makeBatchTask(shotId, "running", "storyboard");
            tasksById.set(runningTask.id, runningTask);
            return runningTask;
        };
        customGetTaskHandler = async taskId => {
            if (releaseFirstTask) {
                await new Promise<void>(resolve => {
                    releaseFirstTask = resolve;
                });
            }
            const t = tasksById.get(taskId)!;
            return t;
        };
        releaseFirstTask = () => {};

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle();
            });

            expect(document.body.textContent).toContain("当前镜头正在参与本章批量任务");
            const saveBtn = findButton("保存脚本");
            const deleteBtn = findButton("删除镜头");
            const matchAssetsBtn = findButton("匹配镜头资产");
            expect(saveBtn?.disabled).toBe(true);
            expect(deleteBtn?.disabled).toBe(true);
            expect(matchAssetsBtn?.disabled).toBe(true);

            const timelineCards = [...document.querySelectorAll<HTMLElement>(".workflow-timeline-shot")];
            expect(timelineCards).toHaveLength(2);
            const firstCardRows = [...timelineCards[0]!.querySelectorAll<HTMLElement>(".workflow-timeline-media")];
            const secondCardRows = [...timelineCards[1]!.querySelectorAll<HTMLElement>(".workflow-timeline-media")];

            expect(firstCardRows[0]?.textContent).toContain("分镜图");
            expect(firstCardRows[0]?.textContent).toContain("生成中");
            expect(firstCardRows[1]?.textContent).toContain("动作预演");
            expect(firstCardRows[1]?.textContent).toContain("待生成");

            expect(secondCardRows[0]?.textContent).toContain("分镜图");
            expect(secondCardRows[0]?.textContent).toContain(activeTaskLimit === 1 ? "待提交" : "生成中");
            expect(imagePosts()).toHaveLength(activeTaskLimit === 1 ? 1 : 2);
            expect(secondCardRows[1]?.textContent).toContain("动作预演");
            expect(secondCardRows[1]?.textContent).toContain("待生成");
        } finally {
            await mounted.dispose();
        }
    });

    test("workbench: 空章节或全部ready/blocked时显示合理空态、开始禁用且无伪成功，孤立批次在无选中镜头时仍可见", async () => {
        detailState.shots = [];
        detailState.shotRevisions = [];
        writeStoredBatchLog({
            version: 1,
            ownerScope: "user-a",
            projectId: "p1",
            unitId: "u1",
            batchId: "batch-orphan-1",
            stage: "storyboard",
            scope: "missing",
            settings: {
                model: useConfigStore.getState().config.imageModel,
                size: "16:9",
                quality: "auto",
                skillIds: [],
            },
            state: "paused",
            items: [
                {
                    itemId: "item-orphan-1",
                    shotId: "s-deleted",
                    revisionId: "r-deleted",
                    fingerprint: "fp-orphan",
                    operation: "create",
                    state: "unknown",
                    issueCode: "submit_unknown",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:00.000Z",
                },
            ],
            createdAt: "2026-10-29T00:00:00.000Z",
            updatedAt: "2026-10-29T00:00:00.000Z",
        });

        const mounted = await mountWorkbench({ activeStage: "storyboard", selectedShot: undefined });
        try {
            expect(document.body.textContent).toContain("当前章节还没有分镜");
            expect(document.body.textContent).toContain("本章分镜图批次（paused）");
            expect(findButton("查看本章批次")).toBeTruthy();
            expect(findButton("刷新状态")).toBeTruthy();

            await act(async () => {
                findButton("查看本章批次")!.click();
                await settle();
            });
            expect(document.body.textContent).toContain("提交状态未知");
            const confirmBtn = findButton("确认提交 0 个任务");
            expect(confirmBtn?.disabled).toBe(true);
        } finally {
            await mounted.dispose();
        }
    });
});

describe("workflow image batch dialog", () => {
    test("dialog: 点击storyboard/previz标题准确，关闭或取消零任务，Escape关闭，改设置立即失效旧预览并重算token，旧报价返回不覆盖新设置", async () => {
        let slowQuoteResolver: ((val: unknown) => void) | null = null;
        customQuoteHandler = body => {
            const size =
                (body?.options as Record<string, unknown>)?.size ??
                ((body?.intent as Record<string, any>)?.options as Record<string, unknown>)?.size;
            if (size === "9:16" || size === "1024x1824") {
                return new Promise(resolve => {
                    slowQuoteResolver = resolve;
                });
            }
            if (size === "1:1" || size === "1024x1024") {
                return {
                    quote: {
                        logicalModel: String(body?.model),
                        capability: "image",
                        amountMicrocredits: 0,
                        billingMode: "fixed_request",
                        estimated: false,
                    },
                };
            }
            return {
                quote: {
                    logicalModel: String(body?.model),
                    capability: "image",
                    amountMicrocredits: 300_000,
                    billingMode: "fixed_request",
                    estimated: true,
                },
            };
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            expect(document.body.textContent).toContain("批量生成分镜图");
            expect(document.body.textContent).toContain("预计 0.6 积分");

            const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
            expect(dialog.contains(document.activeElement)).toBe(true);
            await act(async () => {
                document.activeElement!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", code: "Escape", keyCode: 27, bubbles: true }));
                await settle();
            });
            expect(document.querySelector(".workflow-batch-dialog")).toBeNull();
            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(0);

            await act(async () => {
                findButton("批量生成动作预演")!.click();
                await settle();
            });
            expect(document.body.textContent).toContain("批量生成动作预演");

            // Trigger slow quote (9:16) then fast quote (1:1) to verify stale quote does not overwrite newer settings
            const dialogEl = document.querySelector<HTMLElement>(".workflow-batch-dialog")!;
            const ratio916Btn = findButton("9:16", dialogEl);
            const ratio11Btn = findButton("1:1", dialogEl);
            expect(ratio916Btn).toBeTruthy();
            expect(ratio11Btn).toBeTruthy();
            if (ratio916Btn && ratio11Btn) {
                await act(async () => {
                    ratio916Btn.click();
                    await new Promise(resolve => setTimeout(resolve, 10));
                });
                await act(async () => {
                    ratio11Btn.click();
                    await settle();
                });
                if (slowQuoteResolver) {
                    await act(async () => {
                        slowQuoteResolver!({
                            quote: {
                                logicalModel: "doubao-seedream-4-0-250828",
                                capability: "image",
                                amountMicrocredits: 9_000_000,
                                billingMode: "fixed_request",
                                estimated: true,
                            },
                        });
                        await settle();
                    });
                }
                expect(dialogEl.textContent).toContain("0 积分");
                expect(dialogEl.textContent).not.toContain("免费");
                expect(dialogEl.textContent).not.toContain("18 积分");
            }

            // Unknown quote should display 费用待提交时按实际规格计算, never 免费
            customQuoteHandler = () => {
                throw new Error("quote service unavailable");
            };
            const staleTab = findButton("更新过期", dialogEl);
            await act(async () => {
                staleTab?.click();
                await settle();
            });
            const missingTab = findButton("补齐缺失", dialogEl);
            await act(async () => {
                missingTab?.click();
                await settle();
            });
            expect(dialogEl.textContent).toContain("已知部分预计 0 积分，另有 2 项费用无法预估");
            expect(dialogEl.textContent).not.toContain("免费");
        } finally {
            await mounted.dispose();
        }
    });

    test("dialog: ready/active/failed/stale/missing混合时默认只提交missing，确认数量与实际一致，无当前修订镜头标为blocked且不自动保存", async () => {
        const now = "2026-10-29T00:00:00.000Z";
        detailState.shots = [1, 2, 3, 4, 5, 6].map(n => ({
            id: `s${n}`,
            projectId: "p1",
            unitId: "u1",
            currentRevisionId: n === 6 ? "" : `r${n}`,
            title: `镜头0${n}`,
            description: `画面${n}`,
            position: n - 1,
            durationMs: 3000,
            status: "draft",
            createdAt: now,
            updatedAt: now,
        }));
        detailState.shotRevisions = [1, 2, 3, 4, 5].map(n => ({
            id: `r${n}`,
            shotId: `s${n}`,
            version: 1,
            plotDescription: `镜头${n}画面描述`,
            action: "动作",
            dialogue: "",
            shotSize: "中景",
            cameraAngle: "平视",
            cameraMovement: "固定",
            durationMs: 3000,
            imagePrompt: `镜头${n}提示词`,
            videoPrompt: `镜头${n}视频提示词`,
            negativePrompt: "",
            continuityNotes: "",
            actionBeatsJson: "[]",
            createdAt: now,
        }));
        // s1: ready, s2: active, s3: failed, s4: stale, s5: missing, s6: blocked (no revision)
        detailState.shotArtifacts = [
            makeReadyArtifact("s1", "r1", "storyboard", "task-ready-1"),
            {
                ...makeReadyArtifact("s4", "r-old", "storyboard", "task-stale-4"),
                status: "stale",
            },
        ];
        const activeTask = makeBatchTask("s2", "running", "storyboard");
        const failedTask = {
            ...makeBatchTask("s3", "failed", "storyboard"),
            inputJson: JSON.stringify({
                metadata: {
                    shotId: "s3",
                    shotRevisionId: "r3",
                    artifactType: "storyboard",
                },
            }),
        };
        tasksById.set(activeTask.id, activeTask);
        tasksById.set(failedTask.id, failedTask);
        detailState.tasks = [activeTask, failedTask];

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });

            expect(document.body.textContent).toContain("可提交 1 · 已完成 1 · 在途 1 · 过期 1 · 失败 1 · 阻止 1（已跳过 5 个镜头）");
            expect(document.body.textContent).toContain("镜头缺少已保存的当前脚本修订");
            const confirmBtn = findButton("确认提交 1 个任务");
            expect(confirmBtn).toBeTruthy();
            expect(confirmBtn?.disabled).toBe(false);

            await act(async () => {
                confirmBtn!.click();
                await settle(60);
            });

            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(1);
            const submittedShotId = (postReqs[0]!.body?.input as any)?.metadata?.shotId;
            expect(submittedShotId).toBe("s5");
            expect(recordedRequests.filter(r => r.pathname.includes("/shots"))).toHaveLength(0);
        } finally {
            await mounted.dispose();
        }
    });

    test("dialog: 失败重试模式锁定原任务设置，仅POST原id/retry，canRecoverMedia走提示而非新生成，重试刚被其他请求接受时重新query原id", async () => {
        const failedRetryable: GenerationTask = {
            ...makeBatchTask("s1", "failed", "storyboard"),
            id: "task-fail-s1",
            attempts: 1,
            inputJson: JSON.stringify({
                metadata: {
                    shotId: "s1",
                    shotRevisionId: "r1",
                    artifactType: "storyboard",
                },
            }),
        };
        const failedRecoverMedia: GenerationTask = {
            ...makeBatchTask("s2", "failed", "storyboard"),
            id: "task-recover-s2",
            canRecoverMedia: true,
            attempts: 1,
            inputJson: JSON.stringify({
                metadata: {
                    shotId: "s2",
                    shotRevisionId: "r2",
                    artifactType: "storyboard",
                },
            }),
        };
        tasksById.set(failedRetryable.id, failedRetryable);
        tasksById.set(failedRecoverMedia.id, failedRecoverMedia);
        detailState.tasks = [failedRetryable, failedRecoverMedia];

        // Simulate retry endpoint throwing 409 because another concurrent request just accepted the retry (task is now running/succeeded on re-query)
        let retryCalled = false;
        customRetryTaskHandler = async (taskId, config) => {
            retryCalled = true;
            const updated: GenerationTask = {
                ...failedRetryable,
                status: "running",
                attempts: 2,
                updatedAt: "2026-10-29T00:03:00.000Z",
            };
            tasksById.set(taskId, updated);
            throw makeAxiosReject(config, 409, 409, "task already queued", "conflict");
        };
        let getAfterRetryCount = 0;
        customGetTaskHandler = async taskId => {
            const existing = tasksById.get(taskId)!;
            if (retryCalled && taskId === "task-fail-s1") {
                getAfterRetryCount += 1;
                if (getAfterRetryCount >= 2) {
                    const done: GenerationTask = {
                        ...existing,
                        status: "succeeded",
                        attempts: 2,
                        updatedAt: "2026-10-29T00:04:00.000Z",
                    };
                    tasksById.set(taskId, done);
                    detailState = {
                        ...detailState,
                        shotArtifacts: [
                            ...(detailState.shotArtifacts || []),
                            makeReadyArtifact("s1", "r1", "storyboard", taskId),
                        ],
                    };
                    return done;
                }
            }
            return existing;
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });

            await act(async () => {
                findButton("重试失败")!.click();
                await settle();
            });

            expect(document.body.textContent).toContain("失败重试模式锁定原任务设置");
            expect(document.body.textContent).toContain("重试保存");
            const confirmBtn = findButton("确认提交 1 个任务");
            expect(confirmBtn).toBeTruthy();

            await act(async () => {
                confirmBtn!.click();
                await settle(80);
            });

            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(0);
            expect(
                recordedRequests.filter(
                    r => r.method === "POST" && r.pathname === "/tasks/task-fail-s1/retry",
                ),
            ).toHaveLength(1);
        } finally {
            await mounted.dispose();
        }
    });
});

async function mountBatchHook(withSkill = false) {
    let controller!: ReturnType<typeof useWorkflowImageBatch>;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    function Harness() {
        const config = useConfigStore(state => state.config);
        controller = useWorkflowImageBatch({
            detail: detailState, projectId: "p1", unitId: "u1", editorDirty: false,
            config, skills: withSkill ? [{ skillId: "test-skill", skillName: "测试技能", isAdded: true, versionId: "v1", version: "1" } as any] : [],
            initialSettings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: withSkill ? ["test-skill"] : [] },
            onRefresh: async () => {},
        });
        return null;
    }
    await act(async () => { root.render(<Harness />); await settle(); });
    return {
        get controller() { return controller; },
        async dispose() { await act(async () => root.unmount()); host.remove(); },
    };
}

async function seedPausedBatch() {
    const config = useConfigStore.getState().config;
    const preview = await buildBatchPreview({
        identity: { ownerScope: "user-a", projectId: "p1", unitId: "u1" },
        detail: detailState, stage: "storyboard", scope: "missing", config, skills: [],
        settings: { model: config.imageModel, size: "16:9", quality: "auto", skillIds: [] },
    });
    const log = { ...preview.identity, version: 1, batchId: "paused-shared-batch", stage: preview.stage,
        scope: preview.scope, settings: preview.settings, state: "paused", items: preview.items,
        createdAt: "2026-10-29T00:00:00Z", updatedAt: "2026-10-29T00:00:00Z" };
    writeStoredBatchLog(log);
    return log;
}

function imagePosts() {
    return recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
}

describe("workflow image batch lifecycle", () => {
    test("POST in flight unmount and remount recovers unknown without replay", async () => {
        let rejectPost!: (error: unknown) => void;
        let postConfig!: InternalAxiosRequestConfig;
        customPostTaskHandler = (_body, config) => { postConfig = config; return new Promise((_resolve, reject) => { rejectPost = reject; }); };
        const first = await mountBatchHook();
        await act(async () => { await first.controller.open("storyboard"); await settle(); });
        await act(async () => { void first.controller.confirm(); await settle(); });
        expect(readStoredBatchLog().items[0].state).toBe("submitting");
        await first.dispose();
        const second = await mountBatchHook();
        try {
            await act(async () => { rejectPost(new AxiosError("response lost", "ECONNABORTED", postConfig)); await settle(); });
            expect(readStoredBatchLog().items[0].state).toBe("unknown");
            await act(async () => { await second.controller.refreshStatus(); await settle(); });
            expect(second.controller.log?.items[0]?.state).toBe("unknown");
            expect(imagePosts()).toHaveLength(1);
        } finally { await second.dispose(); }
    });

    test("real stop and resume buttons retain sent task ID and require a new confirmation", async () => {
        let resolvePost!: (task: GenerationTask) => void;
        customPostTaskHandler = () => new Promise(resolve => { resolvePost = resolve; });
        const mounted = await mountWorkbench();
        try {
            await act(async () => { findButton("批量生成分镜图")!.click(); await settle(); });
            await act(async () => { findButton("确认提交 2 个任务")!.click(); await settle(); });
            await act(async () => { findButton("停止剩余提交", document.querySelector(".workflow-batch-dialog")!)!.click(); await settle(); });
            expect(readStoredBatchLog().items[0].state).toBe("submitting");
            expect(readStoredBatchLog().items[1].state).toBe("stopped");
            const done = makeBatchTask("s1", "succeeded", "storyboard");
            tasksById.set(done.id, done);
            detailState.shotArtifacts = [makeReadyArtifact("s1", "r1", "storyboard", done.id)];
            await act(async () => { resolvePost(done); await settle(); });
            expect(readStoredBatchLog().items[0].taskId).toBe(done.id);
            customPostTaskHandler = null;
            await act(async () => { findButton("继续剩余")!.click(); await settle(); });
            expect(imagePosts()).toHaveLength(1);
            expect(findButton("确认提交 1 个任务")?.disabled).toBe(false);
            await act(async () => { findButton("确认提交 1 个任务")!.click(); await settle(80); });
            expect(imagePosts()).toHaveLength(2);
        } finally { await mounted.dispose(); }
    });

    test("stop while confirmation is still loading chapter state prevents every task POST", async () => {
        const mounted = await mountBatchHook();
        let release!: () => void;
        try {
            await act(async () => { await mounted.controller.open("storyboard"); await settle(); });
            customWorkspaceHandler = () => new Promise(resolve => { release = resolve; });
            await act(async () => { void mounted.controller.confirm(); await settle(); });
            expect(release).toBeDefined();
            await act(async () => { await mounted.controller.stopRemaining(); customWorkspaceHandler = null; release(); await settle(80); });
            expect(imagePosts()).toHaveLength(0);
            expect(readStoredBatchLog().state).toBe("stopped");
        } finally { release?.(); await mounted.dispose(); }
    });

    test("first confirmation exposes a real stop button before any batch log exists", async () => {
        const mounted = await mountWorkbench();
        let release!: () => void;
        try {
            await act(async () => { findButton("批量生成分镜图")!.click(); await settle(); });
            customWorkspaceHandler = () => new Promise(resolve => { release = resolve; });
            await act(async () => { findButton("确认提交 2 个任务")!.click(); await settle(); });
            expect(readStoredBatchLog()).toBeNull();
            const stop = findButton("停止剩余提交", document.querySelector(".workflow-batch-dialog")!);
            expect(stop).toBeTruthy();
            await act(async () => { stop!.click(); customWorkspaceHandler = null; release(); await settle(80); });
            expect(imagePosts()).toHaveLength(0);
            expect(readStoredBatchLog().state).toBe("stopped");
        } finally { release?.(); await mounted.dispose(); }
    });

    test("stop inside submit skill preparation sends zero task POSTs", async () => {
        const mounted = await mountBatchHook(true);
        let release!: () => void;
        let preparing = false;
        try {
            await act(async () => { await mounted.controller.open("storyboard"); await settle(); });
            customSkillFileHandler = () => { preparing = true; return new Promise(resolve => { release = resolve; }); };
            await act(async () => { void mounted.controller.confirm(); await settle(); });
            expect(preparing).toBe(true);
            await act(async () => { await mounted.controller.stopRemaining(); release(); await settle(); });
            expect(imagePosts()).toHaveLength(0);
            expect(readStoredBatchLog().items.map((i: any) => i.state)).toEqual(["stopped", "stopped"]);
        } finally { release?.(); await mounted.dispose(); }
    });

    test("stop during original retry task GET sends zero retry POSTs", async () => {
        const failed = makeBatchTask("s1", "failed", "storyboard");
        tasksById.set(failed.id, failed);
        const mounted = await mountBatchHook();
        let release!: (task: GenerationTask) => void;
        try {
            await act(async () => { await mounted.controller.open("storyboard"); await settle(); });
            await act(async () => { await mounted.controller.refreshPreview("failed", mounted.controller.settings); await settle(); });
            customGetTaskHandler = () => new Promise(resolve => { release = resolve; });
            await act(async () => { void mounted.controller.confirm(); await settle(); });
            expect(release).toBeDefined();
            await act(async () => { await mounted.controller.stopRemaining(); release(failed); await settle(); });
            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname.startsWith("/tasks"))).toHaveLength(0);
            expect(readStoredBatchLog().items[0].state).toBe("stopped");
        } finally { release?.(failed); await mounted.dispose(); }
    });

    test("stop during retry baseline persistence sends zero retry POSTs", async () => {
        const failed = makeBatchTask("s1", "failed", "storyboard");
        tasksById.set(failed.id, failed);
        const mounted = await mountBatchHook();
        let release!: () => void;
        try {
            await act(async () => { await mounted.controller.open("storyboard"); await settle(); });
            await act(async () => { await mounted.controller.refreshPreview("failed", mounted.controller.settings); await settle(); });
            customStorageWriteHandler = log => {
                if (!release && log.items[0].state === "submitting" && log.items[0].taskAttemptsBefore === 0) {
                    return new Promise(resolve => { release = resolve; });
                }
                return Promise.resolve();
            };
            await act(async () => { void mounted.controller.confirm(); await settle(); });
            expect(release).toBeDefined();
            await act(async () => { const stopping = mounted.controller.stopRemaining(); release(); await stopping; await settle(); });
            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname.startsWith("/tasks"))).toHaveLength(0);
            expect(readStoredBatchLog().items[0].state).toBe("stopped");
        } finally { release?.(); await mounted.dispose(); }
    });

    test("a pending status refresh cannot overwrite a newer cross-tab task receipt", async () => {
        const seed = await seedPausedBatch();
        writeStoredBatchLog({ ...seed, items: seed.items.map(i => ({ ...i, state: "unknown" })) });
        const mounted = await mountBatchHook();
        let release!: () => void;
        try {
            customWorkspaceHandler = () => new Promise(resolve => { release = resolve; });
            let refreshing!: Promise<void>;
            await act(async () => { refreshing = mounted.controller.refreshStatus(); await settle(); });
            const latest = readStoredBatchLog();
            latest.items[0].taskId = "new-receipt";
            latest.items[0].state = "submitted";
            writeStoredBatchLog(latest);
            await act(async () => { release(); await refreshing; await settle(); });
            expect(readStoredBatchLog().items[0]).toMatchObject({ state: "submitted", taskId: "new-receipt" });
            expect(imagePosts()).toHaveLength(0);
        } finally { release?.(); await mounted.dispose(); }
    });

    test("same batch: two old resume previews cannot overwrite a lost response after acquiring lock", async () => {
        await seedPausedBatch();
        const a = await mountBatchHook();
        const b = await mountBatchHook();
        try {
            await act(async () => { await a.controller.prepareResume(); await settle(); });
            await act(async () => { await b.controller.prepareResume(); await settle(); });
            customPostTaskHandler = async (_body, config) => { throw new AxiosError("lost", "ECONNABORTED", config); };
            await act(async () => { await b.controller.confirm(); await settle(); });
            expect(imagePosts()).toHaveLength(1);
            expect(readStoredBatchLog().items[0].state).toBe("unknown");
            await act(async () => { await a.controller.confirm(); await settle(); });
            expect(imagePosts()).toHaveLength(1);
            expect(readStoredBatchLog().items[0].state).toBe("unknown");
        } finally { await a.dispose(); await b.dispose(); }
    });

    test("refresh while runner POST is pending cannot overwrite its later unknown or stop state", async () => {
        let rejectPost!: (error: unknown) => void;
        let postConfig!: InternalAxiosRequestConfig;
        customPostTaskHandler = (_body, config) => { postConfig = config; return new Promise((_resolve, reject) => { rejectPost = reject; }); };
        const mounted = await mountBatchHook();
        try {
            await act(async () => { await mounted.controller.open("storyboard"); await settle(); });
            await act(async () => { void mounted.controller.confirm(); await settle(); });
            await act(async () => { await mounted.controller.refreshStatus(); await settle(); });
            expect(mounted.controller.runtimeError).toBeNull();
            await act(async () => { await mounted.controller.stopRemaining(); rejectPost(new AxiosError("lost", "ECONNABORTED", postConfig)); await settle(); });
            expect(readStoredBatchLog().items[0].state).toBe("unknown");
            expect(readStoredBatchLog().state).toBe("stopped");
            expect(imagePosts()).toHaveLength(1);
        } finally { await mounted.dispose(); }
    });

    test("second mounted observer never recovers an actively owned submitting log", async () => {
        let resolvePost!: (task: GenerationTask) => void;
        customPostTaskHandler = () => new Promise(resolve => { resolvePost = resolve; });
        const a = await mountBatchHook();
        let b: Awaited<ReturnType<typeof mountBatchHook>> | undefined;
        try {
            await act(async () => { await a.controller.open("storyboard"); await settle(); });
            await act(async () => { void a.controller.confirm(); await settle(); });
            expect(readStoredBatchLog().items[0].state).toBe("submitting");
            const before = JSON.stringify(readStoredBatchLog());
            lockHeldByOtherTab = true;
            b = await mountBatchHook();
            expect(JSON.stringify(readStoredBatchLog())).toBe(before);
        } finally {
            lockHeldByOtherTab = false;
            await act(async () => { resolvePost(makeBatchTask("s1", "failed", "storyboard")); await a.controller.stopRemaining(); await settle(); });
            await b?.dispose(); await a.dispose();
        }
    });

    test("lifecycle: 相同内容连续点开始create次数不翻倍且只一个runner/lock，两标签锁竞争时第二个confirm零POST且获锁后重读日志", async () => {
        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });

            const confirmBtn = findButton("确认提交 2 个任务")!;
            await act(async () => {
                confirmBtn.click();
                confirmBtn.click();
                await settle(80);
            });

            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(2);
            expect(lockRequestCount).toBe(1);

            // Reset shots to missing and simulate second tab holding Web Lock
            detailState = makeWorkflowBatchDetail();
            tasksById.clear();
            recordedRequests = [];
            lockHeldByOtherTab = true;

            await mounted.rerender({ detail: detailState });
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            const secondConfirmBtn = findButton("确认提交 2 个任务")!;
            await act(async () => {
                secondConfirmBtn.click();
                await settle(50);
            });

            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(0);
            expect(document.body.textContent).toContain("本章已在其他标签页处理批量任务");
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: 上限为1时首镜完成且产物同步后补位；产物稍晚出现时保持syncing不重复提交", async () => {
        useUserStore.setState({ runtimeLimits: { ...useUserStore.getState().runtimeLimits!, activeTaskLimit: 1 } });
        let s1PollCount = 0;
        let releaseRunning!: () => void;
        let releaseSyncing!: () => void;
        customPostTaskHandler = async body => {
            const shotId = String((body?.input as any)?.metadata?.shotId || "s1");
            const id = `task-${shotId}`;
            const status = shotId === "s1" ? "running" : "succeeded";
            const task: GenerationTask = {
                ...makeBatchTask(shotId, status, "storyboard"),
                id,
            };
            tasksById.set(id, task);
            if (shotId === "s2") {
                detailState = {
                    ...detailState,
                    shotArtifacts: [
                        ...(detailState.shotArtifacts || []),
                        makeReadyArtifact("s2", "r2", "storyboard", id),
                    ],
                };
            }
            return task;
        };
        customGetTaskHandler = async taskId => {
            if (taskId === "task-s1") {
                s1PollCount += 1;
                if (s1PollCount === 1) {
                    await new Promise<void>(resolve => { releaseRunning = resolve; });
                    return tasksById.get(taskId)!;
                }
                if (s1PollCount === 3) await new Promise<void>(resolve => { releaseSyncing = resolve; });
                const succeededTask: GenerationTask = {
                    ...tasksById.get(taskId)!,
                    status: "succeeded",
                };
                tasksById.set(taskId, succeededTask);
                if (s1PollCount >= 3) {
                    detailState = {
                        ...detailState,
                        shotArtifacts: [
                            ...(detailState.shotArtifacts || []),
                            makeReadyArtifact("s1", "r1", "storyboard", taskId),
                        ],
                    };
                }
                return succeededTask;
            }
            return tasksById.get(taskId)!;
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle(120);
            });

            expect(imagePosts()).toHaveLength(1);
            expect(readStoredBatchLog().items[0].state).toBe("submitted");
            await act(async () => { releaseRunning(); await settle(); });
            expect(imagePosts()).toHaveLength(1);
            expect(readStoredBatchLog().items[0].state).toBe("syncing");
            await act(async () => { releaseSyncing(); await settle(100); });
            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(2);
            expect((postReqs[0]!.body?.input as any)?.metadata?.shotId).toBe("s1");
            expect((postReqs[1]!.body?.input as any)?.metadata?.shotId).toBe("s2");
            expect(s1PollCount).toBeGreaterThanOrEqual(3);
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: 容量满(task_active_limit_exceeded)与429(rate_limited)正确退避，确定性拒绝/余额不足保留已提交项并停止剩余", async () => {
        detailState.shots.push({ ...detailState.shots[1]!, id: "s3", currentRevisionId: "r3", position: 2 });
        detailState.shotRevisions.push({ ...detailState.shotRevisions[1]!, id: "r3", shotId: "s3" });
        let s1Attempts = 0;
        customPostTaskHandler = async (body, config) => {
            const shotId = String((body?.input as any)?.metadata?.shotId || "s1");
            if (shotId === "s1") {
                s1Attempts += 1;
                if (s1Attempts === 1) {
                    throw makeAxiosReject(
                        config,
                        400,
                        400,
                        "active task limit exceeded",
                        "task_active_limit_exceeded",
                    );
                }
                if (s1Attempts === 2) {
                    throw makeAxiosReject(config, 429, 42901, "too many requests", "rate_limited");
                }
                const task = makeBatchTask("s1", "succeeded", "storyboard");
                tasksById.set(task.id, task);
                detailState = {
                    ...detailState,
                    shotArtifacts: [
                        ...(detailState.shotArtifacts || []),
                        makeReadyArtifact("s1", "r1", "storyboard", task.id),
                    ],
                };
                return task;
            }
            throw makeAxiosReject(
                config,
                402,
                402,
                "insufficient credits",
                "insufficient_balance",
            );
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 3 个任务")!.click();
                await settle(120);
            });

            expect(imagePosts().some(r => (r.body?.input as any)?.metadata?.shotId === "s3")).toBe(false);
            expect(s1Attempts).toBe(3);
            expect(sleepDelays.some(d => d === 2_000)).toBe(true);
            expect(sleepDelays.some(d => d === 30_000)).toBe(true);

            const stored = readStoredBatchLog("user-a");
            expect(stored.state).toBe("paused");
            expect(stored.items[0].state).toBe("succeeded");
            expect(stored.items[1].state).toBe("blocked");
            expect(stored.items[1].issueCode).toBe("insufficient_balance");
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: POST已接单但响应丢失或POST中刷新/卸载时submitting恢复为unknown，历史查询无匹配保持unknown绝不重发，正向匹配唯一任务才认领", async () => {
        let postCount = 0;
        let matchingServerTask: GenerationTask | null = null;
        customPostTaskHandler = async (body, config) => {
            postCount += 1;
            const inputObj = (body?.input || {}) as Record<string, any>;
            const meta = inputObj.metadata || {};
            matchingServerTask = {
                ...makeBatchTask("s1", "succeeded", "storyboard"),
                id: "task-lost-response-1",
                projectId: "p1",
                inputJson: JSON.stringify({ input: inputObj, metadata: meta }),
            };
            throw new AxiosError("network timeout", "ECONNABORTED", config);
        };

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle(80);
            });

            expect(postCount).toBe(1);
            const storedAfterLost = readStoredBatchLog("user-a");
            expect(storedAfterLost.items[0].state).toBe("unknown");
            expect(storedAfterLost.items[1].state).toBe("pending");

            // Refresh status when server has no matching task -> stays unknown, never replays POST
            await act(async () => {
                findButton("核对任务")!.click();
                await settle(60);
            });
            expect(postCount).toBe(1);
            const storedAfterEmptyCheck = readStoredBatchLog("user-a");
            expect(storedAfterEmptyCheck.items[0].state).toBe("unknown");

            // Now server exposes the matching task with exact chapterImageBatchId & chapterImageBatchItemId
            tasksById.set(matchingServerTask!.id, matchingServerTask!);
            detailState = {
                ...detailState,
                shotArtifacts: [
                    ...(detailState.shotArtifacts || []),
                    makeReadyArtifact("s1", "r1", "storyboard", matchingServerTask!.id),
                ],
                tasks: [matchingServerTask!],
            };

            await act(async () => {
                findButton("核对任务")!.click();
                await settle(60);
            });

            expect(postCount).toBe(1);
            const storedAfterClaim = readStoredBatchLog("user-a");
            expect(storedAfterClaim.items[0].taskId).toBe("task-lost-response-1");
            expect(storedAfterClaim.items[0].state).toBe("succeeded");
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: 已知ID在最近100条之外时按ID单查恢复不新建；retry响应丢失但旧taskId仍为failed且attempts/updatedAt未进时不误判完成或自动再retry", async () => {
        const offListTask: GenerationTask = {
            ...makeBatchTask("s1", "succeeded", "storyboard"),
            id: "task-off-list-999",
        };
        const unprogressedFailedTask: GenerationTask = {
            ...makeBatchTask("s2", "failed", "storyboard"),
            id: "task-retry-lost-2",
            attempts: 1,
            updatedAt: "2026-10-29T00:00:00.000Z",
        };
        tasksById.set(offListTask.id, offListTask);
        omittedTaskIds.add(offListTask.id);
        tasksById.set(unprogressedFailedTask.id, unprogressedFailedTask);
        detailState.shotArtifacts = [
            makeReadyArtifact("s1", "r1", "storyboard", offListTask.id),
        ];

        writeStoredBatchLog({
            version: 1,
            ownerScope: "user-a",
            projectId: "p1",
            unitId: "u1",
            batchId: "batch-recover-id",
            stage: "storyboard",
            scope: "failed",
            settings: {
                model: useConfigStore.getState().config.imageModel,
                size: "16:9",
                quality: "auto",
                skillIds: [],
            },
            state: "paused",
            items: [
                {
                    itemId: "item-1",
                    shotId: "s1",
                    revisionId: "r1",
                    fingerprint: "fp1",
                    operation: "create",
                    taskId: offListTask.id,
                    state: "submitted",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:00.000Z",
                },
                {
                    itemId: "item-2",
                    shotId: "s2",
                    revisionId: "r2",
                    fingerprint: "fp2",
                    operation: "retry",
                    taskId: unprogressedFailedTask.id,
                    taskAttemptsBefore: 1,
                    taskUpdatedAtBefore: "2026-10-29T00:00:00.000Z",
                    state: "submitting",
                    attempts: 1,
                    updatedAt: "2026-10-29T00:00:00.000Z",
                },
            ],
            createdAt: "2026-10-29T00:00:00.000Z",
            updatedAt: "2026-10-29T00:00:00.000Z",
        });

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                await settle(80);
            });

            const storedAfterMount = readStoredBatchLog("user-a");
            expect(storedAfterMount.items[0].state).toBe("succeeded");
            expect(storedAfterMount.items[1].state).toBe("unknown");

            await act(async () => {
                findButton("刷新状态")!.click();
                await settle(60);
            });

            const storedAfterRefresh = readStoredBatchLog("user-a");
            // Because attempts and updatedAt did not advance past pre-retry baseline, item-2 stays unknown
            expect(storedAfterRefresh.items[1].state).toBe("unknown");
            expect(
                recordedRequests.filter(r => r.method === "POST" && r.pathname.startsWith("/tasks")),
            ).toHaveLength(0);
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: 产物变stale或镜头版本变化时不计有效成功且剩余项预检阻止；停止剩余时已发任务继续、未发不再POST，恢复需重新确认", async () => {
        let stopTriggered = false;
        let hookRef: ReturnType<typeof useWorkflowImageBatch> | null = null;
        customPostTaskHandler = async body => {
            const shotId = String((body?.input as any)?.metadata?.shotId || "s1");
            const task = makeBatchTask(shotId, "running", "storyboard");
            tasksById.set(task.id, task);
            return task;
        };
        customGetTaskHandler = async taskId => {
            const existing = tasksById.get(taskId)!;
            if (!stopTriggered) {
                stopTriggered = true;
                await hookRef?.stopRemaining();
            }
            const done: GenerationTask = { ...existing, status: "succeeded" };
            tasksById.set(taskId, done);
            detailState = {
                ...detailState,
                shotArtifacts: [
                    ...(detailState.shotArtifacts || []),
                    makeReadyArtifact("s1", "r1", "storyboard", taskId),
                ],
            };
            return done;
        };

        function Harness() {
            const config = useConfigStore(state => state.config);
            const controller = useWorkflowImageBatch({
                detail: detailState,
                projectId: "p1",
                unitId: "u1",
                editorDirty: false,
                config,
                skills: [],
                initialSettings: {
                    model: config.imageModel,
                    size: "16:9",
                    quality: "auto",
                    skillIds: [],
                },
                onRefresh: async () => {},
            });
            hookRef = controller;
            return null;
        }

        const host = document.createElement("div");
        document.body.append(host);
        const root = createRoot(host);
        try {
            await act(async () => {
                root.render(<Harness />);
                await settle();
            });
            await act(async () => {
                await hookRef!.open("storyboard");
                await settle();
            });
            await act(async () => {
                await hookRef!.confirm();
                await settle(80);
            });

            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(1);
            expect(hookRef!.log?.state).toBe("stopped");
            expect(hookRef!.log?.items[0]?.state).toBe("succeeded");
            expect(hookRef!.log?.items[1]?.state).toBe("stopped");

            // Mutate s2 revision before prepareResume -> s2 becomes blocked on resume preview
            detailState.shots[1] = {
                ...detailState.shots[1]!,
                currentRevisionId: "r2-mutated",
            };
            await act(async () => {
                await hookRef!.prepareResume();
                await settle();
            });
            expect(hookRef!.preview?.items).toHaveLength(0);
            expect(hookRef!.log?.items[1]?.state).toBe("blocked");
            expect(hookRef!.log?.items[1]?.issueCode).toBe("revision_changed");
        } finally {
            await act(async () => root.unmount());
            host.remove();
        }
    });

    test("lifecycle: 存储写失败或静默跳过时提交前零POST、提交后失败停止补位并保留内存ID", async () => {
        storageFaultMode = "silent_skip";
        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle(60);
            });

            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(0);
            expect(document.body.textContent).toContain("批次记录未可靠保存，已停止新增提交");

            storageFaultMode = "throw_after_first_submit";
            storageWriteCount = 0;
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle(80);
            });

            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(1);
            expect(document.body.textContent).toContain("task-s1-1");
        } finally {
            await mounted.dispose();
        }
    });

    test("lifecycle: 用户A→B→A切换时迟到回执只写原scope，新UI与下一项不受旧epoch驱动", async () => {
        let resolveFirstPost: ((task: GenerationTask) => void) | null = null;
        customPostTaskHandler = () =>
            new Promise<GenerationTask>(resolve => {
                resolveFirstPost = resolve;
            });

        const mounted = await mountWorkbench({ activeStage: "storyboard" });
        try {
            await act(async () => {
                findButton("批量生成分镜图")!.click();
                await settle();
            });
            await act(async () => {
                findButton("确认提交 2 个任务")!.click();
                await settle();
            });

            expect(resolveFirstPost).toBeTruthy();

            // Switch to user-b while user-a's first POST is still in-flight
            await act(async () => {
                setActiveUserScope("user-b");
                useUserStore.setState({
                    hydrated: true,
                    user: {
                        id: "user-b",
                        username: "user-b",
                        email: "user-b@example.com",
                        role: "user",
                        status: "active",
                        balanceMicrocredits: 100_000_000,
                    } as any,
                });
                await settle();
            });

            expect(getScopeStore("user-b").size).toBe(0);
            const postReqs = recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks");
            expect(postReqs).toHaveLength(1);

            // Switch back to user-a: log should be recovered as paused/unknown (since response was aborted on switch), never auto-submitting s2
            await act(async () => {
                setActiveUserScope("user-a");
                useUserStore.setState({
                    hydrated: true,
                    user: {
                        id: "user-a",
                        username: "user-a",
                        email: "user-a@example.com",
                        role: "user",
                        status: "active",
                        balanceMicrocredits: 100_000_000,
                    } as any,
                });
                await settle(50);
            });

            // Only resolve the original epoch's receipt after A → B → A has completed.
            await act(async () => {
                resolveFirstPost!({ ...makeBatchTask("s1", "succeeded", "storyboard"), id: "task-late-user-a" });
                await settle(60);
            });
            expect(readStoredBatchLog("user-a").items[0].taskId).toBe("task-late-user-a");
            expect(getScopeStore("user-b").size).toBe(0);
            expect(recordedRequests.filter(r => r.method === "POST" && r.pathname === "/tasks")).toHaveLength(1);
        } finally {
            await mounted.dispose();
        }
    });
});
