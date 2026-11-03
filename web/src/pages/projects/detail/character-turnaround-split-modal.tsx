import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Button, Checkbox, InputNumber, Select } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Columns3, Sparkles } from "lucide-react";

import { AppModal } from "@/components/ui/product/app-modal";
import { ModelPicker } from "@/components/model-picker";
import { CachedResourceImage } from "@/components/cached-resource-image";
import { useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { resourceFileUrl } from "@/services/api/resources";
import { ApiError } from "@/services/api/request";
import {
    createCharacterTurnaroundSplitPlan,
    executeCharacterTurnaroundSplit,
    type Project,
    type ProjectAsset,
    type TurnaroundConfirmedCropBox,
    type TurnaroundSplitPlanDraft,
    type TurnaroundViewRole,
} from "@/services/api/projects";

import { profileAnalysisConfig, profileAnalysisModel, profileAnalysisRequirements } from "./project-profile-analysis";
import {
    applyVisionPlanDraftToBoxes,
    buildDefaultEqualThirdsBoxes,
    clampSourceCropRect,
    findTurnaroundSheetRepresentation,
    nudgeTurnaroundCropBox,
    scaleSourceToPreviewRect,
    TURNAROUND_VIEW_LABELS,
    TURNAROUND_VIEW_ROLES,
    validateTurnaroundSplitBoxes,
} from "./character-turnaround-split";

type Props = {
    project: Project;
    asset: ProjectAsset;
    onClose: () => void;
    onSaved: (updatedAsset?: ProjectAsset) => void | Promise<void>;
};

export function CharacterTurnaroundSplitModal({ project, asset, onClose, onSaved }: Props) {
    const queryClient = useQueryClient();
    const config = useEffectiveConfig();
    const storedTextModel = useConfigStore((state) => state.config.textModel);
    const [model, setModel] = useState(() => (profileAnalysisModel(config, storedTextModel || "") ? storedTextModel : ""));
    const selectedConfig = profileAnalysisConfig(config, model);
    const modelSelection = profileAnalysisModel(selectedConfig, model);

    const turnaroundRep = useMemo(
        () => findTurnaroundSheetRepresentation(asset.character?.representations),
        [asset.character?.representations],
    );
    const expectedVersionId = asset.character?.versionId || asset.primaryVersionId || "";

    const [sourceSize, setSourceSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 });
    const [previewSize, setPreviewSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 });
    const [boxesByRole, setBoxesByRole] = useState<Record<TurnaroundViewRole, TurnaroundConfirmedCropBox>>(() =>
        buildDefaultEqualThirdsBoxes(300, 100, false),
    );
    const [activeRole, setActiveRole] = useState<TurnaroundViewRole>("front");
    const [nudgeMode, setNudgeMode] = useState<"move" | "resize">("move");
    const [notesByRole, setNotesByRole] = useState<Partial<Record<TurnaroundViewRole, string>>>({});
    const [manualReason, setManualReason] = useState<string>("");
    const [requiresManualReview, setRequiresManualReview] = useState<boolean>(false);
    const [planning, setPlanning] = useState<boolean>(false);
    const [submitting, setSubmitting] = useState<boolean>(false);
    const [errorMessage, setErrorMessage] = useState<string>("");
    const [savedMessage, setSavedMessage] = useState<string>("");
    const idempotencyKeyRef = useRef<string>(`turnaround-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`);
    const previewContainerRef = useRef<HTMLDivElement | null>(null);

    const resetIdempotencyKey = useCallback(() => {
        idempotencyKeyRef.current = `turnaround-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    }, []);

    useEffect(() => {
        if (sourceSize.width > 0 && sourceSize.height > 0) {
            setBoxesByRole(buildDefaultEqualThirdsBoxes(sourceSize.width, sourceSize.height, false));
            resetIdempotencyKey();
        }
    }, [sourceSize.width, sourceSize.height, resetIdempotencyKey]);

    useEffect(() => {
        const el = previewContainerRef.current;
        if (!el || typeof ResizeObserver === "undefined") return;
        const observer = new ResizeObserver((entries) => {
            const rect = entries[0]?.contentRect;
            if (rect && rect.width > 0 && rect.height > 0) {
                setPreviewSize({ width: Math.round(rect.width), height: Math.round(rect.height) });
            }
        });
        observer.observe(el);
        return () => observer.disconnect();
    }, []);

    const boxesList = useMemo(() => TURNAROUND_VIEW_ROLES.map((role) => boxesByRole[role]), [boxesByRole]);
    const validation = useMemo(
        () => validateTurnaroundSplitBoxes(boxesList, sourceSize.width, sourceSize.height),
        [boxesList, sourceSize.width, sourceSize.height],
    );

    const handleResetEqualThirds = () => {
        if (sourceSize.width <= 0 || sourceSize.height <= 0) return;
        setBoxesByRole(buildDefaultEqualThirdsBoxes(sourceSize.width, sourceSize.height, false));
        setManualReason("");
        setRequiresManualReview(false);
        setErrorMessage("");
        resetIdempotencyKey();
    };

    const handleConfirmAll = (confirmed: boolean) => {
        setBoxesByRole((prev) => ({
            front: { ...prev.front, confirmed },
            side: { ...prev.side, confirmed },
            back: { ...prev.back, confirmed },
        }));
        setErrorMessage("");
    };

    const updateBoxRect = (role: TurnaroundViewRole, patch: Partial<Pick<TurnaroundConfirmedCropBox, "x" | "y" | "width" | "height" | "confirmed">>) => {
        setBoxesByRole((prev) => {
            const current = prev[role];
            const clamped =
                sourceSize.width > 0 && sourceSize.height > 0
                    ? clampSourceCropRect(
                          {
                              x: patch.x ?? current.x,
                              y: patch.y ?? current.y,
                              width: patch.width ?? current.width,
                              height: patch.height ?? current.height,
                          },
                          sourceSize.width,
                          sourceSize.height,
                      )
                    : {
                          x: patch.x ?? current.x,
                          y: patch.y ?? current.y,
                          width: patch.width ?? current.width,
                          height: patch.height ?? current.height,
                      };
            return {
                ...prev,
                [role]: {
                    ...current,
                    ...clamped,
                    confirmed: patch.confirmed ?? current.confirmed,
                },
            };
        });
        if (patch.x !== undefined || patch.y !== undefined || patch.width !== undefined || patch.height !== undefined) {
            resetIdempotencyKey();
        }
        setErrorMessage("");
    };

    const swapRoleAssignment = (fromRole: TurnaroundViewRole, toRole: TurnaroundViewRole) => {
        if (fromRole === toRole) return;
        setBoxesByRole((prev) => {
            const fromBox = prev[fromRole];
            const toBox = prev[toRole];
            return {
                ...prev,
                [fromRole]: { ...toBox, role: fromRole, confirmed: false },
                [toRole]: { ...fromBox, role: toRole, confirmed: false },
            };
        });
        setActiveRole(toRole);
        resetIdempotencyKey();
    };

    const handleKeyDownNudge = (event: KeyboardEvent<HTMLDivElement>) => {
        if (sourceSize.width <= 0 || sourceSize.height <= 0) return;
        const step = event.shiftKey ? 10 : 1;
        let dx = 0;
        let dy = 0;
        if (event.key === "ArrowLeft") dx = -step;
        else if (event.key === "ArrowRight") dx = step;
        else if (event.key === "ArrowUp") dy = -step;
        else if (event.key === "ArrowDown") dy = step;
        else return;

        event.preventDefault();
        setBoxesByRole((prev) => ({
            ...prev,
            [activeRole]: nudgeTurnaroundCropBox(prev[activeRole], { dx, dy }, sourceSize.width, sourceSize.height, nudgeMode),
        }));
        resetIdempotencyKey();
    };

    const handleApplyVisionDraft = useCallback(
        (draft: TurnaroundSplitPlanDraft) => {
            if (sourceSize.width <= 0 || sourceSize.height <= 0) return;
            const applied = applyVisionPlanDraftToBoxes(draft, sourceSize.width, sourceSize.height);
            setBoxesByRole(applied.boxesByRole);
            setNotesByRole(applied.notesByRole);
            setRequiresManualReview(applied.requiresManualReview);
            setManualReason(applied.manualReason || "");
            resetIdempotencyKey();
        },
        [sourceSize.width, sourceSize.height, resetIdempotencyKey],
    );

    const handleRequestVisionPlan = async () => {
        if (!turnaroundRep || !expectedVersionId || !modelSelection) return;
        setPlanning(true);
        setErrorMessage("");
        try {
            const { plan } = await createCharacterTurnaroundSplitPlan(project.id, asset.id, {
                expectedVersionId,
                sourceResourceId: turnaroundRep.resourceId,
                modelSelection,
            });
            if (plan.sourceWidth > 0 && plan.sourceHeight > 0 && (sourceSize.width === 0 || sourceSize.height === 0)) {
                setSourceSize({ width: plan.sourceWidth, height: plan.sourceHeight });
            }
            if (plan.draft) {
                handleApplyVisionDraft(plan.draft);
            } else {
                setManualReason("识图建议任务已创建（" + plan.stage + "），当前可直接使用三分均分初值或手动微调三个视角裁框并逐项确认。");
            }
        } catch (err) {
            setErrorMessage(err instanceof ApiError ? err.message : err instanceof Error ? err.message : "识图分析请求失败，请手动调整裁框");
        } finally {
            setPlanning(false);
        }
    };

    const handleSubmitSplit = async () => {
        if (!turnaroundRep || !expectedVersionId) return;
        if (!validation.canSave) {
            setErrorMessage(validation.reason || "请先完成三个视角裁框确认");
            return;
        }
        setSubmitting(true);
        setErrorMessage("");
        try {
            const result = await executeCharacterTurnaroundSplit(project.id, asset.id, {
                idempotencyKey: idempotencyKeyRef.current,
                expectedVersionId,
                sourceResourceId: turnaroundRep.resourceId,
                sourceWidth: sourceSize.width,
                sourceHeight: sourceSize.height,
                boxes: validation.orderedBoxes,
            });
            setSavedMessage("服务端已保存：已从三视图整图无损拆分正/侧/背三个视角并生成新角色版本");
            await Promise.allSettled([
                queryClient.invalidateQueries({ queryKey: ["project", project.id] }),
                queryClient.invalidateQueries({ queryKey: ["characters"] }),
                Promise.resolve(onSaved(result.character.asset)),
            ]);
        } catch (err) {
            setErrorMessage(err instanceof ApiError ? err.message : err instanceof Error ? err.message : "三视图拆分失败，请检查裁框与版本状态后重试");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <AppModal
            flush
            className="workspace-modal workspace-modal-wide library-modal"
            title={null}
            open
            onCancel={onClose}
            styles={{ body: { minHeight: 0, flex: 1 }, header: { margin: 0 }, footer: { margin: 0 } }}
            footer={
                <div className="flex flex-wrap items-center justify-between gap-2 px-6 py-4" style={{ borderTop: "1px solid var(--workspace-border)" }}>
                    <div className="text-xs text-foreground/60">
                        {savedMessage ? (
                            <span className="font-medium text-emerald-600 dark:text-emerald-400">{savedMessage}</span>
                        ) : errorMessage ? (
                            <span className="font-medium text-red-600 dark:text-red-400">{errorMessage}</span>
                        ) : (
                            <span>{validation.canSave ? "三个视角裁框与标签均已人工确认，可安全拆分保存。" : validation.reason}</span>
                        )}
                    </div>
                    <div className="flex items-center gap-2">
                        <Button onClick={onClose}>关闭</Button>
                        <Button
                            type="primary"
                            loading={submitting}
                            disabled={!turnaroundRep || !validation.canSave || Boolean(savedMessage)}
                            onClick={() => void handleSubmitSplit()}
                        >
                            确认拆分为三视图并生成新版本
                        </Button>
                    </div>
                </div>
            }
        >
            <div className="flex min-h-0 flex-col" style={{ height: "min(860px, calc(100dvh - 120px))" }}>
                <div className="shrink-0 px-6 pt-5 pb-3" style={{ borderBottom: "1px solid var(--workspace-border)" }}>
                    <div className="text-[var(--fs-label)] font-medium text-foreground/52">角色三视图拆图与打标 · {asset.title}</div>
                    <h2 className="mt-1 text-xl font-semibold">从三视图整图无损拆分正 / 侧 / 背视角</h2>
                    <p className="mt-1 text-xs text-foreground/55">
                        仅基于原图真实像素无损裁剪 PNG，不重绘角色外观；拆分后保留原三视图整图（turnaround_sheet）与既有声音/设定，不会覆盖默认参考图优先级。
                    </p>
                </div>

                {!turnaroundRep ? (
                    <div className="grid flex-1 place-items-center p-6 text-center">
                        <div className="max-w-md rounded-lg border border-border/70 bg-foreground/[.025] p-5">
                            <div className="text-sm font-semibold">当前角色尚未绑定三视图整图（turnaround_sheet）</div>
                            <p className="mt-2 text-xs leading-5 text-foreground/58">
                                普通单人肖像（primary）不能直接执行三视图拆分。请先在角色图片创作或绑定入口上传/生成一张包含正面、侧面、背面的角色三视图整图。
                            </p>
                        </div>
                    </div>
                ) : (
                    <div className="grid min-h-0 flex-1 gap-5 overflow-y-auto overscroll-contain p-6 lg:grid-cols-[1.25fr_1fr]">
                        <div className="flex flex-col gap-3">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                                <span className="text-xs font-medium text-foreground/70">
                                    源图尺寸：{sourceSize.width > 0 ? `${sourceSize.width} × ${sourceSize.height} px` : "正在读取源图像素尺寸…"}
                                </span>
                                <div className="flex flex-wrap items-center gap-1.5">
                                    <Button size="small" icon={<Columns3 className="size-3.5" />} onClick={handleResetEqualThirds} disabled={sourceSize.width <= 0}>
                                        三分均分初值
                                    </Button>
                                    <Button size="small" icon={<Check className="size-3.5" />} onClick={() => handleConfirmAll(true)} disabled={sourceSize.width <= 0}>
                                        全部确认
                                    </Button>
                                </div>
                            </div>

                            <div
                                ref={previewContainerRef}
                                tabIndex={0}
                                onKeyDown={handleKeyDownNudge}
                                aria-label="三视图裁框预览画布（支持方向键微调当前选中视角框）"
                                className="relative overflow-hidden rounded-lg border border-border/70 bg-black/80 outline-none focus-visible:ring-2 focus-visible:ring-[var(--workspace-accent)]"
                            >
                                <CachedResourceImage
                                    storageKey={`resource:${turnaroundRep.resourceId}`}
                                    src={resourceFileUrl(turnaroundRep.resourceId)}
                                    alt={`${asset.title} 三视图整图`}
                                    className="block h-auto w-full select-none object-contain"
                                    onLoad={(event) => {
                                        const img = event.currentTarget;
                                        if (img.naturalWidth > 0 && img.naturalHeight > 0) {
                                            setSourceSize({ width: img.naturalWidth, height: img.naturalHeight });
                                        }
                                        if (img.clientWidth > 0 && img.clientHeight > 0) {
                                            setPreviewSize({ width: Math.round(img.clientWidth), height: Math.round(img.clientHeight) });
                                        }
                                    }}
                                />
                                {sourceSize.width > 0 && sourceSize.height > 0 && previewSize.width > 0 && previewSize.height > 0
                                    ? TURNAROUND_VIEW_ROLES.map((role) => {
                                          const box = boxesByRole[role];
                                          const rect = scaleSourceToPreviewRect(box, sourceSize.width, sourceSize.height, previewSize.width, previewSize.height);
                                          const isSelected = activeRole === role;
                                          return (
                                              <div
                                                  key={role}
                                                  role="button"
                                                  tabIndex={-1}
                                                  onClick={() => setActiveRole(role)}
                                                  style={{
                                                      left: `${rect.x}px`,
                                                      top: `${rect.y}px`,
                                                      width: `${rect.width}px`,
                                                      height: `${rect.height}px`,
                                                  }}
                                                  className={`absolute cursor-pointer transition-colors ${
                                                      isSelected
                                                          ? "border-2 border-[var(--workspace-accent)] bg-[var(--workspace-accent)]/15"
                                                          : "border border-white/75 bg-black/20 hover:border-white"
                                                  }`}
                                              >
                                                  <span className="inline-flex items-center gap-1 rounded-br bg-black/75 px-1.5 py-0.5 text-[11px] font-medium text-white">
                                                      {TURNAROUND_VIEW_LABELS[role]} ({role})
                                                      {box.confirmed ? " · 已确认" : " · 待确认"}
                                                  </span>
                                              </div>
                                          );
                                      })
                                    : null}
                            </div>

                            <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 bg-foreground/[.025] px-3 py-2 text-xs text-foreground/65">
                                <span>
                                    键盘微调：点击画布后按方向键微调「{TURNAROUND_VIEW_LABELS[activeRole]}」（步长 1px，按住 Shift 为 10px）
                                </span>
                                <Select
                                    size="small"
                                    value={nudgeMode}
                                    onChange={(val) => setNudgeMode(val)}
                                    options={[
                                        { value: "move", label: "方向键：移动位置" },
                                        { value: "resize", label: "方向键：调整宽高" },
                                    ]}
                                />
                            </div>

                            <div className="rounded-md border border-border/60 bg-foreground/[.02] p-3">
                                <div className="flex flex-wrap items-center justify-between gap-2">
                                    <span className="text-xs font-medium">可选：视觉模型辅助识图定位</span>
                                    <div className="flex items-center gap-2">
                                        <ModelPicker
                                            config={selectedConfig}
                                            capability="text"
                                            value={model}
                                            onChange={setModel}
                                            requirements={profileAnalysisRequirements}
                                            modelFilter={(value) => Boolean(profileAnalysisModel(selectedConfig, value))}
                                        />
                                        <Button
                                            size="small"
                                            icon={<Sparkles className="size-3.5" />}
                                            loading={planning}
                                            disabled={!modelSelection || sourceSize.width <= 0}
                                            onClick={() => void handleRequestVisionPlan()}
                                        >
                                            AI 建议裁框
                                        </Button>
                                    </div>
                                </div>
                                {requiresManualReview || manualReason ? (
                                    <div className="mt-2 rounded bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-700 dark:text-amber-300">
                                        {manualReason || "当前图片可能缺少部分视角或边界存在歧义，请人工调整裁框并逐项确认后再保存。"}
                                    </div>
                                ) : null}
                            </div>
                        </div>

                        <div className="flex flex-col gap-3">
                            <div className="text-xs font-semibold text-foreground/70">三视角像素裁框与标签确认（正 / 侧 / 背缺一不可）</div>
                            {TURNAROUND_VIEW_ROLES.map((role) => {
                                const box = boxesByRole[role];
                                const isSelected = activeRole === role;
                                return (
                                    <div
                                        key={role}
                                        onClick={() => setActiveRole(role)}
                                        className={`rounded-lg border p-3 transition-colors ${
                                            isSelected ? "border-[var(--workspace-accent)] bg-surface-active" : "border-border/70 bg-foreground/[.02]"
                                        }`}
                                    >
                                        <div className="flex items-center justify-between gap-2">
                                            <div className="flex items-center gap-2">
                                                <span className="text-sm font-semibold">
                                                    {TURNAROUND_VIEW_LABELS[role]}（{role}）
                                                </span>
                                                <Select
                                                    size="small"
                                                    aria-label={`交换 ${TURNAROUND_VIEW_LABELS[role]} 标签`}
                                                    value={role}
                                                    onChange={(targetRole) => swapRoleAssignment(role, targetRole)}
                                                    options={TURNAROUND_VIEW_ROLES.map((r) => ({
                                                        value: r,
                                                        label: `设为${TURNAROUND_VIEW_LABELS[r]} (${r})`,
                                                    }))}
                                                />
                                            </div>
                                            <Checkbox
                                                checked={box.confirmed}
                                                onChange={(e) => updateBoxRect(role, { confirmed: e.target.checked })}
                                            >
                                                <span className="text-xs font-medium">已确认该视角</span>
                                            </Checkbox>
                                        </div>

                                        {notesByRole[role] ? (
                                            <div className="mt-1 text-xs text-foreground/55">识图备注：{notesByRole[role]}</div>
                                        ) : null}

                                        <div className="mt-2.5 grid grid-cols-4 gap-2">
                                            <label className="grid gap-1 text-[11px] text-foreground/55">
                                                <span>X (px)</span>
                                                <InputNumber
                                                    size="small"
                                                    min={0}
                                                    max={Math.max(0, sourceSize.width - 1)}
                                                    value={box.x}
                                                    onChange={(val) => updateBoxRect(role, { x: Number(val ?? 0) })}
                                                    className="!w-full"
                                                />
                                            </label>
                                            <label className="grid gap-1 text-[11px] text-foreground/55">
                                                <span>Y (px)</span>
                                                <InputNumber
                                                    size="small"
                                                    min={0}
                                                    max={Math.max(0, sourceSize.height - 1)}
                                                    value={box.y}
                                                    onChange={(val) => updateBoxRect(role, { y: Number(val ?? 0) })}
                                                    className="!w-full"
                                                />
                                            </label>
                                            <label className="grid gap-1 text-[11px] text-foreground/55">
                                                <span>宽 W (px)</span>
                                                <InputNumber
                                                    size="small"
                                                    min={1}
                                                    max={Math.max(1, sourceSize.width - box.x)}
                                                    value={box.width}
                                                    onChange={(val) => updateBoxRect(role, { width: Number(val ?? 1) })}
                                                    className="!w-full"
                                                />
                                            </label>
                                            <label className="grid gap-1 text-[11px] text-foreground/55">
                                                <span>高 H (px)</span>
                                                <InputNumber
                                                    size="small"
                                                    min={1}
                                                    max={Math.max(1, sourceSize.height - box.y)}
                                                    value={box.height}
                                                    onChange={(val) => updateBoxRect(role, { height: Number(val ?? 1) })}
                                                    className="!w-full"
                                                />
                                            </label>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                )}
            </div>
        </AppModal>
    );
}
