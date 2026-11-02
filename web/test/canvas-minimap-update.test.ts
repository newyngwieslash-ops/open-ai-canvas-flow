import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
const source = readFileSync(new URL("../src/components/canvas/canvas-mini-map.tsx", import.meta.url), "utf8");
test("minimap viewport updates do not repaint the node background", () => {
    const callback = source.split("const updateViewportRect = useCallback")[1]?.split("useEffect")[0] || "";
    expect(callback).not.toContain("drawMinimap(");
    expect(source).toContain("ref={viewportRectRef}");
});
