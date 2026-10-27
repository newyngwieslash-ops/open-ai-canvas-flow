import { afterAll, expect, test } from "bun:test";
import { apiClient, ApiError } from "../src/services/api/request";
import * as projects from "../src/services/api/projects";

const original = apiClient.defaults.adapter;
afterAll(() => { apiClient.defaults.adapter = original; });

test("preview uses the business envelope, encoded scope and AbortSignal without swallowing errors", async () => {
    const preview = (projects as Record<string, any>).previewProjectUnitShotAssetBindings;
    expect(typeof preview).toBe("function");
    const input = { shotIds: ["s"] };
    const result = { baseline: { projectId: "p", unitId: "u", projectRevision: 1, shotIds: ["s"], shots: [], catalog: [] }, shots: [] };
    const controller = new AbortController();
    let seen: any;
    apiClient.defaults.adapter = async (config) => {
        seen = config;
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: result, msg: "" } };
    };
    expect(await preview("p/a", "u b", input, controller.signal)).toEqual(result);
    expect(seen.url).toBe("/projects/p%2Fa/units/u%20b/shot-asset-bindings/preview");
    expect(seen.method).toBe("post");
    expect(JSON.parse(seen.data)).toEqual(input);
    expect(seen.signal).toBe(controller.signal);
    apiClient.defaults.adapter = async (config) => ({ config, status: 200, statusText: "OK", headers: {}, data: { code: 400, reason: "bad_request", msg: "invalid" } });
    await expect(preview("p", "u", input)).rejects.toBeInstanceOf(ApiError);
    controller.abort();
    await expect(preview("p", "u", input, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
});
