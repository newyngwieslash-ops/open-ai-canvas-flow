import { describe, expect, test } from "bun:test";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import { buildProjectAssetBatchRequest, type BatchDraftItem } from "../src/pages/projects/detail/project-asset-batch";
import { createModelChannel, defaultConfig, normalizeConfigSnapshot } from "../src/stores/use-config-store";

const MODEL = "gpt-image-2.5";

function configWithCustomImageChannel() {
    const imageChannel = createModelChannel({
        id: "proxy",
        name: "Proxy",
        baseUrl: "https://image-proxy.example.com/v1",
        apiKey: "synthetic-image-key",
        apiFormat: "openai",
        interfaceType: "openai-image",
        models: [MODEL],
        modelCosts: [{ model: MODEL, capability: "image", protocol: "openai-image", billingMode: "fixed_request", unitPriceMicrocredits: 0, capabilityConfig: defaultModelCapabilityConfig("openai-image", MODEL) }],
    });
    const otherChannel = createModelChannel({
        id: "other",
        name: "Other",
        baseUrl: "https://other.example.com/v1",
        apiKey: "synthetic-other-key",
        apiFormat: "openai",
        models: ["other-text-model"],
    });
    return normalizeConfigSnapshot({
        config: { ...defaultConfig, channels: [imageChannel, otherChannel], model: `proxy::${MODEL}`, imageModel: `proxy::${MODEL}`, size: "1:1" },
    }).config;
}

const item: BatchDraftItem = {
    assetId: "asset-1",
    expectedVersionId: "version-1",
    referenceSource: "none",
    prompt: "  character  ",
    operation: "character_turnaround",
    assetName: "A",
    assetCategory: "character",
    referenceCount: 0,
    hasCurrentImage: false,
};

describe("buildProjectAssetBatchRequest", () => {
    test("custom image channel carries the model capability contract like canvas generation", () => {
        const request = buildProjectAssetBatchRequest(configWithCustomImageChannel(), `proxy::${MODEL}`, [item]);
        const config = request.config as Record<string, any>;
        expect(request.provider).toBe("custom");
        expect(request.model).toBe(MODEL);
        expect(config.interfaceType).toBe("openai-image");
        expect(config.size).toBe("1:1");
        expect(config.capabilityConfig?.image?.size?.presets?.length).toBeGreaterThan(0);
        expect(request.items[0].prompt).toBe("character");
    });

    test("does not send the full browser channel list or unrelated channel keys", () => {
        const request = buildProjectAssetBatchRequest(configWithCustomImageChannel(), `proxy::${MODEL}`, [item]);
        const config = request.config as Record<string, unknown>;
        expect(config.channels).toBeUndefined();
        expect(JSON.stringify(request)).not.toContain("synthetic-other-key");
    });
});
