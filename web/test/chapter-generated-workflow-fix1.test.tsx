import { afterAll, expect, test } from "bun:test";
import { Window } from "happy-dom";
const dom = new Window({ url: "http://localhost/" });
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key,value] of Object.entries({window:dom,document:dom.document,navigator:dom.navigator,localStorage:dom.localStorage,sessionStorage:dom.sessionStorage,ResizeObserver:dom.ResizeObserver,HTMLElement:dom.HTMLElement,Element:dom.Element,Node:dom.Node,SVGElement:dom.SVGElement,ShadowRoot:dom.ShadowRoot,getComputedStyle:dom.getComputedStyle.bind(dom),requestAnimationFrame:dom.requestAnimationFrame.bind(dom),cancelAnimationFrame:dom.cancelAnimationFrame.bind(dom),IS_REACT_ACT_ENVIRONMENT:true})) { originals.set(key,Object.getOwnPropertyDescriptor(globalThis,key)); Object.defineProperty(globalThis,key,{configurable:true,writable:true,value}); }
const { act }=await import("react");
const { createRoot }=await import("react-dom/client");
const { App, ConfigProvider }=await import("antd");
const { QueryClient,QueryClientProvider }=await import("@tanstack/react-query");
const { MemoryRouter }=await import("react-router");
const { default:Workflow }=await import("@/pages/projects/detail/workflow");
const { apiClient }=await import("@/services/api/request");
const { GeneratedBindingSelection }=await import("@/pages/projects/detail/chapter-generated-binding-selection");
const { useConfigStore, defaultConfig, createModelChannel }=await import("@/stores/use-config-store");
const originalAdapter=apiClient.defaults.adapter;
afterAll(()=>{apiClient.defaults.adapter=originalAdapter;dom.happyDOM.abort();for(const[key,value]of originals){if(value)Object.defineProperty(globalThis,key,value);else Reflect.deleteProperty(globalThis,key);}});
const settle=()=>new Promise(resolve=>setTimeout(resolve,60));
const button=(text:string)=>[...document.querySelectorAll<HTMLButtonElement>("button")].find(item=>item.textContent?.replace(/\s/g,"").includes(text));
const detail={project:{id:"p",stylePresetId:"chinese-2d"},units:[{id:"u",title:"chapter",sourceText:"story"}],shots:[],shotReferences:[],shotRevisions:[],shotArtifacts:[],assets:[],assetCandidates:[],workflows:[]} as never;
const key="project-storyboard-task:p:u";

for(const mode of ["invalid_id","no_rows","network","save_network"]){test(`workflow ${mode} retains retry task and exposes explicit discard of completed output`,async()=>{
 sessionStorage.setItem(key,"bad-task"); const queried:string[]=[];let writes=0;const created:any[]=[];
 const previousConfig=useConfigStore.getState().config;
 useConfigStore.setState({config:{...defaultConfig,model:"system::text",textModel:"system::text",channels:[createModelChannel({id:"system",scope:"system",baseUrl:"https://example.com",apiKey:"test-only",models:["text"]})]}});
 apiClient.defaults.adapter=async(config)=>{
  let data:any;
  if(config.url?.includes("/tasks/")){queried.push(config.url);data={id:"bad-task",type:"canvas_text",status:"succeeded",prompt:"",attempts:1,createdAt:"",updatedAt:"",resultJson:JSON.stringify({rows:mode==="no_rows"?[]:[{plotDescription:"door"}]})};}
  else if(config.url?.endsWith("resolve-generated")){if(mode==="network")throw new Error("network unavailable");data={taskId:"bad-task",baseline:{},rows:[{rowIndex:0,assetVersionIds:[],candidates:[],issues:mode==="save_network"?[]:[{kind:"invalid_id",message:"invalid id"}]}]};}
  else if(config.method==="put"&&mode==="save_network"){writes++;throw new Error("save network unavailable");}
  else if(config.url==="/projects/p/assets")data={assets:[],hasMore:false};
  else if(config.url==="/tasks"&&config.method==="post"){created.push(JSON.parse(config.data));throw new Error("stop at new task boundary");}
  else throw new Error(`unexpected write ${config.url}`);
  return {config,status:200,statusText:"OK",headers:{},data:{code:0,data,msg:""}};
 };
 const host=document.createElement("div");document.body.append(host);const root=createRoot(host);const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 try{
  await act(async()=>{root.render(<MemoryRouter><QueryClientProvider client={client}><ConfigProvider theme={{token:{motion:false}}}><App><Workflow detail={detail} projectId="p" unitId="u" stage="storyboard"/></App></ConfigProvider></QueryClientProvider></MemoryRouter>);await settle();});
  await act(async()=>{await settle();});
  await act(async()=>{button("生成章节分镜")!.click();await settle();});
  if(button("保留原分镜")) await act(async()=>{button("保留原分镜")!.click();await settle();});
  expect(sessionStorage.getItem(key)).toBe("bad-task");
  const discard=button("放弃上次结果并重新生成");expect(discard).toBeDefined();
  if(mode==="network"||mode==="save_network"){
   await act(async()=>{button("生成章节分镜")!.click();await settle();});
   expect(queried).toHaveLength(2);expect(sessionStorage.getItem(key)).toBe("bad-task");if(mode==="save_network")expect(writes).toBe(2);
  }else{
   await act(async()=>{discard!.click();await settle();});
   await act(async()=>{button("放弃并重新生成")!.click();await settle();});
   expect(sessionStorage.getItem(key)).toBeNull();expect(created).toHaveLength(1);expect(created[0]).toMatchObject({projectId:"p",operation:"storyboard",input:{chapterAssetContext:{projectId:"p",unitId:"u"}}});
  }
 }finally{await act(async()=>root.unmount());client.clear();host.remove();sessionStorage.clear();apiClient.defaults.adapter=originalAdapter;useConfigStore.setState({config:previousConfig});}
});}

test("shared generated binding dialog keeps all seven choices accessible and enforces six",async()=>{
 const ids=Array.from({length:7},(_,i)=>`v${i+1}`);let result:unknown;
 const resolution={taskId:"t",baseline:{},rows:[{rowIndex:0,assetVersionIds:ids,candidates:ids.map(id=>({assetId:id,name:id,assetVersionId:id,missingImage:true})),issues:[{kind:"capacity_exceeded",message:"超限选择"},{kind:"missing_image",message:"缺少图片"},{kind:"ambiguous",message:"同名问题"}]}]} as never;
 const host=document.createElement("div");document.body.append(host);const root=createRoot(host);
 try{
  await act(async()=>{root.render(<ConfigProvider theme={{token:{motion:false}}}><GeneratedBindingSelection resolution={resolution} onComplete={value=>{result=value;}}/></ConfigProvider>);await settle();});
  const inputs=[...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
  expect(inputs).toHaveLength(7);expect(inputs.every(input=>!input.checked)).toBe(true);
  expect(document.body.textContent).toContain("缺少图片");expect(document.body.textContent).toContain("同名问题");
  await act(async()=>{inputs[6].focus();inputs[6].click();});
  expect(document.activeElement).toBe(inputs[6]);
  await act(async()=>{button("保存所选引用")!.click();});expect(result).toEqual([["v7"]]);
  for(let i=0;i<5;i++)await act(async()=>inputs[i].click());
  expect(inputs[5].disabled).toBe(true);expect(inputs[6].disabled).toBe(false);
  await act(async()=>{button("保留原分镜")!.click();});expect(result).toBe(false);
 }finally{await act(async()=>root.unmount());host.remove();}
});
