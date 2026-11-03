import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import WorkflowProductionWorkbench from "@/pages/projects/detail/workflow-production-workbench";
import type { ProjectDetail } from "@/services/api/projects";

test("real storyboard workbench exposes chapter generation with its empty-shot action", () => {
    const detail = { project: { id:"p" }, units:[{id:"u",title:"chapter"}], shots:[], shotReferences:[], shotRevisions:[], shotArtifacts:[], assets:[], assetCandidates:[], workflows:[] } as unknown as ProjectDetail;
    const html = renderToString(<MemoryRouter><QueryClientProvider client={new QueryClient()}><App><WorkflowProductionWorkbench activeStage="storyboard" detail={detail} projectId="p" unitId="u" onSelectShot={()=>{}} onRefresh={async()=>{}} onAddShot={()=>{}} addingShot={false} {...{onGenerateStoryboard:()=>{},generatingStoryboard:false}} /></App></QueryClientProvider></MemoryRouter>);
    expect(html).toContain("生成章节分镜");
    expect(html).toContain("新增第一个分镜");
});

test("real workbench keeps missing bound-image feedback visible after persistence", () => {
    const shot={id:"s",unitId:"u",title:"shot",description:"door",durationMs:1000,position:0,status:"draft"};
    const detail={project:{id:"p"},units:[{id:"u",title:"chapter"}],shots:[shot],shotReferences:[{id:"r",shotId:"s",assetVersionId:"v",status:"linked",referencedVersion:{id:"v",assetId:"a",version:1,representations:[]}}],shotRevisions:[],shotArtifacts:[],assets:[],assetCandidates:[],workflows:[]} as unknown as ProjectDetail;
    const html=renderToString(<MemoryRouter><QueryClientProvider client={new QueryClient()}><App><WorkflowProductionWorkbench activeStage="storyboard" detail={detail} projectId="p" unitId="u" selectedShot={shot as never} onSelectShot={()=>{}} onRefresh={async()=>{}} onAddShot={()=>{}} addingShot={false} /></App></QueryClientProvider></MemoryRouter>);
    expect(html).toContain("绑定版本暂无参考图");
});
