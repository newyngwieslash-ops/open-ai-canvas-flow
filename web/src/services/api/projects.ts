import { compactApiParams, http } from "@/services/api/request";
import { normalizeAssetCategory, type AssetCategory } from "@/lib/asset-category";
import type { GenerationTask } from "@/services/api/task-center";


export type BindingCategory = "character" | "environment" | "prop";
export type BindingEvidence = {
    kind: "structured_id" | "exact_name" | "visual_label" | "structured_continuity" | "mention" | "speaker";
    field: string;
    start: number;
    end: number;
    text: string;
};
export type BindingIssue = {
    kind: "invalid_id" | "category_mismatch" | "not_allowed" | "ambiguous" | "unmatched" |
        "missing_version" | "missing_image" | "capacity_exceeded" | "context_warning";
    message: string;
};
export type BindingCandidate = {
    assetId: string;
    category: BindingCategory;
    name: string;
    assetVersionId: string | null;
    evidence: BindingEvidence[];
    issues: BindingIssue[];
    bindable: boolean;
    missingImage: boolean;
    defaultSelected: boolean;
};
export type BindingReference = { referenceId: string; assetVersionId: string; role: string; status: string };
export type BindingArtifact = { artifactId: string; contentFingerprint: string; status: string; selected: boolean };
export type BindingShotBaseline = {
    shotId: string;
    shotFingerprint: string;
    revisionFingerprint: string;
    references: BindingReference[];
    artifacts: BindingArtifact[];
};
export type BindingVersionBaseline = {
    assetVersionId: string;
    versionFingerprint: string;
    mediaFingerprint: string;
    bindable: boolean;
    missingImage: boolean;
};
export type BindingCatalogBaseline = {
    assetId: string;
    assetFingerprint: string;
    linkFingerprint: string;
    versions: BindingVersionBaseline[];
};
export type BindingBaseline = {
    projectId: string;
    unitId: string;
    projectRevision: number;
    shotIds: string[];
    shots: BindingShotBaseline[];
    catalog: BindingCatalogBaseline[];
};
export type BindingPreviewRequest = { shotIds: string[] };
export type BindingShotPreview = {
    shotId: string;
    existingReferences: BindingReference[];
    candidates: BindingCandidate[];
    issues: BindingIssue[];
    existingVersionCount: number;
    remainingCapacity: number;
};
export type BindingPreview = { baseline: BindingBaseline; shots: BindingShotPreview[] };

export function previewProjectUnitShotAssetBindings(projectId: string, unitId: string, input: BindingPreviewRequest, signal?: AbortSignal): Promise<BindingPreview> {
    return http.post<BindingPreview>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}/shot-asset-bindings/preview`, input, { signal });
}

export type Project = {
    id: string;
    userId: string;
    name: string;
    type: string;
    aspectRatio: string;
    sourceType: string;
    description: string;
    coverResourceId?: string;
    stylePresetId: string;
    styleProfileJson?: string;
    defaultImageModel?: string;
    defaultVideoModel?: string;
    status: "active" | "archived" | string;
    revision: number;
    createdAt: string;
    updatedAt: string;
};

export type ProjectCanvas = {
    id: string;
    projectId?: string;
    title: string;
    createdAt: string;
    updatedAt: string;
};

export type CanvasUnitLink = {
    id: string;
    projectId: string;
    canvasId: string;
    unitId: string;
    role: string;
    createdAt: string;
};

export type ProjectUnit = {
    id: string;
    projectId: string;
    kind: "chapter" | "episode" | string;
    title: string;
    sourceText: string;
    wordCount: number;
    status: "draft" | "ready" | "completed" | string;
    position: number;
    createdAt: string;
    updatedAt: string;
};

export type ProjectAsset = {
    id: string;
    title: string;
    mediaType: string;
    category: AssetCategory;
    status: string;
    primaryVersionId?: string;
    versionCount: number;
    usages: string[];
    folderId?: string;
    position: number;
    storageKey?: string;
    durationMs?: number;
    previewText?: string;
    updatedAt: string;
    character?: CharacterCardSummary;
    visual?: { definition: Record<string, unknown>; status: "missing" | "ready" };
    source?: string;
    referenceImages?: Array<{ resourceId: string }>;
    referenceImage?: { resourceId: string };
};

export type ProjectAssetFolder = {
    id: string;
    projectId: string;
    parentId?: string;
    name: string;
    style: "glass" | "stacked" | "midnight" | "paper" | "cinema" | "compact" | string;
    theme: "aurora" | "obsidian" | "ember" | "pearl" | string;
    position: number;
    createdAt: string;
    updatedAt: string;
};

export type CharacterRepresentation = {
    id: string;
    resourceId: string;
    mediaType: string;
    role: "primary" | "front" | "side" | "back" | "turnaround_sheet" | "expression_sheet" | string;
};

export type VoiceProfile = {
    id: string;
    name: string;
    provider: string;
    voiceKey: string;
    language: string;
    timbre: string;
    sampleResourceId?: string;
    compatibleModels: string[];
    status: string;
};

export type CharacterCardSummary = {
    versionId: string;
    version: number;
    definition: Record<string, unknown>;
    representations: CharacterRepresentation[];
    voice?: { profile: VoiceProfile; instructions: string };
    visualStatus: "missing" | "partial" | "ready" | string;
    voiceStatus: "missing" | "ready" | "unavailable" | string;
};

export type ProjectCharacterDetail = {
    asset: ProjectAsset;
    character: CharacterCardSummary;
};

export type ProjectAssetCandidate = {
    id: string;
    projectId: string;
    unitId?: string;
    shotId?: string;
    name: string;
    category: AssetCategory;
    status: "pending_confirmation" | "confirmed" | "ignored" | string;
    detailsJson: string;
    resolvedAssetId?: string;
    createdAt: string;
    updatedAt: string;
};

export type ProjectShot = {
    id: string;
    projectId: string;
    unitId?: string;
    currentRevisionId?: string;
    title: string;
    description: string;
    position: number;
    durationMs: number;
    status: string;
    createdAt: string;
    updatedAt: string;
};

export type ShotRevision = {
    id: string;
    shotId: string;
    version: number;
    plotDescription: string;
    action: string;
    dialogue: string;
    shotSize: string;
    cameraAngle: string;
    cameraMovement: string;
    durationMs: number;
    imagePrompt: string;
    videoPrompt: string;
    negativePrompt: string;
    continuityNotes: string;
    actionBeatsJson: string;
    createdBy?: string;
    createdAt: string;
};

export type ShotArtifact = {
    id: string;
    projectId: string;
    unitId?: string;
    shotId: string;
    revisionId?: string;
    taskId?: string;
    type: "storyboard" | "action_board" | "start_frame" | "end_frame" | "video" | "audio" | "subtitle" | "delivery" | string;
    version: number;
    resourceId?: string;
    status: "pending" | "running" | "ready" | "failed" | "stale" | string;
    selected: boolean;
    metadataJson: string;
    createdAt: string;
    updatedAt: string;
};

export type ShotRevisionInput = {
    plotDescription: string;
    action?: string;
    dialogue?: string;
    shotSize?: string;
    cameraAngle?: string;
    cameraMovement?: string;
    durationMs?: number;
    imagePrompt?: string;
    videoPrompt?: string;
    negativePrompt?: string;
    continuityNotes?: string;
    actionBeats?: Array<Record<string, unknown>>;
};

export type ShotAssetReference = {
    id: string;
    shotId: string;
    assetVersionId: string;
    role: "reference" | "start_frame" | "end_frame" | "keyframe" | "storyboard" | "output" | string;
    status: string;
    createdAt: string;
    asset?: ProjectAsset;
    referencedVersion?: {
        referenceImages?: Array<{ resourceId: string }>;
        referenceImage?: { resourceId: string };
        id: string;
        assetId: string;
        version: number;
        representations: CharacterRepresentation[];
    };
};

export type WorkflowStep = {
    id: string;
    workflowInstanceId: string;
    stepKey: string;
    name: string;
    position: number;
    status: "pending" | "ready" | "running" | "review" | "completed" | "failed" | "skipped" | string;
    error?: string;
    updatedAt: string;
};

export type ProjectWorkflow = {
    instance: { id: string; projectId: string; unitId?: string; scope: string; status: string; revision: number };
    steps: WorkflowStep[];
};

export type ProjectSummary = {
    project: Project;
    canvasCount: number;
    assetCount: number;
    unitCount: number;
    completedUnitCount: number;
};

export type ProjectListPage = {
    projects: ProjectSummary[];
    page: number;
    pageSize: number;
    total: number;
    hasMore: boolean;
};

export type ProjectDetail = {
    project: Project;
    units: ProjectUnit[];
    canvases: ProjectCanvas[];
    canvasUnitLinks: CanvasUnitLink[];
    unitCanvasCounts?: Record<string, number>;
    assets: ProjectAsset[];
    assetFolders: ProjectAssetFolder[];
    workflows: ProjectWorkflow[];
    shots: ProjectShot[];
    shotRevisions: ShotRevision[];
    shotArtifacts: ShotArtifact[];
    shotReferences: ShotAssetReference[];
	assetCandidates: ProjectAssetCandidate[];
	tasks: GenerationTask[];
};

export type ProjectCore = { project: Project };

export type ProjectOverviewMetrics = {
    unitCount: number;
    completedUnitCount: number;
    totalWordCount: number;
    unitsWithoutText: number;
    unitsWithoutShots: number;
    canvasCount: number;
    assetCount: number;
    shotCount: number;
    pendingCandidateCount: number;
    readyStoryboardCount: number;
    readyPrevizCount: number;
    readyVideoCount: number;
    renderSucceededCount: number;
    staleArtifactCount: number;
};

export type ProjectOverviewUnit = {
    unit: ProjectUnit;
    shotCount: number;
    candidateCount: number;
    canvasCount: number;
};

export type ProjectOverview = { metrics: ProjectOverviewMetrics; units: ProjectOverviewUnit[] };

export type ProjectUnitWorkspace = {
    unit: ProjectUnit;
    workflows: ProjectWorkflow[];
    shots: ProjectShot[];
    shotRevisions: ShotRevision[];
    shotArtifacts: ShotArtifact[];
    shotReferences: ShotAssetReference[];
    assetCandidates: ProjectAssetCandidate[];
    assets: ProjectAsset[];
    tasks: GenerationTask[];
};

export type ProjectCanvasPage = {
    canvases: ProjectCanvas[];
    canvasUnitLinks: CanvasUnitLink[];
    page: number;
    pageSize: number;
    total: number;
    hasMore: boolean;
};

export type ProjectAssetPage = {
    assets: ProjectAsset[];
    categoryCounts: Record<string, number>;
    folderCounts: Record<string, number>;
    page: number;
    pageSize: number;
    total: number;
    hasMore: boolean;
};

export type ProjectAssetCandidatePage = {
    candidates: ProjectAssetCandidate[];
    page: number;
    pageSize: number;
    total: number;
    hasMore: boolean;
};

export function listProjects(): Promise<{ projects: ProjectSummary[] }>;
export function listProjects(params: { page: number; pageSize: number }): Promise<ProjectListPage>;
export function listProjects(params?: { page: number; pageSize: number }) {
    return http.get<{ projects: ProjectSummary[] } | ProjectListPage>("/projects", params ? { params: { page: params.page, pageSize: params.pageSize } } : undefined);
}

export function getProject(id: string) {
    return http.get<ProjectDetail>(`/projects/${encodeURIComponent(id)}`).then(normalizeProjectDetail);
}

export function getProjectCore(id: string) {
    return http.get<ProjectCore>(`/projects/${encodeURIComponent(id)}/core`);
}

export function listProjectUnits(projectId: string, config?: { signal?: AbortSignal }) {
    return http.get<{ units: ProjectUnit[]; canvasCounts: Record<string, number> }>(`/projects/${encodeURIComponent(projectId)}/units`, config);
}

export function getProjectOverview(projectId: string) {
    return http.get<ProjectOverview>(`/projects/${encodeURIComponent(projectId)}/overview`);
}

export function getProjectUnitWorkspace(projectId: string, unitId: string) {
    return http.get<ProjectUnitWorkspace>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}/workspace`);
}

export function listProjectCanvases(projectId: string, page = 1, pageSize = 40) {
    return http.get<ProjectCanvasPage>(`/projects/${encodeURIComponent(projectId)}/canvases`, { params: { page, pageSize } });
}

export function listProjectAssetsPage(projectId: string, options: { page?: number; pageSize?: number; category?: string; mediaType?: string; status?: string; folderId?: string; query?: string; signal?: AbortSignal } = {}, signal?: AbortSignal) {
    return http.get<ProjectAssetPage>(`/projects/${encodeURIComponent(projectId)}/assets`, { params: {
        page: options.page || 1,
        pageSize: options.pageSize || 40,
        category: options.category || undefined,
        mediaType: options.mediaType || undefined,
        status: options.status || undefined,
        folderId: options.folderId,
        q: options.query || undefined,
    }, signal: signal || options.signal }).then((page) => ({ ...page, assets: page.assets.map(normalizeProjectAsset) }));
}

export function listProjectAssets(projectId: string) {
    return http.get<{ assets: ProjectAsset[] }>(`/projects/${encodeURIComponent(projectId)}/assets`).then((result) => ({ assets: result.assets.map(normalizeProjectAsset) }));
}

export function listProjectAssetCandidates(projectId: string, options: { page?: number; pageSize?: number; unitId?: string; status?: string; category?: string; query?: string } = {}) {
    return http.get<ProjectAssetCandidatePage>(`/projects/${encodeURIComponent(projectId)}/asset-candidates`, { params: {
        page: options.page || 1,
        pageSize: options.pageSize || 100,
        unitId: options.unitId || undefined,
        status: options.status || undefined,
        category: options.category || undefined,
        q: options.query || undefined,
    } }).then((page) => ({ ...page, candidates: page.candidates.map(normalizeProjectAssetCandidate) }));
}

function normalizeProjectAsset(asset: ProjectAsset): ProjectAsset {
    return { ...asset, category: normalizeAssetCategory(asset.category), usages: Array.isArray(asset.usages) ? asset.usages : [] };
}

function normalizeProjectAssetCandidate(candidate: ProjectAssetCandidate): ProjectAssetCandidate {
    return { ...candidate, category: normalizeAssetCategory(candidate.category) };
}

function normalizeProjectDetail(detail: ProjectDetail): ProjectDetail {
    const workflows = Array.isArray(detail.workflows)
        ? detail.workflows.map((workflow) => ({
            ...workflow,
            steps: Array.isArray(workflow.steps) ? workflow.steps : [],
        }))
        : [];
    const assets = Array.isArray(detail.assets)
        ? detail.assets.map((asset) => ({
            ...normalizeProjectAsset(asset),
            ...(asset.character ? {
                character: {
                    ...asset.character,
                    representations: Array.isArray(asset.character.representations) ? asset.character.representations : [],
                },
            } : {}),
        }))
        : [];

    return {
        ...detail,
        units: Array.isArray(detail.units) ? detail.units : [],
        canvases: Array.isArray(detail.canvases) ? detail.canvases : [],
        canvasUnitLinks: Array.isArray(detail.canvasUnitLinks) ? detail.canvasUnitLinks : [],
        unitCanvasCounts: detail.unitCanvasCounts || {},
        assets,
        assetFolders: Array.isArray(detail.assetFolders) ? detail.assetFolders : [],
        workflows,
        shots: Array.isArray(detail.shots) ? detail.shots : [],
        shotRevisions: Array.isArray(detail.shotRevisions) ? detail.shotRevisions : [],
        shotArtifacts: Array.isArray(detail.shotArtifacts) ? detail.shotArtifacts : [],
        shotReferences: Array.isArray(detail.shotReferences) ? detail.shotReferences : [],
		assetCandidates: Array.isArray(detail.assetCandidates) ? detail.assetCandidates.map(normalizeProjectAssetCandidate) : [],
		tasks: Array.isArray(detail.tasks) ? detail.tasks : [],
    };
}

export function createProject(input: { name: string; type: string; aspectRatio: string; sourceType: string; description?: string; stylePresetId?: string; styleProfileJson?: string; defaultImageModel?: string; defaultVideoModel?: string }) {
    return http.post<{ project: Project }>("/projects", input);
}

export function updateProject(projectId: string, input: Partial<Pick<Project, "name" | "type" | "aspectRatio" | "sourceType" | "description" | "coverResourceId" | "stylePresetId" | "styleProfileJson" | "defaultImageModel" | "defaultVideoModel" | "status">>) {
    return http.patch<{ project: Project }>(`/projects/${encodeURIComponent(projectId)}`, input);
}

export function deleteProject(projectId: string) {
    return http.delete<{ id: string }>(`/projects/${encodeURIComponent(projectId)}`);
}

export function createProjectUnit(projectId: string, input: { kind: string; title: string; sourceText?: string; position?: number }) {
    return http.post<{ unit: ProjectUnit }>(`/projects/${encodeURIComponent(projectId)}/units`, input);
}

export function getProjectUnit(projectId: string, unitId: string, config?: { signal?: AbortSignal }) {
    return http.get<{ unit: ProjectUnit }>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}`, config);
}

export function importProjectUnits(projectId: string, units: Array<{ kind: string; title: string; sourceText?: string }>) {
    return http.post<{ units: ProjectUnit[] }>(`/projects/${encodeURIComponent(projectId)}/units/import`, { units });
}

export function reorderProjectUnits(projectId: string, unitIds: string[]) {
    return http.patch<{ unitIds: string[] }>(`/projects/${encodeURIComponent(projectId)}/units/reorder`, { unitIds });
}

export function updateProjectUnit(projectId: string, unitId: string, input: { title?: string; sourceText: string; status?: ProjectUnit["status"] }) {
    return http.patch<{ unit: ProjectUnit }>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}`, input);
}

export function deleteProjectUnit(projectId: string, unitId: string) {
    return http.delete<{ id: string }>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}`);
}

export function linkCanvasUnit(projectId: string, input: { canvasId: string; unitId: string; role?: string }) {
    return http.post<{ link: { id: string; projectId: string; canvasId: string; unitId: string; role: string } }>(`/projects/${encodeURIComponent(projectId)}/canvas-links`, input);
}

export function unlinkCanvasUnit(projectId: string, canvasId: string, unitId: string) {
    return http.delete<{ canvasId: string; unitId: string }>(`/projects/${encodeURIComponent(projectId)}/canvas-links/${encodeURIComponent(canvasId)}/units/${encodeURIComponent(unitId)}`);
}

export function unlinkCanvasProject(projectId: string, canvasId: string) {
    return http.delete<{ canvasId: string }>(`/projects/${encodeURIComponent(projectId)}/canvases/${encodeURIComponent(canvasId)}`);
}

export function linkProjectAsset(projectId: string, input: { assetId: string; category: AssetCategory; folderId?: string; title?: string; source?: "uploaded" | "canvas" }, signal?: AbortSignal) {
    return http.post<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets`, input, { signal });
}

export function unlinkProjectAsset(projectId: string, assetId: string) {
    return http.delete<{ id: string }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}`);
}

export function updateProjectAssetCategory(projectId: string, assetId: string, category: AssetCategory, signal?: AbortSignal) {
    return http.patch<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}`, { category }, { signal });
}

export function moveProjectAsset(projectId: string, assetId: string, folderId: string, signal?: AbortSignal) {
    return http.patch<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}`, { folderId }, { signal });
}

export function listProjectAssetFolders(projectId: string, signal?: AbortSignal) {
    return http.get<{ folders: ProjectAssetFolder[] }>(`/projects/${encodeURIComponent(projectId)}/asset-folders`, { signal });
}

export function createProjectAssetFolder(projectId: string, input: { name: string; parentId?: string; style?: ProjectAssetFolder["style"]; theme?: ProjectAssetFolder["theme"] }) {
    return http.post<{ folder: ProjectAssetFolder }>(`/projects/${encodeURIComponent(projectId)}/asset-folders`, input);
}

export function updateProjectAssetFolder(projectId: string, folderId: string, input: { name?: string; parentId?: string; style?: ProjectAssetFolder["style"]; theme?: ProjectAssetFolder["theme"] }) {
    return http.patch<{ folder: ProjectAssetFolder }>(`/projects/${encodeURIComponent(projectId)}/asset-folders/${encodeURIComponent(folderId)}`, input);
}

export function deleteProjectAssetFolder(projectId: string, folderId: string) {
    return http.delete<{ id: string }>(`/projects/${encodeURIComponent(projectId)}/asset-folders/${encodeURIComponent(folderId)}`);
}

export function createProjectAssetVersion(projectId: string, assetId: string, input: { prompt?: string; definitionJson?: string; note?: string }) {
    return http.post<{ version: { id: string; assetId: string; version: number; status: string } }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/versions`, input);
}

export function listVoiceProfiles() {
    return http.get<{ profiles: VoiceProfile[] }>("/voice-profiles");
}

export type CharacterListPage = {
    characters: ProjectCharacterDetail[];
    page: number;
    pageSize: number;
    total: number;
    hasMore: boolean;
};

export function listCharacters(options: { page?: number; pageSize?: number; query?: string; ids?: string[] } = {}) {
    return http.get<CharacterListPage>("/characters", {
        params: compactApiParams({
            page: options.page,
            pageSize: options.pageSize,
            q: options.query,
            ids: options.ids?.length ? options.ids.join(",") : undefined,
        }),
    });
}

export function getCharacter(assetId: string) {
    return http.get<ProjectCharacterDetail>(`/characters/${encodeURIComponent(assetId)}`);
}

export function createCharacter(input: { name: string; definition?: Record<string, unknown>; imageResourceId?: string; audioResourceId?: string; voiceName?: string; instructions?: string }) {
    return http.post<ProjectCharacterDetail>("/characters", input);
}

export function updateCharacter(assetId: string, input: { name: string; definition: Record<string, unknown> }) {
    return http.patch<ProjectCharacterDetail>(`/characters/${encodeURIComponent(assetId)}`, input);
}

export function replaceCharacterRepresentations(assetId: string, representations: Array<{ role: string; resourceId: string; metadata?: Record<string, unknown> }>) {
    return http.put<ProjectCharacterDetail>(`/characters/${encodeURIComponent(assetId)}/representations`, { representations });
}

export function bindCharacterVoice(assetId: string, input: { sampleResourceId?: string; voiceName?: string; instructions?: string }) {
    return http.put<ProjectCharacterDetail>(`/characters/${encodeURIComponent(assetId)}/voice`, input);
}

export function unbindCharacterVoice(assetId: string) {
    return http.delete<ProjectCharacterDetail>(`/characters/${encodeURIComponent(assetId)}/voice`);
}

export function createProjectCharacter(projectId: string, input: { name: string; definition?: Record<string, unknown>; referenceImages?: Array<{ resourceId: string }>; imageResourceId?: string; imageRole?: "portrait" | "turnaround_sheet"; folderId?: string }) {
    return http.post<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters`, input);
}

export function createProjectAssetEntity(projectId: string, input: { name: string; category: "environment" | "prop"; definition: Record<string, unknown>; referenceImages?: Array<{ resourceId: string }>; imageResourceId?: string; folderId?: string }) {
    return http.post<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/asset-entities`, input);
}

export function updateProjectAssetReferenceImages(projectId: string, assetId: string, input: { referenceImages: Array<{ resourceId: string }>; expectedVersionId: string }) {
    return http.put<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/reference-images`, input);
}

export function adoptProjectAssetImageCandidate(projectId: string, assetId: string, taskId: string, input: { expectedVersionId: string }) {
    return http.post<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/image-candidates/${encodeURIComponent(taskId)}/adopt`, input);
}

export type ProfileAnalysisCategory = "character" | "environment" | "prop";
export type ProfileAnalysisField = "appearance" | "physique" | "clothing" | "props" | "description" | "prompt";
export type ProfileAnalysisModelSelection = { kind: "logical"; logicalModelId: string } | { kind: "system"; channelId: string; modelKey: string };
export type ProfileAnalysisView = {
    taskId: string;
    projectId: string;
    assetId: string;
    category: ProfileAnalysisCategory;
    baseVersionId: string;
    source: { resourceId: string };
    status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
    stage: string;
    createdAt: string;
    updatedAt: string;
    canCancel: boolean;
    canAdopt: boolean;
    currentVersionId: string;
    blockedReason: string;
    // Detail-only allowlist projection: absent keys and explicit null remain distinct.
    baseline?: { versionId: string; fields: Partial<Record<ProfileAnalysisField, unknown>> };
    draft?: Partial<Record<ProfileAnalysisField, string>>;
    adoption?: { adoptedVersionId: string; selectedFields: ProfileAnalysisField[]; adoptedAt: string } | null;
    error?: { reason: string; message: string };
};
export type ProfileAnalysisAdoptInput = { expectedVersionId: string; selectedFields: ProfileAnalysisField[]; idempotencyKey: string };

export function createProjectAssetProfileAnalysis(projectId: string, assetId: string, input: { expectedVersionId: string; sourceResourceId: string; modelSelection: ProfileAnalysisModelSelection }) {
    return http.post<{ analysis: ProfileAnalysisView }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/profile-analyses`, input);
}

export function listProjectAssetProfileAnalyses(projectId: string, assetId: string, options: { limit?: number; beforeTaskId?: string; signal?: AbortSignal } = {}) {
    return http.get<{ analyses: ProfileAnalysisView[]; nextCursor?: string }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/profile-analyses`, { signal: options.signal, params: compactApiParams({ limit: options.limit, beforeTaskId: options.beforeTaskId }) });
}

export function getProjectAssetProfileAnalysis(projectId: string, assetId: string, taskId: string, signal?: AbortSignal) {
    return http.get<{ analysis: ProfileAnalysisView }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/profile-analyses/${encodeURIComponent(taskId)}`, { signal });
}

export function adoptProjectAssetProfileAnalysis(projectId: string, assetId: string, taskId: string, input: ProfileAnalysisAdoptInput) {
    return http.post<{ adoptedVersionId: string; replayed: boolean; asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/profile-analyses/${encodeURIComponent(taskId)}/adopt`, input).then((result) => ({ ...result, asset: normalizeProjectAsset(result.asset) }));
}

export function getProjectCharacter(projectId: string, assetId: string, config?: { signal?: AbortSignal }) {
    return http.get<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters/${encodeURIComponent(assetId)}`, config);
}

export function updateProjectCharacter(projectId: string, assetId: string, input: { name: string; definition: Record<string, unknown>; expectedVersionId: string }) {
    return http.patch<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters/${encodeURIComponent(assetId)}`, input);
}

export function replaceProjectCharacterRepresentations(projectId: string, assetId: string, representations: Array<{ role: string; resourceId: string; metadata?: Record<string, unknown> }>, expectedVersionId: string) {
    return http.put<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters/${encodeURIComponent(assetId)}/representations`, { representations, expectedVersionId });
}

/** 非角色项目资产（场景/道具等）图片绑定：每次绑定生成新资产版本。角色必须走 replaceProjectCharacterRepresentations。 */
export function replaceProjectAssetRepresentations(projectId: string, assetId: string, representations: Array<{ resourceId: string; metadata?: Record<string, unknown> }>) {
    return http.put<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/representations`, { representations });
}

export function bindProjectCharacterVoice(projectId: string, assetId: string, input: { voiceProfileId?: string; sampleResourceId?: string; voiceName?: string; instructions?: string }) {
    return http.put<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters/${encodeURIComponent(assetId)}/voice`, input);
}

export function unbindProjectCharacterVoice(projectId: string, assetId: string) {
    return http.delete<ProjectCharacterDetail>(`/projects/${encodeURIComponent(projectId)}/characters/${encodeURIComponent(assetId)}/voice`);
}

export function createUnitWorkflow(projectId: string, unitId: string) {
    return http.post<{ workflow: ProjectWorkflow }>(`/projects/${encodeURIComponent(projectId)}/workflows`, { unitId });
}

export function saveProjectShot(projectId: string, input: { id?: string; unitId?: string; title: string; description?: string; position?: number; durationMs?: number; status?: string; revision?: Partial<ShotRevisionInput> }) {
    return http.post<{ shot: ProjectShot }>(`/projects/${encodeURIComponent(projectId)}/shots`, input);
}

export function deleteProjectShot(projectId: string, shotId: string) {
    return http.delete<{ deleted: boolean }>(`/projects/${encodeURIComponent(projectId)}/shots/${encodeURIComponent(shotId)}`);
}

export type BindingSelection = { shotId: string; assetVersionIds: string[] };
export type BindingAppendRequest = { baseline: BindingBaseline; selections: BindingSelection[] };
export type BindingShotAppendResult = { shotId: string; addedCount: number; addedVersionIds: string[]; unchanged: boolean; references: BindingReference[]; artifacts: BindingArtifact[] };
export type BindingAppendResult = { projectRevision: number; unchanged: boolean; addedCount: number; shots: BindingShotAppendResult[] };

export function appendProjectUnitShotAssetBindings(projectId: string, unitId: string, input: BindingAppendRequest, signal?: AbortSignal) {
    return http.post<BindingAppendResult>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}/shot-asset-bindings/append`, input, { signal }).then((result) => ({
        ...result,
        shots: (result.shots ?? []).map((shot) => ({
            ...shot,
            addedVersionIds: shot.addedVersionIds ?? [],
            addedCount: typeof shot.addedCount === "number" ? shot.addedCount : (shot.addedVersionIds?.length ?? 0),
            references: shot.references ?? [],
            artifacts: shot.artifacts ?? [],
        })),
    }));
}

export type GeneratedBindingBaseline = Pick<BindingBaseline, "projectId" | "unitId" | "projectRevision" | "catalog"> & { taskId: string; taskResultFingerprint: string };
export type GeneratedBindingInput = { taskId: string; baseline: GeneratedBindingBaseline; rowIndices: number[] };
export type GeneratedBindingResolution = { taskId: string; baseline: GeneratedBindingBaseline; rows: Array<{ rowIndex: number; candidates: BindingCandidate[]; assetVersionIds: string[]; issues: BindingIssue[] }> };

export function resolveGeneratedProjectUnitShotAssetBindings(projectId: string, unitId: string, input: { taskId: string }, signal?: AbortSignal) {
    return http.post<GeneratedBindingResolution>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}/shot-asset-bindings/resolve-generated`, input, { signal });
}

export function replaceProjectUnitShots(projectId: string, unitId: string, shots: Array<{ title: string; description: string; durationMs: number; revision?: Partial<ShotRevisionInput>; assetVersionIds?: string[] }>, expectedShotIds?: string[], generatedBinding?: GeneratedBindingInput) {
    return http.put<{ shots: ProjectShot[] }>(`/projects/${encodeURIComponent(projectId)}/units/${encodeURIComponent(unitId)}/shots`, { shots, ...(expectedShotIds ? { expectedShotIds } : {}), ...(generatedBinding ? { generatedBinding } : {}) });
}

export function linkShotAsset(projectId: string, shotId: string, input: { assetVersionId: string; role: ShotAssetReference["role"] }) {
    return http.post<{ reference: ShotAssetReference }>(`/projects/${encodeURIComponent(projectId)}/shots/${encodeURIComponent(shotId)}/assets`, input);
}

export function unlinkShotAsset(projectId: string, shotId: string, referenceId: string) {
    return http.delete<{ unlinked: boolean }>(`/projects/${encodeURIComponent(projectId)}/shots/${encodeURIComponent(shotId)}/assets/${encodeURIComponent(referenceId)}`);
}

export function createShotRevision(projectId: string, shotId: string, input: ShotRevisionInput) {
    return http.post<{ shot: ProjectShot; revision: ShotRevision }>(`/projects/${encodeURIComponent(projectId)}/shots/${encodeURIComponent(shotId)}/revisions`, input);
}

export function createProjectAssetCandidates(projectId: string, candidates: Array<{ unitId?: string; shotId?: string; name: string; category: AssetCategory; details?: Record<string, unknown> }>, source?: "chapter_character_extract" | "agent") {
	return http.post<{ candidates: ProjectAssetCandidate[] }>(`/projects/${encodeURIComponent(projectId)}/asset-candidates`, { candidates, source });
}

export function confirmProjectAssetCandidate(projectId: string, candidateId: string, assetId?: string) {
    return http.post<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/asset-candidates/${encodeURIComponent(candidateId)}/confirm`, { assetId: assetId || "" });
}

export function bindProjectAssetImage(projectId: string, assetId: string, input: { resourceId: string; expectedVersionId: string }) {
    return http.put<{ asset: ProjectAsset }>(`/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/image`, input);
}

export function updateWorkflowStep(projectId: string, stepId: string, input: { status: string; outputJson?: string; error?: string }) {
    return http.patch<{ step: WorkflowStep }>(`/projects/${encodeURIComponent(projectId)}/workflow-steps/${encodeURIComponent(stepId)}`, input);
}

export function registerProjectTaskOutput(projectId: string, stepId: string, input: { taskId: string; canvasId?: string; unitId?: string; shotId?: string; artifactType?: string; assetVersionId?: string; resourceId?: string; mediaType?: string; role?: string; metadataJson?: string; outputJson?: string }) {
    return http.post<{ step: WorkflowStep }>(`/projects/${encodeURIComponent(projectId)}/workflow-steps/${encodeURIComponent(stepId)}/task-output`, input);
}
