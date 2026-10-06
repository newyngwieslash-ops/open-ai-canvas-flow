// 角色剧本建议面板：页面私有组件，只做渲染编排与会话状态持有。
// 生命周期纯逻辑（守卫、dirty、采纳决策、停止序列、保存门禁）在 character-suggestion-session.ts，
// 建议产生（读取/预算/任务/解析核验）在 project-character-suggestions.ts。
// 依据：docs/openspec/changes/character-script-suggestions/design.md §5（生命周期）§6（界面）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Collapse, Form, Spin, Tag, type FormInstance } from "antd";
import { Sparkles, Square } from "lucide-react";

import { ModelPicker } from "@/components/model-picker";
import { navigateToSettings } from "@/lib/settings-navigation";
import { cancelGenerationTask } from "@/services/api/task-center";
import { listProjectUnits } from "@/services/api/projects";
import { useEffectiveConfig, useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import {
    appendUnitIds,
    applyAdoptionToDraft,
    beginRunInheritingDirty,
    canUseSuggestionResult,
    createSuggestionSessionGuard,
    createStopController,
    decideSaveGate,
    emptySuggestionSessionUi,
    fieldReview,
    isReviewResolved,
    markDirtyFieldsFromValues,
    observeProgrammaticWrites,
    phaseAfterTaskUpdate,
    recordAutoFillEvidence,
    suggestionStatusText,
    type RunningChannel,
    type SuggestionAdoptionAction,
    type SuggestionRun,
    type SuggestionRunIdentity,
} from "./character-suggestion-session";
import {
    characterFields,
    computeSafeAutoFillFields,
    type CharacterFieldKey,
    type VerifiedPerson,
    type VerifiedSuggestionResult,
} from "./character-script-suggestions";
import {
    defaultCharacterSuggestionDependencies,
    defaultSelectedStoryUnits,
    extractCharacterSuggestions,
    type CharacterSuggestionRun,
} from "./project-character-suggestions";

// assets.tsx 表单实例的最小使用面（面板不依赖整个 antd Form 类型，也不渲染表单本体）。
// 方法参数放宽为 string：FormInstance 的字面量键类型可赋给更宽的参数（逆变兼容）。
export type CharacterSuggestionForm = {
    getFieldValue: (key: string) => string | undefined;
    getFieldsValue: () => Record<string, unknown>;
    setFieldsValue: (values: Record<string, unknown>) => void;
};

// FormInstance<CharacterForm> 结构上兼容 CharacterSuggestionForm 的取值面。
export function asSuggestionForm(form: FormInstance): CharacterSuggestionForm {
    return {
        getFieldValue: (key: string) => {
            const value = (form.getFieldValue as (name: string) => unknown)(key);
            return typeof value === "string" ? value : undefined;
        },
        getFieldsValue: () => {
            const values = (form.getFieldsValue as () => unknown)();
            return values && typeof values === "object" ? values as Record<string, unknown> : {};
        },
        setFieldsValue: (values: Record<string, unknown>) => {
            (form.setFieldsValue as (input: Record<string, unknown>) => void)(values);
        },
    };
}

// 面板暴露给编辑器的最小 API：保存门禁 + 运行通知通道（运行中禁用保存）
// + 弹窗关闭时的会话失效。通道由编辑器创建、经 props 注入并随 api 对象携带：
// 订阅绑在编辑器持有的通道上，api 对象替换不会孤儿化订阅（Fix Round 2）。
export type CharacterSuggestionPanelApi = {
    reviewGate: () => string;
    invalidateSession: () => void;
    runningChannel?: RunningChannel;
};

export type CharacterSuggestionPanelProps = {
    projectId: string;
    // 真实 FormInstance：Form.useWatch 的 isFormInstance 检查要求 _init 标记，
    // 包装对象会让订阅静默失效（dirty 追踪死代码）。取值/写值面仍经 asSuggestionForm 收口。
    form: CharacterSuggestionForm;
    watchForm: FormInstance;
    editorAssetId: string;
    // 运行通知通道：编辑器创建并订阅，注入后经 api 对象携带（单一事实来源在编辑器侧）。
    runningChannel: RunningChannel;
    // API 注册：面板挂载后把保存门禁与会话失效句柄交给编辑器。
    onApiReady: (api: CharacterSuggestionPanelApi) => void;
};

// ---------- 主组件 ----------

export function CharacterSuggestionPanel({ projectId, form, watchForm, editorAssetId, runningChannel, onApiReady }: CharacterSuggestionPanelProps) {
    // 姓名来自编辑器表单（useWatch 订阅）：改名即构成新会话身份（design §5）。
    const watchedName = Form.useWatch("name", watchForm) || "";
    const characterName = typeof watchedName === "string" ? watchedName : "";
    // 12 字段逐字段订阅（Critical 1 修复）：值相对上一帧变化（含主动清空）即标记该字段 dirty。
    // dirty 标记绑定当前会话轮；弹窗内其他字段的编辑不清掉其他字段的已采纳依据。
    // 变更→dirty 的判定逻辑在 markDirtyFieldsFromValues（session.ts 已测纯函数）。
    const watchedRole = Form.useWatch("role", watchForm);
    const watchedAliases = Form.useWatch("aliases", watchForm);
    const watchedAppearance = Form.useWatch("appearance", watchForm);
    const watchedPhysique = Form.useWatch("physique", watchForm);
    const watchedClothing = Form.useWatch("clothing", watchForm);
    const watchedPersonality = Form.useWatch("personality", watchForm);
    const watchedProps = Form.useWatch("props", watchForm);
    const watchedConsistencyPrompt = Form.useWatch("consistencyPrompt", watchForm);
    const watchedMultiViewPrompt = Form.useWatch("multiViewPrompt", watchForm);
    const watchedVoiceLanguage = Form.useWatch("voiceLanguage", watchForm);
    const watchedVoiceAge = Form.useWatch("voiceAge", watchForm);
    const watchedVoiceTimbre = Form.useWatch("voiceTimbre", watchForm);
    const watchedRecord: Record<string, unknown> = {
        role: watchedRole,
        aliases: watchedAliases,
        appearance: watchedAppearance,
        physique: watchedPhysique,
        clothing: watchedClothing,
        personality: watchedPersonality,
        props: watchedProps,
        consistencyPrompt: watchedConsistencyPrompt,
        multiViewPrompt: watchedMultiViewPrompt,
        voiceLanguage: watchedVoiceLanguage,
        voiceAge: watchedVoiceAge,
        voiceTimbre: watchedVoiceTimbre,
    };
    const lastWatchedRef = useRef<Record<string, unknown> | null>(null);
    // 本轮程序化写入（安全自动填充/采纳应用）的字段：观察 effect 不把程序写入误标为用户编辑。
    const programmaticWritesRef = useRef<Set<CharacterFieldKey>>(new Set());
    useEffect(() => {
        const last = lastWatchedRef.current;
        lastWatchedRef.current = { ...watchedRecord };
        // 消耗本批程序化写入（更新器外）：StrictMode 双调用时两次拿到同一份快照，结果一致。
        const writtenKeys = programmaticWritesRef.current.size ? [...programmaticWritesRef.current] : [];
        programmaticWritesRef.current = new Set();
        setRun((current) => {
            if (!current) return current;
            // StrictMode 下更新器会被双调用：ref 消耗必须在更新器外完成（Minor Fix Round 2），
            // 否则第一次调用清空写入集、第二次调用把程序写入误标为用户编辑。
            const written = writtenKeys;
            if (written.length) {
                return observeProgrammaticWrites({ run: current, writtenKeys: written, watched: watchedRecord, previous: last || {} });
            }
            return markDirtyFieldsFromValues({ run: current, watched: watchedRecord, previous: last || {} });
        });
        // watchedRecord 逐键来自稳定的 12 个 useWatch 订阅；按整体值比较，任何字段变化都重算 dirty。
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [watchedRole, watchedAliases, watchedAppearance, watchedPhysique, watchedClothing, watchedPersonality, watchedProps, watchedConsistencyPrompt, watchedMultiViewPrompt, watchedVoiceLanguage, watchedVoiceAge, watchedVoiceTimbre]);
    const effectiveConfig = useEffectiveConfig();
    const isAiConfigReady = useConfigStore((state) => state.isAiConfigReady);
    const userId = useUserStore((state) => state.user?.id || "");
    const [open, setOpen] = useState(false);
    const [selectedTextModel, setSelectedTextModel] = useState("");
    const [selectedUnitIds, setSelectedUnitIds] = useState<string[]>([]);
    const [phase, setPhase] = useState<"idle" | "loading-sources" | "extracting" | "review" | "error">("idle");
    const [progress, setProgress] = useState<{ read: number; total: number } | null>(null);
    const [taskStatusText, setTaskStatusText] = useState("");
    // 任务事件序号：onTaskUpdate 每次触发 +1（停止后仍触发），驱动迟到 taskId 补取消 effect。
    const [taskEventSeq, setTaskEventSeq] = useState(0);
    const [runError, setRunError] = useState<{ title: string } | null>(null);
    const [excludedUnits, setExcludedUnits] = useState<Array<{ unitId: string; title: string }>>([]);
    const [verified, setVerified] = useState<VerifiedSuggestionResult | null>(null);
    const [reviewPerson, setReviewPerson] = useState<VerifiedPerson | null>(null);
    const [stopNotice, setStopNotice] = useState<string | null>(null);
    const [run, setRun] = useState<SuggestionRun | null>(null);

    // 会话守卫跨渲染存活；AbortController 每轮新建。
    const guardRef = useRef(createSuggestionSessionGuard());
    const abortRef = useRef<AbortController | null>(null);
    const knownTaskIdRef = useRef<string | null>(null);

    // 章节摘要：面板展开时拉取，默认全选剧情章节（kind chapter/episode，position 排序）。
    const unitsQuery = useQuery({
        queryKey: ["project", projectId, "units"],
        queryFn: () => listProjectUnits(projectId),
        enabled: open,
        staleTime: 30_000,
    });
    const allUnits = useMemo(() => unitsQuery.data?.units || [], [unitsQuery.data]);
    const storyUnits = useMemo(
        () => allUnits
            .filter((unit) => unit.kind === "chapter" || unit.kind === "episode")
            .sort((a, b) => a.position - b.position)
            .map((unit) => ({ id: unit.id, title: unit.title, wordCount: unit.wordCount })),
        [allUnits],
    );
    useEffect(() => {
        if (!open) return;
        setSelectedUnitIds(defaultSelectedStoryUnits(allUnits));
    }, [open, allUnits]);

    // 模型默认值：配置就绪后初始化一次，之后尊重用户选择。
    useEffect(() => {
        setSelectedTextModel((current) => current || effectiveConfig.textModel || "");
    }, [effectiveConfig]);

    // 当前轮身份：项目/用户/编辑对象/姓名/范围任一变化即构成新身份（model 属发起前配置）。
    const currentIdentity: SuggestionRunIdentity = useMemo(() => ({
        projectId,
        userId,
        editorAssetId,
        characterName: characterName.trim(),
        selectedUnitIds,
        model: selectedTextModel,
    }), [projectId, userId, editorAssetId, characterName, selectedUnitIds, selectedTextModel]);
    const currentIdentityRef = useRef(currentIdentity);
    useEffect(() => {
        currentIdentityRef.current = currentIdentity;
    }, [currentIdentity]);
    // run 的最新值经 ref 提供给同步回调（startExtraction 继承 dirty 标记）。
    const runRef = useRef<SuggestionRun | null>(null);
    useEffect(() => {
        runRef.current = run;
    }, [run]);

    const running = phase === "loading-sources" || phase === "extracting";
    const nameReady = Boolean(characterName.trim());
    const modelReady = Boolean(selectedTextModel && isAiConfigReady(effectiveConfig, selectedTextModel));
    const rangeReady = selectedUnitIds.length > 0;
    const blockReason = !nameReady ? "请先输入角色名称" : !modelReady ? "请先选择可用的文本模型" : !rangeReady ? "请至少选择一个章节" : "";

    // 身份变化（关闭重开/改名/改范围/换项目/换用户/换编辑对象）：当前轮立即脱离表单。
    // 旧响应/旧错误到达时被守卫拒绝，不污染新会话（不依赖 isMounted 布尔量）。
    // 完整失效本轮 UI 状态（verified/reviewPerson/stopNotice/excludedUnits 一并清理）：
    // 只清 run 会留下「建议区仍展示但证据清零、门禁永久阻塞」的半失效状态（Fix Round 1）。
    useEffect(() => {
        if (!run) return;
        if (canUseSuggestionResult(run.identity, currentIdentity)) return;
        guardRef.current.invalidate();
        setRun(null);
        const empty = emptySuggestionSessionUi();
        setVerified(empty.verified);
        setReviewPerson(empty.reviewPerson);
        setStopNotice(empty.stopNotice);
        setExcludedUnits(empty.excludedUnits);
        if (abortRef.current && !abortRef.current.signal.aborted) {
            abortRef.current.abort();
            abortRef.current = null;
            setPhase("idle");
            setProgress(null);
            setTaskStatusText("");
        }
        // running 不进依赖：身份变化本身已构成失效。
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [currentIdentity]);

    // 弹窗关闭/编辑对象切换时彻底复位（父组件在 onClose 时调用）。
    // 同步把运行快照归零：会话失效后不再有运行中的提取，编辑器保存禁用必须解除。
    const invalidateSession = useCallback(() => {
        guardRef.current.invalidate();
        setRun(null);
        abortRef.current?.abort();
        abortRef.current = null;
        knownTaskIdRef.current = null;
        runningChannel.setRunning(false);
        setPhase("idle");
        setProgress(null);
        setTaskStatusText("");
        setRunError(null);
        setStopNotice(null);
        setVerified(null);
        setReviewPerson(null);
        setExcludedUnits([]);
    }, [runningChannel]);

    // matched 安全事实自动填充（design §4）：走 Task 1 已测的 computeSafeAutoFillFields
    // （matched + fact 唯一选项 + 字段空 + 未编辑）。填充的字段登记程序化写入（不误标用户编辑）
    // 并记录 AI 证据（供「已自动填充」标注与门禁判定）。
    const applySafeAutoFill = useCallback((person: VerifiedPerson, runState: SuggestionRun) => {
        const fill = computeSafeAutoFillFields({
            result: { status: "matched", people: [person] },
            formValues: form.getFieldsValue() as Record<string, string>,
            dirtyFields: runState.dirtyFields,
        });
        const keys = Object.keys(fill) as CharacterFieldKey[];
        if (!keys.length) return;
        keys.forEach((key) => programmaticWritesRef.current.add(key));
        form.setFieldsValue(fill);
        setRun((current) => (current ? recordAutoFillEvidence({ run: current, keys }) : current));
    }, [form]);

    // 提取入口：按钮禁用 + beginSuggestionRun 新 token（旧轮立即失效）共同防双击。
    const startExtraction = useCallback(async () => {
        if (running || !nameReady || !modelReady || !rangeReady) return;
        const controller = new AbortController();
        abortRef.current = controller;
        knownTaskIdRef.current = null;
        const guard = guardRef.current;
        // 新一轮继承上一轮的字段编辑标记：用户清空/改写过的字段重提取时不回写 AI 值（design §4）。
        const nextRun = beginRunInheritingDirty({ guard, identity: currentIdentityRef.current, previousRun: runRef.current });
        programmaticWritesRef.current = new Set();
        setRun(nextRun);
        setStopNotice(null);
        setRunError(null);
        setExcludedUnits([]);
        setVerified(null);
        setReviewPerson(null);
        setProgress({ read: 0, total: selectedUnitIds.length });
        setPhase("loading-sources");
        try {
            const result: CharacterSuggestionRun = await extractCharacterSuggestions(
                {
                    projectId,
                    characterName: characterName.trim(),
                    selectedUnitIds,
                    model: selectedTextModel,
                    config: effectiveConfig,
                    signal: controller.signal,
                    onProgress: ({ read, total }) => setProgress({ read, total }),
                    onTaskUpdate: (task) => {
                        if (task.id) knownTaskIdRef.current = task.id;
                        // 停止后到达的任务状态更新不再覆盖当前文案（本地会话已失效），
                        // 但事件序号仍推进，驱动迟到 taskId 补取消。
                        setTaskEventSeq((seq) => seq + 1);
                        if (controller.signal.aborted) return;
                        // 读取完成收到首个任务事件即进入 extracting（「模型分析中」真实展示）。
                        setPhase((current) => phaseAfterTaskUpdate({ phase: current, task }));
                        if (!controller.signal.aborted) setTaskStatusText(taskStatusLabel(task.status));
                    },
                },
                defaultCharacterSuggestionDependencies,
            );
            // 应用前再核对会话与身份：已停止/旧轮/身份漂移的结果一律丢弃。
            if (controller.signal.aborted) return;
            if (!guard.isCurrent(nextRun.token)) return;
            if (!canUseSuggestionResult(nextRun.identity, currentIdentityRef.current)) return;
            setVerified(result.verified);
            setExcludedUnits(result.excludedUnits);
            setPhase("review");
            if (result.verified.status === "matched" && result.verified.people[0]) {
                const person = result.verified.people[0];
                setReviewPerson(person);
                applySafeAutoFill(person, nextRun);
            }
        } catch (error) {
            // 停止/身份切换导致的终止不上报为当前轮失败。
            if (controller.signal.aborted) return;
            if (!guard.isCurrent(nextRun.token)) return;
            setRunError({ title: error instanceof Error ? error.message : "提取失败" });
            setPhase("error");
        } finally {
            if (guard.isCurrent(nextRun.token)) {
                setPhase((current) => (current === "loading-sources" || current === "extracting" ? "idle" : current));
                setProgress(null);
                setTaskStatusText("");
                abortRef.current = null;
            }
        }
    }, [running, nameReady, modelReady, rangeReady, projectId, characterName, selectedUnitIds, selectedTextModel, effectiveConfig, applySafeAutoFill]);

    // 停止序列（character-suggestion-session.createStopController）：
    // 失效守卫 → abort 读取/监听 → 对已知 taskId 调后端取消接口；取消失败只如实显示。
    // 迟到 taskId（停止后 onTaskUpdate 才到达）由 effect 经 stopRef.reportLateTaskId 补取消：
    // 单一防重来源（与停止时的取消共享 canceledTaskIds），不再自建去重集合。
    const stopRef = useRef<ReturnType<typeof createStopController> | null>(null);
    const stopExtraction = useCallback(() => {
        const controller = abortRef.current;
        if (!controller) return;
        const guard = guardRef.current;
        const knownTaskId = knownTaskIdRef.current || undefined;
        const stop = createStopController({
            guard,
            runToken: 0,
            signal: controller.signal,
            abort: () => controller.abort(),
            cancelTask: (taskId: string) => cancelGenerationTask(taskId),
        });
        stopRef.current = stop;
        void stop({ taskId: knownTaskId, onEvent: (event) => {
            if (event.kind === "cancel-failed") setStopNotice(`任务取消请求失败：${event.message}。本地结果已丢弃，任务可在任务中心查看。`);
        } });
        setRun(null);
        setPhase("idle");
        setProgress(null);
        setTaskStatusText("");
    }, []);

    // 迟到 taskId：停止（signal 已 abort）后 onTaskUpdate 仍可能写入 taskId —— 补发取消。
    // 未收到 taskId 的请求不能假定尚未创建（design §4）。防重由 stop 控制器的
    // canceledTaskIds 单轨承担（与停止时的取消共享），避免双轨重复取消请求。
    useEffect(() => {
        const controller = abortRef.current;
        const taskId = knownTaskIdRef.current;
        const stop = stopRef.current;
        if (!controller || !controller.signal.aborted || !taskId || !stop) return;
        const reported = stop.reportLateTaskId(taskId);
        if (reported.cancelled) return;
        // 取消失败由 stop 控制器的 cancel-failed 事件统一上报（onEvent 已接 setStopNotice）。
    }, [taskEventSeq]);

    // 单字段采纳/替换/冲突选择/暂不采用：写表单 + 记录证据 + 标记 dirty（用户主动事件）。
    // 应用决策走 session.ts 已测的 applyAdoptionToDraft（含越界 optionIndex 防御）。
    const applyAdoption = useCallback((fieldKey: CharacterFieldKey, optionIndex: number, action: SuggestionAdoptionAction) => {
        if (!reviewPerson || !run) return;
        const field = reviewPerson.fields[fieldKey];
        const applied = applyAdoptionToDraft({ draft: form.getFieldsValue() as Record<string, string>, fieldKey, field, optionIndex, action });
        if (action !== "dismiss") {
            // 采纳写入是程序化写表单：登记后观察 effect 不把它误标为用户编辑。
            programmaticWritesRef.current.add(fieldKey);
        }
        setRun((current) => (current ? {
            ...current,
            dirtyFields: { ...current.dirtyFields, [fieldKey]: true },
            evidence: { ...current.evidence, ...applied.evidence },
        } : current));
        if (action === "dismiss") return;
        form.setFieldsValue(applied.values);
    }, [reviewPerson, run, form]);

    // 歧义人物选定（design §6）：进入该人物的可审核状态并回写姓名（展示给用户确认，
    // 不静默替换）。新轮运行身份绑定「回写后的姓名」：回写触发的身份重算与该身份一致，
    // 漂移 effect 不会把它当漂移清理；新 run 让本人物的字段建议可正常采纳、门禁可解除
    // （此前选完人物后 verified 停留 ambiguous、run 被清空，形成交互死路——Fix Round 1）。
    // 开新轮必须继承歧义轮已积累的 dirty 标记（Fix Round 2 · Important 2）：
    // 提取中编辑/清空的字段在重提取时同样不得被 applySafeAutoFill 静默重写。
    const chooseAmbiguousPerson = useCallback((person: VerifiedPerson) => {
        setReviewPerson(person);
        const identity: SuggestionRunIdentity = { ...currentIdentityRef.current, characterName: person.name.trim() };
        setRun(beginRunInheritingDirty({ guard: guardRef.current, identity, previousRun: runRef.current }));
        if (characterName.trim() !== person.name) form.setFieldsValue({ name: person.name });
    }, [characterName, form]);

    // 保存门禁注册给编辑器：走 session.ts 已测的 decideSaveGate / isReviewResolved
    // （含提取运行中禁用与身份歧义分支；建议流程未开启时放行原保存）。
    // 审核对象来自 matched 或用户已确认的 ambiguous 人物（design §6）。
    const reviewGate = useCallback((): string => {
        const suggestionActive = Boolean(run);
        const identityChosen = !verified || verified.status !== "ambiguous" || Boolean(reviewPerson);
        const pending = reviewPerson
            ? characterFields.filter(([key]) => reviewPerson.fields[key as CharacterFieldKey].status !== "unspecified" && !run?.evidence?.[key as CharacterFieldKey]).length
            : 0;
        const gate = decideSaveGate({
            suggestionActive: suggestionActive || Boolean(reviewPerson),
            running,
            identityChosen,
            reviewResolved: isReviewResolved(pending, run?.evidence || {}),
        });
        return gate.allowed ? "" : gate.reason || "建议审核未完成，暂不能保存";
    }, [run, reviewPerson, verified, running]);
    // API 注册：门禁与运行通道随审核状态更新，失效句柄保持稳定引用。
    // 通道由编辑器创建并订阅（props 注入），随 api 对象携带：api 对象替换
    // （useMemo 依赖翻转生成新对象）不会孤儿化订阅（Fix Round 2 · Important 1）。
    const api = useMemo<CharacterSuggestionPanelApi>(
        () => ({ reviewGate, invalidateSession, runningChannel }),
        [reviewGate, invalidateSession, runningChannel],
    );
    useEffect(() => {
        onApiReady(api);
    }, [onApiReady, api]);
    // 运行状态快照：经稳定通道同步给编辑器（useSyncExternalStore 订阅方触发保存按钮禁用）。
    useEffect(() => {
        runningChannel.setRunning(running);
    }, [running, runningChannel]);

    const reviewFields = useMemo(() => {
        if (!reviewPerson) return [];
        return characterFields.map(([key, label]) => ({
            key: key as CharacterFieldKey,
            label,
            field: reviewPerson.fields[key as CharacterFieldKey],
            autoFilled: Boolean(run?.evidence?.[key as CharacterFieldKey]) && !run?.dirtyFields[key as CharacterFieldKey],
        }));
    }, [reviewPerson, run]);

    // 阶段状态文案（aria-live）：读取 n/N、模型分析中 + 任务状态（session.ts 已测纯函数）。
    const statusText = suggestionStatusText({ phase, progress, taskStatusText });

    return (
            <Collapse
                ghost
                activeKey={open ? ["suggestion"] : []}
                onChange={(keys) => setOpen(Array.isArray(keys) ? keys.includes("suggestion") : keys === "suggestion")}
                items={[{
                    key: "suggestion",
                    label: (
                        <span className="flex min-w-0 flex-wrap items-center gap-1.5 text-[var(--fs-label)] font-medium text-foreground/78">
                            <Sparkles className="size-3.5 shrink-0" aria-hidden="true" />
                            从剧本提取
                            <span className="text-[var(--fs-tiny)] font-normal text-foreground/45">AI 建议，确认保存前不会创建角色</span>
                        </span>
                    ),
                    extra: running ? (
                        <Button type="text" size="small" icon={<Square className="size-3.5" />} onClick={(event) => { event.stopPropagation(); stopExtraction(); }}>停止</Button>
                    ) : null,
                    children: (
                        <div className="grid gap-3 pb-2" aria-live="polite">
                            {!modelReady ? (
                                <p className="text-[var(--fs-tiny)] text-foreground/48">
                                    未配置可用文本模型，<button type="button" className="underline underline-offset-2" onClick={() => navigateToSettings({ continueCreation: true })}>前往设置</button>。
                                </p>
                            ) : null}
                            <label className="block">
                                <span className="mb-1.5 block text-xs font-medium text-foreground/68">文本模型</span>
                                <ModelPicker config={effectiveConfig} capability="text" value={selectedTextModel} onChange={setSelectedTextModel} variant="creation" fullWidth placeholder="选择用于整理角色设定的文本模型" showSelectedPrice={false} onMissingConfig={() => navigateToSettings({ continueCreation: true })} />
                            </label>
                            <fieldset className="min-w-0 rounded-md border border-border/70 p-2.5" disabled={running}>
                                <legend className="px-1 text-[var(--fs-tiny)] text-foreground/52">章节范围（默认全部剧情章节）</legend>
                                {unitsQuery.isLoading ? <Spin size="small" /> : storyUnits.length ? (
                                    <div className="max-h-32 overflow-y-auto overscroll-contain" data-canvas-wheel-scroll>
                                        {storyUnits.map((unit) => (
                                            <label key={unit.id} className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-[var(--fs-label)] hover:bg-surface-hover">
                                                <input
                                                    type="checkbox"
                                                    checked={selectedUnitIds.includes(unit.id)}
                                                    onChange={() => setSelectedUnitIds((current) => appendUnitIds(current, unit.id))}
                                                />
                                                <span className="min-w-0 flex-1 truncate">{unit.title}</span>
                                                {unit.wordCount ? <span className="shrink-0 text-[var(--fs-micro)] tabular-nums text-foreground/38">{unit.wordCount} 字</span> : <span className="shrink-0 text-[var(--fs-micro)] text-foreground/38">无正文</span>}
                                            </label>
                                        ))}
                                    </div>
                                ) : <p className="text-[var(--fs-tiny)] text-foreground/45">当前项目还没有剧情章节。</p>}
                                <p className="mt-1.5 text-[var(--fs-tiny)] text-foreground/45">
                                    实际范围：{selectedUnitIds.length ? `${selectedUnitIds.length} 个章节` : "未选择"}；提取基于本次读取的已保存正文。
                                </p>
                            </fieldset>
                            {statusText ? <p className="text-[var(--fs-tiny)] text-foreground/60">{statusText}</p> : null}
                            {stopNotice ? <p role="status" className="text-[var(--fs-tiny)] text-foreground/70">{stopNotice}</p> : null}
                            {runError ? (
                                <div className="rounded-md border border-border/70 bg-foreground/[.03] p-2.5">
                                    <p className="text-[var(--fs-label)] font-medium text-foreground/80">{runError.title}</p>
                                    <p className="mt-1 text-[var(--fs-tiny)] text-foreground/50">可以调整后重新提取，或继续手工填写。</p>
                                </div>
                            ) : null}
                            {excludedUnits.length ? (
                                <p className="text-[var(--fs-tiny)] text-foreground/45">未分析（无正文）：{excludedUnits.map((unit) => unit.title).join("、")}</p>
                            ) : null}
                            {verified?.status === "not_found" ? (
                                <p className="text-[var(--fs-label)] text-foreground/62">本次范围内没有找到「{characterName}」，可以调整姓名、扩大范围或手工填写。</p>
                            ) : null}
                            {verified?.status === "ambiguous" ? (
                                <div className="rounded-md border border-border/70 bg-foreground/[.03] p-2.5">
                                    <p className="text-[var(--fs-label)] font-medium text-foreground/80">存在多个同名/近名人物，请先确认要整理谁：</p>
                                    <ul className="mt-1.5 grid gap-1.5">
                                        {verified.people.map((person) => (
                                            <li key={`${person.name}-${person.identitySummary}`} className="flex flex-wrap items-center justify-between gap-2">
                                                <span className="min-w-0">
                                                    <span className="text-[var(--fs-label)] font-medium">{person.name}</span>
                                                    <span className="ml-2 text-[var(--fs-tiny)] text-foreground/50">{person.identitySummary}</span>
                                                </span>
                                                <Button type="text" size="small" onClick={() => chooseAmbiguousPerson(person)}>整理这位</Button>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            ) : null}
                            {reviewPerson && (verified?.status === "matched" || verified?.status === "ambiguous") ? (
                                <div className="grid gap-2">
                                    <div className="rounded-md bg-foreground/[.03] p-2.5">
                                        <p className="text-[var(--fs-label)] font-medium">{reviewPerson.name}</p>
                                        <p className="mt-0.5 text-[var(--fs-tiny)] text-foreground/50">{reviewPerson.identitySummary}</p>
                                        <p className="mt-1 text-[var(--fs-micro)] text-foreground/38">身份依据：{reviewPerson.identityEvidence.map((item) => `「${item.unitTitle}」`).join("、")}</p>
                                    </div>
                                    {reviewFields.map(({ key, label, field, autoFilled }) => (
                                        <SuggestionFieldRow
                                            key={key}
                                            label={label}
                                            field={field}
                                            formValue={String(form.getFieldValue(key) ?? "")}
                                            dirty={Boolean(run?.dirtyFields[key])}
                                            evidence={run?.evidence?.[key]}
                                            autoFilled={autoFilled}
                                            onAdopt={(optionIndex, action) => applyAdoption(key, optionIndex, action)}
                                        />
                                    ))}
                                </div>
                            ) : null}
                            {!running ? (
                                <div>
                                    <Button type="default" size="small" disabled={!nameReady || !modelReady || !rangeReady} title={blockReason || undefined} onClick={() => void startExtraction()}>
                                        开始提取
                                    </Button>
                                    {blockReason ? <p className="mt-1 text-[var(--fs-tiny)] text-foreground/45">{blockReason}</p> : null}
                                </div>
                            ) : null}
                        </div>
                    ),
                }]}
            />
    );
}

// 单字段建议行：状态标签、建议值、来源展开、采纳/替换/暂不采用操作。
// autoFilled：本轮由系统安全填充且用户未改写——给出与手动采用不同的视觉标注（Minor #8）。
function SuggestionFieldRow({ label, field, formValue, dirty, evidence, autoFilled, onAdopt }: {
    label: string;
    field: VerifiedPerson["fields"][CharacterFieldKey];
    formValue: string;
    dirty: boolean;
    evidence?: { origin: "ai"; dismissed: boolean };
    autoFilled: boolean;
    onAdopt: (optionIndex: number, action: SuggestionAdoptionAction) => void;
}) {
    const review = fieldReview({ field, formValue, dirty });
    const basisLabel = { fact: "剧本依据", inference: "AI 推断", production: "制作建议" } as const;
    const valueText = (option: { value: string | string[] }) => typeof option.value === "string" ? option.value : option.value.join("，");
    if (field.status === "unspecified") {
        return (
            <div className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5" style={{ background: "var(--surface-hover)" }}>
                <span className="text-[var(--fs-label)] text-foreground/55">{label}</span>
                <span className="text-[var(--fs-tiny)] text-foreground/38">正文未明确</span>
            </div>
        );
    }
    if (field.status === "conflict") {
        return (
            <div className="rounded-md px-2 py-1.5" style={{ background: "var(--surface-hover)" }}>
                <div className="flex items-center justify-between gap-2">
                    <span className="text-[var(--fs-label)] text-foreground/68">{label}</span>
                    {evidence?.dismissed ? <Tag bordered={false}>已暂不采用</Tag> : null}
                </div>
                <p className="mt-1 text-[var(--fs-tiny)] text-foreground/50">不同章节描述不一致，请选择一项：</p>
                <div className="mt-1 grid gap-1.5">
                    {field.options.map((option, index) => (
                        <div key={index} className="flex flex-wrap items-center justify-between gap-2 rounded border border-border/60 px-2 py-1.5">
                            <span className="min-w-0"><span className="text-[var(--fs-label)]">{valueText(option)}</span> <Tag bordered={false}>{basisLabel[option.basis]}</Tag></span>
                            <Button type="text" size="small" onClick={() => onAdopt(index, "choose")}>选这项</Button>
                        </div>
                    ))}
                    <Button type="text" size="small" className="justify-self-start" onClick={() => onAdopt(0, "dismiss")}>暂不采用</Button>
                </div>
            </div>
        );
    }
    const option = field.options[0];
    if (!option) return null;
    const evidenceList = option.evidence;
    return (
        <div className="rounded-md px-2 py-1.5" style={{ background: "var(--surface-hover)" }}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-[var(--fs-label)] text-foreground/68">{label}</span>
                <span className="flex flex-wrap items-center gap-1">
                    {autoFilled && !dirty ? <Tag bordered={false} color="blue">已自动填充</Tag> : null}
                    {evidence?.dismissed ? <Tag bordered={false}>已暂不采用</Tag> : null}
                    {evidence && !evidence.dismissed && dirty ? <Tag bordered={false}>AI 原建议依据</Tag> : null}
                </span>
            </div>
            <div className="mt-1 flex flex-wrap items-start justify-between gap-2">
                <span className="min-w-0"><span className="text-[var(--fs-label)]">{valueText(option)}</span> <Tag bordered={false}>{basisLabel[option.basis]}</Tag></span>
                {review.action === "replace" ? <Button type="text" size="small" onClick={() => onAdopt(0, "replace")}>替换</Button> : null}
                {review.action === "adopt" ? <Button type="text" size="small" onClick={() => onAdopt(0, "adopt")}>采用</Button> : null}
            </div>
            {evidenceList.length ? (
                <Collapse ghost size="small" className="mt-1" items={[{
                    key: "evidence",
                    label: <span className="text-[var(--fs-tiny)] text-foreground/45">来源（{evidenceList.length}）</span>,
                    children: (
                        <ul className="grid gap-1">
                            {evidenceList.map((item, index) => (
                                <li key={index} className="text-[var(--fs-tiny)] leading-5 text-foreground/55">
                                    <span className="text-foreground/40">{item.unitTitle}：</span>「{item.quote}」
                                </li>
                            ))}
                        </ul>
                    ),
                }]} />
            ) : null}
        </div>
    );
}

// 任务状态到可读文案（仅展示用途）。
function taskStatusLabel(status: string): string {
    if (status === "queued") return "排队中";
    if (status === "running") return "生成中";
    if (status === "succeeded") return "已完成";
    if (status === "failed") return "失败";
    if (status === "cancelled") return "已取消";
    return status;
}
