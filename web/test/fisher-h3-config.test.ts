import { expect, test } from "bun:test";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import { createFisherH3Channel } from "../src/stores/use-config-store";

test("Fisher preset stores exact protocol and model with a blank key", () => {
    const channel = createFisherH3Channel();
    expect(channel.baseUrl).toBe("https://api.work-fisher.com");
    expect(channel.apiKey).toBe("");
    expect(channel.models).toEqual(["MiniMax-H3"]);
    expect(channel.modelCosts?.[0].protocol).toBe("fisher-h3-video");
});

test("Fisher capability keeps independent media slots and service resolutions", () => {
    const profile = defaultModelCapabilityConfig("fisher-h3-video", "MiniMax-H3").video!;
    expect(profile.references.maxImages).toBe(11);
    expect(profile.references.maxAudios).toBe(4);
    expect(profile.references.maxVideos).toBe(3);
    expect(profile.references.promptMaxChars).toBe(10000);
    expect(profile.duration.max).toBe(60);
    expect(profile.resolutions).toEqual(["480P", "768P", "1080P"]);
    expect(profile.ratios).toContain("2:3");
    expect(defaultModelCapabilityConfig("minimax-video").video!.references.maxImages).toBe(9);
});
