import { createElement } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterAll, describe, expect, test } from "bun:test";
import { Window } from "happy-dom";

import * as liveViewport from "@/lib/canvas/canvas-live-viewport";
import { imageWheelSize, useCanvasImageWheelResize } from "@/pages/canvas/use-canvas-image-wheel-resize";
import { CanvasNodeType, type CanvasNodeData, type Position } from "@/types/canvas";

const originalDescriptors = new Map<string, PropertyDescriptor | undefined>();
let activeDom: Window | null = null;

function setupDom() {
    activeDom?.happyDOM.abort();
    const dom = new Window({ url: "http://localhost/" });
    activeDom = dom;
    const globals = {
        window: dom,
        document: dom.document,
        navigator: dom.navigator,
        localStorage: dom.localStorage,
        sessionStorage: dom.sessionStorage,
        HTMLElement: dom.HTMLElement,
        HTMLDivElement: dom.HTMLDivElement,
        Element: dom.Element,
        Event: dom.Event,
        CustomEvent: dom.CustomEvent,
        KeyboardEvent: dom.KeyboardEvent,
        PointerEvent: dom.PointerEvent ?? dom.MouseEvent,
        IS_REACT_ACT_ENVIRONMENT: true,
    };
    for (const [key, value] of Object.entries(globals)) {
        if (!originalDescriptors.has(key)) {
            originalDescriptors.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
        }
        Object.defineProperty(globalThis, key, {
            configurable: true,
            writable: true,
            value,
        });
    }
    return dom;
}

afterAll(() => {
    activeDom?.happyDOM.abort();
    activeDom = null;
    for (const [key, descriptor] of originalDescriptors) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

function createImageNode(overrides?: Partial<CanvasNodeData>): CanvasNodeData {
    return {
        id: "img-1",
        type: CanvasNodeType.Image,
        title: "测试图片",
        position: { x: 100, y: 80 },
        width: 360,
        height: 240,
        metadata: {
            content: "/api/resources/img-1",
            storageKey: "resources/img-1.png",
            naturalWidth: 1200,
            naturalHeight: 800,
        },
        ...overrides,
    };
}

describe("canvas image wheel geometry preview", () => {
    test("20 wheel events across 5 rAF frames emit 5 previews and 0 onResize until finish commits once", async () => {
        setupDom();
        const container = document.createElement("div");
        const nodeElement = document.createElement("div");
        nodeElement.dataset.nodeId = "img-1";
        nodeElement.style.transform = "translate(100px, 80px)";
        nodeElement.style.width = "360px";
        nodeElement.style.height = "240px";
        container.appendChild(nodeElement);
        document.body.appendChild(container);

        const initial = createImageNode();
        const nodesRef = { current: [initial] };
        const selectedNodeIdsRef = { current: new Set(["img-1"]) };
        const historyPausedRef = { current: false };
        const containerRef = { current: container };
        const resizeCalls: Array<{ id: string; width: number; height: number; position?: Position }> = [];
        const previewEvents: Array< unknown > = [];

        const subscribe = (liveViewport as Record<string, unknown>).subscribeCanvasNodeGeometryPreview as
            | ((el: HTMLDivElement, cb: (preview: unknown) => void) => () => void)
            | undefined;
        const unsubscribe = subscribe ? subscribe(container, (preview) => previewEvents.push(preview)) : () => {};

        const rafQueue: FrameRequestCallback[] = [];
        const originalRaf = globalThis.requestAnimationFrame;
        const originalCaf = globalThis.cancelAnimationFrame;
        globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
            rafQueue.push(cb);
            return rafQueue.length;
        }) as typeof requestAnimationFrame;
        globalThis.cancelAnimationFrame = (() => {}) as typeof cancelAnimationFrame;

        let wheelHandler: ((delta: number) => boolean) | null = null;
        function Harness({ projectId }: { projectId: string }) {
            wheelHandler = useCanvasImageWheelResize({
                containerRef,
                projectId,
                nodesRef,
                selectedNodeIdsRef,
                historyPausedRef,
                setNodes: (updater) => {
                    nodesRef.current = typeof updater === "function" ? updater(nodesRef.current) : updater;
                },
                onResize: (id, width, height, position) => {
                    resizeCalls.push({ id, width, height, position });
                    nodesRef.current = nodesRef.current.map((node) =>
                        node.id === id ? { ...node, width, height, position: position || node.position, metadata: { ...node.metadata, manualSize: true } } : node,
                    );
                },
            });
            return createElement("div");
        }

        const host = document.createElement("div");
        const root = createRoot(host);
        await act(async () => {
            root.render(createElement(Harness, { projectId: "project-1" }));
        });

        try {
            let expectedNode = initial;
            for (let frameIndex = 0; frameIndex < 5; frameIndex += 1) {
                for (let step = 0; step < 4; step += 1) {
                    const handled = wheelHandler!(-30);
                    expect(handled).toBe(true);
                    expectedNode = { ...expectedNode, ...imageWheelSize(expectedNode, -30) };
                }
                expect(historyPausedRef.current).toBe(true);
                const nextFrame = rafQueue.shift();
                expect(nextFrame).toBeDefined();
                nextFrame!(performance.now());
            }

            // During the 5 rAF frames, onResize must NOT be called; preview must be emitted 5 times.
            expect(resizeCalls).toHaveLength(0);
            expect(previewEvents).toHaveLength(5);
            expect(parseFloat(nodeElement.style.width)).toBeCloseTo(expectedNode.width, 4);
            expect(parseFloat(nodeElement.style.height)).toBeCloseTo(expectedNode.height, 4);
            expect(nodeElement.style.transform).toContain("translate(");

            // Finish on Control keyup commits once and clears preview in committed mode.
            window.dispatchEvent(new KeyboardEvent("keyup", { key: "Control", bubbles: true }));
            expect(historyPausedRef.current).toBe(false);
            expect(resizeCalls).toHaveLength(1);
            expect(resizeCalls[0]?.width).toBeCloseTo(expectedNode.width, 8);
            expect(resizeCalls[0]?.height).toBeCloseTo(expectedNode.height, 8);
            expect(resizeCalls[0]?.position?.x).toBeCloseTo(expectedNode.position.x, 8);
            expect(resizeCalls[0]?.position?.y).toBeCloseTo(expectedNode.position.y, 8);
            expect(previewEvents[previewEvents.length - 1]).toBeNull();
            expect(parseFloat(nodeElement.style.width)).toBeCloseTo(expectedNode.width, 4);
        } finally {
            unsubscribe();
            globalThis.requestAnimationFrame = originalRaf;
            globalThis.cancelAnimationFrame = originalCaf;
            await act(async () => {
                root.unmount();
            });
        }
    });

    test("restores DOM preview and skips commit when node is externally moved or locked during gesture", async () => {
        setupDom();
        const container = document.createElement("div");
        const nodeElement = document.createElement("div");
        nodeElement.dataset.nodeId = "img-1";
        nodeElement.style.transform = "translate(100px, 80px)";
        nodeElement.style.width = "360px";
        nodeElement.style.height = "240px";
        container.appendChild(nodeElement);
        document.body.appendChild(container);

        const initial = createImageNode();
        const nodesRef = { current: [initial] };
        const selectedNodeIdsRef = { current: new Set(["img-1"]) };
        const historyPausedRef = { current: false };
        const containerRef = { current: container };
        const resizeCalls: Array<{ id: string; width: number; height: number; position?: Position }> = [];

        let wheelHandler: ((delta: number) => boolean) | null = null;
        function Harness() {
            wheelHandler = useCanvasImageWheelResize({
                containerRef,
                projectId: "project-1",
                nodesRef,
                selectedNodeIdsRef,
                historyPausedRef,
                setNodes: () => {},
                onResize: (id, width, height, position) => {
                    resizeCalls.push({ id, width, height, position });
                },
            });
            return createElement("div");
        }

        const host = document.createElement("div");
        const root = createRoot(host);
        await act(async () => {
            root.render(createElement(Harness));
        });

        try {
            expect(wheelHandler!(-60)).toBe(true);
            // Simulate remote geometry modification before gesture finishes
            nodesRef.current = [{ ...initial, position: { x: 220, y: 140 } }];
            window.dispatchEvent(new KeyboardEvent("keyup", { key: "Control", bubbles: true }));

            expect(resizeCalls).toHaveLength(0);
            expect(historyPausedRef.current).toBe(false);
            expect(nodeElement.style.transform).toBe("translate(100px, 80px)");
            expect(nodeElement.style.width).toBe("360px");
            expect(nodeElement.style.height).toBe("240px");
        } finally {
            await act(async () => {
                root.unmount();
            });
        }
    });

    test("cancels preview without committing when projectId changes mid-gesture, and preserves non-geometry metadata updates", async () => {
        setupDom();
        const container = document.createElement("div");
        const nodeElement = document.createElement("div");
        nodeElement.dataset.nodeId = "img-1";
        nodeElement.style.transform = "translate(100px, 80px)";
        nodeElement.style.width = "360px";
        nodeElement.style.height = "240px";
        container.appendChild(nodeElement);
        document.body.appendChild(container);

        const initial = createImageNode();
        const nodesRef = { current: [initial] };
        const selectedNodeIdsRef = { current: new Set(["img-1"]) };
        const historyPausedRef = { current: false };
        const containerRef = { current: container };
        const resizeCalls: Array<{ id: string; width: number; height: number; position?: Position }> = [];

        let wheelHandler: ((delta: number) => boolean) | null = null;
        function Harness({ projectId }: { projectId: string }) {
            wheelHandler = useCanvasImageWheelResize({
                containerRef,
                projectId,
                nodesRef,
                selectedNodeIdsRef,
                historyPausedRef,
                onResize: (id, width, height, position) => {
                    resizeCalls.push({ id, width, height, position });
                    nodesRef.current = nodesRef.current.map((node) =>
                        node.id === id ? { ...node, width, height, position: position || node.position, metadata: { ...node.metadata, manualSize: true } } : node,
                    );
                },
            });
            return createElement("div");
        }

        const host = document.createElement("div");
        const root = createRoot(host);
        await act(async () => {
            root.render(createElement(Harness, { projectId: "project-1" }));
        });

        try {
            expect(wheelHandler!(-60)).toBe(true);
            expect(historyPausedRef.current).toBe(true);

            // Switch project mid-gesture: should restore and not commit
            await act(async () => {
                root.render(createElement(Harness, { projectId: "project-2" }));
            });
            expect(historyPausedRef.current).toBe(false);
            expect(resizeCalls).toHaveLength(0);
            expect(nodeElement.style.width).toBe("360px");

            // Now start a new gesture and update non-geometry metadata concurrently
            expect(wheelHandler!(-60)).toBe(true);
            nodesRef.current = [{ ...initial, metadata: { ...initial.metadata, prompt: "remote-caption" } }];
            window.dispatchEvent(new KeyboardEvent("keyup", { key: "Control", bubbles: true }));
            expect(resizeCalls).toHaveLength(1);
            expect(nodesRef.current[0]?.metadata?.prompt).toBe("remote-caption");
            expect(nodesRef.current[0]?.metadata?.manualSize).toBe(true);
        } finally {
            await act(async () => {
                root.unmount();
            });
        }
    });
});
