import { describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import { canvasReferencePreviewRequested } from "../src/components/canvas/use-resolved-canvas-resource-references";

describe("canvas reference preview demand", () => {
    test("preview demand is explicit without filtering reference metadata", () => {
        expect(canvasReferencePreviewRequested("a", new Set())).toBe(false);
        expect(canvasReferencePreviewRequested("a", new Set(["a"]))).toBe(true);
        expect(canvasReferencePreviewRequested("b", new Set(["a"]))).toBe(false);
        expect(canvasReferencePreviewRequested("a")).toBe(true);
    });

    test("resolves only requested previews and restores referenced prompt chips without loading 2000 unreferenced assets", () => {
        const result = Bun.spawnSync([process.execPath, "test", "./test/fixtures/canvas-reference-preview-demand.harness.tsx"], {
            cwd: resolve(import.meta.dir, ".."),
            timeout: 15000,
        });
        const output = new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr);
        expect(output).toContain("2 pass");
        expect(output).toContain("0 fail");
        expect(result.exitCode).toBe(0);
    });
});
