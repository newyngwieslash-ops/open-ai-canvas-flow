import { resolveProjectCanvasStyle } from "@/components/canvas/canvas-style-picker-modal";
import type { ProjectCore, ProjectDetail, ProjectUnitWorkspace, ShotRevision } from "@/services/api/projects";
import type { Skill } from "@/services/api/skills";
import { http } from "@/services/api/request";
import { resourceIdFromStorageKey } from "@/services/api/resources";
import type { FlowExportBundle, FlowExportMedia } from "@/services/flow-bridge";
import { skillRuntime, SKILL_RUNTIME_PROFILES } from "@/services/skill-runtime";
import { buildShotAssetReferenceContext } from "./workflow-shot-references";

type ExportOptions = {
    unitId: string;
    shotId?: string;
    editorDirty: boolean;
    origin: string;
    resolution: string;
};

const artifactLabels = {
    storyboard: "分镜图",
    action_board: "动作预演",
    start_frame: "首帧",
    end_frame: "尾帧",
} as const;

export async function loadWorkflowFlowBundle(options: ExportOptions & { projectId: string; signal?: AbortSignal }) {
    if (options.editorDirty) throw new Error("当前镜头有未保存修改，请先保存脚本再提交到 Flow");
    const projectPath = `/projects/${encodeURIComponent(options.projectId)}`;
    const [core, workspace] = await Promise.all([
        http.get<ProjectCore>(`${projectPath}/core`, { signal: options.signal }),
        http.get<ProjectUnitWorkspace>(`${projectPath}/units/${encodeURIComponent(options.unitId)}/workspace`, { signal: options.signal }),
    ]);
    options.signal?.throwIfAborted();
    if (core.project.id !== options.projectId || workspace.unit.projectId !== options.projectId || workspace.unit.id !== options.unitId) {
        throw new Error("服务器返回的项目或章节与当前页面不一致");
    }
    return buildWorkflowFlowBundle({ ...workspace, project: core.project, units: [workspace.unit], canvases: [], canvasUnitLinks: [], assetFolders: [] }, options);
}

export function buildWorkflowFlowBundle(detail: ProjectDetail, options: ExportOptions): FlowExportBundle {
    if (options.editorDirty) throw new Error("当前镜头有未保存修改，请先保存脚本再提交到 Flow");
    const unit = detail.units.find(item => item.id === options.unitId && item.projectId === detail.project.id);
    if (!unit) throw new Error("未找到当前项目章节");
    const origin = new URL(options.origin);
    if (!/^https?:$/.test(origin.protocol) || origin.origin !== options.origin) throw new Error("导出来源必须是当前页面的 HTTP/HTTPS origin");
    const shots = (detail.shots || []).filter(shot => shot.projectId === detail.project.id && shot.unitId === unit.id && (!options.shotId || shot.id === options.shotId))
        .slice().sort((left, right) => left.position - right.position);
    if (!shots.length) throw new Error(options.shotId ? "所选镜头不属于当前章节" : "当前章节没有可提交的镜头");
    const style = resolveProjectCanvasStyle(detail.project.stylePresetId, detail.project.styleProfileJson);
    const mediaById = new Map<string, FlowExportMedia>();
    const bundle: FlowExportBundle = {
        version: 1,
        id: crypto.randomUUID(),
        createdAt: new Date().toISOString(),
        source: { origin: origin.origin, projectId: detail.project.id, unitId: unit.id, projectTitle: detail.project.name, unitTitle: unit.title },
        shots: [],
        media: [],
    };
    for (const shot of shots) {
        const sourceRevision = (detail.shotRevisions || []).find(item => item.id === shot.currentRevisionId && item.shotId === shot.id);
        if (!sourceRevision) throw new Error(`“${shot.title}”缺少已保存的当前脚本版本，请先保存或刷新`);
        const revision = copyRevision(sourceRevision);
        if (!revision.videoPrompt.trim() && !revision.plotDescription.trim()) throw new Error(`“${shot.title}”缺少视频提示词和镜头画面`);
        if (!Number.isFinite(revision.durationMs) || revision.durationMs <= 0) throw new Error(`“${shot.title}”的镜头时长无效`);
        const warnings: string[] = [];
        const shotMediaIds = new Set<string>();
        const addMedia = (media: Omit<FlowExportMedia, "shotIds">) => {
            if (!media.id?.trim()) throw new Error(`“${media.name}”没有可导出的服务端资源，请先保存资产`);
            const existing = mediaById.get(media.id);
            if (existing) {
                existing.roles = Array.from(new Set([...existing.roles, ...media.roles]));
                if (!existing.shotIds.includes(shot.id)) existing.shotIds.push(shot.id);
                for (const key of ["assetId", "assetVersionId", "artifactId"] as const) {
                    if (!existing[key]) existing[key] = media[key];
                    else if (media[key] && existing[key] !== media[key]) {
                        // One binary can have multiple source identities; retain the extra mapping in the shot's review notes.
                        warnings.push(`共用文件 ${media.id}：${media.name}，${key}=${media[key]}（主记录 ${key}=${existing[key]}）`);
                    }
                }
                if (media.status === "stale") existing.status = "stale";
            } else mediaById.set(media.id, { ...media, shotIds: [shot.id] });
            shotMediaIds.add(media.id);
        };

        const references = (detail.shotReferences || []).filter(item => item.shotId === shot.id && item.status === "linked");
        const assetNames = new Map<string, string>();
        const referenceDescriptions: string[] = [];
        for (const reference of references) {
            const asset = reference.asset || (detail.assets || []).find(item => item.id === reference.referencedVersion?.assetId || item.primaryVersionId === reference.assetVersionId);
            if (!asset) throw new Error(`“${shot.title}”的资产引用 ${reference.id} 无法解析，请刷新或重新绑定`);
            const versionId = reference.referencedVersion?.id || reference.assetVersionId;
            if (!versionId) throw new Error(`“${asset.title}”缺少绑定版本，无法完整导出`);
            if (reference.referencedVersion && (reference.referencedVersion.id !== reference.assetVersionId || reference.referencedVersion.assetId !== asset.id)) {
                throw new Error(`“${asset.title}”的绑定版本信息不一致，请刷新或重新绑定`);
            }
            if (!reference.referencedVersion && (asset.character?.versionId || asset.primaryVersionId) !== versionId) {
                throw new Error(`“${asset.title}”缺少绑定的历史版本 ${versionId} 详情，不能用当前版本代替；请刷新或重新绑定`);
            }
            assetNames.set(asset.id, asset.title);
            const context = buildShotAssetReferenceContext({ ...detail, assets: detail.assets || [], shotReferences: [{ ...reference, asset }] }, shot.id);
            const primaryImage = context.referenceImages[0];
            const preferredId = resourceIdFromStorageKey(primaryImage?.storageKey);
            const mediaRoles = [reference.role, asset.category];
            const linkedMedia = new Set<string>();
            const addAssetMedia = (id: string, name: string, roles: string[]) => {
                addMedia({ id, name, roles, assetId: asset.id, assetVersionId: versionId, status: reference.status });
                linkedMedia.add(id);
            };
            if (primaryImage) addAssetMedia(preferredId || "", asset.title, mediaRoles);
            const representations = reference.referencedVersion ? (reference.referencedVersion.representations || []) : (asset.character?.representations || []);
            for (const representation of representations) {
                if (linkedMedia.has(representation.resourceId)) continue;
                addAssetMedia(representation.resourceId, `${asset.title}-${representation.role}`, ["reference", asset.category, representation.role]);
            }
            const referenceImageId = reference.referencedVersion?.referenceImage?.resourceId || (!reference.referencedVersion ? asset.referenceImage?.resourceId : undefined);
            if (referenceImageId && !linkedMedia.has(referenceImageId)) addAssetMedia(referenceImageId, `${asset.title}-参考图`, ["reference", asset.category]);
            if (!primaryImage && /^(audio|video)(\/|$)/.test(asset.mediaType)) {
                addAssetMedia(resourceIdFromStorageKey(asset.storageKey) || "", asset.title, mediaRoles);
            }
            const voiceResourceId = asset.character?.voice?.profile.sampleResourceId;
            if (voiceResourceId) {
                addMedia({ id: voiceResourceId, name: `${asset.title}-声音样本`, roles: ["voice_reference", "audio"], assetId: asset.id, assetVersionId: asset.character!.versionId, status: reference.status });
                linkedMedia.add(voiceResourceId);
                warnings.push(`“${asset.title}”声音样本来自当前角色声音配置（版本 ${asset.character!.versionId}），Flow 是否接受声音参考需在插件中确认`);
            }
            if (!linkedMedia.size) throw new Error(`“${asset.title}”没有可导出的媒体文件，请先补全绑定资产`);
            const voiceDescription = context.assetReferences[0]?.voiceDescription;
            referenceDescriptions.push(`- ${asset.title}：${Array.from(linkedMedia, id => `文件“${mediaById.get(id)!.name}”`).join("、")}${voiceDescription ? `；声音画像：${voiceDescription}` : ""}`);
        }

        for (const [type, label] of Object.entries(artifactLabels)) {
            const artifacts = (detail.shotArtifacts || []).filter(item => item.projectId === detail.project.id && item.shotId === shot.id && item.type === type)
                .slice().sort((left, right) => right.version - left.version);
            const selected = artifacts.find(item => item.selected);
            const latest = artifacts[0];
            for (const artifact of Array.from(new Set([selected, latest])).filter(item => item !== undefined)) {
                const artifactLabel = `${shot.title}-${label}-v${artifact.version}`;
                if (!artifact.resourceId) {
                    warnings.push(`${artifactLabel}尚无可导出的文件（${artifact.status}）`);
                    continue;
                }
                const stale = artifact.status === "stale" || Boolean(artifact.revisionId && artifact.revisionId !== revision.id);
                addMedia({
                    id: artifact.resourceId, name: artifactLabel,
                    roles: [type, ...(artifact === selected ? ["selected"] : []), ...(artifact === latest ? ["latest"] : [])],
                    artifactId: artifact.id, status: stale ? "stale" : artifact.status,
                });
                if (stale) warnings.push(`${artifactLabel}已过期或来自旧脚本版本 ${artifact.revisionId || "未知"}；已保留，请在插件中核对`);
                else if (artifact.status !== "ready") warnings.push(`${artifactLabel}状态为 ${artifact.status}，请在插件中核对`);
            }
            if (artifacts.length && (type === "storyboard" || type === "action_board")) warnings.push(`${label}可能含多格画面，应在插件中选择用途，不能直接当作单张首帧`);
        }
        if (!shotMediaIds.size) warnings.push("此镜头没有媒体参考，将仅携带完整文本脚本");
        const beatText = readableActionBeats(revision.actionBeatsJson, warnings);
        const section = (label: string, value: string) => value.trim() ? `【${label}】\n${value.trim()}` : "";
        const prompt = [
            section("视频提示词", revision.videoPrompt || revision.plotDescription),
            section("镜头画面", revision.plotDescription),
            section("表演与动作", revision.action),
            section("对白 / 旁白", revision.dialogue),
            section("镜头语言", [revision.shotSize && `景别：${revision.shotSize}`, revision.cameraAngle && `机位：${revision.cameraAngle}`, revision.cameraMovement && `运镜：${revision.cameraMovement}`].filter(Boolean).join("\n")),
            section("时长", `${revision.durationMs / 1000} 秒`),
            section("项目画风", style ? `${style.title}\n${style.prompt}` : ""),
            section("画面提示词", revision.imagePrompt),
            section("动作节拍", beatText),
            section("排除内容", revision.negativePrompt),
            section("接戏备注", revision.continuityNotes),
            section("资产参考", referenceDescriptions.join("\n")),
        ].filter(Boolean).join("\n\n").replace(/@\[asset:([^\]]+)\]/g, (_, id: string) => {
            const name = assetNames.get(id);
            if (!name) throw new Error(`“${shot.title}”的提示词引用了未绑定资产 ${id}，请先重新绑定或修改脚本`);
            return name;
        });
        bundle.shots.push({
            id: shot.id, title: shot.title, position: shot.position,
            revisionId: revision.id, revision, prompt, durationMs: revision.durationMs,
            settings: { aspectRatio: detail.project.aspectRatio || "16:9", resolution: options.resolution },
            mediaIds: Array.from(shotMediaIds), warnings: Array.from(new Set(warnings)),
        });
    }
    bundle.media = Array.from(mediaById.values());
    return bundle;
}

export async function applyWorkflowFlowSkills(bundle: FlowExportBundle, options: { skills: Skill[]; selectedSkillIds: string[]; signal?: AbortSignal }) {
    const selectedIds = Array.from(new Set(options.selectedSkillIds));
    if (!selectedIds.length) return bundle;
    if (selectedIds.length > SKILL_RUNTIME_PROFILES.shortDrama.maxSkills) throw new Error(`Flow 导出最多使用 ${SKILL_RUNTIME_PROFILES.shortDrama.maxSkills} 个技能，请调整选择`);
    const missing = selectedIds.filter(id => !options.skills.some(skill => skill.skillId === id && skill.isAdded));
    if (missing.length) throw new Error("所选技能尚未加载或已不可用，请刷新技能列表后重试");
    const shots: FlowExportBundle["shots"] = [];
    for (const shot of bundle.shots) {
        options.signal?.throwIfAborted();
        const prepared = await skillRuntime.prepare({ profile: "shortDrama", prompt: shot.prompt, skills: options.skills, selectedSkillIds: selectedIds });
        options.signal?.throwIfAborted();
        if (selectedIds.some(id => !prepared.provenance.skillIds.includes(id))) throw new Error("所选技能未完整编入提示词，已停止提交");
        const warnings = [...shot.warnings, "已将当前所选技能编入视频提示词；技能文件遵循项目现有上下文预算"];
        for (const version of prepared.provenance.skillVersions) {
            warnings.push(`技能 ${version.skillId}：版本 ${version.version}，版本 ID ${version.versionId}`);
        }
        for (const file of prepared.provenance.skillFiles) {
            if (file.sha256 && /^[a-f0-9]{64}$/i.test(file.sha256)) warnings.push(`技能文件摘要 ${file.skillId}：SHA-256 ${file.sha256}`);
        }
        shots.push({ ...shot, prompt: prepared.prompt, warnings });
    }
    return { ...bundle, shots };
}

function readableActionBeats(value: string, warnings: string[]) {
    if (!value.trim()) return "";
    try {
        const beats: unknown = JSON.parse(value);
        if (!Array.isArray(beats)) throw new Error("Expected an array");
        return beats.map((beat, index) => `${index + 1}. ${typeof beat === "string" ? beat : JSON.stringify(beat)}`).join("\n");
    } catch {
        warnings.push("动作节拍不是有效数组，已保留原文，请在插件中核对");
        return value;
    }
}

function copyRevision(value: ShotRevision): ShotRevision {
    // Explicit fields keep unrelated task inputs, credentials and runtime metadata out of the bridge.
    return {
        id: value.id, shotId: value.shotId, version: value.version,
        plotDescription: value.plotDescription, action: value.action, dialogue: value.dialogue,
        shotSize: value.shotSize, cameraAngle: value.cameraAngle, cameraMovement: value.cameraMovement,
        durationMs: value.durationMs, imagePrompt: value.imagePrompt, videoPrompt: value.videoPrompt,
        negativePrompt: value.negativePrompt, continuityNotes: value.continuityNotes,
        actionBeatsJson: value.actionBeatsJson, createdAt: value.createdAt,
        ...(value.createdBy ? { createdBy: value.createdBy } : {}),
    };
}
