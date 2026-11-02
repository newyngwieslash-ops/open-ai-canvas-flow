import { useCallback, useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from "react";
import { flushSync } from "react-dom";
import { applyCanvasNodeGeometryPreview } from "@/lib/canvas/canvas-live-viewport";
import { getNodeResizeMinSize } from "@/lib/canvas/node-registry";
import { solveNodeResize } from "@/lib/canvas/node-resize-geometry";
import type { CanvasNodeData, Position } from "@/types/canvas";

type WheelGeometry = ReturnType<typeof imageWheelSize>;

const sameGeometry = (
    a: { width: number; height: number; position: Position },
    b: { width: number; height: number; position: Position },
) => a.width === b.width && a.height === b.height && a.position.x === b.position.x && a.position.y === b.position.y;

function scheduleFrame(callback: FrameRequestCallback): number {
    if (typeof requestAnimationFrame === "function") return requestAnimationFrame(callback);
    return setTimeout(() => callback(Date.now()), 16) as unknown as number;
}

function cancelFrame(handle: number) {
    if (typeof cancelAnimationFrame === "function") {
        cancelAnimationFrame(handle);
        return;
    }
    clearTimeout(handle);
}

export function imageWheelSize(node: CanvasNodeData, delta: number) {
    const minimum = getNodeResizeMinSize({ ...node, metadata: { ...node.metadata, freeResize: false } });
    const factor = Math.exp(-Math.max(-240, Math.min(240, delta)) * 0.0015);
    const size = solveNodeResize(
        { position: node.position, width: node.width, height: node.height, minimumWidth: minimum.width, minimumHeight: minimum.height, ratio: node.width / node.height, corner: "bottom-right" },
        { x: node.width * (factor - 1), y: node.height * (factor - 1) },
    );
    return { ...size, position: { x: node.position.x + (node.width - size.width) / 2, y: node.position.y + (node.height - size.height) / 2 } };
}

export function useCanvasImageWheelResize({
    containerRef,
    projectId,
    nodesRef,
    selectedNodeIdsRef,
    historyPausedRef,
    onResize,
}: {
    containerRef?: RefObject<HTMLDivElement | null>;
    projectId: string;
    nodesRef: { current: CanvasNodeData[] };
    selectedNodeIdsRef: { current: Set<string> };
    historyPausedRef: { current: boolean };
    setNodes?: Dispatch<SetStateAction<CanvasNodeData[]>>;
    onResize: (id: string, width: number, height: number, position?: Position) => void;
}) {
    const active = useRef(false);
    const latestGeometry = useRef<{ id: string; size: WheelGeometry } | null>(null);
    const initialNode = useRef<CanvasNodeData | null>(null);
    const dirtyPreview = useRef(false);
    const frame = useRef<number | null>(null);
    const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const flush = useCallback(() => {
        if (frame.current !== null) cancelFrame(frame.current);
        frame.current = null;
        if (!dirtyPreview.current || !latestGeometry.current) return;
        dirtyPreview.current = false;
        const next = latestGeometry.current;
        applyCanvasNodeGeometryPreview(containerRef?.current ?? null, { nodeId: next.id, ...next.size });
    }, [containerRef]);
    const finish = useCallback(() => {
        if (timer.current) clearTimeout(timer.current);
        timer.current = null;
        if (!active.current) return;
        flush();
        const next = latestGeometry.current;
        const start = initialNode.current;
        const current = next ? nodesRef.current.find((node) => node.id === next.id) : undefined;
        active.current = false;
        historyPausedRef.current = false;
        if (next && start && current && !current.metadata?.locked && sameGeometry(current, start) && !sameGeometry(next.size, start)) {
            flushSync(() => onResize(next.id, next.size.width, next.size.height, next.size.position));
            applyCanvasNodeGeometryPreview(containerRef?.current ?? null, null, "committed");
        } else {
            applyCanvasNodeGeometryPreview(containerRef?.current ?? null, null, "restore");
        }
        latestGeometry.current = null;
        initialNode.current = null;
        dirtyPreview.current = false;
    }, [containerRef, flush, historyPausedRef, nodesRef, onResize]);
    useEffect(() => {
        const container = containerRef?.current ?? null;
        const keyup = (event: KeyboardEvent) => {
            if (event.key === "Control") finish();
        };
        window.addEventListener("keyup", keyup, true);
        window.addEventListener("pointerdown", finish, true);
        window.addEventListener("blur", finish);
        return () => {
            window.removeEventListener("keyup", keyup, true);
            window.removeEventListener("pointerdown", finish, true);
            window.removeEventListener("blur", finish);
            if (timer.current) clearTimeout(timer.current);
            if (frame.current !== null) cancelFrame(frame.current);
            frame.current = null;
            timer.current = null;
            if (active.current || latestGeometry.current) {
                applyCanvasNodeGeometryPreview(containerRef?.current || container, null, "restore");
            }
            latestGeometry.current = null;
            initialNode.current = null;
            dirtyPreview.current = false;
            if (active.current) historyPausedRef.current = false;
            active.current = false;
        };
    }, [containerRef, finish, historyPausedRef, projectId]);
    return useCallback(
        (delta: number) => {
            if (!Number.isFinite(delta) || !delta || (historyPausedRef.current && !active.current) || selectedNodeIdsRef.current.size !== 1) return false;
            const id = selectedNodeIdsRef.current.values().next().value;
            const node = nodesRef.current.find((node) => node.id === id);
            if (!node || node.type !== "image" || node.metadata?.locked || node.metadata?.imageLayerGroup || node.metadata?.imageLayer || !(node.width > 0 && node.height > 0)) return false;
            if (active.current && latestGeometry.current && latestGeometry.current.id !== node.id) {
                finish();
            }
            if (!active.current) initialNode.current = node;
            active.current = true;
            historyPausedRef.current = true;
            const latest = latestGeometry.current;
            const geometryNode = latest?.id === node.id ? { ...node, ...latest.size } : node;
            latestGeometry.current = { id: node.id, size: imageWheelSize(geometryNode, delta) };
            dirtyPreview.current = true;
            if (frame.current === null) frame.current = scheduleFrame(flush);
            if (timer.current) clearTimeout(timer.current);
            timer.current = setTimeout(finish, 240);
            return true;
        },
        [finish, flush, historyPausedRef, nodesRef, selectedNodeIdsRef],
    );
}
