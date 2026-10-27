import { useState } from "react";
import { Button, Checkbox } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";
import type { GeneratedBindingResolution } from "@/services/api/projects";

export function GeneratedBindingSelection({ resolution, onComplete }: { resolution: GeneratedBindingResolution; onComplete: (selection: string[][] | false) => void }) {
    const [selection, setSelection] = useState(() => resolution.rows.map((row) => row.assetVersionIds.length > 6 ? [] : [...row.assetVersionIds]));
    return <AppModal open title="确认分镜资产引用" onCancel={() => onComplete(false)} footer={<>
        <Button onClick={() => onComplete(false)}>保留原分镜</Button>
        <Button type="primary" onClick={() => onComplete(selection)}>保存所选引用</Button>
    </>}>
        {resolution.rows.map((row, index) => <fieldset key={row.rowIndex} className="mb-4 border-0 p-0">
            <legend>镜头 {row.rowIndex + 1} · 已选 {selection[index].length}/6</legend>
            {row.assetVersionIds.length > 6 ? <p>引用超过容量，请明确选择最多 6 个版本（也可不选择）。</p> : null}
            <ul>{row.issues.map((issue, issueIndex) => <li key={issueIndex}>{issue.message}</li>)}</ul>
            {row.assetVersionIds.map((id) => {
                const candidate = row.candidates.find((item) => item.assetVersionId === id);
                return <div key={id}><Checkbox checked={selection[index].includes(id)} disabled={!selection[index].includes(id) && selection[index].length >= 6} onChange={(event) => setSelection((current) => current.map((ids, rowIndex) => rowIndex !== index ? ids : event.target.checked ? [...ids, id] : ids.filter((value) => value !== id)))}>
                    {candidate?.name || id}{candidate?.missingImage ? "（暂无参考图）" : ""}
                </Checkbox></div>;
            })}
        </fieldset>)}
    </AppModal>;
}

export function useGeneratedBindingSelection() {
    const [pending, setPending] = useState<{ resolution: GeneratedBindingResolution; resolve: (selection: string[][] | false) => void }>();
    return {
        select: (_issues: unknown, resolution: GeneratedBindingResolution) => new Promise<string[][] | false>((resolve) => setPending({ resolution, resolve })),
        dialog: pending ? <GeneratedBindingSelection resolution={pending.resolution} onComplete={(selection) => { pending.resolve(selection); setPending(undefined); }} /> : null,
    };
}
