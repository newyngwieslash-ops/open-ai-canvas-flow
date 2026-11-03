// Reference-image profile review uses AppModal and semantic surface/foreground tokens in both themes.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Checkbox, Select } from "antd";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { AppModal } from "@/components/ui/product/app-modal";
import { ModelPicker } from "@/components/model-picker";
import { CachedResourceImage } from "@/components/cached-resource-image";
import { useUserStore } from "@/stores/use-user-store";
import { useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { resourceFileUrl, resourceStorageKey } from "@/services/api/resources";
import { ApiError } from "@/services/api/request";
import { profileAnalysisErrorMessage, PROFILE_ANALYSIS_REASONS } from "@/services/api/error-codes";
import { cancelGenerationTask } from "@/services/api/task-center";
import { loadAssetsForUse } from "@/services/user-data-sync";
import { adoptProjectAssetProfileAnalysis, createProjectAssetProfileAnalysis, getProjectAssetProfileAnalysis, listProjectAssetProfileAnalyses, listProjectAssetsPage, type ProfileAnalysisField, type ProfileAnalysisView, type Project, type ProjectAsset } from "@/services/api/projects";
import { profileAnalysisConfig, profileAnalysisFields, profileAnalysisKey, profileAnalysisModel, profileAnalysisRequirements, profileDefinition, profileFieldLabels, profileValue, validProfileDraft, profileAdoptionStorage, type PendingProfileAdoption } from "./project-profile-analysis";
import { ProjectAssetImageModal } from "./project-asset-image-modal";

type Props = { project: Project; asset: ProjectAsset; onClose: () => void; onSaved: () => void | Promise<void> };
const active = (view?: ProfileAnalysisView) => view?.status === "queued" || view?.status === "running";
const statusLabels: Record<ProfileAnalysisView["status"], string> = { queued: "排队中", running: "分析中", succeeded: "待审阅", failed: "分析失败", cancelled: "已取消" };

export function ProjectProfileAnalysisModal(props: Props) {
    const userId = useUserStore((state) => state.user?.id);
    const owner = useRef(userId);
    useEffect(() => { if (owner.current !== userId) props.onClose(); }, [userId, props.onClose]);
    // Account changes must not reuse the previous owner's asset prop.
    return userId && userId === owner.current ? <ProfileAnalysisSession key={`${userId}:${props.project.id}:${props.asset.id}`} {...props} userId={userId} /> : null;
}

function ProfileAnalysisSession({ project, asset: initialAsset, userId, onClose, onSaved }: Props & { userId: string }) {
    const [open, setOpen] = useState(true);
    const client = useQueryClient();
    const config = useEffectiveConfig();
    const storedTextModel = useConfigStore((state) => state.config.textModel);
    const [model, setModel] = useState(() => profileAnalysisModel(config, storedTextModel || "") ? storedTextModel : "");
    const selectedConfig = profileAnalysisConfig(config, model);
    const selection = profileAnalysisModel(selectedConfig, model);
    const key = profileAnalysisKey(userId, project.id, initialAsset.id);
    const [asset, setAsset] = useState(initialAsset);
    const [taskId, setTaskId] = useState("");
    const [selected, setSelected] = useState<ProfileAnalysisField[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [saved, setSaved] = useState("");
    const [conflict, setConflict] = useState(false);
    const [uncertainCreate, setUncertainCreate] = useState(false);
    const [referenceEditor, setReferenceEditor] = useState(false);
    const [pending, setPending] = useState<PendingProfileAdoption>();
    const [pendingReady, setPendingReady] = useState(false);
    const storage = useMemo(() => profileAdoptionStorage(userId, project.id, initialAsset.id), [userId, project.id, initialAsset.id]);
    const [currentRead, setCurrentRead] = useState<"ready" | "loading" | "unavailable">("ready");
    const assetRead = useRef<Promise<void> | null>(null);
    const mounted = useRef(true);
    const working = useRef(false);
    const alive = useCallback(() => mounted.current && useUserStore.getState().user?.id === userId, [userId]);
    useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
    useEffect(() => {
        let cancelled = false;
        void storage.load().then((record) => {
            if (cancelled || !alive()) return;
            if (record) { setPending(record); setTaskId(record.taskId); setSelected(record.request.selectedFields); }
            setPendingReady(true);
        }).catch(() => { if (!cancelled && alive()) setError("采用恢复记录读取失败，请关闭后重试；未提交新请求。"); });
        return () => { cancelled = true; };
    }, [storage, alive]);

    const history = useInfiniteQuery({
        queryKey: [...key, "list"], initialPageParam: undefined as string | undefined,
        queryFn: ({ pageParam, signal }) => listProjectAssetProfileAnalyses(project.id, asset.id, { limit: 20, beforeTaskId: pageParam, signal }),
        getNextPageParam: (page) => page.nextCursor,
        refetchInterval: (query) => query.state.data?.pages.some((page) => page.analyses.some(active)) ? 2000 : false,
        retry: false,
    });
    const tasks = history.data?.pages.flatMap((page) => page.analyses) || [];
    useEffect(() => { if (pendingReady && !taskId && tasks[0]) setTaskId(tasks[0].taskId); }, [pendingReady, taskId, tasks]);
    const detail = useQuery({
        queryKey: profileAnalysisKey(userId, project.id, asset.id, taskId || "no-task"),
        queryFn: ({ signal }) => getProjectAssetProfileAnalysis(project.id, asset.id, taskId, signal),
        enabled: Boolean(taskId), retry: false,
        refetchInterval: (query) => active(query.state.data?.analysis) ? 2000 : false,
    });
    const analysis = detail.data?.analysis;
    useEffect(() => {
        setSelected(pending?.taskId === taskId ? pending.request.selectedFields : []);
        setConflict(false); setError(""); setSaved("");
        // A pending record locks task navigation; clearing it must not reset the saved state.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [taskId]);
    const baseline = analysis?.baseline?.versionId === analysis?.baseVersionId && analysis?.baseline?.fields && typeof analysis.baseline.fields === "object" && !Array.isArray(analysis.baseline.fields) ? analysis.baseline.fields : undefined;
    const stale = Boolean(analysis && (analysis.baseVersionId !== asset.primaryVersionId || analysis.currentVersionId !== analysis.baseVersionId));
    const draftValid = Boolean(analysis && validProfileDraft(analysis.category, analysis.draft));
    const currentMismatch = Boolean(!saved && analysis?.currentVersionId && analysis.currentVersionId !== asset.primaryVersionId);
    const currentAvailable = !currentMismatch && currentRead === "ready";
    const adoptable = Boolean(pendingReady && !saved && baseline && currentAvailable && analysis?.canAdopt && analysis.status === "succeeded" && draftValid && !analysis.adoption && !stale && !conflict);

    const refreshAsset = useCallback((expectedVersionId?: string) => {
        if (assetRead.current) return assetRead.current;
        setCurrentRead("loading");
        const read = async () => {
            // No single-asset GET exists: page by ID without filtering on a possibly edited title.
            for (let pageNumber = 1; alive(); pageNumber++) {
                const page = await listProjectAssetsPage(project.id, { page: pageNumber, pageSize: 100 });
                if (!alive()) return;
                const latest = page.assets.find((item) => item.id === initialAsset.id);
                if (latest) {
                    if (expectedVersionId && latest.primaryVersionId !== expectedVersionId) throw new Error("current version unavailable");
                    setAsset(latest); setCurrentRead("ready");
                    return;
                }
                if (!page.hasMore) throw new Error("asset unavailable");
            }
        };
        assetRead.current = read().catch((reason) => { if (alive()) setCurrentRead("unavailable"); throw reason; }).finally(() => { assetRead.current = null; });
        return assetRead.current;
    }, [alive, project.id, initialAsset.id]);
    useEffect(() => {
        if (currentMismatch && !working.current) void refreshAsset(analysis?.currentVersionId).catch(() => {});
        // Reconcile once for each server-reported version, not repeatedly on a failed read.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [analysis?.currentVersionId, refreshAsset]);
    const afterSaved = async (refreshCurrent = false) => {
        if (!alive()) return;
        setSaved("服务端已保存"); setError(""); setPending(undefined);
        // Receipt confirmation is final even if local cleanup or any subsequent read fails.
        const results = await Promise.allSettled([
            storage.clear(),
            ...(refreshCurrent ? [refreshAsset()] : []),
            client.invalidateQueries({ queryKey: ["project", project.id] }, { throwOnError: true }),
            client.invalidateQueries({ queryKey: ["characters"] }, { throwOnError: true }),
            client.invalidateQueries({ queryKey: ["character-studio", asset.id] }, { throwOnError: true }),
            client.invalidateQueries({ queryKey: ["asset-library"] }, { throwOnError: true }),
            client.invalidateQueries({ queryKey: key }, { throwOnError: true }),
            loadAssetsForUse([asset.id], { refresh: true }),
            Promise.resolve().then(() => { if (alive()) return onSaved(); }),
        ]);
        if (alive() && results.some((result) => result.status === "rejected")) setSaved("服务端已保存，但本地刷新失败，请刷新页面后继续使用");
    };
    const run = async (action: () => Promise<void>) => {
        if (working.current || !alive()) return;
        working.current = true; setBusy(true); setError("");
        try { await action(); }
        catch (reason) { if (alive()) setError(profileAnalysisErrorMessage(reason instanceof ApiError ? reason.reason : undefined)); }
        finally { working.current = false; if (alive()) setBusy(false); }
    };
    const recover = async () => {
        if (taskId) {
            try {
                const recovered = await detail.refetch({ throwOnError: true });
                if (!alive()) return;
                if (recovered.data?.analysis.adoption) { await afterSaved(true); return; }
            } catch (reason) {
                if (!pending || !(reason instanceof ApiError && reason.reason === PROFILE_ANALYSIS_REASONS.notFound)) throw reason;
            }
        }
        await Promise.all([history.refetch({ throwOnError: true }), refreshAsset()]);
    };
    const primaryReferenceImage = asset.referenceImages?.[0] || asset.referenceImage;
    const create = () => run(async () => {
        if (!pendingReady || !currentAvailable || !selection || !asset.primaryVersionId || !primaryReferenceImage?.resourceId || pending) return;
        // Creation is not idempotent: every deliberate new request first reads recovery.
        const recovered = await history.refetch({ throwOnError: true });
        if (!alive() || recovered.data?.pages.some((page) => page.analyses.some(active))) return;
        try {
            const result = await createProjectAssetProfileAnalysis(project.id, asset.id, { expectedVersionId: asset.primaryVersionId, sourceResourceId: primaryReferenceImage.resourceId, modelSelection: selection });
            if (!alive()) return;
            client.setQueryData(profileAnalysisKey(userId, project.id, asset.id, result.analysis.taskId), result);
            setTaskId(result.analysis.taskId); setUncertainCreate(false);
            await history.refetch();
        } catch (reason) {
            if (!alive()) return;
            setUncertainCreate(true);
            setError("创建结果尚未确认，请先检查恢复列表；重新分析会创建新任务，可能再次计费。");
            await history.refetch();
            if (reason instanceof ApiError && reason.status && reason.status < 500) setError(profileAnalysisErrorMessage(reason.reason));
        }
    });
    const adopt = () => run(async () => {
        if (!pendingReady || saved || (!pending && (!analysis || !adoptable || !selected.length))) return;
        const record = pending || { taskId: analysis!.taskId, request: { expectedVersionId: analysis!.baseVersionId, selectedFields: [...selected].sort(), idempotencyKey: crypto.randomUUID() } };
        try { await storage.save(record); }
        catch { if (alive()) setError("采用请求未能持久保存，未提交；请检查浏览器存储后重试。"); return; }
        if (!alive()) return;
        setPending(record);
        if (pending) {
            // A durable task ID permits exact replay even after list/detail cleanup.
            try {
                const recovered = await detail.refetch({ throwOnError: true });
                if (!alive()) return;
                if (recovered.data?.analysis.adoption) { await afterSaved(true); return; }
            } catch (reason) {
                if (!(reason instanceof ApiError && reason.reason === PROFILE_ANALYSIS_REASONS.notFound)) throw reason;
            }
        }
        if (!alive()) return;
        try {
            const result = await adoptProjectAssetProfileAnalysis(project.id, asset.id, record.taskId, record.request);
            if (!alive()) return;
            setAsset(result.asset); setCurrentRead("ready");
            await afterSaved();
        } catch (reason) {
            if (!alive()) return;
            if (reason instanceof ApiError && reason.status === 409) {
                setConflict(true);
                setError(profileAnalysisErrorMessage(reason.reason));
                await storage.clear();
                if (!alive()) return;
                setPending(undefined);
                await Promise.allSettled([refreshAsset(), detail.refetch()]);
            } else {
                setError("采用结果尚未确认；字段选择已锁定，请刷新恢复，或重试同一采用请求。不会重新调用模型。");
                const recovered = await detail.refetch();
                if (alive() && recovered.data?.analysis.adoption) await afterSaved(true);
            }
        }
    });
    const cancel = () => run(async () => {
        if (!analysis?.canCancel) return;
        try { await cancelGenerationTask(analysis.taskId); }
        catch (reason) { if (!(reason instanceof ApiError && reason.reason === PROFILE_ANALYSIS_REASONS.taskStateChanged)) throw reason; }
        finally { if (alive()) await Promise.all([detail.refetch(), history.refetch()]); }
    });
    const showCurrent = stale || conflict;
    return <>
        <AppModal open={open} flush title={null} footer={null} width={960} onCancel={() => setOpen(false)} afterClose={onClose}>
            <div className="max-h-[85vh] overflow-y-auto p-6 text-foreground" data-canvas-no-zoom data-canvas-wheel-scroll>
                <h2 className="text-[length:var(--fs-title)] font-semibold">{asset.title} · 分析参考图</h2>
                <p className="mt-2 text-[length:var(--fs-body)] text-foreground/60">只分析可见内容，模型建议不是资产事实。默认不采用任何字段，关闭不会取消任务或保存草稿。</p>
                <div className="mt-5 grid gap-4 sm:grid-cols-2">
                    <figure aria-label="新分析原图" className="border border-border p-3" style={{ borderRadius: "var(--radius-lg)" }}>
                        <figcaption>新分析原图：已保存的原始参考图{primaryReferenceImage ? ` · ${primaryReferenceImage.resourceId}` : ""}</figcaption>
                        {!currentAvailable ? <p role="status">{currentRead === "unavailable" ? "当前值暂不可用，请刷新恢复" : "当前值正在刷新"}</p> : null}
                        {primaryReferenceImage ? <CachedResourceImage storageKey={resourceStorageKey(primaryReferenceImage.resourceId)} src={resourceFileUrl(primaryReferenceImage.resourceId)} alt="已保存的原始参考图" className="mt-2 max-h-48 w-full object-contain" /> : <><p className="my-3">尚无原始参考图，当前成品不能代替原图。保存后将以新主版本分析。</p><Button onClick={() => setReferenceEditor(true)} disabled={busy}>选择并保存原始参考图</Button></>}
                    </figure>
                    <div className="grid content-start gap-3">
                        <ModelPicker config={selectedConfig} value={model} onChange={setModel} capability="text" requirements={profileAnalysisRequirements} modelFilter={(value) => Boolean(profileAnalysisModel(selectedConfig, value))} fullWidth />
                        <p className="text-[length:var(--fs-body)] text-foreground/60">仅支持可信系统／逻辑视觉文本模型；自定义渠道与工作流模型暂不可用。不会自动切换付费模型。</p>
                        {!selection ? <p role="status">请选择已声明支持图片输入的系统文本模型</p> : null}
                        <Button disabled={busy || !pendingReady || !currentAvailable || Boolean(pending) || !selection || !primaryReferenceImage || !asset.primaryVersionId || history.isPending || history.isError || tasks.some(active)} onClick={create}>{taskId || uncertainCreate ? "重新分析（新任务）" : "开始分析"}</Button>
                        <p className="text-[length:var(--fs-body)] text-foreground/60">分析可能计费；重新分析会创建新任务，不会自动覆盖档案。</p>
                    </div>
                </div>
                <section aria-label="分析任务恢复" className="mt-5 grid gap-3">
                    <div className="flex flex-wrap gap-2"><Select aria-label="分析任务" className="min-w-56 flex-1" value={taskId || undefined} placeholder="尚无分析任务" disabled={busy || Boolean(pending)} onChange={setTaskId} options={tasks.map((task) => ({ value: task.taskId, label: `${task.adoption ? "已采用" : statusLabels[task.status]} · ${task.createdAt || task.taskId}` }))} /><Button disabled={busy} onClick={() => run(recover)}>刷新恢复</Button>{history.hasNextPage ? <Button onClick={() => void history.fetchNextPage()}>加载更早分析</Button> : null}</div>
                    {history.isError ? <p role="alert">恢复列表读取失败，请刷新后再提交分析。</p> : null}
                    {detail.isError ? <p role="alert">{profileAnalysisErrorMessage(detail.error instanceof ApiError ? detail.error.reason : undefined)}</p> : null}
                    {analysis ? <>
                        <figure aria-label="所选分析任务原图" className="border border-border p-3" style={{ borderRadius: "var(--radius-lg)" }}>
                            <figcaption>所选分析任务原图 · {analysis.source.resourceId}</figcaption>
                            <CachedResourceImage storageKey={resourceStorageKey(analysis.source.resourceId)} src={resourceFileUrl(analysis.source.resourceId)} alt="所选分析任务原图" className="mt-2 max-h-48 w-full object-contain" />
                        </figure>
                        <p role="status">{analysis.adoption ? "已采用" : statusLabels[analysis.status]}</p>
                        {active(analysis) ? <p>每2秒更新状态，不展示未经校验的模型流。取消仅尽力中止上游，不保证供应商停止或退费。</p> : null}
                        {analysis.canCancel ? <Button disabled={busy} onClick={cancel}>取消分析任务</Button> : null}
                        {analysis.error ? <p role="alert">{profileAnalysisErrorMessage(analysis.error.reason)}</p> : null}
                        {!baseline ? <p role="alert">历史基准不可读取，不能将当前值当作基准；禁止首次采用，已有采用请求仍可精确重放。</p> : null}
                        {analysis.blockedReason === PROFILE_ANALYSIS_REASONS.sourceUnavailable || analysis.blockedReason === PROFILE_ANALYSIS_REASONS.sourceChanged ? <p role="alert">{profileAnalysisErrorMessage(analysis.blockedReason)}</p> : null}
                        {stale ? <p role="alert">资产版本已变化，草稿已保留；仅供对照，不会更换基准或自动重新分析。</p> : null}
                        {draftValid ? <div className="grid gap-3">{profileAnalysisFields(analysis.category).map((field) => {
                            const suggestion = analysis.draft![field]!.trim();
                            const old = baseline?.[field];
                            return <section key={field} className="border border-border p-3" style={{ borderRadius: "var(--radius-lg)" }} aria-label={profileFieldLabels[field]}>
                                <Checkbox checked={selected.includes(field)} disabled={busy || Boolean(pending) || !adoptable || !suggestion} onChange={(event) => setSelected((values) => event.target.checked ? [...values, field] : values.filter((value) => value !== field))}>{profileFieldLabels[field]}</Checkbox>
                                <div className={`mt-2 grid gap-3 ${showCurrent ? "sm:grid-cols-3" : "sm:grid-cols-2"}`}>
                                    <div><strong>基准值</strong><pre className="mt-1 whitespace-pre-wrap break-words font-sans">{baseline ? profileValue(old) : "历史基准不可读取，不能将当前值当作基准"}</pre></div>
                                    {showCurrent ? <div><strong>当前值</strong><pre className="mt-1 whitespace-pre-wrap break-words font-sans">{currentAvailable ? profileValue(profileDefinition(asset)[field]) : currentRead === "unavailable" ? "当前值暂不可用，请刷新恢复" : "当前值正在刷新"}</pre></div> : null}
                                    <div><strong>建议值</strong><p className="mt-1 whitespace-pre-wrap break-words">{suggestion || "未知／待确认"}</p></div>
                                </div>
                                {baseline && old !== undefined && typeof old !== "string" && suggestion ? <p className="mt-2 text-[length:var(--fs-body)]">注意：原值为{Array.isArray(old) ? "数组" : "JSON"}，勾选采用将把整个字段替换为文本；不勾选保持原类型和值。</p> : null}
                            </section>;
                        })}</div> : analysis.draft ? <p role="alert">草稿格式不可用，不能采用。</p> : null}
                    </> : null}
                </section>
                {error ? <p role="alert" className="mt-4">{error}</p> : null}
                {saved ? <p role="status" className="mt-4">{saved}</p> : null}
                <div className="mt-5 flex justify-end gap-2"><Button onClick={() => setOpen(false)}>关闭</Button>{pending ? <Button type="primary" disabled={busy || conflict} onClick={adopt}>重试同一采用请求</Button> : <Button type="primary" disabled={busy || !adoptable || !selected.length} onClick={adopt}>采用所选{selected.length}项</Button>}</div>
            </div>
        </AppModal>
        {referenceEditor ? <ProjectAssetImageModal project={project} asset={asset} initialMode="generate" onClose={() => { setReferenceEditor(false); void run(refreshAsset); }} onSaved={() => { void run(async () => { await refreshAsset(); if (alive()) await onSaved(); }); }} /> : null}
    </>;
}
