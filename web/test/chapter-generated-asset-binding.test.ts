import { expect, test } from "bun:test";
import { loadChapterAssetCandidates } from "@/pages/projects/detail/chapter-asset-candidates";
import { chapterStoryboardFromGenerationTask, generateChapterStoryboard } from "@/pages/projects/detail/project-chapter-ai";
import { storyboardRowsToProjectShots } from "@/pages/projects/detail/chapter-storyboard-production";
import * as production from "@/pages/projects/detail/chapter-storyboard-production";
import { apiClient } from "@/services/api/request";
import { buildShotAssetReferenceContext } from "@/pages/projects/detail/workflow-shot-references";
import { submitBackendGenerationTask } from "@/services/api/generation-task";
import { createModelChannel, defaultConfig, encodeChannelModel } from "@/stores/use-config-store";

test("recovered storyboard keeps the real server task ID for resolve and save", () => {
    const result = chapterStoryboardFromGenerationTask({
        id: "server-task-17", type: "canvas_text", status: "succeeded", prompt: "", attempts: 1,
        createdAt: "", updatedAt: "",
        resultJson: JSON.stringify({ text: JSON.stringify({ rows: [{ plotDescription: "A door opens" }] }) }),
    });
    expect(result).toMatchObject({ taskId: "server-task-17" });
    expect(result.rows).toHaveLength(1);
});

test("generated save resolves the real task then sends baseline and row IDs with the old shot set", async () => {
    const save = (production as Record<string, any>).saveGeneratedChapterStoryboard;
    expect(typeof save).toBe("function");
    const oldAdapter = apiClient.defaults.adapter;
    const calls: Array<{url?: string; data: any}> = [];
    const baseline = { projectId: "p", unitId: "u", projectRevision: 2, taskId: "t", taskResultFingerprint: "fingerprint", catalog: [] };
    const resolution = { taskId: "t", baseline, rows: [{ rowIndex: 0, assetVersionIds: ["v"], candidates: [], issues: [{kind:"missing_image",message:"missing image"}] }] };
    apiClient.defaults.adapter = async (config) => {
        calls.push({url:config.url,data:JSON.parse(config.data)});
        return {config,status:200,statusText:"OK",headers:{},data:{code:0,data: config.method === "post" ? resolution : {shots:[]},msg:""}};
    };
    try {
        const result=chapterStoryboardFromGenerationTask({id:"t",type:"canvas_text",status:"succeeded",prompt:"",attempts:1,createdAt:"",updatedAt:"",resultJson:JSON.stringify({rows:[{plotDescription:"door"}]})});
        const saved=await save({project:{id:"p"},assets:[],shots:[{id:"old",unitId:"u"}]},"u",result);
        expect(calls[0]).toEqual({url:"/projects/p/units/u/shot-asset-bindings/resolve-generated",data:{taskId:"t"}});
        expect(calls[1].data).toMatchObject({expectedShotIds:["old"],generatedBinding:{taskId:"t",baseline,rowIndices:[0]},shots:[{assetVersionIds:["v"]}]});
        expect(saved.issues).toEqual([{rowIndex:0,kind:"missing_image",message:"missing image"}]);
    } finally { apiClient.defaults.adapter=oldAdapter; }
});

test("chapter adapter uses resolved versions instead of canvas node IDs", () => {
    const { rows } = chapterStoryboardFromGenerationTask({ id: "t", type: "canvas_text", status: "succeeded", prompt: "", attempts: 1, createdAt: "", updatedAt: "", resultJson: JSON.stringify({ rows: [{ plotDescription: "A door opens", assetBindings: [{ nodeId: "forged", role: "reference" }] }] }) });
    const resolution = { taskId: "t", baseline: {}, rows: [{ rowIndex: 0, assetVersionIds: ["real-character-v", "real-scene-v", "real-prop-v"], candidates: [], issues: [] }] };
    const shots = storyboardRowsToProjectShots(rows, undefined, resolution as never);
    expect(shots[0].assetVersionIds).toEqual(["real-character-v", "real-scene-v", "real-prop-v"]);
});

test("generated prompts use resolved visible assets and never forged canvas bindings", () => {
    const { rows } = chapterStoryboardFromGenerationTask({id:"t",type:"canvas_text",status:"succeeded",prompt:"",attempts:1,createdAt:"",updatedAt:"",resultJson:JSON.stringify({rows:[{plotDescription:"door",videoMotionPrompt:"open door",assetBindings:[{nodeId:"forged",role:"reference"}]}]})});
    const detail={assets:[{id:"forged",title:"wrong",primaryVersionId:"wrong-v",category:"prop"}]} as never;
    const resolution={rows:[{rowIndex:0,assetVersionIds:["real-v","missing-v"],candidates:[{assetId:"real",name:"door",category:"environment",assetVersionId:"real-v",missingImage:false},{assetId:"missing",name:"actor",category:"character",assetVersionId:"missing-v",missingImage:true}],issues:[]}]} as never;
    const shots=storyboardRowsToProjectShots(rows,detail,resolution);
    expect(shots[0].revision.videoPrompt).toContain("@[asset:real]");
    expect(shots[0].revision.videoPrompt).not.toContain("@[asset:forged]");
    expect(shots[0].revision.videoPrompt).not.toContain("@[asset:missing]");
});

test("generated save presents issues before any replacement and preserves cancellation", async () => {
    const original=apiClient.defaults.adapter;let replacements=0;let seen=false;
    apiClient.defaults.adapter=async(config)=>{
        if(config.method==="put")replacements++;
        return {config,status:200,statusText:"OK",headers:{},data:{code:0,data:{taskId:"t",baseline:{},rows:[{rowIndex:0,assetVersionIds:[],candidates:[],issues:[{kind:"ambiguous",message:"ambiguous asset"}]}]},msg:""}};
    };
    const result=chapterStoryboardFromGenerationTask({id:"t",type:"canvas_text",status:"succeeded",prompt:"",attempts:1,createdAt:"",updatedAt:"",resultJson:JSON.stringify({rows:[{plotDescription:"door"}]})});
    try {
        await expect((production.saveGeneratedChapterStoryboard as any)({project:{id:"p"},assets:[],shots:[]},"u",result,async(issues:any[])=>{seen=issues[0].kind==="ambiguous";expect(replacements).toBe(0);return false;})).rejects.toMatchObject({name:"AbortError"});
        expect(seen).toBe(true);expect(replacements).toBe(0);
    } finally {apiClient.defaults.adapter=original;}
});

test("persisted version without an image never borrows the current asset head image", () => {
    const asset={id:"a",title:"actor",category:"character",mediaType:"entity",primaryVersionId:"current-v",character:{versionId:"current-v",representations:[{id:"current",resourceId:"wrong-current-image",role:"primary",mediaType:"image"}]}};
    const detail={assets:[asset],shotReferences:[{id:"ref",shotId:"shot",assetVersionId:"persisted-v",status:"linked",asset,referencedVersion:{id:"persisted-v",assetId:"a",version:1,representations:[]}}]} as never;
    expect(buildShotAssetReferenceContext(detail,"shot").referenceImages).toEqual([]);
});

test("persisted original image is the reference for both downstream modes", async () => {
    const asset={id:"a",title:"actor",category:"character",mediaType:"entity",primaryVersionId:"current-v",character:{versionId:"current-v",representations:[]}};
    const detail={assets:[asset],shotReferences:[{id:"ref",shotId:"shot",assetVersionId:"persisted-v",status:"linked",asset,referencedVersion:{id:"persisted-v",assetId:"a",version:1,representations:[],referenceImage:{resourceId:"bound-original"}}}]} as never;
    const context=buildShotAssetReferenceContext(detail,"shot");
    expect(context.referenceImages.map((image)=>image.storageKey)).toEqual(["resource:bound-original"]);
    for(const mode of ["image","video"] as const){
        const protocol=mode==="image"?"volcengine-ark-agent-plan-image":"volcengine-ark-agent-plan-video";
        const channel=createModelChannel({id:"test",name:"test",baseUrl:"https://ark.cn-beijing.volces.com/api/plan/v3",apiKey:"test-only",interfaceType:protocol,models:["test-model"],modelCosts:[{model:"test-model",capability:mode,protocol,billingMode:"fixed_request",unitPriceMicrocredits:0}]});
        const model=encodeChannelModel(channel.id,"test-model");
        let payload:any;
        await submitBackendGenerationTask({projectId:"p",mode,prompt:"door",config:{...defaultConfig,model,videoModel:model,channels:[channel],baseUrl:channel.baseUrl,interfaceType:protocol},referenceImages:context.referenceImages},{createId:()=>"stable",waitTask:async()=>{throw new Error("must not wait");},createTask:async(input)=>{payload=input;return {id:"image-task",type:mode,status:"queued",prompt:"",attempts:0,createdAt:"",updatedAt:""};}});
        expect(payload.input.mode).toBe(mode);
        expect(payload.input.referenceImages.map((image:any)=>image.storageKey)).toEqual(["resource:bound-original"]);
    }
});

test("generated save retains prepared shots, baseline, and rowIndices across transient network failure and replays without re-resolving or re-confirming", async () => {
    const save = production.saveGeneratedChapterStoryboard;
    const oldAdapter = apiClient.defaults.adapter;
    let resolveCount = 0;
    let putCount = 0;
    let confirmCount = 0;
    const putPayloads: any[] = [];
    const baselineV1 = { projectId: "p-retry", unitId: "u-retry", projectRevision: 3, taskId: "t-retry", taskResultFingerprint: "fp-1", catalog: [] };
    const baselineDrifted = { projectId: "p-retry", unitId: "u-retry", projectRevision: 4, taskId: "t-retry", taskResultFingerprint: "fp-1", catalog: [] };

    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post") {
            resolveCount += 1;
            const currentBaseline = resolveCount === 1 ? baselineV1 : baselineDrifted;
            const currentVersion = resolveCount === 1 ? "v-original" : "v-drifted";
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        taskId: "t-retry",
                        baseline: currentBaseline,
                        rows: [{
                            rowIndex: 0,
                            assetVersionIds: [currentVersion],
                            candidates: [{ assetId: "a", name: "阿青", category: "character", assetVersionId: currentVersion, missingImage: false }],
                            issues: [{ kind: "missing_image", message: "部分资产缺少参考图" }],
                        }],
                    },
                    msg: "",
                },
            };
        }
        putCount += 1;
        putPayloads.push(JSON.parse(config.data));
        if (putCount === 1) {
            throw new Error("Network Error: ECONNRESET");
        }
        return {
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: { code: 0, data: { shots: [{ id: "new-shot-1" }] }, msg: "" },
        };
    };

    try {
        const result = chapterStoryboardFromGenerationTask({
            id: "t-retry",
            type: "canvas_text",
            status: "succeeded",
            prompt: "",
            attempts: 1,
            createdAt: "",
            updatedAt: "",
            resultJson: JSON.stringify({ rows: [{ plotDescription: "阿青推门而入", videoMotionPrompt: "推门" }] }),
        });
        const detail = {
            project: { id: "p-retry" },
            assets: [{ id: "a", title: "阿青", primaryVersionId: "v-original", category: "character" }],
            shots: [{ id: "old-shot-1", unitId: "u-retry" }],
        } as never;

        await expect(
            save(detail, "u-retry", result, async (issues) => {
                confirmCount += 1;
                expect(issues).toEqual([{ rowIndex: 0, kind: "missing_image", message: "部分资产缺少参考图" }]);
                return true;
            }),
        ).rejects.toThrow("Network Error: ECONNRESET");

        expect(resolveCount).toBe(1);
        expect(confirmCount).toBe(1);
        expect(putCount).toBe(1);

        // Even if local detail.shots mutated before user clicked retry, the retained payload must replay verbatim
        const mutatedDetail = {
            project: { id: "p-retry" },
            assets: [{ id: "a", title: "阿青", primaryVersionId: "v-drifted", category: "character" }],
            shots: [{ id: "mutated-local-shot", unitId: "u-retry" }],
        } as never;

        const retrySaved = await save(mutatedDetail, "u-retry", result, async () => {
            confirmCount += 1;
            return true;
        });

        expect(resolveCount).toBe(1);
        expect(confirmCount).toBe(1);
        expect(putCount).toBe(2);
        expect(putPayloads[1]).toEqual(putPayloads[0]);
        expect(putPayloads[1]).toMatchObject({
            expectedShotIds: ["old-shot-1"],
            generatedBinding: { taskId: "t-retry", baseline: baselineV1, rowIndices: [0] },
            shots: [{ assetVersionIds: ["v-original"] }],
        });
        expect(retrySaved.issues).toEqual([{ rowIndex: 0, kind: "missing_image", message: "部分资产缺少参考图" }]);
    } finally {
        apiClient.defaults.adapter = oldAdapter;
    }
});

test("generated save clears retained payload on terminal 409 conflict or explicit discard", async () => {
    const save = production.saveGeneratedChapterStoryboard;
    const clearPending = (production as Record<string, any>).clearPendingGeneratedStoryboardSave;
    expect(typeof clearPending).toBe("function");
    const oldAdapter = apiClient.defaults.adapter;
    let resolveCount = 0;
    let putMode: "conflict" | "network" | "ok" = "conflict";

    apiClient.defaults.adapter = async (config) => {
        if (config.method === "post") {
            resolveCount += 1;
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        taskId: "t-clear",
                        baseline: { projectId: "p-clear", unitId: "u-clear", projectRevision: resolveCount, taskId: "t-clear", taskResultFingerprint: "fp", catalog: [] },
                        rows: [{ rowIndex: 0, assetVersionIds: [`v-${resolveCount}`], candidates: [], issues: [] }],
                    },
                    msg: "",
                },
            };
        }
        if (putMode === "conflict") {
            return {
                config,
                status: 409,
                statusText: "Conflict",
                headers: {},
                data: { code: 409, reason: "storyboard_asset_binding_conflict", msg: "资产绑定基线已过期" },
            };
        }
        if (putMode === "network") {
            throw new Error("Network timeout");
        }
        return {
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: { code: 0, data: { shots: [] }, msg: "" },
        };
    };

    try {
        const result = chapterStoryboardFromGenerationTask({
            id: "t-clear",
            type: "canvas_text",
            status: "succeeded",
            prompt: "",
            attempts: 1,
            createdAt: "",
            updatedAt: "",
            resultJson: JSON.stringify({ rows: [{ plotDescription: "镜头1" }] }),
        });
        const detail = { project: { id: "p-clear" }, assets: [], shots: [{ id: "s1", unitId: "u-clear" }] } as never;

        // 1. Terminal 409 conflict must clear any retained payload so the next attempt re-resolves fresh baseline
        await expect(save(detail, "u-clear", result)).rejects.toThrow("资产绑定基线已过期");
        expect(resolveCount).toBe(1);

        putMode = "network";
        await expect(save(detail, "u-clear", result)).rejects.toThrow("Network timeout");
        expect(resolveCount).toBe(2);

        // 2. Explicit discard clears retained payload so subsequent attempt re-resolves
        clearPending("p-clear", "u-clear", "t-clear");
        putMode = "ok";
        await save(detail, "u-clear", result);
        expect(resolveCount).toBe(3);
    } finally {
        apiClient.defaults.adapter = oldAdapter;
    }
});

test("full-chain chapter generation paginates 81+ candidates across 2 pages, submits chapterAssetContext, resolves and saves bindings, and isolates downstream image/video reference images to bound versions", async () => {
    const oldAdapter = apiClient.defaults.adapter;
    const oldFetch = globalThis.fetch;
    const oldWindow = (globalThis as Record<string, unknown>).window;
    if (!oldWindow) {
        (globalThis as Record<string, unknown>).window = Object.assign(new EventTarget(), {
            setTimeout: globalThis.setTimeout.bind(globalThis),
            clearTimeout: globalThis.clearTimeout.bind(globalThis),
        });
    }
    globalThis.fetch = (async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/tasks/task-e2e-1/text-events")) {
            return new Response('id: 1\nevent: terminal\ndata: {"status":"succeeded"}\n\n', {
                status: 200,
                headers: { "content-type": "text/event-stream" },
            });
        }
        throw new Error(`Unexpected fetch: ${url}`);
    }) as typeof fetch;
    const candidatePageCalls: Array<{ page: string | number; pageSize: string | number }> = [];
    let createdTaskPayload: any = null;
    let resolvePayload: any = null;
    let putPayload: any = null;

    const coreItems = [
        { id: "char-hero", title: "阿青", category: "character", mediaType: "entity", primaryVersionId: "v-hero-1", character: { versionId: "v-hero-1", definition: { outfit: "黑衣剑装" } } },
        { id: "env-hall", title: "青云大殿", category: "environment", mediaType: "image", primaryVersionId: "v-env-1", visual: { versionId: "v-env-1", definition: { lighting: "夜雨烛火" } } },
        { id: "prop-sword", title: "青钢剑", category: "prop", mediaType: "image", primaryVersionId: "v-prop-1", visual: { versionId: "v-prop-1", definition: { material: "青钢" } } },
        { id: "char-no-img", title: "无图老者", category: "character", mediaType: "entity", primaryVersionId: "v-no-img-1", character: { versionId: "v-no-img-1", definition: {} } },
        { id: "prop-no-ver", title: "断剑残片", category: "prop", mediaType: "image", primaryVersionId: "" },
        { id: "char-dup-p1", title: "黑衣卫", category: "character", mediaType: "entity", primaryVersionId: "v-dup-p1", character: { versionId: "v-dup-p1", definition: {} } },
    ];
    const fillerItems = Array.from({ length: 75 }, (_, i) => {
        const idx = String(i + 1).padStart(2, "0");
        const cats = ["character", "environment", "prop"] as const;
        const category = cats[i % 3];
        return {
            id: `fill-asset-${idx}`,
            title: `配角资产${idx}`,
            category,
            mediaType: category === "character" ? "entity" : "image",
            primaryVersionId: `fill-ver-${idx}`,
        };
    });
    const page2DupItem = {
        id: "char-dup-p2",
        title: "黑衣卫",
        category: "character",
        mediaType: "entity",
        primaryVersionId: "v-dup-p2",
        character: { versionId: "v-dup-p2", definition: {} },
    };
    const all82Items = [...coreItems, ...fillerItems, page2DupItem];
    const page1Items = all82Items.slice(0, 80);
    const page2Items = all82Items.slice(80);

    const generatedRows = [
        {
            shotNumber: "SC.01",
            plotDescription: "阿青手持青钢剑走入青云大殿",
            dialogue: "阿青：来者何人",
            shotSize: "中景",
            camera: "平视",
            motion: "推进",
            durationSeconds: 5,
            imageGenerationPrompt: "阿青在青云大殿持青钢剑",
            videoMotionPrompt: "阿青持剑缓步前行",
            negativePrompt: "",
            continuityOut: "",
            timeBeats: "1s-5s 踏入大殿",
            projectAssetRefs: [
                { assetId: "char-hero", category: "character" },
                { assetId: "env-hall", category: "environment" },
                { assetId: "prop-sword", category: "prop" },
            ],
        },
        {
            shotNumber: "SC.02",
            plotDescription: "无图老者在殿角静坐凝视",
            dialogue: "",
            shotSize: "近景",
            camera: "平视",
            motion: "固定",
            durationSeconds: 4,
            imageGenerationPrompt: "无图老者静坐",
            videoMotionPrompt: "无图老者抬头",
            negativePrompt: "",
            continuityOut: "",
            timeBeats: "",
            projectAssetRefs: [{ assetId: "char-no-img", category: "character" }],
        },
        {
            shotNumber: "SC.03",
            plotDescription: "黑衣卫从殿柱暗影中现身包围",
            dialogue: "",
            shotSize: "全景",
            camera: "俯视",
            motion: "拉远",
            durationSeconds: 4,
            imageGenerationPrompt: "黑衣卫现身",
            videoMotionPrompt: "黑衣卫跃出",
            negativePrompt: "",
            continuityOut: "",
            timeBeats: "",
            projectAssetRefs: [],
        },
    ];

    const e2eBaseline = {
        projectId: "p-e2e",
        unitId: "u-e2e",
        projectRevision: 5,
        taskId: "task-e2e-1",
        taskResultFingerprint: "fp-e2e",
        catalog: [],
    };
    const e2eResolution = {
        taskId: "task-e2e-1",
        baseline: e2eBaseline,
        rows: [
            {
                rowIndex: 0,
                assetVersionIds: ["v-hero-1", "v-env-1", "v-prop-1"],
                candidates: [
                    { assetId: "char-hero", name: "阿青", category: "character", assetVersionId: "v-hero-1", version: 1, missingImage: false },
                    { assetId: "env-hall", name: "青云大殿", category: "environment", assetVersionId: "v-env-1", version: 1, missingImage: false },
                    { assetId: "prop-sword", name: "青钢剑", category: "prop", assetVersionId: "v-prop-1", version: 1, missingImage: false },
                ],
                issues: [],
            },
            {
                rowIndex: 1,
                assetVersionIds: ["v-no-img-1"],
                candidates: [
                    { assetId: "char-no-img", name: "无图老者", category: "character", assetVersionId: "v-no-img-1", version: 2, missingImage: true },
                ],
                issues: [{ kind: "missing_image", assetId: "char-no-img", name: "无图老者", message: "绑定资产版本缺少参考图" }],
            },
            {
                rowIndex: 2,
                assetVersionIds: ["v-dup-p1", "v-dup-p2"],
                candidates: [
                    { assetId: "char-dup-p1", name: "黑衣卫", category: "character", assetVersionId: "v-dup-p1", version: 1, missingImage: false },
                    { assetId: "char-dup-p2", name: "黑衣卫", category: "character", assetVersionId: "v-dup-p2", version: 1, missingImage: false },
                ],
                issues: [{ kind: "ambiguous", name: "黑衣卫", message: "存在同名资产 黑衣卫，请确认具体版本" }],
            },
        ],
    };

    apiClient.defaults.adapter = async (config) => {
        if (config.method === "get" && config.url === "/projects/p-e2e/assets") {
            const page = Number(config.params?.page || 1);
            const pageSize = Number(config.params?.pageSize || 80);
            candidatePageCalls.push({ page, pageSize });
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        assets: page === 1 ? page1Items : page2Items,
                        total: 82,
                        page,
                        pageSize,
                        hasMore: page === 1,
                    },
                    msg: "",
                },
            };
        }
        if (config.method === "post" && config.url === "/tasks") {
            createdTaskPayload = JSON.parse(config.data);
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        id: "task-e2e-1",
                        type: "canvas_text",
                        operation: "storyboard",
                        status: "running",
                        prompt: createdTaskPayload.prompt,
                        attempts: 1,
                        createdAt: "",
                        updatedAt: "",
                    },
                    msg: "",
                },
            };
        }
        if (config.method === "get" && config.url === "/tasks/task-e2e-1") {
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    data: {
                        id: "task-e2e-1",
                        type: "canvas_text",
                        operation: "storyboard",
                        status: "succeeded",
                        prompt: createdTaskPayload?.prompt || "",
                        attempts: 1,
                        createdAt: "",
                        updatedAt: "",
                        resultJson: JSON.stringify({ rows: generatedRows }),
                    },
                    msg: "",
                },
            };
        }
        if (config.method === "post" && config.url === "/projects/p-e2e/units/u-e2e/shot-asset-bindings/resolve-generated") {
            resolvePayload = JSON.parse(config.data);
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: { code: 0, data: e2eResolution, msg: "" },
            };
        }
        if (config.method === "put" && config.url === "/projects/p-e2e/units/u-e2e/shots") {
            putPayload = JSON.parse(config.data);
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: { code: 0, data: { shots: [{ id: "shot-e2e-1" }, { id: "shot-e2e-2" }, { id: "shot-e2e-3" }] }, msg: "" },
            };
        }
        throw new Error(`Unexpected request: ${config.method} ${config.url}`);
    };

    try {
        // Step 1: Load 82 candidates across 2 pages (pageSize=80)
        const candidates = await loadChapterAssetCandidates("p-e2e");
        expect(candidatePageCalls).toEqual([
            { page: 1, pageSize: 80 },
            { page: 2, pageSize: 80 },
        ]);
        expect(candidates).toHaveLength(82);

        // Step 2: Generate chapter storyboard with chapterAssetContext
        const textChannel = createModelChannel({
            id: "e2e-text",
            name: "e2e-text",
            baseUrl: "https://api.openai.com/v1",
            apiKey: "test-key",
            interfaceType: "openai-compatible",
            models: ["gpt-4o-mini"],
        });
        const textModel = encodeChannelModel(textChannel.id, "gpt-4o-mini");
        const generated = await generateChapterStoryboard({
            projectId: "p-e2e",
            chapterId: "u-e2e",
            chapterTitle: "第一章 青云夜雨",
            sourceText: "阿青持青钢剑踏入青云大殿，无图老者静坐，黑衣卫从暗处现身。",
            projectStyle: { presetId: "guofeng", title: "国风仙侠", prompt: "国风仙侠电影质感" },
            characters: [{ assetId: "char-hero", versionId: "v-hero-1", name: "阿青", definition: { outfit: "黑衣剑装" } }],
            assetCandidates: { status: "complete", assets: candidates },
            config: { ...defaultConfig, textModel, model: textModel, channels: [textChannel] },
            skills: [],
            selectedSkillIds: [],
        });

        expect(generated.taskId).toBe("task-e2e-1");
        expect(createdTaskPayload.input.chapterAssetContext).toEqual({
            projectId: "p-e2e",
            unitId: "u-e2e",
            scope: { kind: "all", assetIds: [] },
        });
        expect(createdTaskPayload.input.canvasAssets.map((a: any) => a.id)).toContain("char-dup-p2");
        expect(createdTaskPayload.input.canvasAssets).toHaveLength(82);

        // Step 3: Save generated storyboard with user confirmation selecting Page 2 duplicate asset v-dup-p2 on Row 2
        const initialDetail = {
            project: { id: "p-e2e" },
            assets: [
                { id: "char-hero", title: "阿青", category: "character", primaryVersionId: "v-hero-1" },
                { id: "env-hall", title: "青云大殿", category: "environment", primaryVersionId: "v-env-1" },
                { id: "prop-sword", title: "青钢剑", category: "prop", primaryVersionId: "v-prop-1" },
                { id: "char-no-img", title: "无图老者", category: "character", primaryVersionId: "v-no-img-1" },
                { id: "char-dup-p1", title: "黑衣卫", category: "character", primaryVersionId: "v-dup-p1" },
                { id: "char-dup-p2", title: "黑衣卫", category: "character", primaryVersionId: "v-dup-p2" },
            ],
            shots: [{ id: "old-init-shot", unitId: "u-e2e" }],
        } as never;

        const saved = await production.saveGeneratedChapterStoryboard(initialDetail, "u-e2e", generated, async (issues) => {
            expect(issues.map((i) => i.kind)).toEqual(["missing_image", "ambiguous"]);
            return [
                ["v-hero-1", "v-env-1", "v-prop-1"],
                ["v-no-img-1"],
                ["v-dup-p2"],
            ];
        });

        expect(resolvePayload).toEqual({ taskId: "task-e2e-1" });
        expect(putPayload).toMatchObject({
            expectedShotIds: ["old-init-shot"],
            generatedBinding: {
                taskId: "task-e2e-1",
                baseline: e2eBaseline,
                rowIndices: [0, 1, 2],
            },
            shots: [
                { assetVersionIds: ["v-hero-1", "v-env-1", "v-prop-1"] },
                { assetVersionIds: ["v-no-img-1"] },
                { assetVersionIds: ["v-dup-p2"] },
            ],
        });
        expect(putPayload.shots[0].revision.videoPrompt).toContain("@[asset:char-hero]");
        expect(putPayload.shots[0].revision.videoPrompt).toContain("@[asset:env-hall]");
        expect(putPayload.shots[0].revision.videoPrompt).toContain("@[asset:prop-sword]");
        expect(putPayload.shots[1].revision.videoPrompt).not.toContain("@[asset:char-no-img]");
        expect(putPayload.shots[2].revision.videoPrompt).toContain("@[asset:char-dup-p2]");
        expect(saved.shotCount).toBe(3);

        // Step 4: Downstream image & video generation from persisted workspace shotReferences
        // Simulate char-hero advancing its primaryVersionId to v-hero-2 (resource:res-hero-v2), while shot-e2e-1 stays bound to v-hero-1 (resource:res-hero-v1)
        const workspaceDetailAfterSave = {
            assets: [
                {
                    id: "char-hero",
                    title: "阿青",
                    category: "character",
                    mediaType: "entity",
                    primaryVersionId: "v-hero-2",
                    character: { versionId: "v-hero-2", representations: [{ id: "rep-h2", resourceId: "res-hero-v2", role: "reference", mediaType: "image" }] },
                },
                {
                    id: "env-hall",
                    title: "青云大殿",
                    category: "environment",
                    mediaType: "image",
                    primaryVersionId: "v-env-1",
                    visual: { versionId: "v-env-1", representations: [{ id: "rep-e1", resourceId: "res-env-v1", role: "reference", mediaType: "image" }] },
                },
                {
                    id: "prop-sword",
                    title: "青钢剑",
                    category: "prop",
                    mediaType: "image",
                    primaryVersionId: "v-prop-1",
                    visual: { versionId: "v-prop-1", representations: [{ id: "rep-p1", resourceId: "res-prop-v1", role: "reference", mediaType: "image" }] },
                },
                {
                    id: "char-no-img",
                    title: "无图老者",
                    category: "character",
                    mediaType: "entity",
                    primaryVersionId: "v-no-img-other",
                    character: { versionId: "v-no-img-other", representations: [{ id: "rep-wrong", resourceId: "res-wrong-fallback", role: "reference", mediaType: "image" }] },
                },
            ],
            shotReferences: [
                {
                    id: "ref-s1-hero",
                    shotId: "shot-e2e-1",
                    assetVersionId: "v-hero-1",
                    status: "linked",
                    asset: { id: "char-hero", title: "阿青", category: "character", mediaType: "entity", primaryVersionId: "v-hero-2" },
                    referencedVersion: { id: "v-hero-1", assetId: "char-hero", version: 1, representations: [], referenceImage: { resourceId: "res-hero-v1" } },
                },
                {
                    id: "ref-s1-env",
                    shotId: "shot-e2e-1",
                    assetVersionId: "v-env-1",
                    status: "linked",
                    asset: { id: "env-hall", title: "青云大殿", category: "environment", mediaType: "image", primaryVersionId: "v-env-1" },
                    referencedVersion: { id: "v-env-1", assetId: "env-hall", version: 1, representations: [], referenceImage: { resourceId: "res-env-v1" } },
                },
                {
                    id: "ref-s1-prop",
                    shotId: "shot-e2e-1",
                    assetVersionId: "v-prop-1",
                    status: "linked",
                    asset: { id: "prop-sword", title: "青钢剑", category: "prop", mediaType: "image", primaryVersionId: "v-prop-1" },
                    referencedVersion: { id: "v-prop-1", assetId: "prop-sword", version: 1, representations: [], referenceImage: { resourceId: "res-prop-v1" } },
                },
                {
                    id: "ref-s2-no-img",
                    shotId: "shot-e2e-2",
                    assetVersionId: "v-no-img-1",
                    status: "linked",
                    asset: { id: "char-no-img", title: "无图老者", category: "character", mediaType: "entity", primaryVersionId: "v-no-img-other" },
                    referencedVersion: { id: "v-no-img-1", assetId: "char-no-img", version: 2, representations: [] },
                },
            ],
        } as never;

        const shot1Context = buildShotAssetReferenceContext(workspaceDetailAfterSave, "shot-e2e-1");
        expect(shot1Context.referenceImages.map((img) => img.storageKey)).toEqual([
            "resource:res-hero-v1",
            "resource:res-env-v1",
            "resource:res-prop-v1",
        ]);
        expect(shot1Context.referenceImages.map((img) => img.storageKey)).not.toContain("resource:res-hero-v2");

        const shot2Context = buildShotAssetReferenceContext(workspaceDetailAfterSave, "shot-e2e-2");
        expect(shot2Context.referenceImages).toEqual([]);

        for (const mode of ["image", "video"] as const) {
            const protocol = mode === "image" ? "volcengine-ark-agent-plan-image" : "volcengine-ark-agent-plan-video";
            const channel = createModelChannel({
                id: `e2e-${mode}`,
                name: `e2e-${mode}`,
                baseUrl: "https://ark.cn-beijing.volces.com/api/plan/v3",
                apiKey: "test-only",
                interfaceType: protocol,
                models: ["test-model"],
                modelCosts: [{ model: "test-model", capability: mode, protocol, billingMode: "fixed_request", unitPriceMicrocredits: 0 }],
            });
            const model = encodeChannelModel(channel.id, "test-model");
            let submittedPayload: any;
            await submitBackendGenerationTask(
                {
                    projectId: "p-e2e",
                    mode,
                    prompt: "阿青在青云大殿持青钢剑",
                    config: { ...defaultConfig, model, videoModel: model, channels: [channel], baseUrl: channel.baseUrl, interfaceType: protocol },
                    referenceImages: shot1Context.referenceImages,
                },
                {
                    createId: () => "stable",
                    waitTask: async () => {
                        throw new Error("must not wait");
                    },
                    createTask: async (input) => {
                        submittedPayload = input;
                        return { id: `${mode}-task`, type: mode, status: "queued", prompt: "", attempts: 0, createdAt: "", updatedAt: "" };
                    },
                },
            );
            expect(submittedPayload.input.referenceImages.map((img: any) => img.storageKey)).toEqual([
                "resource:res-hero-v1",
                "resource:res-env-v1",
                "resource:res-prop-v1",
            ]);
        }
    } finally {
        apiClient.defaults.adapter = oldAdapter;
        globalThis.fetch = oldFetch;
        if (!oldWindow) {
            delete (globalThis as Record<string, unknown>).window;
        }
    }
});


