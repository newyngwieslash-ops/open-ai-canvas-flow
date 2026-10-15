// 站点默认身份与旧默认值的精确映射；自定义品牌和自由标题保持原样。
export const SOURCE_UI_BRAND = "影策";
export const UI_BRAND_NAME = "源铭振跃";

export function displayBrandName(value: string): string {
    return value.trim() === SOURCE_UI_BRAND ? UI_BRAND_NAME : value;
}

export function displayBrandTitle(title: string, brandName: string): string {
    return displayBrandName(title || brandName);
}
