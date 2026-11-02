import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { strToU8, zipSync } from "fflate";
import { performanceProject } from "../test/helpers/canvas-performance-fixture";
import type { CanvasExportFile } from "../src/types/canvas-export";

const count = Number(Bun.argv[2]);
const output = Bun.argv[3];
if (!Number.isSafeInteger(count) || count < 1 || count > 50000 || !output) throw new Error("用法：bun run scripts/create-canvas-performance-fixture.ts <1..50000> <output.zip>");
const manifest: CanvasExportFile = { app: "ymzy", version: 4, exportedAt: "2026-10-28T00:00:00.000Z", projects: [{ project: performanceProject(count), files: [] }] };
await mkdir(resolve(dirname(output)), { recursive: true });
await Bun.write(output, zipSync({ "projects.json": strToU8(JSON.stringify(manifest)) }));
console.log(`已生成 ${count} 节点夹具：${output}`);
