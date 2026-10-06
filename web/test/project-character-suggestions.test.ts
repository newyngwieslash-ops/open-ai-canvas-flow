import { describe, expect, test } from "bun:test";
import {
    findMatchingExistingCharacters,
    computeSafeAutoFillFields,
    characterFieldKeys,
    characterFields,
    parseCharacterSuggestions,
    verifySuggestionEvidence,
    type SuggestionResult,
} from "@/pages/projects/detail/character-script-suggestions";

// 本测试文件只使用人工编写的小剧本片段与虚构人物，不含任何真实用户资料。

describe("characterFields 字段清单迁移", () => {
    test("保持与角色表单一致的 12 个字段键和中文标签", () => {
        expect([...characterFields]).toEqual([
            ["role", "剧情定位与人物关系"],
            ["aliases", "别名"],
            ["appearance", "稳定外貌"],
            ["physique", "身高、体型与体态"],
            ["clothing", "默认服装造型"],
            ["personality", "性格与表演基线"],
            ["props", "固定道具"],
            ["consistencyPrompt", "跨镜头一致性约束"],
            ["multiViewPrompt", "三视图补充约束"],
            ["voiceLanguage", "语言与口音"],
            ["voiceAge", "声音年龄感"],
            ["voiceTimbre", "音色气质"],
        ]);
    });

    test("字段键清单与字段表一一对应", () => {
        expect(characterFieldKeys).toEqual(characterFields.map(([key]) => key));
        expect(characterFieldKeys).toHaveLength(12);
    });
});

// ---------- 解析夹具：人工编写的虚构小剧本与最小合法建议 ----------

const snapshotUnits = [
    {
        unitId: "unit-1",
        title: "第一集·深夜加班",
        text: "深夜的办公室只剩林夏一人。她揉了揉眼睛，打开磁吸小夜灯。\r\n吴奶奶端着一杯热牛奶走过来：“这么晚还不走？”\n林夏勉强笑了笑：“改完这一稿就睡。”\n吴奶奶又说：“夏夏，喝完再改。”",
    },
    {
        unitId: "unit-2",
        title: "第二集·对门的灯",
        text: "吴奶奶把米色布袋放在桌上，里面装着刚买的菜。",
    },
];

const FIELD_STATUSES: Record<string, { status: string; options: unknown[] }> = {};
for (const key of characterFieldKeys) {
    FIELD_STATUSES[key] = { status: "unspecified", options: [] };
}

// 最小合法建议：matched 一人，除 role/aliases 外全部"正文未明确"。
const validResult = () => ({
    status: "matched",
    people: [
        {
            name: "林夏",
            aliases: ["夏夏"],
            identitySummary: "深夜加班的都市上班族，吴奶奶的对门邻居",
            identityEvidence: [{ unitId: "unit-1", quote: "深夜的办公室只剩林夏一人" }],
            fields: {
                ...Object.fromEntries(characterFieldKeys.map((key) => [key, { status: "unspecified", options: [] }])),
                role: {
                    status: "suggested",
                    options: [{ value: "加班晚归的都市上班族", basis: "fact", evidence: [{ unitId: "unit-1", quote: "这么晚还不走？" }] }],
                },
                aliases: {
                    status: "suggested",
                    options: [{ value: ["夏夏"], basis: "fact", evidence: [{ unitId: "unit-1", quote: "夏夏，喝完再改。" }] }],
                },
            },
        },
    ],
});

const parseOk = (raw: string): SuggestionResult => parseCharacterSuggestions(raw);
const parseFail = (raw: string, message?: RegExp) => {
    let error: unknown;
    try {
        parseCharacterSuggestions(raw);
    } catch (caught) {
        error = caught;
    }
    expect(error).toBeInstanceOf(Error);
    if (message) expect((error as Error).message).toMatch(message);
};

describe("parseCharacterSuggestions 输入形态", () => {
    test("接受裸 JSON 对象并保留建议内容", () => {
        const result = parseOk(JSON.stringify(validResult()));
        expect(result.status).toBe("matched");
        expect(result.people).toHaveLength(1);
        expect(result.people[0].name).toBe("林夏");
        expect(result.people[0].fields.role.status).toBe("suggested");
        expect(result.people[0].fields.role.options[0].value).toBe("加班晚归的都市上班族");
        expect(result.people[0].fields.role.options[0].basis).toBe("fact");
        expect(result.people[0].fields.aliases.options[0].value).toEqual(["夏夏"]);
        expect(result.people[0].fields.appearance.status).toBe("unspecified");
    });

    test("接受整体 ```json 围栏", () => {
        const result = parseOk("```json\n" + JSON.stringify(validResult(), null, 2) + "\n```");
        expect(result.status).toBe("matched");
        expect(result.people[0].name).toBe("林夏");
    });

    test("拒绝正文夹带 JSON（前后有说明文字）", () => {
        parseFail("好的，以下是整理结果：\n" + JSON.stringify(validResult()) + "\n以上供参考。", /围栏|纯 JSON|JSON/);
    });

    test("拒绝顶层数组或损坏 JSON", () => {
        parseFail(JSON.stringify([validResult()]), /JSON 对象/);
        parseFail("{ not valid json", /JSON/);
    });
});

describe("parseCharacterSuggestions 状态基数", () => {
    test("matched 必须恰好一人", () => {
        parseFail(JSON.stringify({ status: "matched", people: [] }), /恰好一人/);
        const two = validResult();
        two.people.push(validResult().people[0]);
        parseFail(JSON.stringify(two), /恰好一人/);
    });

    test("ambiguous 至少两人，not_found 必须为空数组", () => {
        const one = { status: "ambiguous", people: validResult().people };
        parseFail(JSON.stringify(one), /至少两人/);
        const secondPerson = JSON.parse(JSON.stringify(validResult().people[0])) as { name: string };
        secondPerson.name = "吴奶奶";
        const twoAmbiguous = { status: "ambiguous", people: [validResult().people[0], secondPerson] };
        expect(parseOk(JSON.stringify(twoAmbiguous)).people).toHaveLength(2);
        parseFail(JSON.stringify({ status: "not_found", people: validResult().people }), /空数组/);
        expect(parseOk(JSON.stringify({ status: "not_found", people: [] })).status).toBe("not_found");
    });
});

describe("parseCharacterSuggestions 严格合同", () => {
    test("12 字段必须齐全且不允许未知字段", () => {
        const missing = validResult();
        delete (missing.people[0].fields as Record<string, unknown>).appearance;
        parseFail(JSON.stringify(missing), /12 个字段|字段齐全/);
        const extra = validResult();
        (extra.people[0].fields as Record<string, unknown>).height = { status: "unspecified", options: [] };
        parseFail(JSON.stringify(extra), /未知字段/);
        const personExtra = validResult();
        (personExtra.people[0] as Record<string, unknown>).gender = "female";
        parseFail(JSON.stringify(personExtra), /未知字段/);
    });

    test("字段状态基数：suggested 恰一个选项、unspecified 无选项、conflict 至少两个有依据选项", () => {
        const emptySuggested = validResult();
        emptySuggested.people[0].fields.role = { status: "suggested", options: [] };
        parseFail(JSON.stringify(emptySuggested), /恰好一个选项/);
        const withUnspecifiedOptions = validResult();
        withUnspecifiedOptions.people[0].fields.appearance = {
            status: "unspecified",
            options: [{ value: "26 岁女性", basis: "fact", evidence: [{ unitId: "unit-1", quote: "深夜的办公室只剩林夏一人" }] }],
        };
        parseFail(JSON.stringify(withUnspecifiedOptions), /options 必须为空|必须为空/);
        const singleConflict = validResult();
        singleConflict.people[0].fields.appearance = {
            status: "conflict",
            options: [{ value: "短发", basis: "fact", evidence: [{ unitId: "unit-1", quote: "深夜的办公室只剩林夏一人" }] }],
        };
        parseFail(JSON.stringify(singleConflict), /至少两个/);
    });

    test("fact 与 inference 必须至少一个引用，production 可以没有", () => {
        const factNoEvidence = validResult();
        factNoEvidence.people[0].fields.role.options[0].evidence = [];
        parseFail(JSON.stringify(factNoEvidence), /引用/);
        const inferenceNoEvidence = validResult();
        inferenceNoEvidence.people[0].fields.role.options[0].basis = "inference";
        inferenceNoEvidence.people[0].fields.role.options[0].evidence = [];
        parseFail(JSON.stringify(inferenceNoEvidence), /引用/);
        const productionNoEvidence = validResult();
        productionNoEvidence.people[0].fields.multiViewPrompt = {
            status: "suggested",
            options: [{ value: "正面、侧面、背面保持同一发型", basis: "production", evidence: [] }],
        };
        expect(parseOk(JSON.stringify(productionNoEvidence)).people[0].fields.multiViewPrompt.options[0].basis).toBe("production");
    });

    test("身份引用不能为空", () => {
        const noIdentity = validResult();
        noIdentity.people[0].identityEvidence = [];
        parseFail(JSON.stringify(noIdentity), /身份引用|至少一个/);
    });

    test("类型严格：字符串字段不接受数字、对象或数组", () => {
        const numericName = validResult();
        (numericName.people[0] as Record<string, unknown>).name = 42;
        parseFail(JSON.stringify(numericName), /name/);
        const objectValue = validResult();
        objectValue.people[0].fields.role.options[0].value = { desc: "高个子" };
        parseFail(JSON.stringify(objectValue), /role|字符串/);
        const stringAliases = validResult();
        (stringAliases.people[0] as Record<string, unknown>).aliases = "夏夏";
        parseFail(JSON.stringify(stringAliases), /aliases|数组/);
        const objectAliasItem = validResult();
        objectAliasItem.people[0].aliases = [{ text: "夏夏" } as unknown as string];
        parseFail(JSON.stringify(objectAliasItem), /aliases/);
    });

    test("aliases 数组元素去除空白项并与姓名规范化去重", () => {
        const merged = validResult();
        merged.people[0].aliases = ["夏夏", "  ", "夏夏", "林夏", "小雨"];
        const result = parseOk(JSON.stringify(merged));
        expect(result.people[0].aliases).toEqual(["夏夏", "小雨"]);
    });

    test("字符串与数量上限超限直接拒绝，不静默截断", () => {
        const longName = validResult();
        longName.people[0].name = "名".repeat(101);
        parseFail(JSON.stringify(longName), /长度|超/);
        const longQuote = validResult();
        longQuote.people[0].identityEvidence[0].quote = "言".repeat(501);
        parseFail(JSON.stringify(longQuote), /长度|超/);
        const manyAliases = validResult();
        manyAliases.people[0].aliases = Array.from({ length: 11 }, (_, index) => `别名${index}`);
        parseFail(JSON.stringify(manyAliases), /数量|超/);
        const manyPeople = { status: "ambiguous", people: Array.from({ length: 21 }, () => validResult().people[0]) };
        parseFail(JSON.stringify(manyPeople), /数量|超/);
    });

    test("身份摘要为空直接拒绝", () => {
        const emptySummary = validResult();
        emptySummary.people[0].identitySummary = "   ";
        parseFail(JSON.stringify(emptySummary), /身份摘要/);
    });
});

describe("parseCharacterSuggestions 制作字段伪装事实", () => {
    test("consistencyPrompt 与 multiViewPrompt 的 fact 选项按制作建议处理，引用保留", () => {
        const disguised = validResult();
        disguised.people[0].fields.consistencyPrompt = {
            status: "suggested",
            options: [{ value: "短发，米色风衣", basis: "fact", evidence: [{ unitId: "unit-1", quote: "这么晚还不走？" }] }],
        };
        const result = parseOk(JSON.stringify(disguised));
        expect(result.people[0].fields.consistencyPrompt.options[0].basis).toBe("production");
        expect(result.people[0].fields.consistencyPrompt.options[0].evidence).toHaveLength(1);
    });
});

// ---------- 引用核验（快照） ----------

// 测试快照：人工编写的虚构小剧本，含 CRLF 行尾以覆盖换行归一化。
const evidenceSnapshot = {
    projectId: "proj-1",
    createdAt: "2026-10-06T00:00:00.000Z",
    units: snapshotUnits.map((unit) => ({ ...unit })),
};
type SnapshotForTest = typeof evidenceSnapshot;

describe("verifySuggestionEvidence 引用核验", () => {
    test("有效引用通过并携带快照中的章节标题", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const verified = verifySuggestionEvidence(result, evidenceSnapshot);
        expect(verified.people).toHaveLength(1);
        const roleEvidence = verified.people[0].fields.role.options[0].evidence[0];
        expect(roleEvidence.unitId).toBe("unit-1");
        expect(roleEvidence.unitTitle).toBe("第一集·深夜加班");
        const aliasEvidence = verified.people[0].fields.aliases.options[0].evidence[0];
        expect(aliasEvidence.unitTitle).toBe("第一集·深夜加班");
    });

    test("CRLF 与 LF 差异不影响逐字匹配", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const lfSnapshot: SnapshotForTest = {
            ...evidenceSnapshot,
            units: evidenceSnapshot.units.map((unit) => ({ ...unit, text: unit.text.replace(/\r\n/g, "\n") })),
        };
        expect(() => verifySuggestionEvidence(result, lfSnapshot)).not.toThrow();
    });

    test("unitId 不在本轮快照中时整轮拒绝", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const forged = JSON.parse(JSON.stringify(result)) as SuggestionResult;
        forged.people[0].fields.role.options[0].evidence[0].unitId = "unit-missing";
        expect(() => verifySuggestionEvidence(forged, evidenceSnapshot)).toThrow(/unit-missing|章节/);
    });

    test("quote 无法在对应章节纯文本中逐字匹配时整轮拒绝（伪造原文）", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const forged = JSON.parse(JSON.stringify(result)) as SuggestionResult;
        forged.people[0].fields.role.options[0].evidence[0].quote = "林夏大喊一声冲了出去";
        expect(() => verifySuggestionEvidence(forged, evidenceSnapshot)).toThrow(/原文|quote/);
    });

    test("quote 放到另一章（文字真实存在于 unit-2）也拒绝", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const swapped = JSON.parse(JSON.stringify(result)) as SuggestionResult;
        swapped.people[0].fields.role.options[0].evidence[0].quote = "吴奶奶把米色布袋放在桌上";
        expect(() => verifySuggestionEvidence(swapped, evidenceSnapshot)).toThrow(/原文|quote/);
    });

    test("身份引用同样核验，无效则整轮拒绝", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const forged = JSON.parse(JSON.stringify(result)) as SuggestionResult;
        forged.people[0].identityEvidence[0].quote = "凭空捏造的一句话";
        expect(() => verifySuggestionEvidence(forged, evidenceSnapshot)).toThrow(/原文|quote/);
    });

    test("全部拒绝：不为调用方返回部分有效结果", () => {
        const result = parseOk(JSON.stringify(validResult()));
        const partlyValid = JSON.parse(JSON.stringify(result)) as SuggestionResult;
        // 身份引用有效，字段引用无效 —— 仍必须整轮抛错。
        partlyValid.people[0].fields.aliases.options[0].evidence[0].unitId = "unit-missing";
        let thrown = false;
        try {
            verifySuggestionEvidence(partlyValid, evidenceSnapshot);
        } catch {
            thrown = true;
        }
        expect(thrown).toBe(true);
    });

    test("快照章节正文为空时任何引用都无法匹配", () => {
        const emptySnapshot: SnapshotForTest = { ...evidenceSnapshot, units: [{ unitId: "unit-9", title: "空章", text: "" }] };
        const result = parseOk(JSON.stringify(validResult()));
        expect(() => verifySuggestionEvidence(result, emptySnapshot)).toThrow();
    });
});

// ---------- 重名/别名匹配（纯匹配逻辑；分页拉取由调用方负责） ----------

const existingCharacters = [
    { id: "char-1", title: "林夏", aliases: ["夏夏", "林工"] },
    { id: "char-2", title: "吴奶奶", aliases: [] },
    { id: "char-3", title: "陈默", aliases: ["默哥"] },
];

describe("findMatchingExistingCharacters 重名匹配", () => {
    test("输入姓名命中正式姓名", () => {
        const matches = findMatchingExistingCharacters("林夏", existingCharacters);
        expect(matches.map((item) => item.id)).toEqual(["char-1"]);
    });

    test("输入姓名命中别名（忽略大小写与空白、间隔符差异）", () => {
        const withLatinAlias = [...existingCharacters, { id: "char-4", title: "陈艾玛", aliases: ["emma-wang"] }];
        const latinHit = findMatchingExistingCharacters("EMMA.WANG", withLatinAlias);
        expect(latinHit.map((item) => item.id)).toEqual(["char-4"]);
        const aliasHit = findMatchingExistingCharacters("默哥", existingCharacters);
        expect(aliasHit.map((item) => item.id)).toEqual(["char-3"]);
    });

    test("排除自身角色", () => {
        const matches = findMatchingExistingCharacters("吴奶奶", existingCharacters, "char-2");
        expect(matches).toEqual([]);
    });

    test("无命中返回空数组，不抛错", () => {
        expect(findMatchingExistingCharacters("王大力", existingCharacters)).toEqual([]);
    });

    test("空白输入返回空数组", () => {
        expect(findMatchingExistingCharacters("   ", existingCharacters)).toEqual([]);
    });

    test("别名数组缺失或含非字符串时不抛错", () => {
        const loose = [{ id: "char-x", title: "夏夏", aliases: undefined }, { id: "char-y", title: "小雨", aliases: [7 as unknown as string, "夏夏"] }];
        const matches = findMatchingExistingCharacters("夏夏", loose as typeof existingCharacters);
        expect(matches.map((item) => item.id)).toEqual(["char-x", "char-y"]);
    });
});

// ---------- 安全自动填充纯决策 ----------

describe("computeSafeAutoFillFields 安全填充决策", () => {
    // 构造一个"身份已确定"（matched）且 role/aliases 为唯一 fact 建议的已核验结果。
    const matchedVerified = () => verifySuggestionEvidence(parseOk(JSON.stringify(validResult())), evidenceSnapshot);

    const baseInputs = () => ({
        result: matchedVerified(),
        // 表单当前值：全部为空（FormFieldValue 与 CharacterForm 的字段值同构）
        formValues: { name: "林夏", role: "", aliases: "" } as Record<string, string>,
        dirtyFields: {} as Record<string, boolean>,
    });

    test("matched + 唯一 fact + 字段为空 + 未编辑 → 自动填充", () => {
        const fill = computeSafeAutoFillFields(baseInputs());
        expect(fill.role).toBe("加班晚归的都市上班族");
        // aliases 唯一 fact 选项 → 以数组转换成表单逗号串
        expect(fill.aliases).toBe("夏夏");
        // unspecified 字段不进入填充
        expect("appearance" in fill).toBe(false);
    });

    test("身份歧义（ambiguous）一律不填", () => {
        const inputs = baseInputs();
        const ambiguous = verifySuggestionEvidence(
            parseOk(JSON.stringify((() => { const r = validResult(); r.status = "ambiguous"; r.people.push(JSON.parse(JSON.stringify({ ...validResult().people[0], name: "吴奶奶" }))); return r; })())),
            evidenceSnapshot,
        );
        expect(computeSafeAutoFillFields({ ...inputs, result: ambiguous })).toEqual({});
    });

    test("推断（inference）与制作建议（production）不自动填充", () => {
        const inputs = baseInputs();
        const raw = validResult();
        raw.people[0].fields.role.options[0].basis = "inference";
        const inferenceResult = verifySuggestionEvidence(parseOk(JSON.stringify(raw)), evidenceSnapshot);
        expect(computeSafeAutoFillFields({ ...inputs, result: inferenceResult }).role).toBeUndefined();
        const rawProduction = validResult();
        rawProduction.people[0].fields.role.options[0].basis = "production";
        const productionResult = verifySuggestionEvidence(parseOk(JSON.stringify(rawProduction)), evidenceSnapshot);
        expect(computeSafeAutoFillFields({ ...inputs, result: productionResult }).role).toBeUndefined();
    });

    test("字段已有值不覆盖", () => {
        const inputs = baseInputs();
        inputs.formValues.role = "用户手填的定位";
        expect(computeSafeAutoFillFields(inputs).role).toBeUndefined();
    });

    test("用户编辑过的字段不覆盖（含主动清空）", () => {
        const inputs = baseInputs();
        inputs.dirtyFields.role = true;
        expect(computeSafeAutoFillFields(inputs).role).toBeUndefined();
        const cleared = baseInputs();
        cleared.dirtyFields.aliases = true;
        expect(computeSafeAutoFillFields(cleared).aliases).toBeUndefined();
    });

    test("conflict 字段不自动填充", () => {
        const inputs = baseInputs();
        const raw = validResult();
        raw.people[0].fields.role = {
            status: "conflict",
            options: [
                { value: "加班晚归的都市上班族", basis: "fact", evidence: [{ unitId: "unit-1", quote: "这么晚还不走？" }] },
                { value: "大学生", basis: "inference", evidence: [{ unitId: "unit-1", quote: "改完这一稿就睡。" }] },
            ],
        };
        const conflictResult = verifySuggestionEvidence(parseOk(JSON.stringify(raw)), evidenceSnapshot);
        expect(computeSafeAutoFillFields({ ...inputs, result: conflictResult }).role).toBeUndefined();
    });

    test("dirty 标记未提供的字段按未编辑处理", () => {
        const fill = computeSafeAutoFillFields(baseInputs());
        expect(fill.role).toBeDefined();
    });
});
