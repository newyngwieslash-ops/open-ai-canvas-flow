import { useMemo, useRef, useState } from "react";
import { Button } from "antd";
import { X } from "lucide-react";
import { canvasThemes } from "@/lib/canvas-theme";
import { isCanvasWorkflowProvider } from "@/lib/canvas/canvas-workflow";
import { useActiveTheme } from "@/stores/canvas/use-canvas-theme-store";
import type { CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import type { CanvasGenerationMode, CanvasNodeMetadata, CanvasWorkspaceMode } from "@/types/canvas";
import { resolveModelRequestConfig, useEffectiveConfig } from "@/stores/use-config-store";
import { normalizeGenerationNodeMentionTokens, type NodeGenerationInput } from "./canvas-node-generation";
import { CanvasPresetPicker, type CanvasPromptPreset } from "./canvas-preset-picker";
import { CanvasVideoPromptTools } from "./canvas-video-prompt-tools";
import { CanvasResourceMentionTextarea } from "./canvas-resource-mention-textarea";
import { getEditableSelection, setEditableSelection } from "./canvas-mention-editable";
import { composerReferences, insertComposerPreset } from "./canvas-composer-references";
import { CanvasTextLeaseIndicator } from "./use-canvas-text-lease";

type ComposerProps = {
    nodeId: string; canvasId: string;
    value: string; inputs: NodeGenerationInput[]; skillReferences?: CanvasResourceReference[];
    generationMode?: CanvasGenerationMode; metadata?: CanvasNodeMetadata;
    onChange: (value: string) => boolean | void; onMetadataChange?: (patch: Partial<CanvasNodeMetadata>) => void;
    onClose: () => void; workspaceMode?: CanvasWorkspaceMode;
};

export function CanvasConfigComposer({ nodeId, canvasId, value, inputs, skillReferences = [], generationMode = "image", metadata, onChange, onMetadataChange, onClose, workspaceMode = "professional" }: ComposerProps) {
    const readOnly = Boolean(metadata?.locked);
    const theme = canvasThemes[useActiveTheme()];
    const container = useRef<HTMLDivElement>(null);
    const caret = useRef<{ start: number; end: number } | null>(null);
    const [presetsOpen, setPresetsOpen] = useState(false);
    const config = useEffectiveConfig();
    const fisherSelected = resolveModelRequestConfig(config, metadata?.model || config.videoModel).interfaceType === "fisher-h3-video";
    const references = useMemo(() => composerReferences(inputs, skillReferences), [inputs, skillReferences]);
    const prompt = useMemo(() => normalizeGenerationNodeMentionTokens(value, inputs), [value, inputs]);
    const simple = workspaceMode === "simple";
    const workflow = generationMode === "video" && isCanvasWorkflowProvider(metadata);
    const updatePrompt = (next: string) => {
        if (readOnly) return false;
        if (next === prompt) return true;
        return onChange(next);
    };

    const rememberCaret = () => {
        const editor = container.current?.querySelector<HTMLElement>("[contenteditable='true'],textarea");
        caret.current = editor instanceof HTMLTextAreaElement ? { start: editor.selectionStart, end: editor.selectionEnd } : getEditableSelection(editor || null);
    };
    const insertPreset = (preset: CanvasPromptPreset) => {
        const position = caret.current || { start: prompt.length, end: prompt.length };
        const next = insertComposerPreset(prompt, position.start, position.end, preset.prompt);
        if (updatePrompt(next.text) === false) return;
        setPresetsOpen(false);
        requestAnimationFrame(() => {
            const editor = container.current?.querySelector<HTMLElement>("[contenteditable='true'],textarea");
            editor?.focus();
            if (editor instanceof HTMLTextAreaElement) editor.setSelectionRange(next.caret, next.caret);
            else if (editor) setEditableSelection(editor, next.caret);
        });
    };
    const frameOptions = references.filter((ref) => ref.kind === "image" && ref.previewUrl).map((ref) => {
        const input = inputs.find((item) => item.nodeId === ref.nodeId);
        // Fisher-H3 按素材库图片 ID 引用参考，其余渠道沿用节点 ID。
        return { nodeId: fisherSelected && input?.image ? input.image.id : ref.nodeId, label: ref.label, title: ref.title, previewUrl: ref.previewUrl };
    });
    return (
        <section data-canvas-no-zoom className="canvas-config-composer rounded-xl bg-surface-active p-4" style={{ background: theme.spatial.elevated, color: theme.node.text }}
            onPointerDown={(event) => event.stopPropagation()} onMouseDown={(event) => event.stopPropagation()} onWheel={(event) => event.stopPropagation()}>
            <header className="mb-3 flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <h3 className="text-xs font-semibold">{simple ? "快速生成" : "组装提示词"}</h3>
                    <p className="mt-1 text-xs opacity-60">{workflow ? "工作流按字段顺序接收连接素材" : "输入 @ 选择已连接素材或技能"}</p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    {!simple ? <CanvasPresetPicker mode={generationMode} skillReferences={skillReferences} open={presetsOpen} onOpenChange={setPresetsOpen} onSelect={insertPreset} /> : null}
                    <Button size="small" type="text" aria-label="关闭提示词编辑器" icon={<X className="size-4" />} onClick={onClose} />
                </div>
            </header>
            {generationMode === "video" && onMetadataChange && (!simple || fisherSelected) ? <div className="mb-3">
                <CanvasVideoPromptTools metadata={metadata} config={{ ...config, model: metadata?.model || config.videoModel }} frameOptions={frameOptions}
                    audioOptions={inputs.flatMap((input) => input.audio ? [{ id: input.audio.id, label: input.title }] : [])}
                    videoOptions={inputs.flatMap((input) => input.video ? [{ id: input.video.id, label: input.title }] : [])}
                    referenceMode={workflow ? "all" : "frames"}
                    referenceSummary={{ imageCount: inputs.filter((input) => input.type === "image" || input.type === "character").length, videoCount: inputs.filter((input) => input.type === "video").length, audioCount: inputs.filter((input) => input.type === "audio").length }} onMetadataChange={onMetadataChange} />
            </div> : null}
            <div ref={container} className="canvas-config-composer-editor rounded-lg" style={{ background: theme.node.fill }} onBlurCapture={rememberCaret} onKeyUpCapture={rememberCaret} onPointerUpCapture={rememberCaret}>
                <CanvasResourceMentionTextarea aria-label="组装生成提示词" value={prompt} references={references} readOnly={readOnly} onChange={updatePrompt} sendOnEnter={false}
                    placeholder="输入提示词，按 @ 引用连接素材或技能" className="min-h-32 max-h-[min(42vh,360px)] w-full px-4 py-3 text-sm leading-7"
                    onKeyDown={(event) => {
                        event.stopPropagation();
                        if (!readOnly && event.key === "/" && !simple && !event.nativeEvent.isComposing) requestAnimationFrame(() => { rememberCaret(); setPresetsOpen(true); });
                    }} />
            </div>
            <CanvasTextLeaseIndicator nodeId={nodeId} canvasId={canvasId} />
        </section>
    );
}
