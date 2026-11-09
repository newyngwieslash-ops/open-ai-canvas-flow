import { Button, Input } from "antd";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { ImageSizePicker } from "../../../components/image-size-picker";
import { ModelPicker } from "../../../components/model-picker";
import { SkillRuntimePicker } from "../../../components/skills/skill-runtime-picker";
import { StatusBadge } from "../../../components/ui/base/badges";
import { SegmentedControl } from "../../../components/ui/base/segmented-control";
import { Select } from "../../../components/ui/base/select";
import { AppModal } from "../../../components/ui/product/app-modal/app-modal";
import { EmptyState } from "../../../components/ui/product/empty-state";
import { imageQualityLabel } from "../../../lib/image-quality";
import { imageResolutionUsesQuality } from "../../../lib/image-size-presets";
import {
    modelCapabilityConfigFor,
    normalizeImageValue,
} from "../../../lib/model-capabilities";
import type { Skill } from "../../../services/api/skills";
import type { AiConfig } from "../../../stores/use-config-store";
import type { WorkflowImageBatchController } from "./use-workflow-image-batch";
import {
    type BatchCandidate,
    type BatchCategory,
    type BatchItem,
    type BatchItemState,
    type BatchScope,
    batchPriceLabel,
} from "./workflow-batch-plan";

const SCOPE_OPTIONS: Array<{ value: BatchScope; label: string }> = [
    { value: "missing", label: "补齐缺失" },
    { value: "failed", label: "重试失败" },
    { value: "stale", label: "更新过期" },
];

function categoryStatusBadge(category: BatchCategory): {
    label: string;
    tone: "neutral" | "loading" | "success" | "warning" | "error";
} {
    switch (category) {
        case "missing":
            return { label: "待补齐", tone: "neutral" };
        case "ready":
            return { label: "已完成", tone: "success" };
        case "active":
            return { label: "已受理/生成中", tone: "loading" };
        case "syncing":
            return { label: "待产物同步", tone: "loading" };
        case "failed":
            return { label: "失败", tone: "error" };
        case "stale":
            return { label: "已过期", tone: "warning" };
        case "blocked":
            return { label: "阻止", tone: "error" };
    }
}

export function batchItemStateLabel(state: BatchItemState): {
    label: string;
    tone: "neutral" | "loading" | "success" | "warning" | "error";
} {
    switch (state) {
        case "pending":
            return { label: "待提交", tone: "neutral" };
        case "submitting":
            return { label: "提交中", tone: "loading" };
        case "submitted":
            return { label: "已受理/生成中", tone: "loading" };
        case "syncing":
            return { label: "待产物同步", tone: "loading" };
        case "succeeded":
            return { label: "成功", tone: "success" };
        case "failed":
            return { label: "失败", tone: "error" };
        case "blocked":
            return { label: "阻止", tone: "error" };
        case "unknown":
            return { label: "提交状态未知", tone: "warning" };
        case "stopped":
            return { label: "已停止", tone: "neutral" };
    }
}

export function issueCodeDescription(issueCode?: string): string | null {
    if (!issueCode) return null;
    switch (issueCode) {
        case "revision_changed":
        case "reference_changed":
            return "镜头脚本或参考资产已变化，需重新预览确认";
        case "model_unavailable":
            return "当前选择的图片模型不可用或不兼容";
        case "capacity_wait":
            return "活动任务容量已满，正在等待名额释放";
        case "rate_wait":
            return "触发请求频率限制，正在等待重试窗口";
        case "submit_unknown":
            return "请求响应中断，无法确认服务端是否已受理；为防重复扣费已暂停自动重发，请刷新核对或前往任务中心检查";
        case "media_recovery_required":
            return "图片已生成但保存未完成，请前往任务中心使用「重试保存」，无需重新生图";
        case "task_read_failed":
            return "查询任务状态连续失败，已保留任务 ID 并暂停补位";
        case "artifact_sync_timeout":
            return "任务已完成但工作区产物尚未同步完成，请点击「刷新状态」核对";
        case "storage_failed":
            return "本地批次日志未能可靠保存，已停止继续提交新任务";
        case "archived_project":
            return "项目已归档，停止提交";
        case "active_list_truncated":
            return "活动任务列表达到 100 条上限，无法完整确认在途状态";
        default:
            return `已阻止（${issueCode}）`;
    }
}

export function WorkflowImageBatchDialog({
    controller,
    config,
    skills,
}: {
    controller: WorkflowImageBatchController;
    config: AiConfig;
    skills: Skill[];
}) {
    const { stage, canConfirm, settings, scope, preview, log } = controller;
    const [manualTaskId, setManualTaskId] = useState("");

    const hasActiveRun = Boolean(
        log &&
            (log.state === "running" ||
                log.state === "waiting" ||
                log.items.some(
                    item =>
                        item.state === "submitting" ||
                        item.state === "submitted" ||
                        item.state === "syncing" ||
                        item.state === "unknown",
                )),
    );
    const settingsLocked = scope === "failed" || hasActiveRun || Boolean(controller.blockedReason);

    const imageProfile = useMemo(
        () => (settings.model ? modelCapabilityConfigFor(config, settings.model).image : undefined),
        [config, settings.model],
    );

    const handleModelChange = (nextModel: string) => {
        const profile = modelCapabilityConfigFor(config, nextModel).image;
        const normalized = profile
            ? normalizeImageValue(profile, {
                  size: settings.size,
                  quality: settings.quality,
                  count: "1",
              })
            : { size: settings.size, quality: settings.quality };
        void controller.refreshPreview(scope, {
            ...settings,
            model: nextModel,
            size: normalized.size,
            quality: normalized.quality,
        });
    };

    const handleSizeQualityChange = (nextSize: string, nextQuality?: string) => {
        void controller.refreshPreview(scope, {
            ...settings,
            size: nextSize,
            quality: nextQuality || settings.quality,
        });
    };

    const handleQualityOnlyChange = (nextQuality: string) => {
        void controller.refreshPreview(scope, {
            ...settings,
            quality: nextQuality,
        });
    };

    const handleSkillsChange = (nextSkillIds: string[]) => {
        void controller.refreshPreview(scope, {
            ...settings,
            skillIds: nextSkillIds,
        });
    };

    const candidateCounts = useMemo(() => {
        const counts = {
            eligible: 0,
            ready: 0,
            active: 0,
            syncing: 0,
            stale: 0,
            failed: 0,
            blocked: 0,
            skipped: 0,
        };
        for (const cand of preview?.candidates || []) {
            if (cand.eligible) counts.eligible += 1;
            else counts.skipped += 1;
            if (cand.category === "ready") counts.ready += 1;
            else if (cand.category === "active") counts.active += 1;
            else if (cand.category === "syncing") counts.syncing += 1;
            else if (cand.category === "stale") counts.stale += 1;
            else if (cand.category === "failed") counts.failed += 1;
            else if (cand.category === "blocked") counts.blocked += 1;
        }
        return counts;
    }, [preview?.candidates]);

    const logItemByShot = useMemo(() => {
        const map = new Map<string, BatchItem>();
        if (!log || log.stage !== stage || (!hasActiveRun && preview)) return map;
        for (const item of log.items || []) {
            map.set(item.shotId, item);
        }
        return map;
    }, [hasActiveRun, log, preview, stage]);

    const showStopButton = controller.confirming || Boolean(
        log &&
            (log.state === "running" ||
                log.state === "waiting" ||
                log.items.some(item => item.state === "pending" || item.state === "submitting")) &&
            log.state !== "stopped" &&
            log.state !== "finished",
    );

    const showRefreshButton = Boolean(
        log &&
            log.items.some(
                item =>
                    item.state === "submitted" ||
                    item.state === "syncing" ||
                    item.state === "unknown",
            ),
    );

    const showResumeButton = Boolean(
        log &&
            !hasActiveRun &&
            (log.state === "paused" || log.state === "stopped") &&
            log.items.some(item => item.state === "pending" || item.state === "stopped"),
    );

    const showRetryFailedButton = Boolean(
        log &&
            !hasActiveRun &&
            log.items.some(item => item.state === "failed"),
    );

    const hasUnknownItems = Boolean(log?.items.some(item => item.state === "unknown"));

    const settingsControls = (
        <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs font-medium">处理范围</span>
                <SegmentedControl
                    size="sm"
                    value={scope}
                    options={SCOPE_OPTIONS}
                    disabled={hasActiveRun}
                    ariaLabel="处理范围"
                    onChange={nextScope => {
                        if (hasActiveRun) return;
                        void controller.refreshPreview(nextScope as BatchScope, settings);
                    }}
                />
            </div>
            {scope === "failed" ? (
                <p className="text-xs text-muted-foreground">
                    失败重试模式锁定原任务设置，使用原任务 ID 重新入队，不新建重复任务。
                </p>
            ) : null}
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <div>
                    <div className="mb-1 text-xs font-medium">图片模型</div>
                    <ModelPicker
                        config={config}
                        value={settings.model}
                        capability="image"
                        onChange={settingsLocked ? () => {} : handleModelChange}
                        fullWidth
                        placeholder="选择图片模型"
                        showSelectedPrice
                    />
                </div>
                <div>
                    <div className="mb-1 text-xs font-medium">技能库</div>
                    <SkillRuntimePicker
                        profile="shortDrama"
                        skills={skills}
                        value={settings.skillIds}
                        onChange={settingsLocked ? () => {} : handleSkillsChange}
                    />
                </div>
            </div>
            {imageProfile ? (
                <div className="flex flex-col gap-2">
                    <ImageSizePicker
                        profile={imageProfile}
                        size={settings.size}
                        quality={settings.quality}
                        onChange={settingsLocked ? () => {} : handleSizeQualityChange}
                    />
                    {imageProfile.quality.supported && !imageResolutionUsesQuality(imageProfile) ? (
                        <div>
                            <div className="mb-1 text-xs font-medium">生成画质</div>
                            <Select
                                value={settings.quality}
                                disabled={settingsLocked}
                                onChange={handleQualityOnlyChange}
                                options={imageProfile.quality.values.map(value => ({
                                    value,
                                    label: imageQualityLabel(value),
                                }))}
                            />
                        </div>
                    ) : null}
                </div>
            ) : null}
        </div>
    );

    const candidateRows = (() => {
        const candidates: BatchCandidate[] = preview?.candidates || [];
        if (candidates.length === 0 && (!log || log.items.length === 0)) {
            return (
                <EmptyState
                    size="compact"
                    title="当前章节暂无可处理的分镜"
                    description="请先在当前章节添加并保存镜头脚本后再执行批量生成"
                />
            );
        }

        if (candidates.length > 0) {
            return (
                <div className="flex flex-col gap-2">
                    {candidates.map((cand, index) => {
                        const logItem = logItemByShot.get(cand.shotId);
                        const badge = logItem
                            ? batchItemStateLabel(logItem.state)
                            : categoryStatusBadge(cand.category);
                        const issueText = [
                            ...cand.issues,
                            ...(logItem?.issueCode
                                ? [issueCodeDescription(logItem.issueCode)].filter(Boolean)
                                : []),
                        ].join("；");
                        return (
                            <div
                                key={cand.shotId}
                                className="flex flex-wrap items-center justify-between gap-2 rounded border border-[var(--workspace-border)] px-3 py-2 text-xs"
                                data-shot-id={cand.shotId}
                            >
                                <div className="flex items-center gap-2">
                                    <span className="font-mono text-muted-foreground">
                                        {String(index + 1).padStart(2, "0")}
                                    </span>
                                    <strong>{cand.title}</strong>
                                    <StatusBadge tone={badge.tone} label={badge.label} />
                                    {cand.eligible ? (
                                        <span className="text-muted-foreground">纳入本次提交</span>
                                    ) : (
                                        <span className="text-muted-foreground">本次跳过</span>
                                    )}
                                </div>
                                <div className="flex flex-wrap items-center gap-2 text-muted-foreground">
                                    {logItem?.taskId || cand.taskId ? (
                                        <span>任务 {logItem?.taskId || cand.taskId}</span>
                                    ) : null}
                                    {issueText ? <span>{issueText}</span> : null}
                                </div>
                            </div>
                        );
                    })}
                </div>
            );
        }

        return (
            <div className="flex flex-col gap-2">
                {(log?.items || []).map((item, index) => {
                    const badge = batchItemStateLabel(item.state);
                    const issueText = issueCodeDescription(item.issueCode);
                    return (
                        <div
                            key={item.itemId}
                            className="flex flex-wrap items-center justify-between gap-2 rounded border border-[var(--workspace-border)] px-3 py-2 text-xs"
                            data-shot-id={item.shotId}
                        >
                            <div className="flex items-center gap-2">
                                <span className="font-mono text-muted-foreground">
                                    {String(index + 1).padStart(2, "0")}
                                </span>
                                <strong>{item.shotId}</strong>
                                <StatusBadge tone={badge.tone} label={badge.label} />
                                {item.attempts > 0 ? (
                                    <span className="text-muted-foreground">
                                        已尝试 {item.attempts} 次
                                    </span>
                                ) : null}
                            </div>
                            <div className="flex flex-wrap items-center gap-2 text-muted-foreground">
                                {item.taskId ? <span>任务 {item.taskId}</span> : null}
                                {issueText ? <span>{issueText}</span> : null}
                            </div>
                        </div>
                    );
                })}
            </div>
        );
    })();

    const priceAndCountSummary = (
        <div className="flex flex-col gap-1 text-xs">
            {controller.corruptedLogError ? (
                <div className="flex flex-wrap items-center justify-between gap-2 text-danger">
                    <span>
                        本地批次记录无法解析（{controller.corruptedLogError}
                        ）。清理前请先前往任务中心核对是否有在途或刚完成的任务，避免重复提交。
                    </span>
                    <div className="flex items-center gap-2">
                        <Link to="/tasks">前往任务中心</Link>
                        <Button
                            size="small"
                            danger
                            onClick={() => void controller.clearCorruptedLog()}
                        >
                            清理损坏记录
                        </Button>
                    </div>
                </div>
            ) : null}
            {controller.blockedReason && !controller.corruptedLogError ? (
                <p className="text-danger">{controller.blockedReason}</p>
            ) : null}
            {controller.runtimeError ? (
                <p className="text-danger">{controller.runtimeError}</p>
            ) : null}
            {controller.statusNotice ? (
                <p className="text-muted-foreground">{controller.statusNotice}</p>
            ) : null}
            {preview ? (
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <span>
                        可提交 {candidateCounts.eligible} · 已完成 {candidateCounts.ready} · 在途{" "}
                        {candidateCounts.active + candidateCounts.syncing} · 过期{" "}
                        {candidateCounts.stale} · 失败 {candidateCounts.failed} · 阻止{" "}
                        {candidateCounts.blocked}
                        {candidateCounts.skipped > 0
                            ? `（已跳过 ${candidateCounts.skipped} 个镜头）`
                            : ""}
                    </span>
                </div>
            ) : null}
            {log ? (
                <div className="flex flex-wrap items-center justify-between gap-2 text-muted-foreground">
                    <span>
                        当前批次状态：{log.state} · 成功{" "}
                        {log.items.filter(i => i.state === "succeeded").length} · 失败{" "}
                        {log.items.filter(i => i.state === "failed").length} · 待提交{" "}
                        {log.items.filter(i => i.state === "pending").length} · 已停止{" "}
                        {log.items.filter(i => i.state === "stopped").length} · 未知{" "}
                        {log.items.filter(i => i.state === "unknown").length}
                    </span>
                    <span>并行生成，完成后自动补位（账户活动任务上限 {controller.activeTaskLimit}）</span>
                </div>
            ) : null}
            {hasUnknownItems ? (
                <div className="mt-1 flex flex-wrap items-center gap-2">
                    <Input
                        size="small"
                        style={{ maxWidth: 260 }}
                        value={manualTaskId}
                        onChange={e => setManualTaskId(e.target.value)}
                        placeholder="输入确切任务 ID 进行正向认领"
                        aria-label="确切任务 ID"
                    />
                    <Button
                        size="small"
                        loading={controller.busy}
                        onClick={() => void controller.refreshStatus(manualTaskId)}
                    >
                        核对任务
                    </Button>
                    <Link to="/tasks" className="text-xs">
                        打开任务中心核对
                    </Link>
                </div>
            ) : null}
        </div>
    );

    return (
        <AppModal
            flush
            open={controller.dialogOpen}
            title={stage === "previz" ? "批量生成动作预演" : "批量生成分镜图"}
            onCancel={controller.closeDialog}
            footer={null}
            width="min(880px, calc(100vw - 32px))"
        >
            <div className="workflow-batch-dialog">
                <p>仅处理当前章节；每镜提交一个图片任务。</p>
                {stage === "previz" ? (
                    <p>动作预演为黑白图片，不是视频，不自动使用已生成分镜图。</p>
                ) : null}
                <div className="workflow-batch-dialog-controls">{settingsControls}</div>
                <div
                    className="workflow-batch-dialog-list"
                    role="region"
                    aria-label="批量镜头检查结果"
                >
                    {candidateRows}
                </div>
                <div role="status" aria-live="polite">
                    {priceAndCountSummary}
                </div>
                <footer>
                    {preview ? <strong className="basis-full text-right text-xs" aria-live="polite">{batchPriceLabel(preview.price)}</strong> : null}
                    {showStopButton ? (
                        <Button danger onClick={() => void controller.stopRemaining()}>
                            停止剩余提交（已提交任务仍继续运行）
                        </Button>
                    ) : null}
                    {showRefreshButton ? (
                        <Button
                            loading={controller.busy}
                            onClick={() => void controller.refreshStatus(manualTaskId)}
                        >
                            刷新状态
                        </Button>
                    ) : null}
                    {showResumeButton ? (
                        <Button
                            disabled={controller.busy || Boolean(controller.blockedReason)}
                            onClick={() => void controller.prepareResume()}
                        >
                            继续剩余
                        </Button>
                    ) : null}
                    {showRetryFailedButton ? (
                        <Button
                            disabled={controller.busy || Boolean(controller.blockedReason)}
                            onClick={() => void controller.prepareRetryFailed()}
                        >
                            重试失败
                        </Button>
                    ) : null}
                    <Button onClick={controller.closeDialog}>取消</Button>
                    <Button
                        type="primary"
                        disabled={!canConfirm}
                        loading={controller.busy}
                        onClick={() => void controller.confirm()}
                    >
                        确认提交 {controller.preview?.items.length ?? 0} 个任务
                    </Button>
                </footer>
            </div>
        </AppModal>
    );
}
