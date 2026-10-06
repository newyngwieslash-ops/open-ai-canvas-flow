// 角色剧本建议的会话生命周期纯逻辑：会话守卫状态机、运行身份、dirty/证据归属、
// 采纳与替换决策、保存门禁和停止序列编排。本模块不依赖 React、AntD 或网络，
// 弹窗组件（character-suggestion-panel.tsx / assets.tsx）只做渲染编排。
// 依据：docs/openspec/changes/character-script-suggestions/design.md §4-§6。
import { aliasesArrayToFormValue } from "./character-script-suggestions";
import type { CharacterFieldKey, VerifiedSuggestedField } from "./character-script-suggestions";

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
export function decideSaveGate(input: { suggestionActive: boolean; reviewResolved: boolean; identityChosen: boolean }): SaveGate {
    if (!input.suggestionActive) return { allowed: true };
    if (!input.identityChosen) return { allowed: false, reason: "存在同名候选，请先选择要处理的人物再保存" };
    if (!input.reviewResolved) return { allowed: false, reason: "AI 建议尚未处理完，请先完成审核（采纳、替换或暂不采用）再保存" };
    return { allowed: true };
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
