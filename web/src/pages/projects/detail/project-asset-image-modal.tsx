// 项目资产单图创作：复用产品弹窗、模型与素材选择器，消费现有语义token。
import { useEffect, useRef, useState } from "react";
import { App, Button, Checkbox, Input, Radio, Select } from "antd";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { AppModal } from "@/components/ui/product/app-modal";
import { ModelPicker } from "@/components/model-picker";
import { AssetLibraryPickerModal, type AssetLibraryPickerItem } from "@/components/assets/asset-library-picker-modal";
import { CachedResourceImage } from "@/components/cached-resource-image";
import { resolveProjectCanvasStyle } from "@/components/canvas/canvas-style-picker-modal";
import { createProjectCharacter, createProjectAssetEntity, updateProjectAssetReferenceImage, adoptProjectAssetImageCandidate, bindProjectAssetImage, replaceProjectCharacterRepresentations, listProjectAssets, type Project, type ProjectAsset } from "@/services/api/projects";
import { listAssetImageCandidateTasks, listGenerationTasks, queryGenerationTask, type GenerationTask } from "@/services/api/task-center";
import { parseBackendGenerationResult } from "@/services/api/generation-task";
import { ApiError } from "@/services/api/request";
import { resourceFileUrl, resourceStorageKey } from "@/services/api/resources";
import { uploadImage } from "@/services/image-storage";
import { loadAssetsForUse } from "@/services/user-data-sync";
import { useAssetStore } from "@/stores/use-asset-store";
import { useUserStore } from "@/stores/use-user-store";
import { configuredModelMatchesCapability, useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { modelCompatibilityError, resolveCompatibleModel, type ModelRequirements } from "@/lib/model-selection";
import { generateCharacterTurnaround } from "./project-character-media";
import { generateProjectVisualImage } from "./project-visual-asset";
import { currentAssetImageId, directCharacterRepresentations, nextCandidateCursor, referenceImageForAsset, referenceModelError, requireRemoteImage, type ReferenceSource } from "./project-reference-image";

type Props = { project: Project; asset?: ProjectAsset; folderId?: string; initialMode?: "direct" | "generate"; onClose: () => void; onSaved: () => void };
type SelectedImage = { resourceId: string; name: string };

export function ProjectAssetImageModal({ project, asset: initialAsset, folderId, initialMode = "generate", onClose, onSaved }: Props) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const userId = useUserStore((state) => state.user?.id);
    const config = useEffectiveConfig();
    const [asset, setAsset] = useState(initialAsset);
    const [category, setCategory] = useState<"character" | "environment" | "prop">(initialAsset?.category === "environment" || initialAsset?.category === "prop" ? initialAsset.category : "character");
    const [mode, setMode] = useState<"direct" | "generate" | "text">(initialMode);
    const [name, setName] = useState(initialAsset?.title || "");
    const [instructions, setInstructions] = useState(String(initialAsset?.character?.definition.appearance || initialAsset?.visual?.definition.prompt || ""));
    const [image, setImage] = useState<SelectedImage>();
    const [source, setSource] = useState<ReferenceSource>(!initialAsset || initialAsset.referenceImage ? "original" : currentAssetImageId(initialAsset) ? "current" : "none");
    const [turnaround, setTurnaround] = useState(false);
    const [picker, setPicker] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const working = useRef(false);
    const mounted = useRef(true);
    const owner = useRef(userId);
    const alive = () => mounted.current && owner.current === useUserStore.getState().user?.id;
    useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
    useEffect(() => { if (userId !== owner.current) onClose(); }, [userId, onClose]);
    const [model, setModel] = useState(() => project.defaultImageModel && configuredModelMatchesCapability(config, project.defaultImageModel, "image") ? project.defaultImageModel : config.imageModel || "");
    const hasReference = source !== "none";
    const requirements: ModelRequirements = { capability: "image", input: { textCount: 1, imageCount: hasReference ? 1 : 0, videoCount: 0, audioCount: 0, characterCount: 0 } };
    const routedModel = resolveCompatibleModel(config, model, requirements) || model;
    const modelError = !model ? "请选择图片模型" : referenceModelError(config, routedModel, hasReference) || modelCompatibilityError(config, routedModel, requirements);
    const candidateKey = ["project-asset-image-candidates", userId, project.id, asset?.id];
    const activeTasks = useQuery({
        queryKey: ["project-asset-image-active", userId, project.id, asset?.id],
        queryFn: async ({ signal }) => {
            const tasks = await listGenerationTasks(1, { projectId: project.id, activeOnly: true, assetId: asset!.id }, undefined, signal);
            // The backend scopes assetId before LIMIT; target-metadata matches need not expose clientContext.nodeId.
            return tasks.filter((task) => task.projectId === project.id && (task.status === "queued" || task.status === "running"));
        },
        enabled: Boolean(asset?.id && userId),
        refetchInterval: 2_000,
    });
    const activeTask = Boolean(activeTasks.data?.length);
    const activeUnavailable = Boolean(asset && (activeTasks.isPending || activeTasks.isError || activeTasks.isFetching));
    const previousActive = useRef({ assetId: asset?.id, active: false });
    useEffect(() => {
        if (previousActive.current.assetId === asset?.id && previousActive.current.active && activeTasks.isSuccess && !activeTask) {
            void queryClient.invalidateQueries({ queryKey: ["project-asset-image-candidates", userId, project.id, asset?.id] });
        }
        if (activeTasks.isSuccess) previousActive.current = { assetId: asset?.id, active: activeTask };
    }, [activeTask, activeTasks.isSuccess, asset?.id, project.id, queryClient, userId]);
    const candidates = useInfiniteQuery({
        queryKey: candidateKey,
        initialPageParam: undefined as string | undefined,
        queryFn: ({ pageParam, signal }) => listAssetImageCandidateTasks(project.id, { assetId: asset!.id, beforeTaskId: pageParam, signal }),
        getNextPageParam: nextCandidateCursor,
        enabled: Boolean(asset?.id && userId),
        refetchInterval: activeTask ? 2_000 : false,
    });
    const tasks = [...new Map((candidates.data?.pages.flat() || []).map((task) => [task.id, task])).values()];
    const personalImages = useAssetStore((state) => state.assets).filter((item) => item.kind === "image");
    const pickerItems: AssetLibraryPickerItem[] = personalImages.map((item) => ({ id: item.id, title: item.title, category: item.category || "image", kindLabel: "图片", asset: item }));

    const refreshAsset = async () => {
        const result = await listProjectAssets(project.id);
        const latest = result.assets.find((item) => item.id === asset?.id);
        if (alive() && latest) setAsset(latest);
        if (alive()) onSaved();
    };
    const saved = async (next: ProjectAsset) => {
        if (!alive()) return;
        setAsset(next);
        onSaved();
        void queryClient.invalidateQueries({ queryKey: ["asset-library"] });
        try { await loadAssetsForUse([next.id], { refresh: true }); }
        catch { if (alive()) message.warning("服务端已保存，但本地素材刷新失败，请刷新页面后继续使用"); }
    };
    const execute = async (action: () => Promise<void>) => {
        if (working.current) return;
        working.current = true;
        setBusy(true);
        setError("");
        try { await action(); }
        catch (reason) {
            if (!alive()) return;
            if (reason instanceof ApiError && reason.reason === "project_asset_version_changed") {
                setError("资产版本已变化，未覆盖当前成品；候选已保留。请检查最新资产，旧版本候选不能强制采用。");
                await refreshAsset().catch(() => { if (alive()) message.warning("最新资产读取失败，请刷新页面"); });
            } else setError(reason instanceof Error ? reason.message : "操作失败，请重试");
        } finally { working.current = false; if (alive()) setBusy(false); }
    };
    const create = async () => {
        if (!name.trim()) throw new Error("请输入资产名称");
        if (mode === "direct" && !image) throw new Error("请选择一张成品图片");
        if (mode === "generate" && !instructions.trim()) throw new Error("请填写文字要求");
        if (mode === "generate" && source === "original" && !image) throw new Error("请选择一张原始参考图");
        const input = { name: name.trim(), folderId, definition: category === "character" ? { appearance: instructions.trim() } : { description: instructions.trim(), prompt: instructions.trim() }, ...(mode === "generate" && source === "original" && image ? { referenceImage: { resourceId: image.resourceId } } : {}), ...(mode === "direct" && image ? { imageResourceId: image.resourceId } : {}) };
        const result = category === "character" ? await createProjectCharacter(project.id, { ...input, ...(input.imageResourceId ? { imageRole: turnaround ? "turnaround_sheet" as const : "portrait" as const } : {}) }) : await createProjectAssetEntity(project.id, { ...input, category });
        await saved(result.asset);
        if (alive()) setImage(undefined);
        return result.asset;
    };
    const generate = async (target: ProjectAsset) => {
        if (!target.primaryVersionId) throw new Error("资产版本未加载，请刷新");
        const reference = referenceImageForAsset(target, source);
        const generationConfig = { ...config, model: routedModel, imageModel: routedModel, count: "1" };
        if (modelError) throw new Error(modelError);
        if (!useConfigStore.getState().isAiConfigReady(generationConfig, routedModel)) throw new Error("请先配置可用的图片模型");
        const common = { projectId: project.id, config: generationConfig, projectStyle: resolveProjectCanvasStyle(project.stylePresetId, project.styleProfileJson), adoptionMode: "manual" as const, referenceSource: source, referenceImages: reference ? [reference] : [], onTaskCreated: () => { if (alive()) void queryClient.invalidateQueries({ queryKey: ["project-asset-image-active", owner.current, project.id, target.id] }); } };
        if (target.category === "character") await generateCharacterTurnaround({ ...common, assetId: target.id, versionId: target.primaryVersionId, name: target.title, definition: { ...target.character?.definition, multiViewPrompt: instructions.trim() } });
        else await generateProjectVisualImage({ ...common, asset: { ...target, visual: { status: target.visual?.status || "missing", definition: { ...target.visual?.definition, prompt: instructions.trim() } } } });
        if (alive()) {
            message.success("生成任务已完成，请预览候选并明确采用；当前成品未改变");
            await queryClient.invalidateQueries({ queryKey: ["project-asset-image-candidates", owner.current, project.id, target.id] });
        }
    };
    const submit = () => execute(async () => {
        if (mode === "generate" && (modelError || !instructions.trim())) throw new Error(modelError || "请填写文字要求");
        if (mode === "generate" && !useConfigStore.getState().isAiConfigReady({ ...config, model: routedModel, imageModel: routedModel }, routedModel)) throw new Error("请先配置可用的图片模型");
        if (mode === "generate" && activeUnavailable) throw new Error("请先成功读取该资产的活动任务，再提交新任务");
        if (mode === "generate" && activeTask) throw new Error("该资产已有生成中的任务，请等待完成");
        if (!asset) {
            const created = await create();
            if (mode === "generate" && alive()) await generate(created);
            else if (alive()) { message.success("资产已保存到服务端"); onClose(); }
            return;
        }
        if (mode === "direct") {
            if (!image || !asset.primaryVersionId) throw new Error("请选择成品图片并确认资产版本已加载");
            const result = asset.category === "character"
                ? await replaceProjectCharacterRepresentations(project.id, asset.id, directCharacterRepresentations(image.resourceId, turnaround), asset.primaryVersionId)
                : await bindProjectAssetImage(project.id, asset.id, { resourceId: image.resourceId, expectedVersionId: asset.primaryVersionId });
            await saved(result.asset);
            if (alive()) { setImage(undefined); message.success("成品图片已保存"); }
        } else {
            if (image && source === "original") throw new Error("请先保存所选原始参考图，再生成候选");
            await generate(asset);
        }
    });
    const saveReference = () => execute(async () => {
        if (!asset?.primaryVersionId || !image) throw new Error("请选择原始参考图并确认版本已加载");
        const result = await updateProjectAssetReferenceImage(project.id, asset.id, { resourceId: image.resourceId, expectedVersionId: asset.primaryVersionId });
        await saved(result.asset);
        if (alive()) { setImage(undefined); setSource("original"); message.success("原始参考图已保存，当前成品未改变"); }
    });
    const adopt = (task: GenerationTask) => execute(async () => {
        if (!asset?.primaryVersionId || !task.assetImageCandidate) throw new Error("候选或资产版本未加载，请刷新");
        const result = await adoptProjectAssetImageCandidate(project.id, asset.id, task.id, { expectedVersionId: asset.primaryVersionId });
        await saved(result.asset);
        if (alive()) { await candidates.refetch(); message.success("候选已采用为当前成品，原始参考图保持不变"); }
    });

    return <>
        <AppModal open flush title={null} footer={null} width={880} onCancel={onClose}>
            <div className="max-h-[85vh] overflow-y-auto p-6 text-foreground" data-canvas-no-zoom data-canvas-wheel-scroll>
                <h2 className="text-xl font-semibold">{asset ? `${asset.title} · 图片创作` : "新建资产"}</h2>
                <p className="mt-2 text-sm text-foreground/60">原始参考、生成候选与当前成品分别保存。生成不会自动替换成品。</p>
                <div className="mt-5 grid gap-5 sm:grid-cols-2">
                    <div className="grid content-start gap-4">
                        {!asset ? <><label className="grid gap-2">资产分类<Select aria-label="资产分类" value={category} onChange={setCategory} options={[{ value: "character", label: "角色" }, { value: "environment", label: "场景" }, { value: "prop", label: "道具" }]} disabled={busy} /></label><label className="grid gap-2">名称<Input aria-label="资产名称" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} disabled={busy} /></label></> : null}
                        <Radio.Group aria-label="图片用途" value={mode} onChange={(event) => { setMode(event.target.value); setImage(undefined); }} disabled={busy} options={[{ value: "direct", label: "直接使用成品" }, { value: "generate", label: "参考图生成" }, ...(!asset ? [{ value: "text", label: "仅创建设定" }] : [])]} />
                        {mode !== "text" ? <>
                            {mode === "generate" ? <Radio.Group aria-label="生成参考来源" value={source} disabled={busy} onChange={(event) => { setSource(event.target.value); setImage(undefined); }} options={[{ value: "original", label: "原始参考图" }, { value: "current", label: "当前成品", disabled: !asset || !currentAssetImageId(asset) }, { value: "none", label: "无参考（文生图）" }]} /> : null}
                            {(mode === "direct" || source === "original") ? <div className="grid gap-2"><Button disabled={busy} onClick={() => setPicker(true)}>上传或选择一张{mode === "direct" ? "成品图" : "原始参考图"}</Button>{image ? <><ImagePreview resourceId={image.resourceId} label={image.name} /><Button type="text" disabled={busy} onClick={() => setImage(undefined)}>取消本次选择</Button>{asset && mode === "generate" ? <Button loading={busy} onClick={saveReference}>保存原始参考图</Button> : null}</> : null}</div> : null}
                            {mode === "direct" && category === "character" ? <Checkbox checked={turnaround} disabled={busy} onChange={(event) => setTurnaround(event.target.checked)}>这张成品已明确包含角色三视图（默认普通人像）</Checkbox> : null}
                        </> : null}
                        <label className="grid gap-2">{mode === "generate" ? "文字要求" : "文字设定（可选）"}<Input.TextArea aria-label="文字要求" rows={4} value={instructions} onChange={(event) => setInstructions(event.target.value)} disabled={busy} /></label>
                        {mode === "generate" ? <><ModelPicker config={config} value={model} onChange={setModel} capability="image" requirements={requirements} fullWidth modelFilter={(value) => !referenceModelError(config, value, hasReference)} />{modelError ? <p className="text-sm text-foreground/60">{modelError}</p> : null}{category === "character" ? <p className="text-sm text-foreground/60">角色生成输出为一张三视图，不会拆分。普通照片请使用“直接使用成品”。</p> : null}</> : null}
                    </div>
                    <div className="grid content-start gap-4">
                        <ImagePreview resourceId={asset?.referenceImage?.resourceId} label="已保存的原始参考图" />
                        <ImagePreview resourceId={asset ? currentAssetImageId(asset) : undefined} label="当前成品" />
                    </div>
                </div>
                {error ? <p role="alert" className="mt-4 text-sm text-foreground">{error}</p> : null}
                <div className="mt-5 flex justify-end gap-2"><Button onClick={onClose}>关闭</Button><Button type="primary" loading={busy} disabled={mode === "generate" && (Boolean(modelError) || activeTask || activeUnavailable || !instructions.trim())} onClick={submit}>{mode === "text" ? "创建设定" : mode === "direct" ? "保存成品资产" : asset ? category === "character" ? "生成三视图候选" : "生成图片候选" : "创建并生成候选"}</Button></div>
                {asset ? <section className="mt-6 border-t border-border pt-4" aria-label="生成候选"><div className="flex justify-between"><h3 className="font-medium">生成候选</h3><Button size="small" disabled={busy} onClick={() => { void activeTasks.refetch(); void candidates.refetch(); void refreshAsset().catch(() => setError("资产读取失败，请重试")); }}>刷新</Button></div><p className="mt-2 text-sm text-foreground/60">关闭或刷新后可在这里恢复。只有明确采用才会更新成品。</p>
                    {activeTasks.isError ? <p role="alert">活动任务读取失败，暂不能生成。{activeTasks.error.message} 请刷新重试。</p> : activeTasks.isPending ? <p role="status">正在读取活动任务…</p> : activeTask ? <p role="status">该资产正在生成，完成后自动刷新候选。</p> : null}
                    {candidates.isError ? <p role="alert">候选读取失败，请刷新重试。</p> : null}
                    {!tasks.length ? <p className="py-4 text-sm text-foreground/60">{candidates.isLoading ? "正在读取候选…" : "还没有生成任务"}</p> : null}
                    <div className="mt-3 grid gap-4 sm:grid-cols-2">{tasks.map((task) => <CandidateCard key={task.id} task={task} versionId={asset.primaryVersionId} busy={busy} onAdopt={() => adopt(task)} />)}</div>
                    {candidates.hasNextPage ? <Button className="mt-4" loading={candidates.isFetchingNextPage} onClick={() => void candidates.fetchNextPage()}>加载更早的候选</Button> : null}
                </section> : null}
            </div>
        </AppModal>
        <AssetLibraryPickerModal open={picker} remoteLibrary remoteKind="image" acceptRemoteItems multiple={false} items={pickerItems} categoryLabels={{ all: "全部图片" }} title="选择单张图片" onClose={() => setPicker(false)} upload={{ accept: "image/*", description: "上传完成并持久化到服务端后才可使用", onUpload: async (files) => {
            if (files.length !== 1) throw new Error("一次只能选择一张图片");
            const uploaded = await uploadImage(files[0]);
            requireRemoteImage(uploaded);
            if (!alive()) throw new Error("账号或页面已切换，请重新选择");
            return [useAssetStore.getState().addAsset({ kind: "image", title: files[0].name, coverUrl: uploaded.url, tags: [], source: "项目参考图", data: { dataUrl: uploaded.url, storageKey: uploaded.storageKey, width: uploaded.width, height: uploaded.height, bytes: uploaded.bytes, mimeType: uploaded.mimeType } })];
        } }} onConfirm={(ids, items) => {
            if (ids.length !== 1) throw new Error("请选择一张图片");
            const selected = items?.find((item) => item.id === ids[0])?.asset;
            if (selected?.kind !== "image") throw new Error("所选图片未加载，请重新选择");
            const resourceId = requireRemoteImage(selected.data);
            setImage({ resourceId, name: selected.title });
            setPicker(false);
        }} />
    </>;
}

function ImagePreview({ resourceId, label }: { resourceId?: string; label: string }) {
    return <figure className="rounded-lg border border-border bg-surface-active p-3"><figcaption className="mb-2 text-sm text-foreground/60">{label}</figcaption>{resourceId ? <CachedResourceImage storageKey={resourceStorageKey(resourceId)} src={resourceFileUrl(resourceId)} alt={label} className="max-h-60 w-full object-contain" /> : <p className="py-8 text-center text-sm text-foreground/45">尚未设置</p>}</figure>;
}

function CandidateCard({ task, versionId, busy, onAdopt }: { task: GenerationTask; versionId?: string; busy: boolean; onAdopt: () => void }) {
    const [preview, setPreview] = useState(task.previewUrl || "");
    const [previewError, setPreviewError] = useState("");
    const candidate = task.assetImageCandidate;
    const adopted = candidate?.state === "adopted";
    const stale = !adopted && Boolean(candidate && candidate.baseVersionId !== versionId);
    useEffect(() => {
        if (task.previewUrl) { setPreview(task.previewUrl); return; }
        if (task.status !== "succeeded") return;
        const controller = new AbortController();
        void queryGenerationTask(task.id, { signal: controller.signal }).then((detail) => {
            const image = parseBackendGenerationResult(detail).images?.[0];
            if (!controller.signal.aborted && image?.resourceId) setPreview(resourceFileUrl(image.resourceId));
        }).catch(() => { if (!controller.signal.aborted) setPreviewError("预览读取失败，可刷新重试"); });
        return () => controller.abort();
    }, [task.id, task.previewUrl, task.status]);
    const label = adopted ? "已采用" : stale ? "版本已过期" : task.status === "succeeded" ? "待采用" : task.status === "failed" ? "生成失败" : task.status === "cancelled" ? "已取消" : "生成中";
    return <article className="rounded-lg border border-border p-3">{preview ? <img src={preview} alt="生成候选" className="max-h-64 w-full object-contain" /> : <p className="py-6 text-sm text-foreground/60">{previewError || label}</p>}<div className="mt-3 flex items-center justify-between gap-2"><span className="text-sm">{label}</span><Button disabled={busy || task.status !== "succeeded" || !candidate || adopted || stale} onClick={onAdopt}>采用为当前成品</Button></div>{stale ? <p className="mt-2 text-sm text-foreground/60">资产已被修改，候选保留但不能覆盖新版本。</p> : null}{task.error ? <p className="mt-2 text-sm">{task.error}</p> : null}</article>;
}
