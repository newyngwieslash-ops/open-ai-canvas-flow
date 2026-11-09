import { useEffect, useMemo, useRef, useState } from "react";
import { App, Button, Checkbox, Input, Radio, Select } from "antd";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AppModal } from "@/components/ui/product/app-modal";
import { ModelPicker } from "@/components/model-picker";
import {
    adoptProjectAssetImageCandidate,
    cancelProjectAssetBatchTasks,
    createProjectAssetBatch,
    getProjectAssetBatch,
    listProjectAssetBatches,
    preflightProjectAssetBatch,
    retryProjectAssetBatchItem,
    stopProjectAssetBatch,
    type Project,
    type ProjectAsset,
    type ProjectAssetBatchItemView,
    type ProjectAssetBatchPreflightResult,
    type ProjectAssetBatchReferenceSource,
    type ProjectAssetBatchView,
} from "@/services/api/projects";
import { ApiError } from "@/services/api/request";
import {
    configuredModelMatchesCapability,
    useConfigStore,
    useEffectiveConfig,
} from "@/stores/use-config-store";
import { modelCompatibilityError, resolveCompatibleModel, type ModelRequirements } from "@/lib/model-selection";
import { referenceModelError } from "./project-reference-image";
import {
    MAX_PROJECT_ASSET_BATCH_ITEMS,
    buildDefaultBatchDraftItems,
    buildProjectAssetBatchRequest,
    formatBatchPreflightCostSummary,
    summarizeBatchExecution,
    validateBatchDraftItems,
    type BatchDraftItem,
} from "./project-asset-batch";
import { categoryLabel } from "./shared";

export type ProjectAssetBatchExecutionListProps = {
    batch: ProjectAssetBatchView;
    adoptingTaskId: string;
    retryingItemId: string;
    stopping: boolean;
    cancelling: boolean;
    onStopUnsubmitted: () => void;
    onCancelActiveTasks: () => void;
    onRetryItem: (item: ProjectAssetBatchItemView) => void;
    onAdoptCandidate: (item: ProjectAssetBatchItemView) => void;
};

function itemStatusLabel(status: ProjectAssetBatchItemView["status"]): string {
    switch (status) {
        case "pending":
            return "待派发";
        case "running":
            return "生成中";
        case "cancel_requested":
            return "取消确认中";
        case "succeeded":
            return "已生成候选";
        case "failed":
            return "已失败";
        case "stopped":
            return "已停止派发";
        case "cancelled":
            return "已取消";
        case "reconcile_required":
            return "需核对账单/恢复";
        default:
            return status;
    }
}

export function ProjectAssetBatchExecutionList({
    batch,
    adoptingTaskId,
    retryingItemId,
    stopping,
    cancelling,
    onStopUnsubmitted,
    onCancelActiveTasks,
    onRetryItem,
    onAdoptCandidate,
}: ProjectAssetBatchExecutionListProps) {
    const [previewItemId, setPreviewItemId] = useState<string | null>(null);
    const previewItem = batch.items.find((item) => item.id === previewItemId);
    const stats = summarizeBatchExecution(batch);
    const costSummary = formatBatchPreflightCostSummary({
        totalKnownMicrocredits: batch.totalKnownMicrocredits,
        priceUnknownCount: batch.priceUnknownCount,
        totalCount: batch.totalCount,
    });

    return (
        <div className="grid gap-4">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-surface-active p-3">
                <div className="grid gap-1 text-sm">
                    <div className="font-medium">
                        批次状态：{batch.status} · 共 {batch.totalCount} 项（成功 {stats.succeededCount} / 运行中{" "}
                        {stats.runningCount + stats.cancelRequestedCount} / 待派发 {stats.pendingCount} / 失败{" "}
                        {stats.failedCount} / 待核对 {stats.reconcileRequiredCount}）
                    </div>
                    <div className="text-xs text-foreground/60">
                        已知预估消耗：{costSummary.knownCreditsLabel} 积分
                        {costSummary.hasUnknownPrice ? ` · ${costSummary.warningText}` : ""}
                    </div>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                    {stats.canStopUnsubmitted ? (
                        <Button size="small" loading={stopping} onClick={onStopUnsubmitted}>
                            停止未提交项（{stats.pendingCount}）
                        </Button>
                    ) : null}
                    {stats.canCancelActiveTasks ? (
                        <Button size="small" danger loading={cancelling} onClick={onCancelActiveTasks}>
                            请求取消活动任务（{stats.runningCount + stats.cancelRequestedCount}）
                        </Button>
                    ) : null}
                </div>
            </div>

            <div className="grid gap-3">
                {batch.items.map((item) => {
                    const isAdopted = item.candidate?.state === "adopted";
                    const canAdopt =
                        item.status === "succeeded" &&
                        Boolean(item.taskId) &&
                        item.candidate?.state === "pending_confirmation";
                    const canRetry = item.status === "failed" && item.canRetry;

                    return (
                        <div
                            key={item.id}
                            className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border p-3"
                        >
                            <div className="grid min-w-0 flex-1 gap-1">
                                <div className="flex flex-wrap items-center gap-2">
                                    <span className="font-medium">{item.assetName}</span>
                                    <span className="rounded bg-surface-active px-2 py-0.5 text-xs text-foreground/70">
                                        {categoryLabel(item.assetCategory)}
                                    </span>
                                    <span className="rounded bg-surface-active px-2 py-0.5 text-xs font-medium">
                                        {itemStatusLabel(item.status)}
                                    </span>
                                    <span className="text-xs text-foreground/50">尝试 #{item.attempt}</span>
                                </div>
                                {item.errorMessage ? (
                                    <p className="text-xs text-danger">{item.errorMessage}</p>
                                ) : null}
                                {item.status === "reconcile_required" ? (
                                    <p className="text-xs text-warning">
                                        该项存在待恢复媒体检查点或未决账单状态，禁止直接重发收费请求，请先在任务中心核对。
                                    </p>
                                ) : null}
                            </div>

                            <div className="flex items-center gap-3">
                                {item.outputUrl ? (
                                    <button
                                        type="button"
                                        aria-label={`放大查看 ${item.assetName} 候选图片`}
                                        aria-haspopup="dialog"
                                        title="点击放大预览"
                                        className="shrink-0 cursor-zoom-in overflow-hidden rounded border border-border focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
                                        onClick={() => setPreviewItemId(item.id)}
                                    >
                                        <img
                                            src={item.outputUrl}
                                            alt={`${item.assetName} 候选预览`}
                                            className="block h-14 w-14 object-cover"
                                        />
                                    </button>
                                ) : null}
                                {isAdopted ? (
                                    <span className="text-xs font-medium text-success">已采用为当前成品</span>
                                ) : canAdopt ? (
                                    <Button
                                        size="small"
                                        type="primary"
                                        loading={adoptingTaskId === item.taskId}
                                        onClick={() => onAdoptCandidate(item)}
                                    >
                                        采用此候选
                                    </Button>
                                ) : null}
                                {canRetry ? (
                                    <Button
                                        size="small"
                                        loading={retryingItemId === item.id}
                                        onClick={() => onRetryItem(item)}
                                    >
                                        重试此项
                                    </Button>
                                ) : null}
                            </div>
                        </div>
                    );
                })}
            </div>
            <AppModal
                open={Boolean(previewItem?.outputUrl)}
                title={previewItem ? `${previewItem.assetName} · 候选预览` : "候选预览"}
                footer={null}
                centered
                width="min(1160px, calc(100vw - 32px))"
                onCancel={() => setPreviewItemId(null)}
            >
                {previewItem?.outputUrl ? (
                    <button
                        type="button"
                        aria-label={`收起 ${previewItem.assetName} 候选图片`}
                        title="点击图片收起"
                        className="flex w-full cursor-zoom-out justify-center rounded-[var(--r-md)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
                        onClick={() => setPreviewItemId(null)}
                    >
                        <img
                            src={previewItem.outputUrl}
                            alt={`${previewItem.assetName} 候选大图`}
                            draggable={false}
                            className="block max-h-[75vh] max-w-full object-contain"
                        />
                    </button>
                ) : null}
                <p className="mt-3 text-center text-xs text-foreground/60">点击图片、空白处或按 Esc 收起预览</p>
            </AppModal>
        </div>
    );
}

type ModalProps = {
    project: Project;
    assets: ProjectAsset[];
    onClose: () => void;
    onSaved: () => void;
};

function createIdempotencyKey() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return `batch-${crypto.randomUUID()}`;
    }
    return `batch-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export function ProjectAssetBatchModal({ project, assets, onClose, onSaved }: ModalProps) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const config = useEffectiveConfig();

    const allDraftCandidates = useMemo(() => buildDefaultBatchDraftItems(assets), [assets]);
    const [selectedIds, setSelectedIds] = useState<string[]>(() =>
        allDraftCandidates.map((item) => item.assetId),
    );
    const [draftMap, setDraftMap] = useState<Record<string, BatchDraftItem>>(() =>
        Object.fromEntries(allDraftCandidates.map((item) => [item.assetId, item])),
    );

    useEffect(() => {
        setDraftMap((prev) => {
            const next = { ...prev };
            for (const candidate of allDraftCandidates) {
                const existing = next[candidate.assetId];
                next[candidate.assetId] = existing
                    ? {
                          ...existing,
                          assetName: candidate.assetName,
                          expectedVersionId: candidate.expectedVersionId,
                          referenceCount: candidate.referenceCount,
                          hasCurrentImage: candidate.hasCurrentImage,
                      }
                    : candidate;
            }
            return next;
        });
    }, [allDraftCandidates]);

    const selectedItems = useMemo(
        () =>
            selectedIds
                .map((id) => draftMap[id])
                .filter((item): item is BatchDraftItem => Boolean(item)),
        [draftMap, selectedIds],
    );

    const [model, setModel] = useState(() =>
        project.defaultImageModel &&
        configuredModelMatchesCapability(config, project.defaultImageModel, "image")
            ? project.defaultImageModel
            : config.imageModel || "",
    );

    const maxReferenceCount = useMemo(
        () =>
            selectedItems.reduce((max, item) => {
                const count =
                    item.referenceSource === "none"
                        ? 0
                        : item.referenceSource === "current"
                          ? 1
                          : Math.max(1, item.referenceCount);
                return Math.max(max, count);
            }, 0),
        [selectedItems],
    );

    const requirements: ModelRequirements = {
        capability: "image",
        input: {
            textCount: 1,
            imageCount: maxReferenceCount,
            videoCount: 0,
            audioCount: 0,
            characterCount: 0,
        },
    };
    const routedModel = resolveCompatibleModel(config, model, requirements) || model;
    const modelError = !model
        ? "请选择图片模型"
        : referenceModelError(config, routedModel, maxReferenceCount) ||
          modelCompatibilityError(config, routedModel, requirements);

    const [preflight, setPreflight] = useState<ProjectAssetBatchPreflightResult | null>(null);
    const [idempotencyKey, setIdempotencyKey] = useState(() => createIdempotencyKey());
    const [activeBatchId, setActiveBatchId] = useState<string>("");
    const [busy, setBusy] = useState(false);
    const [stopping, setStopping] = useState(false);
    const [cancelling, setCancelling] = useState(false);
    const [retryingItemId, setRetryingItemId] = useState("");
    const [adoptingTaskId, setAdoptingTaskId] = useState("");
    const [error, setError] = useState("");

    const batchesQuery = useQuery({
        queryKey: ["project-asset-batches", project.id],
        queryFn: ({ signal }) => listProjectAssetBatches(project.id, { limit: 10, signal }),
    });

    useEffect(() => {
        if (!activeBatchId && batchesQuery.data?.batches?.length) {
            const running = batchesQuery.data.batches.find(
                (b) => b.status === "running" || b.status === "pending",
            );
            if (running) setActiveBatchId(running.id);
        }
    }, [activeBatchId, batchesQuery.data]);

    const activeBatchQuery = useQuery({
        queryKey: ["project-asset-batch", project.id, activeBatchId],
        queryFn: ({ signal }) => getProjectAssetBatch(project.id, activeBatchId, signal),
        enabled: Boolean(activeBatchId),
        refetchInterval: (query) => {
            const b = query.state.data?.batch;
            if (!b) return false;
            return summarizeBatchExecution(b).hasPollingItems ? 2_000 : false;
        },
    });

    const activeBatch = activeBatchQuery.data?.batch;
    const prevSignatureRef = useRef("");
    const requestSignature = useMemo(
        () =>
            JSON.stringify({
                routedModel,
                items: selectedItems.map((item) => ({
                    assetId: item.assetId,
                    expectedVersionId: item.expectedVersionId,
                    referenceSource: item.referenceSource,
                    prompt: item.prompt,
                    operation: item.operation,
                })),
            }),
        [routedModel, selectedItems],
    );

    useEffect(() => {
        if (prevSignatureRef.current && prevSignatureRef.current !== requestSignature) {
            setPreflight(null);
            setIdempotencyKey(createIdempotencyKey());
        }
        prevSignatureRef.current = requestSignature;
    }, [requestSignature]);

    const buildRequestPayload = () => {
        const validationError = validateBatchDraftItems(selectedItems);
        if (validationError) throw new Error(validationError);
        if (modelError) throw new Error(modelError);
        const generationConfig = { ...config, model: routedModel, imageModel: routedModel, count: "1" };
        if (!useConfigStore.getState().isAiConfigReady(generationConfig, routedModel)) {
            throw new Error("请先配置可用的图片模型");
        }
        return buildProjectAssetBatchRequest(config, routedModel, selectedItems);
    };

    const handlePreflight = async () => {
        setBusy(true);
        setError("");
        try {
            const payload = buildRequestPayload();
            const res = await preflightProjectAssetBatch(project.id, payload);
            setPreflight(res);
        } catch (err) {
            setError(err instanceof Error ? err.message : "预检失败，请重试");
        } finally {
            setBusy(false);
        }
    };

    const handleSubmitBatch = async () => {
        if (!preflight) return;
        setBusy(true);
        setError("");
        try {
            const payload = buildRequestPayload();
            const res = await createProjectAssetBatch(project.id, {
                ...payload,
                idempotencyKey,
                preflightDigest: preflight.preflightDigest,
            });
            setActiveBatchId(res.batch.id);
            void queryClient.invalidateQueries({ queryKey: ["project-asset-batches", project.id] });
            void queryClient.invalidateQueries({ queryKey: ["project-asset-batch", project.id, res.batch.id] });
            message.success("批量生成批次已提交，生成结果将作为待确认候选保存");
        } catch (err) {
            if (err instanceof ApiError && (err.reason === "batch_preflight_stale" || err.reason === "project_asset_version_changed")) {
                setPreflight(null);
                setIdempotencyKey(createIdempotencyKey());
                setError("资产版本或费用预检已更新，请重新执行预检后再确认提交");
                onSaved();
            } else {
                setError(err instanceof Error ? err.message : "提交批次失败，请重试");
            }
        } finally {
            setBusy(false);
        }
    };

    const handleStopUnsubmitted = async () => {
        if (!activeBatch) return;
        setStopping(true);
        setError("");
        try {
            await stopProjectAssetBatch(project.id, activeBatch.id);
            await activeBatchQuery.refetch();
            void queryClient.invalidateQueries({ queryKey: ["project-asset-batches", project.id] });
            message.success("已停止尚未派发的生成项");
        } catch (err) {
            setError(err instanceof Error ? err.message : "停止未提交项失败");
        } finally {
            setStopping(false);
        }
    };

    const handleCancelActiveTasks = async () => {
        if (!activeBatch) return;
        setCancelling(true);
        setError("");
        try {
            await cancelProjectAssetBatchTasks(project.id, activeBatch.id);
            await activeBatchQuery.refetch();
            void queryClient.invalidateQueries({ queryKey: ["project-asset-batches", project.id] });
            message.success("已向运行中任务发起取消请求，已生成的候选不受影响");
        } catch (err) {
            setError(err instanceof Error ? err.message : "取消活动任务失败");
        } finally {
            setCancelling(false);
        }
    };

    const handleRetryItem = async (item: ProjectAssetBatchItemView) => {
        if (!activeBatch) return;
        setRetryingItemId(item.id);
        setError("");
        try {
            await retryProjectAssetBatchItem(project.id, activeBatch.id, item.id);
            await activeBatchQuery.refetch();
            message.success(`已重新提交「${item.assetName}」`);
        } catch (err) {
            setError(err instanceof Error ? err.message : "重试失败");
        } finally {
            setRetryingItemId("");
        }
    };

    const handleAdoptCandidate = async (item: ProjectAssetBatchItemView) => {
        if (!item.taskId) return;
        setAdoptingTaskId(item.taskId);
        setError("");
        try {
            await adoptProjectAssetImageCandidate(project.id, item.assetId, item.taskId, {
                expectedVersionId: item.baseVersionId,
            });
            await activeBatchQuery.refetch();
            onSaved();
            message.success(`已采用「${item.assetName}」的候选图片为当前成品`);
        } catch (err) {
            if (err instanceof ApiError && err.reason === "project_asset_version_changed") {
                setError(`「${item.assetName}」版本已变化，未覆盖当前成品；候选已保留，请刷新核对最新资产版本。`);
                onSaved();
            } else {
                setError(err instanceof Error ? err.message : "采用候选失败");
            }
        } finally {
            setAdoptingTaskId("");
        }
    };

    const costSummary = preflight
        ? formatBatchPreflightCostSummary({
              totalKnownMicrocredits: preflight.totalKnownMicrocredits,
              priceUnknownCount: preflight.priceUnknownCount,
              totalCount: preflight.items.length,
          })
        : null;

    return (
        <AppModal open flush title={null} footer={null} width={960} onCancel={onClose}>
            <div
                className="max-h-[85vh] overflow-y-auto p-6 text-foreground"
                data-canvas-no-zoom
                data-canvas-wheel-scroll
            >
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h2 className="text-xl font-semibold">批量生成资产图片</h2>
                        <p className="mt-1 text-sm text-foreground/60">
                            跨分类（角色、场景、道具）批量发起单图生成，单批上限 {MAX_PROJECT_ASSET_BATCH_ITEMS} 项。生成结果仅保存为待确认候选，需逐项预览并手动采用。
                        </p>
                    </div>
                    {batchesQuery.data?.batches?.length ? (
                        <Select
                            aria-label="切换历史批次"
                            className="min-w-[240px]"
                            value={activeBatchId || undefined}
                            placeholder="查看近期批次执行状态"
                            allowClear
                            onChange={(val) => setActiveBatchId(val || "")}
                            options={batchesQuery.data.batches.map((listed) => {
                                const b = activeBatch?.id === listed.id ? activeBatch : listed;
                                return {
                                    value: b.id,
                                    label: `批次 ${b.id.slice(0, 8)} · ${b.status} (${b.totalCount}项)`,
                                };
                            })}
                        />
                    ) : null}
                </div>

                {error ? (
                    <div className="mt-4 rounded-lg border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
                        {error}
                    </div>
                ) : null}

                {activeBatch ? (
                    <div className="mt-5 grid gap-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-base font-medium">当前批次执行与候选采用</h3>
                            <Button size="small" onClick={() => setActiveBatchId("")}>
                                新建另一批生成
                            </Button>
                        </div>
                        <ProjectAssetBatchExecutionList
                            key={activeBatch.id}
                            batch={activeBatch}
                            adoptingTaskId={adoptingTaskId}
                            retryingItemId={retryingItemId}
                            stopping={stopping}
                            cancelling={cancelling}
                            onStopUnsubmitted={handleStopUnsubmitted}
                            onCancelActiveTasks={handleCancelActiveTasks}
                            onRetryItem={handleRetryItem}
                            onAdoptCandidate={handleAdoptCandidate}
                        />
                    </div>
                ) : (
                    <div className="mt-5 grid gap-4">
                        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-surface-active p-3">
                            <div className="flex items-center gap-3">
                                <span className="text-sm font-medium">图片模型</span>
                                <ModelPicker
                                    config={config}
                                    capability="image"
                                    requirements={requirements}
                                    value={model}
                                    onChange={(next) => setModel(next)}
                                    modelFilter={(value) =>
                                        !referenceModelError(config, value, maxReferenceCount)
                                    }
                                />
                            </div>
                            <div className="flex items-center gap-2 text-xs text-foreground/60">
                                <span>
                                    已选 {selectedItems.length} / {allDraftCandidates.length} 个可生成资产
                                </span>
                                <Button
                                    size="small"
                                    onClick={() =>
                                        setSelectedIds(
                                            selectedIds.length === allDraftCandidates.length
                                                ? []
                                                : allDraftCandidates.map((i) => i.assetId),
                                        )
                                    }
                                >
                                    {selectedIds.length === allDraftCandidates.length ? "取消全选" : "全选"}
                                </Button>
                            </div>
                        </div>

                        <div className="grid max-h-[42vh] gap-2 overflow-y-auto pr-1">
                            {allDraftCandidates.map((candidate) => {
                                const draft = draftMap[candidate.assetId] || candidate;
                                const checked = selectedIds.includes(candidate.assetId);
                                return (
                                    <div
                                        key={candidate.assetId}
                                        className="grid gap-2 rounded-lg border border-border p-3"
                                    >
                                        <div className="flex flex-wrap items-center justify-between gap-2">
                                            <Checkbox
                                                checked={checked}
                                                onChange={(e) => {
                                                    const nextChecked = e.target.checked;
                                                    setSelectedIds((prev) =>
                                                        nextChecked
                                                            ? [...prev, candidate.assetId]
                                                            : prev.filter((id) => id !== candidate.assetId),
                                                    );
                                                }}
                                            >
                                                <span className="font-medium">{draft.assetName}</span>
                                                <span className="ml-2 rounded bg-surface-active px-2 py-0.5 text-xs text-foreground/70">
                                                    {categoryLabel(draft.assetCategory)}
                                                </span>
                                            </Checkbox>

                                            <Radio.Group
                                                size="small"
                                                value={draft.referenceSource}
                                                disabled={!checked || busy}
                                                onChange={(e) => {
                                                    const nextSource = e.target
                                                        .value as ProjectAssetBatchReferenceSource;
                                                    setDraftMap((prev) => ({
                                                        ...prev,
                                                        [candidate.assetId]: {
                                                            ...draft,
                                                            referenceSource: nextSource,
                                                        },
                                                    }));
                                                }}
                                                options={[
                                                    {
                                                        value: "original",
                                                        label: `原始参考图 (${draft.referenceCount})`,
                                                        disabled: draft.referenceCount === 0,
                                                    },
                                                    {
                                                        value: "current",
                                                        label: "当前成品",
                                                        disabled: !draft.hasCurrentImage,
                                                    },
                                                    { value: "none", label: "无参考（文生图）" },
                                                ]}
                                            />
                                        </div>
                                        {checked ? (
                                            <Input
                                                aria-label={`${draft.assetName} 生成提示词`}
                                                value={draft.prompt}
                                                disabled={busy}
                                                onChange={(e) => {
                                                    const nextPrompt = e.target.value;
                                                    setDraftMap((prev) => ({
                                                        ...prev,
                                                        [candidate.assetId]: {
                                                            ...draft,
                                                            prompt: nextPrompt,
                                                        },
                                                    }));
                                                }}
                                                placeholder="输入该资产的生成提示词"
                                            />
                                        ) : null}
                                    </div>
                                );
                            })}
                        </div>

                        {preflight && costSummary ? (
                            <div className="rounded-lg border border-border bg-surface-active p-3 text-sm">
                                <div className="font-medium">
                                    预检通过：共 {preflight.items.length} 项 · 已知预估费用{" "}
                                    {costSummary.knownCreditsLabel} 积分
                                </div>
                                {costSummary.hasUnknownPrice ? (
                                    <div className="mt-1 text-xs text-warning">{costSummary.warningText}</div>
                                ) : null}
                            </div>
                        ) : null}

                        <div className="flex justify-end gap-2">
                            <Button onClick={onClose}>关闭</Button>
                            <Button
                                loading={busy && !preflight}
                                disabled={selectedItems.length === 0}
                                onClick={handlePreflight}
                            >
                                1. 预检能力与费用
                            </Button>
                            <Button
                                type="primary"
                                loading={busy && Boolean(preflight)}
                                disabled={!preflight}
                                onClick={handleSubmitBatch}
                            >
                                2. 确认提交批次
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </AppModal>
    );
}
