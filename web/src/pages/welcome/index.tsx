import { Component, lazy, Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowRight, ArrowUpRight, Pause, Play } from "lucide-react";

import { displayBrandName, displayBrandTitle } from "@/lib/brand-display";
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

function WelcomeExperience({ look, brandName, onLookChange }: { look: WelcomeLook; brandName: string; onLookChange: (id: string) => void }) {
    const [chapter, setChapter] = useState(0);
    const [reduced, setReduced] = useState(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches);
    const [reel, setReel] = useState(false);
    const [failed, setFailed] = useState(false);
    const [ready, setReady] = useState(false);
    const [playing, setPlaying] = useState(false);
    const progressRef = useRef(0);
    const chapters = getWelcomeChapters(brandName);
    const exchanges = getWelcomeExchanges(look);
    const stills = lookStills(look);
    const appearance = useAppearanceStore((state) => state.appearance);
    const welcomeTitle = displayBrandTitle(appearance.seoTitle, appearance.brandName) || `${brandName} · 让一个故事从文字走向银幕`;
    const animate = reel && !reduced && !failed;
    const opening = chapter === 0 ? look.opening : undefined;
    const showingOpening = opening && !(animate && ready);
    progressRef.current = chapter / (chapters.length - 1);

    useEffect(() => {
        document.title = welcomeTitle;
    }, [welcomeTitle]);
    useEffect(() => {
        const media = window.matchMedia("(prefers-reduced-motion: reduce)");
        const onMotion = () => setReduced(media.matches);
        media.addEventListener("change", onMotion);
        return () => media.removeEventListener("change", onMotion);
    }, []);

    return (
        <main className="welcome-page">
            <a className="welcome-skip" href="#workbench">前往工作台介绍</a>
            <section id="story" className="welcome-story" aria-label={`${brandName}创作之旅`}>
                <aside className="welcome-rail">
                    <a className="welcome-rail-brand" href="/welcome" aria-label={`${brandName}首页`}>{brandName}</a>
                    <p className="welcome-micro">OPEN SOURCE<br />AI FILM WORKBENCH</p>
                    <div className="welcome-looks" role="group" aria-label="素材版本">
                        {welcomeLooks.map((item, index) => (
                            <button key={item.id} id={item.id === look.id ? "welcome-look-select" : undefined} type="button" aria-pressed={item.id === look.id} onClick={() => onLookChange(item.id)}>
                                <img src={item.opening?.image ?? item.frames[0]} alt="" width={280} height={120} />
                                <span>{item.label.split(" · ")[0]}<small>{String(index + 1).padStart(2, "0")}</small></span>
                            </button>
                        ))}
                    </div>
                    <nav className="welcome-chapter-nav" aria-label="故事章节">
                        {chapters.map((item, index) => (
                            <button key={item.id} type="button" aria-current={chapter === index ? "step" : undefined} onClick={() => setChapter(index)}><span>{String(index + 1).padStart(2, "0")}</span>{item.label}</button>
                        ))}
                    </nav>
                    <p className="welcome-micro welcome-rail-note">STORY / {String(chapter + 1).padStart(2, "0")} — 06</p>
                </aside>

                <div className="welcome-intro">
                    <header className="welcome-header">
                        <nav aria-label="首页导航">
                            <a href="#workbench">工作台</a>
                            <a href="#contributors">演职员表</a>
                            <a href={github} target="_blank" rel="noreferrer">GitHub <ArrowUpRight size={12} /></a>
                        </nav>
                    </header>
                    <div className="welcome-copy">
                        <h1>{brandName}</h1>
                        <span className="welcome-rule" aria-hidden="true" />
                        <h2>让一个故事从文字走向银幕</h2>
                        <p>开源的 AI 影视与短剧创作工作台<br />从灵感、到分镜、到成片，一站式实现。</p>
                        <div className="welcome-copy-actions">
                            <a className="welcome-action" href="/create">开始创作 <ArrowRight size={24} /></a>
                            <span className="welcome-micro">ANYONE<br />A STORY<br />A BIGGER TOMORROW</span>
                        </div>
                    </div>
                </div>

                <div className="welcome-cinema">
                    <div className="welcome-cinema-top welcome-micro"><span>{showingOpening ? opening.title : look.label}</span><span>{brandName}</span></div>
                    <div className="welcome-screen">
                        <img className={`welcome-poster${animate && ready ? " is-ready" : ""}`} src={opening?.image ?? look.frames[Math.min(chapter * 2, look.frames.length - 1)]} alt={opening?.title ?? `${look.title} · ${chapters[chapter].label}演示画面`} width={2060} height={763} fetchPriority="high" />
                        {animate && (
                            <SceneBoundary onError={() => setFailed(true)}>
                                <Suspense fallback={null}>
                                    <StoryReel look={look} progress={progressRef} paused={false} onReady={() => setReady(true)} onError={() => setFailed(true)} />
                                </Suspense>
                            </SceneBoundary>
                        )}
                    </div>
                    <div className="welcome-cinema-caption">
                        <p>{chapter === 0 ? "想象，是另一种真实。" : chapters[chapter].title}</p>
                        <div>
                            {look.video && <button type="button" onClick={() => setPlaying(true)}><Play size={12} /> 观看片段</button>}
                            <button type="button" aria-pressed={animate} disabled={reduced || failed} onClick={() => { setReady(false); setReel(!reel); }}>{animate ? <Pause size={12} /> : <Play size={12} />}{failed ? "静态画面" : reduced ? "已减少动态效果" : animate ? "暂停卷轴" : "播放卷轴"}</button>
                        </div>
                    </div>
                    <a className="welcome-cinema-credit welcome-micro" href={`/welcome/credits.html#${showingOpening ? "concept" : look.id}`} target="_blank" rel="noreferrer">{showingOpening ? "AI 生成概念画面 · 素材说明" : `${look.credit} · 署名与许可`} <ArrowUpRight size={12} /></a>
                </div>
            </section>

            <section id="workbench" className="welcome-showcase" aria-label="工作台预览">
                <div className="welcome-section-note"><p className="welcome-micro">WORKFLOW<br />FOR<br />BOLDER<br />STORIES</p><p>工具服务于创作者<br />而非取代他们</p><span aria-hidden="true">/</span></div>
                <div className="welcome-sheets">
                    {showcases.map((item, index) => (
                        <article key={item.name} className="welcome-sheet">
                            <div className="welcome-sheet-heading"><span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span><div><h2><a href={item.href}>{item.name} <ArrowUpRight size={16} /></a></h2><p>{item.detail}</p></div></div>
                            <a className="welcome-sheet-preview" href={item.href} aria-label={`进入${item.name}`}><img src={item.image} alt={`${brandName}${item.name}界面预览`} width={1600} height={900} loading="lazy" /></a>
                        </article>
                    ))}
                </div>
            </section>

            <section className="welcome-method" aria-labelledby="welcome-method-title">
                <div className="welcome-section-note"><p className="welcome-micro">FROM<br />SCRIPT<br />TO<br />SCREEN</p><h2 id="welcome-method-title">你给它什么</h2><span aria-hidden="true">/</span></div>
                <div className="welcome-exchanges">
                    {exchanges.map((item, index) => (
                        <article key={item.lead}>
                            <div className="welcome-call"><div><span>{String(index + 1).padStart(2, "0")}</span><span>剧本片段</span></div><h3>{item.call}</h3><p>{item.lead}</p><p>{look.screenplay[Math.min(index * 4 + 1, 11)]}<br />{look.screenplay[Math.min(index * 4 + 2, 11)]}</p><p className="welcome-script-reply">{item.reply}</p></div>
                            <figure className="welcome-reply"><img src={item.frame} alt={index === 0 && look.opening ? look.opening.title : `${look.title} · ${item.lead}`} width={1200} height={800} loading="lazy" /><figcaption>{item.reply}</figcaption></figure>
                        </article>
                    ))}
                </div>
            </section>

            <WelcomeContributorsCard />

            <section className="welcome-close">
                <div className="welcome-close-stills">{stills.slice(0, 3).map((src) => <img key={src} src={src} alt="" width={1200} height={800} loading="lazy" />)}</div>
                <div className="welcome-close-field"><p>你的故事，<br />现在就可以开始。</p><a className="welcome-action" href="/create">开始创作 <ArrowRight size={24} /></a></div>
            </section>

            <footer className="welcome-footer">
                <div className="welcome-footer-top"><a href="/welcome">{brandName}</a><nav aria-label="页脚导航"><a href="/canvas">自由画布</a><a href="/create">即时创作</a><a href="/projects">项目工作台</a><a href="/welcome/credits.html">演示素材署名</a><a href={`${github}/blob/main/LICENSE`} target="_blank" rel="noreferrer">MIT License <ArrowUpRight size={12} /></a></nav></div>
                <p className="welcome-wordmark">{brandName}</p><p className="welcome-legal welcome-micro">OPEN SOURCE · AI FILM WORKBENCH</p>
            </footer>
            {playing && look.video && <FilmDialog look={look} src={look.video} onClose={() => setPlaying(false)} />}
        </main>
    );
}

function FilmDialog({ look, src, onClose }: { look: WelcomeLook; src: string; onClose: () => void }) {
    const dialogRef = useRef<HTMLDialogElement>(null);
    useEffect(() => {
        const dialog = dialogRef.current;
        const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        dialog?.showModal();
        return () => { dialog?.close(); if (trigger?.isConnected) trigger.focus({ preventScroll: true }); };
    }, []);
    return (
        <dialog ref={dialogRef} className="welcome-film-dialog" aria-label={`${look.title}影片片段`} onCancel={onClose} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
            <button type="button" className="welcome-dialog-close" autoFocus aria-label="关闭影片" onClick={onClose}>关闭</button>
            <video src={src} poster={look.frames[0]} controls autoPlay muted playsInline />
            <p>{look.credit ?? look.title}</p>
        </dialog>
    );
}

class SceneBoundary extends Component<{ children: ReactNode; onError: () => void }, { failed: boolean }> {
    state = { failed: false };
    static getDerivedStateFromError() { return { failed: true }; }
    componentDidCatch() { this.props.onError(); }
    render() { return this.state.failed ? null : this.props.children; }
}
