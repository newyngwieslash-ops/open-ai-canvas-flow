// @ 引用候选菜单：分组列表、预览与定位。

import { useVirtualizer } from "@tanstack/react-virtual";
import { type CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { type MouseEvent, type PointerEvent, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { type AssetCategory } from "@/stores/use-asset-store";
import { ASSET_CATEGORY_LABELS } from "@/lib/asset-category";
import { CachedResourceImage } from "@/components/cached-resource-image";
import { createPortal } from "react-dom";
import { ArrowLeft, ChevronRight, FileText, Folder, Image as ImageIcon, Music2, Pencil, Search, UserRound, Video, Workflow } from "lucide-react";
import { CanvasNodeType } from "@/types/canvas";
import { primeVideoPreviewFrame } from "./canvas-mention-chips";
import { pointForOffset } from "./canvas-mention-editable";

const VIRTUAL_MENTION_LIST_THRESHOLD = 80;
const VIRTUAL_MENTION_ROW_HEIGHT = 44;

export function MentionMenu({
    anchor,
    connectedReferences,
    assetReferences,
    filteredReferences,
    query,
    cursorOffset,
    activeReferenceId,
    preferredWidth,
    onQueryChange,
    onClose,
    onSelect,
}: {
    anchor: HTMLElement;
    connectedReferences: CanvasResourceReference[];
    assetReferences: CanvasResourceReference[];
    filteredReferences: CanvasResourceReference[];
    query: string;
    cursorOffset: number;
    activeReferenceId?: string;
    preferredWidth: number;
    onQueryChange: (query: string) => void;
    onClose: () => void;
    onSelect: (reference: CanvasResourceReference) => void;
}) {
    const menuRef = useRef<HTMLDivElement | null>(null);
    const scrollRef = useRef<HTMLDivElement | null>(null);
    const selectedRef = useRef(false);
    const [category, setCategory] = useState<AssetCategory | null>(null);
    const [position, setPosition] = useState(() => mentionMenuPosition(anchor, cursorOffset, preferredWidth));

    useLayoutEffect(() => {
        let frame = 0;
        const updatePosition = () => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => setPosition(mentionMenuPosition(anchor, cursorOffset, preferredWidth)));
        };
        updatePosition();
        const observer = new ResizeObserver(updatePosition);
        observer.observe(anchor);
        window.addEventListener("resize", updatePosition);
        window.addEventListener("scroll", updatePosition, true);
        return () => {
            cancelAnimationFrame(frame);
            observer.disconnect();
            window.removeEventListener("resize", updatePosition);
            window.removeEventListener("scroll", updatePosition, true);
        };
    }, [anchor, cursorOffset, preferredWidth]);

    const stopCanvasInteraction = (event: PointerEvent | MouseEvent) => {
        event.stopPropagation();
    };
    const selectReference = (reference: CanvasResourceReference) => {
        if (selectedRef.current) return;
        selectedRef.current = true;
        onSelect(reference);
    };
    const categoryItems = Object.entries(ASSET_CATEGORY_LABELS)
        .map(([value, label]) => ({ value: value as AssetCategory, label, count: assetReferences.filter((item) => item.category === value).length }))
        .filter((item) => item.count > 0);
    const connectedNodes = connectedReferences.filter((item) => item.kind !== "skill");
    const skillReferences = connectedReferences.filter((item) => item.kind === "skill");
    const visibleReferences = query ? filteredReferences : category ? assetReferences.filter((item) => item.category === category) : [];

    useLayoutEffect(() => {
        const closeOnOutsidePointer = (event: globalThis.PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node) || menuRef.current?.contains(target) || anchor.contains(target)) return;
            onClose();
        };
        window.addEventListener("pointerdown", closeOnOutsidePointer, true);
        return () => window.removeEventListener("pointerdown", closeOnOutsidePointer, true);
    }, [anchor, onClose]);

    return createPortal(
        <div
            ref={menuRef}
            data-canvas-resource-mention-menu="true"
            className="canvas-resource-mention-menu fixed z-[var(--z-tooltip)]"
            data-placement={position.showAbove ? "top" : "bottom"}
            style={{ left: position.left, top: position.top, width: position.width, maxHeight: position.maxHeight, transform: position.showAbove ? "translateY(-100%)" : undefined }}
            onPointerDown={stopCanvasInteraction}
            onMouseDown={stopCanvasInteraction}
            onClick={(event) => event.stopPropagation()}
        >
            <div className="canvas-resource-mention-search">
                <Search aria-hidden />
                <input
                    value={query}
                    placeholder="搜索节点、素材或技能"
                    aria-label="搜索引用素材"
                    onChange={(event) => onQueryChange(event.target.value)}
                    onPointerDown={(event) => event.stopPropagation()}
                    onKeyDown={(event) => {
                        if (event.key === "Escape") {
                            event.preventDefault();
                            onClose();
                            anchor.focus();
                            return;
                        }
                        if (event.key === "Enter" && filteredReferences.length) {
                            event.preventDefault();
                            selectReference(filteredReferences[0]);
                        }
                    }}
                />
            </div>
            <div ref={scrollRef} className="canvas-resource-mention-scroll thin-scrollbar">
                {query ? (
                    <MentionReferenceList references={visibleReferences} activeReferenceId={activeReferenceId} scrollParentRef={scrollRef} onSelect={selectReference} />
                ) : category ? (
                    <>
                        <button type="button" className="canvas-resource-mention-back" onClick={() => setCategory(null)}>
                            <ArrowLeft aria-hidden />
                            <span>{ASSET_CATEGORY_LABELS[category]}</span>
                            <small>{visibleReferences.length}</small>
                        </button>
                        <MentionReferenceList references={visibleReferences} activeReferenceId={activeReferenceId} scrollParentRef={scrollRef} onSelect={selectReference} />
                    </>
                ) : (
                    <>
                        {connectedNodes.length ? (
                            <section className="canvas-resource-mention-section">
                                <h4>
                                    <span>画布节点</span>
                                    <small>{connectedNodes.length}</small>
                                </h4>
                                <MentionReferenceList references={connectedNodes} activeReferenceId={activeReferenceId} scrollParentRef={scrollRef} onSelect={selectReference} />
                            </section>
                        ) : null}
                        {skillReferences.length ? (
                            <section className="canvas-resource-mention-section">
                                <h4>
                                    <span>技能库</span>
                                    <small>{skillReferences.length}</small>
                                </h4>
                                <MentionReferenceList references={skillReferences} activeReferenceId={activeReferenceId} scrollParentRef={scrollRef} onSelect={selectReference} />
                            </section>
                        ) : null}
                        {categoryItems.length ? (
                            <section className="canvas-resource-mention-section">
                                <h4>
                                    <span>素材库</span>
                                    <small>{assetReferences.length}</small>
                                </h4>
                                {categoryItems.map((item) => (
                                    <button key={item.value} type="button" className="canvas-resource-mention-folder" onClick={() => setCategory(item.value)}>
                                        <Folder aria-hidden />
                                        <span>{item.label}</span>
                                        <small>{item.count}</small>
                                        <ChevronRight aria-hidden />
                                    </button>
                                ))}
                            </section>
                        ) : null}
                    </>
                )}
            </div>
        </div>,
        document.body,
    );
}

export function MentionReferenceList({
    references,
    activeReferenceId,
    scrollParentRef,
    onSelect,
}: {
    references: CanvasResourceReference[];
    activeReferenceId?: string;
    scrollParentRef?: RefObject<HTMLDivElement | null>;
    onSelect: (reference: CanvasResourceReference) => void;
}) {
    const activeRef = useRef<HTMLButtonElement | null>(null);
    const virtualized = references.length >= VIRTUAL_MENTION_LIST_THRESHOLD && Boolean(scrollParentRef);
    const virtualizer = useVirtualizer({
        count: virtualized ? references.length : 0,
        getScrollElement: () => scrollParentRef?.current ?? null,
        estimateSize: () => VIRTUAL_MENTION_ROW_HEIGHT,
        overscan: 8,
        initialRect: { width: 320, height: 280 },
    });

    useLayoutEffect(() => {
        if (!activeReferenceId) return;
        if (virtualized) {
            const index = references.findIndex((item) => item.id === activeReferenceId);
            if (index >= 0) virtualizer.scrollToIndex(index, { align: "auto" });
            return;
        }
        activeRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }, [activeReferenceId, references, virtualized, virtualizer]);

    if (!references.length) return <div className="canvas-resource-mention-empty">没有匹配的引用</div>;

    const renderRow = (reference: CanvasResourceReference, isActive: boolean, ref?: RefObject<HTMLButtonElement | null>) => (
        <button
            key={reference.id}
            ref={ref}
            type="button"
            className={`canvas-resource-mention-item ${reference.kind === "skill" ? "is-skill" : ""} ${isActive ? "is-active" : ""}`}
            aria-selected={isActive}
            onPointerDown={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onSelect(reference);
            }}
            onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onSelect(reference);
            }}
        >
            <ReferencePreview reference={reference} active={isActive} />
            <span className="canvas-resource-mention-copy">
                <span className="canvas-resource-mention-title-row">
                    <strong title={reference.label}>{reference.label}</strong>
                    {reference.kind === "skill" ? <em>技能</em> : null}
                </span>
                {reference.kind === "skill" ? (
                    <span className="canvas-resource-mention-meta">
                        <span>{reference.skill?.description || reference.text || "工作流技能"}</span>
                        <small>
                            {reference.skill?.version ? `v${reference.skill.version}` : ""}
                            {reference.skill?.fileCount ? ` · ${reference.skill.fileCount} 文件` : ""}
                        </small>
                    </span>
                ) : reference.text && reference.text !== reference.title ? (
                    <span className="canvas-resource-mention-meta">
                        <span>{reference.text}</span>
                    </span>
                ) : null}
            </span>
        </button>
    );

    if (virtualized) {
        const items = virtualizer.getVirtualItems();
        return (
            <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
                {items.map((item) => {
                    const reference = references[item.index];
                    if (!reference) return null;
                    return (
                        <div key={reference.id} data-index={item.index} ref={virtualizer.measureElement} style={{ position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${item.start}px)` }}>
                            {renderRow(reference, reference.id === activeReferenceId)}
                        </div>
                    );
                })}
            </div>
        );
    }

    return references.map((reference) => renderRow(reference, reference.id === activeReferenceId, reference.id === activeReferenceId ? activeRef : undefined));
}

export function ReferencePreview({ reference, active = false }: { reference: CanvasResourceReference; active?: boolean }) {
    const Icon = reference.sourceType === CanvasNodeType.Drawing ? Pencil : reference.kind === "character" ? UserRound : reference.kind === "audio" ? Music2 : reference.kind === "video" ? Video : reference.kind === "image" ? ImageIcon : FileText;
    const fallback = (
        <span className="canvas-resource-mention-preview is-fallback">
            <Icon aria-hidden />
        </span>
    );
    if (reference.kind === "image" && (reference.storageKey || reference.previewUrl)) {
        return <CachedResourceImage storageKey={reference.storageKey} src={reference.previewUrl} variant="thumbnail" fallback={fallback} alt="" className="canvas-resource-mention-preview is-image" />;
    }
    if (reference.kind === "video" && reference.previewStorageKey) {
        return <CachedResourceImage storageKey={reference.previewStorageKey} src={reference.previewUrl} variant="thumbnail" fallback={fallback} alt="" className="canvas-resource-mention-preview is-video" loading="lazy" decoding="async" />;
    }
    if (reference.kind === "video" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="canvas-resource-mention-preview is-video" loading="lazy" decoding="async" />;
    if (reference.kind === "video" && reference.mediaUrl && active) {
        return <video src={reference.mediaUrl} aria-hidden="true" muted playsInline preload="metadata" className="canvas-resource-mention-preview is-video" onLoadedMetadata={(event) => primeVideoPreviewFrame(event.currentTarget)} />;
    }
    if (reference.kind === "character" && (reference.storageKey || reference.previewUrl)) {
        return <CachedResourceImage storageKey={reference.storageKey} src={reference.previewUrl} variant="thumbnail" fallback={fallback} alt="" className="canvas-resource-mention-preview is-character" />;
    }
    if (reference.kind === "skill") {
        return (
            <span className="canvas-resource-mention-preview is-skill">
                <Workflow aria-hidden />
            </span>
        );
    }
    return fallback;
}

export type MentionAnchorRect = Pick<DOMRect, "left" | "right" | "top" | "bottom" | "width" | "height">;

export function mentionMenuPosition(anchor: HTMLElement, cursorOffset: number, preferredWidth: number) {
    const caret = mentionCaretRect(anchor, cursorOffset);
    const boundary = anchor.closest(".ant-modal-container")?.getBoundingClientRect() || { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
    const inset = 8;
    const gap = 8;
    const availableWidth = Math.max(0, boundary.right - boundary.left - inset * 2);
    const width = Math.min(preferredWidth, availableWidth);
    const left = clamp(caret.left, boundary.left + inset, boundary.right - width - inset);
    const spaceAbove = Math.max(0, caret.top - boundary.top - gap - inset);
    const spaceBelow = Math.max(0, boundary.bottom - caret.bottom - gap - inset);
    const showAbove = spaceBelow < 220 && spaceAbove > spaceBelow;
    const maxHeight = Math.min(320, showAbove ? spaceAbove : spaceBelow);
    const top = showAbove ? caret.top - gap : caret.bottom + gap;
    return { left, top, width, maxHeight, showAbove };
}

export function mentionCaretRect(anchor: HTMLElement, cursorOffset: number): MentionAnchorRect {
    if (anchor instanceof HTMLTextAreaElement) return textareaCaretRect(anchor, cursorOffset);
    const point = pointForOffset(anchor, cursorOffset);
    const range = document.createRange();
    range.setStart(point.node, point.offset);
    range.collapse(true);
    const rangeRect = range.getClientRects()[0] || range.getBoundingClientRect();
    if (rangeRect && (rangeRect.height || rangeRect.width)) return rangeRect;
    return fallbackCaretRect(anchor);
}

export function textareaCaretRect(textarea: HTMLTextAreaElement, cursorOffset: number): MentionAnchorRect {
    const rect = textarea.getBoundingClientRect();
    const computed = window.getComputedStyle(textarea);
    const mirror = document.createElement("div");
    const marker = document.createElement("span");
    const copiedProperties = [
        "boxSizing",
        "borderTopWidth",
        "borderRightWidth",
        "borderBottomWidth",
        "borderLeftWidth",
        "paddingTop",
        "paddingRight",
        "paddingBottom",
        "paddingLeft",
        "fontFamily",
        "fontSize",
        "fontStyle",
        "fontVariant",
        "fontWeight",
        "fontStretch",
        "lineHeight",
        "letterSpacing",
        "textAlign",
        "textIndent",
        "textTransform",
        "wordSpacing",
        "tabSize",
        "wordBreak",
        "overflowWrap",
    ] as const;
    copiedProperties.forEach((property) => {
        mirror.style[property] = computed[property];
    });
    Object.assign(mirror.style, {
        position: "fixed",
        left: `${rect.left}px`,
        top: `${rect.top}px`,
        width: `${rect.width}px`,
        height: "auto",
        minHeight: "0",
        maxHeight: "none",
        overflow: "hidden",
        visibility: "hidden",
        pointerEvents: "none",
        whiteSpace: "pre-wrap",
        zIndex: "-1",
    });
    mirror.append(document.createTextNode(textarea.value.slice(0, Math.max(0, cursorOffset))));
    marker.textContent = "\u200b";
    mirror.append(marker);
    document.body.append(mirror);
    const markerRect = marker.getBoundingClientRect();
    mirror.remove();

    const lineHeight = Number.parseFloat(computed.lineHeight) || Number.parseFloat(computed.fontSize) * 1.4 || 20;
    const top = clamp(markerRect.top - textarea.scrollTop, rect.top, rect.bottom - lineHeight);
    const left = clamp(markerRect.left - textarea.scrollLeft, rect.left, rect.right);
    return { left, right: left, top, bottom: top + lineHeight, width: 0, height: lineHeight };
}

export function fallbackCaretRect(anchor: HTMLElement): MentionAnchorRect {
    const rect = anchor.getBoundingClientRect();
    const computed = window.getComputedStyle(anchor);
    const lineHeight = Number.parseFloat(computed.lineHeight) || Number.parseFloat(computed.fontSize) * 1.4 || 20;
    const left = rect.left + Number.parseFloat(computed.paddingLeft || "0");
    const top = rect.top + Number.parseFloat(computed.paddingTop || "0");
    return { left, right: left, top, bottom: top + lineHeight, width: 0, height: lineHeight };
}

export function clamp(value: number, min: number, max: number) {
    if (max < min) return min;
    return Math.min(Math.max(value, min), max);
}
