// 角色剧本建议的会话生命周期纯逻辑：会话守卫状态机、运行身份、dirty/证据归属、
// 采纳与替换决策、保存门禁和停止序列编排。本模块不依赖 React、AntD 或网络，
// 弹窗组件（character-suggestion-panel.tsx / assets.tsx）只做渲染编排。
// 依据：docs/openspec/changes/character-script-suggestions/design.md §4-§6。
import { aliasesArrayToFormValue } from "./character-script-suggestions";
import { characterFieldKeys } from "./character-script-suggestions";
import type { CharacterFieldKey, VerifiedPerson, VerifiedSuggestedField, VerifiedSuggestionResult } from "./character-script-suggestions";

// ---------- 会话守卫状态机 ----------

// 跨轮会话守卫：所有轮次的 token 在同一个计数器上推进，新轮 begin() 后旧 token
// 立即失效；invalidate() 静默推进计数（不发新 token）使当前轮失效。
// 不用 isMounted 布尔量区分两轮请求（design §4）。
export type SuggestionSessionGuard = {
    begin: () => number;
    isCurrent: (token: number) => boolean;
    invalidate: () => void;
};

export function createSuggestionSessionGuard(): SuggestionSessionGuard {
    let token = 0;
    return {
        begin: () => {
            token += 1;
            return token;
        },
        isCurrent: (candidate: number) => candidate === token,
        invalidate: () => {
            token += 1;
        },
    };
}

// ---------- 运行身份 ----------

// 一轮提取绑定的完整身份：任何一项变化都使旧结果不可应用到当前表单
// （关闭/重开、改名、改范围、换项目、换用户、换编辑对象）。
// model 属于发起前配置：换模型不构成会话目标变化。
export type SuggestionRunIdentity = {
    projectId: string;
    userId: string;
    editorAssetId: string;
    characterName: string;
    selectedUnitIds: string[];
    model: string;
};

export type SuggestionRun = {
    token: number;
    identity: SuggestionRunIdentity;
    // 本会话内用户编辑过的字段标记；主动清空也算编辑（design §4）。
    dirtyFields: Partial<Record<CharacterFieldKey, boolean>>;
    // 已产生 AI 依据的字段：origin 固定为 "ai"，用户改写后仍标注为 AI 原建议。
    evidence: Partial<Record<CharacterFieldKey, FieldEvidenceMeta>>;
};

export type FieldEvidenceMeta = {
    origin: "ai";
    dismissed: boolean;
};

export function canUseSuggestionResult(runIdentity: SuggestionRunIdentity, currentIdentity: SuggestionRunIdentity): boolean {
    return runIdentity.projectId === currentIdentity.projectId
        && runIdentity.userId === currentIdentity.userId
        && runIdentity.editorAssetId === currentIdentity.editorAssetId
        && runIdentity.characterName === currentIdentity.characterName
        && runIdentity.selectedUnitIds.length === currentIdentity.selectedUnitIds.length
        && runIdentity.selectedUnitIds.every((unitId, index) => unitId === currentIdentity.selectedUnitIds[index]);
}

export function beginSuggestionRun(guard: SuggestionSessionGuard, identity: SuggestionRunIdentity): SuggestionRun {
    const token = guard.begin();
    return { token, identity, dirtyFields: {}, evidence: {} };
}

// 关闭弹窗/切换编辑对象时失效当前轮。返回是否发生了实际失效（幂等）。
export function invalidateSuggestionSession(guard: SuggestionSessionGuard, runToken: number): boolean {
    const wasCurrent = guard.isCurrent(runToken);
    guard.invalidate();
    return wasCurrent;
}

// ---------- 单字段审核决策 ----------

// 表单键相对上一帧变化的字段列表：undefined/null 归一为空串后按字符串比较。
export function changedFieldKeys(input: { watched: Record<string, unknown>; previous: Record<string, unknown> }): CharacterFieldKey[] {
    const changed: CharacterFieldKey[] = [];
    for (const key of characterFieldKeys) {
        const next = input.watched[key] === undefined || input.watched[key] === null ? "" : String(input.watched[key]);
        const last = input.previous[key] === undefined || input.previous[key] === null ? "" : String(input.previous[key]);
        if (next !== last) changed.push(key);
    }
    return changed;
}

// 表单值变化 → dirty 标记（面板逐字段 useWatch 的可测接缝，design §4）。
// 只比较 characterFieldKeys 中出现的键：值相对上一帧变化（含主动清空为空串、
// 上一帧有值本帧缺失）即置 dirty；无变化返回原 run 引用，run 为 null 返回 null。
export function markDirtyFieldsFromValues(input: {
    run: SuggestionRun | null;
    watched: Record<string, unknown>;
    previous: Record<string, unknown>;
}): SuggestionRun | null {
    const { run, watched, previous } = input;
    if (!run) return null;
    const changed = changedFieldKeys({ watched, previous });
    if (!changed.length) return run;
    const dirtyUpdates: Partial<Record<CharacterFieldKey, boolean>> = {};
    for (const key of changed) dirtyUpdates[key] = true;
    return { ...run, dirtyFields: { ...run.dirtyFields, ...dirtyUpdates } };
}

// 程序化写入观察：本轮由代码（安全自动填充/采纳应用）写入的字段不误标为用户编辑。
// 传入本轮程序写入的键（writtenKeys）；这些键若同时被用户编辑过（已有 dirty）则保留 dirty。
export function observeProgrammaticWrites(input: {
    run: SuggestionRun;
    writtenKeys: CharacterFieldKey[];
    watched: Record<string, unknown>;
    previous: Record<string, unknown>;
}): SuggestionRun {
    const changed = new Set(changedFieldKeys({ watched: input.watched, previous: input.previous }));
    const dirtyUpdates: Partial<Record<CharacterFieldKey, boolean>> = {};
    for (const key of changed) {
        if (input.writtenKeys.includes(key)) continue;
        dirtyUpdates[key] = true;
    }
    if (!Object.keys(dirtyUpdates).length) return input.run;
    return { ...input.run, dirtyFields: { ...input.run.dirtyFields, ...dirtyUpdates } };
}

// 自动填充记录：把程序安全填充的字段登记为 AI 证据（非 dismissed），供
// 「已自动填充」标注与保存门禁判定（已处理字段）。不改动 dirty 状态。
export function recordAutoFillEvidence(input: { run: SuggestionRun; keys: CharacterFieldKey[] }): SuggestionRun {
    if (!input.keys.length) return input.run;
    const evidence = { ...input.run.evidence };
    for (const key of input.keys) evidence[key] = { origin: "ai", dismissed: false };
    return { ...input.run, evidence };
}

// 新一轮提取继承上一轮的字段编辑标记：用户清空/改写过字段的标记跨轮保留，
// 重提取不再把 AI 值静默写回（design §4「旧 AI 自动填入的值不在重试时静默覆盖」）。
export function seedDirtyFields(input: { run: SuggestionRun; dirtyFields: Partial<Record<CharacterFieldKey, boolean>> }): SuggestionRun {
    const inherited: Partial<Record<CharacterFieldKey, boolean>> = {};
    for (const [key, value] of Object.entries(input.dirtyFields)) {
        if (value) inherited[key as CharacterFieldKey] = true;
    }
    return { ...input.run, dirtyFields: { ...inherited, ...input.run.dirtyFields } };
}

// 发起新一轮并继承上一轮的编辑标记（seedDirtyFields 的发起点组合）：重新提取与
// 歧义选人（chooseAmbiguousPerson）开新轮共用同一入口，保证 dirty 跨轮语义单轨。
// 首次发起（无上一轮）等价于 beginSuggestionRun。
export function beginRunInheritingDirty(input: {
    guard: SuggestionSessionGuard;
    identity: SuggestionRunIdentity;
    previousRun: SuggestionRun | null;
}): SuggestionRun {
    return seedDirtyFields({
        run: beginSuggestionRun(input.guard, input.identity),
        dirtyFields: input.previousRun?.dirtyFields || {},
    });
}

// ---------- 运行状态通知通道（编辑器持有，跨 api 对象替换存活） ----------

// 运行快照订阅句柄：与 useSyncExternalStore 的 subscribe 契约一致。
export type UnsubscribeRunning = () => void;

// 提取运行中禁用保存的通知通道：由编辑器（assets.tsx）创建并订阅，面板经 api 对象
// 拿到同一引用后只写快照。旧实现把回调挂在面板 useMemo 的 api 对象上——running 翻转
// 或门禁依赖变化都会生成新 api 对象，onApiReady 只换 ref 不重挂回调，订阅成孤儿，
// 保存禁用通知从此丢失（不禁用或卡在禁用）。通道与 api 对象解耦后，对象替换不影响
// 订阅链路。同值写入去重：不影响快照的 setRunning 不触发订阅者。
export type RunningChannel = {
    subscribe: (onChange: () => void) => UnsubscribeRunning;
    getRunning: () => boolean;
    setRunning: (running: boolean) => void;
};

export function createRunningChannel(): RunningChannel {
    let running = false;
    let subscribers = new Set<() => void>();
    return {
        subscribe: (onChange: () => void) => {
            subscribers.add(onChange);
            return () => { subscribers.delete(onChange); };
        },
        getRunning: () => running,
        setRunning: (next: boolean) => {
            if (next === running) return;
            running = next;
            for (const notify of subscribers) notify();
        },
    };
}

// ---------- 单字段审核决策 ----------

export type FieldReviewAction = "auto" | "replace" | "adopt" | "choose" | "none";

export type FieldReview = {
    action: FieldReviewAction;
};

// 单字段采纳决策（design §4/§5）：
// - suggested + fact + 字段为空 + 未 dirty → 自动填充；
// - 字段已有值或被编辑过 → 需用户点「替换」；
// - inference/production → 需用户主动「采用」；
// - conflict → 必须由用户选择一项（不自动挑值）；
// - unspecified → 只反馈状态。
export function fieldReview(input: { field: VerifiedSuggestedField; formValue: string; dirty: boolean }): FieldReview {
    if (input.field.status === "unspecified") return { action: "none" };
    if (input.field.status === "conflict") return { action: "choose" };
    const option = input.field.options[0];
    if (!option) return { action: "none" };
    if (option.basis !== "fact") return { action: "adopt" };
    if (input.dirty || (input.formValue || "").trim()) return { action: "replace" };
    return { action: "auto" };
}

// conflict / 未经处理的建议是否存在需要用户显式确认的动作。
export function needsExplicitConfirm(review: FieldReview): boolean {
    return review.action === "choose";
}

// ---------- 采纳应用 ----------

export type SuggestionAdoptionAction = "adopt" | "replace" | "choose" | "dismiss";

export type AdoptionApplyResult = {
    values: Partial<Record<CharacterFieldKey, string>>;
    evidence: Partial<Record<CharacterFieldKey, FieldEvidenceMeta>>;
};

// 把一次明确的采纳/替换/冲突选择/暂不采用应用到表单草稿。
// 采纳后证据 origin 固定 "ai"：用户后续改写该字段也不冒充手写值的证明。
export function applyAdoptionToDraft(input: {
    draft: Record<string, string>;
    fieldKey: CharacterFieldKey;
    field: VerifiedSuggestedField;
    optionIndex: number;
    action: SuggestionAdoptionAction;
}): AdoptionApplyResult {
    if (input.field.status === "unspecified") throw new Error("unspecified 字段没有可采纳的选项");
    const option = input.field.options[input.optionIndex];
    if (!option) throw new Error(`字段 ${input.fieldKey} 不存在第 ${input.optionIndex} 个选项`);
    if (input.action === "dismiss") {
        // 显式暂不采用：不改动当前草稿值，只把该字段标记为已处理。
        return { values: { [input.fieldKey]: input.draft[input.fieldKey] || "" }, evidence: { [input.fieldKey]: { origin: "ai", dismissed: true } } };
    }
    const value = input.fieldKey === "aliases" ? aliasesArrayToFormValue(option.value as string[]) : (option.value as string);
    return { values: { [input.fieldKey]: value }, evidence: { [input.fieldKey]: { origin: "ai", dismissed: false } } };
}

// ---------- 审核完成判定与保存门禁 ----------

// pendingFieldCount：本轮需要用户给出决定的字段数（有建议内容的字段总数）；
// evidence：已处理的字段表。全部待处理字段都有处理记录（采纳/替换/选择/暂不采用）才算完成。
export function isReviewResolved(pendingFieldCount: number, evidence: Partial<Record<CharacterFieldKey, FieldEvidenceMeta>>): boolean {
    return Object.keys(evidence).length >= pendingFieldCount;
}

export type SaveGate = { allowed: boolean; reason?: string };

// 保存门禁（design §4/§5）：建议流程未完成审核（有未解决冲突/身份）时不放行，
// 不允许静默丢弃建议直接保存。未开启建议流程的表单按原方式保存。
export function decideSaveGate(input: { suggestionActive: boolean; reviewResolved: boolean; identityChosen: boolean; running?: boolean }): SaveGate {
    if (!input.suggestionActive) return { allowed: true };
    if (input.running) return { allowed: false, reason: "建议提取进行中，请等待完成或先停止再保存" };
    if (!input.identityChosen) return { allowed: false, reason: "存在同名候选，请先选择要处理的人物再保存" };
    if (!input.reviewResolved) return { allowed: false, reason: "AI 建议尚未处理完，请先完成审核（采纳、替换或暂不采用）再保存" };
    return { allowed: true };
}

// ---------- 提取阶段与状态文案 ----------

// 提取阶段推进（design §4：idle → loading-sources → extracting → review/error）。
// 读取阶段收到首个带 id 的任务事件即进入 extracting（模型分析开始）；
// 其余阶段不被任务事件改写（review/error/idle 由提取完成/失败路径设置）。
export function phaseAfterTaskUpdate(input: { phase: SuggestionPhase; task?: { id?: string } }): SuggestionPhase {
    if (input.phase !== "loading-sources") return input.phase;
    return input.task?.id ? "extracting" : input.phase;
}

export type SuggestionPhase = "idle" | "loading-sources" | "extracting" | "review" | "error";

// 阶段状态文案（aria-live 区域）：读取显示 n/N，提取显示模型分析中 + 任务状态。
export function suggestionStatusText(input: { phase: SuggestionPhase; progress: { read: number; total: number } | null; taskStatusText: string }): string {
    if (input.phase === "loading-sources" && input.progress) return `正在读取章节 ${input.progress.read}/${input.progress.total}…`;
    if (input.phase === "extracting") return input.taskStatusText ? `模型分析中：${input.taskStatusText}` : "模型分析中…";
    return "";
}

// ---------- 会话 UI 状态复位 ----------

// 一轮建议会话的可展示 UI 状态：身份漂移/弹窗关闭时整体复位（与关闭一致的清理，
// 不残留旧建议区或半失效的门禁）。verified/reviewPerson/stopNotice 清空，
// excludedUnits 归空数组。
export type SuggestionSessionUi = {
    verified: VerifiedSuggestionResult | null;
    reviewPerson: VerifiedPerson | null;
    stopNotice: string | null;
    excludedUnits: Array<{ unitId: string; title: string }>;
};

export function emptySuggestionSessionUi(): SuggestionSessionUi {
    return { verified: null, reviewPerson: null, stopNotice: null, excludedUnits: [] };
}


// ---------- 停止序列 ----------

export type StopSequenceEvent =
    | { kind: "invalidated"; staleToken: number }
    | { kind: "aborted" }
    | { kind: "cancel"; taskId: string }
    | { kind: "cancel-failed"; taskId: string; message: string }
    | { kind: "no-task-id" };

export type StopSequenceInput = { taskId?: string; onEvent?: (event: StopSequenceEvent) => void };

export type StopSequenceController = ((input: StopSequenceInput) => Promise<void>) & {
    // 停止后 onTaskUpdate 才送来的迟到 taskId：补发取消（守卫已失效，结果不可能再应用）。
    reportLateTaskId: (taskId: string) => { cancelled: boolean };
};

// 停止序列（design §4）：
// 1. 先失效会话守卫（旧响应/迟到结果不能再应用）；
// 2. abort 读取与监听 signal；
// 3. 对已知 taskId 调用已有取消接口；
// 4. 未收到 taskId 不假定任务未创建：停止后到达的 taskId 由 reportLateTaskId 补取消。
// 取消失败只如实上报（cancel-failed 事件），不自动重提取。
export function createStopController(input: {
    guard: SuggestionSessionGuard;
    runToken: number;
    signal: AbortSignal;
    abort: () => void;
    cancelTask: (taskId: string) => Promise<unknown> | unknown;
}): StopSequenceController {
    const { guard, runToken, signal, abort, cancelTask } = input;
    const canceledTaskIds = new Set<string>();
    let lastOnEvent: ((event: StopSequenceEvent) => void) | undefined;

    const emit = (event: StopSequenceEvent) => lastOnEvent?.(event);

    // 同步发起取消请求：成功/失败都经事件上报；失败不吞错也不触发重提取。
    const requestCancel = async (taskId: string) => {
        if (canceledTaskIds.has(taskId)) return;
        canceledTaskIds.add(taskId);
        try {
            await cancelTask(taskId);
            emit({ kind: "cancel", taskId });
        } catch (error) {
            emit({ kind: "cancel-failed", taskId, message: error instanceof Error ? error.message : "取消请求失败" });
        }
    };

    const run = async ({ taskId, onEvent }: StopSequenceInput) => {
        lastOnEvent = onEvent;
        // 1. 失效守卫；2. abort 读取/监听（AbortController.abort 由调用方注入，
        // 因为 Bun 的 AbortSignal 实例上没有 abort 方法）。
        guard.invalidate();
        if (!signal.aborted) abort();
        emit({ kind: "invalidated", staleToken: runToken });
        emit({ kind: "aborted" });
        // 3. 已知 taskId 才发取消请求；未收到 taskId 不能假定任务尚未创建。
        if (taskId) await requestCancel(taskId);
        else emit({ kind: "no-task-id" });
    };

    const controller = run as StopSequenceController;
    controller.reportLateTaskId = (taskId: string) => {
        // 守卫仍有效说明停止未发生（或新一轮已开始），不在此补取消。
        if (guard.isCurrent(runToken) || canceledTaskIds.has(taskId)) return { cancelled: false };
        // 同步发起（测试与 UI 都能在调用后立刻观测到取消请求已发出）。
        canceledTaskIds.add(taskId);
        try {
            const result = cancelTask(taskId);
            void Promise.resolve(result).then(
                () => emit({ kind: "cancel", taskId }),
                (error: unknown) => emit({ kind: "cancel-failed", taskId, message: error instanceof Error ? error.message : "取消请求失败" }),
            );
        } catch (error) {
            emit({ kind: "cancel-failed", taskId, message: error instanceof Error ? error.message : "取消请求失败" });
        }
        return { cancelled: true };
    };
    return controller;
}

// ---------- 章节范围选择辅助 ----------

// toggle 一个章节：已选移除、未选追加，保持原有顺序。
export function appendUnitIds(selected: string[], unitId: string): string[] {
    if (selected.includes(unitId)) return selected.filter((item) => item !== unitId);
    return [...selected, unitId];
}
