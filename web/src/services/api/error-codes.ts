// Asset profile analysis exposes only machine-readable, safe failure categories.
export const PROFILE_ANALYSIS_REASONS = {
    invalidRequest: "asset_profile_analysis_invalid_request",
    notFound: "asset_profile_analysis_not_found",
    versionChanged: "project_asset_version_changed",
    sourceChanged: "asset_profile_analysis_source_changed",
    sourceUnavailable: "asset_profile_analysis_source_unavailable",
    baselineUnavailable: "asset_profile_analysis_baseline_unavailable",
    modelUnsupported: "asset_profile_analysis_model_unsupported",
    modelUnknown: "asset_profile_analysis_model_unknown",
    invalidResult: "asset_profile_analysis_invalid_result",
    notAdoptable: "asset_profile_analysis_not_adoptable",
    idempotencyConflict: "asset_profile_analysis_idempotency_conflict",
    alreadyAdopted: "asset_profile_analysis_already_adopted",
    taskStateChanged: "task_state_changed",
    writeBusy: "asset_profile_analysis_write_busy",
} as const;

export function profileAnalysisErrorMessage(reason?: string) {
    switch (reason) {
        case PROFILE_ANALYSIS_REASONS.versionChanged:
        case PROFILE_ANALYSIS_REASONS.sourceChanged: return "资产版本或原图已变化，草稿已保留；请对照当前值，不能覆盖新版本。";
        case PROFILE_ANALYSIS_REASONS.notFound: return "分析任务不存在或已清理，草稿不可恢复；已采用内容仍保留在资产历史版本。";
        case PROFILE_ANALYSIS_REASONS.sourceUnavailable: return "原始参考图不可用，请检查已保存的原图。";
        case PROFILE_ANALYSIS_REASONS.baselineUnavailable: return "历史基准不可读取，禁止首次采用；已有采用请求仍可精确重放。";
        case PROFILE_ANALYSIS_REASONS.modelUnsupported:
        case PROFILE_ANALYSIS_REASONS.modelUnknown: return "所选模型无法验证视觉输入能力，请明确选择可用的系统文本模型。";
        case PROFILE_ANALYSIS_REASONS.invalidResult: return "模型结果未通过安全格式校验，资产未修改。";
        case PROFILE_ANALYSIS_REASONS.alreadyAdopted: return "该草稿已采用，不能追加字段；请刷新查看结果。";
        case PROFILE_ANALYSIS_REASONS.idempotencyConflict: return "采用请求与原记录不一致，草稿已保留，请刷新核对。";
        case PROFILE_ANALYSIS_REASONS.notAdoptable: return "该任务当前不可采用，草稿已保留。";
        case PROFILE_ANALYSIS_REASONS.taskStateChanged: return "任务状态已变化，正在刷新真实结果。";
        case PROFILE_ANALYSIS_REASONS.writeBusy: return "保存暂时繁忙，请使用同一请求重试。";
        default: return "操作未完成，请刷新状态后重试。";
    }
}

export const STORYBOARD_ASSET_BINDING_REASONS = {
    invalidRequest: "storyboard_asset_binding_invalid_request",
    invalidReference: "storyboard_asset_binding_invalid_reference",
    capacityExceeded: "storyboard_asset_binding_capacity_exceeded",
    conflict: "storyboard_asset_binding_conflict",
    busy: "storyboard_asset_binding_busy",
} as const;

export const STORYBOARD_ASSET_BINDING_CODES = {
    invalidRequest: 400,
    invalidReference: 42201,
    capacityExceeded: 42202,
    conflict: 409,
    busy: 503,
} as const;

export function storyboardAssetBindingErrorMessage(reason?: string) {
    switch (reason) {
        case STORYBOARD_ASSET_BINDING_REASONS.invalidRequest: return "绑定请求格式不合法，请刷新后重试。";
        case STORYBOARD_ASSET_BINDING_REASONS.invalidReference: return "所选资产版本不可用或不属于当前项目，未写入任何绑定。";
        case STORYBOARD_ASSET_BINDING_REASONS.capacityExceeded: return "单个镜头最多绑定 6 个资产版本，未写入任何绑定。";
        case STORYBOARD_ASSET_BINDING_REASONS.conflict: return "分镜或资产基准已变化，请重新预览核对后再确认绑定。";
        case STORYBOARD_ASSET_BINDING_REASONS.busy: return "资产绑定繁忙，请使用原请求重试。";
        default: return "绑定操作未完成，请刷新后重试。";
    }
}

export const TASK_SUBMISSION_REASONS = {
    activeLimitExceeded: "task_active_limit_exceeded",
    rateLimited: "rate_limited",
} as const;

