import { useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent } from "react";
import { AGENT_LAUNCHER_POSITION_KEY, avoidAgentLauncherTargets, clampAgentLauncherPosition, moveAgentLauncher, restoreAgentLauncherPosition, type AgentLauncherGesture, type AgentLauncherPosition } from "@/lib/canvas/agent-launcher-position";

const viewport = () => ({ width: window.innerWidth, height: window.innerHeight });

export function useAgentLauncherPosition(onOpen: () => void, width = 76, height = 76) {
    const size = { width, height };
    const [preferredPosition, setPosition] = useState(() => {
        try {
            return restoreAgentLauncherPosition(localStorage.getItem(AGENT_LAUNCHER_POSITION_KEY), viewport(), size);
        } catch (error) {
            console.warn("Agent 入口位置无法读取", error);
            return restoreAgentLauncherPosition(null, viewport(), size);
        }
    });
    const [dragging, setDragging] = useState(false);
    const [placement, setPlacement] = useState({ position: preferredPosition, obscured: false });
    const launcherRef = useRef<HTMLButtonElement>(null);
    const position = dragging ? preferredPosition : placement.position;
    const gestureRef = useRef<(AgentLauncherGesture & { pointerId: number }) | null>(null);
    const suppressClick = useRef(false);

    const save = (next: AgentLauncherPosition) => {
        try {
            localStorage.setItem(AGENT_LAUNCHER_POSITION_KEY, JSON.stringify(next));
        } catch (error) {
            console.warn("Agent 入口位置无法保存", error);
        }
    };

    useLayoutEffect(() => {
        let frame: number | null = null;
        const visibleRect = (element: Element) => {
            // Hidden modal ancestors can retain nonzero descendant rectangles.
            for (let node: Element | null = element; node; node = node.parentElement) {
                const style = getComputedStyle(node);
                if (node.hasAttribute("hidden") || node.getAttribute("aria-hidden") === "true" || style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse") return null;
            }
            const rect = element.getBoundingClientRect();
            const view = viewport();
            return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < view.width && rect.top < view.height ? rect : null;
        };
        const observed = new Set<Element>();
        const resizeObserver = new ResizeObserver(() => schedule());
        const decorations = ".canvas-agent-launcher-label, .canvas-agent-launcher-badge, .canvas-agent-launcher-status";
        const measure = () => {
            frame = null;
            const launcher = launcherRef.current;
            const parts = launcher ? [...launcher.querySelectorAll(decorations)] : [];
            let footprint;
            if (launcher) {
                // display:none retains the mounted avatar but has no geometry.
                // Measure invisibly in this synchronous turn, then restore it.
                const { display, visibility } = launcher.style;
                if (display === "none") {
                    launcher.style.visibility = "hidden";
                    launcher.style.display = "";
                }
                try {
                    const root = launcher.getBoundingClientRect();
                    if (root.width > 0 && root.height > 0) {
                        const scaleX = root.width / width;
                        const scaleY = root.height / height;
                        footprint = { left: -12 - width * 0.0175, top: -12 - height * 0.0175, right: width * 1.0175 + 12, bottom: height * 1.0175 + 46 };
                        for (const part of parts) {
                            const rect = part.getBoundingClientRect();
                            if (!rect.width || !rect.height) continue;
                            // Normalize the current motion scale and reserve its
                            // maximum hover extent before the pointer enters.
                            footprint.left = Math.min(footprint.left, ((rect.left - root.left) / scaleX - width / 2) * 1.035 + width / 2 - 12);
                            footprint.right = Math.max(footprint.right, ((rect.right - root.left) / scaleX - width / 2) * 1.035 + width / 2 + 12);
                            footprint.top = Math.min(footprint.top, ((rect.top - root.top) / scaleY - height / 2) * 1.035 + height / 2 - 12);
                            footprint.bottom = Math.max(footprint.bottom, ((rect.bottom - root.top) / scaleY - height / 2) * 1.035 + height / 2 + 12);
                        }
                    }
                } finally {
                    launcher.style.display = display;
                    launcher.style.visibility = visibility;
                }
            }
            const targets = [...document.querySelectorAll("[data-agent-launcher-avoid], [role='dialog'][aria-modal='true'], [role='alertdialog'][aria-modal='true']")];
            const nextObserved = new Set<Element>([document.body, ...targets, ...parts, ...(launcher ? [launcher] : [])]);
            for (const target of observed) if (!nextObserved.has(target)) {
                resizeObserver.unobserve(target);
                observed.delete(target);
            }
            for (const target of nextObserved) if (!observed.has(target)) {
                resizeObserver.observe(target);
                observed.add(target);
            }
            const rects = targets.map((target) => ({ target, rect: visibleRect(target) })).filter((item) => item.rect !== null);
            const modalOpen = rects.some(({ target }) => target.getAttribute("aria-modal") === "true");
            const preferred = clampAgentLauncherPosition(preferredPosition, viewport(), size);
            const next = modalOpen ? null : dragging ? preferred : avoidAgentLauncherTargets(preferred, viewport(), size, rects.map(({ rect }) => rect!), footprint);
            setPlacement((current) => {
                const position = next ?? preferred;
                const obscured = next === null;
                return current.obscured === obscured && current.position.right === position.right && current.position.bottom === position.bottom ? current : { position, obscured };
            });
        };
        const schedule = () => {
            if (frame === null) frame = window.requestAnimationFrame(measure);
        };
        // Ignore animation/frame churn inside the launcher; observe only layout
        // events and relevant DOM mutations, coalesced into one measurement.
        const mutations = new MutationObserver((records) => {
            if (records.some((record) => !launcherRef.current?.contains(record.target) || record.type === "childList" || record.type === "characterData")) schedule();
        });
        mutations.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ["class", "style", "hidden", "aria-hidden", "aria-modal", "data-agent-launcher-avoid"] });
        measure();
        window.addEventListener("resize", schedule);
        window.addEventListener("scroll", schedule, true);
        return () => {
            if (frame !== null) window.cancelAnimationFrame(frame);
            mutations.disconnect();
            resizeObserver.disconnect();
            window.removeEventListener("resize", schedule);
            window.removeEventListener("scroll", schedule, true);
        };
    }, [width, height, preferredPosition.right, preferredPosition.bottom, dragging]);

    const onPointerDown = (event: PointerEvent<HTMLButtonElement>) => {
        event.stopPropagation();
        if (event.button !== 0 || !event.isPrimary || gestureRef.current) return;
        suppressClick.current = false;
        gestureRef.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, start: position, dragged: false };
        event.currentTarget.setPointerCapture(event.pointerId);
    };
    const onPointerMove = (event: PointerEvent<HTMLButtonElement>) => {
        event.stopPropagation();
        const gesture = gestureRef.current;
        if (!gesture || gesture.pointerId !== event.pointerId) return;
        const next = moveAgentLauncher(gesture, event.clientX, event.clientY, viewport(), size);
        gesture.dragged = next.dragged;
        if (!next.dragged) return;
        event.preventDefault();
        suppressClick.current = true;
        setDragging(true);
        setPosition(next.position);
    };
    const finish = (event: PointerEvent<HTMLButtonElement>, cancelled: boolean) => {
        event.stopPropagation();
        const gesture = gestureRef.current;
        if (!gesture || gesture.pointerId !== event.pointerId) return;
        gestureRef.current = null;
        const next = cancelled ? { dragged: gesture.dragged, position: clampAgentLauncherPosition(position, viewport(), size) } : moveAgentLauncher(gesture, event.clientX, event.clientY, viewport(), size);
        suppressClick.current = cancelled || next.dragged;
        setDragging(false);
        // A click/cancel without movement must not adopt an automatic detour.
        if (next.dragged) {
            setPosition(next.position);
            save(next.position);
        }
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    };
    const onClick = (event: MouseEvent<HTMLButtonElement>) => {
        event.stopPropagation();
        // Keyboard/assistive activation remains available after a drag or cancellation.
        if (event.detail !== 0 && suppressClick.current) {
            event.preventDefault();
            return;
        }
        onOpen();
    };
    const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
        event.stopPropagation();
        const delta = event.shiftKey ? 40 : 10;
        const offsets: Record<string, [number, number]> = { ArrowLeft: [delta, 0], ArrowRight: [-delta, 0], ArrowUp: [0, delta], ArrowDown: [0, -delta] };
        const offset = offsets[event.key];
        if (!offset) return;
        event.preventDefault();
        const next = clampAgentLauncherPosition({ right: position.right + offset[0], bottom: position.bottom + offset[1] }, viewport(), size);
        setPosition(next);
        save(next);
    };

    return {
        position,
        dragging,
        launcherRef,
        obscured: placement.obscured,
        handlers: {
            onPointerDown,
            onPointerMove,
            onPointerUp: (event: PointerEvent<HTMLButtonElement>) => finish(event, false),
            onPointerCancel: (event: PointerEvent<HTMLButtonElement>) => finish(event, true),
            onLostPointerCapture: (event: PointerEvent<HTMLButtonElement>) => finish(event, true),
            onClick,
            onKeyDown,
            onDoubleClick: (event: MouseEvent<HTMLButtonElement>) => event.stopPropagation(),
        },
    };
}
