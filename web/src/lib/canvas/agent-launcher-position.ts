export type AgentLauncherPosition = { right: number; bottom: number };
export type AgentLauncherViewport = { width: number; height: number };
export type AgentLauncherGesture = { x: number; y: number; start: AgentLauncherPosition; dragged: boolean };

export const AGENT_LAUNCHER_POSITION_KEY = "canvas:agent-launcher-position:v1";
export const AGENT_LAUNCHER_SIZE = 76;
const MARGIN = 20; // Includes the label below the orb.

export function clampAgentLauncherPosition(position: AgentLauncherPosition, viewport: AgentLauncherViewport, size = { width: AGENT_LAUNCHER_SIZE, height: AGENT_LAUNCHER_SIZE }): AgentLauncherPosition {
    const clamp = (value: number, extent: number, dimension: number) => {
        const available = Math.max(0, extent - dimension);
        const margin = Math.min(MARGIN, available / 2);
        return Math.min(Math.max(value, margin), available - margin);
    };
    return { right: clamp(position.right, viewport.width, size.width), bottom: clamp(position.bottom, viewport.height, size.height) };
}

export function restoreAgentLauncherPosition(raw: string | null, viewport: AgentLauncherViewport, size?: AgentLauncherViewport): AgentLauncherPosition {
    if (raw) {
        try {
            const value = JSON.parse(raw);
            if (value && Number.isFinite(value.right) && Number.isFinite(value.bottom)) return clampAgentLauncherPosition(value, viewport, size);
        } catch {
            // Damaged UI preferences must not hide the launcher.
        }
    }
    return clampAgentLauncherPosition({ right: MARGIN, bottom: MARGIN }, viewport, size);
}

export function moveAgentLauncher(gesture: AgentLauncherGesture, x: number, y: number, viewport: AgentLauncherViewport, size?: AgentLauncherViewport) {
    const dx = x - gesture.x;
    const dy = y - gesture.y;
    const dragged = gesture.dragged || Math.hypot(dx, dy) >= 6;
    return {
        dragged,
        position: clampAgentLauncherPosition(dragged ? { right: gesture.start.right - dx, bottom: gesture.start.bottom - dy } : gesture.start, viewport, size),
    };
}

export type AgentLauncherProtectedRect = { left: number; top: number; right: number; bottom: number };

// A nearest free axis-aligned rectangle touches the preferred point, viewport
// edge or an obstacle boundary. Search these finite candidates, never pixels.
export function avoidAgentLauncherTargets(preferred: AgentLauncherPosition, viewport: AgentLauncherViewport, size: AgentLauncherViewport, targets: AgentLauncherProtectedRect[], footprint = { left: -12, top: -12, right: size.width + 12, bottom: size.height + 46 }): AgentLauncherPosition | null {
    const position = clampAgentLauncherPosition(preferred, viewport, size);
    const min = clampAgentLauncherPosition({ right: 0, bottom: 0 }, viewport, size);
    const max = clampAgentLauncherPosition({ right: viewport.width, bottom: viewport.height }, viewport, size);
    const origin = { x: viewport.width - position.right - size.width, y: viewport.height - position.bottom - size.height };
    const bounds = { left: viewport.width - max.right - size.width, right: viewport.width - min.right - size.width, top: viewport.height - max.bottom - size.height, bottom: viewport.height - min.bottom - size.height };
    const blocked = targets.map((rect) => ({ left: rect.left - footprint.right, right: rect.right - footprint.left, top: rect.top - footprint.bottom, bottom: rect.bottom - footprint.top }));
    const clear = (x: number, y: number) => !blocked.some((rect) => x > rect.left && x < rect.right && y > rect.top && y < rect.bottom);
    if (clear(origin.x, origin.y)) return position;
    const xs = new Set([origin.x, bounds.left, bounds.right]);
    const ys = new Set([origin.y, bounds.top, bounds.bottom]);
    for (const rect of blocked) {
        if (rect.left >= bounds.left && rect.left <= bounds.right) xs.add(rect.left);
        if (rect.right >= bounds.left && rect.right <= bounds.right) xs.add(rect.right);
        if (rect.top >= bounds.top && rect.top <= bounds.bottom) ys.add(rect.top);
        if (rect.bottom >= bounds.top && rect.bottom <= bounds.bottom) ys.add(rect.bottom);
    }
    let best: AgentLauncherPosition | null = null;
    let distance = Infinity;
    for (const x of xs) for (const y of ys) {
        const nextDistance = (x - origin.x) ** 2 + (y - origin.y) ** 2;
        if (nextDistance < distance && clear(x, y)) {
            best = { right: viewport.width - x - size.width, bottom: viewport.height - y - size.height };
            distance = nextDistance;
        }
    }
    return best;
}
