import { Component, lazy, Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowUpRight } from "lucide-react";

import { BrandLogo } from "@/components/brand/brand-logo";
import { displayBrandName } from "@/lib/brand-display";
import { useAppearanceStore } from "@/stores/use-appearance-store";

import { WelcomeContributorsCard } from "./contributors-card";
import { getWelcomeChapters, getWelcomeExchanges, getWelcomeLook, lookStills, showcases, welcomeLooks, type WelcomeLook } from "./story";
import "./welcome.css";

const StoryReel = lazy(() => import("./story-reel"));
const github = "https://github.com/ddcat-ai/open-ai-canvas";

export default function WelcomePage() {
    const [look, setLook] = useState(getWelcomeLook);
    const appearance = useAppearanceStore((state) => state.appearance);
    const restorePickerFocus = useRef(false);
    useEffect(() => {
        if (!restorePickerFocus.current) return;
        document.getElementById("welcome-look-select")?.focus({ preventScroll: true });
        restorePickerFocus.current = false;
    }, [look]);
    useEffect(() => {
        const onHistory = () => setLook(getWelcomeLook());
        window.addEventListener("popstate", onHistory);
        return () => window.removeEventListener("popstate", onHistory);
    }, []);
    const changeLook = (id: string) => {
        const next = welcomeLooks.find((item) => item.id === id);
        if (!next || next.id === look.id) return;
        const url = new URL(window.location.href);
        url.searchParams.set("look", id);
        window.history.pushState(null, "", url);
        restorePickerFocus.current = true;
        setLook(next);
    };
    return <WelcomeExperience key={look.id} look={look} brandName={displayBrandName(appearance.brandName)} onLookChange={changeLook} />;
}

function splitTitle(title: string) {
    const parts = title.split("，");
    if (parts.length < 2) return [title];
    return [parts[0] + "，", parts.slice(1).join("，")];
}

function WelcomeExperience({ look, brandName, onLookChange }: { look: WelcomeLook; brandName: string; onLookChange: (id: string) => void }) {
    const storyRef = useRef<HTMLElement>(null);
    const progressRef = useRef(0);
    const [chapter, setChapter] = useState(0);
    const [reduced, setReduced] = useState(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches);
    const [paused, setPaused] = useState(false);
    const [failed, setFailed] = useState(false);
    const [ready, setReady] = useState(false);
    const [menu, setMenu] = useState(false);
    const [showcase, setShowcase] = useState(0);
    const [playing, setPlaying] = useState(false);
    const videoRef = useRef<HTMLVideoElement>(null);
    const chapters = getWelcomeChapters(brandName);
    const exchanges = getWelcomeExchanges(look);
    const stills = lookStills(look);
    const seoTitle = useAppearanceStore((state) => state.appearance.seoTitle);
    const welcomeTitle = seoTitle || `${brandName} · 让一个故事从文字走向银幕`;
    const active = showcases[showcase];
    const staticScene = reduced || failed;
    const mosaic = stills.slice(0, 5);
    const closeStills = stills.slice(0, 3);
    const footage = welcomeLooks.find((item) => item.video)?.video;
    const activeChapter = chapters[chapter];
    const titleLines = chapter === 0 ? [activeChapter.title] : splitTitle(activeChapter.title);
    const TitleTag = chapter === 0 ? "h1" : "h2";

    useEffect(() => {
        document.title = welcomeTitle;
    }, [welcomeTitle]);
    useEffect(() => {
        const media = window.matchMedia("(prefers-reduced-motion: reduce)");
        const onMotion = () => setReduced(media.matches);
        media.addEventListener("change", onMotion);
        const onScroll = () => {
            if (!storyRef.current) return;
            const rect = storyRef.current.getBoundingClientRect();
            const progress = Math.max(0, Math.min(1, -rect.top / (rect.height - window.innerHeight)));
            progressRef.current = progress;
            storyRef.current.style.setProperty("--story-progress", String(progress));
            setChapter(Math.min(5, Math.floor(progress * 6)));
        };
        window.addEventListener("scroll", onScroll, { passive: true });
        window.addEventListener("resize", onScroll);
        onScroll();
        return () => {
            media.removeEventListener("change", onMotion);
            window.removeEventListener("scroll", onScroll);
            window.removeEventListener("resize", onScroll);
        };
    }, []);

    useEffect(() => {
        if (!playing) return;
        const close = (event: KeyboardEvent) => { if (event.key === "Escape") setPlaying(false); };
        window.addEventListener("keydown", close);
        return () => window.removeEventListener("keydown", close);
    }, [playing]);

    const jumpTo = (index: number) => {
        if (!storyRef.current) return;
        const height = storyRef.current.offsetHeight - window.innerHeight;
        window.scrollTo({ top: storyRef.current.offsetTop + height * (index === 5 ? 0.97 : index / 6 + (index ? 0.04 : 0)), behavior: reduced ? "instant" : "smooth" });
        setMenu(false);
    };

    return (
        <div className="welcome-page">
            <a className="welcome-skip" href="#workbench">前往工作台介绍</a>
            <section ref={storyRef} id="story" className="welcome-story" aria-label={`${brandName}创作之旅`}>
                <div className={`welcome-stage chapter-${chapter}${staticScene ? " is-static" : ""}`}>
                    <header className="welcome-header">
                        <a className="welcome-brand" href="/welcome" aria-label={`${brandName}首页`}>
                            <BrandLogo theme="dark" className="welcome-brand-logo" alt="" fallback={<span className="welcome-brand-logo is-fallback" />} />
                            {brandName}
                        </a>
                        <nav className={menu ? "welcome-nav is-open" : "welcome-nav"} aria-label="首页导航">
                            <a href="#workbench" onClick={() => setMenu(false)}>工作台</a>
                            <a href="#contributors" onClick={() => setMenu(false)}>演职员表</a>
                            <a href={github} target="_blank" rel="noreferrer">GitHub</a>
                        </nav>
                        <a className="welcome-action" href="/create">开始创作</a>
                        <button className="welcome-menu" type="button" aria-label={menu ? "关闭菜单" : "打开菜单"} aria-expanded={menu} onClick={() => setMenu(!menu)}>{menu ? "关闭" : "菜单"}</button>
                    </header>
                    <div className="welcome-masthead">
                        <div className={`welcome-poster${ready && !staticScene ? " is-ready" : ""}`} aria-hidden="true">
                            <img src={look.frames[staticScene ? Math.min(chapter * 2, look.frames.length - 1) : 0]} alt="" width={1600} height={900} fetchPriority="high" />
                        </div>
                        {staticScene ? (
                            <div className="welcome-mosaic" aria-hidden="true">
                                <img className="is-span-photo" src={mosaic[0]} alt="" width={1200} height={800} />
                                <span className="is-tile is-ink" />
                                <img src={mosaic[1] ?? mosaic[0]} alt="" width={800} height={600} />
                                <img src={mosaic[2] ?? mosaic[0]} alt="" width={800} height={600} />
                                <span className="is-tile is-accent is-span-color" />
                                <img src={mosaic[3] ?? mosaic[0]} alt="" width={800} height={600} />
                                <span className="is-tile is-field" />
                                <img src={mosaic[4] ?? mosaic[1] ?? mosaic[0]} alt="" width={800} height={600} />
                                <span className="is-tile is-ink" />
                            </div>
                        ) : (
                            <SceneBoundary onError={() => setFailed(true)}>
                                <Suspense fallback={null}>
                                    <StoryReel look={look} progress={progressRef} paused={paused} onReady={() => setReady(true)} onError={() => setFailed(true)} />
                                </Suspense>
                            </SceneBoundary>
                        )}
                    </div>
                    <div className="welcome-copy">
                        <aside className="welcome-rail">
                            <div className="welcome-looks" role="group" aria-label="素材版本">
                                {welcomeLooks.map((item) => (
                                    <button key={item.id} id={item.id === look.id ? "welcome-look-select" : undefined} type="button" className={item.id === look.id ? "is-current" : ""} onClick={() => onLookChange(item.id)}>
                                        {item.label}
                                    </button>
                                ))}
                                {look.credit && <a href={`/welcome/credits.html#${look.id}`} target="_blank" rel="noreferrer">{look.credit}</a>}
                            </div>
                            <nav className="welcome-chapter-nav" aria-label="故事章节">
                                {chapters.map((item, index) => (
                                    <button key={item.id} type="button" className={chapter === index ? "is-current" : ""} aria-current={chapter === index ? "step" : undefined} onClick={() => jumpTo(index)}>{item.label}</button>
                                ))}
                            </nav>
                            <button type="button" className="welcome-pause" disabled={staticScene} aria-pressed={paused || staticScene} onClick={() => setPaused(!paused)}>{paused || staticScene ? "播放画面" : "暂停画面"}</button>
                        </aside>
                        <section key={activeChapter.id} className="welcome-chapter is-active">
                            <TitleTag className="welcome-lines">
                                {titleLines.map((line) => <span key={line} className="line"><span>{line}</span></span>)}
                            </TitleTag>
                            {chapter === 0 && activeChapter.subtitle && <p className="welcome-standfirst">{activeChapter.subtitle}</p>}
                            {chapter === 0 && footage && <button type="button" className="welcome-film-link" onClick={() => setPlaying(true)}>观看片段</button>}
                            {chapter === 0 && <a className="welcome-action welcome-action-foot" href="/create">开始创作</a>}
                        </section>
                        <button type="button" className="welcome-next" onClick={() => chapter < 5 ? jumpTo(chapter + 1) : document.getElementById("workbench")?.scrollIntoView({ behavior: reduced ? "instant" : "smooth" })}>
                            {chapter === 5 ? "进入创作现场" : "下一幕"}
                        </button>
                    </div>
                </div>
            </section>

            <section id="workbench" className="welcome-showcase">
                <div className="welcome-index" role="tablist" aria-label="工作台预览">
                    {showcases.map((item, index) => (
                        <button key={item.name} type="button" role="tab" aria-selected={showcase === index} className={showcase === index ? "is-current" : ""} onClick={() => setShowcase(index)}>{item.name}</button>
                    ))}
                </div>
                <div className="welcome-sheet-stage">
                    <article className="welcome-sheet" data-ui>
                        <img src={active.image} alt={`${brandName}${active.name}界面`} width={1600} height={900} loading="lazy" />
                        <p>{active.detail}</p>
                        <a href={active.href}>进入{active.name}<ArrowUpRight size={16} /></a>
                    </article>
                </div>
            </section>

            <section className="welcome-method">
                <h2>你给它什么</h2>
                {exchanges.map((item, index) => (
                    <article key={item.lead} className={index === 2 ? "is-final" : undefined}>
                        <p className="welcome-lead">{item.lead}</p>
                        <label className="welcome-call" data-ui>
                            <span>场记</span>
                            <textarea readOnly rows={2} value={item.call} />
                        </label>
                        <figure className="welcome-reply" data-ui>
                            <img src={item.frame} alt="" width={1200} height={800} loading="lazy" />
                            <figcaption>{item.reply}</figcaption>
                        </figure>
                    </article>
                ))}
            </section>

            <WelcomeContributorsCard />

            <section className="welcome-close">
                <div className="welcome-close-stills">
                    {(closeStills.length === 3 ? closeStills : [stills[0], stills[0], stills[0]]).map((src, index) => (
                        footage && index === 1 ? (
                            <video key={src} src={footage} poster={src} muted loop playsInline autoPlay preload="metadata" />
                        ) : (
                            <img key={`${src}-${index}`} src={src} alt="" width={1200} height={800} loading="lazy" />
                        )
                    ))}
                </div>
                <div className="welcome-close-field">
                    <p className="welcome-close-copy">你的故事，<br />现在就可以<br /><a href="/create">开始创作</a>。</p>
                    <p className="welcome-close-note">开源工作台，MIT 许可。从一句话或一张画布进入。</p>
                    {look.credit && <p className="welcome-close-note"><a href={`/welcome/credits.html#${look.id}`} target="_blank" rel="noreferrer">{look.credit} · 署名与许可</a></p>}
                </div>
            </section>

            <footer className="welcome-footer">
                <div className="welcome-footer-cols">
                    <div>
                        <p>工作台</p>
                        <a href="/canvas">自由画布</a>
                        <a href="/create">即时创作</a>
                        <a href="/projects">项目工作台</a>
                    </div>
                    <div>
                        <p>本页</p>
                        <a href="#workbench">创作现场</a>
                        <a href="#contributors">演职员表</a>
                        <a href="/create">开始创作</a>
                    </div>
                    <div>
                        <p>源码</p>
                        <a href={github} target="_blank" rel="noreferrer">GitHub</a>
                        <a href={`${github}/blob/main/LICENSE`} target="_blank" rel="noreferrer">MIT License</a>
                    </div>
                    <div>
                        <p>画面</p>
                        <a href="/welcome/credits.html" target="_blank" rel="noreferrer">演示素材署名</a>
                        <span>开源 AI 影视创作工作台</span>
                    </div>
                </div>
                <p className="welcome-wordmark">{brandName}</p>
                <p className="welcome-legal">Open Source · MIT License</p>
            </footer>
            {playing && footage && <FilmDialog look={look} src={footage} onClose={() => setPlaying(false)} videoRef={videoRef} />}
        </div>
    );
}

function FilmDialog({ look, src, onClose, videoRef }: { look: WelcomeLook; src: string; onClose: () => void; videoRef: React.RefObject<HTMLVideoElement | null> }) {
    const dialogRef = useRef<HTMLDialogElement>(null);
    useEffect(() => { const dialog = dialogRef.current; dialog?.showModal(); return () => dialog?.close(); }, []);
    return (
        <dialog ref={dialogRef} className="welcome-film-dialog" aria-label={`${look.title}影片片段`} onCancel={onClose} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
            <div>
                <button type="button" className="welcome-dialog-close" autoFocus aria-label="关闭影片" onClick={onClose}>关闭</button>
                <video ref={videoRef} src={src} poster={look.frames[0]} controls autoPlay muted playsInline />
                <p>{look.credit ?? look.title}</p>
            </div>
        </dialog>
    );
}

class SceneBoundary extends Component<{ children: ReactNode; onError: () => void }, { failed: boolean }> {
    state = { failed: false };
    static getDerivedStateFromError() { return { failed: true }; }
    componentDidCatch() { this.props.onError(); }
    render() { return this.state.failed ? null : this.props.children; }
}
