import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { Window } from "happy-dom";
import type {
    BindingAppendRequest,
    BindingAppendResult,
    BindingBaseline,
    BindingPreview,
    ProjectDetail,
    ProjectShot,
} from "../src/services/api/projects";
import {
    STORYBOARD_ASSET_BINDING_CODES,
    STORYBOARD_ASSET_BINDING_REASONS,
} from "../src/services/api/error-codes";

const dom = new Window({ url: "http://localhost/projects/proj-1/chapters/unit-1" });
const originals = new Map<string, PropertyDescriptor | undefined>();
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
    HTMLButtonElement: dom.HTMLButtonElement,
    Element: dom.Element,
    Node: dom.Node,
    SVGElement: dom.SVGElement,
    ShadowRoot: dom.ShadowRoot,
    Event: dom.Event,
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
const { useUserStore } = await import("../src/stores/use-user-store");
const WorkflowProductionWorkbench = (await import("../src/pages/projects/detail/workflow-production-workbench")).default;

const drawerModulePath = "../src/pages/projects/detail/chapter-asset-binding-drawer";
const drawerModule = await import(drawerModulePath).catch(() => ({} as Record<string, any>));

const originalAdapter = apiClient.defaults.adapter;
const originalUser = useUserStore.getState().user;

beforeEach(() => {
    document.body.innerHTML = "";
    useUserStore.setState({ user: { id: "user-1" } as NonNullable<typeof originalUser> });
});

afterAll(() => {
    apiClient.defaults.adapter = originalAdapter;
    useUserStore.setState({ user: originalUser });
    dom.happyDOM.abort();
    for (const [key, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

const settle = (ms = 40) => new Promise((resolve) => setTimeout(resolve, ms));

function makeBaseline(shotIds = ["shot-1"], projectRevision = 3): BindingBaseline {
    return {
        projectId: "proj-1",
        unitId: "unit-1",
        projectRevision,
        shotIds,
        shots: shotIds.map((shotId) => ({
            shotId,
            shotFingerprint: `sf-${shotId}-${projectRevision}`,
            revisionFingerprint: `rf-${shotId}-${projectRevision}`,
            references: [],
            artifacts: [
                {
                    artifactId: `art-${shotId}`,
                    contentFingerprint: `cf-${shotId}`,
                    status: "ready",
                    selected: true,
                },
            ],
        })),
        catalog: [
            {
                assetId: "asset-sangruo",
                assetFingerprint: "af-1",
                linkFingerprint: "lf-1",
                versions: [
                    {
                        assetVersionId: "ver-sangruo-1",
                        versionFingerprint: "vf-1",
                        mediaFingerprint: "mf-1",
                        bindable: true,
                        missingImage: false,
                    },
                ],
            },
        ],
    };
}

function findButton(labelSubstring: string): HTMLButtonElement | undefined {
    const normalized = labelSubstring.replace(/\s+/g, "");
    return [...document.querySelectorAll<HTMLButtonElement>("button")].find((btn) =>
        btn.textContent?.replace(/\s+/g, "").includes(normalized),
    );
}

async function mountDrawer(props: {
    projectId?: string;
    unitId?: string;
    shotIds?: string[];
    open?: boolean;
    onClose?: () => void;
    onApplied?: (result: BindingAppendResult) => void;
    client?: InstanceType<typeof QueryClient>;
}) {
    expect(typeof drawerModule.ChapterAssetBindingDrawer).toBe("function");
    const DrawerComponent = drawerModule.ChapterAssetBindingDrawer;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const client = props.client || new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const renderWith = async (nextProps: Partial<typeof props> = {}) => {
        const merged = { ...props, ...nextProps };
        await act(async () => {
            root.render(
                <ConfigProvider theme={{ token: { motion: false } }}>
                    <App>
                        <QueryClientProvider client={client}>
                            <DrawerComponent
                                projectId={merged.projectId ?? "proj-1"}
                                unitId={merged.unitId ?? "unit-1"}
                                shotIds={merged.shotIds ?? ["shot-1"]}
                                open={merged.open ?? true}
                                onClose={merged.onClose ?? (() => {})}
                                onApplied={merged.onApplied ?? (() => {})}
                            />
                        </QueryClientProvider>
                    </App>
                </ConfigProvider>,
            );
        });
        await act(async () => {
            await settle();
        });
    };

    await renderWith();

    return {
        client,
        rerender: renderWith,
        dispose: async () => {
            await act(async () => root.unmount());
            if (!props.client) client.clear();
            host.remove();
        },
    };
}

describe("ChapterAssetBindingDrawer", () => {
    test("renders read-only existing references with multi-role distinct capacity, default selections, disabled missing_version, and selectable missing_image", async () => {
        const previewData: BindingPreview = {
            baseline: makeBaseline(["shot-1"]),
            shots: [
                {
                    shotId: "shot-1",
                    existingReferences: [
                        {
                            referenceId: "ref-role-1",
                            assetVersionId: "ver-existing-same",
                            role: "reference",
                            status: "linked",
                        },
                        {
                            referenceId: "ref-role-2",
                            assetVersionId: "ver-existing-same",
                            role: "start_frame",
                            status: "linked",
                        },
                    ],
                    existingVersionCount: 1,
                    remainingCapacity: 5,
                    issues: [
                        {
                            kind: "ambiguous",
                            message: "存在同名候选，请核对原文后手动确认",
                        },
                    ],
                    candidates: [
                        {
                            assetId: "asset-visual",
                            category: "character",
                            name: "桑若",
                            assetVersionId: "ver-sangruo-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: true,
                            evidence: [
                                {
                                    kind: "visual_label",
                                    field: "plotDescription",
                                    start: 0,
                                    end: 5,
                                    text: "角色：桑若",
                                },
                            ],
                            issues: [],
                        },
                        {
                            assetId: "asset-mention",
                            category: "character",
                            name: "裴让",
                            assetVersionId: "ver-peirang-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [
                                {
                                    kind: "mention",
                                    field: "plotDescription",
                                    start: 0,
                                    end: 6,
                                    text: "🌅裴让停止喝粥",
                                },
                            ],
                            issues: [],
                        },
                        {
                            assetId: "asset-speaker",
                            category: "character",
                            name: "沈知意",
                            assetVersionId: "ver-shen-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [
                                {
                                    kind: "speaker",
                                    field: "dialogue",
                                    start: 0,
                                    end: 3,
                                    text: "沈知意",
                                },
                            ],
                            issues: [],
                        },
                        {
                            assetId: "asset-no-version",
                            category: "prop",
                            name: "青玉簪",
                            assetVersionId: null,
                            bindable: false,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [
                                {
                                    kind: "structured_id",
                                    field: "projectAssetRefs",
                                    start: 0,
                                    end: 0,
                                    text: "",
                                },
                            ],
                            issues: [
                                {
                                    kind: "missing_version",
                                    message: "该资产尚无可用版本，不可绑定",
                                },
                            ],
                        },
                        {
                            assetId: "asset-no-image",
                            category: "environment",
                            name: "栖云阁",
                            assetVersionId: "ver-qiyunge-1",
                            bindable: true,
                            missingImage: true,
                            defaultSelected: false,
                            evidence: [
                                {
                                    kind: "exact_name",
                                    field: "plotDescription",
                                    start: 0,
                                    end: 3,
                                    text: "栖云阁",
                                },
                            ],
                            issues: [
                                {
                                    kind: "missing_image",
                                    message: "当前版本无可用参考图，绑定后仅记录版本引用",
                                },
                            ],
                        },
                    ],
                },
            ],
        };

        apiClient.defaults.adapter = async (config) => ({
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: { code: 0, data: previewData, msg: "ok" },
        });

        const view = await mountDrawer({ shotIds: ["shot-1"] });
        try {
            const text = document.body.textContent || "";
            // Exact artifact invalidation notice
            expect(text).toContain("仅实增镜头的相关产物将过期并取消选中，历史内容保留");
            // Both existing reference rows are displayed as read-only
            expect(text).toContain("ref-role-1");
            expect(text).toContain("ref-role-2");
            expect(text).toContain("start_frame");
            expect(text).toContain("只读");
            // Same assetVersionId across 2 roles counts as 1 distinct version, leaving 5 capacity
            expect(text).toContain("已有 1");
            expect(text).toContain("剩余容量 5");

            // Shot-level ambiguity issue and candidate-level issues displayed
            expect(text).toContain("存在同名候选，请核对原文后手动确认");
            expect(text).toContain("该资产尚无可用版本，不可绑定");
            expect(text).toContain("当前版本无可用参考图，绑定后仅记录版本引用");

            // Unicode code-point slicing [0, 6) on "🌅裴让停止喝粥" -> "🌅裴让停止喝" (no broken surrogate pair)
            expect(text).toContain("🌅裴让停止喝");

            // Checkbox states per candidate
            const sangruoCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-visual"]')!;
            const peirangCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-mention"]')!;
            const shenCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-speaker"]')!;
            const noVersionCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-no-version"]')!;
            const noImageCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-no-image"]')!;

            expect(sangruoCheckbox).toBeTruthy();
            expect(sangruoCheckbox.checked).toBe(true);
            expect(sangruoCheckbox.disabled).toBe(false);

            expect(peirangCheckbox).toBeTruthy();
            expect(peirangCheckbox.checked).toBe(false);
            expect(peirangCheckbox.disabled).toBe(false);

            expect(shenCheckbox).toBeTruthy();
            expect(shenCheckbox.checked).toBe(false);
            expect(shenCheckbox.disabled).toBe(false);

            expect(noVersionCheckbox).toBeTruthy();
            expect(noVersionCheckbox.checked).toBe(false);
            expect(noVersionCheckbox.disabled).toBe(true);

            expect(noImageCheckbox).toBeTruthy();
            expect(noImageCheckbox.checked).toBe(false);
            expect(noImageCheckbox.disabled).toBe(false);

            // Missing-image candidate can be toggled on
            await act(async () => {
                noImageCheckbox.click();
                await settle();
            });
            expect(noImageCheckbox.checked).toBe(true);
        } finally {
            await view.dispose();
        }
    });

    test("enforces 5 + 2 capacity limit: displays 总量 7 and 剩余容量 1 and requires manual reduction before confirm", async () => {
        const existingFive = [1, 2, 3, 4, 5].map((n) => ({
            referenceId: `ref-${n}`,
            assetVersionId: `ver-exist-${n}`,
            role: "reference",
            status: "linked",
        }));
        const previewData: BindingPreview = {
            baseline: makeBaseline(["shot-1"]),
            shots: [
                {
                    shotId: "shot-1",
                    existingReferences: existingFive,
                    existingVersionCount: 5,
                    remainingCapacity: 1,
                    issues: [
                        {
                            kind: "capacity_exceeded",
                            message: "已有版本与明确建议超过 6 个，请手动选择",
                        },
                    ],
                    candidates: [
                        {
                            assetId: "asset-new-1",
                            category: "character",
                            name: "新候选甲",
                            assetVersionId: "ver-new-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: true,
                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 4, text: "新候选甲" }],
                            issues: [],
                        },
                        {
                            assetId: "asset-new-2",
                            category: "prop",
                            name: "新候选乙",
                            assetVersionId: "ver-new-2",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: true,
                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 4, text: "新候选乙" }],
                            issues: [],
                        },
                    ],
                },
            ],
        };

        const appendCalls: BindingAppendRequest[] = [];
        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/append")) {
                appendCalls.push(JSON.parse(config.data));
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 4,
                            unchanged: false,
                            addedCount: 1,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    addedCount: 1,
                                    addedVersionIds: ["ver-new-1"],
                                    unchanged: false,
                                    references: [...existingFive, { referenceId: "ref-6", assetVersionId: "ver-new-1", role: "reference", status: "linked" }],
                                    artifacts: [],
                                },
                            ],
                        },
                    },
                };
            }
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: { code: 0, data: previewData, msg: "ok" },
            };
        };

        const view = await mountDrawer({ shotIds: ["shot-1"] });
        try {
            const text = document.body.textContent || "";
            expect(text).toContain("总量 7");
            expect(text).toContain("剩余容量 1");

            const confirmBtn = findButton("确认追加绑定")!;
            expect(confirmBtn).toBeTruthy();
            // Cannot confirm while 5 + 2 = 7 > 6
            expect(confirmBtn.disabled).toBe(true);

            // Reduce selection by unchecking the second candidate -> 5 + 1 = 6
            const secondCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-new-2"]')!;
            await act(async () => {
                secondCheckbox.click();
                await settle();
            });
            expect(confirmBtn.disabled).toBe(false);

            await act(async () => {
                confirmBtn.click();
                await settle();
            });
            expect(appendCalls).toHaveLength(1);
            expect(appendCalls[0]?.selections).toEqual([
                { shotId: "shot-1", assetVersionIds: ["ver-new-1"] },
            ]);
        } finally {
            await view.dispose();
        }
    });

    test("blocks preview when shotIds > 100 without splitting into batches, and aborts in-flight preview on close with zero writes", async () => {
        let previewCalls = 0;
        let appendCalls = 0;
        let capturedSignal: AbortSignal | undefined;

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                previewCalls++;
                capturedSignal = config.signal as AbortSignal | undefined;
                return await new Promise((_resolve, reject) => {
                    capturedSignal?.addEventListener("abort", () => {
                        const abortErr = new Error("Aborted");
                        abortErr.name = "AbortError";
                        reject(abortErr);
                    });
                });
            }
            if (config.url?.endsWith("/append")) {
                appendCalls++;
            }
            throw new Error("unexpected call");
        };

        const oversizedShotIds = Array.from({ length: 101 }, (_, idx) => `shot-${idx + 1}`);
        const view = await mountDrawer({ shotIds: oversizedShotIds });
        try {
            expect(previewCalls).toBe(0);
            expect(document.body.textContent).toContain("100");
            expect(document.body.textContent).toContain("缩小");
            expect(findButton("确认追加绑定")?.disabled).toBe(true);

            // Narrow to 2 valid shots -> starts in-flight preview
            await view.rerender({ shotIds: ["shot-1", "shot-2", "shot-1"] });
            expect(previewCalls).toBe(1);
            expect(capturedSignal?.aborted).toBe(false);

            // Close while preview is still in flight -> aborts signal and makes zero append calls
            await view.rerender({ open: false });
            expect(capturedSignal?.aborted).toBe(true);
            expect(appendCalls).toBe(0);
        } finally {
            await view.dispose();
        }
    });

    test("retains exact baseline and selections on network/busy failure for idempotent retry without re-previewing", async () => {
        const baselineV1 = makeBaseline(["shot-1"], 7);
        const baselineV2 = makeBaseline(["shot-1"], 99);
        let previewCount = 0;
        const appendRequests: BindingAppendRequest[] = [];
        let shouldFailAppend = true;

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                previewCount++;
                const activeBaseline = previewCount === 1 ? baselineV1 : baselineV2;
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            baseline: activeBaseline,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    existingReferences: [],
                                    existingVersionCount: 0,
                                    remainingCapacity: 6,
                                    issues: [],
                                    candidates: [
                                        {
                                            assetId: "asset-sangruo",
                                            category: "character",
                                            name: "桑若",
                                            assetVersionId: "ver-sangruo-1",
                                            bindable: true,
                                            missingImage: false,
                                            defaultSelected: true,
                                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 2, text: "桑若" }],
                                            issues: [],
                                        },
                                    ],
                                },
                            ],
                        } satisfies BindingPreview,
                    },
                };
            }
            if (config.url?.endsWith("/append")) {
                appendRequests.push(JSON.parse(config.data));
                if (shouldFailAppend) {
                    throw new Error("Network disconnected after server commit");
                }
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 8,
                            unchanged: true,
                            addedCount: 0,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    addedCount: 0,
                                    addedVersionIds: [],
                                    unchanged: true,
                                    references: [{ referenceId: "ref-1", assetVersionId: "ver-sangruo-1", role: "reference", status: "linked" }],
                                    artifacts: [],
                                },
                            ],
                        } satisfies BindingAppendResult,
                    },
                };
            }
            throw new Error("unexpected request");
        };

        let appliedResult: BindingAppendResult | undefined;
        const view = await mountDrawer({
            shotIds: ["shot-1"],
            onApplied: (res) => {
                appliedResult = res;
            },
        });
        try {
            expect(previewCount).toBe(1);
            const confirmBtn = findButton("确认追加绑定")!;
            await act(async () => {
                confirmBtn.click();
                await settle();
            });

            // First append failed due to network error; preview must NOT be re-run automatically
            expect(appendRequests).toHaveLength(1);
            expect(previewCount).toBe(1);
            expect(document.body.textContent).toContain("重试");

            // Selection checkbox is locked while pending retry payload is held
            const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-sangruo"]')!;
            expect(checkbox.disabled).toBe(true);

            // Retry replays the exact original baselineV1 and selections
            shouldFailAppend = false;
            const retryBtn = findButton("重试确认")!;
            expect(retryBtn).toBeTruthy();
            await act(async () => {
                retryBtn.click();
                await settle();
            });

            expect(previewCount).toBe(1);
            expect(appendRequests).toHaveLength(2);
            expect(appendRequests[1]).toEqual(appendRequests[0]);
            expect(appendRequests[1]?.baseline.projectRevision).toBe(7);
            expect(appliedResult?.projectRevision).toBe(8);
        } finally {
            await view.dispose();
        }
    });

    test("marks preview as stale on 409 conflict, blocks re-confirming old baseline, and refreshes preview on explicit user action without auto-replaying", async () => {
        const baselineV1 = makeBaseline(["shot-1"], 3);
        const baselineV2 = makeBaseline(["shot-1"], 4);
        let previewCount = 0;
        const appendRequests: BindingAppendRequest[] = [];

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                previewCount++;
                const isSecond = previewCount >= 2;
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            baseline: isSecond ? baselineV2 : baselineV1,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    existingReferences: [],
                                    existingVersionCount: 0,
                                    remainingCapacity: 6,
                                    issues: [],
                                    candidates: [
                                        {
                                            assetId: "asset-sangruo",
                                            category: "character",
                                            name: isSecond ? "桑若（新基准）" : "桑若",
                                            assetVersionId: isSecond ? "ver-sangruo-2" : "ver-sangruo-1",
                                            bindable: true,
                                            missingImage: false,
                                            defaultSelected: true,
                                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 2, text: "桑若" }],
                                            issues: [],
                                        },
                                    ],
                                },
                            ],
                        } satisfies BindingPreview,
                    },
                };
            }
            if (config.url?.endsWith("/append")) {
                appendRequests.push(JSON.parse(config.data));
                return {
                    config,
                    status: 409,
                    statusText: "Conflict",
                    headers: {},
                    data: {
                        code: STORYBOARD_ASSET_BINDING_CODES.conflict,
                        reason: STORYBOARD_ASSET_BINDING_REASONS.conflict,
                        msg: "conflict",
                    },
                };
            }
            throw new Error("unexpected request");
        };

        const view = await mountDrawer({ shotIds: ["shot-1"] });
        try {
            expect(previewCount).toBe(1);
            const confirmBtn = findButton("确认追加绑定")!;
            await act(async () => {
                confirmBtn.click();
                await settle();
            });

            expect(appendRequests).toHaveLength(1);
            expect(previewCount).toBe(1);
            expect(document.body.textContent).toContain("分镜或资产基准已变化");
            // Old preview cannot be confirmed again
            expect(findButton("确认追加绑定")?.disabled).toBe(true);

            // User clicks "重新预览" -> fetches fresh preview and does NOT auto-submit append
            const refreshBtn = findButton("重新预览")!;
            expect(refreshBtn).toBeTruthy();
            await act(async () => {
                refreshBtn.click();
                await settle();
            });

            expect(previewCount).toBe(2);
            expect(appendRequests).toHaveLength(1);
            expect(document.body.textContent).toContain("桑若（新基准）");
            expect(findButton("确认追加绑定")?.disabled).toBe(false);
        } finally {
            await view.dispose();
        }
    });

    test("real WorkflowProductionWorkbench exposes 匹配镜头资产 button, opens drawer, and refreshes workspace after append", async () => {
        const shot: ProjectShot = {
            id: "shot-1",
            projectId: "proj-1",
            unitId: "unit-1",
            title: "镜头 1",
            description: "桑若走入栖云阁",
            position: 0,
            durationMs: 3000,
            status: "ready",
            createdAt: "2026-10-26T00:00:00Z",
            updatedAt: "2026-10-26T00:00:00Z",
        };
        const detail: ProjectDetail = {
            project: {
                id: "proj-1",
                userId: "user-1",
                name: "测试项目",
                type: "short_drama",
                aspectRatio: "16:9",
                sourceType: "novel",
                description: "",
                stylePresetId: "default",
                status: "active",
                revision: 3,
                createdAt: "",
                updatedAt: "",
            },
            units: [
                {
                    id: "unit-1",
                    projectId: "proj-1",
                    kind: "chapter",
                    title: "第一章",
                    sourceText: "桑若走入栖云阁",
                    wordCount: 7,
                    status: "ready",
                    position: 0,
                    createdAt: "",
                    updatedAt: "",
                },
            ],
            canvases: [],
            canvasUnitLinks: [],
            assets: [],
            assetFolders: [],
            workflows: [],
            shots: [shot],
            shotRevisions: [],
            shotArtifacts: [],
            shotReferences: [],
            assetCandidates: [],
            tasks: [],
        };

        let previewCalled = false;
        let appendCalled = false;
        let refreshCalled = 0;

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/shot-asset-bindings/preview")) {
                previewCalled = true;
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            baseline: makeBaseline(["shot-1"], 3),
                            shots: [
                                {
                                    shotId: "shot-1",
                                    existingReferences: [],
                                    existingVersionCount: 0,
                                    remainingCapacity: 6,
                                    issues: [],
                                    candidates: [
                                        {
                                            assetId: "asset-sangruo",
                                            category: "character",
                                            name: "桑若",
                                            assetVersionId: "ver-sangruo-1",
                                            bindable: true,
                                            missingImage: false,
                                            defaultSelected: true,
                                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 2, text: "桑若" }],
                                            issues: [],
                                        },
                                    ],
                                },
                            ],
                        } satisfies BindingPreview,
                    },
                };
            }
            if (config.url?.endsWith("/shot-asset-bindings/append")) {
                appendCalled = true;
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 4,
                            unchanged: false,
                            addedCount: 1,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    addedCount: 1,
                                    addedVersionIds: ["ver-sangruo-1"],
                                    unchanged: false,
                                    references: [{ referenceId: "ref-new-1", assetVersionId: "ver-sangruo-1", role: "reference", status: "linked" }],
                                    artifacts: [],
                                },
                            ],
                        } satisfies BindingAppendResult,
                    },
                };
            }
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: { code: 0, data: { assets: [], total: 0, categoryCounts: {} }, msg: "ok" },
            };
        };

        const host = document.createElement("div");
        document.body.append(host);
        const root = createRoot(host);
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        try {
            await act(async () => {
                root.render(
                    <MemoryRouter initialEntries={["/projects/proj-1/chapters/unit-1"]}>
                        <ConfigProvider theme={{ token: { motion: false } }}>
                            <App>
                                <QueryClientProvider client={client}>
                                    <WorkflowProductionWorkbench
                                        activeStage="storyboard"
                                        detail={detail}
                                        projectId="proj-1"
                                        unitId="unit-1"
                                        selectedShot={shot}
                                        onSelectShot={() => {}}
                                        onRefresh={async () => {
                                            refreshCalled++;
                                        }}
                                        onAddShot={() => {}}
                                        addingShot={false}
                                    />
                                </QueryClientProvider>
                            </App>
                        </ConfigProvider>
                    </MemoryRouter>,
                );
            });
            await act(async () => {
                await settle();
            });

            const matchBtn = findButton("匹配镜头资产")!;
            expect(matchBtn).toBeTruthy();

            await act(async () => {
                matchBtn.click();
                await settle();
            });
            expect(previewCalled).toBe(true);
            expect(document.body.textContent).toContain("仅实增镜头的相关产物将过期并取消选中，历史内容保留");

            const confirmBtn = findButton("确认追加绑定")!;
            expect(confirmBtn).toBeTruthy();
            await act(async () => {
                confirmBtn.click();
                await settle();
            });

            expect(appendCalled).toBe(true);
            expect(refreshCalled).toBeGreaterThanOrEqual(1);
        } finally {
            await act(async () => root.unmount());
            client.clear();
            host.remove();
        }
    });

    test("handles backend capacity_exceeded with defaultSelected=false, 503 busy retry, empty shotIds, and user scope isolation", async () => {
        const existingFive = [1, 2, 3, 4, 5].map((n) => ({
            referenceId: `ref-${n}`,
            assetVersionId: `ver-exist-${n}`,
            role: "reference",
            status: "linked",
        }));
        let previewCalls = 0;
        let appendCalls = 0;
        let failBusyOnce = true;

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                previewCalls++;
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            baseline: makeBaseline(["shot-1"], 10 + previewCalls),
                            shots: [
                                {
                                    shotId: "shot-1",
                                    existingReferences: existingFive,
                                    existingVersionCount: 5,
                                    remainingCapacity: 1,
                                    issues: [
                                        {
                                            kind: "capacity_exceeded",
                                            message: "已有版本与明确建议超过 6 个，请手动选择",
                                        },
                                    ],
                                    candidates: [
                                        {
                                            assetId: "asset-cap-1",
                                            category: "character",
                                            name: "候选角色一",
                                            assetVersionId: "ver-cap-1",
                                            bindable: true,
                                            missingImage: false,
                                            defaultSelected: false,
                                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 4, text: "候选角色一" }],
                                            issues: [{ kind: "capacity_exceeded", message: "镜头资产容量不足，请手动勾选" }],
                                        },
                                        {
                                            assetId: "asset-cap-2",
                                            category: "prop",
                                            name: "候选道具二",
                                            assetVersionId: "ver-cap-2",
                                            bindable: true,
                                            missingImage: false,
                                            defaultSelected: false,
                                            evidence: [{ kind: "structured_id", field: "projectAssetRefs", start: 0, end: 0, text: "" }],
                                            issues: [{ kind: "capacity_exceeded", message: "镜头资产容量不足，请手动勾选" }],
                                        },
                                    ],
                                },
                            ],
                        } satisfies BindingPreview,
                    },
                };
            }
            if (config.url?.endsWith("/append")) {
                appendCalls++;
                if (failBusyOnce) {
                    failBusyOnce = false;
                    return {
                        config,
                        status: 503,
                        statusText: "Service Unavailable",
                        headers: {},
                        data: {
                            code: STORYBOARD_ASSET_BINDING_CODES.busy,
                            reason: STORYBOARD_ASSET_BINDING_REASONS.busy,
                            msg: "busy",
                        },
                    };
                }
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 12,
                            unchanged: false,
                            addedCount: 1,
                            shots: [
                                {
                                    shotId: "shot-1",
                                    addedCount: 1,
                                    addedVersionIds: ["ver-cap-1"],
                                    unchanged: false,
                                    references: [...existingFive, { referenceId: "ref-6", assetVersionId: "ver-cap-1", role: "reference", status: "linked" }],
                                    artifacts: [],
                                },
                            ],
                        } satisfies BindingAppendResult,
                    },
                };
            }
            throw new Error("unexpected request");
        };

        // 1. Empty shotIds makes zero preview calls and disables confirm
        const view = await mountDrawer({ shotIds: [] });
        try {
            expect(previewCalls).toBe(0);
            expect(document.body.textContent).toContain("当前没有可匹配的分镜镜头");
            expect(findButton("确认追加绑定")?.disabled).toBe(true);

            // 2. Rerender with shot-1 -> backend returns 5 existing + 2 capacity_exceeded candidates (defaultSelected=false)
            await view.rerender({ shotIds: ["shot-1"] });
            expect(previewCalls).toBe(1);
            const text = document.body.textContent || "";
            expect(text).toContain("总量 7");
            expect(text).toContain("剩余容量 1");
            expect(text).toContain("结构化依据（无文本摘录）");
            // Confirm is blocked until user manually selects <= 1 or clicks "本镜不新增"
            expect(findButton("确认追加绑定")?.disabled).toBe(true);

            // Select 1 candidate -> fits remainingCapacity (1), unblocks confirm
            const firstCapCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-cap-1"]')!;
            await act(async () => {
                firstCapCheckbox.click();
                await settle();
            });
            expect(findButton("确认追加绑定")?.disabled).toBe(false);

            // 3. Submit append -> receives 503 busy -> retains baseline & selections for retry
            await act(async () => {
                findButton("确认追加绑定")!.click();
                await settle();
            });
            expect(appendCalls).toBe(1);
            expect(previewCalls).toBe(1);
            expect(document.body.textContent).toContain("资产绑定繁忙，请使用原请求重试");

            // Retry succeeds with same baseline
            await act(async () => {
                findButton("重试确认")!.click();
                await settle();
            });
            expect(appendCalls).toBe(2);
            expect(previewCalls).toBe(1);

            // 4. Switching user scope isolates preview cache and reloads fresh preview for new user
            await act(async () => {
                useUserStore.setState({ user: { id: "user-2" } as NonNullable<typeof originalUser> });
                await settle();
            });
            expect(previewCalls).toBe(2);
        } finally {
            await view.dispose();
        }
    });

    test("completes full multi-shot legacy chapter E2E flow: 3-shot preview, cancel zero-write, 5+2 capacity reduction, disconnect idempotent retry, 409 conflict refresh, and account switch isolation", async () => {
        const shot1ExistingFive = [1, 2, 3, 4, 5].map((n) => ({
            referenceId: `ref-s1-${n}`,
            assetVersionId: `ver-s1-exist-${n}`,
            role: "reference",
            status: "linked",
        }));
        const shot2MultiRoleExisting = [
            {
                referenceId: "ref-s2-role-char",
                assetVersionId: "ver-s2-same",
                role: "character",
                status: "confirmed",
            },
            {
                referenceId: "ref-s2-role-ref",
                assetVersionId: "ver-s2-same",
                role: "reference",
                status: "linked",
            },
        ];

        const makeMultiShotPreview = (projectRevision: number, candidateSuffix = ""): BindingPreview => ({
            baseline: makeBaseline(["shot-1", "shot-2", "shot-3"], projectRevision),
            shots: [
                {
                    shotId: "shot-1",
                    existingReferences: shot1ExistingFive,
                    existingVersionCount: 5,
                    remainingCapacity: 1,
                    issues: [
                        {
                            kind: "capacity_exceeded",
                            message: "已有版本与明确建议超过 6 个，请手动选择",
                        },
                    ],
                    candidates: [
                        {
                            assetId: "asset-s1-new-1",
                            category: "character",
                            name: `裴让${candidateSuffix}`,
                            assetVersionId: "ver-peirang-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: true,
                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 5, text: "角色：裴让" }],
                            issues: [],
                        },
                        {
                            assetId: "asset-s1-new-2",
                            category: "environment",
                            name: `栖云阁${candidateSuffix}`,
                            assetVersionId: "ver-qiyunge-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: true,
                            evidence: [{ kind: "visual_label", field: "plotDescription", start: 6, end: 12, text: "场景：栖云阁" }],
                            issues: [],
                        },
                    ],
                },
                {
                    shotId: "shot-2",
                    existingReferences: shot2MultiRoleExisting,
                    existingVersionCount: 1,
                    remainingCapacity: 5,
                    issues: [],
                    candidates: [],
                },
                {
                    shotId: "shot-3",
                    existingReferences: [],
                    existingVersionCount: 0,
                    remainingCapacity: 6,
                    issues: [],
                    candidates: [
                        {
                            assetId: "asset-s3-mention",
                            category: "character",
                            name: "桑若",
                            assetVersionId: "ver-sangruo-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [{ kind: "mention", field: "plotDescription", start: 0, end: 6, text: "桑若视线落在" }],
                            issues: [],
                        },
                        {
                            assetId: "asset-s3-speaker",
                            category: "character",
                            name: "沈知意",
                            assetVersionId: "ver-shen-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [{ kind: "speaker", field: "dialogue", start: 0, end: 3, text: "沈知意" }],
                            issues: [],
                        },
                    ],
                },
            ],
        });

        let previewCallCount = 0;
        const appendPayloads: BindingAppendRequest[] = [];
        let appendPhase: "disconnect_once" | "idempotent_ok" | "conflict_409" = "disconnect_once";

        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/shot-asset-bindings/preview")) {
                previewCallCount++;
                const rev = previewCallCount >= 3 ? 25 : 11;
                const suffix = previewCallCount >= 3 ? "（刷新基线）" : "";
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: { code: 0, msg: "ok", data: makeMultiShotPreview(rev, suffix) },
                };
            }
            if (config.url?.endsWith("/shot-asset-bindings/append")) {
                appendPayloads.push(JSON.parse(config.data));
                if (appendPhase === "disconnect_once") {
                    appendPhase = "idempotent_ok";
                    throw new Error("Network disconnected after server commit");
                }
                if (appendPhase === "idempotent_ok") {
                    return {
                        config,
                        status: 200,
                        statusText: "OK",
                        headers: {},
                        data: {
                            code: 0,
                            msg: "ok",
                            data: {
                                projectRevision: 12,
                                unchanged: true,
                                addedCount: 0,
                                shots: [
                                    {
                                        shotId: "shot-1",
                                        addedCount: 0,
                                        addedVersionIds: [],
                                        unchanged: true,
                                        references: [
                                            ...shot1ExistingFive,
                                            { referenceId: "ref-s1-6", assetVersionId: "ver-peirang-1", role: "reference", status: "linked" },
                                        ],
                                        artifacts: [{ artifactId: "art-shot-1", contentFingerprint: "cf-shot-1", status: "stale", selected: false }],
                                    },
                                    {
                                        shotId: "shot-2",
                                        addedCount: 0,
                                        addedVersionIds: [],
                                        unchanged: true,
                                        references: shot2MultiRoleExisting,
                                        artifacts: [{ artifactId: "art-shot-2", contentFingerprint: "cf-shot-2", status: "ready", selected: true }],
                                    },
                                    {
                                        shotId: "shot-3",
                                        addedCount: 0,
                                        addedVersionIds: [],
                                        unchanged: true,
                                        references: [],
                                        artifacts: [{ artifactId: "art-shot-3", contentFingerprint: "cf-shot-3", status: "ready", selected: true }],
                                    },
                                ],
                            } satisfies BindingAppendResult,
                        },
                    };
                }
                return {
                    config,
                    status: 409,
                    statusText: "Conflict",
                    headers: {},
                    data: {
                        code: STORYBOARD_ASSET_BINDING_CODES.conflict,
                        reason: STORYBOARD_ASSET_BINDING_REASONS.conflict,
                        msg: "conflict",
                    },
                };
            }
            throw new Error("unexpected request");
        };

        let closeCount = 0;
        const appliedResults: BindingAppendResult[] = [];
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        const initialWorkspaceCache = {
            project: { id: "proj-1", revision: 11 },
            shots: [{ id: "shot-1" }, { id: "shot-2" }, { id: "shot-3" }],
            shotReferences: [...shot1ExistingFive, ...shot2MultiRoleExisting],
            shotArtifacts: [
                { id: "art-shot-1", shotId: "shot-1", status: "ready", selected: true },
                { id: "art-shot-2", shotId: "shot-2", status: "ready", selected: true },
                { id: "art-shot-3", shotId: "shot-3", status: "ready", selected: true },
            ],
        };
        client.setQueryData(["project", "proj-1"], initialWorkspaceCache);
        client.setQueryData(["project", "proj-1", "unit-workspace", "unit-1"], initialWorkspaceCache);

        const view = await mountDrawer({
            client,
            shotIds: ["shot-1", "shot-2", "shot-3"],
            onClose: () => {
                closeCount++;
            },
            onApplied: (res) => {
                appliedResults.push(res);
                drawerModule.mergeBindingAppendResultIntoProjectCache(client, "proj-1", "unit-1", res);
            },
        });
        try {
            // 1. 3-shot chapter preview + Cancel zero-write verification
            expect(previewCallCount).toBe(1);
            const initialText = document.body.textContent || "";
            expect(initialText).toContain("总量 7");
            expect(initialText).toContain("剩余容量 1");
            expect(initialText).toContain("ref-s2-role-char");
            expect(initialText).toContain("ref-s2-role-ref");

            const s3MentionCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-s3-mention"]')!;
            const s3SpeakerCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-s3-speaker"]')!;
            expect(s3MentionCheckbox.checked).toBe(false);
            expect(s3SpeakerCheckbox.checked).toBe(false);

            const cancelBtn = findButton("取消")!;
            expect(cancelBtn).toBeTruthy();
            await act(async () => {
                cancelBtn.click();
                await settle();
            });
            expect(closeCount).toBe(1);
            expect(appendPayloads).toHaveLength(0);

            // 2. 5 + 2 capacity reduction on Shot 1: uncheck asset-s1-new-2 so 5 + 1 = 6 <= 6
            const confirmBtn = findButton("确认追加绑定")!;
            expect(confirmBtn.disabled).toBe(true);
            const s1SecondCheckbox = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-s1-new-2"]')!;
            await act(async () => {
                s1SecondCheckbox.click();
                await settle();
            });
            expect(confirmBtn.disabled).toBe(false);

            // 3. Disconnect idempotent retry: first /append throws network disconnect -> "重试确认" replays identical baseline & selections -> unchanged=true -> cache merged
            await act(async () => {
                confirmBtn.click();
                await settle();
            });
            expect(appendPayloads).toHaveLength(1);
            expect(previewCallCount).toBe(1);
            expect(document.body.textContent).toContain("重试");

            const retryBtn = findButton("重试确认")!;
            expect(retryBtn).toBeTruthy();
            await act(async () => {
                retryBtn.click();
                await settle();
            });
            expect(previewCallCount).toBe(1);
            expect(appendPayloads).toHaveLength(2);
            expect(appendPayloads[1]).toEqual(appendPayloads[0]);
            expect(appendPayloads[1]?.selections).toEqual([
                { shotId: "shot-1", assetVersionIds: ["ver-peirang-1"] },
                { shotId: "shot-2", assetVersionIds: [] },
                { shotId: "shot-3", assetVersionIds: [] },
            ]);
            expect(appliedResults).toHaveLength(1);
            expect(appliedResults[0]?.unchanged).toBe(true);
            expect(appliedResults[0]?.projectRevision).toBe(12);

            const cachedAfterReplay = client.getQueryData<any>(["project", "proj-1"]);
            expect(cachedAfterReplay?.project?.revision).toBe(12);
            const cachedUnitWorkspace = client.getQueryData<any>(["project", "proj-1", "unit-workspace", "unit-1"]);
            expect(cachedUnitWorkspace?.project?.revision).toBe(12);

            // 4. Re-open drawer, reduce capacity, encounter 409 conflict -> "重新预览" fetches new baseline without auto-replaying old selections
            appendPhase = "conflict_409";
            await view.rerender({ open: false });
            await view.rerender({ open: true });
            expect(previewCallCount).toBe(2);

            const s1SecondCheckboxReopen = document.querySelector<HTMLInputElement>('input[type="checkbox"][data-candidate-asset-id="asset-s1-new-2"]')!;
            await act(async () => {
                s1SecondCheckboxReopen.click();
                await settle();
            });
            await act(async () => {
                findButton("确认追加绑定")!.click();
                await settle();
            });
            expect(appendPayloads).toHaveLength(3);
            expect(document.body.textContent).toContain("分镜或资产基准已变化");
            expect(findButton("确认追加绑定")?.disabled).toBe(true);

            const refreshBtn = findButton("重新预览")!;
            expect(refreshBtn).toBeTruthy();
            await act(async () => {
                refreshBtn.click();
                await settle();
            });
            expect(previewCallCount).toBe(3);
            expect(appendPayloads).toHaveLength(3);
            expect(document.body.textContent).toContain("裴让（刷新基线）");

            // 5. Account switch isolation: switching useUserStore clears previous user's preview and reloads for new user scope
            await act(async () => {
                useUserStore.setState({ user: { id: "user-2" } as NonNullable<typeof originalUser> });
                await settle();
            });
            expect(previewCallCount).toBe(4);
            expect(appendPayloads).toHaveLength(3);
        } finally {
            await view.dispose();
        }
    });

    test("deduplicates same-asset candidates within a shot, supports per-shot/global select-all and one-click confirm, displays human-readable shotTitles, and optimistically attaches matched asset in cache", async () => {
        expect(typeof drawerModule.deduplicateShotCandidates).toBe("function");
        expect(typeof drawerModule.computeAutoSelectableVersionIds).toBe("function");

        const duplicateCandidates = [
            {
                assetId: "asset-sangruo",
                category: "character",
                name: "桑若",
                assetVersionId: "ver-sangruo-1",
                bindable: true,
                missingImage: true,
                defaultSelected: false,
                evidence: [
                    { kind: "mention", field: "videoPrompt", start: 4, end: 11, text: "镜头聚焦在桑若" },
                    { kind: "mention", field: "videoPrompt", start: 4, end: 11, text: "镜头聚焦在桑若" },
                ],
                issues: [{ kind: "missing_image", message: "当前版本无可用参考图" }],
            },
            {
                assetId: "asset-sangruo",
                category: "character",
                name: "桑若",
                assetVersionId: "ver-sangruo-1",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    { kind: "mention", field: "plotDescription", start: 0, end: 5, text: "桑若躺在榻上" },
                ],
                issues: [{ kind: "missing_image", message: "当前版本无可用参考图" }],
            },
            {
                assetId: "asset-sangruo",
                category: "character",
                name: "桑若",
                assetVersionId: "ver-sangruo-1",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    { kind: "mention", field: "action", start: 0, end: 5, text: "桑若呼吸微弱" },
                ],
                issues: [],
            },
        ];

        const deduped = drawerModule.deduplicateShotCandidates(duplicateCandidates);
        expect(deduped).toHaveLength(1);
        expect(deduped[0]).toMatchObject({
            assetId: "asset-sangruo",
            assetVersionId: "ver-sangruo-1",
            bindable: true,
            missingImage: false,
            defaultSelected: false,
        });
        expect(deduped[0].evidence).toHaveLength(3);
        expect(deduped[0].issues).toHaveLength(1);

        const previewData: BindingPreview = {
            baseline: makeBaseline(["shot-6", "shot-7"], 5),
            shots: [
                {
                    shotId: "shot-6",
                    existingReferences: [],
                    existingVersionCount: 0,
                    remainingCapacity: 6,
                    issues: [],
                    candidates: [
                        ...duplicateCandidates,
                        {
                            assetId: "asset-warn",
                            category: "prop",
                            name: "药碗",
                            assetVersionId: "ver-bowl-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [{ kind: "mention", field: "plotDescription", start: 0, end: 2, text: "药碗" }],
                            issues: [{ kind: "ambiguous", message: "存在同名候选，请人工确认" }],
                        },
                    ],
                },
                {
                    shotId: "shot-7",
                    existingReferences: [],
                    existingVersionCount: 0,
                    remainingCapacity: 6,
                    issues: [],
                    candidates: [
                        {
                            assetId: "asset-peirang",
                            category: "character",
                            name: "裴让",
                            assetVersionId: "ver-peirang-1",
                            bindable: true,
                            missingImage: false,
                            defaultSelected: false,
                            evidence: [{ kind: "speaker", field: "dialogue", start: 0, end: 2, text: "裴让" }],
                            issues: [],
                        },
                    ],
                },
            ],
        };

        // computeAutoSelectableVersionIds skips ambiguous and deduplicates same assetVersionId
        expect(drawerModule.computeAutoSelectableVersionIds(previewData.shots[0])).toEqual(["ver-sangruo-1"]);
        expect(drawerModule.computeAutoSelectableVersionIds(previewData.shots[1])).toEqual(["ver-peirang-1"]);

        const appendPayloads: BindingAppendRequest[] = [];
        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: { code: 0, data: previewData, msg: "ok" },
                };
            }
            if (config.url?.endsWith("/append")) {
                appendPayloads.push(JSON.parse(config.data));
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 6,
                            unchanged: false,
                            addedCount: 2,
                            shots: [
                                {
                                    shotId: "shot-6",
                                    addedCount: 1,
                                    addedVersionIds: ["ver-sangruo-1"],
                                    unchanged: false,
                                    references: [
                                        {
                                            referenceId: "ref-s6-1",
                                            assetVersionId: "ver-sangruo-1",
                                            role: "reference",
                                            status: "linked",
                                        },
                                    ],
                                    artifacts: [],
                                },
                                {
                                    shotId: "shot-7",
                                    addedCount: 1,
                                    addedVersionIds: ["ver-peirang-1"],
                                    unchanged: false,
                                    references: [
                                        {
                                            referenceId: "ref-s7-1",
                                            assetVersionId: "ver-peirang-1",
                                            role: "reference",
                                            status: "linked",
                                        },
                                    ],
                                    artifacts: [],
                                },
                            ],
                        } satisfies BindingAppendResult,
                    },
                };
            }
            throw new Error("unexpected request");
        };

        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        client.setQueryData(["project", "proj-1"], {
            project: { id: "proj-1", revision: 5 },
            assets: [
                { id: "asset-sangruo", title: "桑若", category: "character", primaryVersionId: "ver-sangruo-1" },
                { id: "asset-peirang", title: "裴让", category: "character", primaryVersionId: "ver-peirang-1" },
            ],
            shotReferences: [],
            shotArtifacts: [],
        });

        const host = document.createElement("div");
        document.body.append(host);
        const root = createRoot(host);
        const DrawerComponent = drawerModule.ChapterAssetBindingDrawer;
        try {
            await act(async () => {
                root.render(
                    <ConfigProvider theme={{ token: { motion: false } }}>
                        <App>
                            <QueryClientProvider client={client}>
                                <DrawerComponent
                                    projectId="proj-1"
                                    unitId="unit-1"
                                    shotIds={["shot-6", "shot-7"]}
                                    shotTitles={{ "shot-6": "镜头06", "shot-7": "镜头07" }}
                                    open={true}
                                    onClose={() => {}}
                                    onApplied={(res: BindingAppendResult) => {
                                        drawerModule.mergeBindingAppendResultIntoProjectCache(client, "proj-1", "unit-1", res);
                                    }}
                                />
                            </QueryClientProvider>
                        </App>
                    </ConfigProvider>,
                );
            });
            await act(async () => {
                await settle();
            });

            // Only 1 checkbox rendered for asset-sangruo in shot-6 (deduplicated from 3 occurrences)
            const sangruoBoxes = document.querySelectorAll<HTMLInputElement>(
                'input[type="checkbox"][data-shot-id="shot-6"][data-candidate-asset-id="asset-sangruo"]',
            );
            expect(sangruoBoxes).toHaveLength(1);
            expect(sangruoBoxes[0]!.checked).toBe(false);

            // Human-readable shot title and raw shotId are both displayed
            const bodyText = document.body.textContent || "";
            expect(bodyText).toContain("镜头06");
            expect(bodyText).toContain("shot-6");
            expect(bodyText).toContain("共发现 2 项可新增绑定候选 · 当前已勾选 0 项");

            // Per-shot select all button works
            const shot6SelectBtn = document.querySelector<HTMLButtonElement>('button[data-shot-select-all="shot-6"]')!;
            expect(shot6SelectBtn).toBeTruthy();
            await act(async () => {
                shot6SelectBtn.click();
                await settle();
            });
            expect(sangruoBoxes[0]!.checked).toBe(true);
            expect(document.body.textContent).toContain("共发现 2 项可新增绑定候选 · 当前已勾选 1 项");

            // Global select all bindable button selects remaining auto-selectable candidates
            const selectAllBtn = findButton("一键全选可绑定项")!;
            expect(selectAllBtn).toBeTruthy();
            await act(async () => {
                selectAllBtn.click();
                await settle();
            });
            expect(document.body.textContent).toContain("共发现 2 项可新增绑定候选 · 当前已勾选 2 项");

            // Clear all selections button resets to 0
            const clearAllBtn = findButton("清空新增勾选")!;
            expect(clearAllBtn).toBeTruthy();
            await act(async () => {
                clearAllBtn.click();
                await settle();
            });
            expect(document.body.textContent).toContain("共发现 2 项可新增绑定候选 · 当前已勾选 0 项");

            // Footer one-click "全选并确认绑定（2 项）" selects all auto-selectable and submits append immediately
            const quickConfirmBtn = findButton("全选并确认绑定（2 项）")!;
            expect(quickConfirmBtn).toBeTruthy();
            await act(async () => {
                quickConfirmBtn.click();
                await settle();
            });

            expect(appendPayloads).toHaveLength(1);
            expect(appendPayloads[0]?.selections).toEqual([
                { shotId: "shot-6", assetVersionIds: ["ver-sangruo-1"] },
                { shotId: "shot-7", assetVersionIds: ["ver-peirang-1"] },
            ]);

            // Optimistic cache update resolves asset from current.assets by primaryVersionId
            const updatedProject = client.getQueryData<any>(["project", "proj-1"]);
            expect(updatedProject?.shotReferences).toHaveLength(2);
            expect(updatedProject?.shotReferences[0]?.asset?.title).toBe("桑若");
            expect(updatedProject?.shotReferences[1]?.asset?.title).toBe("裴让");
        } finally {
            await act(async () => root.unmount());
            client.clear();
            host.remove();
        }
    });

    test("reproduces 镜头10 false-positive context_warning ('不夸张微笑'): clears context_warning when clean occurrence exists, includes single-occurrence context_warning in explicit select-all, prioritizes tiers within capacity, and binds all 3 assets on one-click confirm", async () => {
        const shot10Candidates = [
            {
                assetId: "asset-prop-1",
                category: "prop" as const,
                name: "青布平安符",
                assetVersionId: "ver-prop-1",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    {
                        kind: "mention" as const,
                        field: "plotDescription",
                        start: 0,
                        end: 5,
                        text: "青布平安符",
                    },
                ],
                issues: [],
            },
            {
                assetId: "asset-char-sangruo",
                category: "character" as const,
                name: "桑若",
                assetVersionId: "ver-char-sangruo",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    {
                        kind: "mention" as const,
                        field: "action",
                        start: 0,
                        end: 2,
                        text: "桑若",
                    },
                ],
                issues: [],
            },
            {
                assetId: "asset-char-sangruo",
                category: "character" as const,
                name: "桑若",
                assetVersionId: "ver-char-sangruo",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    {
                        kind: "speaker" as const,
                        field: "dialogue",
                        start: 0,
                        end: 2,
                        text: "桑若",
                    },
                ],
                issues: [],
            },
            {
                assetId: "asset-char-sangruo",
                category: "character" as const,
                name: "桑若",
                assetVersionId: "ver-char-sangruo",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    {
                        kind: "mention" as const,
                        field: "videoPrompt",
                        start: 0,
                        end: 2,
                        text: "桑若以平淡语气说“路过顺手。剑庐值夜风大，贴着暖和些”，不夸张微笑，裴让接过衣物，结尾两人短暂沉默。",
                    },
                ],
                issues: [
                    {
                        kind: "context_warning" as const,
                        message: "包含否定、回忆或非画内语境，请核对原文",
                    },
                ],
            },
            {
                assetId: "asset-char-peirang",
                category: "character" as const,
                name: "裴让",
                assetVersionId: "ver-char-peirang",
                bindable: true,
                missingImage: false,
                defaultSelected: false,
                evidence: [
                    {
                        kind: "mention" as const,
                        field: "videoPrompt",
                        start: 34,
                        end: 36,
                        text: "桑若以平淡语气说“路过顺手。剑庐值夜风大，贴着暖和些”，不夸张微笑，裴让接过衣物，结尾两人短暂沉默。",
                    },
                ],
                issues: [
                    {
                        kind: "context_warning" as const,
                        message: "包含否定、回忆或非画内语境，请核对原文",
                    },
                ],
            },
        ];

        // (a) deduplicateShotCandidates clears context_warning for 桑若 (since action & dialogue are clean),
        // while 裴让 (only in videoPrompt with context_warning) retains context_warning for UI badge display.
        const deduped = drawerModule.deduplicateShotCandidates(shot10Candidates);
        expect(deduped).toHaveLength(3);
        const sangruoDeduped = deduped.find((c: any) => c.assetId === "asset-char-sangruo")!;
        const peirangDeduped = deduped.find((c: any) => c.assetId === "asset-char-peirang")!;
        expect(sangruoDeduped.issues).toEqual([]);
        expect(peirangDeduped.issues).toEqual([
            {
                kind: "context_warning",
                message: "包含否定、回忆或非画内语境，请核对原文",
            },
        ]);

        // (b) computeAutoSelectableVersionIds includes ALL THREE (青布平安符, 桑若, 裴让)
        const shot10Preview: BindingPreview["shots"][number] = {
            shotId: "shot-10",
            existingReferences: [],
            existingVersionCount: 0,
            remainingCapacity: 6,
            issues: [],
            candidates: shot10Candidates,
        };
        expect(drawerModule.computeAutoSelectableVersionIds(shot10Preview)).toEqual([
            "ver-prop-1",
            "ver-char-sangruo",
            "ver-char-peirang",
        ]);

        // (c) When total bindable candidates exceed remainingCapacity, computeAutoSelectableVersionIds
        // prioritizes (1) defaultSelected, (2) clean without context_warning, (3) context_warning,
        // and slices up to remainingCapacity instead of returning [].
        const crowdedShot: BindingPreview["shots"][number] = {
            shotId: "shot-crowded",
            existingReferences: [
                { referenceId: "ref-exist-1", assetVersionId: "ver-exist-1", role: "reference", status: "linked" },
                { referenceId: "ref-exist-2", assetVersionId: "ver-exist-2", role: "reference", status: "linked" },
                { referenceId: "ref-exist-3", assetVersionId: "ver-exist-3", role: "reference", status: "linked" },
            ],
            existingVersionCount: 3,
            remainingCapacity: 3,
            issues: [],
            candidates: [
                {
                    assetId: "asset-warn-1",
                    category: "character",
                    name: "语境角色A",
                    assetVersionId: "ver-warn-1",
                    bindable: true,
                    missingImage: false,
                    defaultSelected: false,
                    evidence: [{ kind: "mention", field: "videoPrompt", start: 0, end: 2, text: "不夸张" }],
                    issues: [{ kind: "context_warning", message: "包含否定、回忆或非画内语境，请核对原文" }],
                },
                {
                    assetId: "asset-clean-1",
                    category: "prop",
                    name: "干净道具B",
                    assetVersionId: "ver-clean-1",
                    bindable: true,
                    missingImage: false,
                    defaultSelected: false,
                    evidence: [{ kind: "mention", field: "action", start: 0, end: 2, text: "干净" }],
                    issues: [],
                },
                {
                    assetId: "asset-default-1",
                    category: "environment",
                    name: "默认场景C",
                    assetVersionId: "ver-default-1",
                    bindable: true,
                    missingImage: false,
                    defaultSelected: true,
                    evidence: [{ kind: "visual_label", field: "plotDescription", start: 0, end: 2, text: "场景" }],
                    issues: [],
                },
                {
                    assetId: "asset-warn-2",
                    category: "prop",
                    name: "语境道具D",
                    assetVersionId: "ver-warn-2",
                    bindable: true,
                    missingImage: false,
                    defaultSelected: false,
                    evidence: [{ kind: "mention", field: "videoPrompt", start: 0, end: 2, text: "无声" }],
                    issues: [{ kind: "context_warning", message: "包含否定、回忆或非画内语境，请核对原文" }],
                },
            ],
        };
        expect(drawerModule.computeAutoSelectableVersionIds(crowdedShot)).toEqual([
            "ver-default-1",
            "ver-clean-1",
            "ver-warn-1",
        ]);

        // (d) In the rendered ChapterAssetBindingDrawer, clicking "全选并确认绑定（3 项）" on 镜头10
        // submits all 3 version IDs: ["ver-prop-1", "ver-char-sangruo", "ver-char-peirang"].
        const previewData: BindingPreview = {
            baseline: makeBaseline(["shot-10"], 9),
            shots: [shot10Preview],
        };
        const appendPayloads: BindingAppendRequest[] = [];
        apiClient.defaults.adapter = async (config) => {
            if (config.url?.endsWith("/preview")) {
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: { code: 0, data: previewData, msg: "ok" },
                };
            }
            if (config.url?.endsWith("/append")) {
                appendPayloads.push(JSON.parse(config.data));
                return {
                    config,
                    status: 200,
                    statusText: "OK",
                    headers: {},
                    data: {
                        code: 0,
                        msg: "ok",
                        data: {
                            projectRevision: 10,
                            unchanged: false,
                            addedCount: 3,
                            shots: [
                                {
                                    shotId: "shot-10",
                                    addedCount: 3,
                                    addedVersionIds: ["ver-prop-1", "ver-char-sangruo", "ver-char-peirang"],
                                    unchanged: false,
                                    references: [
                                        { referenceId: "ref-10-1", assetVersionId: "ver-prop-1", role: "reference", status: "linked" },
                                        { referenceId: "ref-10-2", assetVersionId: "ver-char-sangruo", role: "reference", status: "linked" },
                                        { referenceId: "ref-10-3", assetVersionId: "ver-char-peirang", role: "reference", status: "linked" },
                                    ],
                                    artifacts: [],
                                },
                            ],
                        } satisfies BindingAppendResult,
                    },
                };
            }
            throw new Error("unexpected request");
        };

        const view = await mountDrawer({ shotIds: ["shot-10"] });
        try {
            const quickConfirmBtn = findButton("全选并确认绑定（3 项）")!;
            expect(quickConfirmBtn).toBeTruthy();
            await act(async () => {
                quickConfirmBtn.click();
                await settle();
            });

            expect(appendPayloads).toHaveLength(1);
            expect(appendPayloads[0]?.selections).toEqual([
                {
                    shotId: "shot-10",
                    assetVersionIds: ["ver-prop-1", "ver-char-sangruo", "ver-char-peirang"],
                },
            ]);
        } finally {
            await view.dispose();
        }
    });
});
