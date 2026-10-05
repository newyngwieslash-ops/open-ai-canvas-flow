// UI 字标展示层映射：只影响界面显示的默认品牌字样，
// 不改写 appearance 配置本身，避免联动注册协议、邮件、版权页脚等业务身份。
export const SOURCE_UI_BRAND = "影策";
export const UI_BRAND_NAME = "源铭振跃";

export function displayBrandName(value: string): string {
    return value.trim() === SOURCE_UI_BRAND ? UI_BRAND_NAME : value;
}

export function displayBrandTitle(title: string, brandName: string): string {
    const resolvedTitle = title || brandName;
    if (resolvedTitle.trim() === SOURCE_UI_BRAND && brandName.trim() === SOURCE_UI_BRAND) {
        return UI_BRAND_NAME;
    }
    return resolvedTitle;
}
