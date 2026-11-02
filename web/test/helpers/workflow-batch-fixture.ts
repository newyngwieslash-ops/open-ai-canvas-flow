import type { ProjectDetail, ShotArtifact } from "../../src/services/api/projects";
import type { GenerationTask, TaskStatus } from "../../src/services/api/task-center";
import { createModelChannel, defaultConfig, encodeChannelModel } from "../../src/stores/use-config-store";

const now = "2026-10-29T00:00:00.000Z";

export function makeImageConfig() {
    const modelKey = "doubao-seedream-4-0-250828";
    const channel = createModelChannel({
        id: "test-image", name: "测试图片渠道",
        baseUrl: "https://ark.cn-beijing.volces.com/api/plan/v3",
        apiKey: "test-key", interfaceType: "volcengine-ark-agent-plan-image",
        models: [modelKey],
        modelCosts: [{
            model: modelKey, capability: "image", protocol: "volcengine-ark-agent-plan-image",
            billingMode: "fixed_request", unitPriceMicrocredits: 1,
        }],
    });
    const model = encodeChannelModel(channel.id, modelKey);
    return { ...defaultConfig, channels: [channel], model, imageModel: model, baseUrl: channel.baseUrl };
}

export function makeWorkflowBatchDetail(): ProjectDetail {
    const shots = [1, 2].map(n => ({
        id: `s${n}`, projectId: "p1", unitId: "u1", currentRevisionId: `r${n}`,
        title: `镜头0${n}`, description: `镜头${n}画面`, position: n - 1,
        durationMs: 3000, status: "draft", createdAt: now, updatedAt: now,
    }));
    return {
        project: {
            id: "p1", userId: "user-a", name: "测试短剧", type: "short_drama",
            aspectRatio: "16:9", sourceType: "manual", description: "测试项目",
            stylePresetId: "", status: "active", revision: 1, createdAt: now, updatedAt: now,
        },
        units: [{
            id: "u1", projectId: "p1", kind: "chapter", title: "第一章",
            sourceText: "测试章节", wordCount: 4, status: "ready", position: 0,
            createdAt: now, updatedAt: now,
        }],
        shots,
        shotRevisions: shots.map((shot, index) => ({
            id: `r${index + 1}`, shotId: shot.id, version: 1,
            plotDescription: shot.description, action: `动作${index + 1}`, dialogue: "",
            shotSize: "中景", cameraAngle: "平视", cameraMovement: "固定",
            durationMs: 3000, imagePrompt: `独立画面${index + 1}`, videoPrompt: "动作描述",
            negativePrompt: "", continuityNotes: "", actionBeatsJson: "[]", createdAt: now,
        })),
        workflows: [{
            instance: { id: "wf1", projectId: "p1", unitId: "u1", scope: "unit", status: "active", revision: 1 },
            steps: ["storyboard", "previz", "video"].map((stepKey, position) => ({
                id: `step-${stepKey}`, workflowInstanceId: "wf1", stepKey, name: stepKey,
                position, status: "ready", updatedAt: now,
            })),
        }],
        canvases: [], canvasUnitLinks: [], assets: [], assetFolders: [],
        shotArtifacts: [], shotReferences: [], assetCandidates: [], tasks: [],
    };
}

export function makeBatchTask(shotId: string, status: TaskStatus, artifactType: string): GenerationTask {
    return {
        id: `task-${shotId}-${artifactType}`, projectId: "p1", type: "canvas_image", status,
        prompt: "测试图片", attempts: 0, createdAt: now, updatedAt: now,
        clientContext: {
            domainProjectId: "p1", shotId, artifactType,
            workflowStepId: artifactType === "action_board" ? "step-previz" : "step-storyboard",
        },
        inputJson: JSON.stringify({ metadata: {
            domainProjectId: "p1", unitId: "u1", shotId,
            shotRevisionId: shotId.replace(/^s/, "r"), artifactType,
        } }),
    };
}

export function makeBatchArtifact(shotId: string, status: string, type: string): ShotArtifact {
    return {
        id: `artifact-${shotId}-${type}`, projectId: "p1", unitId: "u1", shotId,
        revisionId: shotId.replace(/^s/, "r"), taskId: `task-${shotId}-${type}`,
        type, version: 1, resourceId: `resource-${shotId}-${type}`, status,
        selected: status === "ready", metadataJson: "{}", createdAt: now, updatedAt: now,
    };
}
