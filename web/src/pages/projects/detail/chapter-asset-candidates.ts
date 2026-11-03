import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getActiveUserScope } from "@/lib/user-scope";
import { listProjectAssetsPage, type ProjectAsset } from "@/services/api/projects";

export type ChapterAssetCandidatesState =
    | { status: "loading" | "error" | "cancelled" }
    | { status: "complete"; assets: ProjectAsset[] };

export function useChapterAssetCandidates(projectId: string, enabled: boolean) {
    const queryClient = useQueryClient();
    const scope = getActiveUserScope();
    const queryKey = useMemo(() => ["project", projectId, "chapter-asset-candidates", scope], [projectId, scope]);
    const [cancelled, setCancelled] = useState(false);
    const query = useQuery({
        queryKey,
        queryFn: ({ signal }) => loadChapterAssetCandidates(projectId, signal),
        enabled: enabled && !cancelled,
        staleTime: 0,
        retry: false,
        refetchOnWindowFocus: false,
    });
    useEffect(() => {
        if (enabled) setCancelled(false);
        return () => { void queryClient.cancelQueries({ queryKey, exact: true }); };
    }, [enabled, queryClient, queryKey]);
    const state: ChapterAssetCandidatesState = !enabled || cancelled
        ? { status: "cancelled" }
        : query.isFetching || query.isPending
            ? { status: "loading" }
            : query.isError
                ? { status: query.error instanceof DOMException && query.error.name === "AbortError" ? "cancelled" : "error" }
                : { status: "complete", assets: query.data };
    return {
        state,
        cancel: () => {
            setCancelled(true);
            return queryClient.cancelQueries({ queryKey, exact: true });
        },
        retry: () => {
            setCancelled(false);
            return query.refetch();
        },
    };
}

export async function loadChapterAssetCandidates(projectId: string, signal?: AbortSignal): Promise<ProjectAsset[]> {
    const assets = new Map<string, ProjectAsset>();
    for (let page = 1; ; page++) {
        signal?.throwIfAborted();
        const result = await listProjectAssetsPage(projectId, { page, pageSize: 80 }, signal);
        signal?.throwIfAborted();
        for (const asset of result.assets) {
            if (!assets.has(asset.id)) assets.set(asset.id, asset);
        }
        if (!result.hasMore) break;
    }
    return [...assets.values()].filter((asset) => asset.category === "character" || asset.category === "environment" || asset.category === "prop");
}
