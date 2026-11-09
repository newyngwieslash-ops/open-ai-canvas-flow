import { runBackendCanvasGenerationTask } from "@/lib/canvas/canvas-project-generation";
import { PromptTemplateOperation, promptTemplateTaskPlaceholder } from "@/lib/prompts";
import { storyboardRowsFromTask, withStoryboardOutputContract } from "@/lib/canvas/canvas-project-domain";
import { parseChapterAssetBreakdown, type ChapterAssetBreakdown } from "@/lib/canvas/chapter-asset-breakdown";
import { parseCharacterBreakdown } from "@/lib/canvas/canvas-character-reference";
import { backendProviderConfig, parseBackendGenerationResult } from "@/services/api/generation-task";
import { createGenerationTask, waitForGenerationTask, type GenerationTask } from "@/services/api/task-center";
import type { Skill } from "@/services/api/skills";
import { skillRuntime } from "@/services/skill-runtime";
import { logicalModelIDForConfig, type AiConfig } from "@/stores/use-config-store";

import { chapterStoryboardAssets, type ChapterStoryboardCharacter } from "./chapter-storyboard-production";
import type { ChapterAssetCandidatesState } from "./chapter-asset-candidates";

type ChapterAnalysisInput = {
    projectId: string;
    projectName: string;
    chapterId: string;
    chapterTitle: string;
    sourceText: string;
    projectStyle: string;
    config: AiConfig;
};

type ChapterTaskOptions = {
    onTaskUpdate?: (task: GenerationTask) => void;
};

export type ChapterTaskKind = "characters" | "storyboard";

export function chapterTaskIdentity(task: GenerationTask): { chapterId: string; kind: ChapterTaskKind } | null {
    const context = task.clientContext;
    if (context?.chapterId && (context.chapterOperation === "characters" || context.chapterOperation === "storyboard")) {
        return { chapterId: context.chapterId, kind: context.chapterOperation };
    }
    const metadata = generationTaskMetadata(task.inputJson);
    const chapterId = typeof metadata.chapterId === "string" ? metadata.chapterId : "";
    if (!chapterId) return null;
    if (metadata.operation === "chapter_character_breakdown") return { chapterId, kind: "characters" };
    if (metadata.source === "short-drama-chapter-storyboard") return { chapterId, kind: "storyboard" };
    return null;
}

export function chapterAssetsFromGenerationTask(task: GenerationTask) {
    const result = parseBackendGenerationResult(task);
    if (!result.text?.trim()) throw new Error("模型没有返回可用的章节资产提取结果");
    // 历史角色任务的输出契约只有角色；仅按明确的模板操作恢复，不能给新任务缺字段兜底。
    if (generationTaskMetadata(task.inputJson).promptTemplateOperation === PromptTemplateOperation.CharacterExtract) {
        return { characters: parseCharacterBreakdown(result.text), scenes: [], props: [] };
    }
    return parseChapterAssetBreakdown(result.text);
}

export function chapterStoryboardFromGenerationTask(task: GenerationTask) {
    return { ...storyboardRowsFromTask(task), taskId: task.id };
}

export async function extractChapterAssets(input: ChapterAnalysisInput, options?: ChapterTaskOptions): Promise<ChapterAssetBreakdown> {
    const result = await runProjectTextTask(input, "chapter_character_breakdown", {
        项目名称: input.projectName,
        章节名称: input.chapterTitle,
        项目画风: input.projectStyle || "项目尚未指定画风，保持视觉描述中性、可执行。",
        章节正文: input.sourceText,
    }, options);
    return parseChapterAssetBreakdown(result);
}

export type ChapterStoryboardAssetScope = { kind: "all" | "selected"; assetIds: string[] };
const storyboardAssetSummaryMaxBytes = 24 * 1024;

export function chapterStoryboardAssetScopeInfo(state: ChapterAssetCandidatesState, scope: ChapterStoryboardAssetScope = { kind: "all", assetIds: [] }) {
    if (state.status !== "complete") throw new Error("项目资产目录尚未完整加载");
    const ids = new Set(scope.assetIds);
    if ((scope.kind === "all" && ids.size) || (scope.kind === "selected" && !ids.size) || ids.size !== scope.assetIds.length) throw new Error("请选择完整目录或明确的非空资产范围");
    const assets = scope.kind === "all" ? state.assets : state.assets.filter((asset) => ids.has(asset.id));
    if (scope.kind === "selected" && assets.length !== ids.size) throw new Error("所选资产不在完整项目目录中");
    const summary = JSON.stringify(assets.map((asset) => ({ assetId: asset.id, category: asset.category, name: asset.title.trim() }))).replace(/[<>&\u2028\u2029]/g, (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, "0")}`);
    if (new TextEncoder().encode(summary).byteLength > storyboardAssetSummaryMaxBytes) throw new Error("项目资产摘要超过 24 KiB，请明确缩小资产范围；不会自动截断");
    return { assets, scope, totalCount: state.assets.length, sentCount: assets.length };
}

type ChapterStoryboardGenerationInput = {
    projectId: string;
    chapterId: string;
    chapterTitle: string;
    sourceText: string;
    projectStyle: { presetId: string; title: string; prompt: string; profileJson?: string };
    characters: ChapterStoryboardCharacter[];
    assetCandidates: ChapterAssetCandidatesState;
    assetScope?: ChapterStoryboardAssetScope;
    config: AiConfig;
    skills: Skill[];
    selectedSkillIds: string[];
};

export async function generateChapterStoryboard(input: ChapterStoryboardGenerationInput, options?: ChapterTaskOptions) {
    if (input.assetCandidates.status === "cancelled") throw new DOMException("项目资产目录加载已取消", "AbortError");
    if (input.assetCandidates.status !== "complete") throw new Error("项目资产目录尚未完整加载");
    const scopeInfo = chapterStoryboardAssetScopeInfo(input.assetCandidates, input.assetScope);
    const assets = chapterStoryboardAssets({ assets: scopeInfo.assets });
    const model = input.config.textModel || input.config.model;
    const config = { ...input.config, model };
    const skillExecution = await skillRuntime.prepare({
        profile: "shortDrama",
        prompt: [
            `章节：${input.chapterTitle}`,
            input.sourceText,
            "请将本章拆解为可直接进入短剧分镜制作的镜头脚本，保持剧情因果、人物关系、关键动作与台词完整。",
        ].join("\n\n"),
        skills: input.skills,
        selectedSkillIds: input.selectedSkillIds,
    });
    // 技能上下文只描述工作流，不会约束输出结构；必须显式带上输出契约，否则模型返回 Markdown 表格。
    const prompt = withStoryboardOutputContract([
        skillExecution.prompt,
        input.projectStyle.prompt.trim() ? `【项目画风（必须遵循）】\n${input.projectStyle.title}\n${input.projectStyle.prompt}\n每个 imageGenerationPrompt 必须明确沿用上述视觉媒介；角色造型和镜头质感不得替换项目媒介，videoMotionPrompt 也不得改变画风。` : "",
    ].filter(Boolean).join("\n\n"));
    const task = await createGenerationTask({
        projectId: input.projectId,
        type: "canvas_text",
        operation: "storyboard",
        prompt,
        model,
        ...(logicalModelIDForConfig(config) ? { logicalModelId: logicalModelIDForConfig(config) } : {}),
        input: {
            // 后端只在 video_ 前缀的任务上兜底 mode，文本任务必须显式带上。
            mode: "text",
            canvasAssets: assets,
            chapterAssetContext: { projectId: input.projectId, unitId: input.chapterId, scope: scopeInfo.scope },
            requirements: "输出可直接写入分镜制作并继续生成分镜图、动作预演和镜头视频的分镜表。",
            projectStyle: input.projectStyle,
            characters: input.characters,
            shotDurationSeconds: 0,
            shotCount: 0,
            config: backendProviderConfig(config, "text"),
            metadata: {
                domainProjectId: input.projectId,
                chapterId: input.chapterId,
                source: "short-drama-chapter-storyboard",
                ...skillExecution.metadata,
            },
        },
    });
    options?.onTaskUpdate?.(task);
    const completed = await waitForGenerationTask(task.id, { initialTask: task, useTextEvents: true, onTaskUpdate: options?.onTaskUpdate });
    return { ...chapterStoryboardFromGenerationTask(completed), skillCount: skillExecution.selectedSkills.length };
}

async function runProjectTextTask(input: ChapterAnalysisInput, operation: string, promptTemplateVariables: Record<string, string>, options?: ChapterTaskOptions) {
    const model = input.config.textModel || input.config.model;
    const result = await runBackendCanvasGenerationTask({
        projectId: input.projectId,
        nodeId: `${operation}:${input.chapterId}`,
        mode: "text",
        prompt: promptTemplateTaskPlaceholder("章节角色、场景与道具提取"),
        config: { ...input.config, model },
        metadata: { domainProjectId: input.projectId, chapterId: input.chapterId, operation, promptTemplateOperation: PromptTemplateOperation.ChapterAssetsExtract, promptTemplateVariables },
        onTaskCreated: options?.onTaskUpdate,
    });
    if (!result.text?.trim()) throw new Error("模型没有返回可用结果");
    return result.text;
}

function generationTaskMetadata(inputJson?: string): Record<string, unknown> {
    if (!inputJson) return {};
    try {
        const input = JSON.parse(inputJson) as { metadata?: unknown };
        return input.metadata && typeof input.metadata === "object" ? input.metadata as Record<string, unknown> : {};
    } catch {
        return {};
    }
}
