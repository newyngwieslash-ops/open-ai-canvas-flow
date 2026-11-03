import { describe, expect, test } from "bun:test";
import { apiClient, ApiError } from "../src/services/api/request";
import {
    STORYBOARD_ASSET_BINDING_CODES,
    STORYBOARD_ASSET_BINDING_REASONS,
    storyboardAssetBindingErrorMessage,
} from "../src/services/api/error-codes";
import {
    appendProjectUnitShotAssetBindings,
    type BindingAppendRequest,
    type BindingShotAppendResult,
} from "../src/services/api/projects";

const sampleRequest: BindingAppendRequest = {
    baseline: {
        projectId: "proj/1",
        unitId: "unit/2",
        projectRevision: 7,
        shotIds: ["shot-1"],
        shots: [
            {
                shotId: "shot-1",
                shotFingerprint: "sf-1",
                revisionFingerprint: "rf-1",
                references: [],
                artifacts: [],
            },
        ],
        catalog: [],
    },
    selections: [
        {
            shotId: "shot-1",
            assetVersionIds: ["ver-1"],
        },
    ],
};

describe("appendProjectUnitShotAssetBindings API", () => {
    test("posts to /shot-asset-bindings/append, unwraps data envelope, and never calls PUT /shots", async () => {
        const previousAdapter = apiClient.defaults.adapter;
        const calls: Array<{ method?: string; url?: string; data?: unknown }> = [];

        apiClient.defaults.adapter = async (config) => {
            calls.push({
                method: config.method,
                url: config.url,
                data: typeof config.data === "string" ? JSON.parse(config.data) : config.data,
            });
            return {
                config,
                status: 200,
                statusText: "OK",
                headers: {},
                data: {
                    code: 0,
                    msg: "ok",
                    data: {
                        projectRevision: 8,
                        unchanged: false,
                        addedCount: 1,
                        shots: [
                            {
                                shotId: "shot-1",
                                addedCount: 1,
                                addedVersionIds: ["ver-1"],
                                unchanged: false,
                                references: [
                                    {
                                        referenceId: "ref-1",
                                        assetVersionId: "ver-1",
                                        role: "reference",
                                        status: "linked",
                                    },
                                ],
                                artifacts: [
                                    {
                                        artifactId: "art-1",
                                        contentFingerprint: "cf-1",
                                        status: "stale",
                                        selected: false,
                                    },
                                ],
                            },
                            {
                                shotId: "shot-2",
                                addedVersionIds: [],
                                unchanged: true,
                            },
                        ],
                    },
                },
            };
        };

        try {
            const controller = new AbortController();
            const result = await appendProjectUnitShotAssetBindings(
                "proj/1",
                "unit/2",
                sampleRequest,
                controller.signal,
            );
            const firstShot: BindingShotAppendResult = result.shots[0]!;
            const secondShot: BindingShotAppendResult = result.shots[1]!;
            expect(calls).toHaveLength(1);
            expect(calls[0]?.method).toBe("post");
            expect(calls[0]?.url).toBe("/projects/proj%2F1/units/unit%2F2/shot-asset-bindings/append");
            expect(calls[0]?.data).toEqual(sampleRequest);
            expect(firstShot).toEqual({
                shotId: "shot-1",
                addedCount: 1,
                addedVersionIds: ["ver-1"],
                unchanged: false,
                references: [
                    {
                        referenceId: "ref-1",
                        assetVersionId: "ver-1",
                        role: "reference",
                        status: "linked",
                    },
                ],
                artifacts: [
                    {
                        artifactId: "art-1",
                        contentFingerprint: "cf-1",
                        status: "stale",
                        selected: false,
                    },
                ],
            });
            expect(secondShot).toEqual({
                shotId: "shot-2",
                addedCount: 0,
                addedVersionIds: [],
                unchanged: true,
                references: [],
                artifacts: [],
            });
        } finally {
            apiClient.defaults.adapter = previousAdapter;
        }
    });

    test("propagates ApiError code and reason for 400, 422, 409, and 503 responses", async () => {
        const previousAdapter = apiClient.defaults.adapter;
        const errorCases = [
            {
                status: 400,
                code: STORYBOARD_ASSET_BINDING_CODES.invalidRequest,
                reason: STORYBOARD_ASSET_BINDING_REASONS.invalidRequest,
            },
            {
                status: 422,
                code: STORYBOARD_ASSET_BINDING_CODES.invalidReference,
                reason: STORYBOARD_ASSET_BINDING_REASONS.invalidReference,
            },
            {
                status: 422,
                code: STORYBOARD_ASSET_BINDING_CODES.capacityExceeded,
                reason: STORYBOARD_ASSET_BINDING_REASONS.capacityExceeded,
            },
            {
                status: 409,
                code: STORYBOARD_ASSET_BINDING_CODES.conflict,
                reason: STORYBOARD_ASSET_BINDING_REASONS.conflict,
            },
            {
                status: 503,
                code: STORYBOARD_ASSET_BINDING_CODES.busy,
                reason: STORYBOARD_ASSET_BINDING_REASONS.busy,
            },
        ] as const;

        try {
            for (const tc of errorCases) {
                apiClient.defaults.adapter = async (config) => ({
                    config,
                    status: tc.status,
                    statusText: "Error",
                    headers: {},
                    data: {
                        code: tc.code,
                        reason: tc.reason,
                        msg: storyboardAssetBindingErrorMessage(tc.reason),
                    },
                });

                let caught: unknown;
                try {
                    await appendProjectUnitShotAssetBindings("proj/1", "unit/2", sampleRequest);
                } catch (err) {
                    caught = err;
                }
                expect(caught).toBeInstanceOf(ApiError);
                const apiErr = caught as ApiError;
                expect(apiErr.code).toBe(tc.code);
                expect(apiErr.reason).toBe(tc.reason);
                expect(storyboardAssetBindingErrorMessage(apiErr.reason)).toBeTruthy();
            }
        } finally {
            apiClient.defaults.adapter = previousAdapter;
        }
    });
});
