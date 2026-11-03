import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";
import { clampAgentLauncherPosition, moveAgentLauncher, restoreAgentLauncherPosition } from "../src/lib/canvas/agent-launcher-position";

const viewport = { width: 1280, height: 800 };
const start = { right: 20, bottom: 20 };

test("Live2D bounds use actual rectangular dimensions when restoring and dragging", () => {
    const size = { width: 180, height: 240 };
    const small = { width: 320, height: 400 };
    expect(restoreAgentLauncherPosition('{"right":999,"bottom":999}', small, size)).toEqual({ right: 120, bottom: 140 });
    expect(moveAgentLauncher({ start, x: 0, y: 0, dragged: true }, -999, -999, small, size).position).toEqual({ right: 120, bottom: 140 });
});

test("launcher restores a valid position and defaults safely for corrupt preferences", () => {
    expect(restoreAgentLauncherPosition('{"right":240,"bottom":180}', viewport)).toEqual({ right: 240, bottom: 180 });
    for (const raw of [null, "invalid", "null", "{}", '{"right":"20","bottom":20}', '{"right":1e999,"bottom":20}']) {
        expect(restoreAgentLauncherPosition(raw, viewport)).toEqual(start);
    }
});

test("launcher stays within all four edges and clamps after a viewport shrink", () => {
    expect(clampAgentLauncherPosition({ right: -500, bottom: -500 }, viewport)).toEqual(start);
    expect(clampAgentLauncherPosition({ right: 5000, bottom: 5000 }, viewport)).toEqual({ right: 1184, bottom: 704 });
    expect(clampAgentLauncherPosition({ right: 1000, bottom: 500 }, { width: 320, height: 300 })).toEqual({ right: 224, bottom: 204 });
    expect(clampAgentLauncherPosition(start, { width: 90, height: 90 })).toEqual({ right: 7, bottom: 7 });
});

test("small pointer jitter remains a click, but dragging back to the origin remains a drag", () => {
    const gesture = { start, x: 500, y: 500, dragged: false };
    expect(moveAgentLauncher(gesture, 502, 503, viewport)).toEqual({ dragged: false, position: start });
    expect(moveAgentLauncher(gesture, 400, 450, viewport)).toEqual({ dragged: true, position: { right: 120, bottom: 70 } });
    expect(moveAgentLauncher({ ...gesture, dragged: true }, 500, 500, viewport)).toEqual({ dragged: true, position: start });
});

// Execute the production hook with a tiny hook scheduler; only React and browser
// side effects are substituted, not the pointer/click handlers being tested.
const build = await Bun.build({
    entrypoints: [fileURLToPath(new URL("../src/components/canvas/use-agent-launcher-position.ts", import.meta.url))],
    target: "browser",
    format: "cjs",
    plugins: [
        {
            name: "hook-scheduler",
            setup(builder) {
                builder.onResolve({ filter: /^react$/ }, () => ({ path: "react", namespace: "test" }));
                builder.onLoad({ filter: /.*/, namespace: "test" }, () => ({ contents: "export const { useState, useRef, useEffect, useLayoutEffect } = globalThis.hooks;", loader: "js" }));
            },
        },
    ],
});
expect(build.success).toBe(true);
const source = await build.outputs[0].text();

function harness({ width = 76, height = 76, viewportHeight = 800, rects = [], raw = null, decoration = null } = {}) {
    const cells = [];
    let cursor = 0;
    let opens = 0;
    let dirty = false;
    const effects = [];
    const observers = [];
    const frames = new Map();
    let frameId = 0;
    const dom = new Window();
    const document = dom.document;
    const nodes = rects.map((rect) => addRegion(rect));
    const stored = new Map();
    if (raw) stored.set("canvas:agent-launcher-position:v1", raw);
    const listeners = new Map();
    const window = { innerWidth: 1280, innerHeight: viewportHeight, addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener: (name) => listeners.delete(name) };
    const module = { exports: {} };
    const hooks = {
        useState(initial) {
            const i = cursor++;
            if (!(i in cells)) cells[i] = typeof initial === "function" ? initial() : initial;
            return [cells[i], (next) => {
                const value = typeof next === "function" ? next(cells[i]) : next;
                dirty ||= !Object.is(cells[i], value);
                cells[i] = value;
            }];
        },
        useRef(initial) { const i = cursor++; return (cells[i] ||= { current: initial }); },
        useEffect(fn, deps) {
            const i = cursor++;
            if (!cells[i] || !deps || deps.some((dep, index) => !Object.is(dep, cells[i].deps?.[index]))) {
                const previous = cells[i];
                cells[i] = { deps };
                effects.push(() => { previous?.cleanup?.(); cells[i].cleanup = fn(); });
            }
        },
    };
    hooks.useLayoutEffect = hooks.useEffect;
    class Observer {
        targets = new Set();
        constructor(callback) { this.callback = callback; observers.push(this); }
        observe(target) { this.targets.add(target); }
        unobserve(target) { this.targets.delete(target); }
        disconnect() { this.targets.clear(); }
    }
    const requestAnimationFrame = (fn) => { frames.set(++frameId, fn); return frameId; };
    const cancelAnimationFrame = (id) => frames.delete(id);
    Object.assign(window, { requestAnimationFrame, cancelAnimationFrame });
    runInNewContext(source, { module, exports: module.exports, hooks, window, document, Element: dom.Element, HTMLElement: dom.HTMLElement, getComputedStyle: dom.getComputedStyle.bind(dom), MutationObserver: Observer, ResizeObserver: Observer, requestAnimationFrame, cancelAnimationFrame, localStorage: { getItem: (key) => stored.get(key) ?? null, setItem: (key, value) => stored.set(key, value) }, console });
    const render = () => { cursor = 0; return module.exports.useAgentLauncherPosition(() => opens++, width, height); };
    let current = render();
    const launcher = document.createElement("button");
    const label = document.createElement("span");
    label.className = "canvas-agent-launcher-label";
    launcher.append(label); document.body.append(launcher);
    let scale = 1;
    launcher.getBoundingClientRect = () => {
        if (launcher.style.display === "none") return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 };
        const left = window.innerWidth - current.position.right - width;
        const top = window.innerHeight - current.position.bottom - height;
        return { left: left + width * (1 - scale) / 2, top: top + height * (1 - scale) / 2, right: left + width * (1 + scale) / 2, bottom: top + height * (1 + scale) / 2, width: width * scale, height: height * scale };
    };
    label.getBoundingClientRect = () => {
        const root = launcher.getBoundingClientRect();
        const d = decoration ?? { left: 0, top: 0, right: 0, bottom: 0 };
        return { left: root.left + d.left * scale, right: root.left + d.right * scale, top: root.top + d.top * scale, bottom: root.top + d.bottom * scale, width: (d.right - d.left) * scale, height: (d.bottom - d.top) * scale };
    };
    if (decoration) current.launcherRef.current = launcher;
    function flush() {
        for (let round = 0; round < 12; round++) {
            while (effects.length) effects.shift()();
            const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach((fn) => fn());
            if (!dirty && !effects.length && !frames.size) return;
            dirty = false; current = render();
            launcher.style.display = current.obscured ? "none" : "";
        }
        throw new Error("launcher layout did not settle within 12 renders");
    }
    function addRegion(rect, modal = false) {
        const node = document.createElement(modal ? "section" : "div");
        if (modal) { node.setAttribute("role", "dialog"); node.setAttribute("aria-modal", "true"); }
        else node.setAttribute("data-agent-launcher-avoid", "");
        node.rect = rect;
        node.getBoundingClientRect = () => ({ ...node.rect, x: node.rect.left, y: node.rect.top, width: node.rect.right - node.rect.left, height: node.rect.bottom - node.rect.top });
        node.getClientRects = () => [node.getBoundingClientRect()];
        document.body.append(node);
        return node;
    }
    flush();
    let captured = null;
    const target = {
        setPointerCapture: (id) => { captured = id; },
        hasPointerCapture: (id) => captured === id,
        releasePointerCapture: () => { captured = null; },
    };
    return {
        fire(name, overrides = {}) {
            const event = { currentTarget: target, pointerId: 1, isPrimary: true, button: 0, clientX: 500, clientY: 500, detail: 1, preventDefault() {}, stopPropagation() {}, ...overrides };
            current.handlers[name](event); current = render(); flush();
        },
        resize(width, height) {
            window.innerWidth = width; window.innerHeight = height;
            listeners.get("resize")(); current = render(); flush();
        },
        changeLayout(fn) {
            fn({ nodes, addRegion, document });
            for (const observer of observers) if (observer.targets.size) observer.callback([{ target: document.body, type: "childList" }]);
            flush();
        },
        changeDecoration(next, nextScale = scale, className = label.className) {
            decoration = next; scale = nextScale;
            label.className = className;
            for (const observer of observers) if (observer.targets.size) observer.callback([{ target: label, type: "characterData" }]);
            flush();
        },
        scroll(fn) { fn(nodes); listeners.get("scroll")?.(); flush(); },
        dispose() {
            cells.forEach((cell) => cell?.cleanup?.());
            expect(listeners.size).toBe(0); expect(frames.size).toBe(0);
            expect(observers.every((observer) => observer.targets.size === 0)).toBe(true);
            dom.happyDOM.abort();
        },
        get state() { return current; },
        get opens() { return opens; },
        get saved() { return [...stored.values()].map(JSON.parse); },
        get captured() { return captured; },
    };
}

test("production handlers preserve click and suppress drag-release activation", () => {
    const h = harness();
    h.fire("onPointerDown");
    h.fire("onPointerMove", { clientX: 502 });
    h.fire("onPointerUp", { clientX: 502 });
    h.fire("onClick");
    expect(h.opens).toBe(1);
    h.fire("onPointerDown");
    h.fire("onPointerMove", { clientX: 400, clientY: 450 });
    expect(h.state.dragging).toBe(true);
    h.fire("onPointerUp", { clientX: 400, clientY: 450 });
    h.fire("onClick");
    expect(h.opens).toBe(1);
    expect(h.state.position).toEqual({ right: 120, bottom: 70 });
    expect(h.saved).toEqual([{ right: 120, bottom: 70 }]);
    expect(h.captured).toBe(null);
    h.fire("onClick", { detail: 0 });
    expect(h.opens).toBe(2);
    h.fire("onPointerDown");
    h.fire("onPointerUp");
    h.fire("onClick");
    expect(h.opens).toBe(3);
});

test("measured long launcher label avoids actions and remeasures text, hover and hidden restoration", () => {
    const preferred = { right: 204, bottom: 244 };
    const target = { left: 900, top: 500, right: 980, bottom: 520 };
    const longLabel = { left: -90, top: 93, right: 166, bottom: 110 };
    const shortLabel = { left: 20, top: 93, right: 56, bottom: 110 };
    const h = harness({ viewportHeight: 720, rects: [target], raw: JSON.stringify(preferred), decoration: shortLabel });
    let modal;
    try {
        expect(h.state.position).toEqual(preferred);
        h.changeDecoration(longLabel);
        expect(h.state.position).not.toEqual(preferred);
        const clear = (viewportHeight = 720) => {
            const left = 1280 - h.state.position.right - 76;
            const top = viewportHeight - h.state.position.bottom - 76;
            return !(left + longLabel.left < target.right && left + longLabel.right > target.left && top + longLabel.top < target.bottom && top + longLabel.bottom > target.top);
        };
        expect(clear()).toBe(true);
        const avoided = h.state.position;
        h.changeDecoration(longLabel, 1.035);
        expect(h.state.position.right).toBeCloseTo(avoided.right);
        expect(h.state.position.bottom).toBeCloseTo(avoided.bottom);
        h.changeLayout(({ addRegion }) => { modal = addRegion({ left: 0, top: 0, right: 1280, bottom: 720 }, true); });
        expect(h.state.obscured).toBe(true);
        h.changeDecoration(shortLabel);
        h.changeLayout(() => modal.remove());
        expect(h.state.obscured).toBe(false);
        expect(h.state.position).toEqual(preferred);
        h.changeLayout(({ addRegion }) => { modal = addRegion({ left: 0, top: 0, right: 1280, bottom: 720 }, true); });
        h.changeDecoration(longLabel);
        h.changeLayout(() => modal.remove());
        expect(h.state.obscured).toBe(false);
        expect(h.state.position).not.toEqual(preferred);
        expect(clear()).toBe(true);
        h.resize(1280, 740);
        expect(clear(740)).toBe(true);
        expect(h.saved).toEqual([preferred]);
    } finally { h.dispose(); }
});

test("measured pending badge left overflow is protected", () => {
    const preferred = { right: 204, bottom: 244 };
    const h = harness({ viewportHeight: 720, raw: JSON.stringify(preferred), rects: [{ left: 950, top: 400, right: 980, bottom: 420 }], decoration: { left: 0, top: 0, right: 0, bottom: 0 } });
    try {
        expect(h.state.position).toEqual(preferred);
        h.changeDecoration({ left: -34, top: 1, right: 14, bottom: 23 }, 1, "canvas-agent-launcher-badge");
        expect(h.state.position).not.toEqual(preferred);
        expect(h.saved).toEqual([preferred]);
    } finally { h.dispose(); }
});

for (const ending of ["onPointerCancel", "onLostPointerCapture"]) {
    test(`production handlers stop safely on ${ending} and ignore other pointers`, () => {
        const h = harness();
        h.fire("onPointerDown", { button: 2 });
        expect(h.captured).toBe(null);
        h.fire("onPointerDown", { pointerType: "touch" });
        h.fire("onPointerMove", { pointerId: 2, clientX: 100 });
        expect(h.state.position).toEqual(start);
        h.fire("onPointerMove", { clientX: 300 });
        h.fire(ending);
        h.fire("onPointerMove", { clientX: 100 });
        h.fire("onClick");
        expect(h.state.position).toEqual({ right: 220, bottom: 20 });
        expect(h.state.dragging).toBe(false);
        expect(h.captured).toBe(null);
        expect(h.opens).toBe(0);
    });
}

test("production handlers support keyboard movement and viewport resizing", () => {
    const h = harness();
    h.fire("onKeyDown", { key: "ArrowLeft" });
    h.fire("onKeyDown", { key: "ArrowUp", shiftKey: true });
    expect(h.state.position).toEqual({ right: 30, bottom: 60 });
    h.resize(100, 100);
    expect(h.state.position).toEqual({ right: 12, bottom: 12 });
    h.fire("onClick", { detail: 0 });
    expect(h.opens).toBe(1);
});

// Deterministic layout fixtures, not browser measurements. Use the actual bundle
// rectangle and three independently protected action rows, not an orb hit area.
const actionRows = [
    { left: 300, right: 586, top: 570, bottom: 630 },
    { left: 612, right: 898, top: 570, bottom: 630 },
    { left: 924, right: 1210, top: 570, bottom: 630 },
];
function overlaps(position, rect, size = { width: 244, height: 220 }, height = 720) {
    const left = 1280 - position.right - size.width;
    const top = height - position.bottom - size.height;
    return left < rect.right && left + size.width > rect.left && top < rect.bottom && top + size.height > rect.top;
}
function assetHarness(options = {}) {
    return harness({ width: 244, height: 220, viewportHeight: 720, rects: actionRows, ...options });
}

test("1280x720 three-card actions are not covered by the full launcher rectangle", () => {
    const h = assetHarness();
    try {
        expect(actionRows.some((rect) => overlaps(h.state.position, rect))).toBe(false);
        expect(h.state.position.right).toBe(20); // Nearest free space is above the rows, not the opposite corner.
        expect(h.state.position.bottom).toBeGreaterThanOrEqual(150);
        expect(h.saved).toEqual([]);
        h.fire("onClick"); expect(h.opens).toBe(1);
    } finally { h.dispose(); }
});

test("temporary avoidance restores saved preference after filtering/removing actions without persisting the detour", () => {
    const preferred = { right: 40, bottom: 30 };
    const h = assetHarness({ raw: JSON.stringify(preferred) });
    try {
        expect(actionRows.some((rect) => overlaps(h.state.position, rect))).toBe(false);
        expect(h.saved).toEqual([preferred]);
        h.changeLayout(({ nodes }) => nodes.forEach((node) => node.remove()));
        expect(h.state.position).toEqual(preferred);
        expect(h.saved).toEqual([preferred]);
    } finally { h.dispose(); }
});

test("scroll and action row resize/reflow recompute avoidance without user intervention", () => {
    const h = assetHarness();
    try {
        h.scroll((nodes) => nodes.forEach((node) => { node.rect = { ...node.rect, top: 50, bottom: 100 }; }));
        expect(h.state.position).toEqual(start);
        h.changeLayout(({ nodes }) => { nodes[2].rect = actionRows[2]; });
        expect(overlaps(h.state.position, actionRows[2])).toBe(false);
        h.resize(1280, 560);
        expect(h.state.position).toEqual(start); // Rows now below the viewport.
    } finally { h.dispose(); }
});

test("invisible, offscreen and hidden-ancestor action rows do not displace the launcher", () => {
    const h = assetHarness();
    try {
        h.changeLayout(({ nodes, document }) => {
            nodes[0].style.display = "none";
            nodes[1].style.visibility = "hidden";
            const hidden = document.createElement("div"); hidden.hidden = true;
            document.body.append(hidden); hidden.append(nodes[2]);
        });
        expect(h.state.position).toEqual(start);
    } finally { h.dispose(); }
});

test("visible modal suppresses the global launcher until close without losing position or unmounting", () => {
    const h = harness(); let dialog;
    try {
        h.changeLayout(({ addRegion }) => { dialog = addRegion({ left: 200, right: 1000, top: 100, bottom: 650 }, true); });
        expect(h.state.obscured).toBe(true);
        h.changeLayout(() => { dialog.style.display = "none"; });
        expect(h.state.obscured).toBe(false);
        expect(h.state.position).toEqual(start);
        h.changeLayout(() => { dialog.style.display = ""; });
        expect(h.state.obscured).toBe(true);
        h.changeLayout(() => dialog.remove());
        expect(h.state.obscured).toBe(false);
        expect(h.saved).toEqual([]);
    } finally { h.dispose(); }
});

test("no legal free rectangle temporarily suppresses launcher and recovers when space returns", () => {
    const full = { left: 0, top: 0, right: 1280, bottom: 720 };
    const h = assetHarness({ rects: [full] });
    try {
        expect(h.state.obscured).toBe(true);
        h.changeLayout(({ nodes }) => nodes[0].remove());
        expect(h.state.obscured).toBe(false);
        expect(h.state.position).toEqual(start);
        expect(h.saved).toEqual([]);
    } finally { h.dispose(); }
});

test("manual drag retains pointer capture and saves user choice while final presentation avoids protected actions", () => {
    const h = assetHarness();
    try {
        const avoided = h.state.position;
        expect(actionRows.some((rect) => overlaps(avoided, rect))).toBe(false);
        h.fire("onPointerDown");
        h.fire("onPointerMove", { clientY: 500 + avoided.bottom - 20 });
        expect(h.captured).toBe(1); expect(h.state.dragging).toBe(true);
        h.fire("onPointerUp", { clientY: 500 + avoided.bottom - 20 });
        expect(h.captured).toBe(null);
        expect(h.saved).toEqual([start]);
        expect(actionRows.some((rect) => overlaps(h.state.position, rect))).toBe(false);
        h.fire("onClick"); expect(h.opens).toBe(0);
        h.fire("onClick", { detail: 0 }); expect(h.opens).toBe(1);
    } finally { h.dispose(); }
});


test("ordinary pointer click on an avoided launcher never adopts the temporary detour as user preference", () => {
    const h = assetHarness();
    try {
        expect(actionRows.some((rect) => overlaps(h.state.position, rect))).toBe(false);
        h.fire("onPointerDown"); h.fire("onPointerUp"); h.fire("onClick");
        expect(h.opens).toBe(1); expect(h.saved).toEqual([]);
        h.changeLayout(({ nodes }) => nodes.forEach((node) => node.remove()));
        expect(h.state.position).toEqual(start);
    } finally { h.dispose(); }
});
