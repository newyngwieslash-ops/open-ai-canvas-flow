import type { CanvasOperationsRequest } from "@/services/api/user-data";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

const timestampField = "metadata.taskUpdatedAt";
const taskIdField = "metadata.taskId";
const owns = (value: object, key: string) => Object.prototype.hasOwnProperty.call(value, key);

function isTaskTimestamp(value: unknown): value is string {
    if (typeof value !== "string") return false;
    const match = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:[Zz]|([+-])(\d{2}):(\d{2}))$/.exec(value);
    if (!match) return false;
    const [, year, month, day, hour, minute, second, , offsetHour, offsetMinute] = match;
    const y = Number(year);
    const m = Number(month);
    const leap = y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0);
    const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    return m >= 1 && m <= 12 && Number(day) >= 1 && Number(day) <= days[m - 1]! && Number(hour) <= 23 && Number(minute) <= 59 && Number(second) <= 59 && (!offsetHour || (Number(offsetHour) <= 23 && Number(offsetMinute) <= 59));
}

/**
 * Only call after an explicit HTTP 409 canvas_field_conflict rejection, never
 * after an ambiguous transport failure. Uses the original snapshots, not a
 * refreshed server baseline. The caller must assign a new mutationId to retry.
 */
export function fenceRejectedTaskTimestampMutation(request: CanvasOperationsRequest, baseline?: CanvasProject, prepared?: CanvasProject): CanvasOperationsRequest | undefined {
    if (!baseline || !prepared || baseline.id !== prepared.id) return undefined;
    let changed = false;
    const operations: CanvasOperationsRequest["operations"] = [];
    for (const operation of request.operations) {
        if (operation.type !== "patch_node") {
            operations.push(operation);
            continue;
        }
        const { patch, before } = operation;
        // Backend timestamp max excludes any explicit taskId write, even an
        // unchanged value. Never partially repair a batch containing one.
        if (owns(patch, "metadata") || owns(patch, taskIdField) || (before && owns(before, "metadata"))) return undefined;
        const baseNode = baseline.nodes.find((node) => node.id === operation.id);
        const nextNode = prepared.nodes.find((node) => node.id === operation.id);
        const taskId = baseNode?.metadata?.taskId;
        if (!owns(patch, timestampField)) {
            operations.push(operation);
            continue;
        }
        // JSON wire encodes an absent original optional field as null.
        const originalTimestamp = before?.[timestampField];
        const baselineTimestamp = baseNode?.metadata?.taskUpdatedAt;
        const originalTimestampMatches = originalTimestamp === null ? baselineTimestamp == null : isTaskTimestamp(originalTimestamp) && originalTimestamp === baselineTimestamp;
        if (
            !isTaskTimestamp(patch[timestampField]) ||
            !before ||
            !owns(before, timestampField) ||
            !originalTimestampMatches ||
            typeof taskId !== "string" ||
            !taskId.trim() ||
            nextNode?.metadata?.taskId !== taskId ||
            patch[timestampField] !== nextNode?.metadata?.taskUpdatedAt
        )
            return undefined;
        if (owns(before, taskIdField)) {
            if (before[taskIdField] !== taskId) return undefined;
            operations.push(operation);
            continue;
        }
        operations.push({ ...operation, before: { ...before, [taskIdField]: taskId } });
        changed = true;
    }
    return changed ? { ...request, operations } : undefined;
}

/**
 * v1.6.8.5 overwrote before["metadata.taskId"] with the node's NEW taskId even
 * when the same patch was binding that task. Such a request claims "taskId is
 * already X, set it to X", which the server rejects forever. A real diff never
 * emits equal before/patch values, so that signature identifies the bug.
 *
 * Only call after an explicit HTTP 409 canvas_field_conflict rejection (the
 * transaction rolled back without a receipt). Restores the original baseline
 * value from the journal snapshot; the caller must assign a new mutationId.
 *//**
 * Older journals recorded task observer changes without the same-task identity
 * fence. A 409 proves that exact request rolled back, so it is safe to add the
 * unchanged task ID from the original snapshots and retry with a new mutation ID.
 * Task binding changes remain untouched and continue through strict CAS.
 */
export function fenceRejectedTaskObservationMutation(request: CanvasOperationsRequest, baseline?: CanvasProject, prepared?: CanvasProject): CanvasOperationsRequest | undefined {
    if (!baseline || !prepared || baseline.id !== prepared.id) return undefined;
    let changed = false;
    const operations: CanvasOperationsRequest["operations"] = [];
    for (const operation of request.operations) {
        if (operation.type !== "patch_node" || !operation.before || owns(operation.before, taskIdField) || owns(operation.patch, taskIdField) || owns(operation.patch, "metadata")) {
            operations.push(operation);
            continue;
        }
        const hasTaskObservation = Object.keys(operation.patch).some((key) => key.startsWith("metadata.task") && key !== taskIdField);
        if (!hasTaskObservation) {
            operations.push(operation);
            continue;
        }
        const baseNode = baseline.nodes.find((node) => node.id === operation.id);
        const nextNode = prepared.nodes.find((node) => node.id === operation.id);
        const taskId = baseNode?.metadata?.taskId;
        if (!baseNode || !nextNode || typeof taskId !== "string" || !taskId.trim() || nextNode.metadata?.taskId !== taskId) return undefined;
        operations.push({ ...operation, before: { ...operation.before, [taskIdField]: taskId } });
        changed = true;
    }
    return changed ? { ...request, operations } : undefined;
}

export function repairLegacyTaskIdFence(request: CanvasOperationsRequest, baseline?: CanvasProject, prepared?: CanvasProject): CanvasOperationsRequest | undefined {
    if (!baseline || !prepared || baseline.id !== prepared.id) return undefined;
    let changed = false;
    const operations: CanvasOperationsRequest["operations"] = [];
    for (const operation of request.operations) {
        if (operation.type !== "patch_node" || !operation.before || !owns(operation.patch, taskIdField) || !owns(operation.before, taskIdField)) {
            operations.push(operation);
            continue;
        }
        const nextTaskId = operation.patch[taskIdField];
        if (operation.before[taskIdField] !== nextTaskId) {
            operations.push(operation);
            continue;
        }
        const baseNode = baseline.nodes.find((node) => node.id === operation.id);
        const nextNode = prepared.nodes.find((node) => node.id === operation.id);
        // Without the original snapshot the true baseline is unknown; never guess.
        if (!baseNode || !nextNode || typeof nextTaskId !== "string" || !nextTaskId || nextNode.metadata?.taskId !== nextTaskId) return undefined;
        const originalTaskId = baseNode.metadata?.taskId ?? null;
        if (originalTaskId === nextTaskId) return undefined;
        operations.push({ ...operation, before: { ...operation.before, [taskIdField]: originalTaskId } });
        changed = true;
    }
    return changed ? { ...request, operations } : undefined;
}

/**
 * A journal replay can re-send create_node for a node the server already
 * committed (lost response or a stale acknowledged baseline). The server
 * rejects it with 409 canvas_field_conflict forever, so the request can never
 * succeed as-is and no local retry can fix it.
 *
 * Drop only create_node operations whose node id already exists on the remote
 * document. Existence is the convergence proof the same server check uses
 * ("显式新增不能覆盖已有节点"); the dropped op carried no field baseline, so
 * removing it keeps every other operation's preconditions intact. Content
 * divergence is not decided here: the save loop compares the refreshed remote
 * snapshot afterwards and reports a real conflict instead of overwriting.
 *
 * Returns the original request when nothing is dropped; the caller must
 * persist the replacement journal and assign a new mutationId.
 */
const staleTaskObserverFields = ["metadata.taskStatus", "metadata.taskProgress", "metadata.taskStage", "metadata.errorDetails"];

/** A 409 proves the transaction rolled back. Task observer fields are not user edits; drop their stale baselines so the remaining canvas edits can commit. */
export function dropStaleTaskObserverFields(request: CanvasOperationsRequest): CanvasOperationsRequest {
    let changed = false;
    const operations: CanvasOperationsRequest["operations"] = [];
    for (const operation of request.operations) {
        if (operation.type !== "patch_node" || !staleTaskObserverFields.some((field) => owns(operation.patch, field))) {
            operations.push(operation);
            continue;
        }
        const patch = { ...operation.patch };
        const before = operation.before ? { ...operation.before } : undefined;
        for (const field of staleTaskObserverFields) {
            delete patch[field];
            if (before) delete before[field];
        }
        changed = true;
        if (Object.keys(patch).length) operations.push({ ...operation, patch, ...(before ? { before } : {}) });
    }
    return changed ? { ...request, operations } : request;
}

export function dropConvergedCreateCanvasOperation(request: CanvasOperationsRequest, remote?: CanvasProject): CanvasOperationsRequest {
    if (!remote || remote.id !== request.operations.find((operation) => operation.type === "create_canvas")?.id) return request;
    const operations = request.operations.filter((operation) => operation.type !== "create_canvas");
    return operations.length === request.operations.length ? request : { ...request, operations };
}

export function dropConvergedCreateConnectionOperations(request: CanvasOperationsRequest, remote?: CanvasProject): CanvasOperationsRequest {
    if (!remote || !request.operations.some((operation) => operation.type === "create_connection")) return request;
    const remoteConnectionIds = new Set(remote.connections.map((connection) => connection.id));
    const operations = request.operations.filter((operation) => operation.type !== "create_connection" || !remoteConnectionIds.has(operation.connection.id));
    return operations.length === request.operations.length ? request : { ...request, operations };
}

export function dropConvergedCreateNodeOperations(request: CanvasOperationsRequest, remote?: CanvasProject): CanvasOperationsRequest {
    if (!remote || !request.operations.some((operation) => operation.type === "create_node")) return request;
    const remoteNodeIds = new Set(remote.nodes.map((node) => node.id));
    const operations = request.operations.filter((operation) => operation.type !== "create_node" || !remoteNodeIds.has(operation.node.id));
    return operations.length === request.operations.length ? request : { ...request, operations };
}
