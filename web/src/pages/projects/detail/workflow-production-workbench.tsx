import { ImageSizePicker } from "@/components/image-size-picker";
import { imageResolutionUsesQuality } from "@/lib/image-size-presets";
import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Dropdown, Form, Image, Input, InputNumber } from "antd";
import { SegmentedControl } from "@/components/ui/base/segmented-control";
import { EmptyState } from "@/components/ui/product/empty-state";
import { StatusBadge } from "@/components/ui/base/badges";
import { AppModal } from "@/components/ui/product/app-modal/app-modal";
import { Box, ChevronDown, ChevronLeft, ChevronRight, Download, Film, Image as ImageIcon, Layers3, List, Maximize2, Play, Plus, RefreshCcw, Save, Search, SlidersHorizontal, Trash2, UsersRound, WandSparkles, X } from "lucide-react";
import { Link, useNavigate } from "react-router";

import { CanvasResourceMentionTextarea } from "@/components/canvas/canvas-resource-mention-textarea";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { ModelPicker } from "@/components/model-picker";
import { CreditSymbol, requestCreditCost } from "@/constant/credits";
import { modelCapabilityConfigFor, normalizeImageValue, normalizeVideoValue, videoDurationOptions } from "@/lib/model-capabilities";
import { imageQualityLabel } from "@/lib/image-quality";
import { modelQuoteDescription, modelQuoteRequest } from "@/lib/model-pricing";
import { customShotTitle, formatShotOrdinal, normalizeDefaultShotTitle } from "@/lib/shot-label";
import { modelCompatibilityError, resolveCompatibleModel, resolveModelVideoBooleanOptions, type ModelRequirements } from "@/lib/model-selection";
import { formatVideoResolutionLabel } from "@/lib/video-generation-options";
import { submitBackendGenerationTask } from "@/services/api/generation-task";
import { quoteModel, type LogicalModelQuote } from "@/services/api/logical-models";
import { type GenerationTask } from "@/services/api/task-center";
import { downloadBrowserMedia } from "@/services/browser-download";
import {
    createUnitWorkflow,
    deleteProjectShot,
    linkShotAsset,
    listProjectAssetsPage,
    saveProjectShot,
    unlinkShotAsset,
    type BindingAppendResult,
    type ProjectAsset,
    type ProjectDetail,
    type ProjectShot,
    type ShotAssetReference,
    type ShotArtifact,
    type ShotRevisionInput,
    type WorkflowStep,
} from "@/services/api/projects";
import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey } from "@/services/api/resources";
import { skillRuntime } from "@/services/skill-runtime";
import { submitFlowBundle } from "@/services/flow-bridge";
import { configuredModelMatchesCapability, modelDisplayName, modelOptionName, resolveModelChannel, selectableModelsByCapability, useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { SkillRuntimePicker, useSkillRuntimeCatalog } from "@/components/skills/skill-runtime-picker";

import {
    ArtifactStatus,
    artifactTypeForStage,
    assetCategoryLabel,
    currentArtifact,
    currentRevision,
    formatDuration,
    type ShortDramaWorkflowStage,
} from "./workflow-shared";
import { buildShotAssetReferenceContext, ensureShotAssetMentionPrompt, resolveShotAssetMentionPrompt } from "./workflow-shot-references";
import { prepareWorkflowImageSubmission } from "./workflow-image-generation";
import { batchArtifactType, type BatchItemState } from "./workflow-batch-plan";
import { useWorkflowImageBatch, type WorkflowImageBatchController } from "./use-workflow-image-batch";
import { WorkflowImageBatchDialog } from "./workflow-image-batch-dialog";
import { applyWorkflowFlowSkills, loadWorkflowFlowBundle } from "./workflow-flow-export";
import { ChapterAssetBindingDrawer, mergeBindingAppendResultIntoProjectCache } from "./chapter-asset-binding-drawer";
import { Select } from "@/components/ui/base/select";

type ShotEditorValues = Omit<ShotRevisionInput, "durationMs"> & {
    title: string;
    durationSeconds: number;
};

type Props = {
    activeStage: ShortDramaWorkflowStage;
    detail: ProjectDetail;
    projectId: string;
    unitId: string;
    workflowStep?: WorkflowStep;
    selectedShot?: ProjectShot;
    onSelectShot: (id: string) => void;
    onRefresh: () => Promise<void>;
    onAddShot: () => void;
    addingShot: boolean;
    onGenerateStoryboard?: () => void;
    generatingStoryboard?: boolean;
    onBatchController?: (controller: WorkflowImageBatchController | null) => void;
};

const productionStageCopy: Record<"storyboard" | "previz" | "video", { label: string; action: string; empty: string }> = {
    storyboard: { label: "分镜图", action: "生成分镜图", empty: "生成静态分镜图，确认构图、景别与角色位置" },
    previz: { label: "动作预演", action: "生成黑白预演", empty: "生成黑白动作预演，确认表演节拍与镜头运动" },
    video: { label: "镜头视频", action: "生成镜头视频", empty: "选择视频模型后生成当前镜头" },
};

export default function WorkflowProductionWorkbench(props: Props) {
    const { activeStage, detail, projectId, unitId, workflowStep, selectedShot, onSelectShot, onRefresh, onAddShot, addingShot } = props;
    const queryClient = useQueryClient();
    const { message, modal } = App.useApp();
    const navigate = useNavigate();
    const effectiveConfig = useEffectiveConfig();
    const isAiConfigReady = useConfigStore((state) => state.isAiConfigReady);
    const creditsEnabled = useUserStore((state) => state.features.creditsEnabled);
    const flowOwnerId = useUserStore((state) => state.user?.id);
    const [form] = Form.useForm<ShotEditorValues>();
    const watchedDuration = Form.useWatch("durationSeconds", form);
    const watchedTitle = Form.useWatch("title", form);
    const [leftTab, setLeftTab] = useState<"assets" | "episodes" | "shots">("episodes");
    const [previewTab, setPreviewTab] = useState<"latest" | "history">("latest");
    const [previewArtifactId, setPreviewArtifactId] = useState("");
    const [imagePreviewArtifact, setImagePreviewArtifact] = useState<ShotArtifact | null>(null);
    const [bindingDrawerOpen, setBindingDrawerOpen] = useState(false);
    const [editorDirty, setEditorDirty] = useState(false);
    const [userEditedForm, setUserEditedForm] = useState(false);
    const [flowProgress, setFlowProgress] = useState("");
    const flowTransfer = useRef<AbortController | null>(null);
    useEffect(() => () => flowTransfer.current?.abort(), [projectId, unitId, selectedShot?.id, editorDirty, flowOwnerId]);
    const [submittingShotIds, setSubmittingShotIds] = useState<Set<string>>(() => new Set());
    const [taskClock, setTaskClock] = useState(() => Date.now());
    const activeShotIdRef = useRef(selectedShot?.id || "");
    activeShotIdRef.current = selectedShot?.id || "";
    const shots = useMemo(() => (detail.shots || []).filter((item) => item.unitId === unitId).slice().sort((left, right) => left.position - right.position), [detail.shots, unitId]);
    const shotIndex = selectedShot ? shots.findIndex((item) => item.id === selectedShot.id) : -1;
    const revision = currentRevision(detail, selectedShot);
    const artifactType = artifactTypeForStage(activeStage);
    const artifacts = useMemo(() => selectedShot ? (detail.shotArtifacts || []).filter((item) => item.shotId === selectedShot.id && item.type === artifactType).slice().sort((left, right) => right.version - left.version) : [], [artifactType, detail.shotArtifacts, selectedShot]);
    const shotTask = useMemo<GenerationTask | undefined>(() => {
        return (detail.tasks || []).filter((task) => task.clientContext?.shotId === selectedShot?.id && task.clientContext?.artifactType === artifactType).sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))[0];
    }, [artifactType, detail.tasks, selectedShot?.id]);
    useEffect(() => {
        if (shotTask?.status !== "queued" && shotTask?.status !== "running") return;
        const timer = window.setInterval(() => setTaskClock(Date.now()), 1_000);
        return () => window.clearInterval(timer);
    }, [shotTask?.status, shotTask?.id]);
    const shotTaskElapsed = shotTask ? formatTaskElapsed(Date.parse(shotTask.startedAt || shotTask.createdAt), taskClock) : "";
    const newestArtifact = artifacts.find((item) => item.selected) || artifacts[0];
    const previewArtifact = artifacts.find((item) => item.id === previewArtifactId) || newestArtifact;
    const generationCapability = activeStage === "video" ? "video" as const : "image" as const;
    const modelOptions = useMemo(() => selectableModelsByCapability(effectiveConfig, generationCapability), [effectiveConfig, generationCapability]);
    const projectDefaultModel = generationCapability === "video" ? detail.project.defaultVideoModel : detail.project.defaultImageModel;
    const globalDefaultModel = generationCapability === "video" ? effectiveConfig.videoModel : effectiveConfig.imageModel;
    const defaultModel = projectDefaultModel && configuredModelMatchesCapability(effectiveConfig, projectDefaultModel, generationCapability) ? projectDefaultModel : globalDefaultModel;
    const initialModel = defaultModel || modelOptions[0] || "";
    const [selectedModel, setSelectedModel] = useState(initialModel);
    const selectedModelRef = useRef(initialModel);
    const [aspectRatio, setAspectRatio] = useState(detail.project.aspectRatio || "16:9");
    const [resolution, setResolution] = useState(effectiveConfig.vquality || "720");
    const [imageQuality, setImageQuality] = useState(effectiveConfig.quality || "auto");
    const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
    const { skills: availableSkills, loading: skillsLoading } = useSkillRuntimeCatalog();
    const shotAssetReferenceContext = useMemo(() => buildShotAssetReferenceContext(detail, selectedShot?.id || ""), [detail, selectedShot?.id]);
    const missingBoundImages = detail.shotReferences.filter((reference) => reference.shotId === selectedShot?.id && reference.status === "linked" && reference.referencedVersion && !shotAssetReferenceContext.referenceImages.some((image) => image.id === reference.referencedVersion?.assetId)).length;
    const referenceByVersionId = useMemo(() => {
        const references = (detail.shotReferences || []).filter((reference) => reference.shotId === selectedShot?.id && reference.role === "reference" && reference.status === "linked");
        return new Map(references.flatMap((reference) => [
            [reference.assetVersionId, reference] as const,
            ...(reference.asset?.primaryVersionId ? [[reference.asset.primaryVersionId, reference] as const] : []),
        ]));
    }, [detail.shotReferences, selectedShot?.id]);
    const currentDurationSeconds = Number(watchedDuration || Math.max(0.5, (selectedShot?.durationMs || 3000) / 1000));
    const generationSeconds = String(Math.max(1, Math.round(currentDurationSeconds)));
    const generationReferenceAudios = generationCapability === "video" ? shotAssetReferenceContext.referenceAudios : [];
    const videoEditOperation = generationCapability === "video" && shotAssetReferenceContext.referenceImages.length ? "reference_to_video" : undefined;
    const modelRequirements = useMemo<ModelRequirements>(() => ({
        capability: generationCapability,
        input: { textCount: 1, imageCount: shotAssetReferenceContext.referenceImages.length, videoCount: 0, audioCount: generationReferenceAudios.length, characterCount: 0 },
        videoOperation: videoEditOperation,
        videoSeconds: generationCapability === "video" ? generationSeconds : undefined,
        imageSize: generationCapability === "image" ? aspectRatio : undefined,
        options: generationCapability === "video"
            ? { size: aspectRatio, vquality: resolution, videoSeconds: Number(generationSeconds) }
            : { size: aspectRatio, quality: imageQuality },
    }), [aspectRatio, generationCapability, generationReferenceAudios.length, generationSeconds, imageQuality, resolution, shotAssetReferenceContext.referenceImages.length, videoEditOperation]);
    const routedModel = resolveCompatibleModel(effectiveConfig, selectedModel, modelRequirements) || selectedModel;
    const activeProfile = useMemo(() => modelCapabilityConfigFor(effectiveConfig, routedModel), [effectiveConfig, routedModel]);
    const videoProfile = generationCapability === "video" ? activeProfile.video : undefined;
    const imageProfile = generationCapability === "image" ? activeProfile.image : undefined;
    const videoBooleanOptions = useMemo(() => generationCapability === "video"
        ? resolveModelVideoBooleanOptions(effectiveConfig, routedModel, {}, {
              videoGenerateAudio: effectiveConfig.videoGenerateAudio,
              videoWatermark: effectiveConfig.videoWatermark,
          })
        : undefined, [effectiveConfig, generationCapability, routedModel]);
    const generationConfig = useMemo(() => ({
        ...effectiveConfig,
        model: routedModel,
        imageModel: generationCapability === "image" ? routedModel : effectiveConfig.imageModel,
        videoModel: generationCapability === "video" ? routedModel : effectiveConfig.videoModel,
        size: aspectRatio,
        quality: imageQuality,
        vquality: resolution,
        videoSeconds: generationSeconds,
        ...(videoBooleanOptions || {}),
    }), [aspectRatio, effectiveConfig, generationCapability, generationSeconds, imageQuality, resolution, routedModel, videoBooleanOptions]);
    const priceChannel = resolveModelChannel(generationConfig, routedModel);
    const configuredCredits = requestCreditCost({
        channelMode: priceChannel.scope === "system" ? "remote" : "local",
        modelCosts: priceChannel.modelCosts,
        model: modelOptionName(routedModel),
        count: 1,
        seconds: generationCapability === "video" ? generationSeconds : 1,
        capability: generationCapability,
        config: generationConfig,
        requirements: modelRequirements,
    });
    const quoteRequest = useMemo(() => modelQuoteRequest(generationConfig, routedModel, generationCapability, modelRequirements), [generationCapability, generationConfig, modelRequirements, routedModel]);
    const quoteRequestKey = JSON.stringify(quoteRequest || null);
    const [routeQuote, setRouteQuote] = useState<LogicalModelQuote | null>(null);
    const generationCredits = routeQuote ? routeQuote.amountMicrocredits / 1_000_000 : configuredCredits;
    const formattedGenerationCredits = generationCredits?.toLocaleString("zh-CN", { maximumFractionDigits: 6 });
    const modelSummary = routedModel ? modelDisplayName(effectiveConfig, routedModel) : "未选择模型";
    const durationSummary = `${Number(watchedDuration || Math.max(0.5, (selectedShot?.durationMs || 3000) / 1000))}s`;
    const resolutionSummary = generationCapability === "video" ? formatVideoResolutionLabel(resolution) : imageQuality.toUpperCase();

    useEffect(() => {
        selectedModelRef.current = initialModel;
        setSelectedModel(initialModel);
        if (!initialModel) return;
        const profile = modelCapabilityConfigFor(effectiveConfig, initialModel);
        if (generationCapability === "video" && profile.video) {
            const normalized = normalizeVideoValue(profile.video, {
                seconds: effectiveConfig.videoSeconds,
                ratio: detail.project.aspectRatio || effectiveConfig.size,
                resolution: effectiveConfig.vquality,
            });
            setAspectRatio(normalized.ratio);
            setResolution(normalized.resolution);
            form.setFieldValue("durationSeconds", Number(normalized.seconds));
        } else if (generationCapability === "image" && profile.image) {
            const normalized = normalizeImageValue(profile.image, { size: detail.project.aspectRatio || effectiveConfig.size, quality: effectiveConfig.quality, count: "1" });
            setAspectRatio(normalized.size);
            setImageQuality(normalized.quality);
        }
    }, [detail.project.aspectRatio, effectiveConfig, form, generationCapability, initialModel]);

    useEffect(() => {
        if (!creditsEnabled || !quoteRequest) {
            setRouteQuote(null);
            return;
        }
        const controller = new AbortController();
        setRouteQuote(null);
        quoteModel(quoteRequest, controller.signal)
            .then(({ quote }) => setRouteQuote(quote))
            .catch(() => {
                if (!controller.signal.aborted) setRouteQuote(null);
            });
        return () => controller.abort();
        // quoteRequestKey captures the normalized request without retriggering on object identity.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [creditsEnabled, quoteRequestKey]);

    useEffect(() => {
        const shotDurationSeconds = Math.max(0.5, (revision?.durationMs || selectedShot?.durationMs || 3000) / 1000);
        const currentModel = selectedModelRef.current || initialModel;
        const normalizedDurationSeconds = generationCapability === "video" && currentModel
            ? Number(normalizeVideoValue(modelCapabilityConfigFor(effectiveConfig, currentModel).video!, { seconds: String(shotDurationSeconds) }).seconds)
            : shotDurationSeconds;
        const videoPrompt = ensureShotAssetMentionPrompt(revision?.videoPrompt || "", shotAssetReferenceContext.mentionReferences);
        form.setFieldsValue({
            title: normalizeDefaultShotTitle(selectedShot?.title, Math.max(0, shotIndex)),
            plotDescription: revision?.plotDescription || selectedShot?.description || "",
            action: revision?.action || "",
            dialogue: revision?.dialogue || "",
            shotSize: revision?.shotSize || "",
            cameraAngle: revision?.cameraAngle || "",
            cameraMovement: revision?.cameraMovement || "",
            durationSeconds: normalizedDurationSeconds,
            imagePrompt: revision?.imagePrompt || "",
            videoPrompt,
            negativePrompt: revision?.negativePrompt || "",
            continuityNotes: revision?.continuityNotes || "",
        });
        setPreviewArtifactId("");
        setImagePreviewArtifact(null);
        setUserEditedForm(false);
        setEditorDirty(Boolean(selectedShot && (!revision || videoPrompt !== revision.videoPrompt)));
    }, [effectiveConfig, form, generationCapability, initialModel, revision?.id, selectedShot?.id, shotAssetReferenceContext.mentionReferences]);

    const scriptDirtyForBatch = Boolean(selectedShot && (userEditedForm || (revision && editorDirty)));
    const defaultBatchImageModel = useMemo(() => {
        const imageModels = selectableModelsByCapability(effectiveConfig, "image");
        if (detail.project.defaultImageModel && configuredModelMatchesCapability(effectiveConfig, detail.project.defaultImageModel, "image")) {
            return detail.project.defaultImageModel;
        }
        if (effectiveConfig.imageModel && configuredModelMatchesCapability(effectiveConfig, effectiveConfig.imageModel, "image")) {
            return effectiveConfig.imageModel;
        }
        return imageModels[0] || "";
    }, [detail.project.defaultImageModel, effectiveConfig]);
    const initialBatchSettings = useMemo(() => ({
        model: generationCapability === "image" && selectedModel ? selectedModel : defaultBatchImageModel,
        size: detail.project.aspectRatio || effectiveConfig.size || "16:9",
        quality: effectiveConfig.quality || "auto",
        skillIds: selectedSkillIds,
    }), [defaultBatchImageModel, detail.project.aspectRatio, effectiveConfig.quality, effectiveConfig.size, generationCapability, selectedModel, selectedSkillIds]);
    const batch = useWorkflowImageBatch({
        detail,
        projectId,
        unitId,
        editorDirty: scriptDirtyForBatch,
        config: effectiveConfig,
        skills: availableSkills,
        initialSettings: initialBatchSettings,
        onRefresh,
    });
    useEffect(() => { props.onBatchController?.(batch); });
    useEffect(() => () => props.onBatchController?.(null), [props.onBatchController]);
    const chapterMutationLocked = batch.chapterMutationLocked;
    const hasActiveOrUnknownBatch = Boolean(
        batch.log &&
            (batch.log.state === "running" ||
                batch.log.state === "waiting" ||
                batch.log.items.some(
                    (item) =>
                        item.state === "submitting" ||
                        item.state === "submitted" ||
                        item.state === "syncing" ||
                        item.state === "unknown",
                )),
    );
    const batchEntryDisabled = scriptDirtyForBatch || Boolean(batch.blockedReason) || batch.busy || hasActiveOrUnknownBatch;
    const selectedShotLocked = Boolean(selectedShot && batch.lockedShotIds.has(selectedShot.id));
    const batchSlotStates = useMemo(() => {
        const map = new Map<string, BatchItemState>();
        if (!batch.log) return map;
        const slotType = batchArtifactType(batch.log.stage);
        for (const item of batch.log.items) {
            map.set(`${item.shotId}:${slotType}`, item.state);
        }
        return map;
    }, [batch.log]);

    const changeGenerationModel = (nextModel: string) => {
        selectedModelRef.current = nextModel;
        setSelectedModel(nextModel);
        const profile = modelCapabilityConfigFor(effectiveConfig, nextModel);
        if (generationCapability === "video" && profile.video) {
            const normalized = normalizeVideoValue(profile.video, {
                seconds: String(form.getFieldValue("durationSeconds") || generationSeconds),
                ratio: aspectRatio,
                resolution,
            });
            setAspectRatio(normalized.ratio);
            setResolution(normalized.resolution);
            form.setFieldValue("durationSeconds", Number(normalized.seconds));
            return;
        }
        if (generationCapability === "image" && profile.image) {
            const normalized = normalizeImageValue(profile.image, { size: aspectRatio, quality: imageQuality, count: "1" });
            setAspectRatio(normalized.size);
            setImageQuality(normalized.quality);
        }
    };

    const saveShot = useMutation({
        mutationFn: async (values: ShotEditorValues) => {
            if (!selectedShot) throw new Error("请先选择镜头");
            if (batch.lockedShotIds.has(selectedShot.id)) throw new Error("当前镜头正在参与本章批量任务，请等待该镜头完成或停止剩余提交后再修改");
            return batch.runChapterMutation(async () => saveProjectShot(projectId, {
                id: selectedShot.id,
                unitId,
                title: values.title,
                description: values.plotDescription,
                position: selectedShot.position,
                durationMs: Math.round(values.durationSeconds * 1000),
                status: selectedShot.status,
                revision: revisionInput(values),
            }), selectedShot.id);
        },
        onSuccess: async () => { setEditorDirty(false); setUserEditedForm(false); await onRefresh(); message.success("镜头脚本已保存为新版本"); },
        onError: (error) => message.error(error instanceof Error ? error.message : "镜头保存失败"),
    });

    const deleteShot = useMutation({
        mutationFn: async ({ shotId }: { shotId: string; nextShotId: string }) => {
            if (batch.lockedShotIds.has(shotId)) throw new Error("当前镜头正在参与本章批量任务，请等待该镜头完成或停止剩余提交后再删除");
            return batch.runChapterMutation(() => deleteProjectShot(projectId, shotId), shotId);
        },
        onSuccess: async (_result, { nextShotId }) => {
            onSelectShot(nextShotId);
            await onRefresh();
            message.success("镜头已删除");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "镜头删除失败"),
    });

    const changeAssetBinding = useMutation({
        mutationFn: async ({ asset, reference }: { asset?: ProjectAsset; reference?: ShotAssetReference }) => {
            if (!selectedShot) throw new Error("请先选择镜头");
            if (batch.lockedShotIds.has(selectedShot.id)) throw new Error("当前镜头正在参与本章批量任务，请等待该镜头完成或停止剩余提交后再调整资产");
            return batch.runChapterMutation(async () => {
                if (reference) return unlinkShotAsset(projectId, selectedShot.id, reference.id);
                if (!asset?.primaryVersionId) throw new Error("该资产还没有可绑定版本");
                return linkShotAsset(projectId, selectedShot.id, { assetVersionId: asset.primaryVersionId, role: "reference" });
            }, selectedShot.id);
        },
        onSuccess: async (_result, variables) => { await onRefresh(); message.success(variables.reference ? "已取消当前镜头的资产引用" : "资产已绑定到当前镜头"); },
        onError: (error) => message.error(error instanceof Error ? error.message : "镜头资产更新失败"),
    });

    const generateArtifact = async () => {
        if (!selectedShot || submittingShotIds.has(selectedShot.id)) return;
        if (batch.lockedShotIds.has(selectedShot.id)) {
            message.warning("当前镜头正在参与本章批量任务，请等待该镜头完成或停止剩余提交后再发起单镜生成");
            return;
        }
        const submittingShot = selectedShot;
        setSubmittingShotIds((current) => new Set(current).add(submittingShot.id));
        try {
            await batch.runChapterMutation(async assertCurrent => {
            const values = await form.validateFields();
            assertCurrent();
            let productionStep = workflowStep;
            if (!productionStep) {
                const initialized = await createUnitWorkflow(projectId, unitId);
                productionStep = (initialized.workflow.steps || []).find((step) => step.stepKey === activeStage);
            }
            if (!productionStep) throw new Error("当前生成阶段不可用，请刷新页面后重试");
            if (productionStep.status === "failed") throw new Error("当前生成阶段失败，请刷新后重试");
            if (!routedModel) throw new Error(activeStage === "video" ? "请先配置视频模型" : "请先配置图片模型");
            const compatibilityError = modelCompatibilityError(effectiveConfig, routedModel, modelRequirements);
            if (compatibilityError) throw new Error(`当前模型配置不可用：${compatibilityError}`);
            assertCurrent();
            const saved = await saveProjectShot(projectId, {
                id: submittingShot.id,
                unitId,
                title: values.title,
                description: values.plotDescription,
                position: submittingShot.position,
                durationMs: Math.round(values.durationSeconds * 1000),
                status: submittingShot.status,
                revision: revisionInput(values),
            });
            const mode = generationCapability;
            const config = { ...generationConfig, videoSeconds: String(Math.max(1, Math.round(values.durationSeconds))) };
            if (!isAiConfigReady(config, routedModel)) throw new Error("当前模型渠道配置不完整，请先到设置中补齐");
            if (mode === "image") {
                const inputValues = revisionInput(values);
                const savedRevision = {
                    version: revision?.version ?? 1,
                    actionBeatsJson: revision?.actionBeatsJson ?? "[]",
                    createdAt: revision?.createdAt ?? saved.shot.updatedAt,
                    id: saved.shot.currentRevisionId || "",
                    shotId: saved.shot.id,
                    plotDescription: inputValues.plotDescription,
                    action: inputValues.action || "",
                    dialogue: inputValues.dialogue || "",
                    shotSize: inputValues.shotSize || "",
                    cameraAngle: inputValues.cameraAngle || "",
                    cameraMovement: inputValues.cameraMovement || "",
                    durationMs: inputValues.durationMs || 3000,
                    imagePrompt: inputValues.imagePrompt || "",
                    videoPrompt: inputValues.videoPrompt || "",
                    negativePrompt: inputValues.negativePrompt || "",
                    continuityNotes: inputValues.continuityNotes || "",
                };
                const submission = await prepareWorkflowImageSubmission({
                    detail,
                    shot: saved.shot,
                    revision: savedRevision,
                    stage: activeStage === "previz" ? "previz" : "storyboard",
                    settings: {
                        model: selectedModel,
                        size: aspectRatio,
                        quality: imageQuality,
                        skillIds: selectedSkillIds,
                    },
                    config: effectiveConfig,
                }, {
                    workflowStepId: productionStep.id,
                    skills: availableSkills,
                });
                await submitBackendGenerationTask(submission);
            } else {
                const basePrompt = [values.videoPrompt || values.plotDescription, values.action, values.dialogue && `台词：${values.dialogue}`, values.continuityNotes].filter(Boolean).join("\n");
                const resolvedPrompt = resolveShotAssetMentionPrompt(basePrompt, shotAssetReferenceContext, { dialogue: values.dialogue, mode });
                const skillExecution = await skillRuntime.prepare({
                    profile: "shortDrama",
                    prompt: resolvedPrompt,
                    skills: availableSkills,
                    selectedSkillIds,
                });
                await submitBackendGenerationTask({
                    projectId,
                    mode,
                    prompt: skillExecution.prompt,
                    config,
                    referenceImages: shotAssetReferenceContext.referenceImages,
                    referenceAudios: generationReferenceAudios,
                    metadata: {
                        ...skillExecution.metadata,
                        workflowStepId: productionStep.id,
                        domainProjectId: projectId,
                        unitId,
                        shotId: saved.shot.id,
                        shotRevisionId: saved.shot.currentRevisionId,
                        artifactType,
                        role: "output",
                        source: "short-drama-workflow",
                        ...(shotAssetReferenceContext.referenceImages.length ? { videoEditOperation: "reference_to_video" } : {}),
                        resolvedCharacterVersions: shotAssetReferenceContext.resolvedCharacterVersions,
                        artifactMetadata: { model: routedModel, aspectRatio, resolution, durationSeconds: values.durationSeconds, ...skillExecution.metadata },
                    },
                });
            }
            if (activeShotIdRef.current === submittingShot.id) {
                setEditorDirty(false);
                setUserEditedForm(false);
            }
            await onRefresh();
            message.success(`${productionStageCopy[activeStage as "storyboard" | "previz" | "video"].label}任务已提交`);
            }, submittingShot.id);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "生成任务提交失败");
        } finally {
            setSubmittingShotIds((current) => {
                const next = new Set(current);
                next.delete(submittingShot.id);
                return next;
            });
        }
    };

    const stageCopy = productionStageCopy[activeStage as "storyboard" | "previz" | "video"];
    const selectedShotSubmitting = submittingShotIds.has(selectedShot?.id || "");

    const exportToFlow = async (scope: "current" | "chapter") => {
        if (flowTransfer.current) return;
        if (editorDirty) return void message.warning("当前镜头有未保存修改，请先保存脚本再提交到 Flow");
        if (!flowOwnerId) return void message.warning("请先登录后再提交到 Flow");
        if (scope === "current" && !selectedShot) return void message.warning("请先选择镜头");
        const controller = new AbortController();
        flowTransfer.current = controller;
        setFlowProgress("正在读取已保存的镜头…");
        try {
            const bundle = await loadWorkflowFlowBundle({
                projectId, unitId, shotId: scope === "current" ? selectedShot?.id : undefined,
                editorDirty, origin: window.location.origin,
                resolution, signal: controller.signal,
            });
            setFlowProgress("正在整理完整视频提示词…");
            const prepared = await applyWorkflowFlowSkills(bundle, { skills: availableSkills, selectedSkillIds, signal: controller.signal });
            const result = await submitFlowBundle(prepared, {
                signal: controller.signal,
                onProgress: (text) => {
                    if (useUserStore.getState().user?.id !== flowOwnerId) controller.abort();
                    if (!controller.signal.aborted) setFlowProgress(text);
                },
            });
            message.success(`已提交 ${result.shotCount} 个镜头，请打开 Flow 插件预览并确认生成`);
        } catch (error) {
            if (!(error instanceof Error && error.name === "AbortError")) message.error(error instanceof Error ? error.message : "提交到 Flow 失败");
        } finally {
            if (flowTransfer.current === controller) {
                flowTransfer.current = null;
                setFlowProgress("");
            }
        }
    };

    const chapterActionsSection = (
        <div className="workflow-chapter-actions" role="group" aria-label="本章操作">
            <span className="workflow-chapter-actions-label">本章操作</span>
            <Button size="small" disabled={chapterMutationLocked} onClick={() => setBindingDrawerOpen(true)}>匹配镜头资产</Button>
            <Button size="small" disabled={batchEntryDisabled} onClick={() => void batch.open("storyboard")}>批量生成分镜图</Button>
            <Button size="small" disabled={batchEntryDisabled} onClick={() => void batch.open("previz")}>批量生成动作预演</Button>
            <Dropdown trigger={["click"]} disabled={Boolean(flowProgress) || !shots.length || saveShot.isPending || deleteShot.isPending || changeAssetBinding.isPending || selectedShotSubmitting} menu={{
                items: [
                    { key: "current", label: "提交当前镜头", disabled: !selectedShot },
                    { key: "chapter", label: `提交本章全部 ${shots.length} 个镜头` },
                ],
                onClick: ({ key }) => void exportToFlow(key === "current" ? "current" : "chapter"),
            }}>
                <Button size="small" loading={Boolean(flowProgress)} disabled={!shots.length || saveShot.isPending || deleteShot.isPending || changeAssetBinding.isPending || selectedShotSubmitting}>提交到 Flow<ChevronDown className="size-3" /></Button>
            </Dropdown>
            {flowProgress ? <><span role="status" aria-live="polite" className="text-xs text-muted-foreground">{flowProgress}</span><Button size="small" onClick={() => flowTransfer.current?.abort()}>取消提交</Button></> : null}
        </div>
    );

    const chapterBatchBarSection = batch.log || batch.blockedReason || batch.runtimeError || batch.statusNotice ? (
        <div className="workflow-chapter-batch-bar" role="status" aria-live="polite">
            <div className="flex flex-wrap items-center gap-2 text-xs">
                {batch.log ? (
                    <span>
                        本章{batch.log.stage === "previz" ? "动作预演" : "分镜图"}批次（{batch.log.state}）：成功 {batch.log.items.filter((item) => item.state === "succeeded").length} / {batch.log.items.length}
                        {batch.log.items.some((item) => item.state === "submitting" || item.state === "submitted" || item.state === "syncing") ? ` · 进行中 ${batch.log.items.filter((item) => item.state === "submitting" || item.state === "submitted" || item.state === "syncing").length}` : ""}
                        {batch.log.items.some((item) => item.state === "pending") ? ` · 待提交 ${batch.log.items.filter((item) => item.state === "pending").length}` : ""}
                        {batch.log.items.some((item) => item.state === "failed") ? ` · 失败 ${batch.log.items.filter((item) => item.state === "failed").length}` : ""}
                        {batch.log.items.some((item) => item.state === "unknown") ? ` · 待核对 ${batch.log.items.filter((item) => item.state === "unknown").length}` : ""}
                    </span>
                ) : null}
                {batch.blockedReason ? <span className="text-danger">{batch.blockedReason}</span> : null}
                {batch.runtimeError ? <span className="text-danger">{batch.runtimeError}</span> : null}
                {batch.statusNotice ? <span className="text-muted-foreground">{batch.statusNotice}</span> : null}
            </div>
            <div className="flex flex-wrap items-center gap-2">
                {batch.corruptedLogError ? (
                    <Button size="small" danger onClick={() => void batch.clearCorruptedLog()}>清理损坏记录</Button>
                ) : null}
                {batch.log ? (
                    <Button size="small" onClick={() => void batch.open(batch.log!.stage)}>查看本章批次</Button>
                ) : null}
                {batch.log && (batch.log.state === "running" || batch.log.state === "waiting" || batch.log.items.some((item) => item.state === "pending")) && batch.log.state !== "stopped" && batch.log.state !== "finished" ? (
                    <Button size="small" danger onClick={() => void batch.stopRemaining()}>停止剩余提交</Button>
                ) : null}
                {batch.log && batch.log.items.some((item) => item.state === "submitted" || item.state === "syncing" || item.state === "unknown") ? (
                    <Button size="small" loading={batch.busy} onClick={() => void batch.refreshStatus()}>刷新状态</Button>
                ) : null}
            </div>
        </div>
    ) : null;

    if (!selectedShot) {
        return (
            <div className="workflow-production-shell">
                {chapterActionsSection}
                {chapterBatchBarSection}
                <div className="workflow-empty-shot">
                    <EmptyState
                        size="compact"
                        title="当前章节还没有分镜"
                        action={
                            <div className="flex gap-2">
                                <Button icon={<Plus className="size-4" />} loading={addingShot} disabled={chapterMutationLocked} onClick={onAddShot}>新增第一个分镜</Button>
                                {activeStage === "storyboard" && props.onGenerateStoryboard ? (
                                    <Button type="primary" loading={props.generatingStoryboard} disabled={chapterMutationLocked} onClick={props.onGenerateStoryboard}>生成章节分镜</Button>
                                ) : null}
                            </div>
                        }
                    />
                </div>
                <WorkflowImageBatchDialog controller={batch} config={effectiveConfig} skills={availableSkills} />
            </div>
        );
    }

    const requestShotSelection = (nextShotId: string) => {
        if (nextShotId === selectedShot.id) return;
        if (!editorDirty) {
            onSelectShot(nextShotId);
            return;
        }
        modal.confirm({
            title: "当前镜头有未保存修改",
            content: "切换镜头会放弃这些修改。",
            okText: "放弃修改并切换",
            cancelText: "继续编辑",
            onOk: () => onSelectShot(nextShotId),
        });
    };

    const requestAddShot = () => {
        if (!editorDirty) {
            onAddShot();
            return;
        }
        modal.confirm({
            title: "当前镜头有未保存修改",
            content: "新增镜头会离开当前编辑内容。",
            okText: "放弃修改并新增",
            cancelText: "继续编辑",
            onOk: onAddShot,
        });
    };

    const requestDeleteShot = () => {
        const nextShot = shots[shotIndex + 1] || shots[shotIndex - 1];
        modal.confirm({
            title: `删除镜头“${watchedTitle || selectedShot.title || "未命名镜头"}”？`,
            content: editorDirty
                ? "该镜头的未保存修改、脚本版本、资产引用和生成产物都会被删除，且无法恢复。"
                : "该镜头的脚本版本、资产引用和生成产物都会被删除，且无法恢复。",
            okText: "删除镜头",
            okButtonProps: { danger: true },
            cancelText: "取消",
            centered: true,
            onOk: () => deleteShot.mutateAsync({ shotId: selectedShot.id, nextShotId: nextShot?.id || "" }),
        });
    };

    const selectRelativeShot = (offset: number) => {
        const next = shots[shotIndex + offset];
        if (next) requestShotSelection(next.id);
    };

    const handleAssetBindingsApplied = async (result: BindingAppendResult) => {
        mergeBindingAppendResultIntoProjectCache(queryClient, projectId, unitId, result);
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["project", projectId] }),
            queryClient.invalidateQueries({ queryKey: ["projects"] }),
            onRefresh(),
        ]);
        message.success(result.addedCount > 0 ? `已追加 ${result.addedCount} 项镜头资产引用` : "镜头资产引用已是最新，无新增变更");
    };

    return (
        <div className="workflow-production-shell">
            <div className="workflow-production-main">
                <aside className="workflow-library-panel">
                        <SegmentedControl
                        block
                        size="sm"
                        value={leftTab}
                        onChange={(value) => setLeftTab(value as typeof leftTab)}
                        options={[{ value: "episodes", label: "章节" }, { value: "shots", label: "镜头" }, { value: "assets", label: "资产" }]}
                    />
                    <div className="workflow-library-scroll thin-scrollbar">
                        {leftTab === "assets" ? <AssetLibrary detail={detail} referenceByVersionId={referenceByVersionId} changing={changeAssetBinding.isPending || selectedShotLocked} onToggle={(asset, reference) => changeAssetBinding.mutate({ asset, reference })} /> : null}
                        {leftTab === "episodes" ? <EpisodeLibrary detail={detail} activeUnitId={unitId} projectId={projectId} activeStage={activeStage} /> : null}
                        {leftTab === "shots" ? <ShotLibrary detail={detail} shots={shots} selectedShotId={selectedShot.id} onSelectShot={requestShotSelection} /> : null}
                    </div>
                </aside>

                <section className="workflow-shot-editor">
                    {chapterActionsSection}
                    {chapterBatchBarSection}
                    {selectedShotLocked ? <p role="status" className="px-4 py-2 text-xs text-warning">当前镜头正在参与本章批量任务，请等待该镜头完成或停止剩余提交后再修改脚本、资产或单镜生成。</p> : null}
                    {missingBoundImages > 0 ? <p role="status" className="px-4 py-2 text-sm text-muted-foreground">有 {missingBoundImages} 个绑定版本暂无参考图，仅使用可用图片生成。</p> : null}
                    <header className="workflow-panel-header">
                        {activeStage === "storyboard" && props.onGenerateStoryboard ? <Button loading={props.generatingStoryboard} disabled={editorDirty || chapterMutationLocked} onClick={props.onGenerateStoryboard}>生成章节分镜</Button> : null}
                        <div className="workflow-shot-heading">
                            <span className="workflow-shot-number">{formatShotOrdinal(shotIndex)}</span>
                            {customShotTitle(watchedTitle || selectedShot.title, shotIndex) ? <h2>{customShotTitle(watchedTitle || selectedShot.title, shotIndex)}</h2> : null}
                            <StatusBadge tone={saveShot.isPending ? "loading" : editorDirty ? "warning" : revision ? "success" : "neutral"} label={saveShot.isPending ? "保存中" : editorDirty ? "有未保存修改" : revision ? "已保存" : "草稿"} className="m-0" />
                        </div>
                        <div className="flex items-center gap-1"><span className="mr-1 text-[var(--fs-micro)] text-foreground/45">{shotIndex + 1} / {shots.length}</span><Button type="text" size="small" icon={<ChevronLeft className="size-4" />} disabled={shotIndex <= 0} onClick={() => selectRelativeShot(-1)} aria-label="上一个镜头" /><Button type="text" size="small" icon={<ChevronRight className="size-4" />} disabled={shotIndex >= shots.length - 1} onClick={() => selectRelativeShot(1)} aria-label="下一个镜头" /></div>
                    </header>
                    <Form form={form} layout="vertical" className="workflow-shot-form" onValuesChange={() => { setEditorDirty(true); setUserEditedForm(true); }} onFinish={(values) => saveShot.mutate(values)}>
                        <div className="workflow-shot-form-scroll thin-scrollbar">
                            <div className="workflow-form-section-heading"><span>镜头脚本</span><small>先写清镜头里发生什么，再调整生成参数</small></div>
                            <Form.Item name="title" label="镜头名称" rules={[{ required: true, message: "请输入镜头名称" }]}><Input placeholder="用一句话概括这个镜头" /></Form.Item>
                            <Form.Item name="videoPrompt" label="视频提示词" rules={[{ required: true, message: "请输入视频提示词" }]}><ShotAssetMentionTextarea references={shotAssetReferenceContext.mentionReferences} /></Form.Item>
                            <BoundAssets detail={detail} shotId={selectedShot.id} changing={changeAssetBinding.isPending || selectedShotLocked} onUnlink={(reference) => changeAssetBinding.mutate({ reference })} />
                            <div className="workflow-form-grid">
                                <Form.Item name="action" label="表演与动作"><Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} placeholder="按动作节拍描述人物表演、走位和物体运动" /></Form.Item>
                                <Form.Item name="dialogue" label="对白 / 旁白"><Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} placeholder="填写对白、旁白或需要保留的声音信息" /></Form.Item>
                            </div>
                            <WorkflowDisclosure
                                icon={<SlidersHorizontal />}
                                title="生成设置"
                                description="生成规格与镜头语言"
                                summary={<><span>{durationSummary}</span><span>{aspectRatio}</span><span>{resolutionSummary}</span><span className="is-model">{modelSummary}</span></>}
                            >
                                <div className="workflow-settings-section">
                                    <div className="workflow-settings-section-title">生成规格</div>
                                    <Form.Item label="生成模型">
                                        <ModelPicker
                                            config={generationConfig}
                                            value={selectedModel}
                                            capability={generationCapability}
                                            requirements={modelRequirements}
                                            onChange={changeGenerationModel}
                                            fullWidth
                                            className="workflow-model-picker"
                                            placeholder={activeStage === "video" ? "选择视频模型" : "选择图片模型"}
                                            showSelectedPrice
                                        />
                                    </Form.Item>
                                    <Form.Item label="技能库"><SkillRuntimePicker profile="shortDrama" skills={availableSkills} loading={skillsLoading} value={selectedSkillIds} onChange={setSelectedSkillIds} /></Form.Item>
                                    <div className="workflow-form-grid is-three">
                                        <Form.Item name="durationSeconds" label="镜头时长（秒）">
                                            {generationCapability === "video" && videoProfile?.duration.selection === "enum"
                                                ? <Select options={videoDurationOptions(videoProfile).map((value) => ({ value, label: `${value} 秒` }))} />
                                                : <InputNumber className="w-full" min={generationCapability === "video" ? videoProfile?.duration.min || 1 : 0.5} max={generationCapability === "video" ? videoProfile?.duration.max || 60 : 60} step={generationCapability === "video" ? videoProfile?.duration.step || 1 : 0.5} />}
                                        </Form.Item>
                                        {generationCapability === "video" ? <Form.Item label="画幅"><Select value={aspectRatio} onChange={setAspectRatio} options={(videoProfile?.ratios || []).map((value) => ({value, label:value}))} /></Form.Item> : null}
                                        {generationCapability === "video" ? (
                                            <Form.Item label="分辨率"><Select value={resolution} onChange={setResolution} options={(videoProfile?.resolutions || []).map((value) => ({ value, label: formatVideoResolutionLabel(value) }))} /></Form.Item>
                                        ) : imageProfile?.quality.supported && !imageResolutionUsesQuality(imageProfile) ? (
                                            <Form.Item label="生成画质"><Select value={imageQuality} onChange={setImageQuality} options={imageProfile.quality.values.map((value) => ({ value, label: imageQualityLabel(value) }))} /></Form.Item>
                                        ) : <div />}
                                    </div>
                                    {generationCapability === "image" && imageProfile ? <ImageSizePicker profile={imageProfile} size={aspectRatio} quality={imageQuality} onChange={(size, quality) => { setAspectRatio(size); if (quality) setImageQuality(quality); }} /> : null}
                                </div>
                                <div className="workflow-settings-section">
                                    <div className="workflow-settings-section-title">镜头语言</div>
                                    <div className="workflow-form-grid is-three">
                                        <Form.Item name="shotSize" label="景别"><Select allowClear placeholder="自动" options={["特写", "近景", "中景", "全景", "远景"].map((value) => ({ value, label: value }))} /></Form.Item>
                                        <Form.Item name="cameraAngle" label="机位角度"><Select allowClear placeholder="自动" options={["平视", "俯拍", "仰拍", "侧面", "过肩"].map((value) => ({ value, label: value }))} /></Form.Item>
                                        <Form.Item name="cameraMovement" label="运镜方式"><Select allowClear placeholder="自动" options={["固定", "推镜", "拉镜", "摇镜", "移镜", "跟拍"].map((value) => ({ value, label: value }))} /></Form.Item>
                                    </div>
                                </div>
                            </WorkflowDisclosure>
                            <WorkflowDisclosure
                                className="is-advanced"
                                icon={<WandSparkles />}
                                title="生成补充"
                                description="仅在模型需要额外约束时填写"
                                summary={<span>提示词 · 排除内容 · 接戏</span>}
                            >
                                <div className="workflow-form-grid">
                                    <Form.Item name="plotDescription" label="镜头画面" rules={[{ required: true, message: "请输入镜头画面" }]}><ShotAssetMentionTextarea variant="scene" references={shotAssetReferenceContext.mentionReferences} /></Form.Item>
                                    <Form.Item name="imagePrompt" label="画面提示词"><Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} placeholder="留空时根据镜头画面自动生成" /></Form.Item>
                                    <Form.Item name="negativePrompt" label="排除内容"><Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="填写不希望出现的元素、动作或画面问题" /></Form.Item>
                                    <Form.Item name="continuityNotes" label="接戏备注"><Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="记录人物位置、朝向、服装、道具及前后镜延续关系" /></Form.Item>
                                </div>
                            </WorkflowDisclosure>
                        </div>
                        <footer className="workflow-editor-actions">
                            <div className="workflow-generation-cost" aria-live="polite">
                                {creditsEnabled && formattedGenerationCredits ? <><CreditSymbol /><span title={routeQuote ? modelQuoteDescription(routeQuote) : undefined}>本次{routeQuote?.estimated ? "预估" : "费用"} {formattedGenerationCredits} 积分</span></> : creditsEnabled && routedModel ? <span>本次费用将在提交时按实际规格计算</span> : null}
                            </div>
                            <div className="flex items-center gap-2"><Button danger icon={<Trash2 className="size-4" />} loading={deleteShot.isPending} disabled={saveShot.isPending || selectedShotSubmitting || changeAssetBinding.isPending || selectedShotLocked} onClick={requestDeleteShot}>删除镜头</Button><Button htmlType="submit" icon={<Save className="size-4" />} loading={saveShot.isPending} disabled={!editorDirty || deleteShot.isPending || selectedShotLocked}>保存脚本</Button><Button type="primary" icon={<Play className="size-4" />} loading={selectedShotSubmitting || shotTask?.status === "queued" || shotTask?.status === "running"} disabled={deleteShot.isPending || selectedShotLocked} onClick={() => void generateArtifact()}>{selectedShotSubmitting ? `${stageCopy.action}（正在提交）` : shotTask?.status === "queued" || shotTask?.status === "running" ? `${stageCopy.action}（已运行${shotTaskElapsed}）` : shotTask?.status === "failed" ? `${stageCopy.action}（上次失败，可重试）` : shotTask?.status === "succeeded" && !newestArtifact ? `${stageCopy.action}（已完成，正在同步）` : newestArtifact ? `${stageCopy.action}（已生成）` : stageCopy.action}</Button></div>
                        </footer>
                    </Form>
                </section>

                <aside className="workflow-preview-panel">
                    <header className="workflow-preview-header">
                        <div className="workflow-preview-header-row">
                            <div className="workflow-preview-title"><Film className="size-4 shrink-0" /><span>产物预览</span></div>
<SegmentedControl size="sm" value={previewTab} onChange={(value) => setPreviewTab(value as typeof previewTab)} options={[{ value: "latest", label: "最新" }, { value: "history", label: `历史 ${artifacts.length}` }]} />
                        </div>
<SegmentedControl
                            block
                            size="sm"
                            className="workflow-preview-stage-switch"
                            value={activeStage}
                            options={[{ value: "storyboard", label: "分镜图" }, { value: "previz", label: "动作预演" }, { value: "video", label: "镜头视频" }]}
                            onChange={(nextStage) => navigate(`/projects/${projectId}/workflow/${unitId}/${nextStage}`)}
                        />
                    </header>
                    <div className="workflow-preview-scroll thin-scrollbar">
                        {previewTab === "latest" ? <LatestPreview artifact={previewArtifact} emptyText={stageCopy.empty} onPreviewImage={setImagePreviewArtifact} /> : <ArtifactHistory artifacts={artifacts} activeId={previewArtifact?.id} onSelect={(artifact) => { setPreviewArtifactId(artifact.id); setPreviewTab("latest"); }} />}
                        <div className="workflow-preview-summary"><div className="flex items-center justify-between gap-2"><span className="text-xs font-medium">当前产物</span><ArtifactStatus artifact={newestArtifact} compact /></div><div className="mt-1 text-[var(--fs-micro)] text-foreground/45">{newestArtifact ? `${formatDuration(selectedShot.durationMs)} · ${resolution}p · v${newestArtifact.version}` : "当前镜头还没有生成产物"}</div></div>
                        <div className="workflow-preview-actions"><Button icon={<RefreshCcw className="size-3.5" />} loading={selectedShotSubmitting || shotTask?.status === "queued" || shotTask?.status === "running"} disabled={selectedShotLocked} onClick={() => void generateArtifact()}>重新生成</Button><Button icon={<Download className="size-3.5" />} disabled={!previewArtifact?.resourceId} onClick={() => previewArtifact?.resourceId && void downloadArtifact(previewArtifact, selectedShot.title, message.error)}>下载{activeStage === "video" ? "视频" : "图片"}</Button></div>
                        <ArtifactHistory artifacts={artifacts.slice(0, 4)} activeId={previewArtifact?.id} onSelect={(artifact) => setPreviewArtifactId(artifact.id)} compact />
                    </div>
                </aside>
            </div>

            <AppModal
                flush
                open={Boolean(imagePreviewArtifact?.resourceId)}
                title={imagePreviewArtifact?.type === "action_board" ? "动作预演预览" : "分镜图预览"}
                footer={null}
                centered
                width="min(960px, calc(100vw - 32px))"
                onCancel={() => setImagePreviewArtifact(null)}
            >
                {imagePreviewArtifact?.resourceId ? <img className={`workflow-image-preview-modal ${imagePreviewArtifact.type === "action_board" ? "grayscale" : ""}`} src={resourceFileUrl(imagePreviewArtifact.resourceId)} alt={imagePreviewArtifact.type === "action_board" ? "动作预演大图" : "分镜图大图"} /> : null}
            </AppModal>

            <ChapterAssetBindingDrawer
                projectId={projectId}
                unitId={unitId}
                shotIds={shots.map((item) => item.id)}
                shotTitles={Object.fromEntries(shots.map((item, idx) => [item.id, customShotTitle(item.title, idx) || formatShotOrdinal(idx)]))}
                open={bindingDrawerOpen}
                onClose={() => setBindingDrawerOpen(false)}
                onApplied={(result) => void handleAssetBindingsApplied(result)}
            />

            <WorkflowImageBatchDialog controller={batch} config={effectiveConfig} skills={availableSkills} />

            <ShotTimeline activeStage={activeStage} detail={detail} shots={shots} selectedShotId={selectedShot.id} submittingShotIds={submittingShotIds} batchSlotStates={batchSlotStates} onSelectShot={requestShotSelection} onAddShot={requestAddShot} addingShot={addingShot || chapterMutationLocked} />
        </div>
    );
}

function formatTaskElapsed(startedAt: number, now: number) {
    const totalSeconds = Math.max(0, Math.floor((now - (Number.isFinite(startedAt) ? startedAt : now)) / 1_000));
    return `${Math.floor(totalSeconds / 60)}分钟${String(totalSeconds % 60).padStart(2, "0")}秒`;
}

function WorkflowDisclosure({ icon, title, description, summary, className = "", children }: { icon: ReactNode; title: string; description: string; summary: ReactNode; className?: string; children: ReactNode }) {
    return (
        <details className={`workflow-disclosure ${className}`}>
            <summary>
                <span className="workflow-disclosure-heading"><span className="workflow-disclosure-icon">{icon}</span><span><strong>{title}</strong><small>{description}</small></span></span>
                <span className="workflow-disclosure-summary">{summary}<ChevronDown className="workflow-disclosure-chevron" /></span>
            </summary>
            <div className="workflow-disclosure-body"><div className="workflow-disclosure-content">{children}</div></div>
        </details>
    );
}

function AssetLibrary({ detail, referenceByVersionId, changing, onToggle }: { detail: ProjectDetail; referenceByVersionId: Map<string, ShotAssetReference>; changing: boolean; onToggle: (asset: ProjectAsset, reference?: ShotAssetReference) => void }) {
    const [category, setCategory] = useState("all");
    const [keyword, setKeyword] = useState("");
    const debouncedKeyword = useDebouncedValue(keyword.trim(), 250);
    const [page, setPage] = useState(1);
    const pageSize = 30;
    const assetsQuery = useQuery({
        queryKey: ["project", detail.project.id, "assets", "workflow-library", category, debouncedKeyword, page, pageSize],
        queryFn: () => listProjectAssetsPage(detail.project.id, { page, pageSize, category: category === "all" ? undefined : category, query: debouncedKeyword || undefined }),
    });
    useEffect(() => setPage(1), [debouncedKeyword]);
    const assetsPage = assetsQuery.data?.assets || [];
    const groups = useMemo(() => {
        const map = new Map<string, ProjectAsset[]>();
        assetsPage.forEach((asset) => map.set(asset.category || "other", [...(map.get(asset.category || "other") || []), asset]));
        return Array.from(map.entries());
    }, [assetsPage]);
    const total = assetsQuery.data?.total || 0;
    const pages = Math.max(1, Math.ceil(total / pageSize));
    return <div className="workflow-asset-groups"><Input allowClear size="small" className="mb-2 w-full" value={keyword} onChange={(event) => setKeyword(event.target.value)} prefix={<Search className="size-3.5 text-foreground/35" />} placeholder="搜索资产名称" aria-label="搜索镜头资产" /><Select size="small" className="mb-2 w-full" value={category} options={[{ value: "all", label: `全部资产（${Object.values(assetsQuery.data?.categoryCounts || {}).reduce((sum, count) => sum + count, 0)}）` }, ...Object.entries(assetsQuery.data?.categoryCounts || {}).filter(([, count]) => count > 0).map(([value, count]) => ({ value, label: `${assetCategoryLabel(value)}（${count}）` }))]} onChange={(value) => { setCategory(value); setPage(1); }} />{assetsQuery.isLoading ? <div className="py-6 text-center text-xs text-foreground/45">正在读取资产…</div> : groups.length ? groups.map(([groupCategory, assets]) => <section key={groupCategory}><h3>{assetCategoryLabel(groupCategory)} <span>({assets.length})</span></h3><div className="workflow-asset-list">{assets.map((asset) => { const reference = asset.primaryVersionId ? referenceByVersionId.get(asset.primaryVersionId) : undefined; const active = Boolean(reference); const previewUrl = assetPreviewUrl(asset); return <button key={asset.id} type="button" className={`workflow-asset-row ${active ? "is-active" : ""}`} disabled={changing || !asset.primaryVersionId} aria-pressed={active} onClick={() => onToggle(asset, reference)}><span className="workflow-asset-thumb">{previewUrl ? <img src={previewUrl} alt="" loading="lazy" /> : asset.category === "character" ? <UsersRound /> : asset.mediaType === "image" ? <ImageIcon /> : <Box />}</span><span className="min-w-0 flex-1"><strong>{asset.title}</strong><small>{active ? "已绑定 · 点击取消" : `${assetCategoryLabel(asset.category)} · v${Math.max(1, asset.versionCount)}`}</small></span>{active ? <span className="workflow-bound-dot" /> : null}</button>; })}</div></section>) : <EmptyState size="compact" title={debouncedKeyword ? "没有找到匹配资产" : "项目还没有资产"} />}{total > pageSize ? <div className="mt-3 flex items-center justify-between border-t border-border/60 pt-2 text-[var(--fs-micro)] text-foreground/45"><span>{page}/{pages} · 共 {total} 项</span><span className="flex gap-1"><Button type="text" size="small" icon={<ChevronLeft className="size-3.5" />} disabled={page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))} /><Button type="text" size="small" icon={<ChevronRight className="size-3.5" />} disabled={page >= pages} onClick={() => setPage((value) => Math.min(pages, value + 1))} /></span></div> : null}</div>;
}

function ShotAssetMentionTextarea({ value = "", onChange = () => undefined, references, variant = "motion" }: { value?: string; onChange?: (value: string) => void; references: ReturnType<typeof buildShotAssetReferenceContext>["mentionReferences"]; variant?: "scene" | "motion" }) {
    const isScene = variant === "scene";
    return (
        <CanvasResourceMentionTextarea
            value={value}
            references={references}
            onChange={onChange}
            sendOnEnter={false}
            containerClassName={`workflow-shot-mention-container ${isScene ? "is-scene" : ""}`}
            className={`thin-scrollbar workflow-shot-mention-editor ${isScene ? "is-scene" : ""}`}
            placeholder={isScene
                ? references.length ? "描述主体、场景、动作、构图与光线；输入 @ 可引用已绑定资产" : "描述主体、场景、动作、构图与光线；先绑定资产后可用 @ 引用"
                : references.length ? "补充动作节奏、运镜变化和动态细节；输入 @ 可引用已绑定资产" : "补充动作节奏、运镜变化和动态细节；绑定资产后可用 @ 引用"}
            aria-label={isScene ? "镜头画面，可使用 @ 引用已绑定资产" : "视频提示词，可使用 @ 引用已绑定资产"}
        />
    );
}

function EpisodeLibrary({ detail, activeUnitId, projectId, activeStage }: { detail: ProjectDetail; activeUnitId: string; projectId: string; activeStage: ShortDramaWorkflowStage }) {
    return <div className="workflow-simple-list">{detail.units.slice().sort((left, right) => left.position - right.position).map((unit, index) => <Link key={unit.id} to={`/projects/${projectId}/workflow/${unit.id}/${activeStage}`} className={unit.id === activeUnitId ? "is-active" : ""}><span>{String(index + 1).padStart(2, "0")}</span><strong>{unit.title}</strong></Link>)}</div>;
}

function ShotLibrary({ detail, shots, selectedShotId, onSelectShot }: { detail: ProjectDetail; shots: ProjectShot[]; selectedShotId: string; onSelectShot: (id: string) => void }) {
    return <div className="workflow-simple-list">{shots.map((shot, index) => { const video = currentArtifact(detail, shot.id, "video"); return <button key={shot.id} type="button" className={shot.id === selectedShotId ? "is-active" : ""} onClick={() => onSelectShot(shot.id)}><span>{formatShotOrdinal(index)}</span><span className="min-w-0 flex-1"><strong>{customShotTitle(shot.title, index) || "未命名"}</strong><small>{formatDuration(shot.durationMs)}</small></span><ArtifactStatus artifact={video} compact /></button>; })}</div>;
}

function BoundAssets({ detail, shotId, changing, onUnlink }: { detail: ProjectDetail; shotId: string; changing: boolean; onUnlink: (reference: ShotAssetReference) => void }) {
    const references = (detail.shotReferences || []).filter((item) => item.shotId === shotId);
    const assetByVersionId = useMemo(() => new Map(detail.assets.filter((asset) => asset.primaryVersionId).map((asset) => [asset.primaryVersionId as string, asset])), [detail.assets]);
    return (
        <div className="workflow-bound-assets">
            <div className="workflow-bound-assets-heading"><span className="workflow-field-label">镜头资产</span><small>{references.length ? `已绑定 ${references.length} 项` : "从左侧资产栏点击绑定"}</small></div>
            <Image.PreviewGroup>
                <div className="workflow-bound-assets-content">
                    {references.length ? references.map((reference) => {
                        const asset = reference.asset || assetByVersionId.get(reference.assetVersionId);
                        const title = asset?.title || "历史资产版本";
                        const previewUrl = asset ? assetPreviewUrl(asset) : "";
                        return <div key={reference.id} className="workflow-bound-asset-chip">
                            <span className="workflow-bound-asset-preview">{previewUrl ? <Image src={previewUrl} alt={`${title}预览`} width={40} height={40} loading="lazy" preview={{ mask: "预览" }} /> : <Box aria-hidden />}</span>
                            <span className="workflow-bound-asset-copy"><em>{asset ? assetCategoryLabel(asset.category) : "历史"}</em><strong title={title}>{title}</strong></span>
                            <button type="button" disabled={changing} aria-label={`取消引用 ${title}`} onClick={() => onUnlink(reference)}><X aria-hidden /></button>
                        </div>;
                    }) : <span>尚未绑定角色、场景或道具</span>}
                </div>
            </Image.PreviewGroup>
        </div>
    );
}

function LatestPreview({ artifact, emptyText, onPreviewImage }: { artifact?: ShotArtifact; emptyText: string; onPreviewImage: (artifact: ShotArtifact) => void }) {
    if (!artifact?.resourceId) return <div className="workflow-media-empty"><span><Play className="size-7" /></span><p>{emptyText}</p></div>;
    const src = resourceFileUrl(artifact.resourceId);
    if (artifact.type === "video") return <VideoArtifactPreview src={src} title="镜头视频" />;
    return <button type="button" className="workflow-preview-media-button" onClick={() => onPreviewImage(artifact)} aria-label={artifact.type === "action_board" ? "点击预览动作预演" : "点击预览分镜图"}>
        <img className={`workflow-preview-media ${artifact.type === "action_board" ? "grayscale" : ""}`} src={src} alt="镜头生成预览" loading="eager" />
        <span className="workflow-preview-expand" aria-hidden="true"><Maximize2 className="size-4" /></span>
    </button>;
}

function VideoArtifactPreview({ src, title }: { src: string; title: string }) {
    const [playing, setPlaying] = useState(false);

    useEffect(() => {
        setPlaying(false);
    }, [src]);

    if (playing) return <video className="workflow-preview-media" src={src} controls autoPlay playsInline preload="metadata" aria-label={title} />;
    return <button type="button" className="workflow-preview-media-button workflow-video-poster" onClick={() => setPlaying(true)} aria-label={`点击播放${title}`}>
        <span className="workflow-preview-media workflow-video-placeholder" aria-hidden="true"><Film /></span>
        <span className="workflow-video-play" aria-hidden="true"><Play className="size-6" fill="currentColor" /></span>
    </button>;
}

function ArtifactHistory({ artifacts, activeId, onSelect, compact = false }: { artifacts: ShotArtifact[]; activeId?: string; onSelect: (artifact: ShotArtifact) => void; compact?: boolean }) {
    if (!artifacts.length) return compact ? null : <EmptyState size="compact" title="暂无历史版本" />;
    return <section className={`workflow-history ${compact ? "is-compact" : ""}`}><div className="workflow-history-title">历史版本</div>{artifacts.map((artifact) => <button key={artifact.id} type="button" className={artifact.id === activeId ? "is-active" : ""} onClick={() => onSelect(artifact)}>{artifact.resourceId ? artifact.type === "video" ? <video src={resourceFileUrl(artifact.resourceId)} muted preload="metadata" /> : <img src={resourceFileUrl(artifact.resourceId)} alt="" loading="lazy" /> : <span className="workflow-history-placeholder"><Layers3 /></span>}<span className="min-w-0 flex-1"><strong>v{artifact.version}{artifact.selected ? " · 当前" : ""}</strong><small>{new Date(artifact.createdAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</small></span><ArtifactStatus artifact={artifact} compact /></button>)}</section>;
}

function ShotTimeline({ activeStage, detail, shots, selectedShotId, submittingShotIds, batchSlotStates, onSelectShot, onAddShot, addingShot }: { activeStage: ShortDramaWorkflowStage; detail: ProjectDetail; shots: ProjectShot[]; selectedShotId: string; submittingShotIds: Set<string>; batchSlotStates: ReadonlyMap<string, BatchItemState>; onSelectShot: (id: string) => void; onAddShot: () => void; addingShot: boolean }) {
    const artifactType = artifactTypeForStage(activeStage);
    const [timelineH, setTimelineH] = useState<number | null>(null);
    const timelineRef = useRef<HTMLElement | null>(null);
    const splitterRef = useRef<{ startY: number; startH: number } | null>(null);
    const clampTimelineHeight = (next: number) => {
        const maxH = typeof window === "undefined" ? 480 : Math.max(260, Math.min(520, Math.round(window.innerHeight * 0.62)));
        return Math.min(maxH, Math.max(120, Math.round(next)));
    };
    const latestTaskByShotAndType = useMemo(() => {
        const tasks = new Map<string, GenerationTask>();
        for (const task of detail.tasks || []) {
            const shotId = task.clientContext?.shotId;
            const type = task.clientContext?.artifactType;
            if (!shotId || !type) continue;
            const key = `${shotId}:${type}`;
            const current = tasks.get(key);
            if (!current || task.updatedAt > current.updatedAt) tasks.set(key, task);
        }
        return tasks;
    }, [detail.tasks]);
    return (
        <section ref={timelineRef} className="workflow-shot-timeline" style={timelineH ? ({ "--workflow-timeline-height": `${timelineH}px` } as CSSProperties) : undefined}>
            <div
                className="workflow-timeline-resizer"
                role="separator"
                 tabIndex={0}
                aria-orientation="horizontal"
                aria-label="上下拖动调整分镜时间轴高度（双击恢复默认）"
                onDoubleClick={() => setTimelineH(null)}
                onKeyDown={(event) => {
                    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
                    event.preventDefault();
                    const currentH = timelineH ?? timelineRef.current?.getBoundingClientRect().height ?? 224;
                    setTimelineH(clampTimelineHeight(currentH + (event.key === "ArrowUp" ? 24 : -24)));
                }}
                onPointerDown={(event) => {
                    const currentH = timelineH ?? timelineRef.current?.getBoundingClientRect().height ?? 224;
                    splitterRef.current = { startY: event.clientY, startH: currentH };
                    event.currentTarget.setPointerCapture(event.pointerId);
                }}
                onPointerMove={(event) => {
                    const drag = splitterRef.current;
                    if (!drag) return;
                    setTimelineH(clampTimelineHeight(drag.startH - (event.clientY - drag.startY)));
                }}
                onPointerUp={() => { splitterRef.current = null; }}
                onPointerCancel={() => { splitterRef.current = null; }}
            >
                <span />
            </div>
            <header><div><strong>{detail.units.find((item) => item.id === shots[0]?.unitId)?.title || "本集"}</strong><span>{shots.length} 镜 · 总时长 {formatDuration(shots.reduce((total, item) => total + item.durationMs, 0))}</span></div><div className="flex items-center gap-1 text-[var(--fs-micro)] text-foreground/40"><List className="size-3.5" /> 共 {shots.length} 镜</div></header>
            <div className="workflow-shot-track thin-scrollbar">{shots.map((shot, index) => <TimelineShot key={shot.id} artifactType={artifactType} detail={detail} shot={shot} latestTaskByShotAndType={latestTaskByShotAndType} submitting={submittingShotIds.has(shot.id)} batchSlotStates={batchSlotStates} index={index} selected={shot.id === selectedShotId} onSelect={() => onSelectShot(shot.id)} />)}<button type="button" className="workflow-add-shot-card" disabled={addingShot} onClick={onAddShot}><Plus className="size-5" /><span>新增分镜</span></button></div>
        </section>
    );
}

function TimelineShot({ artifactType, detail, shot, latestTaskByShotAndType, submitting, batchSlotStates, index, selected, onSelect }: { artifactType: string; detail: ProjectDetail; shot: ProjectShot; latestTaskByShotAndType: Map<string, GenerationTask>; submitting: boolean; batchSlotStates: ReadonlyMap<string, BatchItemState>; index: number; selected: boolean; onSelect: () => void }) {
    const storyboard = currentArtifact(detail, shot.id, "storyboard");
    const previz = currentArtifact(detail, shot.id, "action_board");
    const video = currentArtifact(detail, shot.id, "video");
    const title = customShotTitle(shot.title, index);
    const slots = artifactType === "video"
        ? [
            { type: "action_board", label: "动作预演", artifact: previz?.resourceId ? previz : storyboard },
            { type: "video", label: "镜头视频", artifact: video },
        ]
        : [
            { type: "storyboard", label: "分镜图", artifact: storyboard },
            { type: "action_board", label: "动作预演", artifact: previz },
        ];
    return (
        <button type="button" className={`workflow-timeline-shot ${selected ? "is-active" : ""}`} onClick={onSelect}>
            <span className="workflow-timeline-heading">
                <span className="min-w-0 flex items-center gap-1.5 overflow-hidden">
                    <strong>{formatShotOrdinal(index)}</strong>
                    {title ? <em className="workflow-timeline-title">{title}</em> : null}
                </span>
                <b>{formatDuration(shot.durationMs)}</b>
            </span>
            <span className="workflow-timeline-rows">
                {slots.map((slot) => {
                    const task = latestTaskByShotAndType.get(`${shot.id}:${slot.type}`);
                    const slotSubmitting = submitting && artifactType === slot.type;
                    const batchState = batchSlotStates.get(`${shot.id}:${slot.type}`);
                    return (
                        <span key={slot.type} className={`workflow-timeline-media ${artifactType === slot.type ? "is-current-stage" : ""}`}>
                            {slot.artifact?.resourceId && slot.artifact.type !== "video" ? (
                                <img className={slot.artifact.type === "action_board" ? "grayscale" : ""} src={resourceFileUrl(slot.artifact.resourceId)} alt="" loading="lazy" />
                            ) : (
                                <Film />
                            )}
                            <span className="workflow-timeline-status">
                                <span>{slot.label}{slot.artifact ? ` · v${slot.artifact.version}` : ""}</span>
                                {batchState === "pending" ? (
                                    <StatusBadge variant="filled" tone="neutral" label="待提交" />
                                ) : batchState === "submitting" ? (
                                    <StatusBadge variant="filled" tone="loading" label="提交中" />
                                ) : batchState === "submitted" ? (
                                    <StatusBadge variant="filled" tone="loading" label="生成中" />
                                ) : batchState === "syncing" ? (
                                    <StatusBadge variant="filled" tone="loading" label="待同步" />
                                ) : batchState === "unknown" ? (
                                    <StatusBadge variant="filled" tone="warning" label="待核对" />
                                ) : (
                                    <ArtifactStatus artifact={slot.artifact} taskStatus={slotSubmitting ? "queued" : task?.status} compact />
                                )}
                            </span>
                        </span>
                    );
                })}
            </span>
        </button>
    );
}

function revisionInput(values: ShotEditorValues): ShotRevisionInput {
    return {
        plotDescription: values.plotDescription,
        action: values.action,
        dialogue: values.dialogue,
        shotSize: values.shotSize,
        cameraAngle: values.cameraAngle,
        cameraMovement: values.cameraMovement,
        durationMs: Math.round(values.durationSeconds * 1000),
        imagePrompt: values.imagePrompt,
        videoPrompt: values.videoPrompt,
        negativePrompt: values.negativePrompt,
        continuityNotes: values.continuityNotes,
    };
}

async function downloadArtifact(artifact: ShotArtifact, shotTitle: string, onError: (content: string) => void) {
    if (!artifact.resourceId) return;
    try {
        await downloadBrowserMedia({
            storageKey: resourceStorageKey(artifact.resourceId),
            fileName: `${shotTitle || "shot"}-v${artifact.version}.${artifact.type === "video" ? "mp4" : "png"}`,
        });
    } catch (error) {
        onError(error instanceof Error ? `下载失败：${error.message}` : "下载失败");
    }
}

function assetPreviewUrl(asset: ProjectAsset) {
    const representation = asset.character?.representations?.find((item) => item.role === "primary") || asset.character?.representations?.[0];
    if (representation?.resourceId) return resourceFileUrl(representation.resourceId);
    const resourceId = resourceIdFromStorageKey(asset.storageKey);
    return resourceId && asset.mediaType === "image" ? resourceFileUrl(resourceId) : "";
}
