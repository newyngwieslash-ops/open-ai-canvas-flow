import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button } from "antd";
import { EmptyState } from "@/components/ui/product/empty-state";
import { Link } from "react-router";

import { formatShotOrdinal } from "@/lib/shot-label";
import { saveProjectShot, type ProjectDetail } from "@/services/api/projects";
import { useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { resolveProjectCanvasStyle } from "@/components/canvas/canvas-style-picker-modal";
import { queryGenerationTask, waitForGenerationTask } from "@/services/api/task-center";
import { loadChapterAssetCandidates } from "./chapter-asset-candidates";
import { chapterStoryboardFromGenerationTask, generateChapterStoryboard } from "./project-chapter-ai";
import { chapterStoryboardCharacters, clearPendingGeneratedStoryboardSave, saveGeneratedChapterStoryboard } from "./chapter-storyboard-production";
import { useGeneratedBindingSelection } from "./chapter-generated-binding-selection";
import { ChapterAssetBindingDrawer, mergeBindingAppendResultIntoProjectCache } from "./chapter-asset-binding-drawer";

import { AssetsStage, DeliveryStage, StoryStage } from "./workflow-stage-views";
import { type ShortDramaWorkflowStage, workflowStages } from "./workflow-shared";
import "./workflow.css";

const WorkflowProductionWorkbench = lazy(() => import("./workflow-production-workbench"));

type Props = {
    detail: ProjectDetail;
    projectId: string;
    unitId: string;
    stage: string;
};

export default function ProjectWorkflowView({ detail, projectId, unitId, stage }: Props) {
    const queryClient = useQueryClient();
    const { message, modal } = App.useApp();
    const generatedBindingSelection = useGeneratedBindingSelection();
    const config = useEffectiveConfig();
    const isAiConfigReady = useConfigStore((state) => state.isAiConfigReady);
    const [generatingStoryboard, setGeneratingStoryboard] = useState(false);
    const [bindingDrawerOpen, setBindingDrawerOpen] = useState(false);
    const orderedUnits = useMemo(() => detail.units.slice().sort((left, right) => left.position - right.position), [detail.units]);
    const unit = orderedUnits.find((item) => item.id === unitId) || orderedUnits[0];
    const activeStage = workflowStages.some((item) => item.key === stage) ? stage as ShortDramaWorkflowStage : "video";
    const shots = useMemo(() => (detail.shots || []).filter((item) => item.unitId === unit?.id).slice().sort((left, right) => left.position - right.position), [detail.shots, unit?.id]);
    const workflow = useMemo(() => (detail.workflows || []).find((item) => item.instance?.unitId === unit?.id && (item.steps || []).some((step) => step.stepKey === "previz")), [detail.workflows, unit?.id]);
    const rememberedShotId = sessionStorage.getItem(`project-workflow-selected-shot:${projectId}`) || "";
    const [selectedShotId, setSelectedShotId] = useState(rememberedShotId);
    const selectedShot = shots.find((item) => item.id === selectedShotId) || shots[0];
    const activeStep = workflow?.steps?.find((item) => item.stepKey === activeStage);
    const productionStage = activeStage === "storyboard" || activeStage === "previz" || activeStage === "video";

    useEffect(() => {
        if (!selectedShot) return;
        if (selectedShot.id !== selectedShotId) setSelectedShotId(selectedShot.id);
        sessionStorage.setItem(`project-workflow-selected-shot:${projectId}`, selectedShot.id);
    }, [projectId, selectedShot, selectedShotId]);

    const refresh = async () => {
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["project", projectId] }),
            queryClient.invalidateQueries({ queryKey: ["projects"] }),
        ]);
    };
    const generateStoryboard = async (discardPrevious = false) => {
        if (!unit || generatingStoryboard) return;
        const key = `project-storyboard-task:${projectId}:${unit.id}`;
        let taskId = sessionStorage.getItem(key);
        if (discardPrevious && taskId) {
            const confirmed = await new Promise<boolean>((resolve) => modal.confirm({ title: "放弃上次分镜结果？", content: `将放弃未保存结果并创建新任务。生成成功后将替换本章 ${shots.length} 个分镜及其脚本、引用与生成产物。`, okText: "放弃并重新生成", cancelText: "保留结果", onOk: () => resolve(true), onCancel: () => resolve(false) }));
            if (!confirmed) return;
        }
        if (!taskId && shots.length) {
            const confirmed = await new Promise<boolean>((resolve) => modal.confirm({ title: `替换本章 ${shots.length} 个分镜？`, content: "生成成功后将替换本章镜头、脚本、引用与生成产物。", okText: "生成并替换", cancelText: "保留", onOk: () => resolve(true), onCancel: () => resolve(false) }));
            if (!confirmed) return;
        }
        setGeneratingStoryboard(true);
        try {
            if (discardPrevious && taskId) {
                const previous = await queryGenerationTask(taskId);
                if (!["succeeded", "failed", "cancelled"].includes(previous.status)) throw new Error("上次任务仍在运行，请等待结束后再放弃结果");
                clearPendingGeneratedStoryboardSave(projectId, unit.id, taskId); sessionStorage.removeItem(key);
                taskId = null;
            }
            let result: ReturnType<typeof chapterStoryboardFromGenerationTask>;
            if (taskId) {
                const task = await queryGenerationTask(taskId);
                if (task.status === "failed" || task.status === "cancelled") { clearPendingGeneratedStoryboardSave(projectId, unit.id, taskId); sessionStorage.removeItem(key); throw new Error("上次任务已终止，可重新生成章节分镜"); }
                result = chapterStoryboardFromGenerationTask(task.status === "succeeded" ? task : await waitForGenerationTask(task.id, { initialTask: task, useTextEvents: true }));
            } else {
                const style = resolveProjectCanvasStyle(detail.project.stylePresetId, detail.project.styleProfileJson);
                if (!unit.sourceText?.trim() || !style?.prompt.trim()) throw new Error("请先保存章节正文并设置项目画风");
                const model = config.textModel || config.model;
                if (!isAiConfigReady(config, model)) throw new Error("请先在设置中配置文本模型");
                const assets = await loadChapterAssetCandidates(projectId);
                result = await generateChapterStoryboard({ projectId, chapterId: unit.id, chapterTitle: unit.title, sourceText: unit.sourceText, projectStyle: { presetId: style.id, title: style.title, prompt: style.prompt, profileJson: detail.project.styleProfileJson }, characters: chapterStoryboardCharacters({ ...detail, assets }, unit.id), assetCandidates: { status: "complete", assets }, config, skills: [], selectedSkillIds: [] }, { onTaskUpdate: (task) => sessionStorage.setItem(key, task.id) });
            }
            const saved = await saveGeneratedChapterStoryboard(detail, unit.id, result, generatedBindingSelection.select);
            sessionStorage.removeItem(key);
            await refresh();
            message.success(`已生成 ${saved.shotCount} 个分镜`);
        } catch (error) { message.error(error instanceof Error ? error.message : "章节分镜生成失败"); }
        finally { setGeneratingStoryboard(false); }
    };
    const addShot = useMutation({
        mutationFn: () => {
            if (!unit) throw new Error("请先添加章节");
            return saveProjectShot(projectId, {
                unitId: unit.id,
                title: formatShotOrdinal(shots.length),
                description: "待补充分镜画面",
                position: shots.length,
                durationMs: 3000,
                revision: { plotDescription: "待补充分镜画面", durationMs: 3000 },
            });
        },
        onSuccess: async ({ shot }) => {
            setSelectedShotId(shot.id);
            sessionStorage.setItem(`project-workflow-selected-shot:${projectId}`, shot.id);
            await refresh();
            message.success("已新增分镜");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "新增分镜失败"),
    });
    if (!unit) {
        return <div className="grid h-full place-items-center"><EmptyState title="先添加一个章节，再进入分镜制作" action={<Link to={`/projects/${projectId}/chapters`}><Button type="primary">添加章节</Button></Link>} /></div>;
    }

    return (
        <div className="workflow-page-root">
            {generatedBindingSelection.dialog}
            {activeStage === "storyboard" && sessionStorage.getItem(`project-storyboard-task:${projectId}:${unit.id}`) ? <div className="px-4 py-2"><Button disabled={generatingStoryboard} onClick={() => void generateStoryboard(true)}>放弃上次结果并重新生成</Button></div> : null}
            <main className={`workflow-stage-content ${productionStage ? "is-production" : ""}`}>
                {activeStage === "story" ? <div className="workflow-overview-scroll thin-scrollbar"><StoryStage detail={detail} projectId={projectId} unitId={unit.id} /></div> : null}
                {activeStage === "assets" ? <div className="workflow-overview-scroll thin-scrollbar"><AssetsStage detail={detail} projectId={projectId} unitId={unit.id} hasUnitShots={shots.length > 0} onOpenAssetBinding={() => setBindingDrawerOpen(true)} /></div> : null}
                {productionStage ? <Suspense fallback={<div className="workflow-workbench-loading">正在准备分镜工作台…</div>}><WorkflowProductionWorkbench activeStage={activeStage} detail={detail} projectId={projectId} unitId={unit.id} workflowStep={activeStep} selectedShot={selectedShot} onSelectShot={setSelectedShotId} onRefresh={refresh} onAddShot={() => addShot.mutate()} addingShot={addShot.isPending} onGenerateStoryboard={() => void generateStoryboard()} generatingStoryboard={generatingStoryboard} /></Suspense> : null}
                {activeStage === "delivery" ? <div className="workflow-overview-scroll thin-scrollbar"><DeliveryStage detail={detail} unitId={unit.id} /></div> : null}
            </main>
            <ChapterAssetBindingDrawer
                projectId={projectId}
                unitId={unit.id}
                shotIds={shots.map((shot) => shot.id)}
                open={bindingDrawerOpen}
                onClose={() => setBindingDrawerOpen(false)}
                onApplied={(result) => {
                    mergeBindingAppendResultIntoProjectCache(queryClient, projectId, unit.id, result);
                    void refresh();
                }}
            />
        </div>
    );
}
