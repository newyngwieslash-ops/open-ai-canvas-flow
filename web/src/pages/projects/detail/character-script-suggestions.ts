// 角色剧本建议的纯逻辑模块：字段清单、建议合同类型、严格解析、引用核验、查重与安全填充决策。
// 本模块不依赖 React、AntD 或网络请求，页面 UI 与校验共用同一份字段清单。
// 旧章节完整角色解析器（canvas-character-reference.ts 的 parseCharacterBreakdown）不复用于本功能。

// 姓名规范化复用画布角色引用的现有实现，保证查重与建议两侧口径一致。
import { normalizeCharacterName } from "@/lib/canvas/canvas-character-reference";

// 与 assets.tsx 角色表单完全一致的字段清单：[字段键, 中文标签]。
// 字段键直接来自现有角色表单，UI 显示与建议解析共用，避免两套可漂移的列表。
export const characterFields = [
    ["role", "剧情定位与人物关系"], ["aliases", "别名"], ["appearance", "稳定外貌"], ["physique", "身高、体型与体态"],
    ["clothing", "默认服装造型"], ["personality", "性格与表演基线"], ["props", "固定道具"],
    ["consistencyPrompt", "跨镜头一致性约束"], ["multiViewPrompt", "三视图补充约束"],
    ["voiceLanguage", "语言与口音"], ["voiceAge", "声音年龄感"], ["voiceTimbre", "音色气质"],
] as const;

export type CharacterFieldKey = (typeof characterFields)[number][0];

// 字段键列表（顺序与 characterFields 一致），供解析层核验"12 字段齐全"与白名单。
export const characterFieldKeys: CharacterFieldKey[] = characterFields.map(([key]) => key);

// 角色表单值：aliases 与其他字段在表单里都以字符串承载（逗号分隔），保存时再转换。
export type CharacterForm = { name: string } & Record<CharacterFieldKey, string>;

// ---------- 建议合同（对应 design.md §3） ----------

// 引用：指向本轮快照中某个章节的一段原文。
export type Evidence = { unitId: string; quote: string };

export type SuggestionBasis = "fact" | "inference" | "production";

// 一个候选字段值及其依据。aliases 字段的 value 是 string[]，其余字段是 string。
export type SuggestionOption<T extends string | string[] = string | string[]> = {
    value: T;
    basis: SuggestionBasis;
    evidence: Evidence[];
};

export type SuggestedFieldStatus = "suggested" | "unspecified" | "conflict";

export type SuggestedField =
    | { status: "suggested"; options: [SuggestionOption] }
    | { status: "unspecified"; options: [] }
    | { status: "conflict"; options: [SuggestionOption, SuggestionOption, ...SuggestionOption[]] };

export type Person = {
    name: string;
    aliases: string[];
    identitySummary: string;
    identityEvidence: Evidence[];
    fields: Record<CharacterFieldKey, SuggestedField>;
};

export type SuggestionResultStatus = "matched" | "not_found" | "ambiguous";

export type SuggestionResult = {
    status: SuggestionResultStatus;
    people: Person[];
};

// ---------- 严格解析与合同校验 ----------

// 数量上限：超限直接拒绝整轮，不静默截断（design §3"不得静默截断超限结果"）。
const LIMITS = {
    name: 100,
    alias: 60,
    aliases: 10,
    identitySummary: 300,
    identityEvidence: 5,
    quote: 500,
    evidencePerOption: 5,
    optionValue: 1000,
    optionsPerField: 10,
    people: 20,
};

const RESULT_STATUSES: readonly string[] = ["matched", "not_found", "ambiguous"];
const FIELD_STATUSES: readonly string[] = ["suggested", "unspecified", "conflict"];
const OPTION_BASES: readonly string[] = ["fact", "inference", "production"];

// 三视图/一致性约束默认属于制作建议；模型若把它标成 fact，按制作建议处理而非拒绝。
const PRODUCTION_FIELD_KEYS = new Set<string>(["consistencyPrompt", "multiViewPrompt"]);

// 与旧表单一致：aliases 在表单/写入时按中英文逗号拆分（assets.tsx characterDefinition）。
export function aliasesArrayToFormValue(aliases: string[]) {
    return aliases.join("，");
}

// 与旧表单一致：把逗号分隔的表单值转回数组。
export function aliasesFormValueToArray(value: string) {
    return value.split(/[，,]/).map((item) => item.trim()).filter(Boolean);
}

function fail(message: string): never {
    throw new Error(message);
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

// 只做整体形态处理：裸 JSON 或整体 ```json 围栏。不做正文夹带提取，不做宽松纠错。
function parsePayload(raw: string): Record<string, unknown> {
    let text = raw.trim();
    const fence = text.match(/^```(?:json)?\s*\n?([\s\S]*?)\n?\s*```$/i);
    if (fence) text = fence[1].trim();
    let parsed: unknown;
    try {
        parsed = JSON.parse(text);
    } catch {
        return fail("建议结果不是合法 JSON：需要纯 JSON 对象或整体 ```json 围栏");
    }
    if (!isRecord(parsed)) return fail("建议结果必须是 JSON 对象");
    return parsed;
}

function asNonEmptyString(value: unknown, label: string, limit: number): string {
    if (typeof value !== "string") return fail(`${label} 必须是字符串`);
    const trimmed = value.trim();
    if (!trimmed) return fail(`${label} 不能为空`);
    if (trimmed.length > limit) return fail(`${label} 长度超出上限 ${limit}`);
    return trimmed;
}

function asEvidence(value: unknown, label: string): Evidence {
    if (!isRecord(value)) return fail(`${label} 必须是对象`);
    if (!("unitId" in value) || !("quote" in value)) return fail(`${label} 缺少 unitId 或 quote`);
    return {
        unitId: asNonEmptyString(value.unitId, `${label}.unitId`, 200),
        quote: asNonEmptyString(value.quote, `${label}.quote`, LIMITS.quote),
    };
}

function asEvidenceList(value: unknown, label: string, limit: number): Evidence[] {
    if (!Array.isArray(value)) return fail(`${label} 必须是数组`);
    if (value.length > limit) return fail(`${label} 数量超出上限 ${limit}`);
    return value.map((item, index) => asEvidence(item, `${label}[${index}]`));
}

function normalizeAliases(value: unknown, name: string): string[] {
    if (!Array.isArray(value)) return fail("aliases 必须是字符串数组");
    if (value.length > LIMITS.aliases) return fail(`aliases 数量超出上限 ${LIMITS.aliases}`);
    const nameKey = normalizeCharacterName(name);
    const seen = new Set<string>();
    const result: string[] = [];
    for (const item of value) {
        if (typeof item !== "string") return fail("aliases 必须是字符串数组");
        const alias = item.trim();
        if (!alias) continue;
        if (alias.length > LIMITS.alias) return fail(`别名长度超出上限 ${LIMITS.alias}`);
        const key = normalizeCharacterName(alias);
        // 与正式姓名同名的别名没有信息量，直接丢弃；其余按规范化结果去重。
        if (key === nameKey) continue;
        if (seen.has(key)) continue;
        seen.add(key);
        result.push(alias);
    }
    return result;
}

function asOption(fieldKey: string, value: unknown, index: number): SuggestionOption {
    if (!isRecord(value)) return fail(`字段 ${fieldKey} 的选项 ${index} 必须是对象`);
    if (value.value === undefined) return fail(`字段 ${fieldKey} 的选项 ${index} 缺少 value`);
    const basis = value.basis;
    if (typeof basis !== "string" || !OPTION_BASES.includes(basis)) return fail(`字段 ${fieldKey} 的选项 ${index} basis 非法`);
    const evidence = asEvidenceList(value.evidence, `字段 ${fieldKey} 的选项 ${index}.evidence`, LIMITS.evidencePerOption);
    // fact/inference 必须有至少一个有效引用；production（制作建议）可以不引原文。
    if (basis !== "production" && !evidence.length) return fail(`字段 ${fieldKey} 的选项 ${index}（${basis}）必须提供至少一条原文引用`);
    let optionValue: string | string[];
    if (fieldKey === "aliases") {
        optionValue = normalizeAliases(value.value, "");
        if (!optionValue.length) return fail(`字段 aliases 的选项 ${index} value 不能为空`);
    } else {
        optionValue = asNonEmptyString(value.value, `字段 ${fieldKey} 的选项 ${index}.value`, LIMITS.optionValue);
    }
    // 三视图/一致性约束是制作建议字段；伪装成 fact 时降级为 production 并保留引用，不自动当事实采纳。
    const finalBasis: SuggestionBasis = PRODUCTION_FIELD_KEYS.has(fieldKey) && basis === "fact" ? "production" : (basis as SuggestionBasis);
    return { value: optionValue, basis: finalBasis, evidence };
}

function asSuggestedField(fieldKey: string, value: unknown): SuggestedField {
    if (!isRecord(value)) return fail(`字段 ${fieldKey} 必须是对象`);
    const status = value.status;
    if (typeof status !== "string" || !FIELD_STATUSES.includes(status)) return fail(`字段 ${fieldKey} 的 status 非法`);
    if (!Array.isArray(value.options)) return fail(`字段 ${fieldKey} 的 options 必须是数组`);
    if (value.options.length > LIMITS.optionsPerField) return fail(`字段 ${fieldKey} 的 options 数量超出上限 ${LIMITS.optionsPerField}`);
    const options = value.options.map((item, index) => asOption(fieldKey, item, index));
    // 状态基数（design §3）：suggested 恰一个选项；unspecified 的 options 必须为空；conflict 至少两个有依据的选项。
    if (status === "suggested" && options.length !== 1) return fail(`字段 ${fieldKey} 状态为 suggested 时必须恰好一个选项`);
    if (status === "unspecified") {
        if (options.length) return fail(`字段 ${fieldKey} 状态为 unspecified 时 options 必须为空`);
        return { status, options: [] };
    }
    if (status === "conflict" && options.length < 2) return fail(`字段 ${fieldKey} 状态为 conflict 时必须至少两个选项`);
    return (status === "suggested" ? { status, options: [options[0]] } : { status: "conflict", options }) as SuggestedField;
}

function asPerson(value: unknown): Person {
    if (!isRecord(value)) return fail("人物必须是对象");
    const unknownKeys = Object.keys(value).filter((key) => !["name", "aliases", "identitySummary", "identityEvidence", "fields"].includes(key));
    if (unknownKeys.length) return fail(`人物包含未知字段：${unknownKeys.join("、")}`);
    const name = asNonEmptyString(value.name, "人物 name", LIMITS.name);
    const aliases = normalizeAliases(value.aliases, name);
    const identitySummary = asNonEmptyString(value.identitySummary, "身份摘要", LIMITS.identitySummary);
    const identityEvidence = asEvidenceList(value.identityEvidence, "身份引用", LIMITS.identityEvidence);
    if (!identityEvidence.length) return fail(`人物「${name}」必须提供至少一条身份引用`);
    if (!isRecord(value.fields)) return fail(`人物「${name}」缺少 fields`);
    const fieldsRecord = value.fields as Record<string, unknown>;
    const unknownFieldKeys = Object.keys(fieldsRecord).filter((key) => !characterFieldKeys.includes(key as CharacterFieldKey));
    if (unknownFieldKeys.length) return fail(`人物「${name}」的 fields 包含未知字段：${unknownFieldKeys.join("、")}`);
    const missingKeys = characterFieldKeys.filter((key) => !(key in fieldsRecord));
    if (missingKeys.length) return fail(`人物「${name}」的 fields 缺少 ${missingKeys.length} 个字段（需 12 个字段齐全）：${missingKeys.join("、")}`);
    const fields = {} as Record<CharacterFieldKey, SuggestedField>;
    for (const key of characterFieldKeys) fields[key] = asSuggestedField(key, fieldsRecord[key]);
    return { name, aliases, identitySummary, identityEvidence, fields };
}

// 解析模型输出：只接受纯 JSON 或整体 ```json 围栏，执行完整合同校验。
// 任一合同错误抛错拒绝整轮，不部分采纳（调用方据此走重试/手填路径）。
export function parseCharacterSuggestions(raw: string): SuggestionResult {
    const payload = parsePayload(raw);
    const status = payload.status;
    if (typeof status !== "string" || !RESULT_STATUSES.includes(status)) return fail("建议结果 status 非法");
    if (!Array.isArray(payload.people)) return fail("建议结果 people 必须是数组");
    if (payload.people.length > LIMITS.people) return fail(`people 数量超出上限 ${LIMITS.people}`);
    const people = payload.people.map((item, index) => {
        try {
            return asPerson(item);
        } catch (error) {
            return fail(`第 ${index + 1} 个人物不合法：${error instanceof Error ? error.message : String(error)}`);
        }
    });
    if (status === "matched" && people.length !== 1) return fail("status 为 matched 时必须恰好一人");
    if (status === "ambiguous" && people.length < 2) return fail("status 为 ambiguous 时必须至少两人");
    if (status === "not_found" && people.length) return fail("status 为 not_found 时 people 必须为空数组");
    return { status: status as SuggestionResultStatus, people };
}

// ---------- 快照与引用核验 ----------

// 本轮读取冻结的章节快照：标题与正文都来自实际 GET 的已保存版本，模型标题不参与核验。
export type SuggestionSnapshotUnit = {
    unitId: string;
    title: string;
    text: string;
};

export type SuggestionSnapshot = {
    projectId: string;
    createdAt: string;
    units: SuggestionSnapshotUnit[];
};

// 带核验结果的引用：unitTitle 由本地快照提供，UI 展示章节来源时只信这里。
export type VerifiedEvidence = Evidence & { unitTitle: string };

export type VerifiedOption<T extends string | string[] = string | string[]> = {
    value: T;
    basis: SuggestionBasis;
    evidence: VerifiedEvidence[];
};

export type VerifiedSuggestedField =
    | { status: "suggested"; options: [VerifiedOption] }
    | { status: "unspecified"; options: [] }
    | { status: "conflict"; options: [VerifiedOption, VerifiedOption, ...VerifiedOption[]] };

export type VerifiedPerson = {
    name: string;
    aliases: string[];
    identitySummary: string;
    identityEvidence: VerifiedEvidence[];
    fields: Record<CharacterFieldKey, VerifiedSuggestedField>;
};

export type VerifiedSuggestionResult = {
    status: SuggestionResultStatus;
    people: VerifiedPerson[];
};

// 引用核验只统一 CRLF/LF，不做模糊匹配：quote 必须逐字出现在该章节纯文本中。
function normalizeQuoteText(text: string) {
    return text.replace(/\r\n/g, "\n");
}

function verifyEvidence(evidence: Evidence, units: Map<string, SuggestionSnapshotUnit>, label: string): VerifiedEvidence {
    const unit = units.get(evidence.unitId);
    if (!unit) return fail(`${label} 指向的章节 ${evidence.unitId} 不在本轮快照中`);
    if (!normalizeQuoteText(unit.text).includes(normalizeQuoteText(evidence.quote))) {
        return fail(`${label} 的原文与章节「${unit.title}」不符`);
    }
    return { ...evidence, unitTitle: unit.title };
}

function verifyOption(fieldKey: string, option: SuggestionOption, units: Map<string, SuggestionSnapshotUnit>): VerifiedOption {
    const evidence = option.evidence.map((item, index) => verifyEvidence(item, units, `字段 ${fieldKey} 的引用 ${index + 1}`));
    return { value: option.value, basis: option.basis, evidence };
}

// 对解析完成的建议执行引用核验：unitId 必须在本轮快照中、quote 必须逐字出现在该章节纯文本。
// 任一引用无效抛错拒绝整轮，调用方不得部分采纳。
export function verifySuggestionEvidence(result: SuggestionResult, snapshot: SuggestionSnapshot): VerifiedSuggestionResult {
    const units = new Map(snapshot.units.map((unit) => [unit.unitId, unit]));
    const people = result.people.map((person) => {
        const label = `人物「${person.name}」`;
        const identityEvidence = person.identityEvidence.map((item, index) => verifyEvidence(item, units, `${label}的身份引用 ${index + 1}`));
        const fields = {} as Record<CharacterFieldKey, VerifiedSuggestedField>;
        for (const key of characterFieldKeys) {
            const field = person.fields[key];
            if (field.status === "unspecified") {
                fields[key] = field;
                continue;
            }
            const options = field.options.map((option) => verifyOption(key, option, units));
            fields[key] = (field.status === "suggested"
                ? { status: "suggested", options: [options[0]] }
                : { status: "conflict", options }) as VerifiedSuggestedField;
        }
        return { ...person, identityEvidence, fields };
    });
    return { status: result.status, people };
}

// ---------- 重名/别名匹配 ----------

// 已有角色的最小描述：调用方负责按 category=character 分页拉取全量后传入。
export type ExistingCharacterRef = {
    id: string;
    title: string;
    aliases?: string[];
};

// 按规范化姓名/别名与已有角色的 title/aliases 比对，返回全部命中的角色。
// 查重只是 UI 提示而非数据库唯一约束；分页、失败重试由调用方处理。
export function findMatchingExistingCharacters(
    name: string,
    existing: ExistingCharacterRef[],
    excludeCharacterId?: string,
): ExistingCharacterRef[] {
    const inputKey = normalizeCharacterName(name);
    if (!inputKey) return [];
    return existing.filter((character) => {
        if (excludeCharacterId && character.id === excludeCharacterId) return false;
        const candidateNames = [character.title, ...(Array.isArray(character.aliases) ? character.aliases.filter((alias): alias is string => typeof alias === "string" && Boolean(alias.trim())) : [])];
        return candidateNames.some((candidate) => normalizeCharacterName(candidate) === inputKey);
    });
}

// ---------- 安全自动填充决策 ----------

// 自动填充决策输入：已核验建议 + 表单现状 + 调用方维护的逐字段编辑标记。
export type SafeAutoFillInput = {
    result: VerifiedSuggestionResult;
    // 表单当前值（aliases 为逗号分隔字符串，与 CharacterForm 同构）
    formValues: Record<string, string>;
    // 本会话内用户编辑过的字段标记；用户主动清空也算编辑，调用方必须置 true
    dirtyFields: Record<string, boolean>;
};

// 计算可以静默自动填充的字段值（字段键 → 表单字符串值）。
// 只有同时满足以下条件才进入结果（design §4）：
// 1. 身份已确定（matched；ambiguous 一律不填）；
// 2. 该字段状态为 suggested、唯一选项且 basis 为 fact；
// 3. 字段当前为空；
// 4. 字段从本会话开始未被用户编辑（含主动清空）。
// 推断/制作建议/冲突/已有值/已编辑字段均不进入；用户主动采用/替换由 UI 另走明确事件。
export function computeSafeAutoFillFields(input: SafeAutoFillInput): Partial<Record<CharacterFieldKey, string>> {
    const fill: Partial<Record<CharacterFieldKey, string>> = {};
    if (input.result.status !== "matched" || input.result.people.length !== 1) return fill;
    const person = input.result.people[0];
    for (const key of characterFieldKeys) {
        if (input.dirtyFields[key]) continue;
        if ((input.formValues[key] || "").trim()) continue;
        const field = person.fields[key];
        if (field.status !== "suggested") continue;
        const option = field.options[0];
        if (!option || option.basis !== "fact") continue;
        fill[key] = key === "aliases" ? aliasesArrayToFormValue(option.value as string[]) : (option.value as string);
    }
    return fill;
}