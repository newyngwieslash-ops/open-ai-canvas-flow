import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");

test("dead toolbar state is removed from all consumers", () => {
    for (const path of ["pages/canvas/project.tsx", "pages/canvas/shared.tsx", "pages/canvas/use-canvas-viewport-controller.ts", "pages/canvas/use-canvas-node-editor.ts", "pages/canvas/use-canvas-media-tools.ts"]) {
        expect(source(path)).not.toMatch(/\b(?:toolbarNodeId|setToolbarNodeId|toolbarHideTimerRef)\b/);
    }
    expect(source("components/canvas/canvas-node-toolbar.tsx")).not.toMatch(/\b(?:onKeep|onLeave)\b/);
});

test("script handles use the existing committed inverse scale", () => {
    expect(source("components/canvas/canvas-script-node.tsx")).toContain("calc(32px * var(--canvas-live-inverse-scale, 1))");
    expect(source("pages/canvas/project.tsx")).not.toContain("scale={viewport.k}");
});
