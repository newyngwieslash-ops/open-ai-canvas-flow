import { expect, test } from "bun:test";
import { defaultConfig, createModelChannel } from "../src/stores/use-config-store";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
const modulePath = "../src/pages/projects/detail/project-profile-analysis";
const helpers = await import(modulePath).catch(() => ({}));

test("分析模型只使用明确文本选择和可信视觉目录，绝不回落首渠道", () => {
    expect(typeof helpers.profileAnalysisModel).toBe("function");
    const vision = defaultModelCapabilityConfig("openai-chat", "vision");
    vision.text = { ...vision.text!, references: { ...vision.text!.references, maxImages: 1, maxImageBytes: 1024 } };
    const channel = createModelChannel({ id: "system", scope: "system", models: ["vision"], modelCosts: [{ model: "vision", capability: "text", protocol: "openai-chat", billingMode: "token", unitPriceMicrocredits: 0, capabilityConfig: vision }] });
    const config = { ...defaultConfig, model: "wrong", imageModel: "wrong", textModel: "system::vision", channels: [channel] };
    expect(helpers.profileAnalysisModel(config, "system::vision")).toEqual({ kind: "system", channelId: "system", modelKey: "vision" });
    expect(helpers.profileAnalysisConfig(config, "system::vision")).toMatchObject({ model: "system::vision", textModel: "system::vision", imageModel: "wrong" });
    expect(helpers.profileAnalysisModel(config, "missing::vision")).toBeUndefined();
    expect(helpers.profileAnalysisModel({ ...config, channels: [{ ...channel, scope: "user" }] }, "system::vision")).toBeUndefined();
    expect(helpers.profileAnalysisModel({ ...config, channels: [{ ...channel, modelCosts: channel.modelCosts?.map((cost) => ({ ...cost, capabilityConfig: undefined })) }] }, "system::vision")).toBeUndefined();
    expect(helpers.profileAnalysisModel({ ...config, channels: [{ ...channel, modelCosts: channel.modelCosts?.map((cost) => ({ ...cost, logicalModelId: "logical" })) }] }, "system::vision")).toEqual({ kind: "logical", logicalModelId: "logical" });
});

test("草稿防御白名单与codepoint边界，历史JSON不转成object字符串", () => {
    expect(typeof helpers.profileAnalysisFields).toBe("function");
    expect(helpers.profileAnalysisFields("character")).toEqual(["appearance", "physique", "clothing", "props"]);
    expect(helpers.profileAnalysisFields("environment")).toEqual(["description", "prompt"]);
    expect(helpers.profileAnalysisFields("prop")).toEqual(["description", "prompt"]);
    expect(helpers.validProfileDraft("character", { appearance: "", physique: "", clothing: "红衣", props: [] })).toBe(false);
    expect(helpers.validProfileDraft("prop", { description: "😀".repeat(2000), prompt: "" })).toBe(true);
    expect(helpers.validProfileDraft("prop", { description: "😀".repeat(2001), prompt: "" })).toBe(false);
    expect(helpers.validProfileDraft("prop", { description: "\u0000", prompt: "" })).toBe(false);
    expect(helpers.validProfileDraft("prop", { description: "", prompt: "", name: "bad" })).toBe(false);
    expect(helpers.profileValue(["剑", "伞"])).toContain('[\n');
    expect(helpers.profileValue({ visible: true })).not.toContain("[object Object]");
    expect(helpers.profileAnalysisKey("u", "p", "a", "t")).toEqual(["project-profile-analysis", "u", "p", "a", "t"]);
});
