import { afterEach, describe, expect, test } from "bun:test";
import { applyWorkflowFlowSkills, buildWorkflowFlowBundle, loadWorkflowFlowBundle } from "../src/pages/projects/detail/workflow-flow-export";
import type { ProjectAsset, ShotArtifact } from "../src/services/api/projects";
import type { Skill } from "../src/services/api/skills";
import { http } from "../src/services/api/request";
import { skillRuntime } from "../src/services/skill-runtime";
import { makeBatchArtifact, makeWorkflowBatchDetail } from "./helpers/workflow-batch-fixture";

const options = { unitId: "u1", origin: "http://localhost:5178", resolution: "1080", editorDirty: false };
const originalGet = http.get;
const originalPrepare = skillRuntime.prepare;
afterEach(() => { http.get = originalGet; skillRuntime.prepare = originalPrepare; });

function referenceDetail() {
    const detail = makeWorkflowBatchDetail();
    const asset: ProjectAsset = {
        id: "asset-sangruo", title: "桑若", category: "character", mediaType: "entity", status: "confirmed",
        primaryVersionId: "version-current", versionCount: 2, position: 0, usages: [], updatedAt: "2026-11-01T00:00:00Z",
        character: {
            versionId: "version-current", version: 2, definition: { voiceTimbre: "温和" },
            representations: [{ id: "rep-current", resourceId: "image-current", role: "primary", mediaType: "image/png" }],
            visualStatus: "ready", voiceStatus: "ready",
            voice: { instructions: "自然说话", profile: { id: "voice-1", name: "声音", provider: "private-provider", voiceKey: "NEVER_EXPORT_VOICE_KEY", compatibleModels: [], language: "普通话", timbre: "温和", sampleResourceId: "voice-sample", status: "ready" } },
        },
    };
    detail.assets = [asset];
    detail.shotReferences = detail.shots.map(shot => ({
        id: `ref-${shot.id}`, shotId: shot.id, assetVersionId: "version-old", role: "reference", status: "linked", createdAt: "2026-11-01T00:00:00Z",
        asset,
        referencedVersion: { id: "version-old", assetId: asset.id, version: 1, representations: [
            { id: "rep-front", resourceId: "image-old", role: "primary", mediaType: "image/png" },
            { id: "rep-side", resourceId: "image-side", role: "side", mediaType: "image/png" },
        ] },
    }));
    detail.shotRevisions[0]!.videoPrompt = "@[asset:asset-sangruo]放下陶碗后坐下。";
    return detail;
}

describe("workflow Flow export", () => {
    test("exports every saved field and all six action labels in a readable video prompt", () => {
        const detail = referenceDetail();
        detail.project.stylePresetId = "period-live-action";
        const revision = detail.shotRevisions[0]!;
        revision.action = "叙事意图：两人无需客套。\n表演调度：桑若放下碗后坐下。\n时间节拍：0-2秒递碗。\n情绪：温和、自然\n光线与氛围：晨光柔和。\n声音：陶碗的轻响。";
        revision.dialogue = "桑若：喝粥吧。";
        revision.cameraMovement = "缓慢推近";
        revision.negativePrompt = "不要多余手指";
        revision.continuityNotes = "裴让始终坐在左侧";
        revision.actionBeatsJson = '[{"description":"2-4秒坐下并说话"}]';
        const result = buildWorkflowFlowBundle(detail, { ...options, shotId: "s1" });
        expect(result.version).toBe(1);
        expect(result.source).toEqual({ origin: options.origin, projectId: "p1", unitId: "u1", projectTitle: "测试短剧", unitTitle: "第一章" });
        expect(result.shots).toHaveLength(1);
        const shot = result.shots[0]!;
        expect(shot.revision).toEqual(revision);
        expect(shot.revision).not.toBe(revision);
        for (const value of [revision.action, revision.dialogue, revision.shotSize, revision.cameraAngle, revision.cameraMovement, revision.negativePrompt, revision.continuityNotes, "2-4秒坐下并说话", "【项目画风】"]) expect(shot.prompt).toContain(value);
        expect(shot.prompt).toContain("桑若放下陶碗后坐下");
        expect(shot.prompt).toContain('文件“桑若”');
        expect(shot.prompt).not.toContain("@[asset:");
        expect(shot.prompt).not.toContain("图片1");
        expect(shot.settings).toEqual({ aspectRatio: "16:9", resolution: "1080" });
        expect(shot.durationMs).toBe(revision.durationMs);
    });

    test("uses the bound historical version, keeps all representations and voices, deduplicates binary across shots", () => {
        const result = buildWorkflowFlowBundle(referenceDetail(), options);
        expect(result.shots.map(shot => shot.id)).toEqual(["s1", "s2"]);
        expect(result.media.map(media => media.id)).toEqual(["image-old", "image-side", "voice-sample"]);
        for (const media of result.media) {
            expect(media.shotIds).toEqual(["s1", "s2"]);
            expect(media.assetId).toBe("asset-sangruo");
            expect(media.assetVersionId).toBe(media.id === "voice-sample" ? "version-current" : "version-old");
        }
        expect(result.shots[0]!.mediaIds).toEqual(["image-old", "image-side", "voice-sample"]);
        expect(JSON.stringify(result)).not.toContain("image-current");
        expect(JSON.stringify(result)).not.toContain("NEVER_EXPORT_VOICE_KEY");
        expect(result.shots[0]!.warnings.join("\n")).toContain("声音样本来自当前角色声音配置");
    });

    test("keeps selected and latest artifacts separately, including stale boards and explicit start/end frames", () => {
        const detail = makeWorkflowBatchDetail();
        const artifact = (type: string, version: number, selected: boolean, status: string, revisionId = "r1"): ShotArtifact => ({
            ...makeBatchArtifact("s1", status, type), id: `${type}-${version}`, resourceId: `media-${type}-${version}`, version, selected, revisionId,
        });
        detail.shotArtifacts = [
            artifact("storyboard", 2, true, "ready", "old-revision"), artifact("storyboard", 3, false, "stale"),
            artifact("action_board", 1, true, "stale"), artifact("start_frame", 1, true, "ready"), artifact("end_frame", 1, true, "ready"),
        ];
        const result = buildWorkflowFlowBundle(detail, { ...options, shotId: "s1" });
        expect(result.media).toHaveLength(5);
        expect(result.media.find(media => media.artifactId === "storyboard-2")?.roles).toEqual(["storyboard", "selected"]);
        expect(result.media.find(media => media.artifactId === "storyboard-3")?.roles).toEqual(["storyboard", "latest"]);
        expect(result.media.find(media => media.artifactId === "storyboard-2")?.status).toBe("stale");
        expect(result.media.find(media => media.artifactId === "start_frame-1")?.roles).toContain("start_frame");
        expect(result.media.find(media => media.artifactId === "end_frame-1")?.roles).toContain("end_frame");
        expect(result.shots[0]!.warnings.join("\n")).toContain("old-revision");
        expect(result.shots[0]!.warnings.join("\n")).toContain("多格画面");
    });

    test("does not copy project configuration, task payloads, artifact metadata or injected revision properties", () => {
        const detail = makeWorkflowBatchDetail();
        Object.assign(detail.project, { apiKey: "NEVER_COPY_PROJECT_SECRET", endpoint: "https://private.example" });
        Object.assign(detail.shotRevisions[0]!, { inputJson: "NEVER_COPY_TASK_INPUT", metadata: { cookie: "NEVER_COPY_COOKIE" } });
        detail.shotArtifacts = [{ ...makeBatchArtifact("s1", "ready", "storyboard"), metadataJson: JSON.stringify({ headers: { authorization: "NEVER_COPY_AUTH" }, upstreamUrl: "https://private.example?apiKey=SECRET" }) }];
        const serialized = JSON.stringify(buildWorkflowFlowBundle(detail, options));
        expect(serialized).not.toContain("NEVER_COPY");
        expect(serialized).not.toContain("private.example");
        expect(serialized).not.toContain("metadataJson");
    });

    test("rejects unsaved changes, missing current revisions and unresolved asset tokens instead of falling back", () => {
        const detail = makeWorkflowBatchDetail();
        expect(() => buildWorkflowFlowBundle(detail, { ...options, editorDirty: true })).toThrow("未保存");
        expect(() => buildWorkflowFlowBundle(detail, { ...options, shotId: "outside-chapter" })).toThrow("不属于当前章节");
        detail.shots[0]!.currentRevisionId = "missing";
        expect(() => buildWorkflowFlowBundle(detail, options)).toThrow("当前脚本版本");
        detail.shots[0]!.currentRevisionId = "r1";
        detail.shotRevisions[0]!.videoPrompt = "@[asset:missing] 推门";
        expect(() => buildWorkflowFlowBundle(detail, options)).toThrow("未绑定资产");
    });

    test("never labels current representations as an unavailable historical bound version", () => {
        const detail = referenceDetail();
        detail.shotReferences[0]!.referencedVersion = undefined;
        expect(() => buildWorkflowFlowBundle(detail, options)).toThrow("不能用当前版本代替");
        detail.shotReferences[0]!.assetVersionId = "version-current";
        const current = buildWorkflowFlowBundle(detail, { ...options, shotId: "s1" });
        expect(current.media.find(media => media.id === "image-current")?.assetVersionId).toBe("version-current");
    });

    test("invalid action beats remain available for review and shared files retain conflicting source identities in notes", () => {
        const detail = referenceDetail();
        detail.shotRevisions[0]!.actionBeatsJson = "原有动作节拍文本";
        detail.shotReferences[1]!.assetVersionId = "version-another";
        detail.shotReferences[1]!.referencedVersion!.id = "version-another";
        const result = buildWorkflowFlowBundle(detail, options);
        expect(result.shots[0]!.prompt).toContain("原有动作节拍文本");
        expect(result.shots[0]!.warnings.join("\n")).toContain("不是有效数组");
        expect(result.shots[1]!.warnings.join("\n")).toContain("assetVersionId=version-another");
        expect(result.shots[1]!.warnings.join("\n")).toContain("assetVersionId=version-old");
    });

    test("loads a fresh core and full chapter workspace using app http with cancellation", async () => {
        const detail = referenceDetail();
        const calls: string[] = [];
        const controller = new AbortController();
        http.get = (async (path: string, config: { signal?: AbortSignal }) => {
            calls.push(path);
            expect(config.signal).toBe(controller.signal);
            return path.endsWith("/core") ? { project: detail.project } : { ...detail, unit: detail.units[0] };
        }) as typeof http.get;
        const result = await loadWorkflowFlowBundle({ ...options, projectId: "p1", signal: controller.signal });
        expect(calls).toEqual(["/projects/p1/core", "/projects/p1/units/u1/workspace"]);
        expect(result.shots).toHaveLength(2);
        await expect(loadWorkflowFlowBundle({ ...options, projectId: "p1", editorDirty: true })).rejects.toThrow("未保存");
        expect(calls).toHaveLength(2);
    });

    test("compiles the explicitly selected skills for every shot and carries only whitelisted provenance notes", async () => {
        const source = buildWorkflowFlowBundle(makeWorkflowBatchDetail(), options);
        const calls: string[] = [];
        const selected = { skillId: "skill-1", skillName: "电影调度", isAdded: true } as Skill;
        skillRuntime.prepare = (async input => {
            expect(input.profile).toBe("shortDrama");
            expect(input.selectedSkillIds).toEqual(["skill-1"]);
            calls.push(input.prompt);
            return {
                prompt: `技能指令：保持轴线。\n${input.prompt}`, selectedSkills: [selected], delivery: "linked-context",
                provenance: { skillIds: ["skill-1"], skillVersions: [{ skillId: "skill-1", versionId: "skill-version", version: "1.0" }], skillFiles: [{ skillId: "skill-1", path: "SKILL.md", sha256: "a".repeat(64) }] },
                metadata: { apiKey: "NEVER_COPY_SKILL_SECRET", raw: "NEVER_COPY_RAW" },
            };
        }) as typeof skillRuntime.prepare;
        const result = await applyWorkflowFlowSkills(source, { skills: [selected], selectedSkillIds: ["skill-1"] });
        expect(calls).toEqual(source.shots.map(shot => shot.prompt));
        expect(result.shots.every(shot => shot.prompt.startsWith("技能指令"))).toBe(true);
        expect(source.shots[0]!.prompt).not.toStartWith("技能指令");
        expect(result.shots[0]!.warnings.join("\n")).toContain("skill-version");
        expect(result.shots[0]!.warnings.join("\n")).toContain("a".repeat(64));
        expect(JSON.stringify(result)).not.toContain("NEVER_COPY");
        await expect(applyWorkflowFlowSkills(source, { skills: [], selectedSkillIds: ["skill-1"] })).rejects.toThrow("不可用");
    });
});
