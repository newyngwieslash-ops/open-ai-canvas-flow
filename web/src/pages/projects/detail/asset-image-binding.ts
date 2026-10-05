// 形象绑定覆盖范围（与后端 replaceableAssetCategories 一致）。
// 角色走角色形象接口（三视图），其余分类走通用资产图片绑定接口（单图 primary）。
export const IMAGE_BINDING_CATEGORIES = new Set(["character", "environment", "prop", "material", "other"]);

export function assetSupportsImageBinding(asset: { category: string }) {
    return IMAGE_BINDING_CATEGORIES.has(asset.category);
}
