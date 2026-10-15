import { expect, test } from "bun:test";

import { appearanceLogoURL, applyAppearanceMetadata, brandStudioLabel, DEFAULT_PUBLIC_APPEARANCE, normalizePublicAppearance } from "../src/stores/use-appearance-store";
import { moduleGroupSource } from "./helpers/module-group-source";

type FakeElement = {
    tag: string;
    attrs: Record<string, string>;
    content: string;
    rel: string;
    href: string;
    setAttribute: (name: string, value: string) => void;
    remove: () => void;
};

// 轻量 document 替身：足够支撑 applyAppearanceMetadata 的 meta/favicon 写入路径。
function createMetadataDocument(dark = false) {
    const registered = new Map<string, FakeElement>();
    const appended: FakeElement[] = [];
    const createElement = (tag: string): FakeElement => {
        const element: FakeElement = {
            tag,
            attrs: {},
            content: "",
            rel: "",
            href: "",
            setAttribute(name, value) {
                element.attrs[name] = value;
            },
            remove() {
                for (const [key, value] of registered) if (value === element) registered.delete(key);
            },
        };
        return element;
    };
    const documentMock = {
        title: "",
        documentElement: { classList: { contains: (name: string) => name === "dark" && dark } },
        head: {
            appendChild: (element: FakeElement) => {
                appended.push(element);
                if (element.tag === "meta") {
                    const attribute = element.attrs.name !== undefined ? "name" : "property";
                    registered.set(`${attribute}=${element.attrs[attribute]}`, element);
                }
            },
        },
        defaultView: undefined,
        querySelector: (selector: string) => {
            if (selector === 'link[rel~="icon"]') return null;
            const match = selector.match(/^meta\[(name|property)="(.+)"\]$/);
            return match ? registered.get(`${match[1]}=${match[2]}`) ?? null : null;
        },
        createElement,
    };
    return { documentMock, registered, appended };
}

test("initial HTML stays brand neutral until the public appearance is resolved", async () => {
    const [html, mainSource] = await Promise.all([Bun.file(new URL("../index.html", import.meta.url)).text(), Bun.file(new URL("../src/main.tsx", import.meta.url)).text()]);

    expect(html).not.toContain("影策");
    expect(html).toContain("<title>正在加载</title>");
    expect(mainSource).toContain("bootstrapAppearance()");
    expect(mainSource).toContain('import("./application")');
    expect(mainSource.indexOf("bootstrapAppearance()")).toBeLessThan(mainSource.indexOf('import("./application")'));
});

test("a custom login video never falls back to the built-in poster", () => {
    const appearance = normalizePublicAppearance({
        brandName: "HIMA Studio",
        brandSlug: "hima-studio",
        authHeroTitle: "把灵感，\n变成可见的故事。",
        authHeroDescription: "从同一个创作空间持续推进。",
        authVideoConfigured: true,
        authVideoUrl: "/api/public/appearance/assets/video?v=next",
        authVideoPosterConfigured: false,
        authVideoPosterUrl: "",
    });

    expect(appearance.brandName).toBe("HIMA Studio");
    expect(appearance.brandSlug).toBe("hima-studio");
    expect(appearance.authHeroTitle).toBe("把灵感，\n变成可见的故事。");
    expect(appearance.authHeroDescription).toBe("从同一个创作空间持续推进。");
    expect(appearance.authVideoPosterUrl).toBe("");
    expect(appearance.authVideoAutoplay).toBe(true);
});

test("login video autoplay defaults on and can be disabled explicitly", () => {
    expect(normalizePublicAppearance({}).authVideoAutoplay).toBe(true);
    expect(normalizePublicAppearance({ authVideoAutoplay: false }).authVideoAutoplay).toBe(false);
});

test("appearance URLs reject executable and insecure remote schemes", () => {
    const appearance = normalizePublicAppearance({
        logoConfigured: true,
        logoUrl: "javascript:alert(1)",
        authVideoConfigured: true,
        authVideoUrl: "http://example.com/brand.mp4",
    });

    expect(appearance.logoUrl).toBe("/logo.svg");
    expect(appearance.authVideoUrl).not.toContain("example.com");
});

test("appearance selects theme logos and falls back to the single configured logo", () => {
    const dual = normalizePublicAppearance({
        logoConfigured: true,
        darkLogoConfigured: true,
        logoUrl: "/api/public/appearance/assets/logo?v=dual",
        darkLogoUrl: "/api/public/appearance/assets/logo-dark?v=dual",
        logoFrameEnabled: false,
    });
    expect(appearanceLogoURL(dual, "light")).toContain("/logo?");
    expect(appearanceLogoURL(dual, "dark")).toContain("/logo-dark?");
    expect(dual.logoFrameEnabled).toBe(false);

    const single = normalizePublicAppearance({ logoConfigured: true, logoUrl: "/api/public/appearance/assets/logo?v=single" });
    expect(appearanceLogoURL(single, "light")).toBe(single.logoUrl);
    expect(appearanceLogoURL(single, "dark")).toBe(single.logoUrl);
    expect(single.logoFrameEnabled).toBe(true);
});

test("auth scene consumes resolved appearance instead of hardcoded media constants", async () => {
    const source = await Bun.file(new URL("../src/pages/auth/auth-scene.tsx", import.meta.url)).text();

    expect(source).toContain("appearance.authVideoUrl");
    expect(source).toContain("appearance.authVideoAutoplay");
    expect(source).toContain("appearance.authVideoPosterUrl || undefined");
    expect(source).toContain("appearance.brandName");
    expect(source).toContain("appearance.authHeroTitle");
    expect(source).toContain("appearance.authHeroDescription");
    expect(source).toContain("useThemeStore((state) => state.theme)");
    expect(source).toContain("theme={theme}");
    expect(source).not.toContain("让一个故事，");
    expect(source).not.toContain("AUTH_VIDEO_URL");
    expect(source).not.toContain("AUTH_VIDEO_POSTER");
});

test("appearance management exposes light and dark logo uploads plus the frame switch", async () => {
    const [pageSource, brandSource, adminStyles, globalStyles] = await Promise.all([
        Promise.resolve(moduleGroupSource("pages/admin/settings/appearance-settings-page.tsx")),
        Bun.file(new URL("../src/components/brand/brand-logo.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/styles/admin-ui.css", import.meta.url)).text(),
        Bun.file(new URL("../src/styles/globals.css", import.meta.url)).text(),
    ]);

    expect(pageSource).toContain('slot="logo-dark"');
    expect(pageSource).toContain("浅色模式 Logo");
    expect(pageSource).toContain("深色模式 Logo（可选）");
    expect(pageSource).toContain("禁用 Logo 后面的圆角矩形外框");
    expect(pageSource).toContain("<Switch");
    expect(pageSource).toContain("checked={!logoFrameEnabled}");
    expect(pageSource).toContain("setLogoFrameEnabled(!checked)");
    expect(pageSource).not.toContain("<Checkbox");
    expect(pageSource).toContain("深浅模式 Logo 预览");
    expect(pageSource).toContain("登录页视频自动播放");
    expect(pageSource).toContain("authVideoAutoplay");
    expect(brandSource).toContain("useActiveTheme");
    expect(brandSource).toContain("data-logo-frame-enabled");
    expect(brandSource).toContain("failedSource === source");
    expect(brandSource).toContain('aria-hidden="true"');
    expect(brandSource).toContain('style.visibility = "hidden"');
    expect(brandSource).toContain("setFailedSource(source)");
    expect(adminStyles).toContain(".admin-appearance-logo-preview-mark.is-unframed img");
    expect(globalStyles).toContain('.brand-logo-frame[data-logo-frame-enabled="false"] > :is(img, svg)');
});

test("object storage can adopt the configured English brand identifier without replacing saved prefixes automatically", async () => {
    const source = await Promise.resolve(moduleGroupSource("pages/admin/settings/storage-settings-page.tsx"));

    expect(source).toContain("state.appearance.brandSlug");
    expect(source).toContain("使用品牌标识");
    expect(source).toContain('form.setFieldValue("pathPrefix", brandSlug)');
    expect(source).toContain("setting.pathPrefix || DEFAULT_OSS_PATH_PREFIX");
});

test("appearance management exposes a server-side reset to the built-in Yingce brand", async () => {
    const [pageSource, apiSource] = await Promise.all([Promise.resolve(moduleGroupSource("pages/admin/settings/appearance-settings-page.tsx")), Bun.file(new URL("../src/services/api/appearance.ts", import.meta.url)).text()]);

    expect(pageSource).toContain("恢复源铭振跃默认");
    expect(pageSource).toContain("resetAdminAppearance()");
    expect(pageSource).toContain("已上传文件仍保留在存储资源中");
    expect(apiSource).toContain('http.delete<{ setting: AdminAppearance }>("/admin/settings/appearance")');
});

test("missing public appearance resolves the new default identity and stable machine slug", () => {
    expect(DEFAULT_PUBLIC_APPEARANCE.brandName).toBe("源铭振跃");
    for (const input of [undefined, null, {}, { brandName: "  ", seoTitle: " " }]) {
        const appearance = normalizePublicAppearance(input);
        expect(appearance.brandName).toBe("源铭振跃");
        expect(appearance.brandSlug).toBe("open-ai-canvas");
        expect(appearance.seoTitle).toBe("源铭振跃");
        expect(appearance.seoDescription).toBe("源铭振跃，面向 AI 影视与短剧创作的工作台。");
        expect(appearance.footerCopyright).toBe(`© ${new Date().getFullYear()} 源铭振跃. All rights reserved.`);
        expect(brandStudioLabel(appearance)).toBe("AI 创作工作台");
    }
});

test("default studio label uses a neutral subtitle while custom slugs keep their label", () => {
    expect(brandStudioLabel(DEFAULT_PUBLIC_APPEARANCE)).toBe("AI 创作工作台");
    expect(brandStudioLabel(normalizePublicAppearance({ brandName: "HIMA Studio", brandSlug: "hima-studio" }))).toBe("HIMA STUDIO");
});

test("custom brands using an omitted or explicit default machine slug get a neutral studio label", () => {
    for (const input of [{ brandName: "HIMA Studio" }, { brandName: "HIMA Studio", brandSlug: "open-ai-canvas" }]) {
        const original = { ...input };
        const appearance = normalizePublicAppearance(input);
        expect(brandStudioLabel(appearance)).toBe("AI 创作工作台");
        expect(appearance.brandName).toBe("HIMA Studio");
        expect(appearance.brandSlug).toBe("open-ai-canvas");
        expect(input).toEqual(original);
    }
});

test("only exact legacy SEO and copyright templates are treated as defaults", () => {
    const appearance = normalizePublicAppearance({
        brandName: "HIMA Studio", seoTitle: "影策",
        seoDescription: "影策，面向 AI 影视与短剧创作的工作台。",
        footerCopyright: "© 2020 影策. All rights reserved.",
    });
    expect(appearance.brandName).toBe("HIMA Studio");
    expect(appearance.seoTitle).toBe("源铭振跃");
    expect(appearance.seoDescription).toBe("源铭振跃，面向 AI 影视与短剧创作的工作台。");
    expect(appearance.footerCopyright).toBe("© 2020 源铭振跃. All rights reserved.");
    expect(normalizePublicAppearance({ footerCopyright: "© 2020 影策. All rights reserved. 定制声明" }).footerCopyright).toBe("© 2020 影策. All rights reserved. 定制声明");
});

test("legacy default fields normalize once without mutating the response or unrelated settings", () => {
    const input = {
        brandName: " 影策 ",
        seoTitle: "影策",
        seoDescription: "影策，面向 AI 影视与短剧创作的工作台。",
        footerCopyright: "© 2024 影策. All rights reserved.",
        brandSlug: "open-ai-canvas",
        seoKeywords: "影策,自定义关键词",
        authHeroTitle: "影策的自由文案",
        logoUrl: "/custom-logo.svg",
        darkLogoUrl: "/custom-dark.svg",
        authVideoConfigured: true,
        authVideoUrl: "/custom-video.mp4",
        authVideoPosterUrl: "/custom-poster.jpg",
        icpFilingEnabled: true,
        icpFilingNumber: "自定义备案",
        configured: true,
        revision: "saved-identity",
    };
    const snapshot = { ...input };
    const appearance = normalizePublicAppearance(input);
    expect(appearance).toMatchObject({
        ...input,
        brandName: "源铭振跃",
        seoTitle: "源铭振跃",
        seoDescription: "源铭振跃，面向 AI 影视与短剧创作的工作台。",
        footerCopyright: "© 2024 源铭振跃. All rights reserved.",
    });
    expect(normalizePublicAppearance(appearance)).toEqual(appearance);
    expect(input).toEqual(snapshot);
});

test("custom identity and free SEO or copyright copy are preserved", () => {
    const custom = normalizePublicAppearance({
        brandName: "HIMA Studio",
        brandSlug: "hima-studio",
        seoTitle: "定制标题",
        seoDescription: "影策，面向我们的独立项目。",
        footerCopyright: "© 2024 影策工作室. All rights reserved.",
    });
    expect(custom.brandName).toBe("HIMA Studio");
    expect(custom.seoTitle).toBe("定制标题");
    expect(custom.seoDescription).toBe("影策，面向我们的独立项目。");
    expect(custom.footerCopyright).toBe("© 2024 影策工作室. All rights reserved.");
    expect(brandStudioLabel(custom)).toBe("HIMA STUDIO");
    expect(normalizePublicAppearance({ brandName: "影策工作室" }).seoTitle).toBe("影策工作室");
    expect(normalizePublicAppearance({ brandName: "影策", seoTitle: "影策 - 定制长标题", seoDescription: "" })).toMatchObject({
        brandName: "源铭振跃", seoTitle: "影策 - 定制长标题", seoDescription: "",
    });
});

test("metadata consumes normalized identity and preserves custom titles", () => {
    const legacy = normalizePublicAppearance({
        brandName: "影策", seoTitle: "影策",
        seoDescription: "影策，面向 AI 影视与短剧创作的工作台。",
        footerCopyright: "© 2026 影策. All rights reserved.",
    });
    const { documentMock, registered } = createMetadataDocument();
    applyAppearanceMetadata(legacy, documentMock as unknown as Document);

    expect(documentMock.title).toBe("源铭振跃");
    expect(registered.get("property=og:title")?.content).toBe("源铭振跃");
    expect(registered.get("name=twitter:title")?.content).toBe("源铭振跃");
    expect(registered.get("property=og:site_name")?.content).toBe("源铭振跃");
    expect(registered.get("name=description")?.content).toBe("源铭振跃，面向 AI 影视与短剧创作的工作台。");
    expect(registered.get("property=og:description")?.content).toBe("源铭振跃，面向 AI 影视与短剧创作的工作台。");
    expect(registered.get("name=twitter:description")?.content).toBe("源铭振跃，面向 AI 影视与短剧创作的工作台。");
    expect(legacy.brandName).toBe("源铭振跃");
    expect(legacy.footerCopyright).toBe("© 2026 源铭振跃. All rights reserved.");

    const custom = normalizePublicAppearance({ brandName: "HIMA Studio", brandSlug: "hima-studio", seoTitle: "HIMA Studio - AI 影视工作台" });
    const customDocument = createMetadataDocument();
    applyAppearanceMetadata(custom, customDocument.documentMock as unknown as Document);
    expect(customDocument.documentMock.title).toBe("HIMA Studio - AI 影视工作台");
    expect(customDocument.registered.get("property=og:site_name")?.content).toBe("HIMA Studio");
});

test("the welcome entry resolves availability and appearance before rendering", async () => {
    const source = await Bun.file(new URL("../src/welcome-application.tsx", import.meta.url)).text();

    expect(source).toContain("getWelcomeAvailability");
    expect(source).toContain('window.location.replace("/")');
    expect(source).toContain("await bootstrapAppearance()");
    expect(source.indexOf("bootstrapAppearance()")).toBeLessThan(source.indexOf("<WelcomePage />"));
});

test("the welcome page consumes the displayed brand for title, opening chapter and labels", async () => {
    const [pageSource, storySource] = await Promise.all([
        Bun.file(new URL("../src/pages/welcome/index.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/pages/welcome/story.ts", import.meta.url)).text(),
    ]);

    expect(pageSource).toContain("displayBrandName(appearance.brandName)");
    expect(pageSource).toContain("getWelcomeChapters(brandName)");
    expect(pageSource).toContain("document.title = `${brandName} · 让一个故事从文字走向银幕`");
    expect(pageSource).toContain("aria-label={`${brandName}创作之旅`}");
    expect(pageSource).not.toContain("影策 · 让一个故事");
    expect(storySource).not.toContain('title: "影策"');
});
