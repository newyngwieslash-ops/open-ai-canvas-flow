import { modelCompatibilityError, resolveCompatibleModel, type ModelRequirements } from "@/lib/model-selection";
import type { submitBackendGenerationTask } from "@/services/api/generation-task";
import type { ProjectDetail, ProjectShot, ShotRevision } from "@/services/api/projects";
import type { Skill } from "@/services/api/skills";
import { skillRuntime } from "@/services/skill-runtime";
import type { AiConfig } from "@/stores/use-config-store";
import type { BatchImageSettings, BatchImageStage } from "./workflow-batch-plan";
import { buildShotAssetReferenceContext, resolveShotAssetMentionPrompt, type ShotAssetReferenceContext } from "./workflow-shot-references";

export type WorkflowImageInput = {
    detail: ProjectDetail;
    shot: ProjectShot;
    revision: ShotRevision;
    stage: BatchImageStage;
    settings: BatchImageSettings;
    config: AiConfig;
};

export function buildWorkflowImageInput(input: WorkflowImageInput): {
    prompt: string;
    config: AiConfig;
    requirements: ModelRequirements;
    references: ShotAssetReferenceContext;
    routedModel: string;
} {
    const { detail, shot, revision, stage, settings, config } = input;
    const projectId = detail?.project?.id?.trim() || "";
    const shotId = shot?.id?.trim() || "";
    const unitId = shot?.unitId?.trim() || "";
    const currentRevisionId = shot?.currentRevisionId?.trim() || "";
    if (!projectId || !shotId || !unitId) {
        throw new Error("镜头归属信息不完整");
    }
    if (shot.projectId !== projectId) {
        throw new Error("镜头不属于当前项目");
    }
    if ((detail.units || []).length > 0 && !(detail.units || []).some((unit) => unit.id === unitId && unit.projectId === projectId)) {
        throw new Error("镜头不属于当前项目章节");
    }
    if (!currentRevisionId || !revision?.id?.trim() || revision.id !== currentRevisionId || revision.shotId !== shotId) {
        throw new Error("镜头缺少已保存的当前脚本修订");
    }

    const references = buildShotAssetReferenceContext(detail, shot.id);
    const primaryText = (revision.imagePrompt || revision.plotDescription || "").trim();
    if (!primaryText) {
        throw new Error("镜头缺少画面内容，请先保存脚本");
    }
    const basePrompt = [
        revision.imagePrompt || revision.plotDescription,
        revision.action,
        stage === "previz" ? "黑白分镜草图，清晰动作节拍，电影构图" : "",
    ].filter(Boolean).join("\n");
    const prompt = resolveShotAssetMentionPrompt(basePrompt, references, {
        dialogue: revision.dialogue,
        mode: "image",
    });
    const requirements: ModelRequirements = {
        capability: "image",
        input: {
            textCount: 1,
            imageCount: references.referenceImages.length,
            videoCount: 0,
            audioCount: 0,
            characterCount: 0,
        },
        imageSize: settings.size,
        options: { size: settings.size, quality: settings.quality },
    };
    const routedModel = resolveCompatibleModel(config, settings.model, requirements) || settings.model;
    if (!routedModel?.trim()) {
        throw new Error("请先配置图片模型");
    }
    const problem = modelCompatibilityError(config, routedModel, requirements);
    if (problem) {
        throw new Error(`当前模型配置不可用：${problem}`);
    }
    const generationConfig: AiConfig = {
        ...config,
        model: routedModel,
        imageModel: routedModel,
        size: settings.size,
        quality: settings.quality,
        count: "1",
    };

    return {
        prompt,
        config: generationConfig,
        requirements,
        references,
        routedModel,
    };
}

export async function prepareWorkflowImageSubmission(
    input: WorkflowImageInput,
    context: {
        workflowStepId: string;
        skills: Skill[];
        batch?: { batchId: string; itemId: string };
    },
): Promise<Parameters<typeof submitBackendGenerationTask>[0]> {
    if (!context.workflowStepId?.trim()) {
        throw new Error("当前生成阶段不可用，请刷新页面后重试");
    }
    const built = buildWorkflowImageInput(input);
    const skillExecution = await skillRuntime.prepare({
        profile: "shortDrama",
        prompt: built.prompt,
        skills: context.skills,
        selectedSkillIds: input.settings.skillIds,
    });
    return {
        projectId: input.detail.project.id,
        mode: "image",
        prompt: skillExecution.prompt,
        config: built.config,
        referenceImages: built.references.referenceImages,
        referenceAudios: [],
        metadata: {
            ...skillExecution.metadata,
            workflowStepId: context.workflowStepId,
            domainProjectId: input.detail.project.id,
            unitId: input.shot.unitId,
            shotId: input.shot.id,
            shotRevisionId: input.revision.id,
            artifactType: input.stage === "previz" ? "action_board" : "storyboard",
            role: "output",
            source: "short-drama-workflow",
            ...(context.batch
                ? {
                    chapterImageBatchId: context.batch.batchId,
                    chapterImageBatchItemId: context.batch.itemId,
                }
                : {}),
            resolvedCharacterVersions: built.references.resolvedCharacterVersions,
            artifactMetadata: {
                model: built.config.model,
                aspectRatio: input.settings.size,
                durationSeconds: input.revision.durationMs / 1000,
                ...skillExecution.metadata,
            },
        },
    };
}
