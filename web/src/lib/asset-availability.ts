import { getActiveUserScope } from "@/lib/user-scope";

export const ASSET_DELETING_MESSAGE = "删除中，失败将自动重试";

type AssetAvailability = { id?: string; status?: string };

// An acceptance receipt fences even remote-only entries in independent query caches.
// Keep the receipt for this browser session: a late active page must not revive an ID.
const acceptedDeletions = new Map<string, Set<string>>();

export function recordAcceptedAssetDeletions(ids: readonly string[]): void {
    const scope = getActiveUserScope();
    const accepted = acceptedDeletions.get(scope) ?? new Set<string>();
    for (const id of ids) accepted.add(id);
    acceptedDeletions.set(scope, accepted);
}

function isDeleting(asset: AssetAvailability | undefined): boolean {
    return asset?.status === "deleting" || !!(asset?.id && acceptedDeletions.get(getActiveUserScope())?.has(asset.id));
}

export function isAssetAvailableForReference(asset: AssetAvailability | undefined): boolean {
    return !!asset && !isDeleting(asset) && asset.status !== "archived";
}

export function assertAssetAvailableForReference(asset: AssetAvailability, latest?: AssetAvailability): void {
    if (isDeleting(asset) || isDeleting(latest)) throw new Error(ASSET_DELETING_MESSAGE);
    if (!isAssetAvailableForReference(asset) || (latest && !isAssetAvailableForReference(latest))) throw new Error("素材已归档，请先还原后再引用");
}
